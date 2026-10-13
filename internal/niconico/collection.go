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
	gate := control.NewWorkerGate(pageConcurrency)
	schedulingCtx, stopScheduling := context.WithCancel(ctx)
	defer stopScheduling()
	var stopBefore atomic.Int64
	// A constant result buffer avoids allocating a pool from an uncapped budget.
	results := make(chan pageResult, 1)
	go func() {
		var wg sync.WaitGroup
		defer close(results)
		defer wg.Wait()
		for page := startPage; page <= endPage; {
			if err := gate.Acquire(schedulingCtx); err != nil {
				return
			}
			if schedulingCtx.Err() != nil {
				gate.Release()
				return
			}
			wg.Add(1)
			go func(page int) {
				defer wg.Done()
				defer gate.Release()
				if stopAt := stopBefore.Load(); stopAt > 0 && int64(page) >= stopAt {
					return
				}
				parsed, err := fetchPage(ctx, requestURL(page), httpClientTimeout, retries, limiter, logger, parsePage, control)
				if err != nil || parsed.NotFound || len(parsed.Items) == 0 {
					// Stop only new scheduling; lower outstanding pages still determine
					// the ordered result and may supersede this terminal outcome.
					for {
						current := stopBefore.Load()
						if current > 0 && int64(page) >= current || stopBefore.CompareAndSwap(current, int64(page)) {
							break
						}
					}
					stopScheduling()
					results <- pageResult{page: page, err: err, terminate: err == nil}
					return
				}
				select {
				case results <- pageResult{page: page, ids: filterItems(parsed.Items, commentCount, afterDate, beforeDate)}:
				case <-ctx.Done():
				}
			}(page)
			if page == endPage {
				return
			}
			page++
		}
	}()

	idsByPage := make(map[int][]string)
	var firstErr error
	stopAtPage := 0
	for result := range results {
		if result.err != nil {
			if stopAtPage == 0 || result.page < stopAtPage {
				stopAtPage = result.page
				firstErr = result.err
			}
			continue
		}
		if result.terminate {
			if stopAtPage == 0 || result.page < stopAtPage {
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
	for page := startPage; page <= endPage && (stopAtPage == 0 || page < stopAtPage); {
		ids = append(ids, idsByPage[page]...)
		if page == endPage {
			break
		}
		page++
	}
	return ids, firstErr
}
