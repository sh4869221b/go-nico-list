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

func TestRunRootCmdNoSortDoesNotRequireInputOrderWhenTargetsCompleteOutOfOrder(t *testing.T) {
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
	if got := strings.Fields(out.String()); len(got) != 2 || !strings.Contains(out.String(), "sm1\n") || !strings.Contains(out.String(), "sm2\n") {
		t.Errorf("unexpected stdout output: %q", got)
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
