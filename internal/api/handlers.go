package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/vigneshsubbiah/shipit/internal/auth"
	"github.com/vigneshsubbiah/shipit/internal/db"
	"github.com/vigneshsubbiah/shipit/internal/k8s"
)

type Handler struct {
	db         *db.DB
	encryptKey string
}

func NewHandler(database *db.DB, encryptKey string) *Handler {
	return &Handler{db: database, encryptKey: encryptKey}
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
	ctx := context.Background()
	client, err := k8s.NewClient(kubeconfig)
	if err != nil {
		msg := err.Error()
		h.db.UpdateAppStatus(ctx, appID, "failed", &msg)
		return
	}

	// Create a revision snapshot before deploying
	newRevision := app.CurrentRevision + 1
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
	})
	if err != nil {
		msg := "failed to create revision: " + err.Error()
		h.db.UpdateAppStatus(ctx, appID, "failed", &msg)
		return
	}

	var envVars map[string]string
	json.Unmarshal(app.EnvVars, &envVars)

	// Sync secrets to K8s
	secretName := ""
	secrets, err := h.db.GetSecretsByAppID(ctx, appID)
	if err == nil && len(secrets) > 0 {
		secretData := make(map[string]string)
		for _, s := range secrets {
			// Decrypt secret value
			decrypted, err := auth.Decrypt(s.ValueEncrypted, h.encryptKey)
			if err != nil {
				msg := "failed to decrypt secret: " + err.Error()
				h.db.UpdateAppStatus(ctx, appID, "failed", &msg)
				return
			}
			secretData[s.Key] = string(decrypted)
		}

		// Create/update K8s Secret
		secretName = app.Name + "-secrets"
		if err := client.CreateOrUpdateSecret(secretName, app.Namespace, secretData); err != nil {
			msg := "failed to create k8s secret: " + err.Error()
			h.db.UpdateAppStatus(ctx, appID, "failed", &msg)
			return
		}
	}

	err = client.DeployApp(k8s.DeployRequest{
		Name:       app.Name,
		Namespace:  app.Namespace,
		Image:      app.Image,
		Replicas:   int32(app.Replicas),
		Port:       app.Port,
		EnvVars:    envVars,
		SecretName: secretName,
		// Resource limits
		CPURequest:    app.CPURequest,
		CPULimit:      app.CPULimit,
		MemoryRequest: app.MemoryRequest,
		MemoryLimit:   app.MemoryLimit,
		// Health check
		HealthPath:         app.HealthPath,
		HealthPort:         app.HealthPort,
		HealthInitialDelay: app.HealthInitialDelay,
		HealthPeriod:       app.HealthPeriod,
	})
	if err != nil {
		msg := err.Error()
		h.db.UpdateAppStatus(ctx, appID, "failed", &msg)
		return
	}

	// Update app's current revision and status
	h.db.UpdateAppRevision(ctx, appID, newRevision)
	h.db.UpdateAppStatus(ctx, appID, "running", nil)

	// Sync Ingress if domain is configured
	if app.Domain != nil && *app.Domain != "" {
		port := 80
		if app.Port != nil {
			port = *app.Port
		}
		if err := client.CreateOrUpdateIngress(app.Name, app.Namespace, *app.Domain, port); err != nil {
			// Log but don't fail the deploy
			msg := "warning: failed to sync ingress: " + err.Error()
			h.db.UpdateAppStatus(ctx, appID, "running", &msg)
		} else {
			// Update domain status to active
			activeStatus := "active"
			h.db.UpdateAppDomain(ctx, db.UpdateAppDomainParams{
				ID:           appID,
				Domain:       app.Domain,
				DomainStatus: &activeStatus,
			})
		}
	}

	// Clean up old revisions (keep last 10)
	h.db.DeleteOldRevisions(ctx, appID, 10)
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
		// Rollback to previous revision (current - 1)
		if app.CurrentRevision <= 1 {
			httpError(w, "no previous revision to rollback to", http.StatusBadRequest)
			return
		}
		targetRevision, err = h.db.GetRevision(r.Context(), appID, app.CurrentRevision-1)
		if err != nil {
			httpError(w, "previous revision not found", http.StatusNotFound)
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

	// Re-fetch app with updated config and deploy
	updatedApp, _ := h.db.GetApp(r.Context(), appID)
	go h.deployApp(appID, updatedApp, kubeconfig)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":            "rolling_back",
		"target_revision":   targetRevision.RevisionNumber,
		"target_image":      targetRevision.Image,
	})
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

	if err := client.CreateOrUpdateHPA(app.Name, app.Namespace, config); err != nil {
		httpError(w, "failed to update autoscaling: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Persist HPA config to database
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
	_, err = h.db.UpdateAppHPA(r.Context(), db.UpdateAppHPAParams{
		ID:           appID,
		HPAEnabled:   req.Enabled,
		MinReplicas:  &minRep,
		MaxReplicas:  &maxRep,
		CPUTarget:    cpuTgt,
		MemoryTarget: memTgt,
	})
	if err != nil {
		httpError(w, "failed to save autoscaling config: "+err.Error(), http.StatusInternalServerError)
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

func httpError(w http.ResponseWriter, message string, code int) {
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
