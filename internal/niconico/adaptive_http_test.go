package niconico

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type adaptiveTestClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *adaptiveTestClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *adaptiveTestClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}
func useAdaptiveTestClock(t *testing.T) *adaptiveTestClock {
	t.Helper()
	oldNow, oldSleep := timeNow, sleepFn
	clock := &adaptiveTestClock{now: time.Unix(1000, 0)}
	timeNow = clock.Now
	sleepFn = func(ctx context.Context, d time.Duration) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		clock.Advance(d)
		return nil
	}
	t.Cleanup(func() { timeNow = oldNow; sleepFn = oldSleep })
	return clock
}

func TestAdaptiveUnlimitedHeader429PrecedesSlowClose(t *testing.T) {
	clock := useAdaptiveTestClock(t)
	c := NewAdaptiveHTTPControl(0, false)
	body := &blockingCloseBody{Reader: strings.NewReader(""), entered: make(chan struct{}), release: make(chan struct{})}
	client := &http.Client{Transport: controlRoundTripper(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"2"}}, Body: body, Request: r}, nil
	})}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://fixture.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := adaptiveHTTPAttempt(client, req, 1, time.Time{}, nil, c)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var once sync.Once
	unblock := func() { once.Do(func() { close(body.release) }) }
	defer unblock()
	c.mu.Lock()
	limit, deadline, active := c.limit, c.adaptive.cooldownUntil, c.inFlight
	c.mu.Unlock()
	if limit != 4 || deadline != clock.Now().Add(2*time.Second) || active != 1 {
		t.Fatalf("header feedback missing: limit=%d deadline=%v active=%d", limit, deadline, active)
	}
	done := make(chan error, 1)
	go func() { done <- res.Body.Close() }()
	<-body.entered
	if got := c.Snapshot().Admission.Reserved; got != 1 {
		t.Fatalf("released before close completed: %d", got)
	}
	unblock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := c.Snapshot().Admission.Reserved; got != 0 {
		t.Fatalf("leaked reservation: %d", got)
	}
	if c.metrics != nil {
		t.Fatal("adaptive unexpectedly enabled full metrics")
	}
}

func TestAdaptiveRateWaiterRechecksNewCooldown(t *testing.T) {
	clock := useAdaptiveTestClock(t)
	c := NewAdaptiveHTTPControl(8, false)
	rateStarted, releaseRate := make(chan struct{}), make(chan struct{})
	var once sync.Once
	sleepFn = func(ctx context.Context, d time.Duration) error {
		first := false
		once.Do(func() { first = true; close(rateStarted) })
		if first {
			select {
			case <-releaseRate:
			case <-ctx.Done():
				return ctx.Err()
			}
		} else {
			if d > 0 && c.Snapshot().Admission.Reserved != 0 {
				t.Error("cooldown retained an admission slot")
			}
			clock.Advance(d)
		}
		return ctx.Err()
	}
	var dispatched time.Time
	client := &http.Client{Transport: controlRoundTripper(func(r *http.Request) (*http.Response, error) {
		dispatched = clock.Now()
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok")), Request: r}, nil
	})}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://fixture.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		res, err := adaptiveHTTPAttempt(client, req, 1, time.Time{}, NewRateLimiter(1, 0), c)
		if res != nil {
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
		}
		done <- err
	}()
	<-rateStarted
	c.throttleAdaptive(&adaptiveAttempt{generation: 0}, 2*time.Second)
	close(releaseRate)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !dispatched.Equal(time.Unix(1002, 0)) {
		t.Fatalf("stale rate grant or doubled delay: dispatch=%v", dispatched)
	}
	if got := c.Snapshot().Admission; got.Reserved != 0 || got.Pending != 0 {
		t.Fatalf("leak: %+v", got)
	}
}

func TestAdaptiveRetryCombinesSharedDeadline(t *testing.T) {
	clock := useAdaptiveTestClock(t)
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	calls := 0
	http.DefaultTransport = controlRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		status := 200
		if calls == 1 {
			status = 429
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": []string{"2"}}, Body: io.NopCloser(strings.NewReader("ok")), Request: r}, nil
	})
	c := NewAdaptiveHTTPControl(8, true)
	res, err := retriesRequest(context.Background(), "http://fixture.invalid", time.Second, 2, nil, c)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	if err := res.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if elapsed := clock.Now().Sub(time.Unix(1000, 0)); elapsed != 2*time.Second {
		t.Fatalf("retry and cooldown should overlap, got %v", elapsed)
	}
	if got := c.Snapshot(); got.Metrics.Counters["attempts"] != 2 || got.Metrics.Counters["retries"] != 1 || got.Admission.Reserved != 0 {
		t.Fatalf("snapshot: %+v", got)
	}
}

