package niconico

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptrace"
	"slices"
	"sync"
	"time"
)

const (
	httpMetricWindowSize  = 1024
	httpTracePendingLimit = 64
)

// These fixed registries keep observations bounded and prevent callers from
// accidentally retaining request identifiers or other high-cardinality labels.
var httpCounterNames = [...]string{
	"attempts", "retries", "pages", "page_success", "page_errors",
	"http_200", "http_404", "http_429", "http_other_4xx", "http_5xx", "http_other",
	"transport_errors", "cancellations", "body_bytes", "body_errors",
	"close_errors", "decode_errors", "wait_cancellations", "body_cancellations",
	"connections", "connections_reused", "trace_dropped",
}

var httpDurationNames = [...]string{
	"semaphore_wait", "rate_wait", "backoff_wait", "adaptive_wait", "service", "body_read", "body_close", "decode",
	"connection_acquire", "dns", "connect", "tls", "ttfb",
}

type httpDurationMetric struct {
	count   uint64
	total   float64
	samples []time.Duration
	next    int
}

func (d *httpDurationMetric) observe(elapsed time.Duration) {
	if elapsed < 0 {
		elapsed = 0
	}
	d.count++
	d.total += elapsed.Seconds()
	if len(d.samples) < httpMetricWindowSize {
		if len(d.samples) == cap(d.samples) {
			capacity := min(httpMetricWindowSize, max(16, 2*cap(d.samples)))
			samples := make([]time.Duration, len(d.samples), capacity)
			copy(samples, d.samples)
			d.samples = samples
		}
		d.samples = append(d.samples, elapsed)
		return
	}
	d.samples[d.next] = elapsed
	d.next = (d.next + 1) % len(d.samples)
}

// HTTPDurationSnapshot contains cumulative totals and recent-window quantiles.
// MeanSeconds is cumulative; percentiles use only WindowCount recent samples.
// A missing percentile has too few samples: p50 needs one, p95 needs 20, and
// p99 needs 100. All durations are seconds, and quantiles use nearest ranks.
type HTTPDurationSnapshot struct {
	Count        uint64   `json:"total_count"`
	WindowCount  int      `json:"window_count"`
	TotalSeconds float64  `json:"total_seconds"`
	MeanSeconds  float64  `json:"mean_seconds"`
	P50Seconds   *float64 `json:"p50_seconds,omitempty"`
	P95Seconds   *float64 `json:"p95_seconds,omitempty"`
	P99Seconds   *float64 `json:"p99_seconds,omitempty"`
}

func (d *httpDurationMetric) snapshot() HTTPDurationSnapshot {
	snapshot := HTTPDurationSnapshot{
		Count: d.count, WindowCount: len(d.samples), TotalSeconds: d.total,
	}
	if d.count == 0 {
		return snapshot
	}
	snapshot.MeanSeconds = d.total / float64(d.count)
	// Snapshot has already detached these live samples under the collector lock.
	samples := d.samples
	slices.Sort(samples)
	quantile := func(percent int) *float64 {
		value := samples[(percent*len(samples)+99)/100-1].Seconds()
		return &value
	}
	snapshot.P50Seconds = quantile(50)
	if len(samples) >= 20 {
		snapshot.P95Seconds = quantile(95)
	}
	if len(samples) >= 100 {
		snapshot.P99Seconds = quantile(99)
	}
	return snapshot
}

// HTTPInFlightSnapshot measures actual HTTP work, excluding admission, pacing,
// backoff, and decoding. Average includes the entire measurement lifetime.
type HTTPInFlightSnapshot struct {
	Current int64   `json:"current"`
	Peak    int64   `json:"peak"`
	Average float64 `json:"average"`
}

// HTTPMetricsSnapshot is a detached, JSON-safe view of one collector. Trace
// event counts can exceed application attempts, for example after redirects.
type HTTPMetricsSnapshot struct {
	ElapsedSeconds       float64                         `json:"elapsed_seconds"`
	Counters             map[string]int64                `json:"counters"`
	Durations            map[string]HTTPDurationSnapshot `json:"durations"`
	InFlight             HTTPInFlightSnapshot            `json:"in_flight"`
	ConnectionReuseRatio *float64                        `json:"connection_reuse_ratio,omitempty"`
}

// HTTPMetrics collects aggregate observations for one command invocation. It
// is safe for concurrent use and lazily stores at most 1024 samples per duration.
// A nil collector disables observations; its methods are safe to call.
type HTTPMetrics struct {
	mu          sync.Mutex
	now         func() time.Time
	started     time.Time
	lastChange  time.Time
	counters    [len(httpCounterNames)]int64
	durations   [len(httpDurationNames)]httpDurationMetric
	inFlight    int64
	peak        int64
	inFlightSum float64
}

// NewHTTPMetrics begins a measurement lifetime, including time spent waiting
// for initial scheduling and final result processing.
func NewHTTPMetrics() *HTTPMetrics {
	return newHTTPMetrics(time.Now)
}

func newHTTPMetrics(now func() time.Time) *HTTPMetrics {
	started := now()
	return &HTTPMetrics{now: now, started: started, lastChange: started}
}

