package api

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// lockAppDeploy must serialize concurrent holders for the same appID and
// allow different appIDs to proceed in parallel.
func TestLockAppDeploy_SerializesSameApp(t *testing.T) {
	h := &Handler{}
	const appID = "app-1"

	var active int32
	var maxConcurrent int32
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := h.lockAppDeploy(appID)
			defer unlock()
			cur := atomic.AddInt32(&active, 1)
			for {
				prev := atomic.LoadInt32(&maxConcurrent)
				if cur <= prev || atomic.CompareAndSwapInt32(&maxConcurrent, prev, cur) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			atomic.AddInt32(&active, -1)
		}()
	}
	wg.Wait()
	if maxConcurrent != 1 {
		t.Errorf("expected max 1 goroutine at a time per app, saw %d", maxConcurrent)
	}
}

func TestLockAppDeploy_DifferentAppsRunInParallel(t *testing.T) {
	h := &Handler{}

	started := make(chan struct{}, 2)
	release := make(chan struct{})

	go func() {
		unlock := h.lockAppDeploy("app-A")
		defer unlock()
		started <- struct{}{}
		<-release
	}()
	go func() {
		unlock := h.lockAppDeploy("app-B")
		defer unlock()
		started <- struct{}{}
		<-release
	}()

	// Both should reach their critical section even though neither has released.
	// If they shared a lock (or a global mutex), only one would start.
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("goroutine %d did not start — different appIDs are not parallel", i+1)
		}
	}
	close(release)
}

// Entries should be reused (not allocated fresh) for the same appID across
// calls, otherwise long-lived servers would leak mutexes in sync.Map over time.
func TestLockAppDeploy_ReusesMutexPerAppID(t *testing.T) {
	h := &Handler{}
	const appID = "app-stable"

	unlock1 := h.lockAppDeploy(appID)
	m1, _ := h.deployLocks.Load(appID)
	unlock1()

	unlock2 := h.lockAppDeploy(appID)
	m2, _ := h.deployLocks.Load(appID)
	unlock2()

	if m1 != m2 {
		t.Errorf("mutex for appID %q was replaced between calls: %p → %p", appID, m1, m2)
	}
}
