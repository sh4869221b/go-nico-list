package niconico

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"
)

type trackingReadCloser struct {
	closed bool
}

func (r *trackingReadCloser) Read(_ []byte) (int, error) {
	return 0, io.EOF
}

func (r *trackingReadCloser) Close() error {
	r.closed = true
	return nil
}

func TestNiconicoSort(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "simple",
			input:    []string{"sm12", "sm3", "sm1"},
			expected: []string{"sm1", "sm3", "sm12"},
		},
		{
			name:     "shortString",
			input:    []string{"sm12", "s", "sm3"},
			expected: []string{"sm3", "s", "sm12"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			slice := append([]string(nil), tt.input...)
			NiconicoSort(slice)
			if !reflect.DeepEqual(slice, tt.expected) {
				t.Errorf("%s: expected %v, got %v", tt.name, tt.expected, slice)
			}
		})
	}
}

func TestCloseAndIsNotFound(t *testing.T) {
	t.Run("not found closes body", func(t *testing.T) {
		body := &trackingReadCloser{}
		res := &http.Response{StatusCode: http.StatusNotFound, Body: body}
		if !closeAndIsNotFound(res) {
			t.Fatal("expected true")
		}
		if !body.closed {
			t.Fatal("expected body to be closed")
		}
	})

	t.Run("non-404 does not close body", func(t *testing.T) {
		body := &trackingReadCloser{}
		res := &http.Response{StatusCode: http.StatusOK, Body: body}
		if closeAndIsNotFound(res) {
			t.Fatal("expected false")
		}
		if body.closed {
			t.Fatal("expected body to remain open")
		}
	})
}

func TestRetriesRequest(t *testing.T) {
	retries := 3
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(server.Close)

	res, err := retriesRequest(context.Background(), server.URL, time.Second, retries, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", res.StatusCode)
	}
	if count != 3 {
		t.Errorf("expected 3 attempts, got %d", count)
	}
	res.Body.Close()
}

func TestRetriesRequestExhaustedReturnsError(t *testing.T) {
	retries := 1
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	res, err := retriesRequest(context.Background(), server.URL, time.Second, retries, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if res != nil {
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
		_, err := retriesRequest(ctx, server.URL, time.Second, retries, nil)
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
	res, err := retriesRequest(ctx, "http://example.com", time.Second, 3, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if res != nil {
		t.Errorf("expected nil response, got %v", res)
	}
}

func TestRetriesRequestTimeout(t *testing.T) {
	started := make(chan struct{})
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(done)
	}))
	t.Cleanup(server.Close)

	timeout := 50 * time.Millisecond
	res, err := retriesRequest(context.Background(), server.URL, timeout, 3, nil)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
	if res != nil {
		t.Errorf("expected nil response, got %v", res)
	}

	waitTimeout := time.Second
	if deadline, ok := t.Deadline(); ok {
		if remaining := time.Until(deadline) / 2; remaining > 0 {
			waitTimeout = remaining
		}
	}

	select {
	case <-started:
	case <-time.After(waitTimeout):
		t.Fatal("expected request to start")
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
		tt := tt
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
			}
			if limiter.interval != tt.want {
				t.Errorf("expected interval %v, got %v", tt.want, limiter.interval)
			}
		})
	}
}

