package niconico

import (
	"context"
	"errors"
	"net/http"
	"runtime"
	"strconv"
	"testing"
	"time"
)

func waitForWorkerPending(t *testing.T, c *HTTPControl, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c.mu.Lock()
		got := c.workerPending
		c.mu.Unlock()
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("waiting workers = %d, want %d", got, want)
		}
		runtime.Gosched()
	}
}

func waitForWorkerResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("worker admission did not finish")
		return nil
	}
}

func setAdaptiveWorkerBudget(c *HTTPControl, limit int) {
	c.mu.Lock()
	previous := c.adaptive.limit
	c.adaptive.limit = limit
	c.updateAdaptiveLimitLocked(previous)
	c.mu.Unlock()
}

func TestWorkerGateFixedLimitReleaseAndCancellation(t *testing.T) {
	c := NewAdaptiveConcurrencyControl(0, true, false)
	gate := c.NewWorkerGate(2)
	for range 2 {
		if err := gate.Acquire(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	result := make(chan error, 1)
	go func() { result <- gate.Acquire(ctx) }()
	waitForWorkerPending(t, c, 1)
	// Resizing automatic workers must not override an explicitly fixed gate.
	setAdaptiveWorkerBudget(c, 16)
	c.mu.Lock()
	active, pending := gate.active, c.workerPending
	c.mu.Unlock()
	if active != 2 || pending != 1 {
		t.Fatalf("fixed limit changed: active=%d pending=%d", active, pending)
	}
	cancel()
	if err := waitForWorkerResult(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
	waitForWorkerPending(t, c, 0)
	if err := gate.Acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled admission = %v", err)
	}
	go func() { result <- gate.Acquire(context.Background()) }()
	waitForWorkerPending(t, c, 1)
	gate.Release()
	if err := waitForWorkerResult(t, result); err != nil {
		t.Fatal(err)
	}
	gate.Release()
	gate.Release()
	c.mu.Lock()
	defer c.mu.Unlock()
	if gate.active != 0 || c.workerPending != 0 {
		t.Fatalf("leaked worker: active=%d pending=%d", gate.active, c.workerPending)
	}
}

func TestWorkerGateAdaptiveExpansionWakesWaitingWorker(t *testing.T) {
	c := NewAdaptiveConcurrencyControl(0, true, false)
	setAdaptiveWorkerBudget(c, 1)
	gate := c.NewWorkerGate(0)
	if err := gate.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	result := make(chan error, 1)
	go func() { result <- gate.Acquire(ctx) }()
	waitForWorkerPending(t, c, 1)
	setAdaptiveWorkerBudget(c, 3)
	if err := waitForWorkerResult(t, result); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		gate.Release()
	}
	waitForWorkerPending(t, c, 0)
	if got := c.Snapshot().Admission.Limit; got != 0 {
		t.Fatalf("worker resize changed explicit unlimited HTTP admission: %d", got)
	}
}

func TestWorkerGateAdaptiveShrinkDrainsRunningWorkers(t *testing.T) {
	for _, httpFixed := range []bool{false, true} {
		t.Run("httpFixed="+strconv.FormatBool(httpFixed), func(t *testing.T) {
			c := NewAdaptiveConcurrencyControl(0, httpFixed, false)
			setAdaptiveWorkerBudget(c, 4)
			gate := c.NewWorkerGate(0)
			for range 4 {
				if err := gate.Acquire(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			setAdaptiveWorkerBudget(c, 1)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			result := make(chan error, 1)
			go func() { result <- gate.Acquire(ctx) }()
			waitForWorkerPending(t, c, 1)
			for range 3 {
				gate.Release()
			}
			c.mu.Lock()
			active, pending := gate.active, c.workerPending
			c.mu.Unlock()
			if active != 1 || pending != 1 {
				t.Fatalf("worker admitted before draining: active=%d pending=%d", active, pending)
			}
			gate.Release()
			if err := waitForWorkerResult(t, result); err != nil {
				t.Fatal(err)
			}
			gate.Release()
			waitForWorkerPending(t, c, 0)
			if httpFixed && c.Snapshot().Admission.Limit != 0 {
				t.Fatal("worker shrink changed explicit unlimited HTTP admission")
			}
		})
	}
}

func TestWorkerGateUnlimitedBudgetDoesNotOverflow(t *testing.T) {
	c := NewAdaptiveConcurrencyControl(0, false, false)
	maxInt := int(^uint(0) >> 1)
	setAdaptiveWorkerBudget(c, maxInt)
	gate := c.NewWorkerGate(0)
	c.mu.Lock()
	limit := gate.limitLocked()
	c.mu.Unlock()
	if limit != maxInt {
		t.Fatalf("worker budget overflowed: %d", limit)
	}
	if err := gate.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	gate.Release()
}

func TestAdaptiveConcurrencyFixedUnlimitedHTTPDispatch(t *testing.T) {
	c := NewAdaptiveConcurrencyControl(0, true, false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var attempts []adaptiveAttempt
	for range 12 {
		if err := c.acquire(ctx); err != nil {
			t.Fatal(err)
		}
		attempt, ready, err := c.adaptiveDispatch(ctx, 0, c.pauseEpoch)
		if err != nil || !ready {
			t.Fatalf("explicit unlimited HTTP refused concurrent attempt %d: ready=%v error=%v", len(attempts)+1, ready, err)
		}
		attempts = append(attempts, attempt)
	}
	if got := c.Snapshot(); got.Admission.Limit != 0 || got.Admission.Reserved != 12 || got.Adaptive.InFlight != 12 {
		t.Fatalf("explicit unlimited HTTP snapshot: %+v", got)
	}
	for i := range attempts {
		c.finishAdaptive(&attempts[i], ctx, http.StatusOK, nil, nil, true, nil)
	}
	if got := c.Snapshot(); got.Admission.Limit != 0 || got.Admission.Reserved != 0 || got.Adaptive.InFlight != 0 {
		t.Fatalf("HTTP attempts did not drain: %+v", got)
	}
}

func TestAdaptiveConcurrencyWorkerDemandControlsFeedbackGrowth(t *testing.T) {
	for _, initial := range []int{1, 8} {
		for _, demand := range []bool{false, true} {
			name := "no waiting work"
			if demand {
				name = "waiting worker"
			}
			t.Run(strconv.Itoa(initial)+"/"+name, func(t *testing.T) {
				clock := useAdaptiveTestClock(t)
				c := NewAdaptiveConcurrencyControl(0, true, false)
				setAdaptiveWorkerBudget(c, initial)
				ctx, cancel := context.WithCancel(context.Background())
				t.Cleanup(cancel)
				gate := c.NewWorkerGate(0)
				var waiting <-chan error
				if demand {
					for range initial {
						if err := gate.Acquire(ctx); err != nil {
							t.Fatal(err)
						}
					}
					result := make(chan error, 1)
					waiting = result
					go func() { result <- gate.Acquire(ctx) }()
					waitForWorkerPending(t, c, 1)
				}
				// Keep peer attempts in flight while observing overlapping 100ms
				// completions at a deterministic 5ms completion cadence.
				var held []adaptiveAttempt
				for range initial - 1 {
					if err := c.acquire(ctx); err != nil {
						t.Fatal(err)
					}
					attempt, ready, err := c.adaptiveDispatch(ctx, 0, c.pauseEpoch)
					if err != nil || !ready {
						t.Fatalf("peer dispatch: ready=%v error=%v", ready, err)
					}
					held = append(held, attempt)
				}
				for range 2 * adaptiveWindowSize {
					if err := c.acquire(ctx); err != nil {
						t.Fatal(err)
					}
					attempt, ready, err := c.adaptiveDispatch(ctx, 0, c.pauseEpoch)
					if err != nil || !ready {
						t.Fatalf("sample dispatch: ready=%v error=%v", ready, err)
					}
					clock.Advance(5 * time.Millisecond)
					attempt.started = clock.Now().Add(-100 * time.Millisecond)
					c.finishAdaptive(&attempt, ctx, http.StatusOK, nil, nil, true, nil)
				}
				got := c.Snapshot()
				wantBudget := initial
				if demand {
					wantBudget += 1
					if initial >= 8 {
						wantBudget = initial + adaptiveIncrease
					}
				}
				if got.Admission.Limit != 0 || got.Adaptive.Current != wantBudget {
					t.Fatalf("feedback growth with demand=%v: HTTP=%d worker budget=%d, want %d", demand, got.Admission.Limit, got.Adaptive.Current, wantBudget)
				}
				if demand {
					if err := waitForWorkerResult(t, waiting); err != nil {
						t.Fatal(err)
					}
					for range initial + 1 {
						gate.Release()
					}
				} else if got.Adaptive.LastReason != "scheduler_limited" {
					t.Fatalf("growth without waiting work: %+v", got.Adaptive)
				}
				for i := range held {
					c.finishAdaptive(&held[i], ctx, http.StatusOK, nil, nil, true, nil)
				}
				waitForWorkerPending(t, c, 0)
			})
		}
	}
}

func TestAdaptiveConcurrencyFixedHTTPKeepsExplicitLimitOnFeedback(t *testing.T) {
	for _, limit := range []int{0, 3, 8, 16} {
		t.Run(strconv.Itoa(limit), func(t *testing.T) {
			useAdaptiveTestClock(t)
			c := NewAdaptiveConcurrencyControl(limit, true, false)
			before := c.Snapshot().Adaptive.Current
			c.throttleAdaptive(&adaptiveAttempt{generation: c.adaptive.generation}, time.Second)
			got := c.Snapshot()
			if got.Admission.Limit != limit || got.Adaptive.Current != max(1, before/2) {
				t.Fatalf("HTTP throttle overrode explicit admission or missed worker feedback: %+v", got)
			}
			setAdaptiveWorkerBudget(c, before)
			if got := c.Snapshot().Admission.Limit; got != limit {
				t.Fatalf("worker increase changed explicit HTTP limit: %d, want %d", got, limit)
			}
		})
	}
}
