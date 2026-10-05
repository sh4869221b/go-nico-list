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
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type httpCommandOutputMode struct {
	name   string
	json   bool
	noSort bool
	url    bool
}

var httpCommandOutputModes = []httpCommandOutputMode{
	{name: "line"},
	{name: "unordered", noSort: true},
	{name: "json", json: true},
	{name: "json_input_order", json: true, noSort: true},
}

func TestHTTPControlFlagDefaultsAndValidation(t *testing.T) {
	cfg := newTestRootConfig()
	if cfg.HTTPConcurrency != 0 || cfg.HTTPMetrics {
		t.Fatalf("HTTP controls should be opt-in, got concurrency=%d metrics=%t", cfg.HTTPConcurrency, cfg.HTTPMetrics)
	}
	cmd, _, _ := newTestRootCommand(t, cfg, newTestRootDeps())
	for name, want := range map[string]string{"http-concurrency": "0", "http-metrics": "false"} {
		flag := cmd.Flags().Lookup(name)
		if flag == nil || flag.DefValue != want {
			t.Fatalf("flag %s: got %v, want default %q", name, flag, want)
		}
	}
	for _, unordered := range []bool{false, true} {
		t.Run(fmt.Sprintf("unordered=%t", unordered), func(t *testing.T) {
			for _, test := range []struct {
				name string
				args []string
				edit func(*RootConfig, *RootDeps)
			}{
				{name: "negative_flag", args: []string{"--http-concurrency=-1"}},
				{name: "negative_config", edit: func(c *RootConfig, _ *RootDeps) { c.HTTPConcurrency = -1 }},
				{name: "invalid_date", edit: func(c *RootConfig, _ *RootDeps) { c.DateAfter = "invalid" }},
				{name: "logger_failure", edit: func(c *RootConfig, d *RootDeps) {
					c.LogFilePath = "unopenable.log"
					d.OpenLogFile = func(string) (io.WriteCloser, error) { return nil, errors.New("log open failed") }
				}},
			} {
				t.Run(test.name, func(t *testing.T) {
					cfg := newTestRootConfig()
					cfg.NoSortOutput = unordered
					cfg.HTTPMetrics = true
					var logs bytes.Buffer
					deps := newTestRootDeps()
					deps.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
					if test.edit != nil {
						test.edit(&cfg, &deps)
					}
					out, errOut, err := executeTestRootCommand(t, cfg, deps, test.args...)
					if err == nil {
						t.Fatal("expected initialization error")
					}
					if strings.HasPrefix(test.name, "negative") && !strings.Contains(err.Error(), "http-concurrency") {
						t.Fatalf("expected HTTP concurrency validation error, got %v", err)
					}
					if out.Len() != 0 || strings.Contains(errOut.String(), "Usage:") || logs.Len() != 0 {
						t.Fatalf("initialization failure wrote data, usage, or diagnostics: stdout=%q stderr=%q logs=%q", out.String(), errOut.String(), logs.String())
					}
				})
			}
		})
	}
}