func TestRateLimiterWaitSequence(t *testing.T) {
	origNow := timeNow
	origSleep := sleepFn
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	current := base
	timeNow = func() time.Time { return current }
	sleepFn = func(ctx context.Context, d time.Duration) error {
		current = current.Add(d)
		return nil
	}
	t.Cleanup(func() {
		timeNow = origNow
		sleepFn = origSleep
	})

	limiter := &RateLimiter{interval: 50 * time.Millisecond}

	if err := limiter.Wait(context.Background(), 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !current.Equal(base) {
		t.Fatalf("expected no delay, got %v", current.Sub(base))
	}
	if err := limiter.Wait(context.Background(), 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := current.Sub(base); got != 50*time.Millisecond {
		t.Fatalf("expected delay 50ms, got %v", got)
	}
}

func TestRateLimiterWaitHonorsMinDelay(t *testing.T) {
	origNow := timeNow
	origSleep := sleepFn
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	current := base
	timeNow = func() time.Time { return current }
	sleepFn = func(ctx context.Context, d time.Duration) error {
		current = current.Add(d)
		return nil
	}
	t.Cleanup(func() {
		timeNow = origNow
		sleepFn = origSleep
	})

	limiter := &RateLimiter{interval: 50 * time.Millisecond}
	if err := limiter.Wait(context.Background(), 120*time.Millisecond); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := current.Sub(base); got != 120*time.Millisecond {
		t.Fatalf("expected delay 120ms, got %v", got)
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
		tt := tt
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
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			page := r.URL.Query().Get("page")
			var resp string
			switch page {
			case "1":
				resp = `{"data":{"items":[{"essential":{"id":"sm1","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}},{"essential":{"id":"sm2","registeredAt":"2024-01-15T00:00:00Z","count":{"comment":3}}}]}}`
			case "2":
				resp = `{"data":{"items":[{"essential":{"id":"sm3","registeredAt":"2024-02-10T00:00:00Z","count":{"comment":20}}},{"essential":{"id":"sm4","registeredAt":"2024-05-02T00:00:00Z","count":{"comment":30}}}]}}`
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

		got, err := GetVideoList(context.Background(), "12345", 5, after, before, server.URL, 1, time.Second, nil, 0, 0, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := []string{"sm1", "sm3"}
		if !reflect.DeepEqual(got, expected) {
			t.Errorf("expected %v, got %v", expected, got)
		}
	})

	t.Run("before date is inclusive and next day is excluded", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Query().Get("page") != "1" {
				_, _ = io.WriteString(w, `{"data":{"items":[]}}`)
				return
			}
			_, _ = io.WriteString(w, `{"data":{"items":[{"essential":{"id":"sm1","registeredAt":"2024-04-30T00:00:00Z","count":{"comment":10}}},{"essential":{"id":"sm2","registeredAt":"2024-05-01T00:00:00Z","count":{"comment":10}}}]}}`)
		})
		server := httptest.NewServer(handler)
		t.Cleanup(server.Close)

		after := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		before := time.Date(2024, 4, 30, 0, 0, 0, 0, time.UTC)

		got, err := GetVideoList(context.Background(), "12345", 0, after, before, server.URL, 1, time.Second, nil, 0, 0, logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !reflect.DeepEqual(got, []string{"sm1"}) {
			t.Errorf("expected [sm1], got %v", got)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, "invalid")
		})
		server := httptest.NewServer(handler)
		t.Cleanup(server.Close)

		after := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		before := time.Date(2024, 4, 30, 0, 0, 0, 0, time.UTC)

		_, err := GetVideoList(context.Background(), "12345", 5, after, before, server.URL, 1, time.Second, nil, 0, 0, logger)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
	})
}

func TestGetVideoListContextCanceled(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"items":[]}}`)
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	after := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2024, 4, 30, 0, 0, 0, 0, time.UTC)

	got, err := GetVideoList(ctx, "12345", 0, after, before, server.URL, 1, time.Second, nil, 0, 0, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty result, got %v", got)
	}
}

func TestGetVideoListHandleNotFound(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
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

	got, err := GetVideoList(context.Background(), "12345", 0, after, before, server.URL, 1, time.Second, nil, 0, 0, logger)
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
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
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

	_, err := GetVideoList(context.Background(), "12345", 0, after, before, server.URL, 2, time.Second, nil, 0, 0, logger)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if count != 2 {
		t.Errorf("expected 2 attempts, got %d", count)
	}
}

func TestGetVideoListPartialOnError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
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

	got, err := GetVideoList(context.Background(), "12345", 0, after, before, server.URL, 1, time.Second, nil, 0, 0, logger)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !reflect.DeepEqual(got, []string{"sm1"}) {
		t.Errorf("expected partial result, got %v", got)
	}
}

func TestGetVideoListMaxPagesStopsEarly(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	count := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "1" {
			_, _ = io.WriteString(w, `{"data":{"items":[{"essential":{"id":"sm1","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}}]}}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":{"items":[{"essential":{"id":"sm2","registeredAt":"2024-01-11T00:00:00Z","count":{"comment":10}}}]}}`)
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	after := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2024, 4, 30, 0, 0, 0, 0, time.UTC)

	got, err := GetVideoList(context.Background(), "12345", 0, after, before, server.URL, 1, time.Second, nil, 1, 0, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"sm1"}) {
		t.Errorf("expected [sm1], got %v", got)
	}
	if count != 1 {
		t.Errorf("expected 1 attempt, got %d", count)
	}
}

func TestGetVideoListMaxVideosStopsEarly(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	count := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"items":[{"essential":{"id":"sm1","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}},{"essential":{"id":"sm2","registeredAt":"2024-01-11T00:00:00Z","count":{"comment":10}}},{"essential":{"id":"sm3","registeredAt":"2024-01-12T00:00:00Z","count":{"comment":10}}}]}}`)
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	after := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2024, 4, 30, 0, 0, 0, 0, time.UTC)

	got, err := GetVideoList(context.Background(), "12345", 0, after, before, server.URL, 1, time.Second, nil, 0, 2, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"sm1", "sm2"}) {
		t.Errorf("expected [sm1 sm2], got %v", got)
	}
	if count != 1 {
		t.Errorf("expected 1 attempt, got %d", count)
	}
}
