package k8s

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ErrRolloutFailed is returned from WatchRollout when kube reports the
// Deployment's Progressing condition as False with ProgressDeadlineExceeded.
// Callers can use errors.Is to branch on "rollout failed fast" vs "watch
// timed out waiting for readiness".
var ErrRolloutFailed = errors.New("rollout failed")

const defaultProgressDeadline = 600 * time.Second

// WatchRollout polls the named Deployment every 2s until either:
//   - all updated replicas are ready and available (returns nil)
//   - the Progressing condition flips to False/ProgressDeadlineExceeded
//     (returns an error wrapping ErrRolloutFailed)
//   - ctx is cancelled or deadlines (returns ctx.Err())
//
// We poll instead of using a watch because the fake clientset in tests does
// not emit Modify events for Status updates made via Update(). Polling keeps
// the implementation identical in tests and production.
func (c *Client) WatchRollout(ctx context.Context, name, namespace string) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		dep, err := c.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			// ctx errors (cancel/deadline) surface here via the informer; treat
			// any Get error as fatal for this watch — the deploy will re-enter
			// on next invocation.
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("watch rollout: get deployment: %w", err)
		}

		if rolloutReady(dep) {
			return nil
		}
		if reason, failed := rolloutFailed(dep); failed {
			return fmt.Errorf("%w: %s", ErrRolloutFailed, reason)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// rolloutReady mirrors `kubectl rollout status`: new ReplicaSet has been
// observed by the controller and every desired replica is updated, ready,
// and available.
func rolloutReady(d *appsv1.Deployment) bool {
	if d.Spec.Replicas == nil {
		return false
	}
	if d.Status.ObservedGeneration < d.Generation {
		return false
	}
	desired := *d.Spec.Replicas
	return d.Status.UpdatedReplicas == desired &&
		d.Status.ReadyReplicas == desired &&
		d.Status.AvailableReplicas == desired
}

// rolloutFailed returns (reason, true) when the Deployment's Progressing
// condition is False with ProgressDeadlineExceeded — kube's canonical signal
// that a rollout won't complete on its own.
func rolloutFailed(d *appsv1.Deployment) (string, bool) {
	for _, cond := range d.Status.Conditions {
		if cond.Type != appsv1.DeploymentProgressing {
			continue
		}
		if cond.Status == "False" && cond.Reason == "ProgressDeadlineExceeded" {
			return cond.Message, true
		}
	}
	return "", false
}

// progressDeadline returns the Deployment's configured progress deadline,
// falling back to 600s when unset. Called by deployApp to bound WatchRollout.
func progressDeadline(d *appsv1.Deployment) time.Duration {
	if d == nil || d.Spec.ProgressDeadlineSeconds == nil {
		return defaultProgressDeadline
	}
	return time.Duration(*d.Spec.ProgressDeadlineSeconds) * time.Second
}

// DeploymentProgressDeadline returns the progress deadline for the live
// Deployment. Used by deployApp to size the watch context without duplicating
// the nil-fallback logic. Returns defaultProgressDeadline if the Deployment
// is missing (the deploy that just created it hasn't been observed yet — the
// caller will hit the same default via the nil path anyway).
func (c *Client) DeploymentProgressDeadline(ctx context.Context, name, namespace string) time.Duration {
	dep, err := c.clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		log.Printf("rollout: progress deadline lookup failed app=%s ns=%s err=%v — using default %s", name, namespace, err, defaultProgressDeadline)
		return defaultProgressDeadline
	}
	return progressDeadline(dep)
}
