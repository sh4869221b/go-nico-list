package cmd

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestNewRootCommand_FreshCommandIsolationAcrossConfigs(t *testing.T) {
	runnerWithURL := newTestRunner(t, RootConfig{URL: true}, RootDeps{})
	runnerPlain := newTestRunner(t, RootConfig{URL: false}, RootDeps{})

	if err := runnerWithURL.run("invalid"); err != nil {
		t.Fatalf("unexpected error from URL runner: %v", err)
	}
	if err := runnerPlain.run("invalid"); err != nil {
		t.Fatalf("unexpected error from plain runner: %v", err)
	}

	if got := runnerPlain.stdoutString(); strings.Contains(got, nicoWatchURLPrefix) {
		t.Fatalf("expected second command output to avoid URL prefix leakage, got %q", got)
	}
}

func TestNewRootCommand_WarnsWithInvalidInputMessage(t *testing.T) {
	h := &recordingHandler{}
	deps := RootDeps{Logger: slog.New(h)}
	runner := newTestRunner(t, RootConfig{}, deps)

	if err := runner.run("invalid"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, rec := range h.records {
		if rec.Level == slog.LevelWarn {
			if rec.Message != "invalid input" {
				t.Fatalf("expected warn message %q, got %q", "invalid input", rec.Message)
			}
			return
		}
	}
	t.Fatal("expected at least one warn record")
}

func TestNewRootCommand_PropagatesLogFileCloseError(t *testing.T) {
	closeErr := errors.New("close failed")
	runner := newTestRunner(t, RootConfig{LogFilePath: "dummy.log"}, RootDeps{
		OpenLogFile: func(string) (io.WriteCloser, error) {
			return failingCloseWriter{closeErr: closeErr}, nil
		},
	})

	err := runner.run("invalid")
	if !errors.Is(err, closeErr) {
		t.Fatalf("expected close error %v, got %v", closeErr, err)
	}
}

type failingCloseWriter struct {
	closeErr error
}

func (f failingCloseWriter) Write(p []byte) (int, error) { return len(p), nil }

func (f failingCloseWriter) Close() error { return f.closeErr }
