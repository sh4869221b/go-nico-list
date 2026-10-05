package niconico

import "time"

const (
	adaptiveWindowSize       = 32
	adaptiveMinimumSamples   = 20
	adaptiveDecisionHistory  = 32
	adaptiveWindowDuration   = time.Second
	adaptiveDecisionInterval = 25 * time.Millisecond
	adaptiveIncrease         = 4
)

// adaptiveSample describes exactly one completed application attempt. Caller
// cancellation, 404 and other non-congestion outcomes have neither flag set.
// A 429 is reported separately at header receipt through throttle.
type adaptiveSample struct {
	Generation        uint64
	Service, RateWait time.Duration
	Success, Overload bool
}

// InFlight excludes the completing attempt. Reservations waiting for rate
// permission are deliberately absent from this actual HTTP utilization signal.
type adaptiveLoad struct {
	Pending, InFlight int
}

// AdaptiveWindowSnapshot describes one controller window, whose successful
// latency population is distinct from the full HTTP diagnostic population.
type AdaptiveWindowSnapshot struct {
	Completed        int      `json:"completed"`
	Eligible         int      `json:"eligible"`
	Successes        int      `json:"successes"`
	Overloads        int      `json:"overloads"`
	P50Seconds       *float64 `json:"success_p50_seconds,omitempty"`
	P95Seconds       *float64 `json:"success_p95_seconds,omitempty"`
	Utilization      float64  `json:"actual_utilization"`
	RateWaitFraction float64  `json:"rate_wait_fraction"`
}

// AdaptiveDecision is a bounded, content-free controller transition. Elapsed
// times are relative to this command; no request identifiers are retained.
type AdaptiveDecision struct {
	ElapsedSeconds float64 `json:"elapsed_seconds"`
	From           int     `json:"from"`
	To             int     `json:"to"`
	Generation     uint64  `json:"generation"`
	Reason         string  `json:"reason"`
}

// AdaptiveSnapshot is independent of optional httptrace/full metric collection.
// Window is the active partial window; LastWindow is the last evaluated window.
// Pending/InFlight are the most recently supplied completion-time load.
type AdaptiveSnapshot struct {
	Initial                  int                    `json:"initial"`
	Current                  int                    `json:"current"`
	Max                      int                    `json:"max"`
	Generation               uint64                 `json:"generation"`
	Increases                uint64                 `json:"increases"`
	Decreases                uint64                 `json:"decreases"`
	Attempts                 uint64                 `json:"attempts"`
	Successes                uint64                 `json:"successes"`
	OverloadErrors           uint64                 `json:"overload_errors"`
	HTTP429                  uint64                 `json:"http_429"`
	StaleCompletions         uint64                 `json:"stale_completions"`
	EvaluatedWindows         uint64                 `json:"evaluated_windows"`
	BaselineResets           uint64                 `json:"baseline_resets"`
	LastReason               string                 `json:"last_reason"`
	BaselineP95Seconds       *float64               `json:"baseline_p95_seconds,omitempty"`
	Window                   AdaptiveWindowSnapshot `json:"window"`
	LastWindow               AdaptiveWindowSnapshot `json:"last_window"`
	Pending                  int                    `json:"pending"`
	InFlight                 int                    `json:"in_flight"`
	CoolingDown              bool                   `json:"cooling_down"`
	CooldownRemainingSeconds float64                `json:"cooldown_remaining_seconds"`
	ReadyInSeconds           float64                `json:"ready_in_seconds"`
	Recovering               bool                   `json:"recovering"`
	RecoveryStartsRemaining  int                    `json:"recovery_starts_remaining"`
	HealthyWindows           int                    `json:"healthy_windows"`
	Decisions                []AdaptiveDecision     `json:"decisions"`
}

type adaptiveWindow struct {
	completed, eligible, successes, overloads int
	// Insertion maintains nearest-rank order without snapshot-time sorting.
	services                                [adaptiveWindowSize]time.Duration
	maxService                              time.Duration
	serviceSeconds, rateSeconds, saturation float64
}

func (w *adaptiveWindow) add(sample adaptiveSample, load adaptiveLoad, limit int) {
	w.completed++
	if !sample.Success && !sample.Overload {
		return
	}
	w.eligible++
	w.maxService = max(w.maxService, sample.Service)
	w.serviceSeconds += max(sample.Service, 0).Seconds()
	w.rateSeconds += max(sample.RateWait, 0).Seconds()
	w.saturation += min(1, (float64(max(0, load.InFlight))+1)/float64(limit))
	if sample.Overload {
		w.overloads++
		return
	}
	service := max(sample.Service, 0)
	index := w.successes
	for index > 0 && w.services[index-1] > service {
		w.services[index] = w.services[index-1]
		index--
	}
	w.services[index] = service
	w.successes++
}

