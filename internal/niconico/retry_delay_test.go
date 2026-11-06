package niconico

import (
	"math"
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
