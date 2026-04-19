package k8s

import (
	"context"
	"fmt"
	"log"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const defaultProgressDeadline = 600 * time.Second

// WatchRollout polls the named Deployment every 2s until either:
//   - all updated replicas are ready and available (returns nil)
//   - the Progressing condition flips to False/ProgressDeadlineExceeded
//     (returns an error with the kube-provided reason)
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
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("watch rollout: get deployment: %w", err)
		}

		if rolloutReady(dep) {
			return nil
		}
		if reason, failed := rolloutFailed(dep); failed {
			return fmt.Errorf("rollout failed: %s", reason)
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
//
// Edge case — a Deployment scaled to 0 replicas is trivially ready once the
// controller has observed the generation: there are no pods to wait for.
// This matches kube semantics and `kubectl rollout status` behavior. If
// shipit ever allows a user to create an app at replicas=0, the deploy will
// be reported as `running` immediately — which is correct: the declared
// state ("zero pods") has been fully realised.
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
		// Expected race on first-ever deploy (Create still in flight on slow
		// etcd). Default is correct in that case; log at info only.
		log.Printf("rollout: using default progress deadline %s for app=%s ns=%s err=%v", defaultProgressDeadline, name, namespace, err)
		return defaultProgressDeadline
	}
	return progressDeadline(dep)
}
