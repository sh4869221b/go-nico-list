package cmd

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestResponseBodyLimitPreservesPartialResults(t *testing.T) {
	for _, mylist := range []bool{false, true} {
		for _, mode := range httpCommandOutputModes {
			for _, bestEffort := range []bool{false, true} {
				t.Run(fmt.Sprintf("mylist=%t/%s/bestEffort=%t", mylist, mode.name, bestEffort), func(t *testing.T) {
					var requests atomic.Int32
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						requests.Add(1)
						if r.URL.Query().Get("page") == "1" {
							_, _ = io.WriteString(w, httpCommandPagePayload(mylist, 101, []string{"sm7"}))
							return
						}
						// A valid JSON prefix with oversized trailing whitespace must still fail.
						_, _ = io.WriteString(w, `{"meta":{"status":200}}`+strings.Repeat(" ", 8<<20))
					}))
					defer server.Close()
					cfg := testFetchConfig(server.URL)
					cfg.Retries = 3
					cfg.HTTPConcurrency, cfg.HTTPMetrics = 1, true
					cfg.JSONOutput, cfg.NoSortOutput = mode.json, mode.noSort
					cfg.BestEffort = bestEffort
					var logs bytes.Buffer
					deps := newTestRootDeps()
					deps.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
					target := "nicovideo.jp/user/1"
					if mylist {
						target = "nicovideo.jp/mylist/2"
					}
					out, errOut, err := executeTestRootCommand(t, cfg, deps, target)
					if (err == nil) != bestEffort {
						t.Fatalf("error=%v bestEffort=%t", err, bestEffort)
					}
					const message = "response body exceeds 8 MiB limit"
					if !bestEffort && !strings.Contains(err.Error(), message) {
						t.Fatalf("unexpected error: %v", err)
					}
					if !strings.Contains(logs.String(), message) || !strings.Contains(errOut.String(), "fetch_ok=0 fetch_err=1 output_count=1") {
						t.Fatalf("stderr=%q logs=%q", errOut.String(), logs.String())
					}
					assertHTTPCommandIDs(t, out.String(), mode, []string{"sm7"})
					if requests.Load() != 2 {
						t.Fatalf("oversized page retried: requests=%d", requests.Load())
					}
					event := singleHTTPMetricEvent(t, logs.String())
					assertHTTPMetricCounter(t, event, "body_errors", 1)
					assertHTTPMetricCounter(t, event, "page_errors", 1)
				})
			}
		}
	}
}
