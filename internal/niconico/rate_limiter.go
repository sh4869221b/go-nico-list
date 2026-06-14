package niconico

import (
	"context"
	"sync"
	"time"
)

// RateLimiter enforces a minimum interval between requests.
type RateLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	nextTime time.Time
}

// NewRateLimiter builds a RateLimiter from rate and minimum interval settings.
func NewRateLimiter(rateLimit float64, minInterval time.Duration) *RateLimiter {
	interval := time.Duration(0)
	if rateLimit > 0 {
		seconds := float64(time.Second) / rateLimit
		if seconds < float64(time.Nanosecond) {
			interval = time.Nanosecond
		} else {
			interval = time.Duration(seconds)
		}
	}
	if minInterval > interval {
		interval = minInterval
	}
	if interval <= 0 {
		return nil
	}
	return &RateLimiter{interval: interval}
}

// Wait blocks until the next request slot is available.
func (l *RateLimiter) Wait(ctx context.Context, minDelay time.Duration) error {
	if l == nil {
		return sleepFn(ctx, minDelay)
	}
	if minDelay < 0 {
		minDelay = 0
	}
	now := timeNow()
	readyAt := now.Add(minDelay)
	l.mu.Lock()
	if l.nextTime.After(readyAt) {
		readyAt = l.nextTime
	}
	l.nextTime = readyAt.Add(l.interval)
	l.mu.Unlock()
	return sleepFn(ctx, readyAt.Sub(now))
}
