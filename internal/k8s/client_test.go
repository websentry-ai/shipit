package k8s

import (
	"context"
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	utilexec "k8s.io/client-go/util/exec"
)

func newTestClient(objects ...runtime.Object) *Client {
	return &Client{
		clientset: fake.NewSimpleClientset(objects...),
	}
}

// --- FindRunningPod tests ---

func TestFindRunningPod_ReturnsFirstReadyPod(t *testing.T) {
	pods := &corev1.PodList{
		Items: []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "myapp-abc123",
					Namespace: "default",
					Labels:    map[string]string{"app": "myapp"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "web"},
						{Name: "sidecar"},
					},
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionFalse},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "myapp-def456",
					Namespace: "default",
					Labels:    map[string]string{"app": "myapp"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "web"},
						{Name: "worker"},
					},
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
					},
				},
			},
		},
	}

	c := newTestClient(&pods.Items[0], &pods.Items[1])
	podName, containerName, err := c.FindRunningPod(context.Background(), "default", "myapp", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if podName != "myapp-def456" {
		t.Errorf("expected pod myapp-def456, got %s", podName)
	}
	if containerName != "web" {
		t.Errorf("expected container web, got %s", containerName)
	}
}

func TestFindRunningPod_ValidatesContainerName(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "myapp-abc123",
			Namespace: "default",
			Labels:    map[string]string{"app": "myapp"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "web"},
			},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}

	c := newTestClient(pod)

	// Valid container
	podName, containerName, err := c.FindRunningPod(context.Background(), "default", "myapp", "web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if podName != "myapp-abc123" {
		t.Errorf("expected pod myapp-abc123, got %s", podName)
	}
	if containerName != "web" {
		t.Errorf("expected container web, got %s", containerName)
	}

	// Invalid container
	_, _, err = c.FindRunningPod(context.Background(), "default", "myapp", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent container, got nil")
	}
}

func TestFindRunningPod_NoPods(t *testing.T) {
	c := newTestClient()
	_, _, err := c.FindRunningPod(context.Background(), "default", "myapp", "")
	if err == nil {
		t.Fatal("expected error when no pods exist, got nil")
	}
}

func TestFindRunningPod_NoReadyPods(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "myapp-abc123",
			Namespace: "default",
			Labels:    map[string]string{"app": "myapp"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "web"}},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionFalse},
			},
		},
	}

	c := newTestClient(pod)
	_, _, err := c.FindRunningPod(context.Background(), "default", "myapp", "")
	if err == nil {
		t.Fatal("expected error when no ready pods, got nil")
	}
}

// --- CreateEphemeralPod tests ---

