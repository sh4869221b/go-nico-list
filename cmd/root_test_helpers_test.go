package cmd

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func newTestRootConfig() RootConfig {
	cfg := DefaultConfig()
	cfg.NoProgress = true
	return cfg
}

func newTestRootDeps() RootDeps {
	return RootDeps{
		Logger:     slog.New(slog.DiscardHandler),
		IsTerminal: func(io.Writer) bool { return false },
	}
}

func newTestRootCommand(t *testing.T, cfg RootConfig, deps RootDeps) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	if deps.Stdout == nil {
		deps.Stdout = out
	}
	if deps.Stderr == nil {
		deps.Stderr = errOut
	}
	if deps.Logger == nil {
		deps.Logger = slog.New(slog.DiscardHandler)
	}
	if deps.IsTerminal == nil {
		deps.IsTerminal = func(io.Writer) bool { return false }
	}
	cmd := NewRootCommand(cfg, deps)
	cmd.SetContext(context.Background())
	return cmd, out, errOut
}

func executeTestRootCommand(t *testing.T, cfg RootConfig, deps RootDeps, args ...string) (*bytes.Buffer, *bytes.Buffer, error) {
	t.Helper()
	cmd, out, errOut := newTestRootCommand(t, cfg, deps)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out, errOut, err
}

func newEmptyAPIServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"items":[]}}`)
	}))
	t.Cleanup(server.Close)
	return server
}

func testFetchConfig(serverURL string) RootConfig {
	cfg := newTestRootConfig()
	cfg.BaseURL = serverURL
	cfg.Retries = 1
	cfg.Concurrency = 1
	cfg.HTTPClientTimeout = time.Second
	return cfg
}

type blockingErrorReader struct {
	wait <-chan struct{}
	err  error
}

func (r blockingErrorReader) Read(p []byte) (int, error) {
	<-r.wait
	return 0, r.err
}

type closeErrorReader struct {
	*strings.Reader
	err error
}

func (r closeErrorReader) Close() error { return r.err }

type errorWriter struct{ err error }

func (w errorWriter) Write([]byte) (int, error) { return 0, w.err }
