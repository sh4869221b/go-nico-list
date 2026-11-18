package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunRootCmdReturnsOutputWriteError(t *testing.T) {
	server := newSingleVideoServer(t)
	for _, mode := range []httpCommandOutputMode{
		{name: "sorted"},
		{name: "no-sort", noSort: true},
		{name: "json", json: true},
	} {
		t.Run(mode.name, func(t *testing.T) {
			cfg := testFetchConfig(server.URL)
			cfg.JSONOutput, cfg.NoSortOutput = mode.json, mode.noSort
			writeErr := errors.New("stdout failed")
			var errOut bytes.Buffer
			deps := newTestRootDeps()
			deps.Stdout, deps.Stderr = errorWriter{err: writeErr}, &errOut
			_, _, err := executeTestRootCommand(t, cfg, deps, "nicovideo.jp/user/1")
			if !errors.Is(err, writeErr) {
				t.Fatalf("expected stdout error, got %v", err)
			}
			want := "summary inputs=1 valid=1 invalid=0 fetch_ok=1 fetch_err=0 output_count=0"
			if got := errOut.String(); !strings.Contains(got, want) {
				t.Fatalf("expected summary %q, got %q", want, got)
			}
		})
	}
}

func TestRunRootCmdCountsOnlyCompleteOutputOnShortWrite(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ids []string
		if r.URL.Query().Get("page") == "1" {
			ids = []string{"sm1", "sm2"}
		}
		_, _ = io.WriteString(w, httpCommandPagePayload(false, 2, ids))
	}))
	t.Cleanup(server.Close)
	line := nicoWatchURLPrefix + "sm1\n"
	for _, tt := range []struct {
		name    string
		mode    httpCommandOutputMode
		written func([]byte) int
		count   int
	}{
		{name: "sorted", written: func([]byte) int { return len(line) + 2 }, count: 1},
		{name: "no-sort", mode: httpCommandOutputMode{noSort: true}, written: func([]byte) int { return len(line) + 2 }, count: 1},
		{name: "partial JSON", mode: httpCommandOutputMode{json: true}, written: func(p []byte) int { return len(p) - 2 }},
		{name: "JSON without newline", mode: httpCommandOutputMode{json: true}, written: func(p []byte) int { return len(p) - 1 }, count: 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testFetchConfig(server.URL)
			cfg.JSONOutput, cfg.NoSortOutput, cfg.URL = tt.mode.json, tt.mode.noSort, true
			deps := newTestRootDeps()
			deps.Stdout = writerFunc(func(p []byte) (int, error) { return tt.written(p), nil })
			_, stderr, err := executeTestRootCommand(t, cfg, deps, "nicovideo.jp/user/1")
			if !errors.Is(err, io.ErrShortWrite) {
				t.Fatalf("got %v, want %v", err, io.ErrShortWrite)
			}
			want := fmt.Sprintf("summary inputs=1 valid=1 invalid=0 fetch_ok=1 fetch_err=0 output_count=%d\n", tt.count)
			if !strings.Contains(stderr.String(), want) {
				t.Fatalf("expected summary %q, got %q", want, stderr.String())
			}
		})
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
