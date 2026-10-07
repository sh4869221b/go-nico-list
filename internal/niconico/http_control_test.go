package niconico

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func waitForPending(t *testing.T, c *HTTPControl, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for c.Snapshot().Admission.Pending != n {
		if time.Now().After(deadline) {
			t.Fatalf("pending never became %d: %+v", n, c.Snapshot().Admission)
		}
		runtime.Gosched()
	}
}

func TestHTTPControlFIFOAndCanceledHead(t *testing.T) {
	c := NewHTTPControl(1, false)
	if err := c.acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { first <- c.acquire(canceled) }()
	waitForPending(t, c, 1)
	second := make(chan error, 1)
	go func() { second <- c.acquire(context.Background()) }()
	waitForPending(t, c, 2)
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	waitForPending(t, c, 1)
	c.release()
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	c.release()
	got := c.Snapshot().Admission
	if got.Reserved != 0 || got.Pending != 0 || got.PeakReserved != 1 || got.PeakPending != 2 {
		t.Fatalf("snapshot: %+v", got)
	}
}

func TestHTTPControlDrainAndIncrease(t *testing.T) {
	c := NewHTTPControl(3, false)
	for range 3 {
		if err := c.acquire(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	c.mu.Lock()
	c.limit = 1
	c.mu.Unlock()
	result := make(chan error, 1)
	go func() { result <- c.acquire(context.Background()) }()
	waitForPending(t, c, 1)
	c.release()
	c.release()
	if got := c.Snapshot().Admission; got.Reserved != 1 || got.Pending != 1 {
		t.Fatalf("did not drain: %+v", got)
	}
	c.mu.Lock()
	c.limit = 2
	c.grantLocked()
	c.mu.Unlock()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	c.release()
	c.release()
	if got := c.Snapshot().Admission; got.Reserved != 0 || got.PeakReserved != 3 {
		t.Fatalf("snapshot: %+v", got)
	}
}

func TestHTTPControlCancellationAfterGrant(t *testing.T) {
	c := NewHTTPControl(1, false)
	if err := c.acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.acquire(ctx) }()
	waitForPending(t, c, 1)
	// Hold the mutex through both events so the waiter must observe cancellation
	// after the grant, regardless of which select arm runs.
	c.mu.Lock()
	c.reserved--
	c.grantLocked()
	cancel()
	c.mu.Unlock()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("error: %v", err)
	}
	if got := c.Snapshot().Admission; got.Reserved != 0 || got.Pending != 0 {
		t.Fatalf("leaked grant: %+v", got)
	}
}

type controlRoundTripper func(*http.Request) (*http.Response, error)

func (f controlRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type blockingCloseBody struct {
	io.Reader
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingCloseBody) Close() error {
	b.once.Do(func() { close(b.entered); <-b.release })
	return nil
}

func TestHTTPControlHoldsUntilCloseCompletes(t *testing.T) {
	c := NewHTTPControl(1, true)
	body := &blockingCloseBody{Reader: strings.NewReader("ok"), entered: make(chan struct{}), release: make(chan struct{})}
	client := &http.Client{Transport: controlRoundTripper(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: body, Header: make(http.Header), Request: r}, nil
	})}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://fixture.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := waitForHTTPAttempt(req.Context(), nil, 0, c); err != nil {
		t.Fatal(err)
	}
	res, err := performHTTPAttempt(client, req, 1, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var unblockOnce sync.Once
	unblock := func() { unblockOnce.Do(func() { close(body.release) }) }
	defer unblock()
	if _, err := io.ReadAll(res.Body); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- res.Body.Close() }()
	<-body.entered
	if got := c.Snapshot().Admission; got.Reserved != 1 {
		t.Fatalf("released on headers or EOF: %+v", got)
	}
	next := make(chan error, 1)
	go func() { next <- c.acquire(context.Background()) }()
	waitForPending(t, c, 1)
	unblock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-next; err != nil {
		t.Fatal(err)
	}
	if err := res.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if got := c.Snapshot().Admission; got.Reserved != 1 {
		t.Fatalf("duplicate close released next reservation: %+v", got)
	}
	c.release()
}

