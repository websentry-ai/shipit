package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vigneshsubbiah/shipit/internal/auth"
	"github.com/vigneshsubbiah/shipit/internal/db"
	"github.com/vigneshsubbiah/shipit/internal/k8s"
	"github.com/vigneshsubbiah/shipit/internal/porter"
)

type Handler struct {
	db              *db.DB
	encryptKey      string
	appBaseDomain   string // e.g., "apps.shipit.unboundsec.dev"
	porterDiscovery *porter.DiscoveryService

	// Monitoring add-on: Grafana SSO is shipit-global, sourced from env at
	// boot. Empty values cause InstallMonitoring to fail with a clear error.
	grafanaGoogleClientID      string
	grafanaGoogleClientSecret  string
	grafanaGoogleAllowedDomain string

	// deployLocks serializes concurrent deploys of the same app. Two overlapping
	// deployApp goroutines would race on the Deployment spec (replicas, image)
	// and on the HPA (reconciled on every deploy now). sync.Map lets us allocate
	// a mutex per-appID lazily without a global lock. Blocking (not rejecting)
	// is deliberate: the HTTP handler has already returned 202, so the caller's
	// intent is preserved and the second deploy will converge on the final
	// state once the first finishes.
	deployLocks sync.Map // map[string]*sync.Mutex
}

func NewHandler(database *db.DB, encryptKey, appBaseDomain string, porterDiscovery *porter.DiscoveryService) *Handler {
	return &Handler{
		db:              database,
		encryptKey:      encryptKey,
		appBaseDomain:   appBaseDomain,
		porterDiscovery: porterDiscovery,
	}
}

// SetMonitoringConfig wires the shipit-global Grafana OAuth credentials into
// the handler. Called once at boot from NewRouter; kept as a setter so
// NewHandler doesn't grow another arg list.
func (h *Handler) SetMonitoringConfig(clientID, clientSecret, allowedDomain string) {
	h.grafanaGoogleClientID = clientID
	h.grafanaGoogleClientSecret = clientSecret
	h.grafanaGoogleAllowedDomain = allowedDomain
}

