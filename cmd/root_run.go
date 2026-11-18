package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/sh4869221b/go-nico-list/internal/niconico"
	"github.com/spf13/cobra"
)

// runRootCmdWithConfig fetches the requested targets and reports the command result.
func runRootCmdWithConfig(cmd *cobra.Command, args []string, cfg *RootConfig, deps RootDeps) (retErr error) {
	if err := validateFlagsFor(cfg); err != nil {
		return err
	}
	afterDate, beforeDate, err := parseDateRange(cfg.DateAfter, cfg.DateBefore)
	if err != nil {
		return err
	}

	runLogger := deps.Logger
	var logInfo os.FileInfo
	if cfg.LogFilePath != "" {
		if err := validateInputLogPaths(cfg.InputFilePath, cfg.LogFilePath); err != nil {
			return err
		}
		logFile, err := deps.OpenLogFile(cfg.LogFilePath)
		if err != nil {
			return err
		}
		defer func() {
			if err := logFile.Close(); retErr == nil && err != nil {
				retErr = err
			}
		}()
		if cfg.InputFilePath != "" {
			if file, ok := logFile.(interface{ Stat() (os.FileInfo, error) }); ok {
				logInfo, _ = file.Stat()
			}
		}
		runLogger = slog.New(slog.NewJSONHandler(logFile, nil))
	}
	control, targetConcurrency, pageConcurrency := resolveConcurrency(cmd, cfg)
	if cfg.HTTPMetrics {
		defer func() { runLogger.Info("http_metrics", "http", control.Snapshot()) }()
	}

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()
	targetGate := control.NewWorkerGate(targetConcurrency)
	streamOutput := cfg.NoSortOutput && !cfg.JSONOutput
	inputCtx := ctx
	if !streamOutput {
		// Preserve buffered-output admission: finish the target already
		// read when an input error cancels the fetch work.
		inputCtx = cmd.Context()
	}

	errWriter := cmd.ErrOrStderr()
	out := cmd.OutOrStdout()

	var buffered bufferedOutput
	stream := streamInputsWithConfig(ctx, cancel, cmd, args, cfg, deps, logInfo)
	limiter := niconico.NewRateLimiter(cfg.RateLimit, cfg.MinInterval)
	var inputs jsonInputs

	bar := newInputProgress(stream.progressTotal, errWriter, cfg, deps)
	addProgress := func() {
		_ = bar.Add(1)
	}

	var outputCh chan []string
	var streamWriteDone chan outputWriteResult
	// Queue storage stays bounded independently of the worker concurrency.
	if streamOutput {
		outputCh = make(chan []string, 1)
		streamWriteDone = make(chan outputWriteResult, 1)
		go writeUnorderedOutput(out, outputCh, cfg, cancel, streamWriteDone)
	}

	var wg sync.WaitGroup
	fetchErrors := make(chan error, 1)
	fetchLogDone := logFetchErrors(runLogger, fetchErrors)

	var inputErr error
	var admittedTargets int64
inputLoop:
	for {
		var input string
		var ok bool
		select {
		case <-inputCtx.Done():
			break inputLoop
		case input, ok = <-stream.inputs:
		}
		if !ok {
			break
		}
		inputs.Total++
		if inputErr == nil {
			select {
			case inputErr = <-stream.errs:
			default:
			}
		}
		target, ok := parseInputTarget(input)
		if !ok {
			inputs.Invalid++
			if cfg.JSONOutput {
				buffered.invalidInputs = append(buffered.invalidInputs, input)
			}
			runLogger.Warn("invalid input", "input", input)
			addProgress()
			continue
		}
		inputs.Valid++
		if inputErr != nil {
			addProgress()
			continue
		}
		if err := targetGate.Acquire(inputCtx); err != nil {
			break inputLoop
		}
		targetOrder := int(admittedTargets)
		admittedTargets++
		wg.Add(1)
		go func(target inputTarget, targetOrder int) {
			defer wg.Done()
			defer targetGate.Release()
			defer addProgress()
			fetch := niconico.GetVideoList
			if target.Type == targetTypeMylist {
				fetch = niconico.GetMylistVideoList
			}
			items, err := fetch(ctx, target.ID, cfg.Comment, afterDate, beforeDate, cfg.BaseURL, cfg.Retries, cfg.HTTPClientTimeout, limiter, pageConcurrency, runLogger, control)
			if !streamOutput {
				buffered.addTarget(target, targetOrder, items, err, cfg.JSONOutput)
			}
			if err != nil {
				fetchErrors <- err
			}
			if streamOutput && (err == nil || len(items) > 0) {
				select {
				case outputCh <- items:
				case <-ctx.Done():
				}
			}
		}(target, targetOrder)
	}
	wg.Wait()
	var outputResult outputWriteResult
	if streamOutput {
		close(outputCh)
	}
	close(fetchErrors)
	failures := <-fetchLogDone
	if streamOutput {
		outputResult = <-streamWriteDone
	}
	// Opening or reading input can stay blocked after cancellation.
	if inputErr == nil {
		select {
		case inputErr = <-stream.errs:
		default:
		}
	}
	if streamOutput {
		runLogger.Info("video list", "count", outputResult.count)
	} else {
		runLogger.Info("video list", "count", buffered.itemCount)
		outputResult = buffered.write(out, inputs, cfg)
	}
	if shouldShowProgressWithConfig(errWriter, cfg, deps) {
		if _, err := fmt.Fprintln(errWriter); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(
		errWriter,
		"summary inputs=%d valid=%d invalid=%d fetch_ok=%d fetch_err=%d output_count=%d\n",
		inputs.Total,
		inputs.Valid,
		inputs.Invalid,
		admittedTargets-failures.count,
		failures.count,
		outputResult.count,
	); err != nil {
		return err
	}
	if outputResult.err != nil {
		return outputResult.err
	}
	if inputErr != nil {
		return inputErr
	}
	if cfg.StrictInput && inputs.Invalid > 0 {
		return errors.New("invalid input detected")
	}
	if cfg.BestEffort {
		return nil
	}
	return failures.first
}

// fetchFailures records the number of failed targets and the first error reported.
type fetchFailures struct {
	count int64
	first error
}

// logFetchErrors logs target failures as they arrive and reports the final count and first error.
func logFetchErrors(logger *slog.Logger, fetchErrors <-chan error) <-chan fetchFailures {
	done := make(chan fetchFailures, 1)
	go func() {
		var failures fetchFailures
		for err := range fetchErrors {
			logger.Error("failed to get video list", "error", err)
			failures.count++
			if failures.first == nil {
				failures.first = err
			}
		}
		done <- failures
	}()
	return done
}