func (w *adaptiveWindow) snapshot() AdaptiveWindowSnapshot {
	s := AdaptiveWindowSnapshot{
		Completed: w.completed, Eligible: w.eligible,
		Successes: w.successes, Overloads: w.overloads,
	}
	if w.successes > 0 {
		p50 := w.services[(50*w.successes+99)/100-1].Seconds()
		s.P50Seconds = &p50
	}
	if w.successes >= adaptiveMinimumSamples {
		p95 := w.p95().Seconds()
		s.P95Seconds = &p95
	}
	if w.eligible > 0 {
		s.Utilization = w.saturation / float64(w.eligible)
	}
	if total := w.serviceSeconds + w.rateSeconds; total > 0 {
		s.RateWaitFraction = w.rateSeconds / total
	}
	return s
}

func (w *adaptiveWindow) p95() time.Duration {
	return w.services[(95*w.successes+99)/100-1]
}

// adaptiveController is a pure, event-driven state machine. Its caller owns
// synchronization (HTTPControl.mu), the clock, scheduling and all HTTP work.
// It neither starts timers/goroutines nor enables the full metrics collector.
type adaptiveController struct {
	limit, initial, maximum int
	generation              uint64
	// Generations through pressureCutoff were dispatched before the latest
	// protective decrease. Healthy growth changes freshness, not this cutoff.
	pressureCutoff                            uint64
	hasPressureCutoff                         bool
	cooldownUntil, nextStartAt                time.Time
	started, windowStarted, lastChange        time.Time
	window                                    adaptiveWindow
	lastWindow                                AdaptiveWindowSnapshot
	load                                      adaptiveLoad
	baseline                                  time.Duration
	previousService                           time.Duration
	hasBaseline                               bool
	healthyWindows, slowWindows, floorWindows int
	floorP95                                  time.Duration
	recovering                                bool
	recoveryStarts                            int
	increases, decreases, http429             uint64
	attempts, successes, overloadErrors       uint64
	staleCompletions, evaluatedWindows        uint64
	baselineResets                            uint64
	lastReason                                string
	decisions                                 [adaptiveDecisionHistory]AdaptiveDecision
	decisionNext, decisionCount               int
}

func newAdaptiveController(maximum int, now time.Time) *adaptiveController {
	// Zero records the absence of a configured ceiling, not unlimited admission.
	maximum = max(0, maximum)
	initial := 8
	if maximum > 0 {
		initial = min(initial, maximum)
	}
	return &adaptiveController{
		limit: initial, initial: initial, maximum: maximum,
		started: now, windowStarted: now, lastChange: now,
		lastReason: "insufficient_samples",
	}
}

func (a *adaptiveController) observe(now time.Time, sample adaptiveSample, load adaptiveLoad) {
	a.load = load
	a.attempts++
	if sample.Overload {
		a.overloadErrors++
	} else if sample.Success {
		a.successes++
	}
	if sample.Generation != a.generation {
		a.staleCompletions++
		return
	}
	// Expire before inserting: a twentieth sample after a long idle must not
	// turn nineteen stale samples into a newly qualified healthy population.
	if now.Sub(a.windowStarted).Seconds() > a.windowFreshSeconds() {
		a.lastWindow = a.window.snapshot()
		a.evaluatedWindows++
		a.hold(now, "insufficient_samples")
		a.resetWindow(now)
	}
	a.window.add(sample, load, a.limit)
	if a.window.eligible < adaptiveWindowSize {
		elapsed := now.Sub(a.windowStarted)
		if elapsed < adaptiveWindowDuration {
			return
		}
		// A one-second window cannot contain 20 sequential 100ms successes.
		// Retain this bounded, fresh population until it is adequate, instead
		// of trapping low concurrency in insufficient_samples forever. These
		// are nonoverlapping windows: retained observations earn credit once.
		if a.window.eligible < adaptiveMinimumSamples && elapsed.Seconds() < a.windowFreshSeconds() {
			return
		}
	}
	a.lastWindow = a.window.snapshot()
	a.evaluatedWindows++
	a.evaluate(now)
	a.resetWindow(now)
}

func (a *adaptiveController) windowFreshSeconds() float64 {
	service := max(a.baseline, a.previousService, a.window.maxService)
	return max(adaptiveWindowDuration.Seconds(), 2*service.Seconds(),
		2*adaptiveMinimumSamples*service.Seconds()/float64(a.limit))
}

