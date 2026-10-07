package niconico

import (
	"fmt"
	"testing"
	"time"
)

// Every input uses explicit time. The controller tests need no clocks, sleeps,
// HTTP servers, timers or scheduling assumptions.
func adaptiveTestStart() time.Time {
	return time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
}

func adaptiveTestWindow(a *adaptiveController, now *time.Time, service, rate time.Duration, errors int, load adaptiveLoad) {
	generation := a.generation
	for i := range adaptiveWindowSize {
		*now = now.Add(5 * time.Millisecond)
		a.observe(*now, adaptiveSample{
			Generation: generation, Service: service, RateWait: rate,
			Success: i >= errors, Overload: i < errors,
		}, load)
	}
}

func adaptiveTestHealthy(a *adaptiveController, now *time.Time, service time.Duration) {
	adaptiveTestWindow(a, now, service, 0, 0, adaptiveLoad{Pending: 32, InFlight: a.limit - 1})
}

func TestAdaptiveControllerBoundsAndHealthyGrowth(t *testing.T) {
	for _, maximum := range []int{1, 3, 8, 17, 64} {
		t.Run(time.Duration(maximum).String(), func(t *testing.T) {
			now := adaptiveTestStart()
			a := newAdaptiveController(maximum, now)
			if a.limit != min(8, maximum) {
				t.Fatalf("initial limit = %d", a.limit)
			}
			for range 40 {
				before := a.limit
				adaptiveTestHealthy(a, &now, 100*time.Millisecond)
				if a.limit < before || a.limit-before > adaptiveIncrease || a.limit > maximum {
					t.Fatalf("unsafe growth: %d -> %d, ceiling %d", before, a.limit, maximum)
				}
			}
			if a.limit != maximum || a.lastReason != "at_max" {
				t.Fatalf("did not reach maximum: limit=%d reason=%s", a.limit, a.lastReason)
			}
		})
	}
}

func TestAdaptiveControllerRequiresConsecutiveHealthyWindows(t *testing.T) {
	now := adaptiveTestStart()
	a := newAdaptiveController(32, now)
	adaptiveTestHealthy(a, &now, 100*time.Millisecond)
	if a.limit != 8 || a.healthyWindows != 1 {
		t.Fatalf("first healthy window: limit=%d credit=%d", a.limit, a.healthyWindows)
	}
	// A low-count time window must interrupt credit rather than fabricate p95.
	now = now.Add(time.Second)
	a.observe(now, adaptiveSample{Generation: a.generation, Success: true, Service: 100 * time.Millisecond}, adaptiveLoad{Pending: 32, InFlight: 7})
	if a.lastReason != "insufficient_samples" || a.healthyWindows != 0 || a.snapshot(now).LastWindow.P95Seconds != nil {
		t.Fatalf("low-count window retained credit/quantile: %+v", a.snapshot(now))
	}
	adaptiveTestHealthy(a, &now, 100*time.Millisecond)
	if a.limit != 8 {
		t.Fatal("growth before two fresh healthy windows")
	}
	adaptiveTestHealthy(a, &now, 100*time.Millisecond)
	if a.limit != 12 {
		t.Fatalf("healthy growth = %d, want 12", a.limit)
	}
}

func TestAdaptiveControllerHoldsForRateAndWorkerSupply(t *testing.T) {
	for _, tc := range []struct {
		name string
		load adaptiveLoad
		rate time.Duration
		want string
	}{
		{name: "no_pending", load: adaptiveLoad{InFlight: 7}, want: "scheduler_limited"},
		{name: "few_actual_requests", load: adaptiveLoad{Pending: 10, InFlight: 2}, want: "scheduler_limited"},
		{name: "pacing", load: adaptiveLoad{Pending: 10, InFlight: 7}, rate: 50 * time.Millisecond, want: "rate_limited"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := adaptiveTestStart()
			a := newAdaptiveController(64, now)
			for range 5 {
				adaptiveTestWindow(a, &now, 100*time.Millisecond, tc.rate, 0, tc.load)
			}
			if a.limit != 8 || a.lastReason != tc.want || a.healthyWindows != 0 {
				t.Fatalf("limit=%d reason=%s healthy=%d", a.limit, a.lastReason, a.healthyWindows)
			}
		})
	}
}

