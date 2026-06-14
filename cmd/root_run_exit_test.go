package cmd

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunRootCmdEmitsSummary(t *testing.T) {
	server := newEmptyAPIServer(t)
	cfg := testFetchConfig(server.URL)
	_, errOut, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "nicovideo.jp/user/1", "invalid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := errOut.String()
	want := "summary inputs=2 valid=1 invalid=1 fetch_ok=1 fetch_err=0 output_count=0"
	if !strings.Contains(got, want) {
		t.Fatalf("expected summary %q, got %q", want, got)
	}
}

func TestRunRootCmdStrictInvalidInputReturnsError(t *testing.T) {
	cfg := newTestRootConfig()
	cfg.StrictInput = true
	out, _, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "invalid")
	if err == nil || err.Error() != "invalid input detected" {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no stdout output, got %q", out.String())
	}
}

func TestRunRootCmdStrictInvalidStillOutputsValidResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "1" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":{"items":[]}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"items":[{"essential":{"id":"sm1","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}}]}}`)
	}))
	t.Cleanup(server.Close)
	cfg := testFetchConfig(server.URL)
	cfg.StrictInput = true

	out, _, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "nicovideo.jp/user/1", "invalid")
	if err == nil || err.Error() != "invalid input detected" {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := out.String(); got != "sm1\n" {
		t.Errorf("unexpected stdout output: %q", got)
	}
}

func TestRunRootCmdBestEffortReturnsNilOnFetchError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "invalid")
	}))
	t.Cleanup(server.Close)
	cfg := testFetchConfig(server.URL)
	cfg.BestEffort = true

	_, _, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "nicovideo.jp/user/1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunRootCmdStrictOverridesBestEffort(t *testing.T) {
	cfg := newTestRootConfig()
	cfg.BestEffort = true
	cfg.StrictInput = true
	_, _, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "invalid")
	if err == nil || err.Error() != "invalid input detected" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunRootCmdInvalidInputNoOutput(t *testing.T) {
	out, _, err := executeTestRootCommand(t, newTestRootConfig(), newTestRootDeps(), "invalid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no stdout output, got %q", out.String())
	}
}

func TestRunRootCmdReturnsLineOutputWriteError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") != "1" {
			_, _ = io.WriteString(w, `{"data":{"items":[]}}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":{"items":[{"essential":{"id":"sm1","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}}]}}`)
	}))
	t.Cleanup(server.Close)
	cfg := testFetchConfig(server.URL)
	writeErr := errors.New("stdout failed")
	var errOut bytes.Buffer
	deps := newTestRootDeps()
	deps.Stdout = errorWriter{err: writeErr}
	deps.Stderr = &errOut

	_, _, err := executeTestRootCommand(t, cfg, deps, "nicovideo.jp/user/1")
	if !errors.Is(err, writeErr) {
		t.Fatalf("expected stdout error, got %v", err)
	}
	wantSummary := "summary inputs=1 valid=1 invalid=0 fetch_ok=1 fetch_err=0 output_count=1"
	if got := errOut.String(); !strings.Contains(got, wantSummary) {
		t.Fatalf("expected summary %q, got %q", wantSummary, got)
	}
}

func TestRunRootCmdReturnsSummaryWriteError(t *testing.T) {
	server := newEmptyAPIServer(t)
	cfg := testFetchConfig(server.URL)
	writeErr := errors.New("stderr failed")
	deps := newTestRootDeps()
	deps.Stderr = errorWriter{err: writeErr}

	_, _, err := executeTestRootCommand(t, cfg, deps, "nicovideo.jp/user/1")
	if !errors.Is(err, writeErr) {
		t.Fatalf("expected stderr error, got %v", err)
	}
}