func TestHTTPControlRetryReleasesBeforeBackoff(t *testing.T) {
	c := NewHTTPControl(1, true)
	originalTransport := http.DefaultTransport
	originalSleep := sleepFn
	t.Cleanup(func() { http.DefaultTransport = originalTransport; sleepFn = originalSleep })
	calls := 0
	http.DefaultTransport = controlRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		status := http.StatusTooManyRequests
		if calls == 2 {
			status = http.StatusOK
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{"Retry-After": []string{"2"}}, Request: r}, nil
	})
	var delays []time.Duration
	sleepFn = func(ctx context.Context, d time.Duration) error {
		if d > 0 {
			delays = append(delays, d)
			if got := c.Snapshot().Admission; got.Reserved != 0 {
				t.Errorf("reservation held during backoff: %+v", got)
			}
		}
		return ctx.Err()
	}
	res, err := retriesRequest(context.Background(), "http://fixture.invalid", time.Second, 2, nil, c)
	if err != nil {
		t.Fatal(err)
	}
	if err := res.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if len(delays) != 1 || delays[0] != 2*time.Second {
		t.Fatalf("backoff: %v", delays)
	}
	got := c.Snapshot()
	if got.Admission.Reserved != 0 || got.Metrics.Counters["attempts"] != 2 || got.Metrics.Counters["retries"] != 1 || got.Metrics.Counters["http_429"] != 1 {
		t.Fatalf("snapshot: %+v", got)
	}
}

func TestHTTPControlRateAfterAdmissionAndNoDoubleBackoff(t *testing.T) {
	originalNow, originalSleep := timeNow, sleepFn
	t.Cleanup(func() { timeNow = originalNow; sleepFn = originalSleep })
	now := time.Unix(1000, 0)
	timeNow = func() time.Time { return now }
	var sleeps []time.Duration
	sleepFn = func(ctx context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		now = now.Add(d)
		return ctx.Err()
	}
	c := NewHTTPControl(1, false)
	rate := NewRateLimiter(0, time.Second)
	if err := waitForHTTPAttempt(context.Background(), rate, 0, c); err != nil {
		t.Fatal(err)
	}
	c.release()
	if err := waitForHTTPAttempt(context.Background(), rate, 2*time.Second, c); err != nil {
		t.Fatal(err)
	}
	c.release()
	if len(sleeps) != 3 || sleeps[0] != 0 || sleeps[1] != 2*time.Second || sleeps[2] != 0 {
		t.Fatalf("rate/backoff double counted: %v", sleeps)
	}
}

func TestHTTPControlCancelRateWait(t *testing.T) {
	originalSleep := sleepFn
	t.Cleanup(func() { sleepFn = originalSleep })
	ctx, cancel := context.WithCancel(context.Background())
	c := NewHTTPControl(1, true)
	sleepFn = func(context.Context, time.Duration) error { cancel(); return context.Canceled }
	err := waitForHTTPAttempt(ctx, NewRateLimiter(1, 0), 0, c)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error: %v", err)
	}
	got := c.Snapshot()
	if got.Admission.Reserved != 0 || got.Metrics.Counters["attempts"] != 0 {
		t.Fatalf("leaked or dispatched while canceled: %+v", got)
	}
}

type failingControlBody struct {
	io.Reader
	readErr  error
	closeErr error
	closes   int
}

func (b *failingControlBody) Read(p []byte) (int, error) {
	if b.readErr != nil {
		return 0, b.readErr
	}
	return b.Reader.Read(p)
}
func (b *failingControlBody) Close() error { b.closes++; return b.closeErr }