func TestAdaptiveControllerLatencyNeedsTwoWindowsAndFreezesBaseline(t *testing.T) {
	now := adaptiveTestStart()
	a := newAdaptiveController(32, now)
	adaptiveTestHealthy(a, &now, 100*time.Millisecond)
	adaptiveTestHealthy(a, &now, 200*time.Millisecond)
	if a.limit != 8 || a.baseline != 100*time.Millisecond {
		t.Fatalf("one slow window changed limit/baseline: %d, %s", a.limit, a.baseline)
	}
	adaptiveTestHealthy(a, &now, 200*time.Millisecond)
	if a.limit != 4 || a.baseline != 100*time.Millisecond || !a.recovering || a.healthyWindows != 0 {
		t.Fatalf("sustained latency did not decrease: %+v", a.snapshot(now))
	}
	adaptiveTestHealthy(a, &now, 100*time.Millisecond)
	if a.limit != 4 || a.lastReason != "recovering" {
		t.Fatal("growth without two fresh recovery windows")
	}
	adaptiveTestHealthy(a, &now, 100*time.Millisecond)
	if a.limit != 5 || a.recovering {
		t.Fatal("did not recover with fresh healthy evidence")
	}
}

func TestAdaptiveControllerLowCountInterruptsSlowWindows(t *testing.T) {
	now := adaptiveTestStart()
	a := newAdaptiveController(32, now)
	adaptiveTestHealthy(a, &now, 100*time.Millisecond)
	adaptiveTestHealthy(a, &now, 200*time.Millisecond)
	now = now.Add(time.Second)
	a.observe(now, adaptiveSample{Generation: a.generation}, adaptiveLoad{})
	adaptiveTestHealthy(a, &now, 200*time.Millisecond)
	if a.limit != 8 || a.slowWindows != 1 {
		t.Fatalf("nonconsecutive slow windows reduced limit: limit=%d slow=%d", a.limit, a.slowWindows)
	}
}

func TestAdaptiveControllerIsolatedNoiseDoesNotDecrease(t *testing.T) {
	now := adaptiveTestStart()
	a := newAdaptiveController(8, now)
	adaptiveTestHealthy(a, &now, 100*time.Millisecond)
	for i := range adaptiveWindowSize {
		now = now.Add(5 * time.Millisecond)
		service := 100 * time.Millisecond
		if i == 0 {
			service = 30 * time.Second
		}
		a.observe(now, adaptiveSample{Generation: a.generation, Service: service, Success: i != 1, Overload: i == 1}, adaptiveLoad{Pending: 1, InFlight: 7})
	}
	window := a.snapshot(now).LastWindow
	if a.limit != 8 || a.baseline != 100*time.Millisecond || window.P95Seconds == nil || *window.P95Seconds != 0.1 {
		t.Fatalf("isolated noise changed control: %+v", a.snapshot(now))
	}
	if a.healthyWindows != 0 {
		t.Fatal("an errored window earned healthy credit")
	}
}

func TestAdaptiveControllerEligibleRetryPressure(t *testing.T) {
	for _, errors := range []int{1, 4, 6, 7, 64} {
		now := adaptiveTestStart()
		a := newAdaptiveController(32, now)
		adaptiveTestWindow(a, &now, 100*time.Millisecond, 0, errors, adaptiveLoad{Pending: 10, InFlight: 7})
		want := 8
		if errors >= 4 && errors*10 >= adaptiveWindowSize {
			want = 4
		}
		if a.limit != want {
			t.Fatalf("errors=%d: limit=%d want=%d", errors, a.limit, want)
		}
	}
	// Neutral completions cannot dilute eligible errors or supply successes.
	now := adaptiveTestStart()
	a := newAdaptiveController(32, now)
	for i := range adaptiveWindowSize {
		now = now.Add(5 * time.Millisecond)
		a.observe(now, adaptiveSample{Generation: a.generation, Success: i >= 4 && i < 20, Overload: i < 4}, adaptiveLoad{})
	}
	// Twenty eligible samples qualify at the time boundary even though the
	// count window is not full; neutral completions do not close it early.
	now = adaptiveTestStart().Add(adaptiveWindowDuration)
	a.observe(now, adaptiveSample{Generation: a.generation}, adaptiveLoad{})
	if a.limit != 4 || a.lastWindow.eligible != 20 || a.lastWindow.successes != 16 {
		t.Fatalf("incorrect eligible population: %+v", a.snapshot(now))
	}
}

