package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type blockingAfterLineReader struct {
	line string
	wait <-chan struct{}
	mu   sync.Mutex
	sent bool
}

func (r *blockingAfterLineReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	if !r.sent {
		r.sent = true
		line := r.line
		r.mu.Unlock()
		return copy(p, line), nil
	}
	r.mu.Unlock()
	<-r.wait
	return 0, io.EOF
}

func TestRunRootCmdNoSortMaxVideosStopsSingleTargetPagination(t *testing.T) {
	var pageTwoRequests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"totalCount":300,"items":[{"essential":{"id":"sm1","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}}]}}`)
		case "2":
			pageTwoRequests.Add(1)
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[]}}`)
		default:
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[]}}`)
		}
	}))
	t.Cleanup(server.Close)
	cfg := testFetchConfig(server.URL)
	cfg.NoSortOutput = true
	cfg.MaxVideos = 1

	out, errOut, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "nicovideo.jp/user/1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := strings.Fields(out.String()); len(got) != 1 || got[0] != "sm1" {
		t.Fatalf("unexpected stdout output: %q", out.String())
	}
	if got := pageTwoRequests.Load(); got != 0 {
		t.Fatalf("expected max-videos to stop pagination before page 2, got %d page 2 requests", got)
	}
	if got := errOut.String(); !strings.Contains(got, "output_count=1") {
		t.Fatalf("expected output_count=1 summary, got %q", got)
	}
}

func TestRunRootCmdNoSortMaxVideosReturnsBeforeOpenStdinEOF(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") != "1" {
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[]}}`)
			return
		}
		_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[{"essential":{"id":"sm1","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}}]}}`)
	}))
	t.Cleanup(server.Close)
	unblock := make(chan struct{})
	t.Cleanup(func() { close(unblock) })
	cfg := testFetchConfig(server.URL)
	cfg.NoSortOutput = true
	cfg.MaxVideos = 1
	cfg.ReadStdin = true
	cmd, out, errOut := newTestRootCommand(t, cfg, newTestRootDeps())
	cmd.SetIn(&blockingAfterLineReader{
		line: "nicovideo.jp/user/1\n",
		wait: unblock,
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Execute()
	}()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected --no-sort --max-videos to return before stdin EOF")
	}
	if got := strings.Fields(out.String()); len(got) != 1 || got[0] != "sm1" {
		t.Fatalf("unexpected stdout output: %q", out.String())
	}
	if got := errOut.String(); !strings.Contains(got, "output_count=1") {
		t.Fatalf("expected output_count=1 summary, got %q", got)
	}
}
