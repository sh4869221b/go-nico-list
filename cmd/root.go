/*
Copyright © 2024 sh4869221b <sh4869221b@gmail.com>
*/
package cmd

import (
	"bufio"
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

	"github.com/schollz/progressbar/v3"
	"github.com/sh4869221b/go-nico-list/internal/niconico"
	"golang.org/x/term"

	"github.com/spf13/cobra"
)

var (
	comment           int
	dateafter         string
	datebefore        string
	tab               bool
	url               bool
	concurrency       int           = 3
	retries           int           = defaultRetries
	httpClientTimeout time.Duration = defaultHTTPTimeout
	inputFilePath     string
	readStdin         bool
	logFilePath       string
	forceProgress     bool
	noProgress        bool
	strictInput       bool
	bestEffort        bool
	dedupeOutput      bool
	jsonOutput        bool
	rateLimit         float64
	minInterval       time.Duration
	maxPages          int
	maxVideos         int
	Version           = "unset"
	logger            *slog.Logger
	progressBarNew    func(int64, io.Writer, bool) *progressbar.ProgressBar = func(max int64, writer io.Writer, visible bool) *progressbar.ProgressBar {
		return progressbar.NewOptions64(
			max,
			progressbar.OptionSetWriter(writer),
			progressbar.OptionSetVisibility(visible),
		)
	}
	openInputFile func(string) (io.ReadCloser, error) = func(path string) (io.ReadCloser, error) { return os.Open(path) }
	isTerminal    func(io.Writer) bool                = defaultIsTerminal
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "go-nico-list",
	Short: "niconico {user}/video url get video list",
	Args:  cobra.ArbitraryArgs,
	RunE:  runRootCmd,
}

// validateFlags validates CLI flag values before execution.
func validateFlags() error {
	if concurrency < 1 {
		return errors.New("concurrency must be at least 1")
	}
	if retries < 1 {
		return errors.New("retries must be at least 1")
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
			newList, err := niconico.GetVideoList(ctx, userID, comment, afterDate, beforeDate, tab, url, baseURL, retries, httpClientTimeout, limiter, maxPages, maxVideos, logger)
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
		niconico.NiconicoSort(outputIDs, tab, url)
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
		fmt.Fprintln(out, strings.Join(outputIDs, "\n"))
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

const (
	defaultBaseURL     = "https://nvapi.nicovideo.jp/v3"
	defaultHTTPTimeout = 10 * time.Second
	defaultRetries     = 10
)

var baseURL = defaultBaseURL

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	ExecuteContext(context.Background())
}

// ExecuteContext runs the root command with the provided context.
func ExecuteContext(ctx context.Context) {
	rootCmd.Version = Version
	cobra.CheckErr(rootCmd.ExecuteContext(ctx))
}

// init configures command flags and defaults.
func init() {
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true
	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.go-nico-list.yaml)")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	rootCmd.Flags().IntVarP(&comment, "comment", "c", 0, "lower comment limit `number`")
	rootCmd.Flags().StringVarP(&dateafter, "dateafter", "a", "10000101", "date `YYYYMMDD` after")
	rootCmd.Flags().StringVarP(&datebefore, "datebefore", "b", "99991231", "date `YYYYMMDD` before")
	rootCmd.Flags().BoolVarP(&tab, "tab", "t", false, "id tab Separated flag")
	rootCmd.Flags().BoolVarP(&url, "url", "u", false, "output id add url")

	rootCmd.Flags().IntVarP(&concurrency, "concurrency", "n", 3, "number of concurrent requests")

	rootCmd.Flags().DurationVar(&httpClientTimeout, "timeout", defaultHTTPTimeout, "HTTP client timeout")
	rootCmd.Flags().IntVar(&retries, "retries", defaultRetries, "number of retries for requests")
	rootCmd.Flags().Float64Var(&rateLimit, "rate-limit", 0, "maximum requests per second (0 disables)")
	rootCmd.Flags().DurationVar(&minInterval, "min-interval", 0, "minimum interval between requests (0 disables)")
	rootCmd.Flags().IntVar(&maxPages, "max-pages", 0, "maximum number of pages to fetch (0 disables)")
	rootCmd.Flags().IntVar(&maxVideos, "max-videos", 0, "maximum number of filtered IDs to collect (0 disables)")
	rootCmd.Flags().StringVar(&inputFilePath, "input-file", "", "read inputs from file (newline-separated)")
	rootCmd.Flags().BoolVar(&readStdin, "stdin", false, "read inputs from stdin (newline-separated)")
	rootCmd.Flags().StringVar(&logFilePath, "logfile", "", "log output file path")
	rootCmd.Flags().BoolVar(&forceProgress, "progress", false, "force enable progress output")
	rootCmd.Flags().BoolVar(&noProgress, "no-progress", false, "disable progress output")
	rootCmd.Flags().BoolVar(&strictInput, "strict", false, "return non-zero if any input is invalid")
	rootCmd.Flags().BoolVar(&bestEffort, "best-effort", false, "always exit 0 while logging fetch errors")
	rootCmd.Flags().BoolVar(&dedupeOutput, "dedupe", false, "remove duplicate output IDs before sorting")
	rootCmd.Flags().BoolVar(&jsonOutput, "json", false, "emit JSON output to stdout")

}

