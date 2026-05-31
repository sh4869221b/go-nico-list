package cmd

import (
	"bufio"
	"bytes"
	"context"
	"errors"
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

func newTestRootConfig() RootConfig {
	cfg := DefaultConfig()
	cfg.NoProgress = true
	cfg.ForceProgress = false
	return cfg
}

func newTestRootDeps() RootDeps {
	deps := RootDeps{}
	deps.Logger = slog.New(slog.DiscardHandler)
	deps.IsTerminal = func(io.Writer) bool { return false }
	deps.ProgressBarNew = func(max int64, writer io.Writer, visible bool) *progressbar.ProgressBar {
		return progressbar.NewOptions64(
			max,
			progressbar.OptionSetWriter(writer),
			progressbar.OptionSetVisibility(visible),
		)
	}
	return deps
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

type testRunner struct {
	t      *testing.T
	cmd    runnableCommand
	stdout *bytes.Buffer
}

func newTestRunner(t *testing.T, cfg RootConfig, deps RootDeps) *testRunner {
	t.Helper()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	if deps.Stdout == nil {
		deps.Stdout = stdout
	}
	if deps.Stderr == nil {
		deps.Stderr = stderr
	}

	cmd := NewRootCommand(cfg, deps)
	cmd.SetContext(context.Background())

	return &testRunner{
		t:      t,
		cmd:    cmd,
		stdout: stdout,
	}
}

func (tr *testRunner) run(args ...string) error {
	tr.t.Helper()
	tr.cmd.SetArgs(args)
	return tr.cmd.Execute()
}

func (tr *testRunner) stdoutString() string {
	tr.t.Helper()
	if buf, ok := tr.cmd.OutOrStdout().(*bytes.Buffer); ok {
		return buf.String()
	}
	return tr.stdout.String()
}

type runnableCommand interface {
	SetArgs([]string)
	SetContext(context.Context)
	Execute() error
	OutOrStdout() io.Writer
	ErrOrStderr() io.Writer
}

type recordingHandler struct {
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *recordingHandler) WithGroup(string) slog.Handler { return h }

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

func writeInputsFile(t *testing.T, lines string) string {
	t.Helper()
	tmpDir := t.TempDir()
	inputPath := filepath.Join(tmpDir, "inputs.txt")
	if err := os.WriteFile(inputPath, []byte(lines), 0o644); err != nil {
		t.Fatalf("failed to write inputs: %v", err)
	}
	return inputPath
}

func requireTooLongInputError(t *testing.T, err error) {
	t.Helper()
	if err == nil || !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("unexpected error: %v", err)
	}
}
