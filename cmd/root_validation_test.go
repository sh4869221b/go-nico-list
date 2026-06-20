package cmd

import (
	"testing"
	"time"
)

func TestRetriesValidation(t *testing.T) {
	_, _, err := executeTestRootCommand(t, newTestRootConfig(), newTestRootDeps(), "--retries=0", "12345")
	if err == nil || err.Error() != "retries must be at least 1" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRateLimitValidation(t *testing.T) {
	cfg := newTestRootConfig()
	cfg.RateLimit = -1
	_, _, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "nicovideo.jp/user/1")
	if err == nil || err.Error() != "rate-limit must be at least 0" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTimeoutValidation(t *testing.T) {
	for _, timeout := range []string{"0s", "-1s"} {
		_, _, err := executeTestRootCommand(t, newTestRootConfig(), newTestRootDeps(), "--timeout", timeout, "nicovideo.jp/user/1")
		if err == nil || err.Error() != "timeout must be greater than 0" {
			t.Fatalf("unexpected error for timeout %v: %v", timeout, err)
		}
	}
}

func TestDateRangeOrderValidation(t *testing.T) {
	cfg := newTestRootConfig()
	cfg.DateAfter = "20250102"
	cfg.DateBefore = "20250101"
	_, _, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "nicovideo.jp/user/1")
	if err == nil || err.Error() != "dateafter must be on or before datebefore" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDateAfterFormatValidation(t *testing.T) {
	cfg := newTestRootConfig()
	cfg.DateAfter = "2025-01-01"
	cfg.DateBefore = "20250101"
	_, _, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "nicovideo.jp/user/1")
	if err == nil || err.Error() != "dateafter format error" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDateBeforeFormatValidation(t *testing.T) {
	cfg := newTestRootConfig()
	cfg.DateAfter = "20250101"
	cfg.DateBefore = "2025-01-01"
	_, _, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "nicovideo.jp/user/1")
	if err == nil || err.Error() != "datebefore format error" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDateRangeSameDayAllowed(t *testing.T) {
	server := newEmptyAPIServer(t)
	cfg := testFetchConfig(server.URL)
	cfg.DateAfter = "20250101"
	cfg.DateBefore = "20250101"
	_, _, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "nicovideo.jp/user/1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMinIntervalValidation(t *testing.T) {
	cfg := newTestRootConfig()
	cfg.MinInterval = -time.Second
	_, _, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "nicovideo.jp/user/1")
	if err == nil || err.Error() != "min-interval must be at least 0" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConcurrencyValidation(t *testing.T) {
	_, _, err := executeTestRootCommand(t, newTestRootConfig(), newTestRootDeps(), "--concurrency=0", "12345")
	if err == nil || err.Error() != "concurrency must be at least 1" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPageConcurrencyValidation(t *testing.T) {
	_, _, err := executeTestRootCommand(t, newTestRootConfig(), newTestRootDeps(), "--page-concurrency=0", "12345")
	if err == nil || err.Error() != "page-concurrency must be at least 1" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPageConcurrencyFlagDocumentsPerTargetScope(t *testing.T) {
	cmd, _, _ := newTestRootCommand(t, newTestRootConfig(), newTestRootDeps())
	flag := cmd.Flags().Lookup("page-concurrency")
	if flag == nil {
		t.Fatalf("expected page-concurrency flag")
	}
	if flag.DefValue != "1" {
		t.Fatalf("expected default 1, got %q", flag.DefValue)
	}
	if got := flag.Usage; got != "number of concurrent page requests per target" {
		t.Fatalf("unexpected usage: %q", got)
	}
}
