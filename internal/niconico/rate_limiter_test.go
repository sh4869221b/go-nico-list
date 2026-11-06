package niconico

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

func TestNewRateLimiterSaturatesOverflow(t *testing.T) {
	for _, rate := range []float64{1e-10, math.SmallestNonzeroFloat64, float64(time.Second) / float64(math.MaxInt64)} {
		limiter := NewRateLimiter(rate, 0)
		if limiter == nil {
			t.Fatalf("rate %g disabled the limiter", rate)
		}
		if limiter.interval != time.Duration(math.MaxInt64) {
			t.Fatalf("rate %g: interval = %v, want maximum duration", rate, limiter.interval)
		}
		if delay := limiter.reserveDelay(0); delay != 0 {
			t.Fatalf("first request delay = %v, want zero", delay)
		}
		if delay := limiter.reserveDelay(0); delay <= 0 {
			t.Fatalf("second request delay = %v, want positive", delay)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := limiter.Wait(ctx, 0); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled rate wait: %v", err)
		}
	}
}
