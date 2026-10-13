package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/schollz/progressbar/v3"
)

func TestAdaptiveBufferedInputErrorCompletesCountedTarget(t *testing.T) {
	for _, mode := range []httpCommandOutputMode{
		{name: "sorted_line"},
		{name: "json", json: true},
		{name: "json_input_order", json: true, noSort: true},
	} {
		for _, file := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/file=%t", mode.name, file), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					releaseReadError := make(chan struct{})
					release := sync.OnceFunc(func() { close(releaseReadError) })
					defer release()
					wantErr := errors.New("input read failed after two targets")
					reader := io.NopCloser(io.MultiReader(
						strings.NewReader("nicovideo.jp/user/1\nnicovideo.jp/user/2\n"),
						blockingErrorReader{wait: releaseReadError, err: wantErr},
					))
					requests := make(chan string, 4)
					originalTransport := http.DefaultTransport
					http.DefaultTransport = adaptiveInputErrorTransport(func(r *http.Request) (*http.Response, error) {
						requests <- r.URL.Path
						<-r.Context().Done()
						return nil, r.Context().Err()
					})
					defer func() { http.DefaultTransport = originalTransport }()

					cfg := newTestRootConfig()
					cfg.BaseURL, cfg.Retries, cfg.HTTPMetrics = "https://fixture.invalid", 1, true
					cfg.NoProgress, cfg.ForceProgress = false, true
					cfg.JSONOutput, cfg.NoSortOutput = mode.json, mode.noSort
					cfg.ReadStdin = !file
					if file {
						cfg.InputFilePath = "synthetic-input"
					}
					var logs bytes.Buffer
					var bar *progressbar.ProgressBar
					deps := newTestRootDeps()
					deps.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
					deps.OpenInputFile = func(string) (io.ReadCloser, error) { return reader, nil }
					deps.ProgressBarNew = func(max int64, _ io.Writer, show bool) *progressbar.ProgressBar {
						bar = progressbar.NewOptions64(max, progressbar.OptionSetWriter(io.Discard), progressbar.OptionSetVisibility(show), progressbar.OptionSetSpinnerChangeInterval(0))
						return bar
					}
					command, out, errOut := newTestRootCommand(t, cfg, deps)
					command.SetContext(ctx)
					command.SetIn(reader)
					// Fixing HTTP to one also starts automatic target admission at
					// one, so the first request fills the automatic worker gate.
					command.SetArgs([]string{"--concurrency-adaptive", "--concurrency-http=1"})
					done := make(chan error, 1)
					go func() { done <- command.Execute() }()
					select {
					case path := <-requests:
						if path != "/users/1/videos" {
							t.Fatalf("first dispatched target = %q", path)
						}
					case <-ctx.Done():
						t.Fatal("first target did not dispatch")
					}
					// The input producer has sent both lines before blocking on
					// its next Read. Wait until the consumer has counted target 2
					// and is blocked behind target 1 before allowing the read error.
					synctest.Wait()
					release()
					synctest.Wait()
					select {
					case err := <-done:
						if !errors.Is(err, wantErr) {
							t.Fatalf("input error changed: got %v, want %v", err, wantErr)
						}
					default:
						t.Fatal("command did not finish after input error")
					}
					wantSummary := "summary inputs=2 valid=2 invalid=0 fetch_ok=2 fetch_err=0 output_count=0"
					if !strings.Contains(errOut.String(), wantSummary) {
						t.Errorf("counted target missing from fetch summary: %q", errOut.String())
					}
					if got := bar.State().CurrentNum; got != 2 {
						t.Errorf("progress advanced %d times, want 2 counted targets", got)
					}
					if mode.json {
						var payload jsonOutputPayload
						if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
							t.Fatalf("decode JSON: %v", err)
						}
						if payload.Inputs != (jsonInputs{Total: 2, Valid: 2}) || len(payload.Targets) != 2 {
							t.Errorf("counted target missing from JSON: %+v", payload)
						} else {
							for i, target := range payload.Targets {
								if target.Type != targetTypeUser || target.ID != fmt.Sprintf("%d", i+1) || len(target.Items) != 0 || target.Error != "" {
									t.Errorf("canceled target row = %+v", target)
								}
							}
						}
					} else if out.Len() != 0 {
						t.Errorf("canceled targets wrote line output: %q", out.String())
					}
					select {
					case path := <-requests:
						t.Errorf("canceled target dispatched HTTP: %s", path)
					default:
					}
					event := singleHTTPMetricEvent(t, logs.String())
					assertHTTPMetricCounter(t, event, "attempts", 1)
					assertAdaptiveHTTPMetric(t, event, 1, true)
					if event.HTTP.Admission.Reserved != 0 || event.HTTP.Admission.Pending != 0 {
						t.Errorf("input error leaked admission state: %+v", event.HTTP.Admission)
					}
				})
			})
		}
	}
}

type adaptiveInputErrorTransport func(*http.Request) (*http.Response, error)

func (transport adaptiveInputErrorTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return transport(r)
}
