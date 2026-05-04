-- Phase 4: Cluster-level monitoring stack (kube-prometheus-stack via Helm)
--
-- Per-cluster row tracking the install lifecycle. The install is async
-- (Prometheus StatefulSet + cert provisioning takes 2-5 min) so the UI
-- needs status + status_message to render the right state. grafana_host
-- is the cert-manager-issued hostname like "grafana.<cluster-base-domain>";
-- shipit emits it from the installer once Helm release succeeds, the
-- ingress/cert reconcile happens out-of-band.

ALTER TABLE clusters ADD COLUMN IF NOT EXISTS monitoring_status VARCHAR(32) NOT NULL DEFAULT 'disabled';
ALTER TABLE clusters ADD COLUMN IF NOT EXISTS monitoring_status_message TEXT;
ALTER TABLE clusters ADD COLUMN IF NOT EXISTS monitoring_grafana_host VARCHAR(255);
ALTER TABLE clusters ADD COLUMN IF NOT EXISTS monitoring_helm_release VARCHAR(64);
ALTER TABLE clusters ADD COLUMN IF NOT EXISTS monitoring_chart_version VARCHAR(32);
ALTER TABLE clusters ADD COLUMN IF NOT EXISTS monitoring_installed_at TIMESTAMPTZ;
ALTER TABLE clusters ADD COLUMN IF NOT EXISTS monitoring_updated_at TIMESTAMPTZ;

COMMENT ON COLUMN clusters.monitoring_status IS 'Lifecycle: disabled|installing|ready|failed|uninstalling';
COMMENT ON COLUMN clusters.monitoring_grafana_host IS 'Public hostname of the Grafana ingress (e.g., grafana.apps.shipit.unboundsec.dev)';
COMMENT ON COLUMN clusters.monitoring_helm_release IS 'Helm release name (e.g., kube-prometheus-stack); used by uninstall';
