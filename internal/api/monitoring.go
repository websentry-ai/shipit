package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vigneshsubbiah/shipit/internal/auth"
	"github.com/vigneshsubbiah/shipit/internal/db"
	"github.com/vigneshsubbiah/shipit/internal/k8s"
)

// Monitoring add-on lifecycle + per-app metrics queries. Install is async
// (Helm release returns ~immediately but the underlying StatefulSet/PVC/
// Deployment converge over 2-5 min) — we kick it off in a goroutine and
// flip the cluster row's monitoring_status as workloads come up.

// MonitoringConfig is the body shape for POST /clusters/:id/monitoring.
type MonitoringConfig struct {
	// Optional override of the inferred Grafana hostname. Default:
	// "grafana.<appBaseDomain>" so the existing wildcard cert + ingress
	// pipeline (cert-manager + letsencrypt-prod) covers it.
	GrafanaHost string `json:"grafana_host,omitempty"`
}

// MonitoringResponse is what GET returns. Mirrors the cluster row plus a
// couple of derived fields for the UI.
type MonitoringResponse struct {
	Status        string  `json:"status"` // disabled|installing|ready|failed|uninstalling
	StatusMessage *string `json:"status_message,omitempty"`
	GrafanaHost   *string `json:"grafana_host,omitempty"`
	GrafanaURL    *string `json:"grafana_url,omitempty"` // https://<host>
	HelmRelease   *string `json:"helm_release,omitempty"`
	ChartVersion  *string `json:"chart_version,omitempty"`
	InstalledAt   *string `json:"installed_at,omitempty"`
}

// EnableMonitoring kicks off (or re-runs, idempotent) the install. Returns
// 202 immediately; the goroutine writes status updates as it progresses.
func (h *Handler) EnableMonitoring(w http.ResponseWriter, r *http.Request) {
	clusterID := chi.URLParam(r, "clusterID")
	cluster, err := h.db.GetCluster(r.Context(), clusterID)
	if err != nil {
		httpError(w, "cluster not found", http.StatusNotFound)
		return
	}

	if h.grafanaGoogleClientID == "" || h.grafanaGoogleClientSecret == "" || h.grafanaGoogleAllowedDomain == "" {
		httpError(w, "monitoring requires GRAFANA_GOOGLE_CLIENT_ID, GRAFANA_GOOGLE_CLIENT_SECRET, GRAFANA_GOOGLE_ALLOWED_DOMAIN env vars", http.StatusFailedDependency)
		return
	}

	var req MonitoringConfig
	// Body is optional — empty body is a "use defaults" install.
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	host := req.GrafanaHost
	if host == "" {
		if h.appBaseDomain == "" {
			httpError(w, "no grafana_host provided and APP_BASE_DOMAIN env unset; cannot infer grafana hostname", http.StatusBadRequest)
			return
		}
		host = "grafana." + h.appBaseDomain
	}

	kubeconfig, err := auth.Decrypt(cluster.KubeconfigEncrypted, h.encryptKey)
	if err != nil {
		httpError(w, "failed to decrypt kubeconfig", http.StatusInternalServerError)
		return
	}

	// Mark installing immediately so the UI flips even before the goroutine
	// schedules. Status flips to ready/failed when the goroutine finishes.
	if err := h.db.UpdateClusterMonitoring(r.Context(), db.UpdateClusterMonitoringParams{
		ClusterID:    clusterID,
		Status:       "installing",
		GrafanaHost:  &host,
		HelmRelease:  strPtr(k8s.MonitoringHelmReleaseName()),
		ChartVersion: strPtr(k8s.MonitoringChartVersion()),
	}); err != nil {
		httpError(w, "persist install intent: "+err.Error(), http.StatusInternalServerError)
		return
	}

	go h.installMonitoringAsync(clusterID, host, kubeconfig)

	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(buildMonitoringResponse(&db.Cluster{
		ID:                    clusterID,
		MonitoringStatus:      "installing",
		MonitoringGrafanaHost: &host,
	}))
}

