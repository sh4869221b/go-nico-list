package cmd

import (
	"bytes"
	"errors"
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

	_, _, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "nicovideo.jp/user/1", "nicovideo.jp/user/2", "invalid")
	if err == nil {
		t.Fatalf("expected fetch error")
	}
	data, err := os.ReadFile(cfg.LogFilePath)
	if err != nil {
		t.Fatalf("expected logfile to be created: %v", err)
	}
	if got := bytes.Count(data, []byte("failed to get video list")); got != 2 {
		t.Fatalf("expected 2 fetch error logs in logfile, got %d: %s", got, data)
	}
	if !bytes.Contains(data, []byte(`"level":"WARN","msg":"invalid input"`)) {
		t.Fatalf("expected invalid input warning in logfile, got %s", data)
	}
}

func TestRunRootCmdLogFileCloseError(t *testing.T) {
	closeErr := errors.New("close failed")
	cfg := RootConfig{LogFilePath: "dummy.log", NoProgress: true}
	deps := newTestRootDeps()
	deps.OpenLogFile = func(string) (io.WriteCloser, error) {
		return failingCloseWriter{closeErr: closeErr}, nil
	}
	_, _, err := executeTestRootCommand(t, cfg, deps, "invalid")
	if !errors.Is(err, closeErr) {
		t.Fatalf("expected logfile close error, got %v", err)
	}
}

type failingCloseWriter struct{ closeErr error }

func (f failingCloseWriter) Write(p []byte) (int, error) { return len(p), nil }
func (f failingCloseWriter) Close() error                { return f.closeErr }