func TestAdaptiveController429AfterGrowthReducesCurrentLimit(t *testing.T) {
	now := adaptiveTestStart()
	a := newAdaptiveController(32, now)
	oldGeneration := a.generation
	adaptiveTestHealthy(a, &now, 100*time.Millisecond)
	adaptiveTestHealthy(a, &now, 100*time.Millisecond)
	if a.limit != 12 || a.generation != oldGeneration+1 {
		t.Fatalf("healthy growth setup: %+v", a.snapshot(now))
	}
	grownGeneration := a.generation
	a.throttle(now, oldGeneration, time.Second)
	if a.limit != 6 || a.generation != grownGeneration+1 || a.decreases != 1 {
		t.Fatalf("first old-generation 429 did not halve the grown limit: %+v", a.snapshot(now))
	}
	// Both generations could have outstanding work when the decrease occurred.
	// Neither may decrease again, even though only the older one triggered it.
	for i, generation := range []uint64{oldGeneration, grownGeneration} {
		a.throttle(now.Add(time.Duration(i+1)*time.Millisecond), generation, 2*time.Second)
	}
	if a.limit != 6 || a.generation != grownGeneration+1 || a.decreases != 1 ||
		!a.cooldownUntil.Equal(now.Add(2002*time.Millisecond)) {
		t.Fatalf("pre-decrease generations compounded/shortened the pause: %+v", a.snapshot(now))
	}
	now = a.readyAt()
	a.dispatched(now)
	postRecoveryGeneration := a.generation
	a.throttle(now.Add(time.Millisecond), postRecoveryGeneration, 0)
	if a.limit != 3 || a.generation != postRecoveryGeneration+1 || a.decreases != 2 ||
		!a.cooldownUntil.Equal(now.Add(101*time.Millisecond)) {
		t.Fatalf("fresh post-recovery 429 did not start a new episode: %+v", a.snapshot(now))
	}
}

func TestAdaptiveController429AfterRecoveryGrowthReducesCurrentLimit(t *testing.T) {
	now := adaptiveTestStart()
	a := newAdaptiveController(32, now)
	a.throttle(now, a.generation, time.Second)
	now = a.readyAt()
	a.dispatched(now)
	postRecoveryGeneration := a.generation
	adaptiveTestHealthy(a, &now, 100*time.Millisecond)
	adaptiveTestHealthy(a, &now, 100*time.Millisecond)
	if a.limit != 5 || a.generation != postRecoveryGeneration+1 {
		t.Fatalf("recovery growth setup: %+v", a.snapshot(now))
	}
	grownGeneration := a.generation
	// Growth must not move the pressure cutoff after an earlier decrease either.
	a.throttle(now, postRecoveryGeneration, time.Second)
	if a.limit != 2 || a.generation != grownGeneration+1 || a.decreases != 2 {
		t.Fatalf("growth hid new pressure after recovery: %+v", a.snapshot(now))
	}
	a.throttle(now.Add(time.Millisecond), grownGeneration, 2*time.Second)
	if a.limit != 2 || a.generation != grownGeneration+1 || a.decreases != 2 ||
		!a.cooldownUntil.Equal(now.Add(2001*time.Millisecond)) {
		t.Fatalf("recovery-growth duplicate was not deduplicated: %+v", a.snapshot(now))
	}
}

func TestAdaptiveController429AfterOrdinaryDecreaseOnlyExtendsPause(t *testing.T) {
	for _, reason := range []string{"latency", "retry_pressure"} {
		t.Run(reason, func(t *testing.T) {
			now := adaptiveTestStart()
			a := newAdaptiveController(32, now)
			oldGeneration := a.generation
			adaptiveTestHealthy(a, &now, 100*time.Millisecond)
			adaptiveTestHealthy(a, &now, 100*time.Millisecond)
			grownGeneration := a.generation
			if reason == "latency" {
				adaptiveTestHealthy(a, &now, 200*time.Millisecond)
				adaptiveTestHealthy(a, &now, 200*time.Millisecond)
			} else {
				adaptiveTestWindow(a, &now, 100*time.Millisecond, 0, 4, adaptiveLoad{Pending: 32, InFlight: a.limit - 1})
			}
			if a.limit != 6 || a.generation != grownGeneration+1 || a.decreases != 1 || a.lastReason != reason {
				t.Fatalf("ordinary decrease setup: %+v", a.snapshot(now))
			}
			for i, generation := range []uint64{oldGeneration, grownGeneration} {
				a.throttle(now.Add(time.Duration(i+1)*time.Millisecond), generation, time.Second)
			}
			if a.limit != 6 || a.generation != grownGeneration+1 || a.decreases != 1 ||
				!a.cooldownUntil.Equal(now.Add(1002*time.Millisecond)) {
				t.Fatalf("old 429 compounded ordinary decrease: %+v", a.snapshot(now))
			}
			// A response dispatched after the ordinary decrease may still reduce.
			now = a.readyAt()
			a.dispatched(now)
			a.throttle(now, a.generation, time.Second)
			if a.limit != 3 || a.generation != grownGeneration+2 || a.decreases != 2 {
				t.Fatalf("ordinary decrease hid fresh 429: %+v", a.snapshot(now))
			}
		})
	}
}