func TestHTTPControlsPreserveOutputContracts(t *testing.T) {
	server, args, want := newHTTPCommandFixture(t, 3, 2)
	modes := append(slices.Clone(httpCommandOutputModes),
		httpCommandOutputMode{name: "url", url: true},
		httpCommandOutputMode{name: "unordered_url", noSort: true, url: true},
		httpCommandOutputMode{name: "json_url", json: true, url: true},
	)
	for _, mode := range modes {
		for _, dedupe := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dedupe=%t", mode.name, dedupe), func(t *testing.T) {
				var baselineOut, baselineSummary string
				for _, flags := range []struct {
					name    string
					args    []string
					metrics bool
				}{
					{name: "default"},
					{name: "explicit_off", args: []string{"--http-concurrency=0", "--http-metrics=false"}},
					{name: "cap", args: []string{"--http-concurrency=2"}},
					{name: "metrics", args: []string{"--http-metrics"}, metrics: true},
					{name: "cap_and_metrics", args: []string{"--http-concurrency=2", "--http-metrics"}, metrics: true},
				} {
					t.Run(flags.name, func(t *testing.T) {
						cfg := testFetchConfig(server.URL)
						cfg.Concurrency, cfg.PageConcurrency = 3, 3
						cfg.JSONOutput, cfg.NoSortOutput, cfg.URL = mode.json, mode.noSort, mode.url
						cfg.DedupeOutput = dedupe
						var logs bytes.Buffer
						deps := newTestRootDeps()
						deps.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
						out, errOut, err := executeTestRootCommand(t, cfg, deps, append(slices.Clone(flags.args), args...)...)
						if err != nil {
							t.Fatalf("command: %v", err)
						}
						expected := slices.Clone(want)
						if dedupe {
							slices.Sort(expected)
							expected = slices.Compact(expected)
						}
						assertHTTPCommandIDs(t, out.String(), mode, expected)
						if flags.name == "default" {
							baselineOut, baselineSummary = out.String(), errOut.String()
						} else {
							if mode.json || !mode.noSort {
								if out.String() != baselineOut {
									t.Fatalf("HTTP controls changed deterministic output:\n got %s\nwant %s", out.String(), baselineOut)
								}
							}
							if errOut.String() != baselineSummary {
								t.Fatalf("HTTP controls changed summary: got %q, want %q", errOut.String(), baselineSummary)
							}
						}
						events := httpMetricEvents(t, logs.String())
						if flags.metrics {
							if len(events) != 1 {
								t.Fatalf("expected one metrics event, got %d", len(events))
							}
							assertHTTPMetricCounter(t, events[0], "attempts", 9)
							assertHTTPMetricCounter(t, events[0], "pages", 9)
						} else if len(events) != 0 {
							t.Fatalf("metrics disabled but got %d events", len(events))
						}
					})
				}
			})
		}
	}
}

func TestHTTPConcurrencySharedAcrossMixedTargetsAndKnownPages(t *testing.T) {
	for _, mode := range httpCommandOutputModes {
		t.Run(mode.name, func(t *testing.T) {
			const cap = 2
			started := make(chan struct{}, 9)
			release := make(chan struct{})
			var releaseOnce sync.Once
			var active, peak atomic.Int64
			var mu sync.Mutex
			seen := make(map[string]int)
			fixture, args, want := httpCommandFixture(3, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				key := r.URL.Path + "?page=" + r.URL.Query().Get("page")
				mu.Lock()
				seen[key]++
				mu.Unlock()
				if r.URL.Query().Get("page") != "1" {
					current := active.Add(1)
					for old := peak.Load(); current > old && !peak.CompareAndSwap(old, current); old = peak.Load() {
					}
					started <- struct{}{}
					select {
					case <-release:
					case <-r.Context().Done():
					}
					active.Add(-1)
				}
				w.Header().Set("Content-Type", "application/json")
				payload, ok := fixture[key]
				if !ok {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				_, _ = io.WriteString(w, payload)
			}))
			t.Cleanup(server.Close)
			t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
			cfg := testFetchConfig(server.URL)
			cfg.Concurrency, cfg.PageConcurrency = 3, 3
			cfg.HTTPConcurrency, cfg.HTTPMetrics = cap, true
			cfg.JSONOutput, cfg.NoSortOutput = mode.json, mode.noSort
			cfg.HTTPClientTimeout = 10 * time.Second
			var logs bytes.Buffer
			deps := newTestRootDeps()
			deps.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
			cmd, out, _ := newTestRootCommand(t, cfg, deps)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			t.Cleanup(cancel)
			cmd.SetContext(ctx)
			cmd.SetArgs(args)
			done := make(chan error, 1)
			go func() { done <- cmd.Execute() }()
			for range cap {
				select {
				case <-started:
				case err := <-done:
					t.Fatalf("command ended before filling the cap: %v", err)
				case <-ctx.Done():
					t.Fatal("command failed to fill the cap")
				}
			}
			releaseOnce.Do(func() { close(release) })
			if err := <-done; err != nil {
				t.Fatalf("command: %v", err)
			}
			assertHTTPCommandIDs(t, out.String(), mode, want)
			if peak.Load() != cap {
				t.Fatalf("server observed peak blocked HTTP work %d, want %d", peak.Load(), cap)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(seen) != len(fixture) {
				t.Fatalf("fetched %d distinct pages, want %d: %v", len(seen), len(fixture), seen)
			}
			for key := range fixture {
				if seen[key] != 1 {
					t.Errorf("page %s fetched %d times, want once", key, seen[key])
				}
			}
			event := singleHTTPMetricEvent(t, logs.String())
			admission := event.HTTP.Admission
			if admission.HardMax != cap || admission.Limit != cap || admission.PeakReserved != cap || admission.Reserved != 0 || admission.Pending != 0 {
				t.Fatalf("unexpected final admission snapshot: %+v", admission)
			}
			assertHTTPMetricCounter(t, event, "attempts", 9)
			assertHTTPMetricCounter(t, event, "page_success", 9)
		})
	}
}

