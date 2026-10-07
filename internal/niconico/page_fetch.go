package niconico

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"
)

type parsedPage struct {
	Items      []videoItem
	Status     int
	TotalCount *int
	NotFound   bool
}

type parsePageFunc func([]byte) (parsedPage, error)

func fetchPage(
	ctx context.Context,
	url string,
	httpClientTimeout time.Duration,
	retries int,
	limiter *RateLimiter,
	logger *slog.Logger,
	parsePage parsePageFunc,
	control *HTTPControl,
) (page parsedPage, retErr error) {
	var metrics *HTTPMetrics
	if control != nil {
		metrics = control.metrics
	}
	metrics.addCounter("pages", 1)
	defer func() {
		if retErr != nil {
			metrics.addCounter("page_errors", 1)
		} else {
			metrics.addCounter("page_success", 1)
		}
	}()
	res, err := retriesRequest(ctx, url, httpClientTimeout, retries, limiter, control)
	if err != nil {
		return parsedPage{}, err
	}
	if res == nil {
		return parsedPage{}, nil
	}
	if res.StatusCode == http.StatusNotFound {
		_ = res.Body.Close()
		return parsedPage{NotFound: true}, nil
	}
	var started time.Time
	if metrics != nil {
		started = time.Now()
	}
	body, err := io.ReadAll(res.Body)
	if metrics != nil {
		metrics.observe("body_read", time.Since(started))
		metrics.addCounter("body_bytes", int64(len(body)))
		if err != nil {
			metrics.addCounter("body_errors", 1)
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				metrics.addCounter("body_cancellations", 1)
			}
		}
	}
	_ = res.Body.Close()
	if err != nil {
		logger.Error("failed to read response body", "error", err)
		return parsedPage{}, err
	}
	if metrics != nil {
		started = time.Now()
	}
	page, err = parsePage(body)
	if metrics != nil {
		metrics.observe("decode", time.Since(started))
		if err != nil {
			metrics.addCounter("decode_errors", 1)
		}
	}
	if err != nil {
		logger.Error("failed to unmarshal response body", "error", err)
		return parsedPage{}, err
	}
	if page.Status != http.StatusOK {
		logger.Warn("unexpected meta status", "status", page.Status)
	}
	return page, nil
}

func filterItems(items []videoItem, commentCount int, afterDate time.Time, beforeDate time.Time) []string {
	ids := make([]string, 0, len(items))
	exclusiveBefore := beforeDate.AddDate(0, 0, 1)
	for _, item := range items {
		if item.CommentCount <= commentCount {
			continue
		}
		if item.RegisteredAt.Before(afterDate) {
			continue
		}
		if !item.RegisteredAt.Before(exclusiveBefore) {
			continue
		}
		ids = append(ids, item.ID)
	}
	return ids
}
