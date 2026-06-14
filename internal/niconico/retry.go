package niconico

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	retryBaseDelay = 100 * time.Millisecond
	retryMaxDelay  = 30 * time.Second
)

var (
	timeNow = time.Now
	sleepFn = sleepWithContext
)

// closeAndIsNotFound closes the response body and reports whether the status is 404.
func closeAndIsNotFound(res *http.Response) bool {
	if res == nil || res.StatusCode != http.StatusNotFound {
		return false
	}
	_ = res.Body.Close()
	return true
}

// evaluateResponse validates HTTP responses and returns any retry delay needed.
func evaluateResponse(res *http.Response) (*http.Response, time.Duration, error) {
	if res.StatusCode == http.StatusOK || res.StatusCode == http.StatusNotFound {
		return res, 0, nil
	}
	retryAfter := retryAfterDelay(res)
	_ = res.Body.Close()
	return nil, retryAfter, fmt.Errorf("unexpected status: %d", res.StatusCode)
}

// nextRetryDelay calculates the next backoff delay, honoring Retry-After when larger.
func nextRetryDelay(retryAfter time.Duration, attempt int) time.Duration {
	wait := min(retryBaseDelay*time.Duration(1<<uint(attempt-1)), retryMaxDelay)
	return max(retryAfter, wait)
}

// retriesRequest issues a GET request with retries and rate limiting.
func retriesRequest(ctx context.Context, url string, httpClientTimeout time.Duration, retries int, limiter *RateLimiter) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Frontend-Id", "6")
	req.Header.Set("Accept", "*/*")
	client := &http.Client{Timeout: httpClientTimeout}

	var lastErr error

	delay := time.Duration(0)
	for attempt := 1; attempt <= retries; attempt++ {
		if err := waitBeforeAttempt(ctx, limiter, delay); err != nil {
			return nil, err
		}
		delay = 0

		res, err := client.Do(req)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				if res != nil {
					_ = res.Body.Close()
				}
				return nil, err
			}
			if res != nil {
				_ = res.Body.Close()
			}
			lastErr = err
		} else {
			var retryAfter time.Duration
			res, retryAfter, err = evaluateResponse(res)
			if err == nil {
				return res, nil
			}
			lastErr = err
			delay = retryAfter
		}

		if attempt == retries {
			return nil, lastErr
		}

		delay = nextRetryDelay(delay, attempt)
	}

	return nil, lastErr
}

// sleepWithContext waits for the duration or returns early on context cancellation.
func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// waitBeforeAttempt applies any delay and rate limiting before a request attempt.
func waitBeforeAttempt(ctx context.Context, limiter *RateLimiter, delay time.Duration) error {
	if limiter == nil {
		return sleepFn(ctx, delay)
	}
	return limiter.Wait(ctx, delay)
}

// retryAfterDelay parses Retry-After for 429 responses and returns a delay.
func retryAfterDelay(res *http.Response) time.Duration {
	if res == nil || res.StatusCode != http.StatusTooManyRequests {
		return 0
	}
	value := strings.TrimSpace(res.Header.Get("Retry-After"))
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	if parsed, err := http.ParseTime(value); err == nil {
		if delay := parsed.Sub(timeNow()); delay > 0 {
			return delay
		}
	}
	return 0
}
