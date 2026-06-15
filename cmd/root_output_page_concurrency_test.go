package cmd

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestRunRootCmdPageConcurrencyFetchesUserPagesBeforeSorting(t *testing.T) {
	page3Started := make(chan struct{})
	var page3StartedOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"totalCount":300,"items":[{"essential":{"id":"sm3","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}}]}}`)
		case "2":
			select {
			case <-page3Started:
				_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"totalCount":300,"items":[{"essential":{"id":"sm2","registeredAt":"2024-01-11T00:00:00Z","count":{"comment":10}}}]}}`)
			case <-r.Context().Done():
				return
			}
		case "3":
			page3StartedOnce.Do(func() { close(page3Started) })
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"totalCount":300,"items":[{"essential":{"id":"sm1","registeredAt":"2024-01-12T00:00:00Z","count":{"comment":10}}}]}}`)
		default:
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"totalCount":300,"items":[]}}`)
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { page3StartedOnce.Do(func() { close(page3Started) }) })
	cfg := testFetchConfig(server.URL)
	cfg.PageConcurrency = 2
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	cmd, out, _ := newTestRootCommand(t, cfg, newTestRootDeps())
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"nicovideo.jp/user/1"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := out.String(); got != "sm1\nsm2\nsm3\n" {
		t.Fatalf("unexpected stdout output: %q", got)
	}
}

func TestRunRootCmdPageConcurrencyFetchesMylistPages(t *testing.T) {
	page3Started := make(chan struct{})
	var page3StartedOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"mylist":{"totalCount":300,"items":[{"video":{"id":"sm3","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}}]}}}`)
		case "2":
			select {
			case <-page3Started:
				_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"mylist":{"totalCount":300,"items":[{"video":{"id":"sm2","registeredAt":"2024-01-11T00:00:00Z","count":{"comment":10}}}]}}}`)
			case <-r.Context().Done():
				return
			}
		case "3":
			page3StartedOnce.Do(func() { close(page3Started) })
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"mylist":{"totalCount":300,"items":[{"video":{"id":"sm1","registeredAt":"2024-01-12T00:00:00Z","count":{"comment":10}}}]}}}`)
		default:
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"mylist":{"totalCount":300,"items":[]}}}`)
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { page3StartedOnce.Do(func() { close(page3Started) }) })
	cfg := testFetchConfig(server.URL)
	cfg.PageConcurrency = 2
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	t.Cleanup(cancel)
	cmd, out, _ := newTestRootCommand(t, cfg, newTestRootDeps())
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"nicovideo.jp/mylist/847130"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := out.String(); got != "sm1\nsm2\nsm3\n" {
		t.Fatalf("unexpected stdout output: %q", got)
	}
}
