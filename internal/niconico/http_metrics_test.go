package niconico

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHTTPMetricsNilAndZeroValue(t *testing.T) {
	var disabled *HTTPMetrics
	disabled.addCounter("pages", 1)
	disabled.observe("service", time.Second)
	disabled.startAttempt(false)
	disabled.endAttempt(http.StatusOK, nil, time.Second)
	trace, finalize := disabled.trace(time.Now())
	if trace != nil {
		t.Fatal("disabled collector returned a trace")
	}
	finalize()
	if _, err := json.Marshal(disabled.Snapshot()); err != nil {
		t.Fatalf("marshal disabled snapshot: %v", err)
	}
	var zero HTTPMetrics
	zero.startAttempt(false)
	zero.endAttempt(http.StatusOK, nil, time.Second)
	snapshot := zero.Snapshot()
	if snapshot.Counters["attempts"] != 1 || snapshot.InFlight.Current != 0 {
		t.Fatalf("zero-value collector: %+v", snapshot)
	}
	if _, err := json.Marshal(snapshot); err != nil {
		t.Fatalf("marshal zero-value snapshot: %v", err)
	}
}

func TestHTTPMetricsFixedKeysAndDetachedSnapshot(t *testing.T) {
	metrics := NewHTTPMetrics()
	const privateLabel = "https://private.invalid/user/secret?token=private"
	metrics.addCounter(privateLabel, 1)
	metrics.observe(privateLabel, time.Second)
	metrics.addCounter("pages", -1)
	metrics.addCounter("pages", 1)
	metrics.observe("decode", time.Second)
	snapshot := metrics.Snapshot()
	if len(snapshot.Counters) != len(httpCounterNames) || len(snapshot.Durations) != len(httpDurationNames) {
		t.Fatal("unknown metric labels changed the fixed registries")
	}
	if snapshot.ConnectionReuseRatio != nil {
		t.Fatal("reuse ratio reported without a connection")
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(encoded), privateLabel) {
		t.Fatal("snapshot contains a caller-supplied metric label")
	}
	snapshot.Counters["pages"] = 500
	*snapshot.Durations["decode"].P50Seconds = 500
	snapshot.Durations["decode"] = HTTPDurationSnapshot{}
	after := metrics.Snapshot()
	if after.Counters["pages"] != 1 || after.Durations["decode"].Count != 1 || *after.Durations["decode"].P50Seconds != 1 {
		t.Fatal("changing a snapshot changed the collector")
	}
}

func TestHTTPMetricsQuantileThresholds(t *testing.T) {
	metrics := NewHTTPMetrics()
	for _, count := range []int{0, 1, 19, 20, 99, 100} {
		for metrics.Snapshot().Durations["decode"].Count < uint64(count) {
			metrics.observe("decode", time.Millisecond)
		}
		duration := metrics.Snapshot().Durations["decode"]
		if (duration.P50Seconds != nil) != (count >= 1) ||
			(duration.P95Seconds != nil) != (count >= 20) ||
			(duration.P99Seconds != nil) != (count >= 100) {
			t.Fatalf("wrong quantile availability for %d samples: %+v", count, duration)
		}
		encoded, err := json.Marshal(duration)
		if err != nil {
			t.Fatalf("marshal duration: %v", err)
		}
		if strings.Contains(string(encoded), "p95_seconds") != (count >= 20) || strings.Contains(string(encoded), "p99_seconds") != (count >= 100) {
			t.Fatalf("unsupported quantiles were not omitted from JSON: %s", encoded)
		}
	}
}

func TestHTTPMetricsRollingWindowAndCumulativeTotals(t *testing.T) {
	metrics := NewHTTPMetrics()
	for value := 1; value <= 1200; value++ {
		metrics.observe("service", time.Duration(value)*time.Millisecond)
	}
	duration := metrics.Snapshot().Durations["service"]
	if duration.Count != 1200 || duration.WindowCount != httpMetricWindowSize {
		t.Fatalf("unexpected sample counts: %+v", duration)
	}
	for name, values := range map[string][2]float64{
		"total": {duration.TotalSeconds, 720.6},
		"mean":  {duration.MeanSeconds, 0.6005},
		"p50":   {*duration.P50Seconds, 0.688},
		"p95":   {*duration.P95Seconds, 1.149},
		"p99":   {*duration.P99Seconds, 1.190},
	} {
		if math.Abs(values[0]-values[1]) > 1e-9 {
			t.Errorf("%s = %v, want %v", name, values[0], values[1])
		}
	}
	metrics.observe("service", 2*time.Second)
	if got := *metrics.Snapshot().Durations["service"].P50Seconds; got != 0.689 {
		t.Fatalf("snapshot disturbed the live ring: p50 = %v, want 0.689", got)
	}
	metrics.observe("body_read", -time.Second)
	if got := metrics.Snapshot().Durations["body_read"].TotalSeconds; got != 0 {
		t.Fatalf("negative duration was retained: %v", got)
	}
}

