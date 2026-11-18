package cmd

import (
	"errors"
	"math"
	"time"

	"github.com/sh4869221b/go-nico-list/internal/niconico"
	"github.com/spf13/cobra"
)

func validateFlagsFor(cfg *RootConfig) error {
	if cfg.HTTPConcurrency < 0 {
		return errors.New("concurrency-http must be >= 0")
	}
	if cfg.Concurrency < 1 {
		return errors.New("concurrency-targets must be at least 1")
	}
	if cfg.PageConcurrency < 1 {
		return errors.New("concurrency-pages must be at least 1")
	}
	if cfg.Retries < 1 {
		return errors.New("retries must be at least 1")
	}
	if cfg.HTTPClientTimeout <= 0 {
		return errors.New("timeout must be greater than 0")
	}
	if math.IsNaN(cfg.RateLimit) || math.IsInf(cfg.RateLimit, 0) {
		return errors.New("rate-limit must be finite")
	}
	if cfg.RateLimit < 0 {
		return errors.New("rate-limit must be at least 0")
	}
	if cfg.MinInterval < 0 {
		return errors.New("min-interval must be at least 0")
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

// resolveConcurrency configures shared HTTP admission and applies explicit target and page worker limits.
func resolveConcurrency(cmd *cobra.Command, cfg *RootConfig) (control *niconico.HTTPControl, targets, pages int) {
	targets, pages = cfg.Concurrency, cfg.PageConcurrency
	if !cfg.AdaptiveConcurrency {
		return niconico.NewHTTPControl(cfg.HTTPConcurrency, cfg.HTTPMetrics), targets, pages
	}

	flags := cmd.Flags()
	control = niconico.NewAdaptiveConcurrencyControl(cfg.HTTPConcurrency, flags.Changed("concurrency-http"), cfg.HTTPMetrics)
	if !flags.Changed("concurrency-targets") {
		targets = 0
	}
	if !flags.Changed("concurrency-pages") {
		pages = 0
	}
	return control, targets, pages
}
