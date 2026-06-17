package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/sh4869221b/go-nico-list/internal/niconico"
	"github.com/spf13/cobra"
)

const tabOutputPrefix = "\t\t\t\t\t\t\t\t\t"

func runRootCmdWithConfig(cmd *cobra.Command, args []string, cfg *RootConfig, deps RootDeps) (retErr error) {
	if cfg.NoSortOutput && !cfg.JSONOutput {
		return runRootCmdFastUnordered(cmd, args, cfg, deps)
	}
	if err := validateFlagsFor(cfg); err != nil {
		return err
	}
	afterDate, beforeDate, err := parseDateRange(cfg.DateAfter, cfg.DateBefore)
	if err != nil {
		return err
	}

	newLogger, cleanup, err := setupLoggerFor(cfg.LogFilePath, deps)
	if err != nil {
		return err
	}
	defer func() {
		if err := cleanup(); retErr == nil && err != nil {
			retErr = err
		}
	}()
	runLogger := newLogger

	ctx := context.Background()
	if cmd != nil {
		ctx = cmd.Context()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errWriter := errWriterFor(cmd)

	var idList []string
	var mu sync.Mutex
	stream := streamInputsWithConfig(ctx, cmd, args, cfg, deps)
	limiter := niconico.NewRateLimiter(cfg.RateLimit, cfg.MinInterval)
	var totalInputs int64
	var validInputs int64
	var invalidInputs int64
	var fetchOKCount int64
	var fetchErrCount int64
	invalidInputsList := make([]string, 0)
	targetResults := make([]targetResult, 0)
	errorsList := make([]string, 0)

	bar := newProgressBarWithConfig(cmd, stream.totalKnown, stream.total, cfg, deps)
	var progressMu sync.Mutex
	addProgress := func() {
		progressMu.Lock()
		_ = bar.Add(1)
		progressMu.Unlock()
	}

	sem := make(chan struct{}, cfg.Concurrency)
	var wg sync.WaitGroup
	errCh := make(chan error, cfg.Concurrency)
	fetchErrCh := make(chan error, 1)
	go func() {
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
	}()

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
	nextTargetOrder := 0
	for input := range stream.inputs {
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
			mu.Lock()
			invalidInputsList = append(invalidInputsList, input)
			mu.Unlock()
			runLogger.Warn("invalid input", "input", input)
			addProgress()
			continue
		}
		atomic.AddInt64(&validInputs, 1)
		if inputErr != nil {
			addProgress()
			continue
		}
		targetOrder := nextTargetOrder
		nextTargetOrder++
		sem <- struct{}{}
		wg.Add(1)
		go func(target inputTarget, targetOrder int) {
			defer wg.Done()
			defer func() { <-sem }()
			defer addProgress()
			var newList []string
			var err error
			switch target.Type {
			case targetTypeUser:
				newList, err = niconico.GetVideoList(ctx, target.ID, cfg.Comment, afterDate, beforeDate, cfg.BaseURL, cfg.Retries, cfg.HTTPClientTimeout, limiter, cfg.MaxPages, cfg.MaxVideos, cfg.PageConcurrency, runLogger)
			case targetTypeMylist:
				newList, err = niconico.GetMylistVideoList(ctx, target.ID, cfg.Comment, afterDate, beforeDate, cfg.BaseURL, cfg.Retries, cfg.HTTPClientTimeout, limiter, cfg.MaxPages, cfg.MaxVideos, cfg.PageConcurrency, runLogger)
			}
			if err != nil {
				atomic.AddInt64(&fetchErrCount, 1)
				mu.Lock()
				errorsList = append(errorsList, err.Error())
				targetResults = append(targetResults, targetResult{
					Order: targetOrder,
					Type:  target.Type,
					ID:    target.ID,
					Items: newList,
					Error: err.Error(),
				})
				idList = append(idList, newList...)
				mu.Unlock()
				errCh <- err
				return
			}
			atomic.AddInt64(&fetchOKCount, 1)
			mu.Lock()
			targetResults = append(targetResults, targetResult{
				Order: targetOrder,
				Type:  target.Type,
				ID:    target.ID,
				Items: newList,
				Error: "",
			})
			idList = append(idList, newList...)
			mu.Unlock()
		}(target, targetOrder)
	}
	wg.Wait()
	close(errCh)
	fetchErrRet := <-fetchErrCh
	sortTargetResults(targetResults)
	if inputErr == nil {
		for err := range inputErrCh {
			if err != nil {
				inputErr = err
				break
			}
		}
	}
	close(sem)
	runLogger.Info("video list", "count", len(idList))
	outputIDs := idList
	if cfg.NoSortOutput {
		outputIDs = flattenTargetItemsByInputOrder(targetResults)
	}
	if cfg.DedupeOutput && len(outputIDs) > 0 {
		seen := make(map[string]struct{}, len(outputIDs))
		unique := make([]string, 0, len(outputIDs))
		for _, id := range outputIDs {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			unique = append(unique, id)
		}
		outputIDs = unique
	}
	outputCount := len(outputIDs)
	if outputCount > 0 && !cfg.NoSortOutput {
		niconico.NiconicoSort(outputIDs)
	}
	out := outWriterFor(cmd)
	var outputErr error
	if cfg.JSONOutput {
		jsonPayload := buildJSONOutput(
			totalInputs,
			validInputs,
			invalidInputs,
			invalidInputsList,
			targetResults,
			errorsList,
			outputCount,
			outputIDs,
		)
		enc := json.NewEncoder(out)
		if err := enc.Encode(jsonPayload); err != nil {
			outputErr = err
		}
	} else if outputCount > 0 {
		if err := writeLineOutput(out, outputIDs, cfg.Tab, cfg.URL); err != nil {
			outputErr = err
		}
	}
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
		outputCount,
	); err != nil {
		return err
	}
	if outputErr != nil {
		return outputErr
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