func TestAdaptiveController429DeduplicationExtensionAndRecoveryPacing(t *testing.T) {
	now := adaptiveTestStart()
	a := newAdaptiveController(32, now)
	oldGeneration := a.generation
	a.throttle(now, oldGeneration, time.Second)
	if a.limit != 4 || a.generation != oldGeneration+1 || !a.readyAt().Equal(now.Add(time.Second)) {
		t.Fatalf("first 429: %+v", a.snapshot(now))
	}
	a.throttle(now.Add(50*time.Millisecond), oldGeneration, 2*time.Second)
	// No request can carry the new generation until dispatch resumes.
	a.throttle(now.Add(60*time.Millisecond), oldGeneration, time.Millisecond)
	if a.limit != 4 || a.decreases != 1 || !a.cooldownUntil.Equal(now.Add(2050*time.Millisecond)) {
		t.Fatalf("duplicate 429 compounded/shortened pause: %+v", a.snapshot(now))
	}
	a.dispatched(now.Add(time.Second))
	if a.generation != oldGeneration+1 || a.recoveryStarts != 4 ||
		!a.readyAt().Equal(now.Add(2050*time.Millisecond)) {
		t.Fatal("dispatch before reopening modified generation/pacing")
	}
	oldGeneration = a.generation
	now = a.readyAt()
	a.dispatched(now)
	if !a.readyAt().Equal(now.Add(25*time.Millisecond)) || a.recoveryStarts != 3 {
		t.Fatalf("missing initial recovery pacing: %+v", a.snapshot(now))
	}
	// A fresh post-pause request can reveal a new congestion episode.
	a.throttle(now.Add(time.Millisecond), oldGeneration, 0)
	if a.limit != 2 || a.generation != oldGeneration+1 || a.decreases != 2 {
		t.Fatalf("fresh episode did not reduce: %+v", a.snapshot(now))
	}
	if !a.cooldownUntil.Equal(now.Add(101 * time.Millisecond)) {
		t.Fatal("missing fallback pause")
	}
}

func TestAdaptiveController429FloorInvalidatesAndStaleSamplesAreNeutral(t *testing.T) {
	now := adaptiveTestStart()
	a := newAdaptiveController(1, now)
	a.throttle(now, 0, 0)
	if a.limit != 1 || a.generation != 1 || a.decreases != 0 {
		t.Fatalf("floor event must advance generation only: %+v", a.snapshot(now))
	}
	for range 128 {
		now = now.Add(10 * time.Millisecond)
		a.observe(now, adaptiveSample{Generation: 0, Success: true, Service: time.Millisecond}, adaptiveLoad{Pending: 10})
	}
	if a.window.completed != 0 || a.hasBaseline || a.healthyWindows != 0 || a.staleCompletions != 128 {
		t.Fatalf("stale completions earned credit: %+v", a.snapshot(now))
	}
	a.throttle(now, 0, time.Second)
	if a.generation != 1 || !a.cooldownUntil.Equal(now.Add(time.Second)) {
		t.Fatal("stale 429 did not only extend pause")
	}
	now = a.readyAt()
	a.dispatched(now)
	a.throttle(now, a.generation, time.Second)
	if a.limit != 1 || a.generation != 2 || a.decreases != 0 {
		t.Fatalf("fresh floor 429 did not invalidate again: %+v", a.snapshot(now))
	}
	a.throttle(now.Add(time.Millisecond), 1, 2*time.Second)
	if a.generation != 2 || !a.cooldownUntil.Equal(now.Add(2001*time.Millisecond)) {
		t.Fatal("second floor episode did not deduplicate/extend pause")
	}
}

func TestAdaptiveControllerCooldownDiscardsHealthyCredit(t *testing.T) {
	now := adaptiveTestStart()
	a := newAdaptiveController(32, now)
	adaptiveTestHealthy(a, &now, 100*time.Millisecond)
	a.throttle(now, a.generation, 2*time.Second)
	adaptiveTestHealthy(a, &now, 100*time.Millisecond)
	if a.healthyWindows != 0 || a.lastReason != "cooldown" || a.limit != 4 {
		t.Fatalf("cooldown earned credit: %+v", a.snapshot(now))
	}
}

