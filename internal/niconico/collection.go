package niconico

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
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
	logger *slog.Logger,
	requestURL func(page int) string,
	parsePage parsePageFunc,
) ([]string, error) {
	for page := startPage; ; page++ {
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
		resStr = append(resStr, filterItems(parsed.Items, commentCount, afterDate, beforeDate)...)
	}
	return resStr, nil
}

func shouldCollectSequentially(firstPage parsedPage, pageConcurrency int) bool {
	return pageConcurrency <= 1 || !firstPage.TotalCountKnown
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

func lowerStopBefore(stopBefore *atomic.Int64, page int) {
	newStop := int64(page)
	for {
		current := stopBefore.Load()
		if newStop >= current {
			return
		}
		if stopBefore.CompareAndSwap(current, newStop) {
			return
		}
	}
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
	var stopBefore atomic.Int64
	stopBefore.Store(int64(endPage + 1))
	var wg sync.WaitGroup
	for range pageConcurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for page := range pages {
				if int64(page) >= stopBefore.Load() {
					return
				}
				parsed, err := fetchPage(ctx, requestURL(page), httpClientTimeout, retries, limiter, logger, parsePage)
				if err != nil {
					lowerStopBefore(&stopBefore, page)
					stopOnce.Do(func() { close(stopScheduling) })
					results <- pageResult{page: page, err: err}
					return
				}
				if parsed.NotFound || len(parsed.Items) == 0 {
					lowerStopBefore(&stopBefore, page)
					stopOnce.Do(func() { close(stopScheduling) })
					results <- pageResult{page: page, terminate: true}
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
			if result.page < stopAtPage {
				stopAtPage = result.page
				firstErr = result.err
			}
			continue
		}
		if result.terminate {
			if result.page < stopAtPage {
				stopAtPage = result.page
				firstErr = nil
			}
			continue
		}
		idsByPage[result.page] = result.ids
	}
	if firstErr == nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var ids []string
	for page := startPage; page < stopAtPage; page++ {
		ids = append(ids, idsByPage[page]...)
	}
	return ids, firstErr
}
