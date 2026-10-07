package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestRunRootCmdReturnsOutputWriteError(t *testing.T) {
	server := newSingleVideoServer(t)
	for _, jsonOutput := range []bool{false, true} {
		t.Run(fmt.Sprintf("json=%t", jsonOutput), func(t *testing.T) {
			cfg := testFetchConfig(server.URL)
			cfg.JSONOutput = jsonOutput
			writeErr := errors.New("stdout failed")
			var errOut bytes.Buffer
			deps := newTestRootDeps()
			deps.Stdout, deps.Stderr = errorWriter{err: writeErr}, &errOut
			_, _, err := executeTestRootCommand(t, cfg, deps, "nicovideo.jp/user/1")
			if !errors.Is(err, writeErr) {
				t.Fatalf("expected stdout error, got %v", err)
			}
			want := "summary inputs=1 valid=1 invalid=0 fetch_ok=1 fetch_err=0 output_count=1"
			if got := errOut.String(); !strings.Contains(got, want) {
				t.Fatalf("expected summary %q, got %q", want, got)
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