// evaluate uses an explicit precedence: cooldown, retry pressure, insufficient
// successes, sustained latency, rate limitation, worker supply, recovery/max,
// healthy growth. Only error-free, sufficiently supplied windows earn credit.
func (a *adaptiveController) evaluate(now time.Time) {
	if now.Before(a.cooldownUntil) {
		a.hold(now, "cooldown")
		return
	}
	if a.window.eligible >= adaptiveMinimumSamples && a.window.overloads >= 4 &&
		a.window.overloads*10 >= a.window.eligible {
		a.slowWindows, a.floorWindows = 0, 0
		a.decrease(now, "retry_pressure")
		return
	}
	if a.window.successes < adaptiveMinimumSamples {
		a.hold(now, "insufficient_samples")
		return
	}
	p95 := max(a.window.p95(), time.Nanosecond)
	if !a.hasBaseline {
		a.baseline, a.hasBaseline = p95, true
	}
	if float64(p95) > 1.5*float64(a.baseline) {
		a.healthyWindows = 0
		a.slowWindows++
		if a.rebaselineAtFloor(now, p95) {
			return
		}
		if a.slowWindows >= 2 {
			a.decrease(now, "latency")
		} else {
			a.record(now, a.limit, "latency")
		}
		return
	}
	a.slowWindows, a.floorWindows = 0, 0
	// A loaded or recovering path can never ratchet the reference upward.
	if a.window.overloads == 0 && p95 < a.baseline {
		a.baseline -= (a.baseline - p95) / 5
	}
	if a.lastWindow.RateWaitFraction > 0.1 {
		a.hold(now, "rate_limited")
		return
	}
	if a.load.Pending <= 0 || a.lastWindow.Utilization < 0.8 {
		a.hold(now, "scheduler_limited")
		return
	}
	// A few isolated errors are not sufficient to decrease, but also cannot
	// establish an error-free healthy window for additive growth.
	if a.window.overloads > 0 {
		a.hold(now, "retry_pressure")
		return
	}
	a.healthyWindows = min(a.healthyWindows+1, 2)
	if a.healthyWindows < 2 || now.Sub(a.lastChange) < adaptiveDecisionInterval {
		reason := "healthy"
		if a.recovering {
			reason = "recovering"
		}
		a.record(now, a.limit, reason)
		return
	}
	a.recovering = false
	if a.maximum > 0 && a.limit == a.maximum {
		a.record(now, a.limit, "at_max")
		return
	}
	from := a.limit
	step := adaptiveIncrease
	if a.limit < 8 {
		step = 1
	}
	// Integer representability is not a configured operational ceiling. Never
	// wrap a positive admission limit into zero (which means unlimited to FIFO).
	room := int(^uint(0)>>1) - a.limit
	if a.maximum > 0 {
		room = a.maximum - a.limit
	}
	if room == 0 {
		a.record(now, a.limit, "integer_limit")
		return
	}
	a.limit += min(step, room)
	a.generation++
	a.increases++
	a.lastChange = now
	a.healthyWindows = 0
	a.record(now, from, "healthy")
}

func (a *adaptiveController) decrease(now time.Time, reason string) {
	a.healthyWindows = 0
	if a.limit > 1 && now.Sub(a.lastChange) >= adaptiveDecisionInterval {
		from := a.limit
		a.limit = max(1, a.limit/2)
		a.pressureCutoff, a.hasPressureCutoff = a.generation, true
		a.generation++
		a.decreases++
		a.lastChange = now
		a.slowWindows, a.floorWindows = 0, 0
		a.recovering = true
		a.record(now, from, reason)
		return
	}
	a.record(now, a.limit, reason)
}

func (a *adaptiveController) rebaselineAtFloor(now time.Time, p95 time.Duration) bool {
	if a.limit != 1 || a.window.overloads != 0 {
		a.floorWindows = 0
		return false
	}
	if a.floorWindows == 0 || float64(p95) > 1.1*float64(a.floorP95) || float64(p95) < 0.9*float64(a.floorP95) {
		a.floorWindows = 1
		a.floorP95 = p95
	} else {
		a.floorWindows++
	}
	if a.floorWindows < 4 {
		return false
	}
	a.baseline = p95
	a.baselineResets++
	a.slowWindows, a.floorWindows = 0, 0
	a.recovering = true
	a.record(now, a.limit, "baseline_reset")
	return true
}

func (a *adaptiveController) hold(now time.Time, reason string) {
	a.healthyWindows, a.slowWindows, a.floorWindows = 0, 0, 0
	a.record(now, a.limit, reason)
}