func (h *Handler) installMonitoringAsync(clusterID, host string, kubeconfig []byte) {
	// Detached context so a slow install isn't tied to the request lifecycle.
	// 15-min cap mirrors Helm's internal Wait timeout in monitoring.go.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	client, err := k8s.NewClient(kubeconfig)
	if err != nil {
		h.markMonitoringFailed(clusterID, "build k8s client: "+err.Error())
		return
	}

	req := k8s.MonitoringInstallRequest{
		GrafanaHost:         host,
		GoogleClientID:      h.grafanaGoogleClientID,
		GoogleClientSecret:  h.grafanaGoogleClientSecret,
		GoogleAllowedDomain: h.grafanaGoogleAllowedDomain,
	}
	if err := client.InstallMonitoring(ctx, kubeconfig, req); err != nil {
		h.markMonitoringFailed(clusterID, "helm install: "+err.Error())
		return
	}

	// Reconcile loop: poll workload readiness until both Prometheus and
	// Grafana are ready, or the deadline fires. 5s tick is cheap (we're not
	// hammering the apiserver — just two GETs).
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		st, err := client.GetMonitoringStatus(ctx, kubeconfig)
		if err == nil && st.Phase == "ready" {
			_ = h.db.UpdateClusterMonitoring(ctx, db.UpdateClusterMonitoringParams{
				ClusterID:    clusterID,
				Status:       "ready",
				SetInstalled: true,
				StatusMessage: strPtr(""),
			})
			return
		}
		if err == nil && st.Phase == "failed" {
			h.markMonitoringFailed(clusterID, "helm release reported failed: "+st.Message)
			return
		}
		select {
		case <-ctx.Done():
			h.markMonitoringFailed(clusterID, "install timed out before workloads ready")
			return
		case <-time.After(5 * time.Second):
		}
	}
	h.markMonitoringFailed(clusterID, "install timed out before workloads ready")
}

func (h *Handler) markMonitoringFailed(clusterID, msg string) {
	log.Printf("[monitoring] cluster=%s failed: %s", clusterID, msg)
	_ = h.db.UpdateClusterMonitoring(context.Background(), db.UpdateClusterMonitoringParams{
		ClusterID:     clusterID,
		Status:        "failed",
		StatusMessage: &msg,
	})
}

// DisableMonitoring runs `helm uninstall`. PVCs (TSDB, Grafana SQLite) are
// preserved by Helm — the user can `kubectl delete ns monitoring` for a
// full wipe. Idempotent: missing release returns 200.
func (h *Handler) DisableMonitoring(w http.ResponseWriter, r *http.Request) {
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
		httpError(w, "build k8s client: "+err.Error(), http.StatusInternalServerError)
		return
	}

	_ = h.db.UpdateClusterMonitoring(r.Context(), db.UpdateClusterMonitoringParams{
		ClusterID: clusterID, Status: "uninstalling",
	})
	if err := client.UninstallMonitoring(r.Context(), kubeconfig); err != nil {
		h.markMonitoringFailed(clusterID, "helm uninstall: "+err.Error())
		httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = h.db.UpdateClusterMonitoring(r.Context(), db.UpdateClusterMonitoringParams{
		ClusterID:     clusterID,
		Status:        "disabled",
		StatusMessage: strPtr(""),
	})
	w.WriteHeader(http.StatusOK)
}

