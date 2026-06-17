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

type closeUnblocksReader struct {
	line string

	closeOnce sync.Once
	readOnce  sync.Once
	closed    chan struct{}
	closeDone chan struct{}
}

func newCloseUnblocksReader(line string) *closeUnblocksReader {
	return &closeUnblocksReader{
		line:      line,
		closed:    make(chan struct{}),
		closeDone: make(chan struct{}),
	}
}

func (r *closeUnblocksReader) Read(p []byte) (int, error) {
	sent := false
	r.readOnce.Do(func() {
		sent = true
	})
	if sent {
		return copy(p, r.line), nil
	}
	<-r.closed
	return 0, io.EOF
}

func (r *closeUnblocksReader) Close() error {
	r.closeOnce.Do(func() {
		close(r.closed)
		close(r.closeDone)
	})
	return nil
}

func waitForReaderClose(t *testing.T, reader *closeUnblocksReader) {
	t.Helper()
	select {
	case <-reader.closeDone:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected canceled input producer to close blocked reader")
	}
}

func newSingleVideoServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") != "1" {
			_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[]}}`)
			return
		}
		_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[{"essential":{"id":"sm1","registeredAt":"2024-01-10T00:00:00Z","count":{"comment":10}}}]}}`)
	}))
	t.Cleanup(server.Close)
	return server
}

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

func TestRunRootCmdNoSortMaxVideosClosesBlockedStdinInputStream(t *testing.T) {
	server := newSingleVideoServer(t)
	reader := newCloseUnblocksReader("nicovideo.jp/user/1\n")
	t.Cleanup(func() { _ = reader.Close() })

	cfg := testFetchConfig(server.URL)
	cfg.NoSortOutput = true
	cfg.MaxVideos = 1
	cfg.ReadStdin = true
	cmd, out, errOut := newTestRootCommand(t, cfg, newTestRootDeps())
	cmd.SetIn(reader)

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Execute()
	}()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("expected command to finish after max-videos cancellation")
	}
	if got := strings.Fields(out.String()); len(got) != 1 || got[0] != "sm1" {
		t.Fatalf("unexpected stdout output: %q", out.String())
	}
	if got := errOut.String(); !strings.Contains(got, "output_count=1") {
		t.Fatalf("expected output_count=1 summary, got %q", got)
	}
	waitForReaderClose(t, reader)
}

func TestRunRootCmdNoSortMaxVideosClosesBlockedInputFileInputStream(t *testing.T) {
	server := newSingleVideoServer(t)
	reader := newCloseUnblocksReader("nicovideo.jp/user/1\n")
	t.Cleanup(func() { _ = reader.Close() })

	cfg := testFetchConfig(server.URL)
	cfg.NoSortOutput = true
	cfg.MaxVideos = 1
	cfg.InputFilePath = "dummy"
	deps := newTestRootDeps()
	deps.OpenInputFile = func(string) (io.ReadCloser, error) { return reader, nil }

	out, errOut, err := executeTestRootCommand(t, cfg, deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := strings.Fields(out.String()); len(got) != 1 || got[0] != "sm1" {
		t.Fatalf("unexpected stdout output: %q", out.String())
	}
	if got := errOut.String(); !strings.Contains(got, "output_count=1") {
		t.Fatalf("expected output_count=1 summary, got %q", got)
	}
	waitForReaderClose(t, reader)
}

func TestRunRootCmdNoSortWriteErrorDoesNotWaitForBlockedInput(t *testing.T) {
	server := newSingleVideoServer(t)
	reader := newCloseUnblocksReader("nicovideo.jp/user/1\n")
	t.Cleanup(func() { _ = reader.Close() })

	cfg := testFetchConfig(server.URL)
	cfg.NoSortOutput = true
	cfg.InputFilePath = "dummy"
	writeErr := errors.New("stdout failed")
	deps := newTestRootDeps()
	deps.OpenInputFile = func(string) (io.ReadCloser, error) { return reader, nil }
	deps.Stdout = errorWriter{err: writeErr}

	_, _, err := executeTestRootCommand(t, cfg, deps)
	if !errors.Is(err, writeErr) {
		t.Fatalf("expected stdout error, got %v", err)
	}
	waitForReaderClose(t, reader)
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
