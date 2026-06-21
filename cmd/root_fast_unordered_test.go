package cmd

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type signalWriter struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	once  sync.Once
	wrote chan struct{}
}

func newSignalWriter() *signalWriter {
	return &signalWriter{wrote: make(chan struct{})}
}

func (w *signalWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.wrote) })
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *signalWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func sortedLines(output string) []string {
	lines := strings.Fields(output)
	slices.Sort(lines)
	return lines
}

func sameStringSet(got []string, want []string) bool {
	gotCopy := append([]string{}, got...)
	wantCopy := append([]string{}, want...)
	slices.Sort(gotCopy)
	slices.Sort(wantCopy)
	return slices.Equal(gotCopy, wantCopy)
}

func TestRunRootCmdNoSortStreamsReadyTargetBeforeSlowTargetCompletes(t *testing.T) {
	releaseSlowTarget := make(chan struct{})
	var releaseSlowTargetOnce sync.Once
	releaseSlow := func() { releaseSlowTargetOnce.Do(func() { close(releaseSlowTarget) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/users/1/") {
			select {
			case <-releaseSlowTarget:
			case <-r.Context().Done():
				return
			}
			if r.URL.Query().Get("page") != "1" {
				_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[]}}`)
				return
			}
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[{"essential":{"id":"sm1","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}}]}}`)
			return
		}
		if r.URL.Query().Get("page") != "1" {
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[]}}`)
			return
		}
		_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[{"essential":{"id":"sm2","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}}]}}`)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(releaseSlow)
	cfg := testFetchConfig(server.URL)
	cfg.Concurrency = 2
	cfg.NoSortOutput = true
	stdout := newSignalWriter()
	deps := newTestRootDeps()
	deps.Stdout = stdout

	errCh := make(chan error, 1)
	go func() {
		_, _, err := executeTestRootCommand(t, cfg, deps, "nicovideo.jp/user/1", "nicovideo.jp/user/2")
		errCh <- err
	}()

	select {
	case <-stdout.wrote:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected --no-sort output before the slow target completed")
	}
	releaseSlow()
	if err := <-errCh; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := stdout.String(); !strings.Contains(got, "sm2\n") {
		t.Fatalf("expected fast target output, got %q", got)
	}
}

func TestRunRootCmdNoSortDedupeOutputsEachIDOnce(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") != "1" {
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[]}}`)
			return
		}
		_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[{"essential":{"id":"sm1","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}},{"essential":{"id":"sm1","registeredAt":"2024-01-11T00:00:00Z","count":{"comment":10}}},{"essential":{"id":"sm2","registeredAt":"2024-01-12T00:00:00Z","count":{"comment":10}}}]}}`)
	}))
	t.Cleanup(server.Close)
	cfg := testFetchConfig(server.URL)
	cfg.NoSortOutput = true
	cfg.DedupeOutput = true

	out, _, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "nicovideo.jp/user/1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := sortedLines(out.String()); !sameStringSet(got, []string{"sm1", "sm2"}) {
		t.Fatalf("unexpected stdout output: %q", out.String())
	}
}