func TestAdaptiveCanceledCooldownKeepsNoReservation(t *testing.T) {
	useAdaptiveTestClock(t)
	c := NewAdaptiveHTTPControl(8, false)
	c.throttleAdaptive(&adaptiveAttempt{generation: 0}, time.Hour)
	entered := make(chan struct{})
	sleepFn = func(ctx context.Context, _ time.Duration) error { close(entered); <-ctx.Done(); return ctx.Err() }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.waitAdaptiveReady(ctx, time.Time{}) }()
	<-entered
	if got := c.Snapshot().Admission.Reserved; got != 0 {
		t.Fatalf("cooldown reservation=%d", got)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("error: %v", err)
	}
}

func TestAdaptiveShrunkLimitRejectsStaleReservation(t *testing.T) {
	clock := useAdaptiveTestClock(t)
	c := NewAdaptiveHTTPControl(8, false)
	// Eight old admissions exist, but only four may start after a shrink to four.
	for range 8 {
		if err := c.acquire(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	c.throttleAdaptive(&adaptiveAttempt{generation: 0}, time.Second)
	clock.Advance(time.Second)
	var permits []*adaptiveAttempt
	for range 4 {
		c.mu.Lock()
		readyAt := c.adaptive.readyAt()
		c.mu.Unlock()
		if readyAt.After(clock.Now()) {
			clock.Advance(readyAt.Sub(clock.Now()))
		}
		p, ready, err := c.adaptiveDispatch(context.Background(), 0, c.pauseEpoch)
		if err != nil || !ready {
			t.Fatalf("expected available dispatch: %v", err)
		}
		permits = append(permits, &p)
	}
	clock.Advance(time.Second)
	if p, ready, err := c.adaptiveDispatch(context.Background(), 0, c.pauseEpoch); err != nil || ready {
		t.Fatalf("stale fifth reservation dispatched: %v %v", p, err)
	}
	for range 4 {
		c.release()
	}
	for _, p := range permits {
		c.finishAdaptive(p, context.Background(), 200, nil, nil, true, nil)
	}
	if got := c.Snapshot().Admission.Reserved; got != 0 {
		t.Fatalf("reservation leak %d", got)
	}
}

func TestAdaptiveBodyOutcomeAndCallerCancellation(t *testing.T) {
	cases := []struct {
		name         string
		readErr      error
		cancel       bool
		payload      string
		wantOverload uint64
	}{
		{name: "read failure", readErr: io.ErrUnexpectedEOF, wantOverload: 1},
		{name: "client deadline with live caller", readErr: context.DeadlineExceeded, wantOverload: 1},
		{name: "caller cancellation", readErr: context.Canceled, cancel: true},
		{name: "decode failure", payload: "invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			useAdaptiveTestClock(t)
			original := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = original })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			http.DefaultTransport = controlRoundTripper(func(r *http.Request) (*http.Response, error) {
				if tc.cancel {
					cancel()
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: &failingControlBody{Reader: strings.NewReader(tc.payload), readErr: tc.readErr}, Request: r}, nil
			})
			c := NewAdaptiveHTTPControl(8, false)
			_, _ = fetchPage(ctx, "http://fixture.invalid", time.Second, 1, nil, slog.New(slog.DiscardHandler), parseUserVideoPage, c)
			if got := c.Snapshot().Adaptive.OverloadErrors; got != tc.wantOverload {
				t.Fatalf("overload errors = %d, want %d", got, tc.wantOverload)
			}
			if got := c.Snapshot().Admission.Reserved; got != 0 {
				t.Fatalf("leaked slot %d", got)
			}
		})
	}
}

type adaptiveSlowCloseBody struct {
	io.Reader
	close func()
}

func (b *adaptiveSlowCloseBody) Close() error { b.close(); return nil }

