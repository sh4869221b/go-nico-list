package niconico

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRetriesRequest(t *testing.T) {
	retries := 3
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Frontend-Id"); got != "6" {
			t.Errorf("unexpected X-Frontend-Id header: %q", got)
		}
		if got := r.Header.Get("Accept"); got != "*/*" {
			t.Errorf("unexpected Accept header: %q", got)
		}
		count++
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(server.Close)

	res, err := retriesRequest(context.Background(), server.URL, time.Second, retries, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", res.StatusCode)
	}
	if count != 3 {
		t.Errorf("expected 3 attempts, got %d", count)
	}
	_ = res.Body.Close()
}

func TestRetriesRequestExhaustedReturnsError(t *testing.T) {
	retries := 1
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	res, err := retriesRequest(context.Background(), server.URL, time.Second, retries, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if res != nil {
		_ = res.Body.Close()
		t.Errorf("expected nil response, got %v", res)
	}
	if count != retries {
		t.Errorf("expected %d attempts, got %d", retries, count)
	}
}

func TestRetriesRequestBackoffCanceled(t *testing.T) {
	retries := 3
	count := 0
	handled := make(chan struct{})
	var handledOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.WriteHeader(http.StatusInternalServerError)
		handledOnce.Do(func() { close(handled) })
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		res, err := retriesRequest(ctx, server.URL, time.Second, retries, nil, nil)
		if res != nil {
			_ = res.Body.Close()
		}
		errCh <- err
	}()

	select {
	case <-handled:
	case <-time.After(time.Second):
		t.Fatal("expected request to be handled")
	}

	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("expected retriesRequest to return after cancel")
	}
	if count != 1 {
		t.Errorf("expected 1 attempt, got %d", count)
	}
}

func TestRetriesRequestContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := retriesRequest(ctx, "http://example.com", time.Second, 3, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if res != nil {
		_ = res.Body.Close()
		t.Errorf("expected nil response, got %v", res)
	}
}

func TestRetriesRequestTimeout(t *testing.T) {
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		close(done)
	}))
	t.Cleanup(server.Close)

	timeout := 50 * time.Millisecond
	res, err := retriesRequest(context.Background(), server.URL, timeout, 3, nil, nil)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
	if res != nil {
		_ = res.Body.Close()
		t.Errorf("expected nil response, got %v", res)
	}

	waitTimeout := time.Second
	if deadline, ok := t.Deadline(); ok {
		if remaining := time.Until(deadline) / 2; remaining > 0 {
			waitTimeout = remaining
		}
	}

	select {
	case <-done:
	case <-time.After(waitTimeout):
		t.Fatal("expected handler to observe timeout")
	}
}

func TestNewRateLimiterInterval(t *testing.T) {
	tests := []struct {
		name        string
		rateLimit   float64
		minInterval time.Duration
		wantNil     bool
		want        time.Duration
	}{
		{
			name:        "disabled",
			rateLimit:   0,
			minInterval: 0,
			wantNil:     true,
		},
		{
			name:        "rate-limit only",
			rateLimit:   2.5,
			minInterval: 0,
			want:        time.Duration(float64(time.Second) / 2.5),
		},
		{
			name:        "min-interval only",
			rateLimit:   0,
			minInterval: 150 * time.Millisecond,
			want:        150 * time.Millisecond,
		},
		{
			name:        "min-interval dominates",
			rateLimit:   10,
			minInterval: 200 * time.Millisecond,
			want:        200 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limiter := NewRateLimiter(tt.rateLimit, tt.minInterval)
			if tt.wantNil {
				if limiter != nil {
					t.Fatalf("expected nil limiter, got %+v", limiter)
				}
				return
			}
			if limiter == nil {
				t.Fatal("expected limiter, got nil")
			} else if limiter.interval != tt.want {
				t.Errorf("expected interval %v, got %v", tt.want, limiter.interval)
			}
		})
	}
}

