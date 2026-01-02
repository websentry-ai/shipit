package api

import (
	"bufio"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/vigneshsubbiah/shipit/internal/auth"
	"github.com/vigneshsubbiah/shipit/internal/k8s"
)

func (h *Handler) StreamLogs(w http.ResponseWriter, r *http.Request) {
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

	follow := r.URL.Query().Get("follow") == "true"
	tail := r.URL.Query().Get("tail")

	logStream, err := client.GetLogs(app.Name, app.Namespace, follow, tail)
	if err != nil {
		httpError(w, "failed to get logs: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer logStream.Close()

	// Set headers for streaming
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	flusher, ok := w.(http.Flusher)
	if !ok {
		httpError(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	scanner := bufio.NewScanner(logStream)
	for scanner.Scan() {
		w.Write(scanner.Bytes())
		w.Write([]byte("\n"))
		flusher.Flush()

		// Check if client disconnected
		select {
		case <-r.Context().Done():
			return
		default:
		}
	}
}

func (h *Handler) GetAppStatus(w http.ResponseWriter, r *http.Request) {
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

	status, err := client.GetDeploymentStatus(app.Name, app.Namespace)
	if err != nil {
		httpError(w, "failed to get status: "+err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(status)
}
