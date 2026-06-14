package niconico

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const pageSize = 100

type videoItem struct {
	ID           string
	CommentCount int
	RegisteredAt time.Time
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
	pageConcurrency int,
	logger *slog.Logger,
) ([]string, error) {
	return collectVideoList(ctx, commentCount, afterDate, beforeDate, retries, httpClientTimeout, limiter, maxPages, maxVideos, pageConcurrency, logger,
		func(page int) string {
			return fmt.Sprintf("%s/users/%s/videos?pageSize=%d&page=%d", baseURL, userID, pageSize, page)
		},
		parseUserVideoPage,
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
	maxPages int,
	maxVideos int,
	pageConcurrency int,
	logger *slog.Logger,
) ([]string, error) {
	return collectVideoList(ctx, commentCount, afterDate, beforeDate, retries, httpClientTimeout, limiter, maxPages, maxVideos, pageConcurrency, logger,
		func(page int) string {
			return fmt.Sprintf("%s/mylists/%s?pageSize=%d&page=%d", baseURL, mylistID, pageSize, page)
		},
		parseMylistPage,
	)
}

func parseUserVideoPage(body []byte) (parsedPage, error) {
	var nicoData NicoData
	if err := json.Unmarshal(body, &nicoData); err != nil {
		return parsedPage{}, err
	}
	var countPayload struct {
		Data struct {
			TotalCount *int `json:"totalCount"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &countPayload); err != nil {
		return parsedPage{}, err
	}
	items := make([]videoItem, 0, len(nicoData.Data.Items))
	for _, s := range nicoData.Data.Items {
		items = append(items, videoItem{ID: s.Essential.ID, CommentCount: s.Essential.Count.Comment, RegisteredAt: s.Essential.RegisteredAt})
	}
	totalCount := 0
	if countPayload.Data.TotalCount != nil {
		totalCount = *countPayload.Data.TotalCount
	}
	return parsedPage{
		Items:           items,
		Status:          nicoData.Meta.Status,
		TotalCount:      totalCount,
		TotalCountKnown: countPayload.Data.TotalCount != nil,
	}, nil
}

func parseMylistPage(body []byte) (parsedPage, error) {
	var payload struct {
		Meta struct {
			Status int `json:"status"`
		} `json:"meta"`
		Data struct {
			TotalCount *int `json:"totalCount"`
			Mylist     struct {
				TotalCount *int `json:"totalCount"`
				Items      []struct {
					Video struct {
						ID           string    `json:"id"`
						RegisteredAt time.Time `json:"registeredAt"`
						Count        struct {
							Comment int `json:"comment"`
						} `json:"count"`
					} `json:"video"`
				} `json:"items"`
			} `json:"mylist"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return parsedPage{}, err
	}
	items := make([]videoItem, 0, len(payload.Data.Mylist.Items))
	for _, it := range payload.Data.Mylist.Items {
		items = append(items, videoItem{ID: it.Video.ID, CommentCount: it.Video.Count.Comment, RegisteredAt: it.Video.RegisteredAt})
	}
	totalCount := 0
	totalCountKnown := false
	if payload.Data.Mylist.TotalCount != nil {
		totalCount = *payload.Data.Mylist.TotalCount
		totalCountKnown = true
	} else if payload.Data.TotalCount != nil {
		totalCount = *payload.Data.TotalCount
		totalCountKnown = true
	}
	return parsedPage{
		Items:           items,
		Status:          payload.Meta.Status,
		TotalCount:      totalCount,
		TotalCountKnown: totalCountKnown,
	}, nil
}

func collectVideoList(
	ctx context.Context,
	commentCount int,
	afterDate time.Time,
	beforeDate time.Time,
	retries int,
	httpClientTimeout time.Duration,
	limiter *RateLimiter,
	maxPages int,
	maxVideos int,
	pageConcurrency int,
	logger *slog.Logger,
	requestURL func(page int) string,
	parsePage parsePageFunc,
) ([]string, error) {
	if logger == nil {
		logger = slog.Default()
	}

	firstPage, err := fetchPage(ctx, requestURL(1), httpClientTimeout, retries, limiter, logger, parsePage)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, nil
		}
		return nil, err
	}
	if firstPage.NotFound || len(firstPage.Items) == 0 {
		return nil, nil
	}
	resStr := filterItems(firstPage.Items, commentCount, afterDate, beforeDate)
	if maxVideos > 0 && len(resStr) >= maxVideos {
		return resStr[:maxVideos], nil
	}
	if shouldCollectSequentially(firstPage, pageConcurrency, maxVideos) {
		return collectRemainingSequentially(ctx, resStr, 2, commentCount, afterDate, beforeDate, retries, httpClientTimeout, limiter, maxPages, maxVideos, logger, requestURL, parsePage)
	}
	totalPages := pageCountFor(firstPage.TotalCount)
	if maxPages > 0 && totalPages > maxPages {
		totalPages = maxPages
	}
	if totalPages <= 1 {
		return resStr, nil
	}
	parallelIDs, err := collectPagesParallel(ctx, 2, totalPages, pageConcurrency, commentCount, afterDate, beforeDate, retries, httpClientTimeout, limiter, logger, requestURL, parsePage)
	resStr = append(resStr, parallelIDs...)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, nil
		}
		return resStr, err
	}
	return resStr, nil
}
