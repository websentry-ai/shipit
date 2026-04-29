package k8s

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"text/template"
	"time"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/repo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"sigs.k8s.io/yaml"
)

// Monitoring add-on: installs kube-prometheus-stack into the customer's
// cluster as a single Helm release, exposing Grafana over the cluster's
// existing cert-manager + ingress controller. Per-app graphs hit the
// in-cluster Prometheus through the kube-apiserver service proxy (no
// port-forwarding from shipit), so this file owns lifecycle only —
// metrics queries live in prometheus.go.

const (
	monitoringNamespace = "monitoring"
	helmReleaseName     = "kube-prometheus-stack"
	helmRepoURL         = "https://prometheus-community.github.io/helm-charts"
	helmChartName       = "kube-prometheus-stack"
	// Pinned chart version. Bump deliberately; CRD compatibility lives here.
	helmChartVersion = "58.7.2"
)

// MonitoringInstallRequest carries the per-cluster install knobs. Google
// OAuth credentials are shipit-global (one OAuth client across all clusters
// the shipit instance manages) and supplied by the caller from shipit env.
type MonitoringInstallRequest struct {
	GrafanaHost        string // e.g. "grafana.apps.shipit.unboundsec.dev"
	GoogleClientID     string
	GoogleClientSecret string
	GoogleAllowedDomain string // e.g. "unboundsecurity.ai"
	// Optional knobs with safe defaults.
	RetentionDays      int    // Prometheus TSDB retention; default 14
	PrometheusStorage  string // PVC size; default "5Gi"
	GrafanaStorage     string // PVC size; default "1Gi"
	IngressClass       string // default "nginx"
	ClusterIssuer      string // cert-manager ClusterIssuer; default "letsencrypt-prod"
}

// MonitoringStatus aggregates the readiness of the Prometheus + Grafana
// workloads. Returned by GetMonitoringStatus and consumed by the API layer
// to drive the cluster row's monitoring_status column.
type MonitoringStatus struct {
	Phase             string // installing|ready|failed|disabled
	Message           string
	HelmReleaseExists bool
	PrometheusReady   bool
	GrafanaReady      bool
	GrafanaHost       string
}

// InstallMonitoring runs `helm upgrade --install kube-prometheus-stack ...`
// idempotently against the cluster. Returns once the Helm release is
// recorded; the underlying StatefulSet/Deployment readiness is async and
// observed via GetMonitoringStatus.
func (c *Client) InstallMonitoring(ctx context.Context, kubeconfig []byte, req MonitoringInstallRequest) error {
	if err := req.validate(); err != nil {
		return err
	}
	req.applyDefaults()

	if err := c.ensureNamespace(ctx, monitoringNamespace); err != nil {
		return fmt.Errorf("ensure monitoring namespace: %w", err)
	}

	cfg, cleanup, err := newHelmConfig(kubeconfig, monitoringNamespace)
	if err != nil {
		return fmt.Errorf("init helm config: %w", err)
	}
	defer cleanup()

	chartPath, err := pullChart()
	if err != nil {
		return fmt.Errorf("pull chart: %w", err)
	}

	chrt, err := loader.Load(chartPath)
	if err != nil {
		return fmt.Errorf("load chart: %w", err)
	}

	values, err := renderValues(req)
	if err != nil {
		return fmt.Errorf("render values: %w", err)
	}

	// helm upgrade --install: succeeds whether or not a release exists.
	if exists, err := releaseExists(cfg); err != nil {
		return err
	} else if exists {
		up := action.NewUpgrade(cfg)
		up.Namespace = monitoringNamespace
		up.Wait = false
		up.Timeout = 10 * time.Minute
		_, err := up.RunWithContext(ctx, helmReleaseName, chrt, values)
		return err
	}

	in := action.NewInstall(cfg)
	in.Namespace = monitoringNamespace
	in.CreateNamespace = false // we already ensured it
	in.ReleaseName = helmReleaseName
	in.Wait = false
	in.Timeout = 10 * time.Minute
	_, err = in.RunWithContext(ctx, chrt, values)
	return err
}

// UninstallMonitoring removes the Helm release. PVCs (Prometheus TSDB,
// Grafana SQLite) are NOT auto-deleted — Helm doesn't own them. Caller
// can delete the namespace afterwards if they want a full wipe.
func (c *Client) UninstallMonitoring(ctx context.Context, kubeconfig []byte) error {
	cfg, cleanup, err := newHelmConfig(kubeconfig, monitoringNamespace)
	if err != nil {
		return err
	}
	defer cleanup()

	un := action.NewUninstall(cfg)
	un.Wait = false
	un.Timeout = 5 * time.Minute
	_, err = un.Run(helmReleaseName)
	if err != nil && strings.Contains(err.Error(), "release: not found") {
		return nil // idempotent
	}
	return err
}