// jsonInputs summarizes input counts for JSON output.
type jsonInputs struct {
	Total   int64 `json:"total"`
	Valid   int64 `json:"valid"`
	Invalid int64 `json:"invalid"`
}

// userResult captures per-user results for JSON output.
type userResult struct {
	UserID string   `json:"user_id"`
	Items  []string `json:"items"`
	Error  string   `json:"error"`
}

// jsonOutputPayload defines the JSON output schema.
type jsonOutputPayload struct {
	Inputs      jsonInputs   `json:"inputs"`
	Invalid     []string     `json:"invalid"`
	Users       []userResult `json:"users"`
	Errors      []string     `json:"errors"`
	OutputCount int          `json:"output_count"`
	Items       []string     `json:"items"`
}

// buildJSONOutput assembles the JSON payload from run results.
func buildJSONOutput(
	totalInputs int64,
	validInputs int64,
	invalidInputs int64,
	invalidInputsList []string,
	userResults []userResult,
	errorsList []string,
	outputCount int,
	outputIDs []string,
) jsonOutputPayload {
	items := make([]string, 0, len(outputIDs))
	for _, id := range outputIDs {
		items = append(items, normalizeOutputID(id))
	}
	users := make([]userResult, 0, len(userResults))
	for _, user := range userResults {
		users = append(users, userResult{
			UserID: user.UserID,
			Items:  normalizeOutputList(user.Items),
			Error:  user.Error,
		})
	}
	return jsonOutputPayload{
		Inputs: jsonInputs{
			Total:   totalInputs,
			Valid:   validInputs,
			Invalid: invalidInputs,
		},
		Invalid:     append([]string{}, invalidInputsList...),
		Users:       users,
		Errors:      append([]string{}, errorsList...),
		OutputCount: outputCount,
		Items:       items,
	}
}

const nicoWatchURLPrefix = "https://www.nicovideo.jp/watch/"

// normalizeOutputID strips tab and URL prefixes from an output ID.
func normalizeOutputID(id string) string {
	id = strings.TrimLeft(id, "\t")
	return strings.TrimPrefix(id, nicoWatchURLPrefix)
}

// normalizeOutputList normalizes a list of output IDs.
func normalizeOutputList(items []string) []string {
	normalized := make([]string, 0, len(items))
	for _, item := range items {
		normalized = append(normalized, normalizeOutputID(item))
	}
	return normalized
}

// inputStream bundles input channels and total count metadata.
type inputStream struct {
	inputs     <-chan string
	errs       <-chan error
	totalKnown bool
	total      int64
}

// newProgressBar creates a progress bar configured for the current run.
func newProgressBar(cmd *cobra.Command, totalKnown bool, total int64) *progressbar.ProgressBar {
	if !totalKnown {
		total = -1
	}
	var errWriter io.Writer = os.Stderr
	if cmd != nil {
		errWriter = cmd.ErrOrStderr()
	}
	visible := shouldShowProgress(errWriter)
	writer := errWriter
	if !visible {
		writer = io.Discard
	}
	return progressBarNew(total, writer, visible)
}

// shouldShowProgress reports whether progress output should be visible.
func shouldShowProgress(errWriter io.Writer) bool {
	visible := isTerminal(errWriter)
	if forceProgress {
		visible = true
	}
	if noProgress {
		visible = false
	}
	return visible
}

// defaultIsTerminal reports whether the writer is a terminal.
func defaultIsTerminal(w io.Writer) bool {
	if file, ok := w.(*os.File); ok {
		return term.IsTerminal(int(file.Fd()))
	}
	return false
}

// streamInputs streams inputs from args, input files, and stdin.
func streamInputs(cmd *cobra.Command, args []string) inputStream {
	out := make(chan string)
	errCh := make(chan error, 1)
	totalKnown := inputFilePath == "" && !readStdin
	total := int64(len(args))

	go func() {
		defer close(out)
		defer close(errCh)

		count := 0
		for _, arg := range args {
			out <- arg
			count++
		}

		if inputFilePath != "" {
			n, err := streamLinesFromFile(inputFilePath, out)
			count += n
			if err != nil {
				errCh <- err
				return
			}
		}

		if readStdin {
			var reader io.Reader = os.Stdin
			if cmd != nil {
				reader = cmd.InOrStdin()
			}
			n, err := streamLines(reader, out)
			count += n
			if err != nil {
				errCh <- err
				return
			}
		}

		if count == 0 {
			errCh <- errors.New("no inputs provided")
		}
	}()

	return inputStream{
		inputs:     out,
		errs:       errCh,
		totalKnown: totalKnown,
		total:      total,
	}
}

// streamLinesFromFile streams trimmed lines from a file into out.
func streamLinesFromFile(path string, out chan<- string) (int, error) {
	file, err := openInputFile(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	return streamLines(file, out)
}

// streamLines streams non-empty trimmed lines from a reader into out.
func streamLines(reader io.Reader, out chan<- string) (int, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	count := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		out <- line
		count++
	}
	if err := scanner.Err(); err != nil {
		return count, err
	}
	return count, nil
}
