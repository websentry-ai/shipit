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

func TestDeployApp_ReconcilesHPAWhenEnabled(t *testing.T) {
	c := newTestClient()
	port := 8080
	healthPath := "/healthz"
	minR := int32(3)
	maxR := int32(12)
	cpuTarget := int32(70)
	req := DeployRequest{
		Name:           "svc",
		Namespace:      "default",
		Image:          "r/app:v1",
		Replicas:       3,
		Port:           &port,
		HealthPath:     &healthPath,
		HPAEnabled:     true,
		HPAMinReplicas: &minR,
		HPAMaxReplicas: &maxR,
		HPATargetCPU:   &cpuTarget,
	}
	if err := c.DeployApp(req); err != nil {
		t.Fatalf("DeployApp: %v", err)
	}
	hpa, err := c.clientset.AutoscalingV2().HorizontalPodAutoscalers("default").Get(context.Background(), "svc", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected HPA to exist: %v", err)
	}
	if hpa.Spec.MinReplicas == nil || *hpa.Spec.MinReplicas != 3 {
		t.Errorf("minReplicas = %v, want 3", hpa.Spec.MinReplicas)
	}
	if hpa.Spec.MaxReplicas != 12 {
		t.Errorf("maxReplicas = %d, want 12", hpa.Spec.MaxReplicas)
	}
	if len(hpa.Spec.Metrics) == 0 || hpa.Spec.Metrics[0].Resource.Target.AverageUtilization == nil ||
		*hpa.Spec.Metrics[0].Resource.Target.AverageUtilization != 70 {
		t.Errorf("CPU target not propagated")
	}
}

func TestDeployApp_ClampsHPAMinReplicasToTwo(t *testing.T) {
	c := newTestClient()
	port := 8080
	minR := int32(1)
	maxR := int32(5)
	req := DeployRequest{
		Name:           "svc",
		Namespace:      "default",
		Image:          "r/app:v1",
		Replicas:       2,
		Port:           &port,
		HPAEnabled:     true,
		HPAMinReplicas: &minR,
		HPAMaxReplicas: &maxR,
	}
	if err := c.DeployApp(req); err != nil {
		t.Fatalf("DeployApp: %v", err)
	}
	hpa, err := c.clientset.AutoscalingV2().HorizontalPodAutoscalers("default").Get(context.Background(), "svc", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected HPA: %v", err)
	}
	if hpa.Spec.MinReplicas == nil || *hpa.Spec.MinReplicas != 2 {
		t.Errorf("minReplicas = %v, want 2 (clamped from 1)", hpa.Spec.MinReplicas)
	}
}

func TestDeployApp_ClampsHPAMinReplicasWhenNil(t *testing.T) {
	c := newTestClient()
	port := 8080
	req := DeployRequest{
		Name:       "svc",
		Namespace:  "default",
		Image:      "r/app:v1",
		Replicas:   2,
		Port:       &port,
		HPAEnabled: true,
	}
	if err := c.DeployApp(req); err != nil {
		t.Fatalf("DeployApp: %v", err)
	}
	hpa, err := c.clientset.AutoscalingV2().HorizontalPodAutoscalers("default").Get(context.Background(), "svc", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected HPA: %v", err)
	}
	if hpa.Spec.MinReplicas == nil || *hpa.Spec.MinReplicas != 2 {
		t.Errorf("minReplicas = %v, want 2 when nil", hpa.Spec.MinReplicas)
	}
	if hpa.Spec.MaxReplicas < 2 {
		t.Errorf("maxReplicas = %d, want ≥2 default", hpa.Spec.MaxReplicas)
	}
}

