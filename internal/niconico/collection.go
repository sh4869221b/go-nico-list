package niconico

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// pageResult holds filtered IDs and the API state used to decide whether pagination should continue.
type pageResult struct {
	page       int
	ids        []string
	err        error
	exhausted  bool
	totalCount *int
}

// terminal reports whether an error, a missing page, or an empty API page ends collection.
func (r pageResult) terminal() bool {
	return r.err != nil || r.exhausted
}

// collectVideoList fetches and filters pages using the available total count and worker budget.
func collectVideoList(
	ctx context.Context,
	commentCount int,
	afterDate time.Time,
	beforeDate time.Time,
	retries int,
	httpClientTimeout time.Duration,
	limiter *RateLimiter,
	pageConcurrency int,
	logger *slog.Logger,
	requestURL func(page int) string,
	parsePage parsePageFunc,
	control *HTTPControl,
) ([]string, error) {
	if logger == nil {
		logger = slog.Default()
	}

	exclusiveBefore := beforeDate.AddDate(0, 0, 1)
	fetch := func(page int) pageResult {
		parsed, err := fetchPage(ctx, requestURL(page), httpClientTimeout, retries, limiter, logger, parsePage, control)
		result := pageResult{
			page:       page,
			err:        err,
			exhausted:  parsed.NotFound || len(parsed.Items) == 0,
			totalCount: parsed.TotalCount,
		}
		if !result.terminal() {
			result.ids = filterItems(parsed.Items, commentCount, afterDate, exclusiveBefore)
		}
		parsed.releaseItems()
		return result
	}

	first := fetch(1)
	if first.terminal() {
		return collectionResult(nil, first.err)
	}
	ids := first.ids
	if pageConcurrency == 1 || pageConcurrency < 0 || first.totalCount == nil {
		for page := 2; ; page++ {
			result := fetch(page)
			if result.terminal() {
				return collectionResult(ids, result.err)
			}
			ids = append(ids, result.ids...)
		}
	}

	totalCount := *first.totalCount
	if totalCount <= pageSize {
		return ids, nil
	}
	totalPages := 1 + (totalCount-1)/pageSize
	ids, err := collectPagesParallel(ctx, ids, 2, totalPages, pageConcurrency, fetch, control)
	return collectionResult(ids, err)
}

// collectionResult suppresses canceled fetches and preserves partial results for other errors.
func collectionResult(ids []string, err error) ([]string, error) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, nil
	}
	return ids, err
}

// collectPagesParallel fetches a bounded range and returns IDs preceding the earliest terminal page.
func collectPagesParallel(
	ctx context.Context,
	ids []string,
	startPage int,
	endPage int,
	pageConcurrency int,
	fetch func(page int) pageResult,
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
				result := fetch(page)
				if result.terminal() {
					// Stop only new scheduling; lower outstanding pages still determine
					// the ordered result and may supersede this terminal outcome.
					for {
						current := stopBefore.Load()
						if current > 0 && int64(page) >= current || stopBefore.CompareAndSwap(current, int64(page)) {
							break
						}
					}
					stopScheduling()
					results <- result
					return
				}
				select {
				case results <- result:
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
	var terminal pageResult
	collected := 0
	for result := range results {
		if result.terminal() {
			if terminal.page == 0 || result.page < terminal.page {
				terminal = result
			}
			continue
		}
		idsByPage[result.page] = result.ids
		collected += len(result.ids)
	}
	if terminal.err == nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	// Reserve from observed results only; totalCount is an untrusted API hint.
	if terminal.page == 0 {
		ids = slices.Grow(ids, collected)
	}
	for page := startPage; page <= endPage && (terminal.page == 0 || page < terminal.page); {
		ids = append(ids, idsByPage[page]...)
		if page == endPage {
			break
		}
		page++
	}
	return ids, terminal.err
}

// filterItems keeps videos above the comment threshold in [afterDate, exclusiveBefore).
func filterItems(items []videoItem, commentCount int, afterDate, exclusiveBefore time.Time) []string {
	ids := []string{}
	for i := range items {
		item := &items[i]
		if item.CommentCount <= commentCount {
			continue
		}
		if item.RegisteredAt.Before(afterDate) {
			continue
		}
		if !item.RegisteredAt.Before(exclusiveBefore) {
			continue
		}
		if len(ids) == 0 {
			ids = make([]string, 0, len(items)-i)
		}
		ids = append(ids, item.ID)
	}
	return ids
}
