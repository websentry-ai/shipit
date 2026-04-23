package k8s

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func readyDeployment(name, ns string, replicas int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Generation: 1},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 1,
			Replicas:           replicas,
			UpdatedReplicas:    replicas,
			ReadyReplicas:      replicas,
			AvailableReplicas:  replicas,
		},
	}
}

func laggingDeployment(name, ns string, replicas int32) *appsv1.Deployment {
	// Generation bumped to 2 but controller hasn't observed it yet → not ready.
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Generation: 2},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 1,
			Replicas:           replicas,
			UpdatedReplicas:    0,
			ReadyReplicas:      0,
			AvailableReplicas:  0,
		},
	}
}

func failedDeployment(name, ns string, replicas int32, msg string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Generation: 1},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 1,
			Conditions: []appsv1.DeploymentCondition{
				{
					Type:    appsv1.DeploymentProgressing,
					Status:  "False",
					Reason:  "ProgressDeadlineExceeded",
					Message: msg,
				},
			},
		},
	}
}

func TestWatchRollout_HappyPath(t *testing.T) {
	c := newTestClient(readyDeployment("svc", "default", 3))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.WatchRollout(ctx, "svc", "default"); err != nil {
		t.Fatalf("expected nil error on ready deployment, got %v", err)
	}
}

func TestWatchRollout_GenerationLagTimesOut(t *testing.T) {
	// Deployment generation 2 but observed at 1 → never ready within ctx.
	c := newTestClient(laggingDeployment("svc", "default", 3))

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := c.WatchRollout(ctx, "svc", "default")
	if err == nil {
		t.Fatal("expected ctx deadline error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}

func TestWatchRollout_ProgressDeadlineExceededFailsFast(t *testing.T) {
	c := newTestClient(failedDeployment("svc", "default", 3, "pod stuck ImagePullBackOff"))
	// Long timeout — fast-fail should return before it elapses.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	err := c.WatchRollout(ctx, "svc", "default")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected rollout-failed error, got nil")
	}
	if !strings.HasPrefix(err.Error(), "rollout failed:") {
		t.Errorf("expected 'rollout failed:' prefix in err, got %v", err)
	}
	if !strings.Contains(err.Error(), "pod stuck ImagePullBackOff") {
		t.Errorf("expected condition message in err, got %v", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("fast-fail took too long: %v", elapsed)
	}
}

func TestWatchRollout_CtxCancelledExternally(t *testing.T) {
	c := newTestClient(laggingDeployment("svc", "default", 3))

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel before the first select hits the ticker.
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := c.WatchRollout(ctx, "svc", "default")
	if err == nil {
		t.Fatal("expected cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// Persistent Get errors should NOT surface as a typed error — the watch
// keeps retrying until ctx.Done(), which is what eventually fires. This
// models an apiserver that's down for the whole progressDeadline window.
func TestWatchRollout_PersistentGetErrorHitsCtxTimeout(t *testing.T) {
	c := newTestClient()
	c.clientset.(*fake.Clientset).PrependReactor("get", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("api server down")
	})
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := c.WatchRollout(ctx, "svc", "default")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected ctx deadline exceeded, got %v", err)
	}
}

// Regression guard for Greptile P1 (comment 3106354508): a single transient
// Get error must not trigger rollback. The watch should log the error, wait
// for the next tick, and succeed when the deployment becomes ready.
func TestWatchRollout_TransientGetErrorRecovers(t *testing.T) {
	dep := readyDeployment("svc", "default", 2)
	c := newTestClient(dep)

	var calls int32
	c.clientset.(*fake.Clientset).PrependReactor("get", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		// Fail the first Get, let subsequent Gets fall through to the fake
		// store which has a ready deployment.
		if atomic.AddInt32(&calls, 1) == 1 {
			return true, nil, errors.New("transient etcd leader election")
		}
		return false, nil, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := c.WatchRollout(ctx, "svc", "default")
	if err != nil {
		t.Errorf("transient Get error should not surface, got %v", err)
	}
	if atomic.LoadInt32(&calls) < 2 {
		t.Errorf("expected at least 2 Get calls (retry after transient), got %d", calls)
	}
}

func TestProgressDeadline_UsesDefaultWhenUnset(t *testing.T) {
	d := &appsv1.Deployment{}
	if got := progressDeadline(d); got != defaultProgressDeadline {
		t.Errorf("got %v, want %v", got, defaultProgressDeadline)
	}
}

func TestProgressDeadline_ReadsSpecValue(t *testing.T) {
	secs := int32(120)
	d := &appsv1.Deployment{Spec: appsv1.DeploymentSpec{ProgressDeadlineSeconds: &secs}}
	if got := progressDeadline(d); got != 120*time.Second {
		t.Errorf("got %v, want 120s", got)
	}
}