func TestHTTPMeasuredPageOutcomes(t *testing.T) {
	cases := []struct {
		name              string
		status            int
		payload           string
		readErr, closeErr error
		wantErr           bool
		counter           string
	}{
		{name: "success", status: 200, payload: `{"meta":{"status":200},"data":{"items":[]}}`, counter: "page_success"},
		{name: "natural end", status: 404, counter: "http_404"},
		{name: "server error", status: 503, wantErr: true, counter: "http_5xx"},
		{name: "body failure", status: 200, readErr: io.ErrUnexpectedEOF, wantErr: true, counter: "body_errors"},
		{name: "body cancel", status: 200, readErr: context.Canceled, wantErr: true, counter: "body_cancellations"},
		{name: "decode failure", status: 200, payload: `invalid`, wantErr: true, counter: "decode_errors"},
		{name: "close failure remains nonfatal", status: 200, payload: `{"meta":{"status":200},"data":{"items":[]}}`, closeErr: errors.New("close failed"), counter: "close_errors"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			original := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = original })
			body := &failingControlBody{Reader: strings.NewReader(tc.payload), readErr: tc.readErr, closeErr: tc.closeErr}
			http.DefaultTransport = controlRoundTripper(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Body: body, Header: make(http.Header), Request: r}, nil
			})
			c := NewHTTPControl(1, true)
			_, err := fetchPage(context.Background(), "http://fixture.invalid", time.Second, 1, nil, slog.New(slog.DiscardHandler), parseUserVideoPage, c)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error: %v", err)
			}
			got := c.Snapshot()
			if got.Metrics.Counters[tc.counter] != 1 || got.Metrics.Counters["attempts"] != 1 || got.Metrics.Counters["pages"] != 1 || got.Admission.Reserved != 0 || body.closes != 1 {
				t.Fatalf("snapshot: %+v closes=%d", got, body.closes)
			}
		})
	}
}

func TestHTTPControlTransportFailureAndCanceledBackoff(t *testing.T) {
	originalTransport, originalSleep := http.DefaultTransport, sleepFn
	t.Cleanup(func() { http.DefaultTransport = originalTransport; sleepFn = originalSleep })
	http.DefaultTransport = controlRoundTripper(func(*http.Request) (*http.Response, error) { return nil, errors.New("transport failed") })
	c := NewHTTPControl(1, true)
	sleepFn = func(context.Context, time.Duration) error { return context.Canceled }
	res, err := retriesRequest(context.Background(), "http://fixture.invalid", time.Second, 3, nil, c)
	if res != nil {
		_ = res.Body.Close()
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error: %v", err)
	}
	got := c.Snapshot()
	if got.Admission.Reserved != 0 || got.Metrics.Counters["attempts"] != 1 || got.Metrics.Counters["retries"] != 0 || got.Metrics.Counters["transport_errors"] != 1 || got.Metrics.Counters["wait_cancellations"] != 1 {
		t.Fatalf("snapshot: %+v", got)
	}
}

func TestHTTPControlRateReservationWaitsForCap(t *testing.T) {
	c := NewHTTPControl(1, false)
	if err := c.acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	rate := NewRateLimiter(100, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- waitForHTTPAttempt(ctx, rate, 0, c) }()
	waitForPending(t, c, 1)
	rate.mu.Lock()
	reservedRate := rate.nextTime
	rate.mu.Unlock()
	if !reservedRate.IsZero() {
		t.Fatal("reserved rate permission before HTTP admission")
	}
	c.release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	c.release()
}

func TestHTTPControlRedirectErrorPreservesStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/again", http.StatusFound) }))
	defer server.Close()
	c := NewHTTPControl(1, true)
	res, err := retriesRequest(context.Background(), server.URL, time.Second, 1, nil, c)
	if res != nil {
		_ = res.Body.Close()
	}
	if err == nil {
		t.Fatal("redirect loop unexpectedly succeeded")
	}
	got := c.Snapshot()
	if got.Admission.Reserved != 0 || got.Metrics.Counters["attempts"] != 1 || got.Metrics.Counters["transport_errors"] != 1 || got.Metrics.Counters["http_other"] != 1 || got.Metrics.Durations["body_close"].Count != 1 {
		t.Fatalf("snapshot: %+v", got)
	}
}
