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
	tabStr = "\t\t\t\t\t\t\t\t\t"
	urlStr = "https://www.nicovideo.jp/watch/"
)

var (
	timeNow = time.Now
	sleepFn = sleepWithContext
)

// NiconicoSort sorts video IDs by their numeric part in ascending order, ignoring any preceding tab or URL strings.
func NiconicoSort(slice []string, tab bool, url bool) {
	var num = 2
	if tab {
		num += len(tabStr)
	}
	if url {
		num += len(urlStr)
	}
	str := "%08s"

	sort.Slice(slice, func(i, j int) bool {
		var s1, s2 string
		if len(slice[i]) >= num {
			s1 = slice[i][num:]
		} else {
			s1 = slice[i]
		}
		if len(slice[j]) >= num {
			s2 = slice[j][num:]
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
	tab bool,
	url bool,
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

	var beforeStr = ""
	if tab {
		beforeStr += tabStr
	}
	if url {
		beforeStr += urlStr
	}

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
			if res.StatusCode == http.StatusNotFound {
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
				resStr = append(resStr, fmt.Sprintf("%s%s", beforeStr, s.Essential.ID))
				if maxVideos > 0 && len(resStr) >= maxVideos {
					return resStr, nil
				}
			}
		}
	}
	return resStr, nil
}

func retriesRequest(ctx context.Context, url string, httpClientTimeout time.Duration, retries int, limiter *RateLimiter) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Frontend-Id", "6")
	req.Header.Set("Accept", "*/*")
	client := &http.Client{Timeout: httpClientTimeout}

	var lastErr error
	const baseDelay = 100 * time.Millisecond
	const maxDelay = 30 * time.Second

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
			if res.StatusCode == http.StatusOK || res.StatusCode == http.StatusNotFound {
				return res, nil
			}
			retryAfter := retryAfterDelay(res)
			res.Body.Close()
			lastErr = fmt.Errorf("unexpected status: %d", res.StatusCode)
			if retryAfter > 0 {
				delay = retryAfter
			}
		}

		if attempt == retries {
			return nil, lastErr
		}

		wait := baseDelay * time.Duration(1<<uint(attempt-1))
		if wait > maxDelay {
			wait = maxDelay
		}
		if wait > delay {
			delay = wait
		}
	}

	return nil, lastErr
}

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

func waitBeforeAttempt(ctx context.Context, limiter *RateLimiter, delay time.Duration) error {
	if limiter == nil {
		return sleepFn(ctx, delay)
	}
	return limiter.Wait(ctx, delay)
}

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

type RateLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	nextTime time.Time
}

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
