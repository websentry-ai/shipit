package k8s

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// When DisableZeroDowntime is set, the renderer must skip preStop, topology
// spread, PDB, and force the rolling-update budget back to raw kube defaults.
// This is the "advanced power-user override" path; the Phase 2.11 primitives
// should disappear entirely so the user gets back to the K8s defaults.
func TestDeployApp_LegacyModeStripsReliabilityPrimitives(t *testing.T) {
	c := newTestClient()
	port := 8080
	if err := c.DeployApp(DeployRequest{
		Name:                "svc",
		Namespace:           "default",
		Image:               "r/app:v1",
		Replicas:            4, // would normally get a PDB
		Port:                &port,
		DisableZeroDowntime: true,
	}); err != nil {
		t.Fatalf("DeployApp: %v", err)
	}

	dep, _ := c.clientset.AppsV1().Deployments("default").Get(context.Background(), "svc", metav1.GetOptions{})
	pod := dep.Spec.Template.Spec
	if len(pod.TopologySpreadConstraints) != 0 {
		t.Errorf("topologySpread should be empty in legacy mode, got %d", len(pod.TopologySpreadConstraints))
	}
	if pod.Containers[0].Lifecycle != nil {
		t.Error("preStop hook should be absent in legacy mode")
	}
	ru := dep.Spec.Strategy.RollingUpdate
	if ru.MaxSurge.Type != intstr.String || ru.MaxSurge.StrVal != "25%" {
		t.Errorf("legacy mode maxSurge should be 25%%, got %v", ru.MaxSurge)
	}
	if ru.MaxUnavailable.Type != intstr.String || ru.MaxUnavailable.StrVal != "25%" {
		t.Errorf("legacy mode maxUnavailable should be 25%%, got %v", ru.MaxUnavailable)
	}
	if _, err := c.clientset.PolicyV1().PodDisruptionBudgets("default").Get(context.Background(), "svc", metav1.GetOptions{}); err == nil {
		t.Error("PDB should not exist in legacy mode")
	}
}

// Flipping legacy mode on after a zero-downtime deploy must tear down the
// previously-created PDB (otherwise it lingers as orphaned policy that the
// user can't see in shipit's UI).
func TestDeployApp_LegacyModeDeletesExistingPDB(t *testing.T) {
	c := newTestClient()
	port := 8080
	// First deploy with safe defaults — creates a PDB.
	if err := c.DeployApp(DeployRequest{
		Name:      "svc",
		Namespace: "default",
		Image:     "r/app:v1",
		Replicas:  3,
		Port:      &port,
	}); err != nil {
		t.Fatalf("zero-downtime deploy: %v", err)
	}
	if _, err := c.clientset.PolicyV1().PodDisruptionBudgets("default").Get(context.Background(), "svc", metav1.GetOptions{}); err != nil {
		t.Fatalf("expected PDB after first deploy: %v", err)
	}
	// Second deploy with legacy mode — PDB should be deleted.
	if err := c.DeployApp(DeployRequest{
		Name:                "svc",
		Namespace:           "default",
		Image:               "r/app:v2",
		Replicas:            3,
		Port:                &port,
		DisableZeroDowntime: true,
	}); err != nil {
		t.Fatalf("legacy deploy: %v", err)
	}
	if _, err := c.clientset.PolicyV1().PodDisruptionBudgets("default").Get(context.Background(), "svc", metav1.GetOptions{}); err == nil {
		t.Error("PDB should be deleted after switch to legacy mode")
	}
}

func TestResolveRollingUpdateBudget_HonorsOverrides(t *testing.T) {
	cases := []struct {
		name        string
		fleet       int32
		surge       *string
		unavail     *string
		wantSurge   intstr.IntOrString
		wantUnavail intstr.IntOrString
	}{
		{"derived small fleet", 3, nil, nil, intstr.FromInt(1), intstr.FromInt(0)},
		{"derived large fleet", 10, nil, nil, intstr.FromString("25%"), intstr.FromString("25%")},
		{"override int surge", 3, strPtr("2"), nil, intstr.FromInt(2), intstr.FromInt(0)},
		{"override percent unavail", 10, nil, strPtr("50%"), intstr.FromString("25%"), intstr.FromString("50%")},
		{"both overridden", 3, strPtr("3"), strPtr("1"), intstr.FromInt(3), intstr.FromInt(1)},
		{"bad override falls through to derived", 3, strPtr("garbage"), nil, intstr.FromInt(1), intstr.FromInt(0)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := DeployRequest{MaxSurgeOverride: tc.surge, MaxUnavailableOverride: tc.unavail}
			s, u := resolveRollingUpdateBudget(req, tc.fleet)
			if s != tc.wantSurge || u != tc.wantUnavail {
				t.Errorf("got (%v,%v), want (%v,%v)", s, u, tc.wantSurge, tc.wantUnavail)
			}
		})
	}
}

// terminationGrace = max_request_duration + 10s buffer. Used by long-running
// API endpoints (LLM streaming etc.) that need more than the default 30s.
func TestDeployApp_TerminationGraceTrackedFromRequestDuration(t *testing.T) {
	c := newTestClient()
	port := 8080
	if err := c.DeployApp(DeployRequest{
		Name:                      "svc",
		Namespace:                 "default",
		Image:                     "r/app:v1",
		Replicas:                  2,
		Port:                      &port,
		MaxRequestDurationSeconds: 120,
	}); err != nil {
		t.Fatalf("DeployApp: %v", err)
	}
	dep, _ := c.clientset.AppsV1().Deployments("default").Get(context.Background(), "svc", metav1.GetOptions{})
	got := *dep.Spec.Template.Spec.TerminationGracePeriodSeconds
	if got != 130 {
		t.Errorf("terminationGrace = %d, want 130 (120 + 10s buffer)", got)
	}
}

func strPtr(s string) *string { return &s }