func TestAdaptiveRetryAfterDeadlineStartsAtHeaders(t *testing.T) {
	clock := useAdaptiveTestClock(t)
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	calls := 0
	var retryStarted time.Time
	http.DefaultTransport = controlRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"1"}}, Body: &adaptiveSlowCloseBody{Reader: strings.NewReader(""), close: func() { clock.Advance(800 * time.Millisecond) }}, Request: r}, nil
		}
		retryStarted = clock.Now()
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok")), Request: r}, nil
	})
	c := NewAdaptiveHTTPControl(8, false)
	res, err := retriesRequest(context.Background(), "http://fixture.invalid", time.Second, 2, nil, c)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	if err := res.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if elapsed := retryStarted.Sub(time.Unix(1000, 0)); elapsed != time.Second {
		t.Fatalf("Retry-After was restarted after close: %v", elapsed)
	}
}

func TestAdaptiveExpiredCooldownStillInvalidatesOldRateGrant(t *testing.T) {
	clock := useAdaptiveTestClock(t)
	c := NewAdaptiveHTTPControl(8, false)
	sleeps := 0
	sleepFn = func(ctx context.Context, d time.Duration) error {
		sleeps++
		if sleeps == 1 {
			c.throttleAdaptive(&adaptiveAttempt{generation: 0}, time.Second)
			clock.Advance(2 * time.Second) // The entire pause passes inside the old rate wait.
		} else {
			clock.Advance(d)
		}
		return ctx.Err()
	}
	client := &http.Client{Transport: controlRoundTripper(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok")), Request: r}, nil
	})}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://fixture.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := adaptiveHTTPAttempt(client, req, 1, time.Time{}, NewRateLimiter(1, 0), c)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	if err := res.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if sleeps != 2 {
		t.Fatalf("stale grant was not reacquired: waits=%d", sleeps)
	}
}

func TestAdaptiveStale429CreatesNewPauseIdentity(t *testing.T) {
	clock := useAdaptiveTestClock(t)
	c := NewAdaptiveHTTPControl(8, false)
	c.throttleAdaptive(&adaptiveAttempt{generation: 0}, time.Second)
	clock.Advance(2 * time.Second)
	epoch := c.pauseEpoch
	generation := c.adaptive.generation
	if err := c.acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.throttleAdaptive(&adaptiveAttempt{generation: 0}, time.Second)
	clock.Advance(2 * time.Second)
	if c.adaptive.generation != generation {
		t.Fatal("stale 429 repeatedly reduced the controller")
	}
	_, ready, err := c.adaptiveDispatch(context.Background(), 0, epoch)
	if err != nil || ready {
		t.Fatalf("stale pause identity accepted: ready=%t error=%v", ready, err)
	}
	c.release()
}

func TestAdaptiveManyWorkersFinishAfterCooldown(t *testing.T) {
	c := NewAdaptiveHTTPControl(8, false)
	c.throttleAdaptive(&adaptiveAttempt{generation: 0}, retryBaseDelay)
	client := &http.Client{Transport: controlRoundTripper(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok")), Request: r}, nil
	})}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 32)
	for range 32 {
		go func() {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://fixture.invalid", nil)
			if err != nil {
				done <- err
				return
			}
			res, err := adaptiveHTTPAttempt(client, req, 1, time.Time{}, nil, c)
			if res != nil {
				_, _ = io.Copy(io.Discard, res.Body)
				_ = res.Body.Close()
			}
			done <- err
		}()
	}
	for range 32 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	c.mu.Lock()
	active := c.inFlight
	c.mu.Unlock()
	if got := c.Snapshot().Admission; got.Reserved != 0 || got.Pending != 0 || active != 0 {
		t.Fatalf("work did not drain: %+v active=%d", got, active)
	}
}

func TestAdaptiveNoMaximumStillLimitsAdmission(t *testing.T) {
	c := NewAdaptiveHTTPControl(0, false)
	if c == nil || c.limit != 8 || c.hardMax != 0 || c.adaptive.maximum != 0 {
		t.Fatalf("unexpected construction: %+v", c)
	}
	for range 8 {
		if err := c.acquire(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.acquire(ctx) }()
	waitForPending(t, c, 1)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	for range 8 {
		c.release()
	}
	got := c.Snapshot()
	if got.Admission.Reserved != 0 || got.Admission.Pending != 0 || got.Admission.PeakReserved != 8 || got.Adaptive.Max != 0 {
		t.Fatalf("snapshot=%+v", got)
	}
}
