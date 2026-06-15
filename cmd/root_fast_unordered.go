package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/sh4869221b/go-nico-list/internal/niconico"
	"github.com/spf13/cobra"
)

type unorderedBatch struct {
	items []string
}

type unorderedWriteResult struct {
	count int
	err   error
}

func runRootCmdFastUnordered(cmd *cobra.Command, args []string, cfg *RootConfig, deps RootDeps) (retErr error) {
	if err := validateFlagsFor(cfg); err != nil {
		return err
	}
	afterDate, beforeDate, err := parseDateRange(cfg.DateAfter, cfg.DateBefore)
	if err != nil {
		return err
	}
	runLogger, cleanup, err := setupLoggerFor(cfg.LogFilePath, deps)
	if err != nil {
		return err
	}
	defer func() {
		if err := cleanup(); retErr == nil && err != nil {
			retErr = err
		}
	}()

	ctx := context.Background()
	if cmd != nil {
		ctx = cmd.Context()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errWriter := errWriterFor(cmd)
	stream := streamInputsWithConfig(cmd, args, cfg, deps)
	limiter := niconico.NewRateLimiter(cfg.RateLimit, cfg.MinInterval)
	var totalInputs int64
	var validInputs int64
	var invalidInputs int64
	var fetchOKCount int64
	var fetchErrCount int64

	bar := newProgressBarWithConfig(cmd, stream.totalKnown, stream.total, cfg, deps)
	var progressMu sync.Mutex
	addProgress := func() {
		progressMu.Lock()
		_ = bar.Add(1)
		progressMu.Unlock()
	}

	outputCh := make(chan unorderedBatch, cfg.Concurrency)
	writeDone := make(chan unorderedWriteResult, 1)
	go writeUnorderedOutput(ctx, outWriterFor(cmd), outputCh, cfg, cancel, writeDone)

	sem := make(chan struct{}, cfg.Concurrency)
	var wg sync.WaitGroup
	errCh := make(chan error, cfg.Concurrency)
	fetchErrCh := make(chan error, 1)
	go collectFetchErrors(runLogger, errCh, fetchErrCh)

	inputErrCh := make(chan error, 1)
	go func() {
		for err := range stream.errs {
			if err == nil {
				continue
			}
			inputErrCh <- err
			cancel()
			break
		}
		close(inputErrCh)
	}()

	var inputErr error
	inputClosed := false
inputLoop:
	for {
		var input string
		select {
		case <-ctx.Done():
			break inputLoop
		case nextInput, ok := <-stream.inputs:
			if !ok {
				inputClosed = true
				break inputLoop
			}
			input = nextInput
		}
		atomic.AddInt64(&totalInputs, 1)
		if inputErr == nil {
			select {
			case err := <-inputErrCh:
				if err != nil {
					inputErr = err
				}
			default:
			}
		}
		target, ok := parseInputTarget(input)
		if !ok {
			atomic.AddInt64(&invalidInputs, 1)
			runLogger.Warn("invalid input", "input", input)
			addProgress()
			continue
		}
		atomic.AddInt64(&validInputs, 1)
		if inputErr != nil {
			addProgress()
			continue
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break inputLoop
		}
		wg.Add(1)
		go func(target inputTarget) {
			defer wg.Done()
			defer func() { <-sem }()
			defer addProgress()
			newList, err := fetchTargetList(ctx, target, cfg, afterDate, beforeDate, limiter, runLogger)
			if err != nil {
				atomic.AddInt64(&fetchErrCount, 1)
				errCh <- err
				if len(newList) == 0 {
					return
				}
			} else {
				atomic.AddInt64(&fetchOKCount, 1)
			}
			select {
			case outputCh <- unorderedBatch{items: newList}:
			case <-ctx.Done():
			}
		}(target)
	}
	wg.Wait()
	close(outputCh)
	close(errCh)
	fetchErrRet := <-fetchErrCh
	writeResult := <-writeDone
	if inputErr == nil {
		select {
		case err, ok := <-inputErrCh:
			if ok && err != nil {
				inputErr = err
			}
		default:
		}
	}
	if inputErr == nil && inputClosed && writeResult.err == nil {
		for err := range inputErrCh {
			if err != nil {
				inputErr = err
				break
			}
		}
	}
	close(sem)
	runLogger.Info("video list", "count", writeResult.count)
	if shouldShowProgressWithConfig(errWriter, cfg, deps) {
		if _, err := fmt.Fprintln(errWriter); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(
		errWriter,
		"summary inputs=%d valid=%d invalid=%d fetch_ok=%d fetch_err=%d output_count=%d\n",
		atomic.LoadInt64(&totalInputs),
		atomic.LoadInt64(&validInputs),
		atomic.LoadInt64(&invalidInputs),
		atomic.LoadInt64(&fetchOKCount),
		atomic.LoadInt64(&fetchErrCount),
		writeResult.count,
	); err != nil {
		return err
	}
	if writeResult.err != nil {
		return writeResult.err
	}
	if inputErr != nil {
		return inputErr
	}
	if cfg.StrictInput && atomic.LoadInt64(&invalidInputs) > 0 {
		return errors.New("invalid input detected")
	}
	if cfg.BestEffort {
		return nil
	}
	return fetchErrRet
}

func collectFetchErrors(runLogger *slog.Logger, errCh <-chan error, fetchErrCh chan<- error) {
	var firstErr error
	for err := range errCh {
		if err == nil {
			continue
		}
		runLogger.Error("failed to get video list", "error", err)
		if firstErr == nil {
			firstErr = err
		}
	}
	fetchErrCh <- firstErr
	close(fetchErrCh)
}

func writeUnorderedOutput(ctx context.Context, out io.Writer, outputCh <-chan unorderedBatch, cfg *RootConfig, cancel context.CancelFunc, done chan<- unorderedWriteResult) {
	seen := make(map[string]struct{})
	if !cfg.DedupeOutput {
		seen = nil
	}
	outputCount := 0
	for batch := range outputCh {
		items := batch.items
		if seen != nil {
			items = dedupeStreamingItems(items, seen)
		}
		if cfg.MaxVideos > 0 && outputCount+len(items) > cfg.MaxVideos {
			items = items[:cfg.MaxVideos-outputCount]
		}
		if len(items) > 0 {
			if err := writeLineOutput(out, items, cfg.Tab, cfg.URL); err != nil {
				cancel()
				done <- unorderedWriteResult{count: outputCount, err: err}
				return
			}
			outputCount += len(items)
		}
		if cfg.MaxVideos > 0 && outputCount >= cfg.MaxVideos {
			cancel()
		}
	}
	done <- unorderedWriteResult{count: outputCount}
}

func dedupeStreamingItems(items []string, seen map[string]struct{}) []string {
	unique := make([]string, 0, len(items))
	for _, id := range items {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique
}
