package niconico

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"
)

type parsedPage struct {
	Items           []videoItem
	Status          int
	TotalCount      int
	TotalCountKnown bool
	NotFound        bool
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
) (parsedPage, error) {
	res, err := retriesRequest(ctx, url, httpClientTimeout, retries, limiter)
	if err != nil {
		return parsedPage{}, err
	}
	if res == nil {
		return parsedPage{}, nil
	}
	if closeAndIsNotFound(res) {
		return parsedPage{NotFound: true}, nil
	}
	body, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil {
		logger.Error("failed to read response body", "error", err)
		return parsedPage{}, err
	}
	page, err := parsePage(body)
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
