/*
Copyright (c) 2024 sh4869221b <sh4869221b@gmail.com>
*/
package cmd

import (
	"context"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/schollz/progressbar/v3"
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

const (
	defaultBaseURL     = "https://nvapi.nicovideo.jp/v3"
	defaultHTTPTimeout = 10 * time.Second
	defaultRetries     = 10
)

var baseURL = defaultBaseURL

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "go-nico-list",
	Short: "niconico {user}/video url get video list",
	Args:  cobra.ArbitraryArgs,
	RunE:  runRootCmd,
}

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
