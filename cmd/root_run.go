package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
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

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()
	var stdin io.Reader
	if cfg.ReadStdin {
		stdin = cmd.InOrStdin()
	}

	runLogger := deps.Logger
	var safeLog *guardedLog
	if cfg.LogFilePath != "" {
		stdinInfo := openFileInfo(stdin)
		if err := validateInputLogPaths(cfg.InputFilePath, cfg.LogFilePath, stdinInfo); err != nil {
			return err
		}
		var logFile io.WriteCloser
		var err error
		if usesOSOpener(deps.OpenLogFile, defaultOpenLogFile) {
			logFile, err = openOSLogFile(ctx, cfg.LogFilePath)
		} else {
			logFile, err = deps.OpenLogFile(cfg.LogFilePath)
		}
		if err != nil {
			if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
				var empty bufferedOutput
				result := empty.write(cmd.OutOrStdout(), jsonInputs{}, cfg)
				if err := writeRunSummary(cmd.ErrOrStderr(), jsonInputs{}, 0, 0, result.count); err != nil {
					return err
				}
				return result.err
			}
			return err
		}
		defer func() {
			if err := logFile.Close(); retErr == nil && err != nil {
				retErr = err
			}
		}()
		var logWriter io.Writer = logFile
		if cfg.InputFilePath != "" || stdinInfo != nil {
			safeLog = &guardedLog{writer: logFile, info: openFileInfo(logFile)}
			if err := safeLog.checkInput(stdinInfo); err != nil {
				return err
			}
			if cfg.InputFilePath != "" {
				// Opening a new log can resolve a previously dangling input link.
				info, _ := os.Stat(cfg.InputFilePath)
				if err := safeLog.checkInput(info); err != nil {
					return err
				}
			}
			logWriter = safeLog
		}
		runLogger = slog.New(slog.NewJSONHandler(logWriter, nil))
	}
	control, targetConcurrency, pageConcurrency := resolveConcurrency(cmd, cfg)
	if cfg.HTTPMetrics {
		defer func() { runLogger.Info("http_metrics", "http", control.Snapshot()) }()
	}

	targetGate := control.NewWorkerGate(targetConcurrency)
	streamOutput := cfg.NoSortOutput && !cfg.JSONOutput

	errWriter := cmd.ErrOrStderr()
	out := cmd.OutOrStdout()

	var buffered bufferedOutput
	stream := streamInputsWithConfig(ctx, cancel, args, cfg.InputFilePath, stdin, deps, safeLog)
	limiter := niconico.NewRateLimiter(cfg.RateLimit, cfg.MinInterval)
	var inputs jsonInputs

	bar := newInputProgress(stream.progressTotal, errWriter, cfg, deps)
	addProgress := func() {
		_ = bar.Add(1)
	}

	var outputCh chan []string
	var streamWriteDone chan struct{}
	var streamWriteResult outputWriteResult
	// Queue storage stays bounded independently of the worker concurrency.
	if streamOutput {
		outputCh = make(chan []string, 1)
		streamWriteDone = make(chan struct{})
		go func() {
			streamWriteResult = writeUnorderedOutput(out, outputCh, cfg, cancel)
			close(streamWriteDone)
		}()
	}

	var wg sync.WaitGroup
	fetchErrors := make(chan error, 1)
	fetchLogDone := logFetchErrors(runLogger, fetchErrors)

	var reportedTargets int64
inputLoop:
	for {
		var input string
		var ok bool
		select {
		case <-ctx.Done():
			break inputLoop
		case input, ok = <-stream.inputs:
		}
		if !ok {
			break
		}
		inputs.Total++
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
		targetOrder := int(reportedTargets)
		if err := targetGate.Acquire(ctx); err != nil {
			if !streamOutput {
				// This target was already read. Account for it without
				// starting another worker or making an HTTP request.
				reportedTargets++
				buffered.addTarget(target, targetOrder, nil, nil, cfg.JSONOutput)
				addProgress()
			}
			break inputLoop
		}
		reportedTargets++
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
			if streamOutput && len(items) > 0 {
				select {
				case outputCh <- items:
				case <-streamWriteDone:
				}
			}
		}(target, targetOrder)
	}
	wg.Wait()
	// Join the command-owned input phase. Caller-supplied readers and openers
	// can stay blocked after cancellation.
	<-stream.ownedDone
	var outputResult outputWriteResult
	if streamOutput {
		close(outputCh)
	}
	close(fetchErrors)
	failures := <-fetchLogDone
	if streamOutput {
		<-streamWriteDone
		outputResult = streamWriteResult
	}
	var inputErr error
	select {
	case inputErr = <-stream.errs:
	default:
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
	if err := writeRunSummary(errWriter, inputs, reportedTargets-failures.count, failures.count, outputResult.count); err != nil {
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

func writeRunSummary(out io.Writer, inputs jsonInputs, fetchOK, fetchErr int64, outputCount int) error {
	_, err := fmt.Fprintf(out, "summary inputs=%d valid=%d invalid=%d fetch_ok=%d fetch_err=%d output_count=%d\n",
		inputs.Total, inputs.Valid, inputs.Invalid, fetchOK, fetchErr, outputCount)
	return err
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
