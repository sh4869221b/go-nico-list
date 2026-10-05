package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdaptiveHTTPControlIsIndependentOfDiagnostics(t *testing.T) {
	for _, max := range []int{0, 4, 32} {
		t.Run(fmt.Sprintf("maximum=%d", max), func(t *testing.T) {
			cfg := newTestRootConfig()
			cfg.AdaptiveHTTPConcurrency, cfg.HTTPConcurrency = true, max
			if err := validateFlagsFor(&cfg); err != nil {
				t.Fatalf("valid adaptive config rejected: %v", err)
			}
			initial := 8
			if max > 0 && max < initial {
				initial = max
			}
			control := newCommandHTTPControl(&cfg)
			snapshot := control.Snapshot()
			if snapshot.Adaptive == nil || snapshot.Adaptive.Current != initial || snapshot.Adaptive.Max != max || snapshot.Admission.Limit != initial || snapshot.Admission.HardMax != max {
				t.Fatalf("adaptive control was not initialized: %+v", snapshot)
			}
			if snapshot.Metrics.Counters != nil || snapshot.Metrics.Durations != nil {
				t.Fatal("adaptive control enabled full diagnostics")
			}
			other := newCommandHTTPControl(&cfg)
			if other == control {
				t.Fatal("separate commands shared HTTP control state")
			}
		})
	}
	cfg := newTestRootConfig()
	cfg.HTTPConcurrency = 32
	fixed := newCommandHTTPControl(&cfg).Snapshot()
	if fixed.Adaptive != nil || fixed.Admission.Limit != 32 {
		t.Fatalf("fixed control contract changed: %+v", fixed)
	}
	cfg.HTTPConcurrency = 0
	if control := newCommandHTTPControl(&cfg); control != nil {
		t.Fatal("disabled HTTP controls no longer preserve the original request path")
	}
}

func TestAdaptiveHTTPInputFailuresPreserveErrorsAndCleanup(t *testing.T) {
	for _, mode := range httpCommandOutputModes {
		for _, max := range []int{0, 8} {
			for _, failure := range []string{"open", "read", "close"} {
				t.Run(fmt.Sprintf("%s/maximum=%d/%s", mode.name, max, failure), func(t *testing.T) {
					cfg := newTestRootConfig()
					cfg.AdaptiveHTTPConcurrency, cfg.HTTPConcurrency, cfg.HTTPMetrics = true, max, true
					cfg.JSONOutput, cfg.NoSortOutput = mode.json, mode.noSort
					cfg.InputFilePath = "synthetic-input"
					wantErr := errors.New("synthetic input " + failure + " failure")
					var logs bytes.Buffer
					deps := newTestRootDeps()
					deps.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
					deps.OpenInputFile = func(string) (io.ReadCloser, error) {
						switch failure {
						case "open":
							return nil, wantErr
						case "close":
							return closeErrorReader{Reader: strings.NewReader(""), err: wantErr}, nil
						default:
							ready := make(chan struct{})
							close(ready)
							return io.NopCloser(blockingErrorReader{wait: ready, err: wantErr}), nil
						}
					}
					_, _, err := executeTestRootCommand(t, cfg, deps)
					if !errors.Is(err, wantErr) {
						t.Fatalf("input failure changed: got %v, want %v", err, wantErr)
					}
					event := singleHTTPMetricEvent(t, logs.String())
					assertAdaptiveHTTPMetric(t, event, max)
					assertHTTPMetricCounter(t, event, "attempts", 0)
					if event.HTTP.Admission.Reserved != 0 || event.HTTP.Admission.Pending != 0 {
						t.Fatalf("input error leaked admission state: %+v", event.HTTP.Admission)
					}
				})
			}
		}
	}
}

func TestAdaptiveHTTPUnknownCountAndDuplicateTargets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "1" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		payload := httpCommandPagePayload(strings.Contains(r.URL.Path, "/mylists/"), 1, []string{"sm5", "sm1"})
		payload = strings.Replace(payload, `"totalCount":1,`, "", 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, payload)
	}))
	t.Cleanup(server.Close)
	for _, mode := range httpCommandOutputModes {
		for _, max := range []int{0, 16} {
			t.Run(fmt.Sprintf("%s/maximum=%d", mode.name, max), func(t *testing.T) {
				cfg := testFetchConfig(server.URL)
				cfg.Concurrency, cfg.PageConcurrency = 3, 8
				cfg.HTTPConcurrency, cfg.AdaptiveHTTPConcurrency, cfg.HTTPMetrics = max, true, true
				cfg.NoSortOutput, cfg.JSONOutput = mode.noSort, mode.json
				var logs bytes.Buffer
				deps := newTestRootDeps()
				deps.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
				out, _, err := executeTestRootCommand(t, cfg, deps, "nicovideo.jp/user/1", "nicovideo.jp/mylist/2", "nicovideo.jp/user/1")
				if err != nil {
					t.Fatal(err)
				}
				assertHTTPCommandIDs(t, out.String(), mode, []string{"sm5", "sm1", "sm5", "sm1", "sm5", "sm1"})
				event := singleHTTPMetricEvent(t, logs.String())
				assertAdaptiveHTTPMetric(t, event, max)
				assertHTTPMetricCounter(t, event, "attempts", 6)
				assertHTTPMetricCounter(t, event, "http_404", 3)
				assertHTTPMetricCounter(t, event, "retries", 0)
			})
		}
	}
}