// GetMonitoring returns the current install state for the cluster. Cheap
// DB read; the async reconciler is what keeps this fresh.
func (h *Handler) GetMonitoring(w http.ResponseWriter, r *http.Request) {
	clusterID := chi.URLParam(r, "clusterID")
	cluster, err := h.db.GetCluster(r.Context(), clusterID)
	if err != nil {
		httpError(w, "cluster not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(buildMonitoringResponse(cluster))
}

func buildMonitoringResponse(c *db.Cluster) MonitoringResponse {
	resp := MonitoringResponse{
		Status:        c.MonitoringStatus,
		StatusMessage: c.MonitoringStatusMessage,
		GrafanaHost:   c.MonitoringGrafanaHost,
		HelmRelease:   c.MonitoringHelmRelease,
		ChartVersion:  c.MonitoringChartVersion,
	}
	if c.MonitoringGrafanaHost != nil && *c.MonitoringGrafanaHost != "" {
		u := "https://" + *c.MonitoringGrafanaHost
		resp.GrafanaURL = &u
	}
	if c.MonitoringInstalledAt != nil {
		s := c.MonitoringInstalledAt.Format(time.RFC3339)
		resp.InstalledAt = &s
	}
	if resp.Status == "" {
		resp.Status = "disabled"
	}
	return resp
}

// --- per-app metrics ---

// MetricsResponse is the JSON shape returned by the /apps/:id/metrics
// endpoint. One entry per pod in the most common case (CPU/memory).
// Time-series are returned as parallel arrays (timestamps, values) so the
// frontend can hand them straight to recharts without reshaping.
type MetricsResponse struct {
	Metric string         `json:"metric"`
	Step   int            `json:"step_seconds"`
	From   int64          `json:"from"`
	To     int64          `json:"to"`
	Series []MetricSeries `json:"series"`
}

type MetricSeries struct {
	Labels     map[string]string `json:"labels"`
	Timestamps []int64           `json:"timestamps"` // unix seconds
	Values     []float64         `json:"values"`
}

// supportedMetrics maps the public ?metric= parameter to a PromQL template
// that takes one parameter: a regex matching the pod-name prefix. Centralizing
// here keeps the API surface small and the queries reviewable.
//
// Pod selection uses pod=~"<name>-.*" — shipit names pods after the Deployment
// (which itself is the app name), so this matches reliably while not requiring
// the kube_pod_labels join. Trade-off: two apps named "auth" and "auth-api"
// would alias under "auth-.*"; we accept that for v0 since shipit names are
// unique-per-cluster anyway.
//
// All queries average across pods at the per-pod level (no top-level sum).
var supportedMetrics = map[string]string{
	// CPU cores used per pod, averaged over a 5m window.
	"cpu":          `sum by (pod) (rate(container_cpu_usage_seconds_total{namespace="%s",pod=~"%s-.*",container!="",container!="POD"}[5m]))`,
	// Working-set memory bytes per pod (cAdvisor "working_set" mirrors what
	// the OOM killer uses, more honest than RSS).
	"memory":       `sum by (pod) (container_memory_working_set_bytes{namespace="%s",pod=~"%s-.*",container!="",container!="POD"})`,
	// Network ingress bytes/sec per pod, summed across interfaces.
	"network_in":   `sum by (pod) (rate(container_network_receive_bytes_total{namespace="%s",pod=~"%s-.*"}[5m]))`,
	"network_out":  `sum by (pod) (rate(container_network_transmit_bytes_total{namespace="%s",pod=~"%s-.*"}[5m]))`,
	// Pod restart counter — kube-state-metrics. kube_pod_container_status_restarts_total
	// resets on pod recreation; rate() over the window catches restart bursts.
	"restarts":     `sum by (pod) (rate(kube_pod_container_status_restarts_total{namespace="%s",pod=~"%s-.*"}[5m]))`,
}

// GetAppMetrics translates the public metric name + range params into a
// PromQL query and returns the resulting time-series. step is auto-chosen
// from the range so we get ~120 points per chart by default (max 1000 to
// guard the apiserver proxy and Prometheus).
func (h *Handler) GetAppMetrics(w http.ResponseWriter, r *http.Request) {
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
	if cluster.MonitoringStatus != "ready" {
		httpError(w, "monitoring not ready on this cluster (status="+cluster.MonitoringStatus+")", http.StatusFailedDependency)
		return
	}

	metric := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("metric")))
	tmpl, ok := supportedMetrics[metric]
	if !ok {
		httpError(w, fmt.Sprintf("unsupported metric %q (try cpu|memory|network_in|network_out|restarts)", metric), http.StatusBadRequest)
		return
	}

	from, to, err := parseTimeRange(r.URL.Query())
	if err != nil {
		httpError(w, err.Error(), http.StatusBadRequest)
		return
	}
	step := pickStep(from, to, r.URL.Query().Get("step"))

	kubeconfig, err := auth.Decrypt(cluster.KubeconfigEncrypted, h.encryptKey)
	if err != nil {
		httpError(w, "failed to decrypt kubeconfig", http.StatusInternalServerError)
		return
	}
	client, err := k8s.NewClient(kubeconfig)
	if err != nil {
		httpError(w, "build k8s client: "+err.Error(), http.StatusInternalServerError)
		return
	}

	query := fmt.Sprintf(tmpl, app.Namespace, app.Name)
	matrix, err := client.QueryPrometheusRange(r.Context(), query, from, to, step)
	if err != nil {
		httpError(w, "prometheus query: "+err.Error(), http.StatusBadGateway)
		return
	}

	resp := MetricsResponse{
		Metric: metric,
		Step:   int(step.Seconds()),
		From:   from.Unix(),
		To:     to.Unix(),
		Series: make([]MetricSeries, 0, len(matrix.Series)),
	}
	for _, s := range matrix.Series {
		ms := MetricSeries{
			Labels:     s.Labels,
			Timestamps: make([]int64, 0, len(s.Points)),
			Values:     make([]float64, 0, len(s.Points)),
		}
		for _, p := range s.Points {
			ms.Timestamps = append(ms.Timestamps, int64(p.T))
			ms.Values = append(ms.Values, p.V)
		}
		resp.Series = append(resp.Series, ms)
	}
	json.NewEncoder(w).Encode(resp)
}

