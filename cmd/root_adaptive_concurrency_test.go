package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAdaptiveConcurrencyTargetOverrides(t *testing.T) {
	for _, test := range []struct {
		name      string
		flags     []string
		peak      int
		httpMax   int
		adaptive  bool
		fixedHTTP bool
	}{
		{name: "unchanged_defaults", peak: 3},
		{name: "flag_alone", flags: []string{"--adaptive-concurrency"}, peak: 8, adaptive: true},
		{name: "explicit_default_target", flags: []string{"--adaptive-concurrency", "--concurrency=3"}, peak: 3, adaptive: true},
		{name: "shorthand_before_auto", flags: []string{"-n", "2", "--adaptive-concurrency"}, peak: 2, adaptive: true},
		{name: "page_override_keeps_targets_auto", flags: []string{"--page-concurrency=1", "--adaptive-concurrency"}, peak: 8, adaptive: true},
		{name: "fixed_http_keeps_worker_feedback", flags: []string{"--http-concurrency=2", "--adaptive-concurrency"}, peak: 2, httpMax: 2, adaptive: true, fixedHTTP: true},
		{name: "explicit_unlimited_http", flags: []string{"--adaptive-concurrency", "-n", "12", "--http-concurrency=0"}, peak: 12, adaptive: true, fixedHTTP: true},
		{name: "all_overrides", flags: []string{"--adaptive-concurrency", "--concurrency=3", "--page-concurrency=1", "--http-concurrency=4"}, peak: 3, httpMax: 4, adaptive: true, fixedHTTP: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			barrier := newAdaptiveCommandBarrier(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				barrier.hold(r)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"meta":{"status":200},"data":{"items":[]}}`)
			}))
			t.Cleanup(server.Close)
			cfg := newTestRootConfig()
			cfg.BaseURL, cfg.Retries, cfg.HTTPClientTimeout, cfg.HTTPMetrics = server.URL, 1, 10*time.Second, true
			var args []string
			for target := 1; target <= 12; target++ {
				args = append(args, fmt.Sprintf("nicovideo.jp/user/%d", target))
			}
			out, _, event := executeSynchronizedAdaptiveCommand(t, cfg, append(slices.Clone(test.flags), args...), barrier, test.peak)
			if out != "" {
				t.Fatalf("empty targets wrote data: %q", out)
			}
			assertHTTPMetricCounter(t, event, "attempts", 12)
			if test.adaptive {
				assertAdaptiveHTTPMetric(t, event, test.httpMax, test.fixedHTTP)
			} else if event.HTTP.Adaptive != nil {
				t.Fatal("default command enabled adaptive concurrency")
			}
		})
	}
}

func TestAdaptiveConcurrencyKnownPageOverrides(t *testing.T) {
	for _, mylist := range []bool{false, true} {
		for _, test := range []struct {
			name      string
			flags     []string
			peak      int
			httpMax   int
			fixedHTTP bool
			serial    bool
		}{
			{name: "flag_alone", flags: []string{"--adaptive-concurrency"}, peak: 8},
			{name: "target_override_keeps_pages_auto", flags: []string{"--adaptive-concurrency", "--concurrency=1"}, peak: 8},
			{name: "page_override_before_auto", flags: []string{"--page-concurrency=3", "--adaptive-concurrency"}, peak: 3},
			{name: "explicit_default_page", flags: []string{"--adaptive-concurrency", "--page-concurrency=1"}, peak: 1, serial: true},
			{name: "fixed_http_keeps_pages_auto", flags: []string{"--adaptive-concurrency", "--http-concurrency=16"}, peak: 8, httpMax: 16, fixedHTTP: true},
			{name: "all_overrides", flags: []string{"-n", "1", "--page-concurrency=3", "--http-concurrency=2", "--adaptive-concurrency"}, peak: 2, httpMax: 2, fixedHTTP: true},
			{name: "explicit_unlimited_http", flags: []string{"--adaptive-concurrency", "--concurrency=1", "--page-concurrency=16", "--http-concurrency=0"}, peak: 16, fixedHTTP: true},
		} {
			t.Run(fmt.Sprintf("mylist=%t/%s", mylist, test.name), func(t *testing.T) {
				const pages = 17
				barrier := newAdaptiveCommandBarrier(t)
				var seenMu sync.Mutex
				seen := make(map[int]int)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					page, err := strconv.Atoi(r.URL.Query().Get("page"))
					if err != nil {
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					seenMu.Lock()
					seen[page]++
					seenMu.Unlock()
					if page > pages {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					if page > 1 {
						barrier.hold(r)
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, httpCommandPagePayload(mylist, pages*100, []string{fmt.Sprintf("sm%d", page)}))
				}))
				t.Cleanup(server.Close)
				cfg := newTestRootConfig()
				cfg.BaseURL, cfg.Retries, cfg.HTTPClientTimeout, cfg.HTTPMetrics = server.URL, 1, 10*time.Second, true
				target := "nicovideo.jp/user/1"
				if mylist {
					target = "nicovideo.jp/mylist/1"
				}
				out, _, event := executeSynchronizedAdaptiveCommand(t, cfg, append(slices.Clone(test.flags), target), barrier, test.peak)
				var want []string
				for page := 1; page <= pages; page++ {
					want = append(want, fmt.Sprintf("sm%d", page))
				}
				assertHTTPCommandIDs(t, out, httpCommandOutputModes[0], want)
				assertAdaptiveHTTPMetric(t, event, test.httpMax, test.fixedHTTP)
				attempts := int64(pages)
				if test.serial {
					attempts++
				}
				assertHTTPMetricCounter(t, event, "attempts", attempts)
				seenMu.Lock()
				defer seenMu.Unlock()
				for page := 1; page <= pages; page++ {
					if seen[page] != 1 {
						t.Errorf("page %d fetched %d times, want once", page, seen[page])
					}
				}
				if len(seen) != int(attempts) {
					t.Fatalf("unexpected speculative pages: %v", seen)
				}
			})
		}
	}
}

func TestAdaptiveConcurrencyUnlimitedHTTPKeepsBothWorkerLayersAutomatic(t *testing.T) {
	const targets, pages = 8, 9
	barrier := newAdaptiveCommandBarrier(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil || page < 1 || page > pages {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if page > 1 {
			barrier.hold(r)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, httpCommandPagePayload(false, pages*100, []string{fmt.Sprintf("sm%d", page)}))
	}))
	t.Cleanup(server.Close)
	cfg := newTestRootConfig()
	cfg.BaseURL, cfg.Retries, cfg.HTTPClientTimeout, cfg.HTTPMetrics = server.URL, 1, 10*time.Second, true
	args := []string{"--adaptive-concurrency", "--http-concurrency=0"}
	for target := 1; target <= targets; target++ {
		args = append(args, fmt.Sprintf("nicovideo.jp/user/%d", target))
	}
	out, _, event := executeSynchronizedAdaptiveCommand(t, cfg, args, barrier, targets*(pages-1))
	var want []string
	for range targets {
		for page := 1; page <= pages; page++ {
			want = append(want, fmt.Sprintf("sm%d", page))
		}
	}
	assertHTTPCommandIDs(t, out, httpCommandOutputModes[0], want)
	assertAdaptiveHTTPMetric(t, event, 0, true)
	assertHTTPMetricCounter(t, event, "attempts", targets*pages)
}

type adaptiveCommandBarrier struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	active  atomic.Int64
	peak    atomic.Int64
}

func newAdaptiveCommandBarrier(t *testing.T) *adaptiveCommandBarrier {
	t.Helper()
	barrier := &adaptiveCommandBarrier{started: make(chan struct{}, 256), release: make(chan struct{})}
	t.Cleanup(barrier.open)
	return barrier
}

func (b *adaptiveCommandBarrier) hold(r *http.Request) {
	current := b.active.Add(1)
	defer b.active.Add(-1)
	for old := b.peak.Load(); current > old && !b.peak.CompareAndSwap(old, current); old = b.peak.Load() {
	}
	b.started <- struct{}{}
	select {
	case <-b.release:
	case <-r.Context().Done():
	}
}

func (b *adaptiveCommandBarrier) open() {
	b.once.Do(func() { close(b.release) })
}

func executeSynchronizedAdaptiveCommand(t *testing.T, cfg RootConfig, args []string, barrier *adaptiveCommandBarrier, peak int) (string, string, httpCommandMetricEvent) {
	t.Helper()
	var logs bytes.Buffer
	deps := newTestRootDeps()
	deps.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	command, out, errOut := newTestRootCommand(t, cfg, deps)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	command.SetContext(ctx)
	command.SetArgs(args)
	done := make(chan error, 1)
	go func() { done <- command.Execute() }()
	for range peak {
		select {
		case <-barrier.started:
		case err := <-done:
			t.Fatalf("command ended before filling concurrency %d: %v", peak, err)
		case <-ctx.Done():
			t.Fatalf("command did not reach concurrency %d", peak)
		}
	}
	barrier.open()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("command: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("command did not finish after releasing requests")
	}
	if got := barrier.peak.Load(); got != int64(peak) {
		t.Fatalf("server observed concurrency %d, want %d", got, peak)
	}
	event := singleHTTPMetricEvent(t, logs.String())
	if event.HTTP.Admission.Reserved != 0 || event.HTTP.Admission.Pending != 0 {
		t.Fatalf("command leaked HTTP admission: %+v", event.HTTP.Admission)
	}
	return out.String(), errOut.String(), event
}