func TestRateLimiterWaitSequence(t *testing.T) {
	for _, tc := range []struct {
		name            string
		delays, elapsed []time.Duration
	}{
		{name: "sequence", delays: []time.Duration{0, 0}, elapsed: []time.Duration{0, 50 * time.Millisecond}},
		{name: "minimum delay", delays: []time.Duration{120 * time.Millisecond}, elapsed: []time.Duration{120 * time.Millisecond}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			origNow, origSleep := timeNow, sleepFn
			base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
			current := base
			timeNow = func() time.Time { return current }
			sleepFn = func(ctx context.Context, d time.Duration) error {
				current = current.Add(d)
				return nil
			}
			t.Cleanup(func() { timeNow, sleepFn = origNow, origSleep })
			limiter := &RateLimiter{interval: 50 * time.Millisecond}
			for i, delay := range tc.delays {
				if err := limiter.Wait(context.Background(), delay); err != nil {
					t.Fatal(err)
				}
				if got := current.Sub(base); got != tc.elapsed[i] {
					t.Fatalf("wait %d: elapsed %v, want %v", i, got, tc.elapsed[i])
				}
			}
		})
	}
}

func TestRateLimiterWaitConcurrent(t *testing.T) {
	origNow := timeNow
	origSleep := sleepFn
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	timeNow = func() time.Time { return base }
	var mu sync.Mutex
	delays := make([]time.Duration, 0, 5)
	sleepFn = func(ctx context.Context, d time.Duration) error {
		mu.Lock()
		delays = append(delays, d)
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() {
		timeNow = origNow
		sleepFn = origSleep
	})

	limiter := &RateLimiter{interval: 10 * time.Millisecond}
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := limiter.Wait(context.Background(), 0); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	if len(delays) != 5 {
		t.Fatalf("expected 5 delays, got %d", len(delays))
	}
	slices.Sort(delays)
	for i, d := range delays {
		want := time.Duration(i) * 10 * time.Millisecond
		if d != want {
			t.Fatalf("expected delay %v, got %v", want, d)
		}
	}
}

func TestRetryAfterDelay(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	origNow := timeNow
	timeNow = func() time.Time { return base }
	t.Cleanup(func() { timeNow = origNow })

	tests := []struct {
		name   string
		res    *http.Response
		header string
		want   time.Duration
	}{
		{
			name: "nil response",
			res:  nil,
			want: 0,
		},
		{
			name: "non-429 ignored",
			res:  &http.Response{StatusCode: http.StatusInternalServerError, Header: make(http.Header)},
			want: 0,
		},
		{
			name: "empty header",
			res:  &http.Response{StatusCode: http.StatusTooManyRequests, Header: make(http.Header)},
			want: 0,
		},
		{
			name:   "invalid header",
			res:    &http.Response{StatusCode: http.StatusTooManyRequests, Header: make(http.Header)},
			header: "invalid",
			want:   0,
		},
		{
			name:   "negative seconds",
			res:    &http.Response{StatusCode: http.StatusTooManyRequests, Header: make(http.Header)},
			header: "-5",
			want:   0,
		},
		{
			name:   "seconds header",
			res:    &http.Response{StatusCode: http.StatusTooManyRequests, Header: make(http.Header)},
			header: "120",
			want:   120 * time.Second,
		},
		{
			name:   "http date header",
			res:    &http.Response{StatusCode: http.StatusTooManyRequests, Header: make(http.Header)},
			header: base.Add(90 * time.Second).UTC().Format(http.TimeFormat),
			want:   90 * time.Second,
		},
		{
			name:   "past date header",
			res:    &http.Response{StatusCode: http.StatusTooManyRequests, Header: make(http.Header)},
			header: base.Add(-30 * time.Second).UTC().Format(http.TimeFormat),
			want:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.res != nil && tt.header != "" {
				tt.res.Header.Set("Retry-After", tt.header)
			}
			if got := retryAfterDelay(tt.res); got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestGetVideoList(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		logger := slog.New(slog.DiscardHandler)
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			page := r.URL.Query().Get("page")
			var resp string
			switch page {
			case "1":
				resp = `{"data":{"items":[{"essential":{"id":"sm1","registeredAt":"2024-01-01T00:00:00Z","count":{"comment":10}}},{"essential":{"id":"sm2","registeredAt":"2024-01-15T00:00:00Z","count":{"comment":5}}}]}}`
			case "2":
				resp = `{"data":{"items":[{"essential":{"id":"sm3","registeredAt":"2024-04-30T00:00:00Z","count":{"comment":20}}},{"essential":{"id":"sm4","registeredAt":"2024-05-01T00:00:00Z","count":{"comment":30}}}]}}`
			default:
				resp = `{"data":{"items":[]}}`
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(resp))
		})
		server := httptest.NewServer(handler)
		t.Cleanup(server.Close)

		after := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		before := time.Date(2024, 4, 30, 0, 0, 0, 0, time.UTC)

		got, err := GetVideoList(context.Background(), "12345", 5, after, before, server.URL, 1, time.Second, nil, 1, logger, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := []string{"sm1", "sm3"}
		if !slices.Equal(got, expected) {
			t.Errorf("expected %v, got %v", expected, got)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		logger := slog.New(slog.DiscardHandler)
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, "invalid")
		})
		server := httptest.NewServer(handler)
		t.Cleanup(server.Close)

		after := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		before := time.Date(2024, 4, 30, 0, 0, 0, 0, time.UTC)

		_, err := GetVideoList(context.Background(), "12345", 5, after, before, server.URL, 1, time.Second, nil, 1, logger, nil)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
	})
}