func (a *adaptiveController) resetWindow(now time.Time) {
	// Keep the observed service scale separate from the frozen congestion
	// baseline. A continuous multi-second request after a latency step is not
	// idle simply because its new observation window has no samples yet.
	if a.window.maxService > 0 {
		a.previousService = a.window.maxService
	}
	a.window = adaptiveWindow{}
	a.windowStarted = now
}

// throttle protects all dispatches immediately, even when the triggering
// response predates healthy growth. Responses dispatched before the most recent
// protective decrease may extend the pause but cannot halve again. HTTP body
// ownership and existing in-flight reservations are unchanged by this transition.
func (a *adaptiveController) throttle(now time.Time, generation uint64, delay time.Duration) {
	a.http429++
	deadline := now.Add(max(delay, 100*time.Millisecond))
	if deadline.After(a.cooldownUntil) {
		a.cooldownUntil = deadline
	}
	a.healthyWindows, a.slowWindows, a.floorWindows = 0, 0, 0
	a.recovering = true
	a.resetWindow(now)
	from := a.limit
	if !a.hasPressureCutoff || generation > a.pressureCutoff {
		a.limit = max(1, a.limit/2)
		a.pressureCutoff, a.hasPressureCutoff = a.generation, true
		a.generation++ // Also invalidates observations when already at the floor.
		a.lastChange = now
		if a.limit < from {
			a.decreases++
		}
	}
	a.recoveryStarts = a.limit
	a.record(now, from, "http_429")
}

// dispatched is called only after readyAt and the user rate limiter both allow
// a real dispatch. Pace the first current-limit starts after each pause. No
// admission slot is needed while waiting for the resulting deadline.
func (a *adaptiveController) dispatched(now time.Time) {
	if now.Before(a.readyAt()) {
		return
	}
	if a.recoveryStarts <= 0 {
		a.nextStartAt = time.Time{}
		return
	}
	interval := min(250*time.Millisecond, max(25*time.Millisecond, a.baseline/time.Duration(a.limit)))
	a.recoveryStarts--
	a.nextStartAt = now.Add(interval)
}

func (a *adaptiveController) readyAt() time.Time {
	if a.cooldownUntil.After(a.nextStartAt) {
		return a.cooldownUntil
	}
	return a.nextStartAt
}

func (a *adaptiveController) record(now time.Time, from int, reason string) {
	a.lastReason = reason
	a.decisions[a.decisionNext] = AdaptiveDecision{
		ElapsedSeconds: max(0, now.Sub(a.started).Seconds()),
		From:           from, To: a.limit, Generation: a.generation, Reason: reason,
	}
	a.decisionNext = (a.decisionNext + 1) % len(a.decisions)
	a.decisionCount = min(a.decisionCount+1, len(a.decisions))
}

func (a *adaptiveController) snapshot(now time.Time) AdaptiveSnapshot {
	s := AdaptiveSnapshot{
		Initial: a.initial, Current: a.limit, Max: a.maximum, Generation: a.generation,
		Increases: a.increases, Decreases: a.decreases, HTTP429: a.http429,
		Attempts: a.attempts, Successes: a.successes, OverloadErrors: a.overloadErrors,
		StaleCompletions: a.staleCompletions, EvaluatedWindows: a.evaluatedWindows,
		BaselineResets: a.baselineResets, LastReason: a.lastReason,
		Window: a.window.snapshot(), LastWindow: cloneAdaptiveWindowSnapshot(a.lastWindow),
		Pending: a.load.Pending, InFlight: a.load.InFlight,
		CoolingDown:              now.Before(a.cooldownUntil),
		CooldownRemainingSeconds: max(0, a.cooldownUntil.Sub(now).Seconds()),
		ReadyInSeconds:           max(0, a.readyAt().Sub(now).Seconds()),
		Recovering:               a.recovering, RecoveryStartsRemaining: a.recoveryStarts,
		HealthyWindows: a.healthyWindows,
		Decisions:      make([]AdaptiveDecision, a.decisionCount),
	}
	if a.hasBaseline {
		baseline := a.baseline.Seconds()
		s.BaselineP95Seconds = &baseline
	}
	first := (a.decisionNext - a.decisionCount + len(a.decisions)) % len(a.decisions)
	for i := range s.Decisions {
		s.Decisions[i] = a.decisions[(first+i)%len(a.decisions)]
	}
	return s
}

func cloneAdaptiveWindowSnapshot(s AdaptiveWindowSnapshot) AdaptiveWindowSnapshot {
	if s.P50Seconds != nil {
		value := *s.P50Seconds
		s.P50Seconds = &value
	}
	if s.P95Seconds != nil {
		value := *s.P95Seconds
		s.P95Seconds = &value
	}
	return s
}
