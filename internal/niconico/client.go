package niconico

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
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

// NiconicoSort sorts video IDs by their numeric part in ascending order.
func NiconicoSort(slice []string) {
	const prefixLen = 2
	str := "%08s"

	sort.Slice(slice, func(i, j int) bool {
		var s1, s2 string
		if len(slice[i]) >= prefixLen {
			s1 = slice[i][prefixLen:]
		} else {
			s1 = slice[i]
		}
		if len(slice[j]) >= prefixLen {
			s2 = slice[j][prefixLen:]
		} else {
			s2 = slice[j]
		}
		return fmt.Sprintf(str, s1) < fmt.Sprintf(str, s2)
	})
}

// GetVideoList retrieves video IDs for a user.
func GetVideoList(
	ctx context.Context,
	userID string,
	commentCount int,
	afterDate time.Time,
	beforeDate time.Time,
	baseURL string,
	retries int,
	httpClientTimeout time.Duration,
	limiter *RateLimiter,
	maxPages int,
	maxVideos int,
	logger *slog.Logger,
) ([]string, error) {
	if logger == nil {
		logger = slog.Default()
	}

	var resStr []string

	for page := 1; ; page++ {
		if maxPages > 0 && page > maxPages {
			break
		}
		requestURL := fmt.Sprintf("%s/users/%s/videos?pageSize=100&page=%d", baseURL, userID, page)
		res, err := retriesRequest(ctx, requestURL, httpClientTimeout, retries, limiter)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, nil
			}
			return resStr, err
		}
		if res != nil {
			if closeAndIsNotFound(res) {
				break
			}
			body, err := io.ReadAll(res.Body)
			_ = res.Body.Close()
			if err != nil {
				logger.Error("failed to read response body", "error", err)
				return resStr, err
			}

			var nicoData NicoData
			if err := json.Unmarshal(body, &nicoData); err != nil {
				logger.Error("failed to unmarshal response body", "error", err)
				return resStr, err
			}
			if nicoData.Meta.Status != http.StatusOK {
				logger.Warn("unexpected meta status", "status", nicoData.Meta.Status)
			}
			if len(nicoData.Data.Items) == 0 {
				break
			}
			for _, s := range nicoData.Data.Items {
				if s.Essential.Count.Comment <= commentCount {
					continue
				}
				if s.Essential.RegisteredAt.Before(afterDate) {
					continue
				}
				if !s.Essential.RegisteredAt.Before(beforeDate.AddDate(0, 0, 1)) {
					continue
				}
				resStr = append(resStr, s.Essential.ID)
				if maxVideos > 0 && len(resStr) >= maxVideos {
					return resStr, nil
				}
			}
		}
	}
	return resStr, nil
}

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
	res.Body.Close()
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
					res.Body.Close()
				}
				return nil, err
			}
			if res != nil {
				res.Body.Close()
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