func TestAdaptiveControllerRecoveryPacingIsBoundedAndFinite(t *testing.T) {
	for _, baseline := range []time.Duration{0, time.Second, 10 * time.Second} {
		now := adaptiveTestStart()
		a := newAdaptiveController(32, now)
		a.baseline = baseline
		a.throttle(now, 0, time.Second)
		for range a.limit {
			now = a.readyAt()
			a.dispatched(now)
			interval := a.readyAt().Sub(now)
			if interval < 25*time.Millisecond || interval > 250*time.Millisecond {
				t.Fatalf("pacing outside bounds: %s", interval)
			}
		}
		now = a.readyAt()
		a.dispatched(now)
		if a.recoveryStarts != 0 || a.readyAt().After(now) {
			t.Fatal("recovery ramp never ended")
		}
	}
}

func TestAdaptiveControllerGenuineLatencyStepRebaselinesAtFloor(t *testing.T) {
	now := adaptiveTestStart()
	a := newAdaptiveController(32, now)
	adaptiveTestHealthy(a, &now, 100*time.Millisecond)
	for range 6 {
		adaptiveTestHealthy(a, &now, 300*time.Millisecond)
	}
	if a.limit != 1 || a.baseline != 100*time.Millisecond {
		t.Fatalf("did not drain to floor with frozen baseline: %+v", a.snapshot(now))
	}
	for range 3 {
		adaptiveTestHealthy(a, &now, 300*time.Millisecond)
	}
	if a.baselineResets != 0 {
		t.Fatal("rebaselined without four stable floor windows")
	}
	adaptiveTestHealthy(a, &now, 300*time.Millisecond)
	if a.baseline != 300*time.Millisecond || a.baselineResets != 1 || a.lastReason != "baseline_reset" {
		t.Fatalf("stable floor did not rebaseline: %+v", a.snapshot(now))
	}
	adaptiveTestHealthy(a, &now, 300*time.Millisecond)
	adaptiveTestHealthy(a, &now, 300*time.Millisecond)
	if a.limit != 2 {
		t.Fatalf("permanently stuck after genuine latency step: %d", a.limit)
	}
}

func TestAdaptiveControllerUnstableFloorCannotRebaseline(t *testing.T) {
	now := adaptiveTestStart()
	a := newAdaptiveController(1, now)
	adaptiveTestHealthy(a, &now, 100*time.Millisecond)
	for i := range 10 {
		adaptiveTestHealthy(a, &now, time.Duration(300+100*(i%2))*time.Millisecond)
	}
	if a.baselineResets != 0 || a.baseline != 100*time.Millisecond {
		t.Fatal("unstable floor ratcheted baseline upward")
	}
}

func TestAdaptiveControllerOrdinaryDecisionInterval(t *testing.T) {
	now := adaptiveTestStart()
	a := newAdaptiveController(32, now)
	step := adaptiveDecisionInterval / time.Duration(2*adaptiveWindowSize+1)
	for range 2 * adaptiveWindowSize {
		now = now.Add(step)
		a.observe(now, adaptiveSample{Generation: a.generation, Success: true, Service: time.Millisecond}, adaptiveLoad{Pending: 10, InFlight: 7})
	}
	if a.limit != 8 || a.healthyWindows != 2 {
		t.Fatal("ordinary change violated minimum interval")
	}
	// 429 is protective and bypasses the ordinary decision interval.
	a.throttle(now, a.generation, time.Second)
	if a.limit != 4 {
		t.Fatal("429 was incorrectly held by the ordinary interval")
	}
}

func TestAdaptiveControllerSnapshotsBoundedAndDetached(t *testing.T) {
	now := adaptiveTestStart()
	a := newAdaptiveController(32, now)
	for range 50 {
		adaptiveTestHealthy(a, &now, 100*time.Millisecond)
	}
	s := a.snapshot(now)
	if len(s.Decisions) != adaptiveDecisionHistory || s.EvaluatedWindows != 50 {
		t.Fatalf("unbounded/lost history: %+v", s)
	}
	for i := 1; i < len(s.Decisions); i++ {
		if s.Decisions[i].ElapsedSeconds < s.Decisions[i-1].ElapsedSeconds {
			t.Fatal("decision ring not chronological")
		}
	}
	reason, p95, baseline := s.Decisions[0].Reason, *s.LastWindow.P95Seconds, *s.BaselineP95Seconds
	s.Decisions[0].Reason = "changed"
	*s.LastWindow.P95Seconds = 99
	*s.BaselineP95Seconds = 99
	after := a.snapshot(now)
	if after.Decisions[0].Reason != reason || *after.LastWindow.P95Seconds != p95 || *after.BaselineP95Seconds != baseline {
		t.Fatal("snapshot mutation changed controller state")
	}
}