func TestHTTPMetricsAllocatesSamplesLazilyWithinFixedBound(t *testing.T) {
	metrics := NewHTTPMetrics()
	for index := range metrics.durations {
		if metrics.durations[index].samples != nil {
			t.Fatal("new collector eagerly allocated duration samples")
		}
	}
	// Snapshot must not initialize unused sample storage either.
	_ = metrics.Snapshot()
	for index := range metrics.durations {
		if metrics.durations[index].samples != nil {
			t.Fatal("snapshot allocated samples in an unused duration")
		}
	}
	var metric httpDurationMetric
	for index := range 2 * httpMetricWindowSize {
		metric.observe(time.Duration(index) * time.Millisecond)
		wantLength := min(index+1, httpMetricWindowSize)
		if len(metric.samples) != wantLength || cap(metric.samples) > httpMetricWindowSize {
			t.Fatalf("sample %d: length %d, capacity %d, want length %d and capacity <= %d", index, len(metric.samples), cap(metric.samples), wantLength, httpMetricWindowSize)
		}
		if index == 0 && cap(metric.samples) != 16 {
			t.Fatalf("first sample capacity = %d, want 16", cap(metric.samples))
		}
	}
}

func TestHTTPMetricsInFlightWeightedLifetime(t *testing.T) {
	base := time.Unix(1, 0)
	now := base
	metrics := newHTTPMetrics(func() time.Time { return now })
	now = base.Add(time.Second)
	metrics.startAttempt(false)
	now = base.Add(2 * time.Second)
	metrics.startAttempt(true)
	now = base.Add(3 * time.Second)
	for range 2 {
		snapshot := metrics.Snapshot()
		if snapshot.InFlight.Current != 2 || snapshot.InFlight.Peak != 2 || snapshot.InFlight.Average != 1 {
			t.Fatalf("active in-flight interval was not included: %+v", snapshot.InFlight)
		}
	}
	now = base.Add(4 * time.Second)
	metrics.endAttempt(http.StatusOK, nil, 3*time.Second)
	now = base.Add(5 * time.Second)
	metrics.endAttempt(http.StatusNotFound, nil, 3*time.Second)
	if got := metrics.Snapshot().InFlight.Average; got != 1.2 {
		t.Fatalf("average = %v, want 1.2", got)
	}
	now = base.Add(6 * time.Second)
	snapshot := metrics.Snapshot()
	if snapshot.ElapsedSeconds != 6 || snapshot.InFlight.Average != 1 || snapshot.InFlight.Current != 0 || snapshot.InFlight.Peak != 2 {
		t.Fatalf("full-lifetime average excluded idle processing time: %+v", snapshot)
	}
	if snapshot.Counters["attempts"] != 2 || snapshot.Counters["retries"] != 1 || snapshot.Durations["service"].TotalSeconds != 6 {
		t.Fatalf("attempt counters or durations: %+v", snapshot)
	}
}

func TestHTTPMetricsResponseAndErrorClassification(t *testing.T) {
	metrics := NewHTTPMetrics()
	attempts := []struct {
		status int
		err    error
	}{
		{http.StatusOK, nil}, {http.StatusNotFound, nil}, {http.StatusTooManyRequests, nil},
		{http.StatusForbidden, nil}, {http.StatusInternalServerError, nil}, {http.StatusServiceUnavailable, nil},
		{http.StatusMovedPermanently, nil}, {http.StatusNoContent, nil},
		{0, errors.New("transport failure")}, {0, fmt.Errorf("wrapped: %w", context.Canceled)}, {0, context.DeadlineExceeded},
	}
	for index, attempt := range attempts {
		metrics.startAttempt(index > 0)
		metrics.endAttempt(attempt.status, attempt.err, time.Millisecond)
	}
	metrics.addCounter("body_errors", 1)
	metrics.addCounter("body_cancellations", 1)
	metrics.addCounter("close_errors", 1)
	metrics.addCounter("decode_errors", 1)
	metrics.addCounter("wait_cancellations", 1)
	snapshot := metrics.Snapshot()
	for key, want := range map[string]int64{
		"attempts": 11, "retries": 10, "http_200": 1, "http_404": 1, "http_429": 1,
		"http_other_4xx": 1, "http_5xx": 2, "http_other": 2, "transport_errors": 3,
		"cancellations": 2, "body_errors": 1, "body_cancellations": 1, "close_errors": 1,
		"decode_errors": 1, "wait_cancellations": 1, "pages": 0, "page_success": 0,
	} {
		if got := snapshot.Counters[key]; got != want {
			t.Errorf("%s = %d, want %d", key, got, want)
		}
	}
	if snapshot.InFlight.Current != 0 || snapshot.Durations["service"].Count != uint64(len(attempts)) {
		t.Fatalf("attempt lifetime not completed: %+v", snapshot)
	}
}

