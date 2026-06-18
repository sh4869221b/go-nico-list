package niconico

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// VisitVideoList visits filtered raw video IDs for each user page until the visitor stops.
func VisitVideoList(
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
	visitor func([]string) bool,
	logger *slog.Logger,
) error {
	return visitVideoListPages(ctx, commentCount, afterDate, beforeDate, retries, httpClientTimeout, limiter, maxPages, visitor, logger,
		func(page int) string {
			return fmt.Sprintf("%s/users/%s/videos?pageSize=%d&page=%d", baseURL, userID, pageSize, page)
		},
		parseUserVideoPage,
	)
}

// VisitMylistVideoList visits filtered raw video IDs for each mylist page until the visitor stops.
func VisitMylistVideoList(
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
	visitor func([]string) bool,
	logger *slog.Logger,
) error {
	return visitVideoListPages(ctx, commentCount, afterDate, beforeDate, retries, httpClientTimeout, limiter, maxPages, visitor, logger,
		func(page int) string {
			return fmt.Sprintf("%s/mylists/%s?pageSize=%d&page=%d", baseURL, mylistID, pageSize, page)
		},
		parseMylistPage,
	)
}

// visitVideoListPages visits filtered raw IDs in page order.
func visitVideoListPages(
	ctx context.Context,
	commentCount int,
	afterDate time.Time,
	beforeDate time.Time,
	retries int,
	httpClientTimeout time.Duration,
	limiter *RateLimiter,
	maxPages int,
	visitor func([]string) bool,
	logger *slog.Logger,
	requestURL func(page int) string,
	parsePage parsePageFunc,
) error {
	if logger == nil {
		logger = slog.Default()
	}
	for page := 1; maxPages == 0 || page <= maxPages; page++ {
		parsed, err := fetchPage(ctx, requestURL(page), httpClientTimeout, retries, limiter, logger, parsePage)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			return err
		}
		if parsed.NotFound || len(parsed.Items) == 0 {
			return nil
		}
		ids := filterItems(parsed.Items, commentCount, afterDate, beforeDate)
		if len(ids) > 0 && !visitor(ids) {
			return nil
		}
	}
	return nil
}