func TestAdaptiveControllerCumulativeOutcomesIncludeStaleObservations(t *testing.T) {
	now := adaptiveTestStart()
	a := newAdaptiveController(8, now)
	for _, sample := range []adaptiveSample{
		{Generation: 0, Success: true},
		{Generation: 0, Overload: true},
		{Generation: 0},
	} {
		a.observe(now, sample, adaptiveLoad{})
	}
	a.throttle(now, 0, time.Second)
	a.observe(now, adaptiveSample{Generation: 0, Success: true}, adaptiveLoad{})
	a.observe(now, adaptiveSample{Generation: 0, Overload: true}, adaptiveLoad{})
	s := a.snapshot(now)
	if s.Attempts != 5 || s.Successes != 2 || s.OverloadErrors != 2 || s.HTTP429 != 1 || s.StaleCompletions != 2 {
		t.Fatalf("cumulative observations lost stale/neutral outcomes: %+v", s)
	}
	if s.Window.Completed != 0 {
		t.Fatal("stale outcomes entered decision window")
	}
}

func TestAdaptiveControllerSuccessQuantileThresholdAndNearestRanks(t *testing.T) {
	now := adaptiveTestStart()
	a := newAdaptiveController(8, now)
	for i := 19; i > 0; i-- {
		a.observe(now, adaptiveSample{Success: true, Service: time.Duration(i) * time.Millisecond}, adaptiveLoad{})
	}
	s := a.snapshot(now)
	if s.Window.P95Seconds != nil || s.Window.P50Seconds == nil || *s.Window.P50Seconds != 0.010 {
		t.Fatalf("insufficient-count percentiles: %+v", s.Window)
	}
	a.observe(now, adaptiveSample{Success: true, Service: 20 * time.Millisecond}, adaptiveLoad{})
	s = a.snapshot(now)
	if s.Window.P95Seconds == nil || *s.Window.P95Seconds != 0.019 || *s.Window.P50Seconds != 0.010 {
		t.Fatalf("nearest-rank percentiles: %+v", s.Window)
	}
}

func TestAdaptiveControllerRealisticSequentialFloorRecovery(t *testing.T) {
	now := adaptiveTestStart()
	a := newAdaptiveController(8, now)
	// Three separate congestion episodes bring the ceiling to one. Each has a
	// fresh dispatch after its prior pause; late responses cannot do this.
	for range 3 {
		a.throttle(now, a.generation, 100*time.Millisecond)
		now = a.readyAt()
		a.dispatched(now)
	}
	if a.limit != 1 {
		t.Fatalf("congestion setup limit=%d", a.limit)
	}
	// At concurrency one, each completion advances by the complete service
	// time. Two disjoint 20-success populations take four seconds to collect.
	for i := range 40 {
		now = now.Add(100 * time.Millisecond)
		a.observe(now, adaptiveSample{Generation: a.generation, Success: true, Service: 100 * time.Millisecond}, adaptiveLoad{Pending: 32})
		if i < 39 && a.limit != 1 {
			t.Fatalf("growth reused sparse observations after %d completions", i+1)
		}
	}
	if a.limit != 2 || a.recovering || a.evaluatedWindows != 2 {
		t.Fatalf("realistic sparse recovery failed: %+v", a.snapshot(now))
	}
}

func TestAdaptiveControllerRealisticSequentialLatencyStep(t *testing.T) {
	now := adaptiveTestStart()
	a := newAdaptiveController(1, now)
	// Establish the old reference at the actual floor completion rate.
	for range 20 {
		now = now.Add(100 * time.Millisecond)
		a.observe(now, adaptiveSample{Success: true, Service: 100 * time.Millisecond}, adaptiveLoad{Pending: 32})
	}
	if a.baseline != 100*time.Millisecond {
		t.Fatalf("floor baseline never initialized: %+v", a.snapshot(now))
	}
	// A permanent network step needs four disjoint, stable floor populations,
	// each taking six seconds. No impossible concurrent completions are used.
	for i := range 80 {
		now = now.Add(300 * time.Millisecond)
		a.observe(now, adaptiveSample{Success: true, Service: 300 * time.Millisecond}, adaptiveLoad{Pending: 32})
		if i < 79 && a.baselineResets != 0 {
			t.Fatalf("rebaseline reused evidence after %d completions", i+1)
		}
	}
	if a.baselineResets != 1 || a.baseline != 300*time.Millisecond {
		t.Fatalf("realistic network step trapped baseline: %+v", a.snapshot(now))
	}
}