func TestHTTPMetricsTraceIntervalsRedirectsAndFinalize(t *testing.T) {
	base := time.Unix(1, 0)
	now := base
	metrics := newHTTPMetrics(func() time.Time { return now })
	trace, finalize := metrics.trace(base)
	trace.GetConn("private.invalid:443")
	now = base.Add(time.Second)
	trace.DNSStart(httptrace.DNSStartInfo{Host: "private.invalid"})
	now = base.Add(2 * time.Second)
	trace.DNSDone(httptrace.DNSDoneInfo{})
	trace.ConnectStart("tcp6", "[::1]:443")
	now = base.Add(3 * time.Second)
	trace.ConnectStart("tcp4", "127.0.0.1:443")
	now = base.Add(4 * time.Second)
	trace.ConnectDone("tcp4", "127.0.0.1:443", nil)
	now = base.Add(7 * time.Second)
	trace.ConnectDone("tcp6", "[::1]:443", errors.New("dial failed"))
	trace.TLSHandshakeStart()
	now = base.Add(9 * time.Second)
	trace.TLSHandshakeDone(tls.ConnectionState{}, nil)
	now = base.Add(10 * time.Second)
	trace.GotConn(httptrace.GotConnInfo{})
	now = base.Add(11 * time.Second)
	trace.GotFirstResponseByte()
	now = base.Add(12 * time.Second)
	trace.GetConn("redirect.invalid:443")
	now = base.Add(13 * time.Second)
	trace.GotConn(httptrace.GotConnInfo{Reused: true})
	now = base.Add(14 * time.Second)
	trace.GotFirstResponseByte()
	// Unmatched completions must not create durations measured from zero.
	trace.DNSDone(httptrace.DNSDoneInfo{})
	trace.ConnectDone("tcp", "other.invalid:443", nil)
	trace.TLSHandshakeDone(tls.ConnectionState{}, nil)
	snapshot := metrics.Snapshot()
	for name, want := range map[string]struct {
		count uint64
		total float64
	}{
		"dns": {1, 1}, "connect": {2, 6}, "tls": {1, 2}, "connection_acquire": {2, 11}, "ttfb": {1, 11},
	} {
		got := snapshot.Durations[name]
		if got.Count != want.count || got.TotalSeconds != want.total {
			t.Errorf("%s = %+v, want count %d and total %v", name, got, want.count, want.total)
		}
	}
	if snapshot.Counters["connections"] != 2 || snapshot.Counters["connections_reused"] != 1 || *snapshot.ConnectionReuseRatio != 0.5 {
		t.Fatalf("connection events: %+v", snapshot)
	}
	if snapshot.Counters["attempts"] != 0 {
		t.Fatal("trace events counted as application attempts")
	}
	trace.DNSStart(httptrace.DNSStartInfo{})
	trace.ConnectStart("tcp", "unfinished.invalid:443")
	trace.TLSHandshakeStart()
	finalize()
	finalize()
	exerciseHTTPTrace(trace, "late.invalid:443")
	after := metrics.Snapshot()
	if !reflect.DeepEqual(snapshot.Counters, after.Counters) || !reflect.DeepEqual(snapshot.Durations, after.Durations) {
		t.Fatal("callbacks mutated metrics after finalize")
	}
	encoded, err := json.Marshal(after)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), ".invalid") || strings.Contains(string(encoded), "127.0.0.1") {
		t.Fatal("trace retained addresses in snapshot")
	}
}

func exerciseHTTPTrace(trace *httptrace.ClientTrace, address string) {
	trace.GetConn(address)
	trace.DNSStart(httptrace.DNSStartInfo{Host: address})
	trace.DNSDone(httptrace.DNSDoneInfo{})
	trace.ConnectStart("tcp", address)
	trace.ConnectDone("tcp", address, nil)
	trace.TLSHandshakeStart()
	trace.TLSHandshakeDone(tls.ConnectionState{}, nil)
	trace.GotConn(httptrace.GotConnInfo{Reused: true})
	trace.GotFirstResponseByte()
}

