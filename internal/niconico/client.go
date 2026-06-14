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
	logger *slog.Logger,
) ([]string, error) {
	return collectVideoList(ctx, commentCount, afterDate, beforeDate, retries, httpClientTimeout, limiter, maxPages, maxVideos, logger,
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
	logger *slog.Logger,
) ([]string, error) {
	return collectVideoList(ctx, commentCount, afterDate, beforeDate, retries, httpClientTimeout, limiter, maxPages, maxVideos, logger,
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
	items := make([]videoItem, 0, len(nicoData.Data.Items))
	for _, s := range nicoData.Data.Items {
		items = append(items, videoItem{ID: s.Essential.ID, CommentCount: s.Essential.Count.Comment, RegisteredAt: s.Essential.RegisteredAt})
	}
	return parsedPage{
		Items:           items,
		Status:          nicoData.Meta.Status,
		TotalCount:      nicoData.Data.TotalCount,
		TotalCountKnown: true,
	}, nil
}

func parseMylistPage(body []byte) (parsedPage, error) {
	var payload struct {
		Meta struct {
			Status int `json:"status"`
		} `json:"meta"`
		Data struct {
			Mylist struct {
				Items []struct {
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
	return parsedPage{Items: items, Status: payload.Meta.Status}, nil
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
	logger *slog.Logger,
	requestURL func(page int) string,
	parsePage parsePageFunc,
) ([]string, error) {
	if logger == nil {
		logger = slog.Default()
	}

	var resStr []string

	for page := 1; ; page++ {
		if maxPages > 0 && page > maxPages {
			break
		}
		parsed, err := fetchPage(ctx, requestURL(page), httpClientTimeout, retries, limiter, logger, parsePage)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, nil
			}
			return resStr, err
		}
		if parsed.NotFound {
			break
		}
		if parsed.Items == nil {
			continue
		}
		if len(parsed.Items) == 0 {
			break
		}
		for _, id := range filterItems(parsed.Items, commentCount, afterDate, beforeDate) {
			resStr = append(resStr, id)
			if maxVideos > 0 && len(resStr) >= maxVideos {
				return resStr, nil
			}
		}
	}
	return resStr, nil
}
