package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/vigneshsubbiah/shipit/internal/db"
)

// Reliability ("zero-downtime mode") configuration. Surfaced on the dashboard
// as a single enable/disable toggle plus an Advanced disclosure for users who
// need to override the rolling-update budget.
//
// The renderer always treats nil overrides as "derive from replica count".
// The API normalizes empty strings to nil so the JSON round-trip is
// idempotent.

// ReliabilityConfig is the body shape of PUT /api/apps/{id}/reliability.
type ReliabilityConfig struct {
	Enabled                   bool    `json:"enabled"`
	MaxSurge                  *string `json:"max_surge,omitempty"`
	MaxUnavailable            *string `json:"max_unavailable,omitempty"`
	MaxRequestDurationSeconds int     `json:"max_request_duration_seconds"`
}

// ReliabilityResponse is the shape returned by GET /api/apps/{id}/reliability.
// Mirrors the contract sketched in the design: enabled + advanced + derived
// values + preconditions/blockers, so the UI does not recompute defaults.
type ReliabilityResponse struct {
	Enabled       bool                `json:"enabled"`
	Advanced      ReliabilityAdvanced `json:"advanced"`
	Derived       ReliabilityDerived  `json:"derived"`
	Preconditions Preconditions       `json:"preconditions"`
	Diff          []DiffRow           `json:"diff"`
}

type ReliabilityAdvanced struct {
	MaxSurge                  *string `json:"max_surge"`
	MaxUnavailable            *string `json:"max_unavailable"`
	MaxRequestDurationSeconds int     `json:"max_request_duration_seconds"`
}

type ReliabilityDerived struct {
	RollingUpdate            RollingUpdate `json:"rolling_update"`
	PDB                      *PDBSpec      `json:"pdb,omitempty"`
	TerminationGraceSeconds  int           `json:"termination_grace_seconds"`
	PreStop                  *PreStopSpec  `json:"pre_stop,omitempty"`
	TopologySpread           []TopologyKey `json:"topology_spread"`
	ProgressDeadlineSeconds  int           `json:"progress_deadline_seconds"`
	ImagePullPolicy          string        `json:"image_pull_policy"`
	ReadinessProbeConfigured bool          `json:"readiness_probe_configured"`
}

type RollingUpdate struct {
	MaxSurge       string `json:"max_surge"`
	MaxUnavailable string `json:"max_unavailable"`
}

type PDBSpec struct {
	MinAvailable int `json:"min_available"`
}

type PreStopSpec struct {
	Exec string `json:"exec"`
}

type TopologyKey struct {
	Key     string `json:"key"`
	MaxSkew int    `json:"max_skew"`
}

type Preconditions struct {
	ReplicasOK         bool     `json:"replicas_ok"`
	MinReplicasReq     int      `json:"min_replicas_required"`
	HealthPathSet      bool     `json:"health_path_set"`
	ImageTagImmutable  bool     `json:"image_tag_immutable"`
	Blockers           []string `json:"blockers"`
}

type DiffRow struct {
	Field string `json:"field"`
	From  string `json:"from"`
	To    string `json:"to"`
}