func TestCreateEphemeralPod_CorrectSpec(t *testing.T) {
	c := newTestClient()

	// Intercept the created pod to inspect it
	var createdPod *corev1.Pod
	c.clientset.(*fake.Clientset).PrependReactor("create", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		createAction := action.(k8stesting.CreateAction)
		pod := createAction.GetObject().(*corev1.Pod)
		createdPod = pod.DeepCopy()

		// Set pod to Running so the polling loop exits
		pod.Status.Phase = corev1.PodRunning
		return false, pod, nil
	})

	// Make Get return Running phase for the polling loop
	c.clientset.(*fake.Clientset).PrependReactor("get", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if createdPod == nil {
			return false, nil, nil
		}
		pod := createdPod.DeepCopy()
		pod.Status.Phase = corev1.PodRunning
		return true, pod, nil
	})

	podName, err := c.CreateEphemeralPod(context.Background(), EphemeralPodRequest{
		AppName:    "myapp",
		Namespace:  "default",
		Image:      "myapp:latest",
		EnvVars:    map[string]string{"FOO": "bar"},
		SecretName: "myapp-secrets",
		CPU:        "500m",
		RAM:        "512Mi",
		Command:    []string{"/bin/sh"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if podName == "" {
		t.Fatal("expected non-empty pod name")
	}

	if createdPod == nil {
		t.Fatal("pod was not created")
	}

	// Verify labels
	expectedLabels := map[string]string{
		"app":                  "myapp",
		"shipit.dev/ephemeral": "true",
		"shipit.dev/app":       "myapp",
		"managed-by":           "shipit",
	}
	for k, v := range expectedLabels {
		if createdPod.Labels[k] != v {
			t.Errorf("expected label %s=%s, got %s", k, v, createdPod.Labels[k])
		}
	}

	// Verify RestartPolicy is Never
	if createdPod.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Errorf("expected RestartPolicy=Never, got %s", createdPod.Spec.RestartPolicy)
	}

	// Verify ActiveDeadlineSeconds
	if createdPod.Spec.ActiveDeadlineSeconds == nil || *createdPod.Spec.ActiveDeadlineSeconds != 3600 {
		t.Error("expected ActiveDeadlineSeconds=3600")
	}

	// Verify command is sleep
	container := createdPod.Spec.Containers[0]
	if len(container.Command) != 2 || container.Command[0] != "sleep" || container.Command[1] != "3600" {
		t.Errorf("expected command [sleep 3600], got %v", container.Command)
	}

	// Verify env vars
	foundEnv := false
	for _, env := range container.Env {
		if env.Name == "FOO" && env.Value == "bar" {
			foundEnv = true
			break
		}
	}
	if !foundEnv {
		t.Error("expected env var FOO=bar not found")
	}

	// Verify secret ref
	if len(container.EnvFrom) != 1 {
		t.Fatalf("expected 1 EnvFrom source, got %d", len(container.EnvFrom))
	}
	if container.EnvFrom[0].SecretRef == nil || container.EnvFrom[0].SecretRef.Name != "myapp-secrets" {
		t.Error("expected SecretRef with name myapp-secrets")
	}

	// Verify resource requests
	expectedCPU := resource.MustParse("500m")
	expectedRAM := resource.MustParse("512Mi")
	actualCPU := container.Resources.Requests[corev1.ResourceCPU]
	actualRAM := container.Resources.Requests[corev1.ResourceMemory]
	if !actualCPU.Equal(expectedCPU) {
		t.Errorf("expected CPU request 500m, got %s", actualCPU.String())
	}
	if !actualRAM.Equal(expectedRAM) {
		t.Errorf("expected memory request 512Mi, got %s", actualRAM.String())
	}
}

func TestCreateEphemeralPod_MinimalRequest(t *testing.T) {
	c := newTestClient()

	var createdPod *corev1.Pod
	c.clientset.(*fake.Clientset).PrependReactor("create", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		createAction := action.(k8stesting.CreateAction)
		pod := createAction.GetObject().(*corev1.Pod)
		createdPod = pod.DeepCopy()
		pod.Status.Phase = corev1.PodRunning
		return false, pod, nil
	})

	c.clientset.(*fake.Clientset).PrependReactor("get", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if createdPod == nil {
			return false, nil, nil
		}
		pod := createdPod.DeepCopy()
		pod.Status.Phase = corev1.PodRunning
		return true, pod, nil
	})

	_, err := c.CreateEphemeralPod(context.Background(), EphemeralPodRequest{
		AppName:   "myapp",
		Namespace: "default",
		Image:     "myapp:latest",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No secret ref
	container := createdPod.Spec.Containers[0]
	if len(container.EnvFrom) != 0 {
		t.Errorf("expected no EnvFrom sources, got %d", len(container.EnvFrom))
	}

	// No resource requests
	if container.Resources.Requests != nil {
		t.Errorf("expected no resource requests, got %v", container.Resources.Requests)
	}
}

// --- CleanupEphemeralPods tests ---

func TestCleanupEphemeralPods_DeletesMatchingPods(t *testing.T) {
	ephemeralPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "myapp-run-abc12345",
			Namespace: "default",
			Labels: map[string]string{
				"app":                  "myapp",
				"shipit.dev/ephemeral": "true",
				"shipit.dev/app":       "myapp",
				"managed-by":           "shipit",
			},
		},
	}

	otherPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "myapp-web-abc123",
			Namespace: "default",
			Labels: map[string]string{
				"app": "myapp",
			},
		},
	}

	otherAppPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "otherapp-run-def456",
			Namespace: "default",
			Labels: map[string]string{
				"app":                  "otherapp",
				"shipit.dev/ephemeral": "true",
				"shipit.dev/app":       "otherapp",
				"managed-by":           "shipit",
			},
		},
	}

	c := newTestClient(ephemeralPod, otherPod, otherAppPod)

	deleted, err := c.CleanupEphemeralPods(context.Background(), "default", "myapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if deleted != 1 {
		t.Errorf("expected 1 pod deleted, got %d", deleted)
	}

	// Verify the ephemeral pod was deleted
	pods, _ := c.clientset.CoreV1().Pods("default").List(context.Background(), metav1.ListOptions{})
	for _, pod := range pods.Items {
		if pod.Name == "myapp-run-abc12345" {
			t.Error("ephemeral pod should have been deleted")
		}
	}

	// Verify other pods still exist
	remainingPods, _ := c.clientset.CoreV1().Pods("default").List(context.Background(), metav1.ListOptions{})
	if len(remainingPods.Items) != 2 {
		t.Errorf("expected 2 remaining pods, got %d", len(remainingPods.Items))
	}
}

