package k8s

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestImagePullPolicyFor(t *testing.T) {
	cases := []struct {
		image string
		want  corev1.PullPolicy
	}{
		{"repo/app:latest", corev1.PullAlways},
		{"repo/app", corev1.PullAlways},
		{"registry:5000/repo/app", corev1.PullAlways},
		{"repo/app:abc123", corev1.PullIfNotPresent},
		{"registry:5000/repo/app:v1.2.3", corev1.PullIfNotPresent},
		{"repo/app@sha256:deadbeef", corev1.PullIfNotPresent},
	}
	for _, tc := range cases {
		if got := imagePullPolicyFor(tc.image); got != tc.want {
			t.Errorf("imagePullPolicyFor(%q) = %v, want %v", tc.image, got, tc.want)
		}
	}
}

func TestRollingUpdateBudget(t *testing.T) {
	cases := []struct {
		replicas     int32
		wantSurge    intstr.IntOrString
		wantUnavail  intstr.IntOrString
	}{
		{1, intstr.FromInt(1), intstr.FromInt(0)},
		{2, intstr.FromInt(1), intstr.FromInt(0)},
		{3, intstr.FromInt(1), intstr.FromInt(0)},
		{4, intstr.FromString("25%"), intstr.FromString("25%")},
		{10, intstr.FromString("25%"), intstr.FromString("25%")},
	}
	for _, tc := range cases {
		s, u := rollingUpdateBudget(tc.replicas)
		if s != tc.wantSurge || u != tc.wantUnavail {
			t.Errorf("rollingUpdateBudget(%d) = (%v,%v), want (%v,%v)",
				tc.replicas, s, u, tc.wantSurge, tc.wantUnavail)
		}
	}
}

func TestDeployApp_AppliesReliabilityDefaults(t *testing.T) {
	c := newTestClient()
	port := 8080
	healthPath := "/healthz"
	req := DeployRequest{
		Name:       "svc",
		Namespace:  "default",
		Image:      "r/app:abc123",
		Replicas:   4,
		Port:       &port,
		HealthPath: &healthPath,
	}
	if err := c.DeployApp(req); err != nil {
		t.Fatalf("DeployApp: %v", err)
	}

	dep, err := c.clientset.AppsV1().Deployments("default").Get(context.Background(), "svc", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}

	if dep.Spec.Strategy.RollingUpdate == nil {
		t.Fatal("expected RollingUpdate strategy")
	}
	if dep.Spec.ProgressDeadlineSeconds == nil || *dep.Spec.ProgressDeadlineSeconds == 0 {
		t.Error("ProgressDeadlineSeconds not set")
	}
	if dep.Spec.RevisionHistoryLimit == nil || *dep.Spec.RevisionHistoryLimit == 0 {
		t.Error("RevisionHistoryLimit not set")
	}

	pod := dep.Spec.Template.Spec
	if pod.TerminationGracePeriodSeconds == nil {
		t.Error("TerminationGracePeriodSeconds not set")
	}
	if len(pod.TopologySpreadConstraints) != 2 {
		t.Errorf("want 2 topology constraints, got %d", len(pod.TopologySpreadConstraints))
	}

	ct := pod.Containers[0]
	if ct.Lifecycle == nil || ct.Lifecycle.PreStop == nil {
		t.Error("preStop hook missing")
	}
	if ct.ImagePullPolicy != corev1.PullIfNotPresent {
		t.Errorf("ImagePullPolicy = %v, want IfNotPresent for pinned tag", ct.ImagePullPolicy)
	}
	if ct.ReadinessProbe == nil || ct.LivenessProbe == nil {
		t.Error("readiness + liveness probes required when HealthPath set")
	}
	if ct.ReadinessProbe.PeriodSeconds >= ct.LivenessProbe.PeriodSeconds {
		t.Error("readiness should poll at least as often as liveness")
	}
	if ct.LivenessProbe.InitialDelaySeconds <= ct.ReadinessProbe.InitialDelaySeconds {
		t.Error("liveness initial delay should exceed readiness to protect slow starts")
	}
}

func TestDeployApp_TCPProbeFallbackWhenNoHealthPath(t *testing.T) {
	c := newTestClient()
	port := 9000
	req := DeployRequest{
		Name:      "svc",
		Namespace: "default",
		Image:     "r/app:v1",
		Replicas:  2,
		Port:      &port,
	}
	if err := c.DeployApp(req); err != nil {
		t.Fatalf("DeployApp: %v", err)
	}
	dep, _ := c.clientset.AppsV1().Deployments("default").Get(context.Background(), "svc", metav1.GetOptions{})
	ct := dep.Spec.Template.Spec.Containers[0]
	if ct.ReadinessProbe == nil || ct.ReadinessProbe.TCPSocket == nil {
		t.Fatal("expected TCP readiness probe fallback")
	}
	if ct.ReadinessProbe.TCPSocket.Port != intstr.FromInt(port) {
		t.Errorf("TCP probe port = %v, want %d", ct.ReadinessProbe.TCPSocket.Port, port)
	}
}

func TestEnsurePodDisruptionBudget_CreatesForMultiReplica(t *testing.T) {
	c := newTestClient()
	req := DeployRequest{Name: "svc", Namespace: "default", Replicas: 4}
	if err := c.ensurePodDisruptionBudget(context.Background(), req); err != nil {
		t.Fatalf("ensurePDB: %v", err)
	}
	pdb, err := c.clientset.PolicyV1().PodDisruptionBudgets("default").Get(context.Background(), "svc", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get pdb: %v", err)
	}
	if pdb.Spec.MinAvailable == nil || pdb.Spec.MinAvailable.IntValue() != 3 {
		t.Errorf("want minAvailable=3, got %v", pdb.Spec.MinAvailable)
	}
}

func TestEnsurePodDisruptionBudget_DeletesForSingleReplica(t *testing.T) {
	c := newTestClient()
	// seed an existing PDB
	if err := c.ensurePodDisruptionBudget(context.Background(), DeployRequest{Name: "svc", Namespace: "default", Replicas: 3}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// scale down to 1
	if err := c.ensurePodDisruptionBudget(context.Background(), DeployRequest{Name: "svc", Namespace: "default", Replicas: 1}); err != nil {
		t.Fatalf("scale-down: %v", err)
	}
	_, err := c.clientset.PolicyV1().PodDisruptionBudgets("default").Get(context.Background(), "svc", metav1.GetOptions{})
	if err == nil {
		t.Fatal("expected PDB to be deleted for single-replica app")
	}
}

func TestEnsurePodDisruptionBudget_IdempotentUpdate(t *testing.T) {
	c := newTestClient()
	ctx := context.Background()
	req := DeployRequest{Name: "svc", Namespace: "default", Replicas: 3}
	if err := c.ensurePodDisruptionBudget(ctx, req); err != nil {
		t.Fatalf("first: %v", err)
	}
	// Second call with different replicas should update, not error.
	req.Replicas = 5
	if err := c.ensurePodDisruptionBudget(ctx, req); err != nil {
		t.Fatalf("second: %v", err)
	}
	pdb, _ := c.clientset.PolicyV1().PodDisruptionBudgets("default").Get(ctx, "svc", metav1.GetOptions{})
	if pdb.Spec.MinAvailable.IntValue() != 4 {
		t.Errorf("want minAvailable=4 after scale-up, got %v", pdb.Spec.MinAvailable)
	}
}
