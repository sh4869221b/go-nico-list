package cmd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestRunRootCmdMaxVideosStopsSingleTargetPagination(t *testing.T) {
	var page2Requests int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") != "1" {
			atomic.AddInt64(&page2Requests, 1)
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[{"essential":{"id":"sm2","registeredAt":"2024-01-11T00:00:00Z","count":{"comment":10}}}]}}`)
			return
		}
		_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[{"essential":{"id":"sm1","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}}]}}`)
	}))
	t.Cleanup(server.Close)
	cfg := testFetchConfig(server.URL)
	cfg.MaxVideos = 1

	out, _, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "nicovideo.jp/user/1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := out.String(); got != "sm1\n" {
		t.Fatalf("unexpected stdout output: %q", got)
	}
	if got := atomic.LoadInt64(&page2Requests); got != 0 {
		t.Fatalf("expected max-videos to stop pagination before page 2, got %d page 2 requests", got)
	}
}