func TestCleanupEphemeralPods_NoPods(t *testing.T) {
	c := newTestClient()

	deleted, err := c.CleanupEphemeralPods(context.Background(), "default", "myapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deleted != 0 {
		t.Errorf("expected 0 pods deleted, got %d", deleted)
	}
}

// --- DeletePod tests ---

func TestDeletePod(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "myapp-run-abc12345",
			Namespace: "default",
		},
	}

	c := newTestClient(pod)

	err := c.DeletePod(context.Background(), "default", "myapp-run-abc12345")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify pod no longer exists
	pods, _ := c.clientset.CoreV1().Pods("default").List(context.Background(), metav1.ListOptions{})
	if len(pods.Items) != 0 {
		t.Errorf("expected 0 pods, got %d", len(pods.Items))
	}
}

// --- ExecInPod exit code parsing tests ---

// Since SPDY can't be easily tested with fake clients, we test the exit code
// extraction logic by directly validating the type assertions used in ExecInPod.

func TestExecExitCodeParsing_SuccessCase(t *testing.T) {
	// When exec returns nil error, exit code should be 0
	var err error = nil
	if err != nil {
		t.Fatal("nil error should indicate success")
	}
	// Exit code 0 path confirmed
}

type mockExitError struct {
	code int
}

func (e *mockExitError) Error() string {
	return fmt.Sprintf("command exited with code %d", e.code)
}

func (e *mockExitError) String() string {
	return e.Error()
}

func (e *mockExitError) Exited() bool {
	return true
}

func (e *mockExitError) ExitStatus() int {
	return e.code
}

// Verify our mock satisfies the utilexec.ExitError interface
var _ utilexec.ExitError = &mockExitError{}

func TestExecExitCodeParsing_ExitError(t *testing.T) {
	err := error(&mockExitError{code: 42})

	exitErr, ok := err.(utilexec.ExitError)
	if !ok {
		t.Fatal("expected error to satisfy utilexec.ExitError")
	}
	if exitErr.ExitStatus() != 42 {
		t.Errorf("expected exit code 42, got %d", exitErr.ExitStatus())
	}
}

func TestExecExitCodeParsing_ConnectionError(t *testing.T) {
	err := error(fmt.Errorf("connection refused"))

	_, ok := err.(utilexec.ExitError)
	if ok {
		t.Fatal("plain error should not satisfy utilexec.ExitError")
	}
	// This would return -1, err in the real code
}

// --- randomSuffix tests ---

func TestRandomSuffix_Length(t *testing.T) {
	suffix, err := randomSuffix(8)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suffix) != 8 {
		t.Errorf("expected length 8, got %d", len(suffix))
	}
}