func TestHTTPMetricsConcurrentObservationsAndSnapshot(t *testing.T) {
	metrics := NewHTTPMetrics()
	const workers, iterations = 16, 100
	var workersDone sync.WaitGroup
	workersDone.Add(workers)
	for range workers {
		go func() {
			defer workersDone.Done()
			for range iterations {
				metrics.startAttempt(true)
				metrics.addCounter("pages", 1)
				metrics.observe("decode", time.Millisecond)
				metrics.endAttempt(http.StatusOK, nil, time.Millisecond)
			}
		}()
	}
	for range 10 {
		if _, err := json.Marshal(metrics.Snapshot()); err != nil {
			t.Fatal(err)
		}
	}
	workersDone.Wait()
	snapshot := metrics.Snapshot()
	if snapshot.Counters["attempts"] != workers*iterations || snapshot.Counters["retries"] != workers*iterations || snapshot.Counters["pages"] != workers*iterations {
		t.Fatalf("lost observations: %+v", snapshot.Counters)
	}
	if snapshot.InFlight.Current != 0 || snapshot.InFlight.Peak < 1 || snapshot.InFlight.Peak > workers {
		t.Fatalf("in-flight accounting: %+v", snapshot.InFlight)
	}
	if snapshot.Durations["decode"].Count != workers*iterations || snapshot.Durations["decode"].WindowCount != httpMetricWindowSize {
		t.Fatalf("lost duration observations: %+v", snapshot.Durations["decode"])
	}
}

func TestHTTPMetricsTraceBoundsPendingPhases(t *testing.T) {
	metrics := NewHTTPMetrics()
	trace, finalize := metrics.trace(time.Now())
	defer finalize()
	// Complete one valid sample first so overflow does not erase prior work.
	exerciseHTTPTrace(trace, "first.invalid:443")
	for index := range httpTracePendingLimit + 1 {
		trace.DNSStart(httptrace.DNSStartInfo{})
		trace.TLSHandshakeStart()
		trace.ConnectStart("tcp", fmt.Sprintf("pending-%d.invalid:443", index))
	}
	for index := range httpTracePendingLimit + 1 {
		trace.DNSDone(httptrace.DNSDoneInfo{})
		trace.TLSHandshakeDone(tls.ConnectionState{}, nil)
		trace.ConnectDone("tcp", fmt.Sprintf("pending-%d.invalid:443", index), nil)
	}
	// New starts after overflow must not be paired with old or new completions.
	// Acquisition and connection-event counting remain enabled independently.
	exerciseHTTPTrace(trace, "later.invalid:443")
	snapshot := metrics.Snapshot()
	if got, want := snapshot.Counters["trace_dropped"], int64(3*(httpTracePendingLimit+2)); got != want {
		t.Fatalf("trace_dropped = %d, want %d", got, want)
	}
	for _, phase := range []string{"dns", "tls", "connect"} {
		if got := snapshot.Durations[phase].Count; got != 1 {
			t.Errorf("%s measured a mismatched completion after overflow: %d samples", phase, got)
		}
	}
	if snapshot.Counters["connections"] != 2 || snapshot.Durations["connection_acquire"].Count != 2 {
		t.Fatal("overflow incorrectly disabled other trace phases")
	}
}

func TestHTTPMetricsTracePendingBoundWithDuplicateDialKeys(t *testing.T) {
	metrics := NewHTTPMetrics()
	trace, finalize := metrics.trace(time.Now())
	defer finalize()
	for range httpTracePendingLimit + 1 {
		trace.ConnectStart("tcp", "same.invalid:443")
	}
	for range httpTracePendingLimit + 1 {
		trace.ConnectDone("tcp", "same.invalid:443", nil)
	}
	snapshot := metrics.Snapshot()
	if snapshot.Counters["trace_dropped"] != httpTracePendingLimit+1 || snapshot.Durations["connect"].Count != 0 {
		t.Fatalf("duplicate keys escaped the total pending bound: %+v", snapshot)
	}
}

