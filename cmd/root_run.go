package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sh4869221b/go-nico-list/internal/niconico"
	"github.com/spf13/cobra"
)

const tabOutputPrefix = "\t\t\t\t\t\t\t\t\t"

// validateFlags validates CLI flag values before execution.
func validateFlags() error {
	if concurrency < 1 {
		return errors.New("concurrency must be at least 1")
	}
	if retries < 1 {
		return errors.New("retries must be at least 1")
	}
	if httpClientTimeout <= 0 {
		return errors.New("timeout must be greater than 0")
	}
	if rateLimit < 0 {
		return errors.New("rate-limit must be at least 0")
	}
	if minInterval < 0 {
		return errors.New("min-interval must be at least 0")
	}
	if maxPages < 0 {
		return errors.New("max-pages must be at least 0")
	}
	if maxVideos < 0 {
		return errors.New("max-videos must be at least 0")
	}
	return nil
}

// parseDateRange parses date strings into UTC time values.
func parseDateRange(after, before string) (time.Time, time.Time, error) {
	const dateFormat = "20060102"
	parsedAfter, err := time.Parse(dateFormat, after)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("dateafter format error")
	}
	parsedBefore, err := time.Parse(dateFormat, before)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("datebefore format error")
	}
	if parsedAfter.After(parsedBefore) {
		return time.Time{}, time.Time{}, errors.New("dateafter must be on or before datebefore")
	}
	return parsedAfter, parsedBefore, nil
}

// setupLogger initializes a JSON logger and optional cleanup for log files.
func setupLogger(path string) (*slog.Logger, func(), error) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{}))
	if path == "" {
		return logger, func() {}, nil
	}
	logFile, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, func() {}, err
	}
	cleanup := func() { _ = logFile.Close() }
	logger = slog.New(slog.NewJSONHandler(logFile, &slog.HandlerOptions{}))
	return logger, cleanup, nil
}

// errWriterFor returns the stderr writer for a command.
func errWriterFor(cmd *cobra.Command) io.Writer {
	if cmd == nil {
		return os.Stderr
	}
	return cmd.ErrOrStderr()
}

// outWriterFor returns the stdout writer for a command.
func outWriterFor(cmd *cobra.Command) io.Writer {
	if cmd == nil {
		return os.Stdout
	}
	return cmd.OutOrStdout()
}

// userIDFromMatch extracts the userID named submatch from a regex match.
func userIDFromMatch(match []string, re *regexp.Regexp) string {
	if len(match) == 0 {
		return ""
	}
	result := make(map[string]string)
	for i, name := range re.SubexpNames() {
		if i != 0 && name != "" {
			result[name] = match[i]
		}
	}
	return result["userID"]
}

// formatOutputIDs applies optional tab/url prefixes for line output.
func formatOutputIDs(items []string, withTab bool, withURL bool) []string {
	if !withTab && !withURL {
		return items
	}
	prefix := ""
	if withTab {
		prefix += tabOutputPrefix
	}
	if withURL {
		prefix += nicoWatchURLPrefix
	}
	formatted := make([]string, 0, len(items))
	for _, item := range items {
		formatted = append(formatted, prefix+item)
	}
	return formatted
}

