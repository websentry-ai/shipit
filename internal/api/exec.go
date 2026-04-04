package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/vigneshsubbiah/shipit/internal/auth"
	"github.com/vigneshsubbiah/shipit/internal/k8s"
)

type execRequest struct {
	Command     []string `json:"command"`
	Container   string   `json:"container"`
	ExistingPod bool     `json:"existing_pod"`
	CPU         string   `json:"cpu"`
	RAM         string   `json:"ram"`
	Timeout     int      `json:"timeout"`
}

type execResponse struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	PodName  string `json:"pod_name"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // non-browser clients (CLI, curl)
		}
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		return u.Host == r.Host
	},
}

// ExecCommand runs a command in a container (existing pod or ephemeral) and
// returns the captured stdout, stderr, and exit code.
func (h *Handler) ExecCommand(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appID")

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MB limit

	var req execRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.Command) == 0 {
		httpError(w, "command must be non-empty", http.StatusBadRequest)
		return
	}

	if req.ExistingPod && (req.CPU != "" || req.RAM != "") {
		httpError(w, "cpu and ram cannot be set when using existing_pod", http.StatusBadRequest)
		return
	}

	if req.Timeout <= 0 {
		req.Timeout = 300
	}
	if req.Timeout > 3600 {
		httpError(w, "timeout cannot exceed 3600 seconds", http.StatusBadRequest)
		return
	}
	timeout := time.Duration(req.Timeout) * time.Second
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	app, err := h.db.GetApp(ctx, appID)
	if err != nil {
		httpError(w, "app not found", http.StatusNotFound)
		return
	}

	cluster, err := h.db.GetCluster(ctx, app.ClusterID)
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

	var stdoutBuf, stderrBuf bytes.Buffer
	var podName, containerName string

	if req.ExistingPod {
		podName, containerName, err = client.FindRunningPod(ctx, app.Namespace, app.Name, req.Container)
		if err != nil {
			httpError(w, "failed to find running pod: "+err.Error(), http.StatusInternalServerError)
			return
		}

		exitCode, err := client.ExecInPod(ctx, app.Namespace, podName, containerName, req.Command, nil, &stdoutBuf, &stderrBuf, false)
		if err != nil {
			httpError(w, "exec failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(execResponse{
			Stdout:   stdoutBuf.String(),
			Stderr:   stderrBuf.String(),
			ExitCode: exitCode,
			PodName:  podName,
		})
		return
	}

	// Ephemeral pod flow
	var envVars map[string]string
	if len(app.EnvVars) > 0 {
		if err := json.Unmarshal(app.EnvVars, &envVars); err != nil {
			httpError(w, "failed to parse app env vars", http.StatusInternalServerError)
			return
		}
	}

	secretName, err := h.syncSecretsToK8s(ctx, appID, app.Name, app.Namespace, client)
	if err != nil {
		httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	podName, err = client.CreateEphemeralPod(ctx, k8s.EphemeralPodRequest{
		AppName:    app.Name,
		Namespace:  app.Namespace,
		Image:      app.Image,
		EnvVars:    envVars,
		SecretName: secretName,
		CPU:        req.CPU,
		RAM:        req.RAM,
		Command:    req.Command,
	})
	if err != nil {
		httpError(w, "failed to create ephemeral pod: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		client.DeletePod(cleanupCtx, app.Namespace, podName)
	}()

	exitCode, err := client.ExecInPod(ctx, app.Namespace, podName, "run", req.Command, nil, &stdoutBuf, &stderrBuf, false)
	if err != nil {
		httpError(w, "exec failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(execResponse{
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
		ExitCode: exitCode,
		PodName:  podName,
	})
}

// ExecInteractive upgrades to a WebSocket and provides an interactive exec
// session in either an existing or ephemeral pod.
func (h *Handler) ExecInteractive(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appID")
	existingPod := r.URL.Query().Get("existing_pod") == "true"
	container := r.URL.Query().Get("container")
	cpu := r.URL.Query().Get("cpu")
	ram := r.URL.Query().Get("ram")

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

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Hour)
	defer cancel()

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade already wrote the HTTP error response
		return
	}
	defer conn.Close()

	// Read first message to get the command
	var wsReq struct {
		Command []string `json:"command"`
	}
	if err := conn.ReadJSON(&wsReq); err != nil {
		conn.WriteJSON(map[string]string{"error": "failed to read command: " + err.Error()})
		return
	}
	if len(wsReq.Command) == 0 {
		conn.WriteJSON(map[string]string{"error": "command must be non-empty"})
		return
	}

	var podName, containerName string

	if existingPod {
		podName, containerName, err = client.FindRunningPod(ctx, app.Namespace, app.Name, container)
		if err != nil {
			conn.WriteJSON(map[string]string{"error": "failed to find running pod: " + err.Error()})
			return
		}
	} else {
		var envVars map[string]string
		if len(app.EnvVars) > 0 {
			if err := json.Unmarshal(app.EnvVars, &envVars); err != nil {
				conn.WriteJSON(map[string]string{"error": "failed to parse app env vars"})
				return
			}
		}

		secretName, err := h.syncSecretsToK8s(ctx, appID, app.Name, app.Namespace, client)
		if err != nil {
			conn.WriteJSON(map[string]string{"error": err.Error()})
			return
		}

		podName, err = client.CreateEphemeralPod(ctx, k8s.EphemeralPodRequest{
			AppName:    app.Name,
			Namespace:  app.Namespace,
			Image:      app.Image,
			EnvVars:    envVars,
			SecretName: secretName,
			CPU:        cpu,
			RAM:        ram,
			Command:    wsReq.Command,
		})
		if err != nil {
			conn.WriteJSON(map[string]string{"error": "failed to create ephemeral pod: " + err.Error()})
			return
		}
		defer func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cleanupCancel()
			client.DeletePod(cleanupCtx, app.Namespace, podName)
		}()
		containerName = "run"
	}

	stdinReader, stdinWriter := io.Pipe()
	defer stdinWriter.Close()

	// Read from WebSocket, write to stdin pipe
	go func() {
		defer stdinWriter.Close()
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if _, err := stdinWriter.Write(message); err != nil {
				return
			}
		}
	}()

	// Writer that sends output to WebSocket as text messages
	mu := &sync.Mutex{}
	wsWriter := &wsTextWriter{conn: conn, mu: mu}

	exitCode, execErr := client.ExecInPod(ctx, app.Namespace, podName, containerName, wsReq.Command, stdinReader, wsWriter, wsWriter, true)

	if execErr != nil {
		mu.Lock()
		conn.WriteJSON(map[string]string{"type": "error", "message": execErr.Error()})
		mu.Unlock()
	}

	mu.Lock()
	conn.WriteJSON(map[string]interface{}{"type": "exit", "exit_code": exitCode})
	mu.Unlock()
}

// CleanupExec removes all ephemeral pods for an app.
func (h *Handler) CleanupExec(w http.ResponseWriter, r *http.Request) {
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

	count, err := client.CleanupEphemeralPods(r.Context(), app.Namespace, app.Name)
	if err != nil {
		httpError(w, "cleanup failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]int{"deleted": count})
}

// syncSecretsToK8s loads secrets for an app and ensures the K8s Secret exists.
// Returns the secret name (empty if no secrets).
func (h *Handler) syncSecretsToK8s(ctx context.Context, appID, appName, namespace string, client *k8s.Client) (string, error) {
	secrets, err := h.db.GetSecretsByAppID(ctx, appID)
	if err != nil {
		return "", fmt.Errorf("failed to load secrets: %w", err)
	}
	if len(secrets) == 0 {
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

	secretName := appName + "-secrets"
	if err := client.CreateOrUpdateSecret(secretName, namespace, secretData); err != nil {
		return "", fmt.Errorf("failed to create k8s secret: %w", err)
	}
	return secretName, nil
}

// wsTextWriter implements io.Writer by sending each Write as a WebSocket TextMessage.
// A mutex is used to serialize writes since gorilla/websocket does not support
// concurrent writers.
type wsTextWriter struct {
	conn *websocket.Conn
	mu   *sync.Mutex
}

func (w *wsTextWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	// Sanitize: replace invalid UTF-8 sequences for text messages
	text := strings.ToValidUTF8(string(p), "")
	if err := w.conn.WriteMessage(websocket.TextMessage, []byte(text)); err != nil {
		return 0, err
	}
	return len(p), nil
}