func TestDeployApp_DeletesHPAWhenDisabled(t *testing.T) {
	c := newTestClient()
	port := 8080
	minR := int32(2)
	maxR := int32(6)
	// First deploy with HPA enabled.
	if err := c.DeployApp(DeployRequest{
		Name:           "svc",
		Namespace:      "default",
		Image:          "r/app:v1",
		Replicas:       2,
		Port:           &port,
		HPAEnabled:     true,
		HPAMinReplicas: &minR,
		HPAMaxReplicas: &maxR,
	}); err != nil {
		t.Fatalf("first deploy: %v", err)
	}
	if _, err := c.clientset.AutoscalingV2().HorizontalPodAutoscalers("default").Get(context.Background(), "svc", metav1.GetOptions{}); err != nil {
		t.Fatalf("expected HPA after first deploy: %v", err)
	}
	// Second deploy with HPA flipped off — should delete, not orphan.
	if err := c.DeployApp(DeployRequest{
		Name:       "svc",
		Namespace:  "default",
		Image:      "r/app:v2",
		Replicas:   2,
		Port:       &port,
		HPAEnabled: false,
	}); err != nil {
		t.Fatalf("second deploy: %v", err)
	}
	if _, err := c.clientset.AutoscalingV2().HorizontalPodAutoscalers("default").Get(context.Background(), "svc", metav1.GetOptions{}); err == nil {
		t.Fatal("expected HPA to be deleted after disable, but it still exists")
	}
}

func TestDeployApp_HPADisabledIsNoOpWhenNoPriorHPA(t *testing.T) {
	c := newTestClient()
	port := 8080
	if err := c.DeployApp(DeployRequest{
		Name:       "svc",
		Namespace:  "default",
		Image:      "r/app:v1",
		Replicas:   2,
		Port:       &port,
		HPAEnabled: false,
	}); err != nil {
		t.Fatalf("DeployApp with HPA disabled and no prior HPA should succeed: %v", err)
	}
	if _, err := c.clientset.AutoscalingV2().HorizontalPodAutoscalers("default").Get(context.Background(), "svc", metav1.GetOptions{}); err == nil {
		t.Fatal("no HPA should exist when disabled")
	}
}

func TestDeployApp_HPAMaxBelowMinIsCoerced(t *testing.T) {
	c := newTestClient()
	port := 8080
	minR := int32(5)
	maxR := int32(3) // nonsensical: max < min
	req := DeployRequest{
		Name:           "svc",
		Namespace:      "default",
		Image:          "r/app:v1",
		Replicas:       5,
		Port:           &port,
		HPAEnabled:     true,
		HPAMinReplicas: &minR,
		HPAMaxReplicas: &maxR,
	}
	if err := c.DeployApp(req); err != nil {
		t.Fatalf("DeployApp: %v", err)
	}
	hpa, _ := c.clientset.AutoscalingV2().HorizontalPodAutoscalers("default").Get(context.Background(), "svc", metav1.GetOptions{})
	if hpa.Spec.MaxReplicas != *hpa.Spec.MinReplicas {
		t.Errorf("expected max coerced to equal min=%d, got max=%d", *hpa.Spec.MinReplicas, hpa.Spec.MaxReplicas)
	}
}

func TestDeployApp_HPAMemoryTargetPropagates(t *testing.T) {
	c := newTestClient()
	port := 8080
	minR := int32(2)
	maxR := int32(6)
	memTarget := int32(75)
	req := DeployRequest{
		Name:            "svc",
		Namespace:       "default",
		Image:           "r/app:v1",
		Replicas:        2,
		Port:            &port,
		HPAEnabled:      true,
		HPAMinReplicas:  &minR,
		HPAMaxReplicas:  &maxR,
		HPATargetMemory: &memTarget,
	}
	if err := c.DeployApp(req); err != nil {
		t.Fatalf("DeployApp: %v", err)
	}
	hpa, _ := c.clientset.AutoscalingV2().HorizontalPodAutoscalers("default").Get(context.Background(), "svc", metav1.GetOptions{})
	foundMem := false
	for _, m := range hpa.Spec.Metrics {
		if m.Resource != nil && m.Resource.Name == corev1.ResourceMemory {
			if m.Resource.Target.AverageUtilization != nil && *m.Resource.Target.AverageUtilization == 75 {
				foundMem = true
			}
		}
	}
	if !foundMem {
		t.Error("memory target not propagated to HPA")
	}
}