// runRootCmd executes the main CLI workflow.
func runRootCmd(cmd *cobra.Command, args []string) error {
	if err := validateFlags(); err != nil {
		return err
	}
	afterDate, beforeDate, err := parseDateRange(dateafter, datebefore)
	if err != nil {
		return err
	}

	newLogger, cleanup, err := setupLogger(logFilePath)
	if err != nil {
		return err
	}
	defer cleanup()
	logger = newLogger
	slog.SetDefault(logger)

	ctx := context.Background()
	if cmd != nil {
		ctx = cmd.Context()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	errWriter := errWriterFor(cmd)

	var idList []string
	var mu sync.Mutex
	stream := streamInputs(cmd, args)
	limiter := niconico.NewRateLimiter(rateLimit, minInterval)
	var totalInputs int64
	var validInputs int64
	var invalidInputs int64
	var fetchOKCount int64
	var fetchErrCount int64
	invalidInputsList := make([]string, 0)
	userResults := make([]userResult, 0)
	errorsList := make([]string, 0)

	r := regexp.MustCompile(`((http(s)?://)?(www\.)?)nicovideo\.jp/user/(?P<userID>\d{1,9})(/video)?`)
	bar := newProgressBar(cmd, stream.totalKnown, stream.total)
	var progressMu sync.Mutex
	addProgress := func() {
		progressMu.Lock()
		bar.Add(1)
		progressMu.Unlock()
	}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	errCh := make(chan error, concurrency)
	fetchErrCh := make(chan error, 1)
	go func() {
		var firstErr error
		for err := range errCh {
			if err == nil {
				continue
			}
			logger.Error("failed to get video list", "error", err)
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
		match := r.FindStringSubmatch(input)
		if len(match) == 0 {
			atomic.AddInt64(&invalidInputs, 1)
			mu.Lock()
			invalidInputsList = append(invalidInputsList, input)
			mu.Unlock()
			logger.Warn("invalid user ID", "input", input)
			addProgress()
			continue
		}
		atomic.AddInt64(&validInputs, 1)
		if inputErr != nil {
			addProgress()
			continue
		}
		sem <- struct{}{}
		wg.Add(1)
		userID := userIDFromMatch(match, r)
		go func(userID string) {
			defer wg.Done()
			defer func() { <-sem }()
			defer addProgress()
			newList, err := niconico.GetVideoList(ctx, userID, comment, afterDate, beforeDate, baseURL, retries, httpClientTimeout, limiter, maxPages, maxVideos, logger)
			if err != nil {
				atomic.AddInt64(&fetchErrCount, 1)
				mu.Lock()
				errorsList = append(errorsList, err.Error())
				userResults = append(userResults, userResult{
					UserID: userID,
					Items:  newList,
					Error:  err.Error(),
				})
				idList = append(idList, newList...)
				mu.Unlock()
				errCh <- err
				return
			}
			atomic.AddInt64(&fetchOKCount, 1)
			mu.Lock()
			userResults = append(userResults, userResult{
				UserID: userID,
				Items:  newList,
				Error:  "",
			})
			idList = append(idList, newList...)
			mu.Unlock()
		}(userID)
	}
	wg.Wait()
	close(errCh)
	fetchErrRet := <-fetchErrCh
	sortUserResultsByUserID(userResults)
	if inputErr == nil {
		for err := range inputErrCh {
			if err != nil {
				inputErr = err
				break
			}
		}
	}
	close(sem)
	logger.Info("video list", "count", len(idList))
	outputIDs := idList
	if dedupeOutput && len(outputIDs) > 0 {
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
	if outputCount > 0 {
		niconico.NiconicoSort(outputIDs)
	}
	out := outWriterFor(cmd)
	if jsonOutput {
		jsonPayload := buildJSONOutput(
			totalInputs,
			validInputs,
			invalidInputs,
			invalidInputsList,
			userResults,
			errorsList,
			outputCount,
			outputIDs,
		)
		enc := json.NewEncoder(out)
		if err := enc.Encode(jsonPayload); err != nil {
			return err
		}
	} else if outputCount > 0 {
		formattedIDs := formatOutputIDs(outputIDs, tab, url)
		fmt.Fprintln(out, strings.Join(formattedIDs, "\n"))
	}
	if shouldShowProgress(errWriter) {
		fmt.Fprintln(errWriter)
	}
	fmt.Fprintf(
		errWriter,
		"summary inputs=%d valid=%d invalid=%d fetch_ok=%d fetch_err=%d output_count=%d\n",
		atomic.LoadInt64(&totalInputs),
		atomic.LoadInt64(&validInputs),
		atomic.LoadInt64(&invalidInputs),
		atomic.LoadInt64(&fetchOKCount),
		atomic.LoadInt64(&fetchErrCount),
		outputCount,
	)
	if inputErr != nil {
		return inputErr
	}
	if strictInput && atomic.LoadInt64(&invalidInputs) > 0 {
		return errors.New("invalid input detected")
	}
	if bestEffort {
		return nil
	}
	return fetchErrRet
}