func TestHTTPControlsPreserveStrictBestEffortAndPartialResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/mylists/") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, httpCommandPagePayload(false, 1, []string{"sm7"}))
	}))
	t.Cleanup(server.Close)
	for _, mode := range httpCommandOutputModes {
		for _, behavior := range []struct {
			name       string
			strict     bool
			bestEffort bool
			wantError  bool
		}{
			{name: "default", wantError: true},
			{name: "best_effort", bestEffort: true},
			{name: "strict", strict: true, wantError: true},
			{name: "strict_overrides_best_effort", strict: true, bestEffort: true, wantError: true},
		} {
			t.Run(mode.name+"/"+behavior.name, func(t *testing.T) {
				var baseline, summary, errorText string
				for _, enabled := range []bool{false, true} {
					cfg := testFetchConfig(server.URL)
					cfg.Concurrency, cfg.PageConcurrency = 3, 3
					cfg.JSONOutput, cfg.NoSortOutput = mode.json, mode.noSort
					cfg.StrictInput, cfg.BestEffort = behavior.strict, behavior.bestEffort
					if enabled {
						cfg.HTTPConcurrency, cfg.HTTPMetrics = 2, true
					}
					var logs bytes.Buffer
					deps := newTestRootDeps()
					deps.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
					out, errOut, err := executeTestRootCommand(t, cfg, deps, "nicovideo.jp/user/1", "nicovideo.jp/mylist/2", "invalid")
					if (err != nil) != behavior.wantError {
						t.Fatalf("enabled=%t: error=%v, wantError=%t", enabled, err, behavior.wantError)
					}
					assertHTTPCommandIDs(t, out.String(), mode, []string{"sm7"})
					gotError := fmt.Sprint(err)
					if !enabled {
						baseline, summary, errorText = out.String(), errOut.String(), gotError
					} else {
						if out.String() != baseline || errOut.String() != summary || gotError != errorText {
							t.Fatalf("controls changed partial output/summary/error: stdout=%q stderr=%q error=%q", out.String(), errOut.String(), gotError)
						}
						event := singleHTTPMetricEvent(t, logs.String())
						assertHTTPMetricCounter(t, event, "attempts", 2)
					}
				}
			})
		}
	}
}