// GetReliability returns the current zero-downtime config plus the derived
// values the renderer would apply on the next deploy.
func (h *Handler) GetReliability(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appID")
	app, err := h.db.GetApp(r.Context(), appID)
	if err != nil {
		httpError(w, "app not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(buildReliabilityResponse(app))
}

// SetReliability persists the toggle + overrides. Does not redeploy; the UI
// shows a "redeploy to apply" CTA so users batch with other changes.
//
// Validation deliberately rejects:
//   - both maxSurge=0 and maxUnavailable=0 (rollout deadlocks)
//   - maxUnavailable >= replicas (no pods stay up)
//   - max_request_duration_seconds outside [5, 300]
//   - bad override formats (non-int, non-percent, > 100%)
//
// Replicas < 2 with enabled=true is allowed so the user can flip the toggle
// before bumping replicas; the "blockers" field on GET surfaces the warning.
// This mirrors the HPA flow which silently clamps min<2.
func (h *Handler) SetReliability(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appID")
	app, err := h.db.GetApp(r.Context(), appID)
	if err != nil {
		httpError(w, "app not found", http.StatusNotFound)
		return
	}

	var req ReliabilityConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	surge := normalizeOverride(req.MaxSurge)
	unavail := normalizeOverride(req.MaxUnavailable)

	if err := validateOverride("max_surge", surge); err != nil {
		httpError(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := validateOverride("max_unavailable", unavail); err != nil {
		httpError(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Reject the deadlock case using *effective* values, not just the
	// explicit overrides: on fleets with <=3 replicas the renderer derives
	// maxUnavailable="0", so an explicit max_surge="0" with omitted unavail
	// would still lock up the rollout.
	effSurge, effUnavail := derivedRollingUpdate(int32(app.Replicas), surge, unavail)
	if isZero(&effSurge) && isZero(&effUnavail) {
		httpError(w, "max_surge and max_unavailable cannot both be 0 (rollout would deadlock)", http.StatusBadRequest)
		return
	}
	// maxUnavailable must leave at least one pod running. K8s computes the
	// integer floor of (replicas * pct / 100) for percent values (roundUp=false
	// for maxUnavailable), so 100% on 2 replicas drains both pods. Reject any
	// override — int or percent — that resolves to >= replicas.
	if app.Replicas > 0 {
		if n, ok := resolveUnavailableToInt(unavail, app.Replicas); ok && n >= app.Replicas {
			httpError(w, fmt.Sprintf("max_unavailable (%s) would leave no pods running for %d replicas", *unavail, app.Replicas), http.StatusBadRequest)
			return
		}
	}
	dur := req.MaxRequestDurationSeconds
	if dur == 0 {
		dur = 30 // schema default; tolerate clients that omit the field
	}
	if dur < 5 || dur > 300 {
		httpError(w, "max_request_duration_seconds must be between 5 and 300", http.StatusBadRequest)
		return
	}

	updated, err := h.db.UpdateAppReliability(r.Context(), db.UpdateAppReliabilityParams{
		ID:                        appID,
		ZeroDowntimeEnabled:       req.Enabled,
		MaxSurgeOverride:          surge,
		MaxUnavailableOverride:    unavail,
		MaxRequestDurationSeconds: dur,
	})
	if err != nil {
		httpError(w, "failed to save reliability config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(buildReliabilityResponse(updated))
}

// buildReliabilityResponse computes the derived values + diff the UI displays.
// Logic mirrors internal/k8s/client.go::DeployApp so the preview matches what
// the renderer will actually produce on the next deploy.
func buildReliabilityResponse(app *db.App) ReliabilityResponse {
	resp := ReliabilityResponse{
		Enabled: app.ZeroDowntimeEnabled,
		Advanced: ReliabilityAdvanced{
			MaxSurge:                  app.MaxSurgeOverride,
			MaxUnavailable:            app.MaxUnavailableOverride,
			MaxRequestDurationSeconds: app.MaxRequestDurationSeconds,
		},
	}

	// Effective replicas — same logic as effectiveFleet but without an
	// "existing" deployment lookup (the API stays out of the K8s round-trip
	// for a config-fetch endpoint).
	effective := app.Replicas
	if app.HPAEnabled && app.MinReplicas != nil && *app.MinReplicas > effective {
		effective = *app.MinReplicas
	}

	// Rolling-update preview. Disabled-mode mirrors raw kube defaults.
	if app.ZeroDowntimeEnabled {
		surge, unavail := derivedRollingUpdate(int32(effective), app.MaxSurgeOverride, app.MaxUnavailableOverride)
		resp.Derived.RollingUpdate = RollingUpdate{MaxSurge: surge, MaxUnavailable: unavail}
	} else {
		resp.Derived.RollingUpdate = RollingUpdate{MaxSurge: "25%", MaxUnavailable: "25%"}
	}

	// Termination grace = max_request_duration + 10s buffer, floor 30s.
	grace := app.MaxRequestDurationSeconds + 10
	if grace < 30 {
		grace = 30
	}
	resp.Derived.TerminationGraceSeconds = grace

	if app.ZeroDowntimeEnabled {
		if effective >= 2 {
			resp.Derived.PDB = &PDBSpec{MinAvailable: effective - 1}
		}
		resp.Derived.PreStop = &PreStopSpec{Exec: "sleep 5"}
		resp.Derived.TopologySpread = []TopologyKey{
			{Key: "topology.kubernetes.io/zone", MaxSkew: 1},
			{Key: "kubernetes.io/hostname", MaxSkew: 1},
		}
	} else {
		resp.Derived.TopologySpread = []TopologyKey{}
	}

	resp.Derived.ProgressDeadlineSeconds = 600
	resp.Derived.ImagePullPolicy = imagePullPolicyForImage(app.Image)
	resp.Derived.ReadinessProbeConfigured = app.HealthPath != nil && *app.HealthPath != ""

	// Preconditions. Blockers are user-actionable; warnings (image tag
	// mutability, missing health path) are informational and don't disable
	// the toggle.
	resp.Preconditions = Preconditions{
		ReplicasOK:        app.Replicas >= 2 || (app.HPAEnabled && app.MinReplicas != nil && *app.MinReplicas >= 2),
		MinReplicasReq:    2,
		HealthPathSet:     app.HealthPath != nil && *app.HealthPath != "",
		ImageTagImmutable: imageTagImmutable(app.Image),
		Blockers:          []string{},
	}

	resp.Diff = buildDiff(app, &resp.Derived)
	return resp
}

// derivedRollingUpdate replays the renderer's rollingUpdateBudget logic so the
// UI preview matches the deploy without importing the k8s package. Overrides
// take precedence; bad overrides fall through to the derived value (matches
// renderer behavior).
func derivedRollingUpdate(replicas int32, surgeOverride, unavailOverride *string) (string, string) {
	var surge, unavail string
	if replicas <= 3 {
		surge, unavail = "1", "0"
	} else {
		surge, unavail = "25%", "25%"
	}
	if v := normalizeOverride(surgeOverride); v != nil && validateOverride("max_surge", v) == nil {
		surge = *v
	}
	if v := normalizeOverride(unavailOverride); v != nil && validateOverride("max_unavailable", v) == nil {
		unavail = *v
	}
	return surge, unavail
}

func buildDiff(app *db.App, d *ReliabilityDerived) []DiffRow {
	rows := []DiffRow{
		{Field: "rollingUpdate", From: "default 25%/25%", To: fmt.Sprintf("maxSurge=%s, maxUnavailable=%s", d.RollingUpdate.MaxSurge, d.RollingUpdate.MaxUnavailable)},
		{Field: "terminationGracePeriodSeconds", From: "30s (kube default)", To: fmt.Sprintf("%ds", d.TerminationGraceSeconds)},
	}
	if d.PDB != nil {
		rows = append(rows, DiffRow{Field: "PodDisruptionBudget", From: "absent", To: fmt.Sprintf("minAvailable=%d", d.PDB.MinAvailable)})
	} else if !app.ZeroDowntimeEnabled {
		rows = append(rows, DiffRow{Field: "PodDisruptionBudget", From: "absent", To: "absent"})
	}
	if d.PreStop != nil {
		rows = append(rows, DiffRow{Field: "preStop hook", From: "absent", To: d.PreStop.Exec})
	}
	if len(d.TopologySpread) > 0 {
		keys := make([]string, len(d.TopologySpread))
		for i, t := range d.TopologySpread {
			keys[i] = shortTopologyKey(t.Key)
		}
		rows = append(rows, DiffRow{Field: "topologySpread", From: "absent", To: strings.Join(keys, " + ") + ", maxSkew=1"})
	}
	if !d.ReadinessProbeConfigured {
		rows = append(rows, DiffRow{Field: "readinessProbe", From: "TCP fallback", To: "configure /health for HTTP probe"})
	}
	return rows
}

// shortTopologyKey turns "topology.kubernetes.io/zone" into "zone" for diff
// rendering. Falls back to the full key for unknown shapes.
func shortTopologyKey(k string) string {
	if i := strings.LastIndex(k, "/"); i >= 0 {
		return k[i+1:]
	}
	return k
}

// normalizeOverride collapses empty strings and pointers-to-empty to nil so
// "" and missing field round-trip identically.
func normalizeOverride(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil
	}
	return &v
}

// validateOverride accepts integer ("1") or percent ("25%") shapes. Empty/nil
// is "no override" and returns nil error so the renderer derives the value.
func validateOverride(field string, s *string) error {
	if s == nil {
		return nil
	}
	v := *s
	if strings.HasSuffix(v, "%") {
		n, err := strconv.Atoi(strings.TrimSuffix(v, "%"))
		if err != nil || n < 0 || n > 100 {
			return fmt.Errorf("%s: percent must be between 0%% and 100%%", field)
		}
		return nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return fmt.Errorf("%s: must be a non-negative integer or percent (e.g. \"1\" or \"25%%\")", field)
	}
	return nil
}

// isZero returns true for "0" or "0%" — used to reject the deadlock case.
func isZero(s *string) bool {
	if s == nil {
		return false
	}
	v := strings.TrimSuffix(*s, "%")
	n, err := strconv.Atoi(v)
	return err == nil && n == 0
}

// resolveUnavailableToInt returns the effective pod count an override would
// allow to be unavailable, given the replica count. Mirrors K8s' rounding for
// maxUnavailable (floor) so the validator rejects the same configurations
// the cluster would treat as "drain all pods".
func resolveUnavailableToInt(s *string, replicas int) (int, bool) {
	if s == nil {
		return 0, false
	}
	if strings.HasSuffix(*s, "%") {
		pct, err := strconv.Atoi(strings.TrimSuffix(*s, "%"))
		if err != nil {
			return 0, false
		}
		return (replicas * pct) / 100, true
	}
	n, err := strconv.Atoi(*s)
	if err != nil {
		return 0, false
	}
	return n, true
}

// imageTagImmutable mirrors imagePullPolicyFor in the k8s package without
// importing it. Treats digest-pinned and tagged images (other than :latest)
// as immutable.
func imageTagImmutable(image string) bool {
	if strings.Contains(image, "@") {
		return true
	}
	slash := strings.LastIndex(image, "/")
	if i := strings.LastIndex(image, ":"); i > slash {
		tag := image[i+1:]
		return tag != "" && tag != "latest"
	}
	return false
}

// imagePullPolicyForImage returns the policy string the renderer would apply.
// "IfNotPresent" for immutable tags, "Always" otherwise.
func imagePullPolicyForImage(image string) string {
	if imageTagImmutable(image) {
		return "IfNotPresent"
	}
	return "Always"
}