// initLocked also makes the zero value usable. Normal callers use the
// constructor so that elapsed time includes scheduling before the first event.
func (m *HTTPMetrics) initLocked() {
	if m.now == nil {
		m.now = time.Now
		m.started = m.now()
		m.lastChange = m.started
	}
}

func (m *HTTPMetrics) addCounter(name string, delta int64) {
	if m == nil || delta <= 0 {
		return
	}
	index := slices.Index(httpCounterNames[:], name)
	if index < 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.initLocked()
	m.counters[index] += delta
}

func (m *HTTPMetrics) observe(name string, elapsed time.Duration) {
	if m == nil {
		return
	}
	index := slices.Index(httpDurationNames[:], name)
	if index < 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.initLocked()
	m.durations[index].observe(elapsed)
}

func (m *HTTPMetrics) incrementLocked(name string) {
	m.counters[slices.Index(httpCounterNames[:], name)]++
}

func (m *HTTPMetrics) advanceInFlightLocked(now time.Time) {
	if now.Before(m.lastChange) {
		return
	}
	m.inFlightSum += now.Sub(m.lastChange).Seconds() * float64(m.inFlight)
	m.lastChange = now
}

func (m *HTTPMetrics) startAttempt(retry bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.initLocked()
	m.advanceInFlightLocked(m.now())
	m.incrementLocked("attempts")
	if retry {
		m.incrementLocked("retries")
	}
	m.inFlight++
	m.peak = max(m.peak, m.inFlight)
}

// endAttempt is called once, after body-close completion or transport cleanup.
// err is the Do error; body, close, and decode failures have separate counters.
// Status classification describes response headers, never logical page success.
// The cancellations counter is the context-cancellation subset of transport_errors;
// body_cancellations and wait_cancellations describe their separate boundaries.
func (m *HTTPMetrics) endAttempt(status int, err error, elapsed time.Duration) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.initLocked()
	m.advanceInFlightLocked(m.now())
	if m.inFlight > 0 {
		m.inFlight--
	}
	m.durations[slices.Index(httpDurationNames[:], "service")].observe(elapsed)
	switch {
	case status == http.StatusOK:
		m.incrementLocked("http_200")
	case status == http.StatusNotFound:
		m.incrementLocked("http_404")
	case status == http.StatusTooManyRequests:
		m.incrementLocked("http_429")
	case status >= 400 && status < 500:
		m.incrementLocked("http_other_4xx")
	case status >= 500 && status < 600:
		m.incrementLocked("http_5xx")
	case status > 0:
		m.incrementLocked("http_other")
	}
	if err != nil {
		m.incrementLocked("transport_errors")
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			m.incrementLocked("cancellations")
		}
	}
}

// Snapshot returns cumulative totals and bounded recent-window quantiles. It
// includes the still-active in-flight interval without changing the collector.
func (m *HTTPMetrics) Snapshot() HTTPMetricsSnapshot {
	if m == nil {
		return HTTPMetricsSnapshot{}
	}
	m.mu.Lock()
	m.initLocked()
	now := m.now()
	elapsed := max(0, now.Sub(m.started).Seconds())
	area := m.inFlightSum + max(0, now.Sub(m.lastChange).Seconds())*float64(m.inFlight)
	snapshot := HTTPMetricsSnapshot{
		ElapsedSeconds: elapsed,
		InFlight:       HTTPInFlightSnapshot{Current: m.inFlight, Peak: m.peak},
		Counters:       make(map[string]int64, len(httpCounterNames)),
		Durations:      make(map[string]HTTPDurationSnapshot, len(httpDurationNames)),
	}
	counters, durations := m.counters, m.durations
	for index := range durations {
		durations[index].samples = slices.Clone(durations[index].samples)
	}
	m.mu.Unlock()
	if elapsed > 0 {
		snapshot.InFlight.Average = area / elapsed
	}
	for index, name := range httpCounterNames {
		snapshot.Counters[name] = counters[index]
	}
	for index, name := range httpDurationNames {
		snapshot.Durations[name] = durations[index].snapshot()
	}
	if connections := snapshot.Counters["connections"]; connections > 0 {
		ratio := float64(snapshot.Counters["connections_reused"]) / float64(connections)
		snapshot.ConnectionReuseRatio = &ratio
	}
	return snapshot
}

type httpConnectKey struct {
	network string
	address string
}

// httpTraceTimes lives only with an individual attempt's trace. DNS/TLS hooks
// cannot identify overlapping operations, so ambiguous phases are disabled.
// Connect hooks distinguish different Happy Eyeballs dial keys, but overlapping
// duplicate keys are also ambiguous. Transient addresses are removed on finish.
type httpTraceTimes struct {
	mu               sync.Mutex
	stopped          bool
	firstByte        bool
	acquire          time.Time
	dns              httpTracePending
	tls              httpTracePending
	connects         map[httpConnectKey][]time.Time
	connectCount     int
	connectsDisabled bool
}

