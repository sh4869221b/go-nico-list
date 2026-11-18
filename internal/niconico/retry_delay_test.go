package niconico

import (
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestNextRetryDelayKeepsCapAtLargeAttempts(t *testing.T) {
	for _, attempt := range []int{10, 38, 64, 65, math.MaxInt} {
		if got := nextRetryDelay(0, attempt); got != retryMaxDelay {
			t.Errorf("attempt %d: delay = %v, want %v", attempt, got, retryMaxDelay)
		}
		if got := nextRetryDelay(time.Minute, attempt); got != time.Minute {
			t.Errorf("attempt %d: Retry-After delay = %v, want one minute", attempt, got)
		}
	}
}

func TestRetryAfterOverflowKeepsRequestPacing(t *testing.T) {
	for _, mode := range []string{"fixed", "adaptive"} {
		t.Run(mode, func(t *testing.T) {
			clock := useAdaptiveTestClock(t)
			started := clock.Now()
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if attempts.Add(1) == 1 {
					w.Header().Set("Retry-After", "9223372037")
					w.WriteHeader(http.StatusTooManyRequests)
					return
				}
				_, _ = io.WriteString(w, "ok")
			}))
			t.Cleanup(server.Close)
			control := NewHTTPControl(0, false)
			if mode == "adaptive" {
				control = NewAdaptiveConcurrencyControl(0, false, false)
			}
			res, err := retriesRequest(t.Context(), server.URL, time.Second, 2, nil, control)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = res.Body.Close() }()
			if elapsed := clock.Now().Sub(started); elapsed != time.Duration(math.MaxInt64) || attempts.Load() != 2 {
				t.Fatalf("retry after %v with %d attempts, want %v and 2 attempts", elapsed, attempts.Load(), time.Duration(math.MaxInt64))
			}
		})
	}
}