// Regression guard for the critical bug surfaced in the first elite-pr review:
// when an HPA has scaled a deployment beyond its static Replicas, a subsequent
// DeployApp must NOT reset Spec.Replicas to the DB value (which would fight
// the HPA controller and bounce the pod count on every redeploy).
func TestDeployApp_DoesNotOverrideHPAManagedReplicas(t *testing.T) {
	c := newTestClient()
	port := 8080
	minR := int32(2)
	maxR := int32(20)
	base := DeployRequest{
		Name:           "svc",
		Namespace:      "default",
		Image:          "r/app:v1",
		Replicas:       3,
		Port:           &port,
		HPAEnabled:     true,
		HPAMinReplicas: &minR,
		HPAMaxReplicas: &maxR,
	}
	if err := c.DeployApp(base); err != nil {
		t.Fatalf("initial deploy: %v", err)
	}

	// Simulate HPA scaling the deployment up to 10 pods.
	ctx := context.Background()
	dep, _ := c.clientset.AppsV1().Deployments("default").Get(ctx, "svc", metav1.GetOptions{})
	scaled := int32(10)
	dep.Spec.Replicas = &scaled
	if _, err := c.clientset.AppsV1().Deployments("default").Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("simulate HPA scale: %v", err)
	}

	// Redeploy same app (new image version, unchanged static Replicas).
	base.Image = "r/app:v2"
	if err := c.DeployApp(base); err != nil {
		t.Fatalf("redeploy: %v", err)
	}

	after, _ := c.clientset.AppsV1().Deployments("default").Get(ctx, "svc", metav1.GetOptions{})
	if after.Spec.Replicas == nil || *after.Spec.Replicas != 10 {
		t.Errorf("HPA-scaled replicas clobbered by redeploy: want 10, got %v", after.Spec.Replicas)
	}
}

// When HPA is NOT enabled, redeploying must still honor the static Replicas
// from the DeployRequest (otherwise scale-up via deploy stops working).
func TestDeployApp_NonHPARespectsRequestReplicas(t *testing.T) {
	c := newTestClient()
	port := 8080
	base := DeployRequest{
		Name:      "svc",
		Namespace: "default",
		Image:     "r/app:v1",
		Replicas:  3,
		Port:      &port,
	}
	if err := c.DeployApp(base); err != nil {
		t.Fatalf("initial deploy: %v", err)
	}
	base.Replicas = 6
	if err := c.DeployApp(base); err != nil {
		t.Fatalf("redeploy: %v", err)
	}
	dep, _ := c.clientset.AppsV1().Deployments("default").Get(context.Background(), "svc", metav1.GetOptions{})
	if dep.Spec.Replicas == nil || *dep.Spec.Replicas != 6 {
		t.Errorf("non-HPA redeploy should honor req.Replicas=6, got %v", dep.Spec.Replicas)
	}
}

// PDB minAvailable should track the effective fleet size when HPA is enabled,
// not the static Replicas value. Otherwise an HPA-scaled fleet of 10 would
// have a PDB computed from Replicas=2 allowing only 1 pod to stay up.
func TestEnsurePodDisruptionBudget_UsesHPAMinWhenHigher(t *testing.T) {
	c := newTestClient()
	hpaMin := int32(5)
	req := DeployRequest{
		Name:           "svc",
		Namespace:      "default",
		Replicas:       2, // static
		HPAEnabled:     true,
		HPAMinReplicas: &hpaMin,
	}
	if err := c.ensurePodDisruptionBudget(context.Background(), req); err != nil {
		t.Fatalf("ensurePDB: %v", err)
	}
	pdb, _ := c.clientset.PolicyV1().PodDisruptionBudgets("default").Get(context.Background(), "svc", metav1.GetOptions{})
	if pdb.Spec.MinAvailable.IntValue() != 4 {
		t.Errorf("want minAvailable=4 (hpaMin-1), got %v", pdb.Spec.MinAvailable)
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
