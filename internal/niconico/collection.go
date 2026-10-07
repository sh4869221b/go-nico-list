package niconico

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

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
	control *HTTPControl,
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
				parsed, err := fetchPage(ctx, requestURL(page), httpClientTimeout, retries, limiter, logger, parsePage, control)
				if err != nil || parsed.NotFound || len(parsed.Items) == 0 {
					for {
						current := stopBefore.Load()
						if int64(page) >= current || stopBefore.CompareAndSwap(current, int64(page)) {
							break
						}
					}
					stopOnce.Do(func() { close(stopScheduling) })
					results <- pageResult{page: page, err: err, terminate: err == nil}
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
		defer close(results)
		defer wg.Wait()
		defer close(pages)
		for page := startPage; page <= endPage; page++ {
			select {
			case <-stopScheduling:
				return
			default:
			}
			select {
			case <-stopScheduling:
				return
			case <-ctx.Done():
				return
			case pages <- page:
			}
		}
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
