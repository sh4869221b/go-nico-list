package cmd

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
)

func TestRetriesValidation(t *testing.T) {
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	retries = 0
	t.Cleanup(func() { retries = defaultRetries })

	rootCmd.SetArgs([]string{"12345"})
	err := rootCmd.Execute()
	if err == nil || err.Error() != "retries must be at least 1" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunRootCmdInvalidInput(t *testing.T) {
	// prepare custom progress bar to capture completion state
	var bar *progressbar.ProgressBar
	progressBarNew = func(max int64, description ...string) *progressbar.ProgressBar {
		bar = progressbar.NewOptions64(max, progressbar.OptionSetWriter(io.Discard))
		return bar
	}
	t.Cleanup(func() { progressBarNew = progressbar.Default })

	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	dateafter = "10000101"
	datebefore = "99991231"

	if err := runRootCmd(nil, []string{"invalid"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bar == nil || !bar.IsFinished() {
		t.Errorf("progress bar not finished")
	}
}

func TestRunRootCmdInvalidInputNoOutput(t *testing.T) {
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	dateafter = "10000101"
	datebefore = "99991231"

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())

	if err := runRootCmd(cmd, []string{"invalid"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no stdout output, got %q", out.String())
	}
}

func TestRunRootCmdPartialFailureOutputsResults(t *testing.T) {
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	dateafter = "10000101"
	datebefore = "99991231"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/users/1/") {
			if r.URL.Query().Get("page") != "1" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"data":{"items":[]}}`)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":{"items":[{"essential":{"id":"sm1","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}}]}}`)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "invalid")
	}))
	t.Cleanup(server.Close)

	oldBaseURL := baseURL
	baseURL = server.URL
	t.Cleanup(func() { baseURL = oldBaseURL })

	oldRetries := retries
	retries = 1
	t.Cleanup(func() { retries = oldRetries })

	oldConcurrency := concurrency
	concurrency = 1
	t.Cleanup(func() { concurrency = oldConcurrency })

	oldTimeout := httpClientTimeout
	httpClientTimeout = time.Second
	t.Cleanup(func() { httpClientTimeout = oldTimeout })

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())

	if err := runRootCmd(cmd, []string{"nicovideo.jp/user/1", "nicovideo.jp/user/2"}); err == nil {
		t.Fatalf("expected error")
	}
	if got := out.String(); got != "sm1\n" {
		t.Errorf("unexpected stdout output: %q", got)
	}
}

func TestRunRootCmdLogFile(t *testing.T) {
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	dateafter = "10000101"
	datebefore = "99991231"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "invalid")
	}))
	t.Cleanup(server.Close)

	oldBaseURL := baseURL
	baseURL = server.URL
	t.Cleanup(func() { baseURL = oldBaseURL })

	oldRetries := retries
	retries = 1
	t.Cleanup(func() { retries = oldRetries })

	oldConcurrency := concurrency
	concurrency = 1
	t.Cleanup(func() { concurrency = oldConcurrency })

	oldTimeout := httpClientTimeout
	httpClientTimeout = time.Second
	t.Cleanup(func() { httpClientTimeout = oldTimeout })

	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "app.log")
	oldLogFilePath := logFilePath
	logFilePath = logPath
	t.Cleanup(func() { logFilePath = oldLogFilePath })

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())

	if err := runRootCmd(cmd, []string{"12345"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected logfile to be created: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("expected logfile to contain logs")
	}
}

func TestRunRootCmdLogFileMultipleErrors(t *testing.T) {
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	dateafter = "10000101"
	datebefore = "99991231"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "invalid")
	}))
	t.Cleanup(server.Close)

	oldBaseURL := baseURL
	baseURL = server.URL
	t.Cleanup(func() { baseURL = oldBaseURL })

	oldRetries := retries
	retries = 1
	t.Cleanup(func() { retries = oldRetries })

	oldConcurrency := concurrency
	concurrency = 1
	t.Cleanup(func() { concurrency = oldConcurrency })

	oldTimeout := httpClientTimeout
	httpClientTimeout = time.Second
	t.Cleanup(func() { httpClientTimeout = oldTimeout })

	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "app.log")
	oldLogFilePath := logFilePath
	logFilePath = logPath
	t.Cleanup(func() { logFilePath = oldLogFilePath })

	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())

	if err := runRootCmd(cmd, []string{"nicovideo.jp/user/1", "nicovideo.jp/user/2"}); err == nil {
		t.Fatalf("expected error")
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected logfile to be created: %v", err)
	}
	if got := bytes.Count(data, []byte("failed to get video list")); got < 2 {
		t.Fatalf("expected at least 2 error logs, got %d", got)
	}
}

func TestConcurrencyValidation(t *testing.T) {
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	concurrency = 0
	t.Cleanup(func() { concurrency = 3 })

	rootCmd.SetArgs([]string{"12345"})
	err := rootCmd.Execute()
	if err == nil || err.Error() != "concurrency must be at least 1" {
		t.Fatalf("unexpected error: %v", err)
	}
}