func TestHTTPMetricsTraceDropsAmbiguousReorderedCompletions(t *testing.T) {
	for _, phase := range []string{"dns", "tls", "connect"} {
		t.Run(phase, func(t *testing.T) {
			base := time.Unix(1, 0)
			now := base
			metrics := newHTTPMetrics(func() time.Time { return now })
			trace, finalize := metrics.trace(base)
			defer finalize()
			var start, finish func()
			switch phase {
			case "dns":
				start = func() { trace.DNSStart(httptrace.DNSStartInfo{Host: "same.invalid"}) }
				finish = func() { trace.DNSDone(httptrace.DNSDoneInfo{}) }
			case "tls":
				start = trace.TLSHandshakeStart
				finish = func() { trace.TLSHandshakeDone(tls.ConnectionState{}, nil) }
			case "connect":
				start = func() { trace.ConnectStart("tcp", "same.invalid:443") }
				finish = func() { trace.ConnectDone("tcp", "same.invalid:443", nil) }
			}
			start() // Operation A starts first.
			now = base.Add(time.Second)
			start() // Operation B overlaps, making completion identity ambiguous.
			now = base.Add(2 * time.Second)
			finish() // B finishes first; FIFO would falsely attribute this to A.
			now = base.Add(10 * time.Second)
			finish() // A finishes last.
			start()
			now = base.Add(11 * time.Second)
			finish() // The phase must stay disabled after the ambiguity.
			snapshot := metrics.Snapshot()
			if snapshot.Durations[phase].Count != 0 || snapshot.Counters["trace_dropped"] != 3 {
				t.Fatalf("ambiguous %s intervals were retained: %+v", phase, snapshot)
			}
		})
	}
}

func TestHTTPMetricsTraceDuplicateDialDropsAllPendingConnects(t *testing.T) {
	metrics := NewHTTPMetrics()
	trace, finalize := metrics.trace(time.Now())
	defer finalize()
	trace.ConnectStart("tcp4", "127.0.0.1:443")
	trace.ConnectStart("tcp6", "[::1]:443")
	trace.ConnectStart("tcp4", "127.0.0.1:443")
	trace.ConnectDone("tcp4", "127.0.0.1:443", nil)
	trace.ConnectDone("tcp6", "[::1]:443", nil)
	trace.ConnectDone("tcp4", "127.0.0.1:443", nil)
	snapshot := metrics.Snapshot()
	if snapshot.Durations["connect"].Count != 0 || snapshot.Counters["trace_dropped"] != 3 {
		t.Fatalf("duplicate dial did not conservatively drop all pending connects: %+v", snapshot)
	}
}

func TestHTTPMetricsConcurrentTraceAndFinalize(t *testing.T) {
	metrics := NewHTTPMetrics()
	trace, finalize := metrics.trace(time.Now())
	const workers = 16
	var ready, done sync.WaitGroup
	ready.Add(workers)
	done.Add(workers)
	for worker := range workers {
		go func() {
			defer done.Done()
			address := fmt.Sprintf("worker-%d.invalid:443", worker)
			exerciseHTTPTrace(trace, address)
			ready.Done()
			for range 100 {
				exerciseHTTPTrace(trace, address)
			}
		}()
	}
	ready.Wait()
	finalize()
	before := metrics.Snapshot()
	done.Wait()
	after := metrics.Snapshot()
	if !reflect.DeepEqual(before.Counters, after.Counters) || !reflect.DeepEqual(before.Durations, after.Durations) {
		t.Fatal("trace finalization did not freeze observations")
	}
	if after.Counters["connections"] < workers || after.Durations["ttfb"].Count != 1 {
		t.Fatalf("trace observations missing: %+v", after)
	}
}

func TestHTTPMetricsTraceWithLocalTLSRedirect(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/finish", http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, "private response")
	}))
	defer server.Close()
	metrics := NewHTTPMetrics()
	started := time.Now()
	trace, finalize := metrics.trace(started)
	defer finalize()
	ctx := httptrace.WithClientTrace(context.Background(), trace)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/start", nil)
	if err != nil {
		t.Fatal(err)
	}
	metrics.startAttempt(false)
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_, readErr := io.Copy(io.Discard, response.Body)
	closeErr := response.Body.Close()
	finalize()
	metrics.endAttempt(response.StatusCode, nil, time.Since(started))
	if readErr != nil || closeErr != nil {
		t.Fatalf("response read/close errors: %v / %v", readErr, closeErr)
	}
	snapshot := metrics.Snapshot()
	if snapshot.Counters["attempts"] != 1 || snapshot.Counters["connections"] != 2 || snapshot.Counters["connections_reused"] != 1 {
		t.Fatalf("redirect changed application-attempt semantics: %+v", snapshot.Counters)
	}
	for name, want := range map[string]uint64{"connection_acquire": 2, "tls": 1, "connect": 1, "ttfb": 1, "service": 1} {
		if got := snapshot.Durations[name].Count; got != want {
			t.Errorf("%s count = %d, want %d", name, got, want)
		}
	}
}
