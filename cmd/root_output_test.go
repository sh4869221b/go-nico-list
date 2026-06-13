package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestWriteLineOutputMatchesExistingFormatting(t *testing.T) {
	tests := []struct {
		name    string
		tab     bool
		url     bool
		want    string
		wantNil bool
	}{
		{name: "raw", want: "sm1\nsm2\n"},
		{name: "tab", tab: true, want: tabOutputPrefix + "sm1\n" + tabOutputPrefix + "sm2\n"},
		{name: "url", url: true, want: nicoWatchURLPrefix + "sm1\n" + nicoWatchURLPrefix + "sm2\n"},
		{name: "tab and url", tab: true, url: true, want: tabOutputPrefix + nicoWatchURLPrefix + "sm1\n" + tabOutputPrefix + nicoWatchURLPrefix + "sm2\n"},
		{name: "empty", wantNil: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			items := []string{"sm1", "sm2"}
			if tt.wantNil {
				items = nil
			}

			if err := writeLineOutput(&out, items, tt.tab, tt.url); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := out.String(); got != tt.want {
				t.Fatalf("unexpected output: got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWriteLineOutputReturnsWriterError(t *testing.T) {
	writeErr := errors.New("stdout failed")

	err := writeLineOutput(errorWriter{err: writeErr}, []string{"sm1"}, false, false)

	if !errors.Is(err, writeErr) {
		t.Fatalf("expected stdout error, got %v", err)
	}
}

func TestWriteLineOutputBatchesWrites(t *testing.T) {
	var out countingWriter
	items := make([]string, 100)
	for i := range items {
		items[i] = fmt.Sprintf("sm%d", i)
	}

	if err := writeLineOutput(&out, items, true, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.writes > 2 {
		t.Fatalf("expected batched writes, got %d writes", out.writes)
	}
}

type countingWriter struct {
	buf    bytes.Buffer
	writes int
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.writes++
	return w.buf.Write(p)
}

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

func TestRunRootCmdDedupeRemovesDuplicates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "1" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":{"items":[]}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"items":[{"essential":{"id":"sm1","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}},{"essential":{"id":"sm1","registeredAt":"2024-01-11T00:00:00Z","count":{"comment":10}}},{"essential":{"id":"sm2","registeredAt":"2024-01-12T00:00:00Z","count":{"comment":10}}}]}}`)
	}))
	t.Cleanup(server.Close)
	cfg := testFetchConfig(server.URL)
	cfg.DedupeOutput = true

	out, _, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "nicovideo.jp/user/1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := out.String(); got != "sm1\nsm2\n" {
		t.Errorf("unexpected stdout output: %q", got)
	}
}

func TestRunRootCmdDefaultSortsFetchedIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "1" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":{"items":[]}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"items":[{"essential":{"id":"sm2","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}},{"essential":{"id":"sm1","registeredAt":"2024-01-11T00:00:00Z","count":{"comment":10}}}]}}`)
	}))
	t.Cleanup(server.Close)
	cfg := testFetchConfig(server.URL)

	out, _, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "nicovideo.jp/user/1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := out.String(); got != "sm1\nsm2\n" {
		t.Errorf("unexpected stdout output: %q", got)
	}
}

func TestRunRootCmdNoSortPreservesFetchOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "1" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":{"items":[]}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"items":[{"essential":{"id":"sm2","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}},{"essential":{"id":"sm1","registeredAt":"2024-01-11T00:00:00Z","count":{"comment":10}}}]}}`)
	}))
	t.Cleanup(server.Close)
	cfg := testFetchConfig(server.URL)

	out, _, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "--no-sort", "nicovideo.jp/user/1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := out.String(); got != "sm2\nsm1\n" {
		t.Errorf("unexpected stdout output: %q", got)
	}
}

func TestRunRootCmdNoSortPreservesInputOrderWhenTargetsCompleteOutOfOrder(t *testing.T) {
	user2Completed := make(chan struct{})
	var user2CompletedOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		isUser2 := strings.Contains(r.URL.Path, "/users/2/")
		if r.URL.Query().Get("page") != "1" {
			if isUser2 {
				user2CompletedOnce.Do(func() { close(user2Completed) })
			}
			_, _ = io.WriteString(w, `{"data":{"items":[]}}`)
			return
		}
		if strings.Contains(r.URL.Path, "/users/1/") {
			select {
			case <-user2Completed:
			case <-r.Context().Done():
				http.Error(w, "request canceled before user 2 completed", http.StatusGatewayTimeout)
				return
			}
			_, _ = io.WriteString(w, `{"data":{"items":[{"essential":{"id":"sm1","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}}]}}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":{"items":[{"essential":{"id":"sm2","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}}]}}`)
	}))
	t.Cleanup(server.Close)
	cfg := testFetchConfig(server.URL)
	cfg.Concurrency = 2

	out, _, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "--no-sort", "nicovideo.jp/user/1", "nicovideo.jp/user/2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := out.String(); got != "sm1\nsm2\n" {
		t.Errorf("unexpected stdout output: %q", got)
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

func TestRunRootCmdPartialFailureOutputsResults(t *testing.T) {
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
	cfg := testFetchConfig(server.URL)

	out, _, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "nicovideo.jp/user/1", "nicovideo.jp/user/2")
	if err == nil {
		t.Fatalf("expected error")
	}
	if got := out.String(); got != "sm1\n" {
		t.Errorf("unexpected stdout output: %q", got)
	}
}

func TestRunRootCmdMylistInput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/mylists/847130") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("page") != "1" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"mylist":{"items":[]}}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"mylist":{"items":[{"video":{"id":"sm42","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}}]}}}`)
	}))
	t.Cleanup(server.Close)
	cfg := testFetchConfig(server.URL)

	out, _, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "nicovideo.jp/mylist/847130")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := out.String(); got != "sm42\n" {
		t.Errorf("unexpected stdout output: %q", got)
	}
}