// lockAppDeploy returns an unlock function that must be called when the
// deploy goroutine is done. Blocks until any in-flight deploy for the same
// app has finished.
//
// Lock is held for the full deployApp duration including RunPreDeployJob,
// which can take minutes for migrations. A waiting caller's 202 has already
// been returned, so blocking preserves user intent; the log line below lets
// ops see queueing in logs when a deploy stacks up behind a long pre-deploy
// hook.
func (h *Handler) lockAppDeploy(appID string) func() {
	m, _ := h.deployLocks.LoadOrStore(appID, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	if !tryLock(mu) {
		log.Printf("deploy: waiting for in-flight deploy on app=%s", appID)
		mu.Lock()
	}
	return mu.Unlock
}

// tryLock is a best-effort non-blocking Lock. Used only for the logging
// fast-path above; correctness does not depend on it.
func tryLock(mu *sync.Mutex) bool {
	return mu.TryLock()
}

// Health check
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// Projects

func (h *Handler) ListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.db.ListProjects(r.Context())
	if err != nil {
		httpError(w, "failed to list projects", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(projects)
}

func (h *Handler) CreateProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		httpError(w, "name is required", http.StatusBadRequest)
		return
	}

	project, err := h.db.CreateProject(r.Context(), req.Name)
	if err != nil {
		httpError(w, "failed to create project", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(project)
}

func (h *Handler) GetProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "projectID")
	project, err := h.db.GetProject(r.Context(), id)
	if err != nil {
		httpError(w, "project not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(project)
}

func (h *Handler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "projectID")
	if err := h.db.DeleteProject(r.Context(), id); err != nil {
		httpError(w, "failed to delete project", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Clusters

func (h *Handler) ListClusters(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	clusters, err := h.db.ListClusters(r.Context(), projectID)
	if err != nil {
		httpError(w, "failed to list clusters", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(clusters)
}

func (h *Handler) ConnectCluster(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")

	var req struct {
		Name       string `json:"name"`
		Kubeconfig string `json:"kubeconfig"`
		// AWS EKS direct connection (alternative to kubeconfig)
		AWSClusterName string `json:"aws_cluster_name"`
		AWSRegion      string `json:"aws_region"`
		AWSEndpoint    string `json:"aws_endpoint"`
		AWSCAData      string `json:"aws_ca_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		httpError(w, "name is required", http.StatusBadRequest)
		return
	}

	var kubeconfig []byte

	// Option 1: Direct kubeconfig provided
	if req.Kubeconfig != "" {
		kubeconfig = []byte(req.Kubeconfig)
	} else if req.AWSClusterName != "" {
		// Option 2: AWS EKS direct connection (uses IRSA when running on AWS)
		if req.AWSEndpoint == "" || req.AWSCAData == "" {
			httpError(w, "aws_endpoint and aws_ca_data are required for AWS EKS connection", http.StatusBadRequest)
			return
		}
		region := req.AWSRegion
		if region == "" {
			region = k8s.GetAWSRegion()
		}

		var err error
		kubeconfig, err = k8s.GenerateAWSOIDCKubeconfig(k8s.AWSOIDCKubeconfigParams{
			ClusterName:     req.AWSClusterName,
			ClusterEndpoint: req.AWSEndpoint,
			ClusterCA:       req.AWSCAData,
			Region:          region,
		})
		if err != nil {
			httpError(w, "failed to generate kubeconfig: "+err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		httpError(w, "either kubeconfig or aws_cluster_name is required", http.StatusBadRequest)
		return
	}

	// Encrypt kubeconfig
	encrypted, err := auth.Encrypt(kubeconfig, h.encryptKey)
	if err != nil {
		httpError(w, "failed to encrypt kubeconfig", http.StatusInternalServerError)
		return
	}

	cluster, err := h.db.CreateCluster(r.Context(), projectID, req.Name, encrypted)
	if err != nil {
		httpError(w, "failed to create cluster", http.StatusInternalServerError)
		return
	}

	// Test connection in background
	go h.testClusterConnection(cluster.ID, kubeconfig)

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(cluster)
}

func (h *Handler) testClusterConnection(clusterID string, kubeconfig []byte) {
	ctx := context.Background()
	client, err := k8s.NewClient(kubeconfig)
	if err != nil {
		msg := err.Error()
		h.db.UpdateClusterStatus(ctx, clusterID, "error", &msg, "")
		return
	}

	info, err := client.GetClusterInfo()
	if err != nil {
		msg := err.Error()
		h.db.UpdateClusterStatus(ctx, clusterID, "error", &msg, "")
		return
	}

	h.db.UpdateClusterStatus(ctx, clusterID, "connected", nil, info.Endpoint)

	// Register cluster with Porter discovery service and trigger initial sync
	if h.porterDiscovery != nil {
		h.porterDiscovery.RegisterCluster(clusterID, kubeconfig)
		// Trigger immediate sync for this cluster
		go h.porterDiscovery.SyncCluster(ctx, clusterID, kubeconfig)
	}
}

func (h *Handler) GetCluster(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "clusterID")
	cluster, err := h.db.GetCluster(r.Context(), id)
	if err != nil {
		httpError(w, "cluster not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(cluster)
}

func (h *Handler) DeleteCluster(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "clusterID")

	// Unregister from Porter discovery before deleting
	if h.porterDiscovery != nil {
		h.porterDiscovery.UnregisterCluster(id)
	}

	if err := h.db.DeleteCluster(r.Context(), id); err != nil {
		httpError(w, "failed to delete cluster", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Apps

func (h *Handler) ListApps(w http.ResponseWriter, r *http.Request) {
	clusterID := chi.URLParam(r, "clusterID")
	apps, err := h.db.ListApps(r.Context(), clusterID)
	if err != nil {
		httpError(w, "failed to list apps", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(apps)
}

func (h *Handler) CreateApp(w http.ResponseWriter, r *http.Request) {
	clusterID := chi.URLParam(r, "clusterID")

	var req struct {
		Name      string            `json:"name"`
		Namespace string            `json:"namespace"`
		Image     string            `json:"image"`
		Replicas  int               `json:"replicas"`
		Port      *int              `json:"port"`
		EnvVars   map[string]string `json:"env_vars"`
		// Resource limits
		CPURequest    string `json:"cpu_request"`
		CPULimit      string `json:"cpu_limit"`
		MemoryRequest string `json:"memory_request"`
		MemoryLimit   string `json:"memory_limit"`
		// Health check
		HealthPath         *string `json:"health_path"`
		HealthPort         *int    `json:"health_port"`
		HealthInitialDelay *int    `json:"health_initial_delay"`
		HealthPeriod       *int    `json:"health_period"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.Image == "" {
		httpError(w, "name and image are required", http.StatusBadRequest)
		return
	}
	if req.Namespace == "" {
		req.Namespace = "default"
	}
	if req.Replicas <= 0 {
		req.Replicas = 1
	}
	// Apply default resource limits
	if req.CPURequest == "" {
		req.CPURequest = "100m"
	}
	if req.CPULimit == "" {
		req.CPULimit = "500m"
	}
	if req.MemoryRequest == "" {
		req.MemoryRequest = "128Mi"
	}
	if req.MemoryLimit == "" {
		req.MemoryLimit = "256Mi"
	}

	envVarsJSON, _ := json.Marshal(req.EnvVars)

	app, err := h.db.CreateApp(r.Context(), db.CreateAppParams{
		ClusterID:    clusterID,
		Name:         req.Name,
		Namespace:    req.Namespace,
		Image:        req.Image,
		Replicas:     req.Replicas,
		Port:         req.Port,
		EnvVars:      envVarsJSON,
		CPURequest:   req.CPURequest,
		CPULimit:     req.CPULimit,
		MemRequest:   req.MemoryRequest,
		MemLimit:     req.MemoryLimit,
		HealthPath:   req.HealthPath,
		HealthPort:   req.HealthPort,
		HealthDelay:  req.HealthInitialDelay,
		HealthPeriod: req.HealthPeriod,
	})
	if err != nil {
		httpError(w, "failed to create app", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(app)
}

func (h *Handler) GetApp(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "appID")
	app, err := h.db.GetApp(r.Context(), id)
	if err != nil {
		httpError(w, "app not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(app)
}

func (h *Handler) UpdateApp(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appID")

	// Verify app exists
	existing, err := h.db.GetApp(r.Context(), appID)
	if err != nil {
		httpError(w, "app not found", http.StatusNotFound)
		return
	}

	var req struct {
		Image         *string           `json:"image"`
		Replicas      *int              `json:"replicas"`
		EnvVars       map[string]string `json:"env_vars"`
		CPURequest    *string           `json:"cpu_request"`
		CPULimit      *string           `json:"cpu_limit"`
		MemoryRequest *string           `json:"memory_request"`
		MemoryLimit   *string           `json:"memory_limit"`
		HealthPath    *string           `json:"health_path"`
		HealthPort    *int              `json:"health_port"`
		HealthDelay   *int              `json:"health_initial_delay"`
		HealthPeriod  *int              `json:"health_period"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Build update params, using existing values as defaults
	image := existing.Image
	if req.Image != nil {
		image = *req.Image
	}
	replicas := existing.Replicas
	if req.Replicas != nil {
		replicas = *req.Replicas
	}
	cpuRequest := existing.CPURequest
	if req.CPURequest != nil {
		cpuRequest = *req.CPURequest
	}
	cpuLimit := existing.CPULimit
	if req.CPULimit != nil {
		cpuLimit = *req.CPULimit
	}
	memRequest := existing.MemoryRequest
	if req.MemoryRequest != nil {
		memRequest = *req.MemoryRequest
	}
	memLimit := existing.MemoryLimit
	if req.MemoryLimit != nil {
		memLimit = *req.MemoryLimit
	}

	// Handle env vars - merge with existing if partial update
	var envVarsJSON []byte
	if req.EnvVars != nil {
		envVarsJSON, _ = json.Marshal(req.EnvVars)
	} else {
		envVarsJSON = existing.EnvVars
	}

	// Health check settings
	healthPath := existing.HealthPath
	if req.HealthPath != nil {
		healthPath = req.HealthPath
	}
	healthPort := existing.HealthPort
	if req.HealthPort != nil {
		healthPort = req.HealthPort
	}
	healthDelay := existing.HealthInitialDelay
	if req.HealthDelay != nil {
		healthDelay = req.HealthDelay
	}
	healthPeriod := existing.HealthPeriod
	if req.HealthPeriod != nil {
		healthPeriod = req.HealthPeriod
	}

	app, err := h.db.UpdateApp(r.Context(), db.UpdateAppParams{
		ID:          appID,
		Image:       image,
		Replicas:    replicas,
		EnvVars:     envVarsJSON,
		CPURequest:  cpuRequest,
		CPULimit:    cpuLimit,
		MemRequest:  memRequest,
		MemLimit:    memLimit,
		HealthPath:  healthPath,
		HealthPort:  healthPort,
		HealthDelay: healthDelay,
		HealthPeriod: healthPeriod,
	})
	if err != nil {
		httpError(w, "failed to update app", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(app)
}

func (h *Handler) DeployApp(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appID")

	app, err := h.db.GetApp(r.Context(), appID)
	if err != nil {
		httpError(w, "app not found", http.StatusNotFound)
		return
	}

	cluster, err := h.db.GetCluster(r.Context(), app.ClusterID)
	if err != nil {
		httpError(w, "cluster not found", http.StatusNotFound)
		return
	}

	// Decrypt kubeconfig
	kubeconfig, err := auth.Decrypt(cluster.KubeconfigEncrypted, h.encryptKey)
	if err != nil {
		httpError(w, "failed to decrypt kubeconfig", http.StatusInternalServerError)
		return
	}

	// Update status to deploying
	h.db.UpdateAppStatus(r.Context(), appID, "deploying", nil)

	// Deploy in background
	go h.deployApp(appID, app, kubeconfig)

	json.NewEncoder(w).Encode(map[string]string{"status": "deploying"})
}

func (h *Handler) deployApp(appID string, app *db.App, kubeconfig []byte) {
	// Serialize concurrent deploys on the same app. Without this, two goroutines
	// would race on Deployment spec (replicas, image) and on the HPA (reconciled
	// on every deploy). Lock is acquired BEFORE the DB re-fetch so the second
	// goroutine reads fresh state after the first one's writes have landed.
	unlock := h.lockAppDeploy(appID)
	defer unlock()

	ctx := context.Background()
	client, err := k8s.NewClient(kubeconfig)
	if err != nil {
		msg := err.Error()
		h.db.UpdateAppStatus(ctx, appID, "failed", &msg)
		return
	}

	// Re-fetch the app inside the goroutine so we pick up any HPA / image /
	// env changes the user made between the HTTP handler returning and this
	// goroutine running. Prevents a SetAutoscaling (or similar) call mid-deploy
	// from being silently reverted by the stale snapshot the caller passed in.
	if fresh, err := h.db.GetApp(ctx, appID); err == nil {
		app = fresh
	}

	// Allocate the next revision number via MAX+1, NOT CurrentRevision+1.
	// CurrentRevision tracks the last successful deploy; after an auto-
	// rollback it regresses to the prior success, so CurrentRevision+1
	// collides with the rolled_back revision that still exists under the
	// UNIQUE(app_id, revision_number) constraint. MAX+1 is collision-free.
	newRevision, nextErr := h.db.GetNextRevisionNumber(ctx, appID)
	if nextErr != nil {
		msg := "failed to allocate revision number: " + nextErr.Error()
		h.db.UpdateAppStatus(ctx, appID, "failed", &msg)
		return
	}
	cpuReq := app.CPURequest
	cpuLim := app.CPULimit
	memReq := app.MemoryRequest
	memLim := app.MemoryLimit
	_, err = h.db.CreateRevision(ctx, db.CreateRevisionParams{
		AppID:          appID,
		RevisionNumber: newRevision,
		Image:          app.Image,
		Replicas:       app.Replicas,
		Port:           app.Port,
		EnvVars:        app.EnvVars,
		CPURequest:     &cpuReq,
		CPULimit:       &cpuLim,
		MemRequest:     &memReq,
		MemLimit:       &memLim,
		HealthPath:     app.HealthPath,
		HealthPort:     app.HealthPort,
		HealthDelay:    app.HealthInitialDelay,
		HealthPeriod:   app.HealthPeriod,
		// HPA config snapshot
		HPAEnabled:   app.HPAEnabled,
		MinReplicas:  app.MinReplicas,
		MaxReplicas:  app.MaxReplicas,
		CPUTarget:    app.CPUTarget,
		MemoryTarget: app.MemoryTarget,
		// Domain snapshot
		Domain: app.Domain,
		// Pre-deploy hook snapshot
		PreDeployCommand: app.PreDeployCommand,
	})
	if err != nil {
		msg := "failed to create revision: " + err.Error()
		h.db.UpdateAppStatus(ctx, appID, "failed", &msg)
		return
	}

	var envVars map[string]string
	json.Unmarshal(app.EnvVars, &envVars)

	// Sync secrets to K8s
	secretName, secretErr := h.syncSecretsToCluster(ctx, app, client)
	if secretErr != nil {
		msg := secretErr.Error()
		h.db.UpdateAppStatus(ctx, appID, "failed", &msg)
		return
	}

	// Run pre-deploy hook if configured
	if app.PreDeployCommand != nil && *app.PreDeployCommand != "" {
		h.db.UpdateAppStatus(ctx, appID, "running_predeploy", nil)

		result, err := client.RunPreDeployJob(ctx, k8s.PreDeployJobRequest{
			AppName:    app.Name,
			Namespace:  app.Namespace,
			Image:      app.Image,
			Command:    *app.PreDeployCommand,
			EnvVars:    envVars,
			SecretName: secretName,
		})
		if err != nil {
			msg := "failed to run pre-deploy hook: " + err.Error()
			h.db.UpdateAppStatus(ctx, appID, "failed", &msg)
			h.db.UpdateRevisionStatus(ctx, appID, newRevision, "failed", &msg)
			return
		}
		if !result.Success {
			msg := "pre-deploy hook failed: " + result.Error + "\nLogs:\n" + result.Logs
			h.db.UpdateAppStatus(ctx, appID, "failed", &msg)
			h.db.UpdateRevisionStatus(ctx, appID, newRevision, "failed", &msg)
			return
		}
	}

	err = client.DeployApp(buildDeployRequestFromApp(app, h.appBaseDomain, secretName, envVars))
	if err != nil {
		msg := err.Error()
		h.db.UpdateAppStatus(ctx, appID, "failed", &msg)
		// Mark revision as failed
		h.db.UpdateRevisionStatus(ctx, appID, newRevision, "failed", &msg)
		return
	}

	// Rollout observation. Kube accepted the spec; now watch the
	// Deployment's pods actually come up. A bounded ctx lets us detect
	// stuck rollouts (ImagePullBackOff, CrashLoopBackOff) rather than
	// reporting "running" purely because the apply succeeded.
	h.db.UpdateAppStatus(ctx, appID, "verifying", nil)
	deadline := client.DeploymentProgressDeadline(ctx, app.Name, app.Namespace) + 10*time.Second
	watchCtx, cancel := context.WithTimeout(ctx, deadline)
	watchErr := client.WatchRollout(watchCtx, app.Name, app.Namespace)
	cancel()
	if watchErr != nil {
		log.Printf("deploy: rollout verification failed app=%s revision=%d err=%v", appID, newRevision, watchErr)
		h.autoRollback(ctx, appID, app, client, newRevision, watchErr)
		return
	}

	// Update app's current revision and status
	h.db.UpdateAppRevision(ctx, appID, newRevision)
	h.db.UpdateAppStatus(ctx, appID, "running", nil)
	// Mark revision as successful
	h.db.UpdateRevisionStatus(ctx, appID, newRevision, "success", nil)

	h.syncCustomDomainIngress(ctx, appID, app, client, app.Port)

	// Clean up old revisions (keep last 10). This is also the invariant that
	// autoRollback relies on — prior revision N-1 must exist when deploy N
	// fails. Don't lower `keep` below 2 without updating autoRollback's guard.
	h.db.DeleteOldRevisions(ctx, appID, 10)
}

// syncSecretsToCluster decrypts the app's secrets from DB and writes them to
// the cluster Secret object. Returns the secret name (empty if no secrets).
// Called from both the forward deploy path and autoRollback so the cluster
// Secret always reflects current DB state — critical during rollback because
// secrets aren't versioned in revisions; a rotation during the watch window
// needs to land in the cluster before the rollback pods start.
func (h *Handler) syncSecretsToCluster(ctx context.Context, app *db.App, client *k8s.Client) (string, error) {
	secrets, err := h.db.GetSecretsByAppID(ctx, app.ID)
	if err != nil || len(secrets) == 0 {
		return "", nil
	}
	secretData := make(map[string]string)
	for _, s := range secrets {
		decrypted, err := auth.Decrypt(s.ValueEncrypted, h.encryptKey)
		if err != nil {
			return "", fmt.Errorf("failed to decrypt secret: %w", err)
		}
		secretData[s.Key] = string(decrypted)
	}
	secretName := app.Name + "-secrets"
	if err := client.CreateOrUpdateSecret(secretName, app.Namespace, secretData); err != nil {
		return "", fmt.Errorf("failed to create k8s secret: %w", err)
	}
	return secretName, nil
}

// syncCustomDomainIngress reconciles the Ingress for an app's custom domain
// (if one is configured) and updates the domain_status row on success.
// Idempotent; called from both the forward deploy path and autoRollback.
// Port is passed explicitly because the rolled-back revision may have a
// different Port than the live app.Port (app fields aren't re-copied from
// the revision on rollback).
func (h *Handler) syncCustomDomainIngress(ctx context.Context, appID string, app *db.App, client *k8s.Client, port *int) {
	if app.Domain == nil || *app.Domain == "" {
		return
	}
	p := 80
	if port != nil {
		p = *port
	}
	if err := client.CreateOrUpdateIngress(app.Name, app.Namespace, *app.Domain, p); err != nil {
		msg := "warning: failed to sync ingress: " + err.Error()
		h.db.UpdateAppStatus(ctx, appID, "running", &msg)
		return
	}
	activeStatus := "active"
	h.db.UpdateAppDomain(ctx, db.UpdateAppDomainParams{
		ID:           appID,
		Domain:       app.Domain,
		DomainStatus: &activeStatus,
	})
}

// autoRollback is invoked when WatchRollout reports the new revision never
// became healthy. It redeploys the app's last successful revision
// (app.CurrentRevision) inline — same DeployApp path, no watch on the
// rollback itself (v1). Called under the per-app deploy mutex already held
// by deployApp, so the total worst-case time the mutex is held is ~2 ×
// progressDeadline plus pre-deploy hook time — roughly 20–30 minutes upper
// bound. Concurrent deploys on the same app queue behind it (intended).
//
// Rollback-target selection:
//   - app.CurrentRevision == 0 → no prior successful deploy; app stays
//     "failed". (Happens on a first-ever-deploy failure.)
//   - otherwise → rollback to app.CurrentRevision. On success, revision N
//     (the failed forward) is marked "rolled_back" and app.CurrentRevision
//     is left unchanged (it already points at the successful target).
//     Critically we do NOT target newRevision-1 — with MAX+1 allocation,
//     newRevision-1 can be a previously-rolled-back revision from an earlier
//     incident, which would be the wrong thing to redeploy.
//
// Known v1 gaps (tracked in ROADMAP Phase 2.11):
//   - Server restart between UpdateAppStatus("verifying") and the terminal
//     status leaves the row orphaned in "verifying" — no boot-time reconciler
//     sweeps it.
//   - Secrets are re-resolved from the live rows rather than snapshotted in
//     the revision, so a rollback cannot recover a deploy broken by a deleted
//     secret key that the prior revision's env references.
func (h *Handler) autoRollback(ctx context.Context, appID string, app *db.App, client *k8s.Client, newRevision int, deployErr error) {
	origMsg := "rollout did not become ready: " + deployErr.Error()

	if app.CurrentRevision <= 0 {
		log.Printf("rollback: first-deploy-cannot-rollback app=%s revision=%d", appID, newRevision)
		h.db.UpdateAppStatus(ctx, appID, "failed", &origMsg)
		h.db.UpdateRevisionStatus(ctx, appID, newRevision, "failed", &origMsg)
		return
	}

	prior, err := h.db.GetRevision(ctx, appID, app.CurrentRevision)
	if err != nil {
		log.Printf("rollback: prior-revision-missing app=%s target_revision=%d err=%v", appID, app.CurrentRevision, err)
		msg := origMsg + " | rollback aborted: prior revision " + strconv.Itoa(app.CurrentRevision) + " not found: " + err.Error()
		h.db.UpdateAppStatus(ctx, appID, "failed", &msg)
		h.db.UpdateRevisionStatus(ctx, appID, newRevision, "failed", &msg)
		return
	}

	log.Printf("rollback: starting app=%s from=%d to=%d reason=%v", appID, newRevision, prior.RevisionNumber, deployErr)
	h.db.UpdateAppStatus(ctx, appID, "rolling_back", &origMsg)

	// Env vars come from the revision snapshot. Secret values aren't
	// versioned — re-sync the cluster Secret from current DB state (matches
	// the forward path and handles the case where secrets rotated during the
	// watch window). A rollback can recover from an env-var regression but
	// not from a deleted secret key that revision N-1's env references.
	var envVars map[string]string
	if len(prior.EnvVars) > 0 {
		_ = json.Unmarshal(prior.EnvVars, &envVars)
	}
	secretName, err := h.syncSecretsToCluster(ctx, app, client)
	if err != nil {
		log.Printf("rollback: secret sync failed app=%s err=%v original_err=%v", appID, err, deployErr)
		msg := origMsg + " | rollback secret sync failed: " + err.Error()
		h.db.UpdateAppStatus(ctx, appID, "failed", &msg)
		h.db.UpdateRevisionStatus(ctx, appID, newRevision, "failed", &msg)
		return
	}

	if err := client.DeployApp(buildDeployRequestFromRevision(app, prior, h.appBaseDomain, secretName, envVars)); err != nil {
		log.Printf("rollback: failed app=%s target_revision=%d err=%v original_err=%v", appID, prior.RevisionNumber, err, deployErr)
		msg := origMsg + " | rollback to revision " + strconv.Itoa(prior.RevisionNumber) + " also failed: " + err.Error()
		h.db.UpdateAppStatus(ctx, appID, "failed", &msg)
		h.db.UpdateRevisionStatus(ctx, appID, newRevision, "failed", &msg)
		return
	}

	log.Printf("rollback: succeeded app=%s target_revision=%d", appID, prior.RevisionNumber)
	// app.CurrentRevision already equals prior.RevisionNumber (that's how we
	// selected the rollback target above), so no UpdateAppRevision call is
	// needed. Subsequent deploys will allocate their revision number via
	// GetNextRevisionNumber (MAX+1), which is collision-free regardless.
	h.db.UpdateAppStatus(ctx, appID, "running", nil)
	h.db.UpdateRevisionStatus(ctx, appID, newRevision, "rolled_back", &origMsg)

	// Mirror the happy path: re-reconcile the custom-domain Ingress so it
	// matches revision N-1's Port if that changed between N-1 and N.
	h.syncCustomDomainIngress(ctx, appID, app, client, prior.Port)
}

func (h *Handler) DeleteApp(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appID")

	app, err := h.db.GetApp(r.Context(), appID)
	if err != nil {
		httpError(w, "app not found", http.StatusNotFound)
		return
	}

	cluster, err := h.db.GetCluster(r.Context(), app.ClusterID)
	if err != nil {
		httpError(w, "cluster not found", http.StatusNotFound)
		return
	}

	// Decrypt kubeconfig and delete from K8s
	kubeconfig, err := auth.Decrypt(cluster.KubeconfigEncrypted, h.encryptKey)
	if err == nil {
		if client, err := k8s.NewClient(kubeconfig); err == nil {
			client.DeleteApp(app.Name, app.Namespace)
			// Also delete Ingress if domain was configured
			if app.Domain != nil && *app.Domain != "" {
				client.DeleteIngress(app.Name, app.Namespace)
			}
		}
	}

	if err := h.db.DeleteApp(r.Context(), appID); err != nil {
		httpError(w, "failed to delete app", http.StatusInternalServerError)
		return
	}
	// Drop the per-app deploy mutex so deployLocks doesn't accumulate
	// entries for deleted apps over the process lifetime.
	h.deployLocks.Delete(appID)
	w.WriteHeader(http.StatusNoContent)
}

// Secrets

func (h *Handler) ListSecrets(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appID")

	// Verify app exists
	if _, err := h.db.GetApp(r.Context(), appID); err != nil {
		httpError(w, "app not found", http.StatusNotFound)
		return
	}

	secrets, err := h.db.ListSecrets(r.Context(), appID)
	if err != nil {
		httpError(w, "failed to list secrets", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(secrets)
}

func (h *Handler) SetSecret(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appID")

	// Verify app exists
	if _, err := h.db.GetApp(r.Context(), appID); err != nil {
		httpError(w, "app not found", http.StatusNotFound)
		return
	}

	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Key == "" || req.Value == "" {
		httpError(w, "key and value are required", http.StatusBadRequest)
		return
	}

	// Encrypt the value
	encrypted, err := auth.Encrypt([]byte(req.Value), h.encryptKey)
	if err != nil {
		httpError(w, "failed to encrypt secret", http.StatusInternalServerError)
		return
	}

	secret, err := h.db.SetSecret(r.Context(), appID, req.Key, encrypted)
	if err != nil {
		httpError(w, "failed to set secret", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(secret)
}

func (h *Handler) DeleteSecret(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appID")
	key := chi.URLParam(r, "key")

	// Verify app exists
	if _, err := h.db.GetApp(r.Context(), appID); err != nil {
		httpError(w, "app not found", http.StatusNotFound)
		return
	}

	if err := h.db.DeleteSecret(r.Context(), appID, key); err != nil {
		httpError(w, "failed to delete secret", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Revisions

func (h *Handler) ListRevisions(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appID")

	// Verify app exists
	if _, err := h.db.GetApp(r.Context(), appID); err != nil {
		httpError(w, "app not found", http.StatusNotFound)
		return
	}

	// Get limit from query params, default 10
	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	revisions, err := h.db.ListRevisions(r.Context(), appID, limit)
	if err != nil {
		httpError(w, "failed to list revisions", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(revisions)
}

func (h *Handler) GetRevision(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appID")
	revStr := chi.URLParam(r, "revision")

	revisionNumber, err := strconv.Atoi(revStr)
	if err != nil {
		httpError(w, "invalid revision number", http.StatusBadRequest)
		return
	}

	revision, err := h.db.GetRevision(r.Context(), appID, revisionNumber)
	if err != nil {
		httpError(w, "revision not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(revision)
}

func (h *Handler) RollbackApp(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appID")

	app, err := h.db.GetApp(r.Context(), appID)
	if err != nil {
		httpError(w, "app not found", http.StatusNotFound)
		return
	}

	// Parse optional revision number from request body
	var req struct {
		Revision *int `json:"revision"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	var targetRevision *db.AppRevision

	if req.Revision != nil {
		// Rollback to specific revision
		targetRevision, err = h.db.GetRevision(r.Context(), appID, *req.Revision)
		if err != nil {
			httpError(w, "revision not found", http.StatusNotFound)
			return
		}
	} else {
		// Rollback to the last *successful* revision before CurrentRevision.
		// CurrentRevision-1 is unsafe: MAX+1 allocation can leave gaps where
		// the intervening numbers are rolled_back or failed revisions. We
		// must skip those to find the previous known-good deploy.
		if app.CurrentRevision <= 1 {
			httpError(w, "no previous revision to rollback to", http.StatusBadRequest)
			return
		}
		targetRevision, err = h.db.GetLastSuccessfulRevisionBefore(r.Context(), appID, app.CurrentRevision)
		if err != nil {
			httpError(w, "no previous successful revision to rollback to", http.StatusNotFound)
			return
		}
	}

	// Apply revision configuration to app
	cpuReq := ""
	if targetRevision.CPURequest != nil {
		cpuReq = *targetRevision.CPURequest
	}
	cpuLim := ""
	if targetRevision.CPULimit != nil {
		cpuLim = *targetRevision.CPULimit
	}
	memReq := ""
	if targetRevision.MemoryRequest != nil {
		memReq = *targetRevision.MemoryRequest
	}
	memLim := ""
	if targetRevision.MemoryLimit != nil {
		memLim = *targetRevision.MemoryLimit
	}

	_, err = h.db.UpdateApp(r.Context(), db.UpdateAppParams{
		ID:           appID,
		Image:        targetRevision.Image,
		Replicas:     targetRevision.Replicas,
		EnvVars:      targetRevision.EnvVars,
		CPURequest:   cpuReq,
		CPULimit:     cpuLim,
		MemRequest:   memReq,
		MemLimit:     memLim,
		HealthPath:   targetRevision.HealthPath,
		HealthPort:   targetRevision.HealthPort,
		HealthDelay:  targetRevision.HealthDelay,
		HealthPeriod: targetRevision.HealthPeriod,
	})
	if err != nil {
		httpError(w, "failed to update app configuration", http.StatusInternalServerError)
		return
	}

	// CurrentRevision must point at the target BEFORE deployApp runs. If we
	// leave it at the broken revision, a subsequent watch-timeout would
	// invoke autoRollback, which reads app.CurrentRevision as the rollback
	// target — and redeploys the very revision the user was escaping from.
	if err := h.db.UpdateAppRevision(r.Context(), appID, targetRevision.RevisionNumber); err != nil {
		httpError(w, "failed to update current revision", http.StatusInternalServerError)
		return
	}

	// Get cluster for deployment
	cluster, err := h.db.GetCluster(r.Context(), app.ClusterID)
	if err != nil {
		httpError(w, "cluster not found", http.StatusNotFound)
		return
	}

	// Decrypt kubeconfig
	kubeconfig, err := auth.Decrypt(cluster.KubeconfigEncrypted, h.encryptKey)
	if err != nil {
		httpError(w, "failed to decrypt kubeconfig", http.StatusInternalServerError)
		return
	}

	// Update status to deploying
	h.db.UpdateAppStatus(r.Context(), appID, "rolling_back", nil)

	// Belt-and-suspenders for C1: even if both this re-fetch AND deployApp's
	// in-goroutine re-fetch fail (narrow DB-outage window), the snapshot
	// we hand to deployApp must carry the new CurrentRevision. Otherwise
	// autoRollback would target the broken revision the user is escaping.
	app.CurrentRevision = targetRevision.RevisionNumber

	// Re-fetch app with the UpdateApp changes from above applied. If the
	// re-fetch fails we fall back to the pre-update `app` (with the
	// in-memory CurrentRevision fix above): stale by one field-copy but
	// still a valid pointer, and deployApp's own in-goroutine GetApp will
	// try again. Without this guard, a failing GetApp would pass a nil
	// pointer to deployApp.
	updatedApp, err := h.db.GetApp(r.Context(), appID)
	if err != nil {
		log.Printf("rollback: app re-fetch failed — falling back to pre-update snapshot app=%s err=%v", appID, err)
		updatedApp = app
	}
	go h.deployApp(appID, updatedApp, kubeconfig)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":            "rolling_back",
		"target_revision":   targetRevision.RevisionNumber,
		"target_image":      targetRevision.Image,
	})
}

// Deployment History

func (h *Handler) GetDeploymentHistory(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appID")

	// Verify app exists
	if _, err := h.db.GetApp(r.Context(), appID); err != nil {
		httpError(w, "app not found", http.StatusNotFound)
		return
	}

	// Get limit from query params, default 20
	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	history, err := h.db.GetDeploymentHistory(r.Context(), appID, limit)
	if err != nil {
		httpError(w, "failed to get deployment history", http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(history)
}

// Autoscaling (HPA)

func (h *Handler) GetAutoscaling(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appID")

	app, err := h.db.GetApp(r.Context(), appID)
	if err != nil {
		httpError(w, "app not found", http.StatusNotFound)
		return
	}

	cluster, err := h.db.GetCluster(r.Context(), app.ClusterID)
	if err != nil {
		httpError(w, "cluster not found", http.StatusNotFound)
		return
	}

	kubeconfig, err := auth.Decrypt(cluster.KubeconfigEncrypted, h.encryptKey)
	if err != nil {
		httpError(w, "failed to decrypt kubeconfig", http.StatusInternalServerError)
		return
	}

	client, err := k8s.NewClient(kubeconfig)
	if err != nil {
		httpError(w, "failed to connect to cluster", http.StatusInternalServerError)
		return
	}

	status, err := client.GetHPA(app.Name, app.Namespace)
	if err != nil {
		httpError(w, "failed to get autoscaling status: "+err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(status)
}

func (h *Handler) SetAutoscaling(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appID")

	app, err := h.db.GetApp(r.Context(), appID)
	if err != nil {
		httpError(w, "app not found", http.StatusNotFound)
		return
	}

	var req struct {
		Enabled          bool   `json:"enabled"`
		MinReplicas      *int32 `json:"min_replicas"`
		MaxReplicas      *int32 `json:"max_replicas"`
		TargetCPUPercent *int32 `json:"target_cpu_percent"`
		TargetMemPercent *int32 `json:"target_memory_percent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	cluster, err := h.db.GetCluster(r.Context(), app.ClusterID)
	if err != nil {
		httpError(w, "cluster not found", http.StatusNotFound)
		return
	}

	kubeconfig, err := auth.Decrypt(cluster.KubeconfigEncrypted, h.encryptKey)
	if err != nil {
		httpError(w, "failed to decrypt kubeconfig", http.StatusInternalServerError)
		return
	}

	client, err := k8s.NewClient(kubeconfig)
	if err != nil {
		httpError(w, "failed to connect to cluster", http.StatusInternalServerError)
		return
	}

	// Set defaults
	minReplicas := int32(1)
	if req.MinReplicas != nil {
		minReplicas = *req.MinReplicas
	}
	maxReplicas := int32(10)
	if req.MaxReplicas != nil {
		maxReplicas = *req.MaxReplicas
	}

	// Validate
	if minReplicas < 1 {
		httpError(w, "min_replicas must be at least 1", http.StatusBadRequest)
		return
	}
	if maxReplicas < minReplicas {
		httpError(w, "max_replicas must be >= min_replicas", http.StatusBadRequest)
		return
	}

	config := k8s.HPAConfig{
		Enabled:          req.Enabled,
		MinReplicas:      minReplicas,
		MaxReplicas:      maxReplicas,
		TargetCPUPercent: req.TargetCPUPercent,
		TargetMemPercent: req.TargetMemPercent,
	}

	// DB is the source of truth for reconcileHPA (which runs on every deploy),
	// so persist first. If the k8s write fails afterwards the next deploy will
	// converge. The inverse ordering silently undoes user intent: k8s writes
	// an HPA, DB write fails, next deploy reads stale DB row (Enabled=false)
	// and deletes the HPA the user just created.
	minRep := int(minReplicas)
	maxRep := int(maxReplicas)
	var cpuTgt, memTgt *int
	if req.TargetCPUPercent != nil {
		v := int(*req.TargetCPUPercent)
		cpuTgt = &v
	}
	if req.TargetMemPercent != nil {
		v := int(*req.TargetMemPercent)
		memTgt = &v
	}
	if _, err := h.db.UpdateAppHPA(r.Context(), db.UpdateAppHPAParams{
		ID:           appID,
		HPAEnabled:   req.Enabled,
		MinReplicas:  &minRep,
		MaxReplicas:  &maxRep,
		CPUTarget:    cpuTgt,
		MemoryTarget: memTgt,
	}); err != nil {
		httpError(w, "failed to save autoscaling config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := client.CreateOrUpdateHPA(app.Name, app.Namespace, config); err != nil {
		httpError(w, "failed to update autoscaling: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Fetch and return updated status
	status, err := client.GetHPA(app.Name, app.Namespace)
	if err != nil {
		httpError(w, "failed to get autoscaling status: "+err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(status)
}

// Custom Domains

func (h *Handler) GetDomain(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appID")

	app, err := h.db.GetApp(r.Context(), appID)
	if err != nil {
		httpError(w, "app not found", http.StatusNotFound)
		return
	}

	cluster, err := h.db.GetCluster(r.Context(), app.ClusterID)
	if err != nil {
		httpError(w, "cluster not found", http.StatusNotFound)
		return
	}

	kubeconfig, err := auth.Decrypt(cluster.KubeconfigEncrypted, h.encryptKey)
	if err != nil {
		httpError(w, "failed to decrypt kubeconfig", http.StatusInternalServerError)
		return
	}

	client, err := k8s.NewClient(kubeconfig)
	if err != nil {
		httpError(w, "failed to connect to cluster", http.StatusInternalServerError)
		return
	}

	// Get Ingress status from K8s
	ingressStatus, _ := client.GetIngress(app.Name, app.Namespace)

	response := map[string]interface{}{
		"domain":        app.Domain,
		"domain_status": app.DomainStatus,
	}

	if ingressStatus != nil {
		response["ingress"] = ingressStatus
	}

	json.NewEncoder(w).Encode(response)
}

func (h *Handler) SetDomain(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appID")

	app, err := h.db.GetApp(r.Context(), appID)
	if err != nil {
		httpError(w, "app not found", http.StatusNotFound)
		return
	}

	var req struct {
		Domain *string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Validate domain format if provided
	if req.Domain != nil && *req.Domain != "" {
		// Check if domain is already in use by another app
		existing, err := h.db.GetAppByDomain(r.Context(), *req.Domain)
		if err == nil && existing.ID != appID {
			httpError(w, "domain already in use by another app", http.StatusConflict)
			return
		}
	}

	cluster, err := h.db.GetCluster(r.Context(), app.ClusterID)
	if err != nil {
		httpError(w, "cluster not found", http.StatusNotFound)
		return
	}

	kubeconfig, err := auth.Decrypt(cluster.KubeconfigEncrypted, h.encryptKey)
	if err != nil {
		httpError(w, "failed to decrypt kubeconfig", http.StatusInternalServerError)
		return
	}

	client, err := k8s.NewClient(kubeconfig)
	if err != nil {
		httpError(w, "failed to connect to cluster", http.StatusInternalServerError)
		return
	}

	var domainStatus string

	if req.Domain != nil && *req.Domain != "" {
		// Create or update Ingress
		port := 80
		if app.Port != nil {
			port = *app.Port
		}

		if err := client.CreateOrUpdateIngress(app.Name, app.Namespace, *req.Domain, port); err != nil {
			httpError(w, "failed to create ingress: "+err.Error(), http.StatusInternalServerError)
			return
		}
		domainStatus = "provisioning"
	} else {
		// Delete Ingress if domain is being removed
		if app.Domain != nil && *app.Domain != "" {
			if err := client.DeleteIngress(app.Name, app.Namespace); err != nil {
				// Log but don't fail - ingress might not exist
			}
		}
		domainStatus = ""
	}

	// Update database
	statusPtr := &domainStatus
	if domainStatus == "" {
		statusPtr = nil
	}
	updatedApp, err := h.db.UpdateAppDomain(r.Context(), db.UpdateAppDomainParams{
		ID:           appID,
		Domain:       req.Domain,
		DomainStatus: statusPtr,
	})
	if err != nil {
		httpError(w, "failed to update domain", http.StatusInternalServerError)
		return
	}

	// Get updated Ingress status
	var ingressStatus *k8s.IngressStatus
	if req.Domain != nil && *req.Domain != "" {
		ingressStatus, _ = client.GetIngress(app.Name, app.Namespace)
	}

	response := map[string]interface{}{
		"domain":        updatedApp.Domain,
		"domain_status": updatedApp.DomainStatus,
	}
	if ingressStatus != nil {
		response["ingress"] = ingressStatus
	}

	json.NewEncoder(w).Encode(response)
}

// Pre-deploy Hooks

func (h *Handler) GetPreDeployHook(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appID")

	app, err := h.db.GetApp(r.Context(), appID)
	if err != nil {
		httpError(w, "app not found", http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"pre_deploy_command": app.PreDeployCommand,
	})
}

func (h *Handler) SetPreDeployHook(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appID")

	// Verify app exists
	if _, err := h.db.GetApp(r.Context(), appID); err != nil {
		httpError(w, "app not found", http.StatusNotFound)
		return
	}

	var req struct {
		Command *string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Update the pre-deploy command
	app, err := h.db.UpdateAppPreDeployCommand(r.Context(), appID, req.Command)
	if err != nil {
		httpError(w, "failed to update pre-deploy hook", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"pre_deploy_command": app.PreDeployCommand,
	})
}

// ============================================================================
// User & Token Management (SSO)
// ============================================================================

// GetMe returns the current authenticated user
func (h *Handler) GetMe(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpError(w, "not authenticated", http.StatusUnauthorized)
		return
	}

	json.NewEncoder(w).Encode(user)
}

// ListMyTokens returns the current user's API tokens
func (h *Handler) ListMyTokens(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpError(w, "not authenticated", http.StatusUnauthorized)
		return
	}

	tokens, err := h.db.ListUserTokens(r.Context(), user.ID)
	if err != nil {
		httpError(w, "failed to list tokens", http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(tokens)
}

// CreateMyToken creates a new API token for the current user
func (h *Handler) CreateMyToken(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpError(w, "not authenticated", http.StatusUnauthorized)
		return
	}

	var req struct {
		Name      string `json:"name"`
		ExpiresIn *int   `json:"expires_in"` // days, optional
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		httpError(w, "name is required", http.StatusBadRequest)
		return
	}

	// Generate raw token (show to user once)
	rawToken, err := generateSecureToken()
	if err != nil {
		httpError(w, "failed to generate token", http.StatusInternalServerError)
		return
	}

	// Hash for storage
	tokenHash := hashToken(rawToken)

	// Calculate expiration
	var expiresAt *string
	if req.ExpiresIn != nil && *req.ExpiresIn > 0 {
		t := strconv.FormatInt(int64(*req.ExpiresIn), 10)
		expiresAt = &t
	}

	var expTime *time.Time
	if expiresAt != nil {
		days, _ := strconv.Atoi(*expiresAt)
		t := time.Now().Add(time.Duration(days) * 24 * time.Hour)
		expTime = &t
	}

	token, err := h.db.CreateUserToken(r.Context(), user.ID, req.Name, tokenHash, expTime)
	if err != nil {
		httpError(w, "failed to create token", http.StatusInternalServerError)
		return
	}

	// Return token with raw value (only shown once)
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":          token.ID,
		"name":        token.Name,
		"token":       rawToken, // Only shown once
		"created_at":  token.CreatedAt,
		"expires_at":  token.ExpiresAt,
	})
}

// DeleteMyToken revokes a user's API token
func (h *Handler) DeleteMyToken(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		httpError(w, "not authenticated", http.StatusUnauthorized)
		return
	}

	tokenID := chi.URLParam(r, "tokenID")
	if tokenID == "" {
		httpError(w, "token ID is required", http.StatusBadRequest)
		return
	}

	if err := h.db.DeleteUserToken(r.Context(), tokenID, user.ID); err != nil {
		httpError(w, "failed to delete token", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func httpError(w http.ResponseWriter, message string, code int) {
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// generateSecureToken generates a cryptographically secure token
func generateSecureToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// hashToken hashes a token using SHA-256
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func intPtrToInt32Ptr(p *int) *int32 {
	if p == nil {
		return nil
	}
	// Guard against surprise truncation if DB ever yields an out-of-range
	// value. All current callers (HPA replicas / targets) are bounded well
	// under int32; returning nil is a safer default than silently wrapping.
	if *p < 0 || *p > math.MaxInt32 {
		return nil
	}
	v := int32(*p)
	return &v
}

// GetClusterIngress returns information about the cluster's ingress controller
func (h *Handler) GetClusterIngress(w http.ResponseWriter, r *http.Request) {
	clusterID := chi.URLParam(r, "clusterID")

	cluster, err := h.db.GetCluster(r.Context(), clusterID)
	if err != nil {
		httpError(w, "cluster not found", http.StatusNotFound)
		return
	}

	kubeconfig, err := auth.Decrypt(cluster.KubeconfigEncrypted, h.encryptKey)
	if err != nil {
		httpError(w, "failed to decrypt kubeconfig", http.StatusInternalServerError)
		return
	}

	client, err := k8s.NewClient(kubeconfig)
	if err != nil {
		httpError(w, "failed to connect to cluster", http.StatusInternalServerError)
		return
	}

	info, err := client.GetIngressController()
	if err != nil {
		httpError(w, "failed to get ingress controller: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Include the base domain in the response
	response := struct {
		Available    bool   `json:"available"`
		LoadBalancer string `json:"load_balancer,omitempty"`
		Message      string `json:"message,omitempty"`
		BaseDomain   string `json:"base_domain,omitempty"`
	}{
		Available:    info.Available,
		LoadBalancer: info.LoadBalancer,
		Message:      info.Message,
		BaseDomain:   h.appBaseDomain,
	}

	json.NewEncoder(w).Encode(response)
}

// SwitchAppManagement switches an app between Porter and Shipit management
func (h *Handler) SwitchAppManagement(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appID")

	var req struct {
		ManagedBy string `json:"managed_by"` // "shipit" or "porter"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.ManagedBy != "shipit" && req.ManagedBy != "porter" {
		httpError(w, "managed_by must be 'shipit' or 'porter'", http.StatusBadRequest)
		return
	}

	// Get current app state
	app, err := h.db.GetApp(r.Context(), appID)
	if err != nil {
		httpError(w, "app not found", http.StatusNotFound)
		return
	}

	// Can only switch Porter-discovered apps (they have a porter_app_id)
	if app.PorterAppID == nil || *app.PorterAppID == "" {
		httpError(w, "this app was not discovered from Porter", http.StatusBadRequest)
		return
	}

	// Perform the switch
	if h.porterDiscovery != nil {
		if req.ManagedBy == "shipit" {
			if err := h.porterDiscovery.SwitchToShipit(r.Context(), appID); err != nil {
				httpError(w, "failed to switch to shipit: "+err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			if err := h.porterDiscovery.SwitchToPorter(r.Context(), appID); err != nil {
				httpError(w, "failed to switch to porter: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
	} else {
		// Fallback if porter discovery service not available
		if err := h.db.UpdateAppManagedBy(r.Context(), appID, req.ManagedBy); err != nil {
			httpError(w, "failed to update app: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Return updated app
	app, _ = h.db.GetApp(r.Context(), appID)
	json.NewEncoder(w).Encode(app)
}
