package cmd

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestRunRootCmdLogFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "invalid")
	}))
	t.Cleanup(server.Close)
	cfg := testFetchConfig(server.URL)
	cfg.LogFilePath = filepath.Join(t.TempDir(), "app.log")

	_, _, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "nicovideo.jp/user/1")
	if err == nil {
		t.Fatalf("expected fetch error")
	}
	data, err := os.ReadFile(cfg.LogFilePath)
	if err != nil {
		t.Fatalf("expected logfile to be created: %v", err)
	}
	if !bytes.Contains(data, []byte("failed to get video list")) {
		t.Fatalf("expected fetch error logs in logfile, got %q", string(data))
	}
}

func TestRunRootCmdLogFileMultipleErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "invalid")
	}))
	t.Cleanup(server.Close)
	cfg := testFetchConfig(server.URL)
	cfg.LogFilePath = filepath.Join(t.TempDir(), "app.log")

	_, _, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "nicovideo.jp/user/1", "nicovideo.jp/user/2")
	if err == nil {
		t.Fatalf("expected error")
	}
	data, err := os.ReadFile(cfg.LogFilePath)
	if err != nil {
		t.Fatalf("expected logfile to be created: %v", err)
	}
	if got := bytes.Count(data, []byte("failed to get video list")); got < 2 {
		t.Fatalf("expected at least 2 error logs, got %d", got)
	}
}