func TestHTTPMetricsRetryAndNaturalTermination(t *testing.T) {
	var firstPageAttempts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if firstPageAttempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[{"essential":{"id":"sm8","registeredAt":"2024-01-02T03:04:05Z","count":{"comment":12}}}]}}`)
	}))
	t.Cleanup(server.Close)
	cfg := testFetchConfig(server.URL)
	cfg.Retries, cfg.HTTPConcurrency, cfg.HTTPMetrics = 2, 1, true
	var logs bytes.Buffer
	deps := newTestRootDeps()
	deps.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	out, _, err := executeTestRootCommand(t, cfg, deps, "nicovideo.jp/user/1")
	if err != nil || out.String() != "sm8\n" {
		t.Fatalf("stdout=%q error=%v", out.String(), err)
	}
	event := singleHTTPMetricEvent(t, logs.String())
	for name, want := range map[string]int64{"attempts": 3, "retries": 1, "http_200": 1, "http_404": 1, "http_429": 1, "pages": 2, "page_success": 2} {
		assertHTTPMetricCounter(t, event, name, want)
	}
	if event.HTTP.Admission.Reserved != 0 || event.HTTP.Admission.PeakReserved != 1 {
		t.Fatalf("retry leaked/exceeded reservations: %+v", event.HTTP.Admission)
	}
}

func TestHTTPMetricsRoutingAndContentIsolation(t *testing.T) {
	const secretHeader = "private-header-marker"
	const secretBody = "private-title-marker"
	const privateID = "sm918273645"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Private-Test", secretHeader)
		if r.URL.Query().Get("page") != "1" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		payload := httpCommandPagePayload(false, 1, []string{privateID})
		_, _ = io.WriteString(w, strings.Replace(payload, `"count":`, `"title":"`+secretBody+`","count":`, 1))
	}))
	t.Cleanup(server.Close)
	for _, destination := range []string{"stderr", "injected_logger", "logfile"} {
		t.Run(destination, func(t *testing.T) {
			cfg := testFetchConfig(server.URL)
			cfg.HTTPConcurrency, cfg.HTTPMetrics = 2, true
			var stderr, injected bytes.Buffer
			deps := newTestRootDeps()
			deps.Stderr = &stderr
			loggerOutput := &injected
			if destination == "stderr" {
				loggerOutput = &stderr
			}
			deps.Logger = slog.New(slog.NewJSONHandler(loggerOutput, nil))
			if destination == "logfile" {
				cfg.LogFilePath = filepath.Join(t.TempDir(), "metrics.log")
			}
			out, _, err := executeTestRootCommand(t, cfg, deps, "nicovideo.jp/user/918273645")
			if err != nil || out.String() != privateID+"\n" {
				t.Fatalf("stdout=%q error=%v", out.String(), err)
			}
			logs := loggerOutput.String()
			if destination == "logfile" {
				data, err := os.ReadFile(cfg.LogFilePath)
				if err != nil {
					t.Fatal(err)
				}
				logs = string(data)
				if injected.Len() != 0 {
					t.Fatalf("logfile should replace injected logger: %q", injected.String())
				}
			}
			event := singleHTTPMetricEvent(t, logs)
			for _, forbidden := range []string{server.URL, "/users/", "918273645", secretHeader, secretBody, "registeredAt"} {
				if strings.Contains(string(event.RawHTTP), forbidden) {
					t.Errorf("metrics retained request/response content %q: %s", forbidden, event.RawHTTP)
				}
			}
			if strings.Contains(out.String(), "http_metrics") || strings.Contains(out.String(), "summary") {
				t.Fatalf("operational output leaked to stdout: %q", out.String())
			}
			if destination != "stderr" && strings.Contains(stderr.String(), "http_metrics") {
				t.Fatalf("metrics leaked to stderr: %q", stderr.String())
			}
			if !strings.Contains(stderr.String(), "summary inputs=1 valid=1 invalid=0 fetch_ok=1 fetch_err=0 output_count=1") {
				t.Fatalf("summary missing from stderr: %q", stderr.String())
			}
		})
	}
}

func TestHTTPMetricsCommandIsolation(t *testing.T) {
	server, args, _ := newHTTPCommandFixture(t, 1, 1)
	cfg := testFetchConfig(server.URL)
	cfg.PageConcurrency = 2
	for _, test := range []struct {
		name    string
		flags   []string
		targets []string
		want    int64
		cap     int
	}{
		{name: "first", flags: []string{"--http-metrics", "--http-concurrency=1"}, targets: args[:1], want: 1, cap: 1},
		{name: "second", flags: []string{"--http-metrics", "--http-concurrency=2"}, targets: args, want: 3, cap: 2},
		{name: "disabled", targets: args[:1]},
		{name: "empty", flags: []string{"--http-metrics"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var logs bytes.Buffer
			deps := newTestRootDeps()
			deps.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
			_, _, err := executeTestRootCommand(t, cfg, deps, append(slices.Clone(test.flags), test.targets...)...)
			if test.name == "empty" {
				if err == nil || err.Error() != "no inputs provided" {
					t.Fatalf("expected empty-input error, got %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if test.name == "disabled" {
				if events := httpMetricEvents(t, logs.String()); len(events) != 0 {
					t.Fatalf("previous command enabled metrics on a fresh command: %+v", events)
				}
				return
			}
			event := singleHTTPMetricEvent(t, logs.String())
			assertHTTPMetricCounter(t, event, "attempts", test.want)
			if event.HTTP.Admission.HardMax != test.cap || event.HTTP.Admission.Reserved != 0 || event.HTTP.Admission.Pending != 0 {
				t.Fatalf("command inherited admission state: %+v", event.HTTP.Admission)
			}
		})
	}
	if cfg.HTTPMetrics || cfg.HTTPConcurrency != 0 {
		t.Fatalf("command flags mutated caller config: %+v", cfg)
	}
}

// TestHTTPMetricsLocalReport is also an opt-in verbose measurement report:
//
//	go test ./cmd -run '^TestHTTPMetricsLocalReport$' -count=1 -v
//
// The synthetic run has 120 successful pages, enough for service p95/p99.
// Normal test output stays quiet; verbose output contains only the aggregate
// diagnostic, with no fixture IDs, request URLs, headers, or response content.
func TestHTTPMetricsLocalReport(t *testing.T) {
	server, args, want := newHTTPCommandFixture(t, 40, 100)
	cfg := testFetchConfig(server.URL)
	cfg.Concurrency, cfg.PageConcurrency = 3, 4
	cfg.HTTPConcurrency, cfg.HTTPMetrics = 8, true
	cfg.HTTPClientTimeout = 10 * time.Second
	var logs bytes.Buffer
	deps := newTestRootDeps()
	deps.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	out, _, err := executeTestRootCommand(t, cfg, deps, args...)
	if err != nil {
		t.Fatalf("local measurement command: %v", err)
	}
	assertHTTPCommandIDs(t, out.String(), httpCommandOutputMode{name: "line"}, want)
	event := singleHTTPMetricEvent(t, logs.String())
	for _, name := range []string{"attempts", "pages", "page_success"} {
		assertHTTPMetricCounter(t, event, name, 120)
	}
	if event.HTTP.Admission.HardMax != 8 || event.HTTP.Admission.Reserved != 0 || event.HTTP.Admission.Pending != 0 {
		t.Fatalf("unexpected final admission state: %+v", event.HTTP.Admission)
	}
	for _, line := range strings.Split(logs.String(), "\n") {
		if strings.Contains(line, `"msg":"http_metrics"`) {
			t.Log(line)
		}
	}
}

type httpCommandMetricEvent struct {
	HTTP struct {
		Admission struct {
			HardMax      int `json:"hard_max"`
			Limit        int `json:"limit"`
			Reserved     int `json:"reserved"`
			PeakReserved int `json:"peak_reserved"`
			Pending      int `json:"pending"`
			PeakPending  int `json:"peak_pending"`
		} `json:"admission"`
		Metrics struct {
			Counters map[string]int64 `json:"counters"`
		} `json:"metrics"`
	} `json:"http"`
	RawHTTP json.RawMessage `json:"-"`
}

func httpMetricEvents(tb testing.TB, logs string) []httpCommandMetricEvent {
	tb.Helper()
	var events []httpCommandMetricEvent
	for _, line := range strings.Split(logs, "\n") {
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var envelope struct {
			Message string          `json:"msg"`
			HTTP    json.RawMessage `json:"http"`
		}
		if err := json.Unmarshal([]byte(line), &envelope); err != nil {
			tb.Fatalf("invalid log event: %v: %s", err, line)
		}
		if envelope.Message != "http_metrics" {
			continue
		}
		var event httpCommandMetricEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			tb.Fatalf("invalid metrics event: %v", err)
		}
		if len(envelope.HTTP) == 0 || event.HTTP.Metrics.Counters == nil {
			tb.Fatalf("metrics event missing http/counters: %s", line)
		}
		event.RawHTTP = envelope.HTTP
		events = append(events, event)
	}
	return events
}

func singleHTTPMetricEvent(tb testing.TB, logs string) httpCommandMetricEvent {
	tb.Helper()
	events := httpMetricEvents(tb, logs)
	if len(events) != 1 {
		tb.Fatalf("expected one metrics event, got %d in %q", len(events), logs)
	}
	return events[0]
}

func assertHTTPMetricCounter(tb testing.TB, event httpCommandMetricEvent, name string, want int64) {
	tb.Helper()
	if got, ok := event.HTTP.Metrics.Counters[name]; !ok || got != want {
		tb.Fatalf("counter %q=%d (present=%t), want %d", name, got, ok, want)
	}
}

func assertHTTPCommandIDs(tb testing.TB, output string, mode httpCommandOutputMode, want []string) {
	tb.Helper()
	var got []string
	if mode.json {
		var payload jsonOutputPayload
		if err := json.Unmarshal([]byte(output), &payload); err != nil {
			tb.Fatalf("decode stdout JSON: %v: %q", err, output)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(output), &fields); err != nil {
			tb.Fatal(err)
		}
		for _, name := range []string{"inputs", "invalid", "targets", "errors", "output_count", "items"} {
			if _, ok := fields[name]; !ok {
				tb.Fatalf("stdout JSON missing %q", name)
			}
		}
		if len(fields) != 6 || payload.OutputCount != len(want) {
			tb.Fatalf("JSON schema/count changed: keys=%v output_count=%d, want %d", fields, payload.OutputCount, len(want))
		}
		got = payload.Items
	} else if output != "" {
		if !strings.HasSuffix(output, "\n") {
			tb.Fatalf("line output has no trailing newline: %q", output)
		}
		got = strings.Split(strings.TrimSuffix(output, "\n"), "\n")
		for i, id := range got {
			if mode.url {
				if !strings.HasPrefix(id, nicoWatchURLPrefix) {
					tb.Fatalf("URL output missing prefix: %q", id)
				}
				got[i] = strings.TrimPrefix(id, nicoWatchURLPrefix)
			}
		}
	}
	got, want = slices.Clone(got), slices.Clone(want)
	slices.Sort(got)
	slices.Sort(want)
	if !reflect.DeepEqual(got, want) {
		tb.Fatalf("output ID multiset mismatch:\n got %v\nwant %v", got, want)
	}
}

func newHTTPCommandFixture(tb testing.TB, pages, itemsPerPage int) (*httptest.Server, []string, []string) {
	tb.Helper()
	fixture, args, want := httpCommandFixture(pages, itemsPerPage)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, ok := fixture[r.URL.Path+"?page="+r.URL.Query().Get("page")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, payload)
	}))
	tb.Cleanup(server.Close)
	return server, args, want
}

func httpCommandFixture(pages, itemsPerPage int) (map[string]string, []string, []string) {
	fixture := make(map[string]string)
	var args, want []string
	for target, path := range []string{"/users/1/videos", "/mylists/847130", "/users/2/videos"} {
		if target == 1 {
			args = append(args, "nicovideo.jp/mylist/847130")
		} else {
			args = append(args, "nicovideo.jp/user/"+strconv.Itoa(target/2+1))
		}
		for page := 1; page <= pages; page++ {
			ids := make([]string, itemsPerPage)
			for item := range ids {
				ids[item] = "sm" + strconv.Itoa(2+(target*pages+page-1)*itemsPerPage+itemsPerPage-item)
			}
			// Repeated IDs exercise preservation and dedupe across targets/pages.
			ids[len(ids)-1] = "sm1"
			want = append(want, ids...)
			fixture[path+"?page="+strconv.Itoa(page)] = httpCommandPagePayload(target == 1, pages*100, ids)
		}
	}
	return fixture, args, want
}

func httpCommandPagePayload(mylist bool, total int, ids []string) string {
	var b strings.Builder
	if mylist {
		fmt.Fprintf(&b, `{"meta":{"status":200},"data":{"mylist":{"totalCount":%d,"items":[`, total)
	} else {
		fmt.Fprintf(&b, `{"meta":{"status":200},"data":{"totalCount":%d,"items":[`, total)
	}
	for i, id := range ids {
		if i > 0 {
			b.WriteByte(',')
		}
		key := "essential"
		if mylist {
			key = "video"
		}
		fmt.Fprintf(&b, `{"%s":{"id":"%s","registeredAt":"2024-01-02T03:04:05Z","count":{"comment":12}}}`, key, id)
	}
	b.WriteString(`]}}`)
	if mylist {
		b.WriteByte('}')
	}
	return b.String()
}
