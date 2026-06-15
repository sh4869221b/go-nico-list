package cmd

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"
)

func validateFlagsFor(cfg *RootConfig) error {
	if cfg.Concurrency < 1 {
		return errors.New("concurrency must be at least 1")
	}
	if cfg.PageConcurrency < 1 {
		return errors.New("page-concurrency must be at least 1")
	}
	if cfg.Retries < 1 {
		return errors.New("retries must be at least 1")
	}
	if cfg.HTTPClientTimeout <= 0 {
		return errors.New("timeout must be greater than 0")
	}
	if cfg.RateLimit < 0 {
		return errors.New("rate-limit must be at least 0")
	}
	if cfg.MinInterval < 0 {
		return errors.New("min-interval must be at least 0")
	}
	if cfg.MaxPages < 0 {
		return errors.New("max-pages must be at least 0")
	}
	if cfg.MaxVideos < 0 {
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

func setupLoggerFor(path string, deps RootDeps) (*slog.Logger, func() error, error) {
	deps = normalizeRootDeps(deps)
	if path == "" {
		return deps.Logger, func() error { return nil }, nil
	}
	logFile, err := deps.OpenLogFile(path)
	if err != nil {
		return nil, func() error { return nil }, err
	}
	cleanup := func() error { return logFile.Close() }
	logger := slog.New(slog.NewJSONHandler(logFile, &slog.HandlerOptions{}))
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