func TestGetVideoListContextCanceled(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	after := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2024, 4, 30, 0, 0, 0, 0, time.UTC)

	got, err := GetVideoList(ctx, "12345", 0, after, before, "http://fixture.invalid", 1, time.Second, nil, 1, logger, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty result, got %v", got)
	}
}

func TestGetVideoListHandleNotFound(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	count := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"data":{"items":[]}}`)
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	after := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2024, 4, 30, 0, 0, 0, 0, time.UTC)

	got, err := GetVideoList(context.Background(), "12345", 0, after, before, server.URL, 1, time.Second, nil, 1, logger, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty result, got %v", got)
	}
	if count != 1 {
		t.Errorf("expected 1 attempt, got %d", count)
	}
}

func TestGetVideoListHandleServerError(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	count := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"data":{"items":[]}}`)
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	after := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2024, 4, 30, 0, 0, 0, 0, time.UTC)

	_, err := GetVideoList(context.Background(), "12345", 0, after, before, server.URL, 2, time.Second, nil, 1, logger, nil)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if count != 2 {
		t.Errorf("expected 2 attempts, got %d", count)
	}
}

func TestGetVideoListPartialOnError(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		switch page {
		case "1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":{"items":[{"essential":{"id":"sm1","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}}]}}`)
		default:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, "invalid")
		}
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	after := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2024, 4, 30, 0, 0, 0, 0, time.UTC)

	got, err := GetVideoList(context.Background(), "12345", 0, after, before, server.URL, 1, time.Second, nil, 1, logger, nil)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !slices.Equal(got, []string{"sm1"}) {
		t.Errorf("expected partial result, got %v", got)
	}
}

func TestGetVideoListPageConcurrencyReturnsPartialIDsOnFetchError(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"totalCount":300,"items":[{"essential":{"id":"sm1","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}}]}}`)
		case "2":
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"totalCount":300,"items":[{"essential":{"id":"sm2","registeredAt":"2024-01-11T00:00:00Z","count":{"comment":10}}}]}}`)
		default:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, "invalid")
		}
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	after := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2024, 4, 30, 0, 0, 0, 0, time.UTC)

	got, err := GetVideoList(context.Background(), "12345", 0, after, before, server.URL, 1, time.Second, nil, 2, logger, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !slices.Equal(got, []string{"sm1", "sm2"}) {
		t.Fatalf("unexpected partial ids: %v", got)
	}
}

func TestGetMylistVideoList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/mylists/847130") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("page") != "1" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"mylist":{"items":[]}}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"mylist":{"items":[{"video":{"id":"sm9","registeredAt":"2025-01-02T03:04:05Z","count":{"comment":12}}}]}}}`)
	}))
	t.Cleanup(server.Close)

	ids, err := GetMylistVideoList(
		context.Background(),
		"847130",
		0,
		time.Date(1000, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC),
		server.URL,
		1,
		time.Second, nil, 1, slog.New(slog.DiscardHandler), nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !slices.Equal(ids, []string{"sm9"}) {
		t.Fatalf("unexpected ids: %v", ids)
	}
}