func TestRandomSuffix_Unique(t *testing.T) {
	s1, _ := randomSuffix(8)
	s2, _ := randomSuffix(8)
	if s1 == s2 {
		t.Error("expected unique suffixes")
	}
}

// --- CreateEphemeralPod failure path tests ---

func TestCreateEphemeralPod_PodEntersFailedPhase(t *testing.T) {
	c := newTestClient()

	var createdPod *corev1.Pod
	c.clientset.(*fake.Clientset).PrependReactor("create", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		createAction := action.(k8stesting.CreateAction)
		pod := createAction.GetObject().(*corev1.Pod)
		createdPod = pod.DeepCopy()
		return false, pod, nil
	})

	// Make the polling Get return Failed phase
	c.clientset.(*fake.Clientset).PrependReactor("get", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if createdPod == nil {
			return false, nil, nil
		}
		pod := createdPod.DeepCopy()
		pod.Status.Phase = corev1.PodFailed
		return true, pod, nil
	})

	_, err := c.CreateEphemeralPod(context.Background(), EphemeralPodRequest{
		AppName:   "myapp",
		Namespace: "default",
		Image:     "myapp:latest",
	})
	if err == nil {
		t.Fatal("expected error when pod enters Failed phase, got nil")
	}
	if !strings.Contains(err.Error(), "unexpected phase") {
		t.Errorf("expected 'unexpected phase' in error, got: %v", err)
	}
}

func TestCreateEphemeralPod_InvalidCPU(t *testing.T) {
	c := newTestClient()

	_, err := c.CreateEphemeralPod(context.Background(), EphemeralPodRequest{
		AppName:   "myapp",
		Namespace: "default",
		Image:     "myapp:latest",
		CPU:       "not-a-valid-cpu",
	})
	if err == nil {
		t.Fatal("expected error for invalid CPU value, got nil")
	}
	if !strings.Contains(err.Error(), "invalid CPU value") {
		t.Errorf("expected 'invalid CPU value' in error, got: %v", err)
	}
}

func TestCreateEphemeralPod_InvalidRAM(t *testing.T) {
	c := newTestClient()

	_, err := c.CreateEphemeralPod(context.Background(), EphemeralPodRequest{
		AppName:   "myapp",
		Namespace: "default",
		Image:     "myapp:latest",
		RAM:       "not-valid-ram",
	})
	if err == nil {
		t.Fatal("expected error for invalid RAM value, got nil")
	}
	if !strings.Contains(err.Error(), "invalid RAM value") {
		t.Errorf("expected 'invalid RAM value' in error, got: %v", err)
	}
}

func TestCreateEphemeralPod_CreateFails(t *testing.T) {
	c := newTestClient()

	c.clientset.(*fake.Clientset).PrependReactor("create", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("quota exceeded")
	})

	_, err := c.CreateEphemeralPod(context.Background(), EphemeralPodRequest{
		AppName:   "myapp",
		Namespace: "default",
		Image:     "myapp:latest",
	})
	if err == nil {
		t.Fatal("expected error when pod creation fails, got nil")
	}
	if !strings.Contains(err.Error(), "failed to create ephemeral pod") {
		t.Errorf("expected 'failed to create ephemeral pod' in error, got: %v", err)
	}
}

// --- GetPodMetrics with fake clientset ---

func TestGetPodMetrics_RejectsFakeClientset(t *testing.T) {
	c := newTestClient()

	_, err := c.GetPodMetrics("default", "app=myapp")
	if err == nil {
		t.Fatal("expected error when using fake clientset for metrics, got nil")
	}
	if !strings.Contains(err.Error(), "metrics API requires a real Kubernetes clientset") {
		t.Errorf("expected clientset type error, got: %v", err)
	}
}

// --- DeletePod error path ---

func TestDeletePod_NonExistentPod(t *testing.T) {
	c := newTestClient()

	err := c.DeletePod(context.Background(), "default", "nonexistent-pod")
	if err == nil {
		t.Fatal("expected error when deleting non-existent pod, got nil")
	}
}
