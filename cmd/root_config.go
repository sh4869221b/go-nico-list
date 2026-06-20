package cmd

import (
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

// RootConfig contains all root command flag values and runtime defaults.
type RootConfig struct {
	Comment           int
	DateAfter         string
	DateBefore        string
	URL               bool
	Concurrency       int
	PageConcurrency   int
	Retries           int
	HTTPClientTimeout time.Duration
	InputFilePath     string
	ReadStdin         bool
	LogFilePath       string
	ForceProgress     bool
	NoProgress        bool
	StrictInput       bool
	BestEffort        bool
	DedupeOutput      bool
	NoSortOutput      bool
	JSONOutput        bool
	RateLimit         float64
	MinInterval       time.Duration
	BaseURL           string
	Version           string
}

// RootDeps contains external dependencies used by the root command.
type RootDeps struct {
	Stdout         io.Writer
	Stderr         io.Writer
	Logger         *slog.Logger
	OpenLogFile    func(string) (io.WriteCloser, error)
	ProgressBarNew func(int64, io.Writer, bool) *progressbar.ProgressBar
	OpenInputFile  func(string) (io.ReadCloser, error)
	IsTerminal     func(io.Writer) bool
}

// DefaultConfig returns the CLI's default root command configuration.
func DefaultConfig() RootConfig {
	return RootConfig{
		DateAfter:         "10000101",
		DateBefore:        "99991231",
		Concurrency:       3,
		PageConcurrency:   1,
		Retries:           defaultRetries,
		HTTPClientTimeout: defaultHTTPTimeout,
		BaseURL:           defaultBaseURL,
		Version:           Version,
	}
}

// DefaultDeps returns the root command's default OS-backed dependencies.
func DefaultDeps() RootDeps {
	return RootDeps{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Logger: slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{})),
		OpenLogFile: func(path string) (io.WriteCloser, error) {
			return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		},
		ProgressBarNew: func(max int64, writer io.Writer, visible bool) *progressbar.ProgressBar {
			return progressbar.NewOptions64(
				max,
				progressbar.OptionSetWriter(writer),
				progressbar.OptionSetVisibility(visible),
			)
		},
		OpenInputFile: func(path string) (io.ReadCloser, error) { return os.Open(path) },
		IsTerminal:    defaultIsTerminal,
	}
}

// NewRootCommand creates a fresh root command with flags bound to local config.
func NewRootCommand(cfg RootConfig, deps RootDeps) *cobra.Command {
	cfg = normalizeRootConfig(cfg)
	deps = normalizeRootDeps(deps)

	cmd := &cobra.Command{
		Use:           "go-nico-list",
		Short:         "niconico {user}/video url get video list",
		Args:          cobra.ArbitraryArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       cfg.Version,
	}
	cmd.SetOut(deps.Stdout)
	cmd.SetErr(deps.Stderr)
	cmd.Flags().IntVarP(&cfg.Comment, "comment", "c", cfg.Comment, "lower comment limit `number`")
	cmd.Flags().StringVarP(&cfg.DateAfter, "dateafter", "a", cfg.DateAfter, "date `YYYYMMDD` after")
	cmd.Flags().StringVarP(&cfg.DateBefore, "datebefore", "b", cfg.DateBefore, "date `YYYYMMDD` before")
	cmd.Flags().BoolVarP(&cfg.URL, "url", "u", cfg.URL, "output id add url")
	cmd.Flags().IntVarP(&cfg.Concurrency, "concurrency", "n", cfg.Concurrency, "number of concurrent requests")
	cmd.Flags().IntVar(&cfg.PageConcurrency, "page-concurrency", cfg.PageConcurrency, "number of concurrent page requests per target")
	cmd.Flags().DurationVar(&cfg.HTTPClientTimeout, "timeout", cfg.HTTPClientTimeout, "HTTP client timeout")
	cmd.Flags().IntVar(&cfg.Retries, "retries", cfg.Retries, "number of retries for requests")
	cmd.Flags().Float64Var(&cfg.RateLimit, "rate-limit", cfg.RateLimit, "maximum requests per second (0 disables)")
	cmd.Flags().DurationVar(&cfg.MinInterval, "min-interval", cfg.MinInterval, "minimum interval between requests (0 disables)")
	cmd.Flags().StringVar(&cfg.InputFilePath, "input-file", cfg.InputFilePath, "read inputs from file (newline-separated)")
	cmd.Flags().BoolVar(&cfg.ReadStdin, "stdin", cfg.ReadStdin, "read inputs from stdin (newline-separated)")
	cmd.Flags().StringVar(&cfg.LogFilePath, "logfile", cfg.LogFilePath, "log output file path")
	cmd.Flags().BoolVar(&cfg.ForceProgress, "progress", cfg.ForceProgress, "force enable progress output")
	cmd.Flags().BoolVar(&cfg.NoProgress, "no-progress", cfg.NoProgress, "disable progress output")
	cmd.Flags().BoolVar(&cfg.StrictInput, "strict", cfg.StrictInput, "return non-zero if any input is invalid")
	cmd.Flags().BoolVar(&cfg.BestEffort, "best-effort", cfg.BestEffort, "always exit 0 while logging fetch errors")
	cmd.Flags().BoolVar(&cfg.DedupeOutput, "dedupe", cfg.DedupeOutput, "remove duplicate output IDs before output")
	cmd.Flags().BoolVar(&cfg.NoSortOutput, "no-sort", cfg.NoSortOutput, "skip sorting output IDs for faster output")
	cmd.Flags().BoolVar(&cfg.JSONOutput, "json", cfg.JSONOutput, "emit JSON output to stdout")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runRootCmdWithConfig(cmd, args, &cfg, deps)
	}
	return cmd
}

func normalizeRootConfig(cfg RootConfig) RootConfig {
	defaults := DefaultConfig()
	if cfg.DateAfter == "" {
		cfg.DateAfter = defaults.DateAfter
	}
	if cfg.DateBefore == "" {
		cfg.DateBefore = defaults.DateBefore
	}
	if cfg.Concurrency == 0 {
		cfg.Concurrency = defaults.Concurrency
	}
	if cfg.PageConcurrency == 0 {
		cfg.PageConcurrency = defaults.PageConcurrency
	}
	if cfg.Retries == 0 {
		cfg.Retries = defaults.Retries
	}
	if cfg.HTTPClientTimeout == 0 {
		cfg.HTTPClientTimeout = defaults.HTTPClientTimeout
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaults.BaseURL
	}
	if cfg.Version == "" {
		cfg.Version = defaults.Version
	}
	return cfg
}

func normalizeRootDeps(deps RootDeps) RootDeps {
	defaults := DefaultDeps()
	if deps.Stdout == nil {
		deps.Stdout = defaults.Stdout
	}
	if deps.Stderr == nil {
		deps.Stderr = defaults.Stderr
	}
	if deps.Logger == nil {
		deps.Logger = defaults.Logger
	}
	if deps.OpenLogFile == nil {
		deps.OpenLogFile = defaults.OpenLogFile
	}
	if deps.ProgressBarNew == nil {
		deps.ProgressBarNew = defaults.ProgressBarNew
	}
	if deps.OpenInputFile == nil {
		deps.OpenInputFile = defaults.OpenInputFile
	}
	if deps.IsTerminal == nil {
		deps.IsTerminal = defaults.IsTerminal
	}
	return deps
}
