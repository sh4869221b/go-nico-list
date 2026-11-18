package niconico

import (
	"context"
	"errors"
	"fmt"
	"math"
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
	wait := retryBaseDelay
	for i := 1; i < attempt && wait < retryMaxDelay; i++ {
		wait = min(wait*2, retryMaxDelay)
	}
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
	adaptive := control != nil && control.adaptive != nil

	var lastErr error
	var delay time.Duration
	var adaptiveRetryAt time.Time
	for attempt := 1; attempt <= retries; attempt++ {
		var res *http.Response
		if adaptive {
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
		if adaptive && adaptiveRetryAt.IsZero() {
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
	digits := strings.TrimPrefix(value, "+")
	if seconds, err := strconv.ParseUint(digits, 10, 64); err == nil {
		if seconds > uint64(math.MaxInt64/time.Second) {
			return time.Duration(math.MaxInt64)
		}
		return time.Duration(seconds) * time.Second
	} else if errors.Is(err, strconv.ErrRange) && strings.Trim(digits, "0123456789") == "" {
		return time.Duration(math.MaxInt64)
	}
	if parsed, err := http.ParseTime(value); err == nil {
		if delay := parsed.Sub(timeNow()); delay > 0 {
			return delay
		}
	}
	return 0
}
