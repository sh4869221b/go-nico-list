package niconico

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// GetUniqueVideoList retrieves unique video IDs for a user.
func GetUniqueVideoList(
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
	return collectUniqueVideoList(ctx, commentCount, afterDate, beforeDate, retries, httpClientTimeout, limiter, maxPages, maxVideos, logger,
		func(page int) string {
			return fmt.Sprintf("%s/users/%s/videos?pageSize=%d&page=%d", baseURL, userID, pageSize, page)
		},
		parseUserVideoPage,
	)
}

// GetUniqueMylistVideoList retrieves unique video IDs for a mylist.
func GetUniqueMylistVideoList(
	ctx context.Context,
	mylistID string,
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
	return collectUniqueVideoList(ctx, commentCount, afterDate, beforeDate, retries, httpClientTimeout, limiter, maxPages, maxVideos, logger,
		func(page int) string {
			return fmt.Sprintf("%s/mylists/%s?pageSize=%d&page=%d", baseURL, mylistID, pageSize, page)
		},
		parseMylistPage,
	)
}

func collectUniqueVideoList(
	ctx context.Context,
	commentCount int,
	afterDate time.Time,
	beforeDate time.Time,
	retries int,
	httpClientTimeout time.Duration,
	limiter *RateLimiter,
	maxPages int,
	maxVideos int,
	logger *slog.Logger,
	requestURL func(page int) string,
	parsePage parsePageFunc,
) ([]string, error) {
	if logger == nil {
		logger = slog.Default()
	}

	ids := make([]string, 0, maxVideos)
	seen := make(map[string]struct{}, maxVideos)
	for page := 1; ; page++ {
		if maxPages > 0 && page > maxPages {
			return ids, nil
		}
		parsed, err := fetchPage(ctx, requestURL(page), httpClientTimeout, retries, limiter, logger, parsePage)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, nil
			}
			return ids, err
		}
		if parsed.NotFound || len(parsed.Items) == 0 {
			return ids, nil
		}
		for _, id := range filterItems(parsed.Items, commentCount, afterDate, beforeDate) {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
			if maxVideos > 0 && len(ids) >= maxVideos {
				return ids, nil
			}
		}
	}
}
