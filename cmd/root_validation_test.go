package cmd

import (
	"strings"
	"testing"
)

func TestFlagValidation(t *testing.T) {
	for _, test := range []struct {
		arg, want string
	}{
		{"--retries=0", "retries must be at least 1"},
		{"--rate-limit=-1", "rate-limit must be at least 0"},
		{"--timeout=0s", "timeout must be greater than 0"},
		{"--timeout=-1s", "timeout must be greater than 0"},
		{"--dateafter=2025-01-01", "dateafter format error"},
		{"--datebefore=2025-01-01", "datebefore format error"},
		{"--min-interval=-1s", "min-interval must be at least 0"},
		{"--concurrency=0", "concurrency must be at least 1"},
		{"--page-concurrency=0", "page-concurrency must be at least 1"},
	} {
		t.Run(test.arg, func(t *testing.T) {
			out, errOut, err := executeTestRootCommand(t, newTestRootConfig(), newTestRootDeps(), test.arg, "nicovideo.jp/user/1")
			if err == nil || err.Error() != test.want {
				t.Fatalf("error=%v, want %q", err, test.want)
			}
			if out.Len() != 0 || strings.Contains(errOut.String(), "Usage:") {
				t.Fatalf("validation wrote data or usage: stdout=%q stderr=%q", out.String(), errOut.String())
			}
		})
	}
}

func TestDateRangeValidation(t *testing.T) {
	server := newEmptyAPIServer(t)
	for _, test := range []struct {
		before, want string
	}{
		{"20250101", "dateafter must be on or before datebefore"},
		{"20250102", ""},
	} {
		t.Run(test.before, func(t *testing.T) {
			cfg := testFetchConfig(server.URL)
			cfg.DateAfter, cfg.DateBefore = "20250102", test.before
			_, _, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "nicovideo.jp/user/1")
			if test.want == "" {
				if err != nil {
					t.Fatalf("same-day range rejected: %v", err)
				}
			} else if err == nil || err.Error() != test.want {
				t.Fatalf("error=%v, want %q", err, test.want)
			}
		})
	}
}
