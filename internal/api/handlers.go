package api

import (
	"context"
	"encoding/json"
	"net/http"

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

	envVarsJSON, _ := json.Marshal(req.EnvVars)

	app, err := h.db.CreateApp(r.Context(), clusterID, req.Name, req.Namespace, req.Image, req.Replicas, req.Port, envVarsJSON)
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

	var envVars map[string]string
	json.Unmarshal(app.EnvVars, &envVars)

	err = client.DeployApp(k8s.DeployRequest{
		Name:      app.Name,
		Namespace: app.Namespace,
		Image:     app.Image,
		Replicas:  int32(app.Replicas),
		Port:      app.Port,
		EnvVars:   envVars,
	})
	if err != nil {
		msg := err.Error()
		h.db.UpdateAppStatus(ctx, appID, "failed", &msg)
		return
	}

	h.db.UpdateAppStatus(ctx, appID, "running", nil)
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
		}
	}

	if err := h.db.DeleteApp(r.Context(), appID); err != nil {
		httpError(w, "failed to delete app", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func httpError(w http.ResponseWriter, message string, code int) {
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