// GetMonitoringStatus aggregates Helm release state + workload readiness.
// Used by the API status endpoint and the reconcile path that flips the DB
// row from installing→ready once everything is up.
func (c *Client) GetMonitoringStatus(ctx context.Context, kubeconfig []byte) (*MonitoringStatus, error) {
	cfg, cleanup, err := newHelmConfig(kubeconfig, monitoringNamespace)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	st := &MonitoringStatus{Phase: "disabled"}

	exists, rel, err := getRelease(cfg)
	if err != nil {
		return nil, err
	}
	st.HelmReleaseExists = exists
	if !exists {
		return st, nil
	}
	if rel.Info != nil {
		st.Message = rel.Info.Description
		if rel.Info.Status == release.StatusFailed {
			st.Phase = "failed"
			return st, nil
		}
	}

	// kube-prometheus-stack creates a StatefulSet "prometheus-<release>-prometheus"
	// and a Deployment "<release>-grafana". Names are deterministic for the
	// pinned release name above.
	prom, err := c.clientset.AppsV1().StatefulSets(monitoringNamespace).
		Get(ctx, "prometheus-"+helmReleaseName+"-prometheus", metav1.GetOptions{})
	if err == nil {
		st.PrometheusReady = prom.Status.ReadyReplicas > 0 && prom.Status.ReadyReplicas == *prom.Spec.Replicas
	}
	graf, err := c.clientset.AppsV1().Deployments(monitoringNamespace).
		Get(ctx, helmReleaseName+"-grafana", metav1.GetOptions{})
	if err == nil {
		st.GrafanaReady = graf.Status.ReadyReplicas > 0 && graf.Status.ReadyReplicas == *graf.Spec.Replicas
	}

	switch {
	case st.PrometheusReady && st.GrafanaReady:
		st.Phase = "ready"
	default:
		st.Phase = "installing"
	}
	return st, nil
}

// --- helm plumbing ---

// newHelmConfig wires up a one-shot Helm action.Configuration against the
// given kubeconfig. The kubeconfig is materialized to a tempfile because
// genericclioptions.ConfigFlags reads from disk; the tempfile is deleted
// on cleanup.
func newHelmConfig(kubeconfig []byte, namespace string) (*action.Configuration, func(), error) {
	f, err := os.CreateTemp("", "shipit-kubeconfig-*")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.Remove(f.Name()) }
	if _, err := f.Write(kubeconfig); err != nil {
		cleanup()
		return nil, nil, err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return nil, nil, err
	}

	flags := genericclioptions.NewConfigFlags(false)
	path := f.Name()
	flags.KubeConfig = &path
	flags.Namespace = &namespace

	cfg := new(action.Configuration)
	if err := cfg.Init(flags, namespace, "secret", func(format string, v ...interface{}) {
		// Helm's debug logger is firehose-y; swallow.
	}); err != nil {
		cleanup()
		return nil, nil, err
	}
	return cfg, cleanup, nil
}

// pullChart fetches the pinned kube-prometheus-stack chart tarball into the
// process tempdir and returns its path. Reused on subsequent installs in the
// same shipit process via the Helm cache directory.
func pullChart() (string, error) {
	settings := cli.New()
	pull := action.NewPullWithOpts(action.WithConfig(new(action.Configuration)))
	pull.Settings = settings
	pull.RepoURL = helmRepoURL
	pull.Version = helmChartVersion
	pull.Untar = false
	dest, err := os.MkdirTemp("", "shipit-chart-*")
	if err != nil {
		return "", err
	}
	pull.DestDir = dest
	if _, err := pull.Run(helmChartName); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%s-%s.tgz", dest, helmChartName, helmChartVersion), nil
}

func releaseExists(cfg *action.Configuration) (bool, error) {
	exists, _, err := getRelease(cfg)
	return exists, err
}

func getRelease(cfg *action.Configuration) (bool, *release.Release, error) {
	hist := action.NewHistory(cfg)
	hist.Max = 1
	rels, err := hist.Run(helmReleaseName)
	if err != nil {
		if strings.Contains(err.Error(), "release: not found") {
			return false, nil, nil
		}
		return false, nil, err
	}
	if len(rels) == 0 {
		return false, nil, nil
	}
	return true, rels[0], nil
}