func TestAdaptiveControllerSparseEvidenceExpiresAfterIdle(t *testing.T) {
	now := adaptiveTestStart()
	a := newAdaptiveController(1, now)
	for range 20 {
		now = now.Add(100 * time.Millisecond)
		a.observe(now, adaptiveSample{Success: true, Service: 100 * time.Millisecond}, adaptiveLoad{Pending: 32})
	}
	if a.healthyWindows != 1 {
		t.Fatal("missing first healthy population")
	}
	for range 5 {
		now = now.Add(100 * time.Millisecond)
		a.observe(now, adaptiveSample{Success: true, Service: 100 * time.Millisecond}, adaptiveLoad{Pending: 32})
	}
	now = now.Add(10 * time.Second)
	a.observe(now, adaptiveSample{Success: true, Service: 100 * time.Millisecond}, adaptiveLoad{Pending: 32})
	if a.window.completed != 1 || a.healthyWindows != 0 || a.lastReason != "insufficient_samples" {
		t.Fatalf("stale partial evidence survived idle: %+v", a.snapshot(now))
	}
}

func TestAdaptiveControllerThresholdCrossingAfterIdleCannotEarnCredit(t *testing.T) {
	now := adaptiveTestStart()
	a := newAdaptiveController(8, now)
	for range 3 {
		a.throttle(now, a.generation, 100*time.Millisecond)
		now = a.readyAt()
		a.dispatched(now)
	}
	for range 39 {
		now = now.Add(100 * time.Millisecond)
		a.observe(now, adaptiveSample{Generation: a.generation, Success: true, Service: 100 * time.Millisecond}, adaptiveLoad{Pending: 32})
	}
	if a.healthyWindows != 1 || a.window.successes != 19 {
		t.Fatalf("setup did not reach population boundary: %+v", a.snapshot(now))
	}
	now = now.Add(10 * time.Second)
	a.observe(now, adaptiveSample{Generation: a.generation, Success: true, Service: 100 * time.Millisecond}, adaptiveLoad{Pending: 32})
	if a.limit != 1 || a.healthyWindows != 0 || a.window.successes != 1 {
		t.Fatalf("new sample promoted stale population: %+v", a.snapshot(now))
	}
}

func TestAdaptiveControllerLargeContinuousLatencyStepRebaselines(t *testing.T) {
	now := adaptiveTestStart()
	a := newAdaptiveController(1, now)
	for range 20 {
		now = now.Add(100 * time.Millisecond)
		a.observe(now, adaptiveSample{Success: true, Service: 100 * time.Millisecond}, adaptiveLoad{Pending: 32})
	}
	// A valid single request can outlast the old freshness horizon. Continuous
	// five-second work must still accumulate four disjoint stable windows.
	for range 80 {
		now = now.Add(5 * time.Second)
		a.observe(now, adaptiveSample{Success: true, Service: 5 * time.Second}, adaptiveLoad{Pending: 32})
	}
	if a.baselineResets != 1 || a.baseline != 5*time.Second {
		t.Fatalf("large continuous latency step was mistaken for idle: %+v", a.snapshot(now))
	}
}

func TestAdaptiveControllerSlowParallelBatchesStillDecrease(t *testing.T) {
	for _, tc := range []struct{ maximum, batches int }{{8, 8}, {64, 2}} {
		t.Run(fmt.Sprint(tc.maximum), func(t *testing.T) {
			now := adaptiveTestStart()
			a := newAdaptiveController(tc.maximum, now)
			for !a.hasBaseline || a.limit < tc.maximum {
				adaptiveTestHealthy(a, &now, 100*time.Millisecond)
			}
			// Synchronized batches leave an empty window for a complete service
			// duration. That gap must preserve sustained slow evidence.
			for range tc.batches {
				now = now.Add(2 * time.Second)
				generation := a.generation
				for remaining := tc.maximum - 1; remaining >= 0; remaining-- {
					a.observe(now, adaptiveSample{Generation: generation, Success: true, Service: 2 * time.Second}, adaptiveLoad{Pending: 2 * tc.maximum, InFlight: remaining})
				}
			}
			if a.limit >= tc.maximum || a.baseline != 100*time.Millisecond {
				t.Fatalf("continuous slow batches did not reduce against old baseline: %+v", a.snapshot(now))
			}
		})
	}
}

