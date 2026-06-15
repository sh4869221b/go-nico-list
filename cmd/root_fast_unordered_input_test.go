package cmd

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRunRootCmdNoSortMaxVideosReturnsInputFileReadErrorAfterCap(t *testing.T) {
	requestStarted := make(chan struct{})
	writerDone := make(chan struct{})
	var requestStartedOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestStartedOnce.Do(func() { close(requestStarted) })
		select {
		case <-writerDone:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[{"essential":{"id":"sm1","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}}]}}`)
	}))
	t.Cleanup(server.Close)
	cfg := testFetchConfig(server.URL)
	cfg.NoSortOutput = true
	cfg.MaxVideos = 1
	cfg.InputFilePath = "dummy"
	pr, pw := io.Pipe()
	deps := newTestRootDeps()
	deps.OpenInputFile = func(string) (io.ReadCloser, error) { return pr, nil }

	errCh := make(chan error, 1)
	go func() {
		_, _, err := executeTestRootCommand(t, cfg, deps)
		errCh <- err
	}()
	go func() {
		defer close(writerDone)
		_, _ = io.WriteString(pw, "nicovideo.jp/user/1\n")
		select {
		case <-requestStarted:
		case <-time.After(time.Second):
			_ = pw.CloseWithError(errors.New("request did not start"))
			return
		}
		_, _ = io.WriteString(pw, strings.Repeat("a", 1024*1024+1)+"\n")
		_ = pw.Close()
	}()

	select {
	case err := <-errCh:
		requireTooLongInputError(t, err)
	case <-time.After(time.Second):
		t.Fatal("expected command to finish with input file read error")
	}
}

func TestRunRootCmdNoSortContextCancelDoesNotWaitForBlockedInputErrors(t *testing.T) {
	requestStarted := make(chan struct{})
	var requestStartedOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestStartedOnce.Do(func() { close(requestStarted) })
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	cfg := testFetchConfig(server.URL)
	cfg.NoSortOutput = true
	cfg.HTTPClientTimeout = 5 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	cmd, _, _ := newTestRootCommand(t, cfg, newTestRootDeps())
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"nicovideo.jp/user/1", "nicovideo.jp/user/2", "nicovideo.jp/user/3"})

	errCh := make(chan error, 1)
	go func() { errCh <- cmd.Execute() }()
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("expected first request to start")
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("unexpected error after context cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("expected command to finish after context cancellation")
	}
}
