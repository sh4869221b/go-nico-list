package niconico

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

func collectRemainingSequentially(
	ctx context.Context,
	resStr []string,
	startPage int,
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
	for page := startPage; ; page++ {
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

func shouldCollectSequentially(firstPage parsedPage, pageConcurrency int, maxVideos int) bool {
	return pageConcurrency <= 1 || maxVideos > 0 || !firstPage.TotalCountKnown
}

func pageCountFor(totalCount int) int {
	if totalCount <= 0 {
		return 0
	}
	return (totalCount + pageSize - 1) / pageSize
}

type pageResult struct {
	page      int
	ids       []string
	err       error
	terminate bool
}

func collectPagesParallel(
	ctx context.Context,
	startPage int,
	endPage int,
	pageConcurrency int,
	commentCount int,
	afterDate time.Time,
	beforeDate time.Time,
	retries int,
	httpClientTimeout time.Duration,
	limiter *RateLimiter,
	logger *slog.Logger,
	requestURL func(page int) string,
	parsePage parsePageFunc,
) ([]string, error) {
	pages := make(chan int)
	results := make(chan pageResult, pageConcurrency)
	stopScheduling := make(chan struct{})
	var stopOnce sync.Once
	var wg sync.WaitGroup
	for range pageConcurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for page := range pages {
				select {
				case <-stopScheduling:
					return
				default:
				}
				parsed, err := fetchPage(ctx, requestURL(page), httpClientTimeout, retries, limiter, logger, parsePage)
				if err != nil {
					stopOnce.Do(func() { close(stopScheduling) })
					select {
					case results <- pageResult{page: page, err: err}:
					case <-ctx.Done():
					}
					return
				}
				if parsed.NotFound || len(parsed.Items) == 0 {
					stopOnce.Do(func() { close(stopScheduling) })
					select {
					case results <- pageResult{page: page, terminate: true}:
					case <-ctx.Done():
					}
					return
				}
				select {
				case results <- pageResult{page: page, ids: filterItems(parsed.Items, commentCount, afterDate, beforeDate)}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		for page := startPage; page <= endPage; page++ {
			select {
			case <-stopScheduling:
				close(pages)
				wg.Wait()
				close(results)
				return
			default:
			}
			select {
			case <-stopScheduling:
				close(pages)
				wg.Wait()
				close(results)
				return
			case <-ctx.Done():
				close(pages)
				wg.Wait()
				close(results)
				return
			case pages <- page:
			}
		}
		close(pages)
		wg.Wait()
		close(results)
	}()

	idsByPage := make(map[int][]string)
	var firstErr error
	stopAtPage := endPage + 1
	for result := range results {
		if result.err != nil {
			if firstErr == nil {
				firstErr = result.err
			}
			if result.page < stopAtPage {
				stopAtPage = result.page
			}
			continue
		}
		if result.terminate {
			if result.page < stopAtPage {
				stopAtPage = result.page
			}
			continue
		}
		idsByPage[result.page] = result.ids
	}
	var ids []string
	for page := startPage; page < stopAtPage; page++ {
		ids = append(ids, idsByPage[page]...)
	}
	return ids, firstErr
}
