package niconico

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

const pageSize = 100

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
	pageConcurrency int,
	logger *slog.Logger,
	control *HTTPControl,
) ([]string, error) {
	return collectVideoList(ctx, commentCount, afterDate, beforeDate, retries, httpClientTimeout, limiter, pageConcurrency, logger,
		func(page int) string {
			return fmt.Sprintf("%s/users/%s/videos?pageSize=%d&page=%d", baseURL, userID, pageSize, page)
		},
		parseUserVideoPage, control,
	)
}

// GetMylistVideoList retrieves video IDs for a mylist.
func GetMylistVideoList(
	ctx context.Context,
	mylistID string,
	commentCount int,
	afterDate time.Time,
	beforeDate time.Time,
	baseURL string,
	retries int,
	httpClientTimeout time.Duration,
	limiter *RateLimiter,
	pageConcurrency int,
	logger *slog.Logger,
	control *HTTPControl,
) ([]string, error) {
	return collectVideoList(ctx, commentCount, afterDate, beforeDate, retries, httpClientTimeout, limiter, pageConcurrency, logger,
		func(page int) string {
			return fmt.Sprintf("%s/mylists/%s?pageSize=%d&page=%d", baseURL, mylistID, pageSize, page)
		},
		parseMylistPage, control,
	)
}
