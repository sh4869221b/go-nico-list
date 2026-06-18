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

func TestRunRootCmdNoInputs(t *testing.T) {
	_, _, err := executeTestRootCommand(t, newTestRootConfig(), newTestRootDeps())
	if err == nil || err.Error() != "no inputs provided" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunRootCmdInputFileNoArgs(t *testing.T) {
	server := newEmptyAPIServer(t)
	cfg := testFetchConfig(server.URL)
	cfg.InputFilePath = writeInputsFile(t, "nicovideo.jp/user/1\ninvalid\n\n")

	out, _, err := executeTestRootCommand(t, cfg, newTestRootDeps())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no stdout output, got %q", out.String())
	}
}

func TestRunRootCmdStdinNoArgs(t *testing.T) {
	server := newEmptyAPIServer(t)
	cfg := testFetchConfig(server.URL)
	cfg.ReadStdin = true
	cmd, out, _ := newTestRootCommand(t, cfg, newTestRootDeps())
	cmd.SetIn(strings.NewReader("nicovideo.jp/user/1\n"))

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no stdout output, got %q", out.String())
	}
}

func TestStreamLinesFromFileReturnsCloseError(t *testing.T) {
	closeErr := errors.New("close failed")
	deps := newTestRootDeps()
	deps.OpenInputFile = func(string) (io.ReadCloser, error) {
		return closeErrorReader{Reader: strings.NewReader("nicovideo.jp/user/1\n"), err: closeErr}, nil
	}
	out := make(chan string, 1)
	count, err := streamLinesFromFile(context.Background(), "dummy", out, deps)
	if !errors.Is(err, closeErr) {
		t.Fatalf("expected close error, got %v", err)
	}
	if count != 1 {
		t.Fatalf("unexpected count: %d", count)
	}
}

func TestRunRootCmdInputFileOpenError(t *testing.T) {
	openErr := errors.New("open failed")
	cfg := newTestRootConfig()
	cfg.InputFilePath = "dummy"
	deps := newTestRootDeps()
	deps.OpenInputFile = func(string) (io.ReadCloser, error) {
		return nil, openErr
	}

	_, _, err := executeTestRootCommand(t, cfg, deps)
	if !errors.Is(err, openErr) {
		t.Fatalf("expected open error, got %v", err)
	}
}

func TestRunRootCmdInputFileCloseError(t *testing.T) {
	closeErr := errors.New("close failed")
	server := newEmptyAPIServer(t)
	cfg := testFetchConfig(server.URL)
	cfg.InputFilePath = "dummy"
	deps := newTestRootDeps()
	deps.OpenInputFile = func(string) (io.ReadCloser, error) {
		return closeErrorReader{Reader: strings.NewReader("nicovideo.jp/user/1\n"), err: closeErr}, nil
	}

	_, _, err := executeTestRootCommand(t, cfg, deps)
	if !errors.Is(err, closeErr) {
		t.Fatalf("expected close error, got %v", err)
	}
}

func TestRunRootCmdInputReadErrorCancelsFetches(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	var startedOnce sync.Once
	var canceledOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedOnce.Do(func() { close(started) })
		<-r.Context().Done()
		canceledOnce.Do(func() { close(canceled) })
	}))
	t.Cleanup(server.Close)
	cfg := testFetchConfig(server.URL)
	cfg.HTTPClientTimeout = 5 * time.Second
	cfg.ReadStdin = true
	cmd, _, _ := newTestRootCommand(t, cfg, newTestRootDeps())
	cmd.SetArgs([]string{"nicovideo.jp/user/1"})
	wait := make(chan struct{})
	cmd.SetIn(blockingErrorReader{wait: wait, err: errors.New("stdin read error")})

	errCh := make(chan error, 1)
	go func() { errCh <- cmd.Execute() }()
	waitTimeout := time.Second
	if deadline, ok := t.Deadline(); ok {
		if remaining := time.Until(deadline) / 2; remaining > 0 {
			waitTimeout = remaining
		}
	}
	select {
	case <-started:
	case <-time.After(waitTimeout):
		t.Fatal("expected request to start")
	}
	close(wait)
	select {
	case err := <-errCh:
		if err == nil || err.Error() != "stdin read error" {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(waitTimeout):
		t.Fatal("expected command to finish after input error")
	}
	select {
	case <-canceled:
	case <-time.After(waitTimeout):
		t.Fatal("expected request to be canceled")
	}
}

func TestRunRootCmdJSONStdinReadError(t *testing.T) {
	stdinErr := errors.New("stdin read error")
	cfg := newTestRootConfig()
	cfg.JSONOutput = true
	cfg.ReadStdin = true
	cmd, _, _ := newTestRootCommand(t, cfg, newTestRootDeps())
	wait := make(chan struct{})
	cmd.SetIn(blockingErrorReader{wait: wait, err: stdinErr})

	errCh := make(chan error, 1)
	go func() { errCh <- cmd.Execute() }()

	close(wait)
	select {
	case err := <-errCh:
		if !errors.Is(err, stdinErr) {
			t.Fatalf("expected stdin error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("expected command to finish after stdin error")
	}
}

func TestRunRootCmdInputFileReadErrorCancelsFetches(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	var startedOnce sync.Once
	var canceledOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedOnce.Do(func() { close(started) })
		<-r.Context().Done()
		canceledOnce.Do(func() { close(canceled) })
	}))
	t.Cleanup(server.Close)
	cfg := testFetchConfig(server.URL)
	cfg.HTTPClientTimeout = 5 * time.Second
	cfg.InputFilePath = "dummy"
	longLine := strings.Repeat("a", 1024*1024+1)
	pr, pw := io.Pipe()
	deps := newTestRootDeps()
	deps.OpenInputFile = func(string) (io.ReadCloser, error) { return pr, nil }
	cmd, _, _ := newTestRootCommand(t, cfg, deps)
	cmd.SetArgs([]string{"nicovideo.jp/user/1"})

	errCh := make(chan error, 1)
	go func() { errCh <- cmd.Execute() }()
	waitTimeout := time.Second
	if deadline, ok := t.Deadline(); ok {
		if remaining := time.Until(deadline) / 2; remaining > 0 {
			waitTimeout = remaining
		}
	}
	select {
	case <-started:
	case <-time.After(waitTimeout):
		t.Fatal("expected request to start")
	}
	go func() {
		_, _ = pw.Write([]byte(longLine + "\n"))
		_ = pw.Close()
	}()
	select {
	case err := <-errCh:
		requireTooLongInputError(t, err)
	case <-time.After(waitTimeout):
		t.Fatal("expected command to finish after input error")
	}
	select {
	case <-canceled:
	case <-time.After(waitTimeout):
		t.Fatal("expected request to be canceled")
	}
}