// A DNS/TLS phase with overlapping starts is disabled for the remainder of the
// attempt: its completion hooks cannot identify which operation finished.
// Resuming after dropping starts could still pair a late completion incorrectly.
// Completed measurements remain valid; trace_dropped reports omitted starts.
type httpTracePending struct {
	starts   []time.Time
	disabled bool
}

func (p *httpTracePending) add(now time.Time) int64 {
	if p.disabled {
		return 1
	}
	if len(p.starts) > 0 {
		dropped := int64(len(p.starts) + 1)
		p.starts = nil
		p.disabled = true
		return dropped
	}
	p.starts = append(p.starts, now)
	return 0
}

func popHTTPTraceStart(starts *[]time.Time) (time.Time, bool) {
	if len(*starts) == 0 {
		return time.Time{}, false
	}
	started := (*starts)[0]
	(*starts)[0] = time.Time{}
	*starts = (*starts)[1:]
	return started, true
}

// trace builds a separate race-safe trace for each application attempt. Go may
// invoke hooks after Do has returned or concurrently with other hooks. Calling
// finalize at body-close completion (or transport failure) stops those late
// observations and frees unfinished trace state. It is idempotent. Redirects
// can repeat hooks; TTFB observes only the first response byte in the attempt.
// No URLs, headers, errors, or response contents are retained in the collector.
func (m *HTTPMetrics) trace(start time.Time) (*httptrace.ClientTrace, func()) {
	if m == nil {
		return nil, func() {}
	}
	m.mu.Lock()
	m.initLocked()
	now := m.now
	m.mu.Unlock()
	times := &httpTraceTimes{connects: make(map[httpConnectKey][]time.Time)}
	trace := &httptrace.ClientTrace{
		GetConn: func(string) {
			times.mu.Lock()
			defer times.mu.Unlock()
			if !times.stopped {
				times.acquire = now()
			}
		},
		GotConn: func(info httptrace.GotConnInfo) {
			times.mu.Lock()
			defer times.mu.Unlock()
			if times.stopped {
				return
			}
			if !times.acquire.IsZero() {
				m.observe("connection_acquire", now().Sub(times.acquire))
				times.acquire = time.Time{}
			}
			m.mu.Lock()
			m.incrementLocked("connections")
			if info.Reused {
				m.incrementLocked("connections_reused")
			}
			m.mu.Unlock()
		},
		DNSStart: func(httptrace.DNSStartInfo) {
			times.mu.Lock()
			defer times.mu.Unlock()
			if !times.stopped {
				m.addCounter("trace_dropped", times.dns.add(now()))
			}
		},
		DNSDone: func(httptrace.DNSDoneInfo) {
			times.mu.Lock()
			defer times.mu.Unlock()
			if times.stopped {
				return
			}
			if started, ok := popHTTPTraceStart(&times.dns.starts); ok {
				m.observe("dns", now().Sub(started))
			}
		},
		ConnectStart: func(network, address string) {
			times.mu.Lock()
			defer times.mu.Unlock()
			if times.stopped {
				return
			}
			if times.connectsDisabled {
				m.addCounter("trace_dropped", 1)
				return
			}
			key := httpConnectKey{network: network, address: address}
			// A duplicate key cannot distinguish reordered completions. Drop
			// the entire phase, including still-pending distinct-key starts,
			// rather than inventing intervals from an assumed callback order.
			if times.connectCount >= httpTracePendingLimit || len(times.connects[key]) > 0 {
				m.addCounter("trace_dropped", int64(times.connectCount+1))
				times.connects = nil
				times.connectCount = 0
				times.connectsDisabled = true
				return
			}
			times.connects[key] = append(times.connects[key], now())
			times.connectCount++
		},
		ConnectDone: func(network, address string, _ error) {
			times.mu.Lock()
			defer times.mu.Unlock()
			if times.stopped || times.connectsDisabled {
				return
			}
			key := httpConnectKey{network: network, address: address}
			starts := times.connects[key]
			started, ok := popHTTPTraceStart(&starts)
			if len(starts) == 0 {
				delete(times.connects, key)
			} else {
				times.connects[key] = starts
			}
			if ok {
				times.connectCount--
				m.observe("connect", now().Sub(started))
			}
		},
		TLSHandshakeStart: func() {
			times.mu.Lock()
			defer times.mu.Unlock()
			if !times.stopped {
				m.addCounter("trace_dropped", times.tls.add(now()))
			}
		},
		TLSHandshakeDone: func(tls.ConnectionState, error) {
			times.mu.Lock()
			defer times.mu.Unlock()
			if times.stopped {
				return
			}
			if started, ok := popHTTPTraceStart(&times.tls.starts); ok {
				m.observe("tls", now().Sub(started))
			}
		},
		GotFirstResponseByte: func() {
			times.mu.Lock()
			defer times.mu.Unlock()
			if !times.stopped && !times.firstByte {
				m.observe("ttfb", now().Sub(start))
				times.firstByte = true
			}
		},
	}
	finalize := func() {
		times.mu.Lock()
		defer times.mu.Unlock()
		times.stopped = true
		times.acquire = time.Time{}
		times.dns = httpTracePending{}
		times.tls = httpTracePending{}
		times.connects = nil
	}
	return trace, finalize
}
