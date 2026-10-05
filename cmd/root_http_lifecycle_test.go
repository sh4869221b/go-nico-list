package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/schollz/progressbar/v3"
)

func TestHTTPControlsPreserveOutputFailureAndProgress(t *testing.T) {
	server := newSingleVideoServer(t)
	for _, mode := range httpCommandAdmissionModes() {
		t.Run(mode.name, func(t *testing.T) {
			for _, hide := range []bool{false, true} {
				cfg := testFetchConfig(server.URL)
				cfg.NoSortOutput, cfg.JSONOutput = mode.noSort, mode.json
				cfg.AdaptiveHTTPConcurrency = mode.adaptive
				cfg.HTTPConcurrency, cfg.HTTPMetrics = 1, true
				cfg.ForceProgress, cfg.NoProgress = true, hide
				var logs bytes.Buffer
				deps := newTestRootDeps()
				writeErr := errors.New("stdout failed")
				deps.Stdout = errorWriter{err: writeErr}
				deps.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
				var visible bool
				deps.ProgressBarNew = func(max int64, w io.Writer, show bool) *progressbar.ProgressBar {
					visible = show
					return progressbar.NewOptions64(max, progressbar.OptionSetWriter(io.Discard), progressbar.OptionSetVisibility(show))
				}
				_, _, err := executeTestRootCommand(t, cfg, deps, "nicovideo.jp/user/1")
				if !errors.Is(err, writeErr) {
					t.Fatalf("error changed: %v", err)
				}
				if visible == hide {
					t.Fatalf("progress visibility=%t hide=%t", visible, hide)
				}
				event := singleHTTPMetricEvent(t, logs.String())
				if event.HTTP.Admission.Reserved != 0 || event.HTTP.Admission.Pending != 0 {
					t.Fatalf("leak: %+v", event.HTTP.Admission)
				}
			}
		})
	}
}

func TestHTTPControlsCancelActiveBodyAndQueuedTargets(t *testing.T) {
	for _, mode := range httpCommandAdmissionModes() {
		t.Run(mode.name, func(t *testing.T) {
			started := make(chan struct{})
			var once sync.Once
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				once.Do(func() { close(started) })
				<-r.Context().Done()
			}))
			t.Cleanup(server.Close)
			cfg := testFetchConfig(server.URL)
			cfg.NoSortOutput, cfg.JSONOutput = mode.noSort, mode.json
			cfg.AdaptiveHTTPConcurrency = mode.adaptive
			cfg.Concurrency, cfg.HTTPConcurrency, cfg.HTTPMetrics = 3, 1, true
			cfg.HTTPClientTimeout = 5 * time.Second
			var logs bytes.Buffer
			deps := newTestRootDeps()
			deps.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
			command, _, _ := newTestRootCommand(t, cfg, deps)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			command.SetContext(ctx)
			command.SetArgs([]string{"nicovideo.jp/user/1", "nicovideo.jp/user/2", "nicovideo.jp/mylist/3"})
			done := make(chan error, 1)
			go func() { done <- command.Execute() }()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("request did not start")
			}
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("cancellation behavior changed: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("command failed to cancel")
			}
			event := singleHTTPMetricEvent(t, logs.String())
			if event.HTTP.Admission.Reserved != 0 || event.HTTP.Admission.Pending != 0 {
				t.Fatalf("leak: %+v", event.HTTP.Admission)
			}
			assertHTTPMetricCounter(t, event, "attempts", 1)
		})
	}
}