func TestAdaptiveControllerNeutralOutcomesPreserveEligibleEvidence(t *testing.T) {
	for _, overload := range []bool{false, true} {
		t.Run(fmt.Sprintf("overload=%t", overload), func(t *testing.T) {
			now := adaptiveTestStart()
			a := newAdaptiveController(32, now)
			windows := 2
			if overload {
				windows = 1
			}
			for range windows {
				generation := a.generation
				for range adaptiveWindowSize {
					now = now.Add(5 * time.Millisecond)
					a.observe(now, adaptiveSample{Generation: generation}, adaptiveLoad{Pending: 32, InFlight: a.limit - 1})
					now = now.Add(5 * time.Millisecond)
					a.observe(now, adaptiveSample{Generation: generation, Success: !overload, Overload: overload, Service: 10 * time.Millisecond}, adaptiveLoad{Pending: 32, InFlight: a.limit - 1})
				}
			}
			if a.lastWindow.completed != 2*adaptiveWindowSize || a.lastWindow.eligible != adaptiveWindowSize {
				t.Fatalf("neutral samples erased evidence: %+v", a.snapshot(now))
			}
			if overload {
				if a.limit != 4 || a.lastReason != "retry_pressure" {
					t.Fatalf("overload evidence ignored: %+v", a.snapshot(now))
				}
			} else {
				if a.limit != 12 || a.lastWindow.successes != adaptiveWindowSize {
					t.Fatalf("healthy evidence ignored: %+v", a.snapshot(now))
				}
			}
		})
	}
}

func TestAdaptiveControllerWithoutConfiguredMaximum(t *testing.T) {
	now := adaptiveTestStart()
	a := newAdaptiveController(0, now)
	if a.limit != 8 || a.maximum != 0 {
		t.Fatalf("initial limit=%d maximum=%d", a.limit, a.maximum)
	}
	for range 64 {
		adaptiveTestHealthy(a, &now, 100*time.Millisecond)
	}
	if a.limit <= 64 || a.snapshot(now).Max != 0 {
		t.Fatalf("unexpected hidden ceiling: %+v", a.snapshot(now))
	}
	before := a.limit
	generation := a.generation
	a.throttle(now, generation, time.Second)
	if a.limit != before/2 || a.maximum != 0 || !a.readyAt().After(now) {
		t.Fatalf("unlimited mode lost protection: %+v", a.snapshot(now))
	}
	a.throttle(now, generation, 2*time.Second)
	if a.limit != before/2 {
		t.Fatal("same congestion epoch reduced twice")
	}
	if len(a.snapshot(now).Decisions) > adaptiveDecisionHistory {
		t.Fatal("decision history grew without bound")
	}
}

func TestAdaptiveControllerIntegerHeadroom(t *testing.T) {
	const maxInt = int(^uint(0) >> 1)
	for _, maximum := range []int{0, maxInt} {
		now := adaptiveTestStart()
		a := newAdaptiveController(maximum, now)
		a.limit = maxInt - 2
		adaptiveTestHealthy(a, &now, 100*time.Millisecond)
		adaptiveTestHealthy(a, &now, 100*time.Millisecond)
		if a.limit != maxInt {
			t.Fatalf("limit=%d want=%d", a.limit, maxInt)
		}
		generation, increases := a.generation, a.increases
		adaptiveTestHealthy(a, &now, 100*time.Millisecond)
		adaptiveTestHealthy(a, &now, 100*time.Millisecond)
		if a.limit != maxInt || a.generation != generation || a.increases != increases {
			t.Fatal("integer boundary wrapped or recorded spurious growth")
		}
		want := "integer_limit"
		if maximum > 0 {
			want = "at_max"
		}
		if a.lastReason != want {
			t.Fatalf("reason=%s want=%s", a.lastReason, want)
		}
	}
	var w adaptiveWindow
	w.add(adaptiveSample{Success: true}, adaptiveLoad{InFlight: maxInt}, maxInt)
	if w.snapshot().Utilization != 1 {
		t.Fatal("utilization overflowed")
	}
}