// renderValues produces the values map passed to helm upgrade. We only set
// the knobs that materially differ from kube-prometheus-stack defaults:
// Grafana ingress + Google OAuth, retention/storage, single-replica
// everything (we're optimizing for "small footprint, single user").
const valuesTemplate = `
prometheus:
  prometheusSpec:
    retention: {{.RetentionDays}}d
    resources:
      requests: {cpu: 100m, memory: 400Mi}
      limits:   {memory: 1Gi}
    storageSpec:
      volumeClaimTemplate:
        spec:
          accessModes: ["ReadWriteOnce"]
          resources:
            requests:
              storage: {{.PrometheusStorage}}
    serviceMonitorSelectorNilUsesHelmValues: false
    serviceMonitorNamespaceSelector: {}

alertmanager:
  enabled: false

grafana:
  defaultDashboardsEnabled: true
  resources:
    requests: {cpu: 50m, memory: 100Mi}
    limits:   {memory: 300Mi}
  persistence:
    enabled: true
    size: {{.GrafanaStorage}}
  ingress:
    enabled: true
    ingressClassName: {{.IngressClass}}
    annotations:
      cert-manager.io/cluster-issuer: {{.ClusterIssuer}}
    hosts:
      - {{.GrafanaHost}}
    tls:
      - secretName: grafana-tls
        hosts:
          - {{.GrafanaHost}}
  grafana.ini:
    server:
      root_url: https://{{.GrafanaHost}}
    auth:
      disable_login_form: true
    "auth.anonymous":
      enabled: false
    "auth.google":
      enabled: true
      client_id: {{.GoogleClientID}}
      client_secret: {{.GoogleClientSecret}}
      allowed_domains: {{.GoogleAllowedDomain}}
      allow_sign_up: true
      scopes: openid email profile

# Single-replica everything for the small-cluster footprint.
prometheusOperator:
  resources:
    requests: {cpu: 50m, memory: 100Mi}
    limits:   {memory: 200Mi}

kubeStateMetrics:
  enabled: true
nodeExporter:
  enabled: true
`

func renderValues(req MonitoringInstallRequest) (map[string]interface{}, error) {
	tmpl, err := template.New("values").Parse(valuesTemplate)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, req); err != nil {
		return nil, err
	}
	out := map[string]interface{}{}
	if err := yaml.Unmarshal(buf.Bytes(), &out); err != nil {
		return nil, fmt.Errorf("parse rendered values: %w (rendered: %s)", err, buf.String())
	}
	return out, nil
}

// --- request validation/defaults ---

func (r *MonitoringInstallRequest) validate() error {
	if r.GrafanaHost == "" {
		return fmt.Errorf("grafana_host required")
	}
	if r.GoogleClientID == "" || r.GoogleClientSecret == "" {
		return fmt.Errorf("google oauth client id+secret required")
	}
	if r.GoogleAllowedDomain == "" {
		return fmt.Errorf("google_allowed_domain required (e.g. 'example.com')")
	}
	return nil
}

func (r *MonitoringInstallRequest) applyDefaults() {
	if r.RetentionDays == 0 {
		r.RetentionDays = 14
	}
	if r.PrometheusStorage == "" {
		r.PrometheusStorage = "5Gi"
	}
	if r.GrafanaStorage == "" {
		r.GrafanaStorage = "1Gi"
	}
	if r.IngressClass == "" {
		r.IngressClass = "nginx"
	}
	if r.ClusterIssuer == "" {
		r.ClusterIssuer = "letsencrypt-prod"
	}
}

// --- chart repo discovery (used once on cold start to populate helm cache) ---

// ensureRepoIndexed makes sure the Helm chart repo index is available. Only
// required if pullChart() can't resolve the chart from cache — kept as a
// helper because Helm's repo handling is otherwise hidden.
//
// Currently unused (pullChart() takes RepoURL directly); kept for future
// migration to a multi-chart story.
func ensureRepoIndexed(repoURL string) error {
	settings := cli.New()
	entry := &repo.Entry{Name: "prometheus-community", URL: repoURL}
	r, err := repo.NewChartRepository(entry, getter.All(settings))
	if err != nil {
		return err
	}
	_, err = r.DownloadIndexFile()
	return err
}

// --- helpers used by the API layer (kept here so callers don't import helm) ---

// MonitoringNamespace returns the fixed install namespace; exported so the
// API layer can build apiserver-proxy URLs without re-declaring it.
func MonitoringNamespace() string { return monitoringNamespace }

// MonitoringHelmReleaseName returns the fixed Helm release name. Useful for
// the API layer when constructing service names like
// "<release>-grafana" or "prometheus-<release>-prometheus".
func MonitoringHelmReleaseName() string { return helmReleaseName }

// promServiceURL returns the cluster-internal URL used by the apiserver
// service proxy to reach Prometheus' HTTP API. Kept here so the constant
// for the prometheus-operated service name lives next to the install.
func promServiceURL(path string, query url.Values) string {
	u := &url.URL{
		Path:     fmt.Sprintf("/api/v1/namespaces/%s/services/%s-prometheus:9090/proxy%s", monitoringNamespace, helmReleaseName, path),
		RawQuery: query.Encode(),
	}
	return u.String()
}