// parseTimeRange accepts ?from=&to= as either unix-seconds or RFC3339. If
// only one is set we anchor to "now" for the missing side. Defaults to the
// last 1h when both are missing.
func parseTimeRange(q map[string][]string) (time.Time, time.Time, error) {
	get := func(k string) string {
		if v, ok := q[k]; ok && len(v) > 0 {
			return v[0]
		}
		return ""
	}
	now := time.Now()
	parse := func(name, raw string, fallback time.Time) (time.Time, error) {
		if raw == "" {
			return fallback, nil
		}
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return time.Unix(n, 0), nil
		}
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return time.Time{}, fmt.Errorf("%s: must be unix-seconds or RFC3339", name)
		}
		return t, nil
	}
	from, err := parse("from", get("from"), now.Add(-time.Hour))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	to, err := parse("to", get("to"), now)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if !from.Before(to) {
		return time.Time{}, time.Time{}, fmt.Errorf("from must be before to")
	}
	if to.Sub(from) > 60*24*time.Hour {
		return time.Time{}, time.Time{}, fmt.Errorf("range too large; max 60 days")
	}
	return from, to, nil
}

// pickStep chooses a reasonable step that yields ~120 points across the
// requested range, clamped to [15s, 30m]. Honors an explicit ?step= override
// (in seconds) but caps it at 1000 points to protect Prometheus.
func pickStep(from, to time.Time, override string) time.Duration {
	if override != "" {
		if n, err := strconv.Atoi(override); err == nil && n > 0 {
			d := time.Duration(n) * time.Second
			if dpoints := to.Sub(from) / d; dpoints > 1000 {
				d = to.Sub(from) / 1000
			}
			return d
		}
	}
	target := to.Sub(from) / 120
	if target < 15*time.Second {
		return 15 * time.Second
	}
	if target > 30*time.Minute {
		return 30 * time.Minute
	}
	return target
}

func strPtr(s string) *string { return &s }
