package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/sh4869221b/go-nico-list/internal/niconico"
	"github.com/spf13/cobra"
)

func runRootCmdWithConfig(cmd *cobra.Command, args []string, cfg *RootConfig, deps RootDeps) (retErr error) {
	streamOutput := cfg.NoSortOutput && !cfg.JSONOutput
	if err := validateFlagsFor(cfg); err != nil {
		return err
	}
	afterDate, beforeDate, err := parseDateRange(cfg.DateAfter, cfg.DateBefore)
	if err != nil {
		return err
	}

	runLogger := deps.Logger
	if cfg.LogFilePath != "" {
		logFile, err := deps.OpenLogFile(cfg.LogFilePath)
		if err != nil {
			return err
		}
		defer func() {
			if err := logFile.Close(); retErr == nil && err != nil {
				retErr = err
			}
		}()
		runLogger = slog.New(slog.NewJSONHandler(logFile, nil))
	}
	var control *niconico.HTTPControl
	if cfg.AdaptiveHTTPConcurrency {
		control = niconico.NewAdaptiveHTTPControl(cfg.HTTPConcurrency, cfg.HTTPMetrics)
	} else {
		control = niconico.NewHTTPControl(cfg.HTTPConcurrency, cfg.HTTPMetrics)
	}
	if cfg.HTTPMetrics {
		defer func() { runLogger.Info("http_metrics", "http", control.Snapshot()) }()
	}

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	errWriter := cmd.ErrOrStderr()
	out := cmd.OutOrStdout()

	var idList []string
	var mu sync.Mutex
	stream := streamInputsWithConfig(ctx, cmd, args, cfg, deps)
	limiter := niconico.NewRateLimiter(cfg.RateLimit, cfg.MinInterval)
	var totalInputs int64
	var validInputs int64
	var invalidInputs int64
	var fetchOKCount int64
	var fetchErrCount int64
	var invalidInputsList []string
	var targetResults []targetResult
	var errorsList []string

	progressTotal := stream.total
	if !stream.totalKnown {
		progressTotal = -1
	}
	progressWriter := errWriter
	visible := shouldShowProgressWithConfig(errWriter, cfg, deps)
	if !visible {
		progressWriter = io.Discard
	}
	bar := deps.ProgressBarNew(progressTotal, progressWriter, visible)
	addProgress := func() {
		_ = bar.Add(1)
	}

	var outputCh chan []string
	var writeDone chan unorderedWriteResult
	if streamOutput {
		outputCh = make(chan []string, cfg.Concurrency)
		writeDone = make(chan unorderedWriteResult, 1)
		go writeUnorderedOutput(out, outputCh, cfg, cancel, writeDone)
	}

	sem := make(chan struct{}, cfg.Concurrency)
	var wg sync.WaitGroup
	errCh := make(chan error, cfg.Concurrency)
	fetchErrCh := make(chan error, 1)
	go func() {
		var firstErr error
		for err := range errCh {
			runLogger.Error("failed to get video list", "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
		fetchErrCh <- firstErr
	}()

	inputErrCh := make(chan error, 1)
	go func() {
		if err := <-stream.errs; err != nil {
			inputErrCh <- err
			cancel()
		}
		close(inputErrCh)
	}()

	var inputErr error
	inputClosed := false
	nextTargetOrder := 0
inputLoop:
	for {
		var input string
		var ok bool
		if streamOutput {
			select {
			case <-ctx.Done():
				break inputLoop
			case input, ok = <-stream.inputs:
			}
		} else {
			input, ok = <-stream.inputs
		}
		if !ok {
			inputClosed = true
			break
		}
		totalInputs++
		if inputErr == nil {
			select {
			case inputErr = <-inputErrCh:
			default:
			}
		}
		target, ok := parseInputTarget(input)
		if !ok {
			invalidInputs++
			if cfg.JSONOutput {
				invalidInputsList = append(invalidInputsList, input)
			}
			runLogger.Warn("invalid input", "input", input)
			addProgress()
			continue
		}
		validInputs++
		if inputErr != nil {
			addProgress()
			continue
		}
		targetOrder := nextTargetOrder
		nextTargetOrder++
		if streamOutput {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				break inputLoop
			}
		} else {
			sem <- struct{}{}
		}
		wg.Add(1)
		go func(target inputTarget, targetOrder int) {
			defer wg.Done()
			defer func() { <-sem }()
			defer addProgress()
			var newList []string
			var err error
			switch target.Type {
			case targetTypeUser:
				newList, err = niconico.GetVideoList(ctx, target.ID, cfg.Comment, afterDate, beforeDate, cfg.BaseURL, cfg.Retries, cfg.HTTPClientTimeout, limiter, cfg.PageConcurrency, runLogger, control)
			case targetTypeMylist:
				newList, err = niconico.GetMylistVideoList(ctx, target.ID, cfg.Comment, afterDate, beforeDate, cfg.BaseURL, cfg.Retries, cfg.HTTPClientTimeout, limiter, cfg.PageConcurrency, runLogger, control)
			}
			if err != nil {
				atomic.AddInt64(&fetchErrCount, 1)
			} else {
				atomic.AddInt64(&fetchOKCount, 1)
			}
			if streamOutput {
				if err != nil {
					errCh <- err
					if len(newList) == 0 {
						return
					}
				}
				select {
				case outputCh <- newList:
				case <-ctx.Done():
				}
				return
			}
			mu.Lock()
			if cfg.JSONOutput {
				result := targetResult{Order: targetOrder, Type: target.Type, ID: target.ID, Items: newList}
				if err != nil {
					result.Error = err.Error()
					errorsList = append(errorsList, result.Error)
				}
				targetResults = append(targetResults, result)
			}
			idList = append(idList, newList...)
			mu.Unlock()
			if err != nil {
				errCh <- err
			}
		}(target, targetOrder)
	}
	wg.Wait()
	var outputErr error
	outputCount := 0
	if streamOutput {
		close(outputCh)
	}
	close(errCh)
	fetchErrRet := <-fetchErrCh
	if streamOutput {
		writeResult := <-writeDone
		outputCount, outputErr = writeResult.count, writeResult.err
		if inputErr == nil {
			select {
			case inputErr = <-inputErrCh:
			default:
			}
		}
	}
	if inputErr == nil && (!streamOutput || inputClosed && outputErr == nil) {
		inputErr = <-inputErrCh
	}
	if streamOutput {
		runLogger.Info("video list", "count", outputCount)
	} else {
		runLogger.Info("video list", "count", len(idList))
		outputIDs := idList
		if cfg.NoSortOutput {
			outputIDs = flattenTargetItemsByInputOrder(targetResults)
		}
		if cfg.DedupeOutput && len(outputIDs) > 0 {
			outputIDs = dedupeItems(outputIDs, make(map[string]struct{}, len(outputIDs)))
		}
		outputCount = len(outputIDs)
		if outputCount > 0 && !cfg.NoSortOutput {
			niconico.NiconicoSort(outputIDs)
		}
		if cfg.JSONOutput {
			sortTargetResults(targetResults)
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
			outputErr = json.NewEncoder(out).Encode(jsonPayload)
		} else if outputCount > 0 {
			outputErr = writeLineOutput(out, outputIDs, cfg.URL)
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
		totalInputs,
		validInputs,
		invalidInputs,
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
	if cfg.StrictInput && invalidInputs > 0 {
		return errors.New("invalid input detected")
	}
	if cfg.BestEffort {
		return nil
	}
	return fetchErrRet
}
