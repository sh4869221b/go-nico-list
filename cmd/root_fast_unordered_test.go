package cmd

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type writerFunc func([]byte) (int, error)

func (write writerFunc) Write(p []byte) (int, error) { return write(p) }

func TestWriteUnorderedOutputCountsOnlyFlushedTargets(t *testing.T) {
	writeErr := errors.New("stdout failed")
	var writes []string
	out := writerFunc(func(p []byte) (int, error) {
		writes = append(writes, string(p))
		if len(writes) == 2 {
			return 0, writeErr
		}
		return len(p), nil
	})
	output := make(chan []string, 4)
	output <- []string{"sm1", "sm1", "sm2"}
	output <- []string{"sm2"}
	output <- []string{"sm3", "sm1", "sm4"}
	output <- []string{"sm5"}
	close(output)
	canceled := false

	result := writeUnorderedOutput(out, output, &RootConfig{DedupeOutput: true}, func() { canceled = true })
	if !errors.Is(result.err, writeErr) || result.count != 2 || !canceled {
		t.Fatalf("expected two flushed IDs and cancellation on write failure, got %+v, canceled=%t", result, canceled)
	}
	if len(writes) != 2 || writes[0] != "sm1\nsm2\n" || writes[1] != "sm3\nsm4\n" {
		t.Fatalf("unexpected target writes: %q", writes)
	}
}

type signalWriter struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	once  sync.Once
	wrote chan struct{}
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
	stdout := &signalWriter{wrote: make(chan struct{})}
	deps := newTestRootDeps()
	deps.Stdout = stdout

	errCh := make(chan error, 1)
	go func() {
		_, _, err := executeTestRootCommand(t, cfg, deps, "nicovideo.jp/user/1", "nicovideo.jp/user/2")
		errCh <- err
	}()

	select {
	case <-stdout.wrote:
	case <-time.After(time.Second):
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
