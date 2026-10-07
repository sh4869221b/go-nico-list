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

// nextRetryDelay calculates the next backoff delay, honoring Retry-After when larger.
func nextRetryDelay(retryAfter time.Duration, attempt int) time.Duration {
	wait := min(retryBaseDelay*time.Duration(1<<uint(attempt-1)), retryMaxDelay)
	return max(retryAfter, wait)
}

// retriesRequest issues a GET request with retries and rate limiting.
func retriesRequest(ctx context.Context, url string, httpClientTimeout time.Duration, retries int, limiter *RateLimiter, control *HTTPControl) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Frontend-Id", "6")
	req.Header.Set("Accept", "*/*")
	client := &http.Client{Timeout: httpClientTimeout}

	var lastErr error

	delay := time.Duration(0)
	var adaptiveRetryAt time.Time
	for attempt := 1; attempt <= retries; attempt++ {
		var res *http.Response
		var err error
		if control != nil && control.adaptive != nil {
			res, err = adaptiveHTTPAttempt(client, req, attempt, adaptiveRetryAt, limiter, control)
			adaptiveRetryAt = time.Time{}
		} else {
			if err := waitForHTTPAttempt(ctx, limiter, delay, control); err != nil {
				return nil, err
			}
			res, err = performHTTPAttempt(client, req, attempt, control, nil)
		}
		delay = 0
		if err != nil {
			if res != nil {
				_ = res.Body.Close()
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			lastErr = err
		} else {
			if body, ok := res.Body.(*controlledBody); ok && body.adaptive != nil {
				adaptiveRetryAt = body.adaptive.retryAt
			}
			if res.StatusCode == http.StatusOK || res.StatusCode == http.StatusNotFound {
				return res, nil
			}
			delay = retryAfterDelay(res)
			_ = res.Body.Close()
			lastErr = fmt.Errorf("unexpected status: %d", res.StatusCode)
		}

		if attempt == retries {
			return nil, lastErr
		}

		delay = nextRetryDelay(delay, attempt)
		if control != nil && control.adaptive != nil && adaptiveRetryAt.IsZero() {
			adaptiveRetryAt = timeNow().Add(delay)
		}
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
