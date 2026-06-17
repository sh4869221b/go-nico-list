package cmd

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

func TestRunRootCmdNoSortDedupeMaxVideosStopsAfterEnoughUniqueIDs(t *testing.T) {
	var page3Requests int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			_, _ = io.WriteString(w, userVideoPage(300, "sm1", "sm1"))
		case "2":
			_, _ = io.WriteString(w, userVideoPage(300, "sm2"))
		default:
			atomic.AddInt64(&page3Requests, 1)
			_, _ = io.WriteString(w, userVideoPage(300, "sm3"))
		}
	}))
	t.Cleanup(server.Close)

	cfg := testFetchConfig(server.URL)
	cfg.NoSortOutput = true
	cfg.DedupeOutput = true
	cfg.MaxVideos = 2
	cfg.PageConcurrency = 4

	out, errOut, err := executeTestRootCommand(t, cfg, newTestRootDeps(), "nicovideo.jp/user/1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := sortedLines(out.String()); !slices.Equal(got, []string{"sm1", "sm2"}) {
		t.Fatalf("expected two unique output IDs, got %q", out.String())
	}
	if got := atomic.LoadInt64(&page3Requests); got != 0 {
		t.Fatalf("expected unique cap to stop before page 3, got %d page 3 requests", got)
	}
	if got := errOut.String(); !strings.Contains(got, "output_count=2") {
		t.Fatalf("expected output_count=2 summary, got %q", got)
	}
}

func userVideoPage(totalCount int, ids ...string) string {
	items := make([]string, 0, len(ids))
	for i, id := range ids {
		items = append(items, fmt.Sprintf(`{"essential":{"id":%q,"registeredAt":"2024-01-%02dT00:00:00Z","count":{"comment":10}}}`, id, i+1))
	}
	return fmt.Sprintf(`{"meta":{"status":200},"data":{"totalCount":%d,"items":[%s]}}`, totalCount, strings.Join(items, ","))
}
