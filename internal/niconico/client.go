package niconico

import (
	"context"
	"log/slog"
	"strconv"
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
	pageURLPrefix := baseURL + "/users/" + userID + "/videos?pageSize=" + strconv.Itoa(pageSize) + "&page="
	return collectVideoList(ctx, commentCount, afterDate, beforeDate, retries, httpClientTimeout, limiter, pageConcurrency, logger, pageURLPrefix, parseUserVideoPage, control)
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
	pageURLPrefix := baseURL + "/mylists/" + mylistID + "?pageSize=" + strconv.Itoa(pageSize) + "&page="
	return collectVideoList(ctx, commentCount, afterDate, beforeDate, retries, httpClientTimeout, limiter, pageConcurrency, logger, pageURLPrefix, parseMylistPage, control)
}
