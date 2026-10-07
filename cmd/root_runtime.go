package cmd

import (
	"errors"
	"time"
)

func validateFlagsFor(cfg *RootConfig) error {
	if cfg.HTTPConcurrency < 0 {
		return errors.New("http-concurrency must be >= 0")
	}
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
