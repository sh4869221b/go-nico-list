package cmd

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// BenchmarkAdaptiveHTTPCommand is a local synthetic comparison, not a live API
// capacity recommendation. Sustained cases offer 16 target x 8 page workers,
// sufficient to exercise all tested hard ceilings. Fixed4 supplies a conservative
// zero-429 reference for the capacity-drop fixture; faster configurations with
// rejected requests must be reported separately when comparing load constraints.
// Service delays are deliberate
// benchmark inputs; no test uses sleeps to establish request ordering.
//
// Timed work includes a fresh command, page discovery, HTTP, output and optional
// diagnostic serialization. Fixture reset and ID-multiset assertions are outside
// timing. Each iteration must produce exactly the same IDs, including duplicates.
// The server reports attempts/retries/429 even when diagnostics are disabled.
//
// Run focused five-repeat comparisons, for example:
//
//	go test ./cmd -run '^$' -bench '^BenchmarkAdaptiveHTTPCommand/stable_capacity/.*/metrics=false$' -benchtime=1x -count=5 -benchmem
//
// Sustained iterations take seconds. Short jobs expose startup/convergence costs;
// these timings include command construction, but not OS process startup.
func BenchmarkAdaptiveHTTPCommand(b *testing.B) {
	for _, workload := range []adaptiveHTTPWorkload{
		{name: "stable_capacity", targets: 16, pages: 256, pageWorkers: 8, delay: 8 * time.Millisecond, capacity: 32},
		{name: "capacity_drop_recovery_429", targets: 16, pages: 128, pageWorkers: 8, delay: 8 * time.Millisecond, capacity: 32, drop: true},
		{name: "high_latency", targets: 16, pages: 32, pageWorkers: 8, delay: 50 * time.Millisecond, capacity: 128},
		{name: "rate_bound", targets: 16, pages: 8, pageWorkers: 8, delay: 2 * time.Millisecond, capacity: 128, rate: 200, minInterval: 6 * time.Millisecond},
		{name: "low_worker_supply", targets: 3, pages: 64, pageWorkers: 1, delay: 5 * time.Millisecond, capacity: 128},
		{name: "unknown_count", targets: 16, pages: 32, pageWorkers: 8, delay: 5 * time.Millisecond, capacity: 128, unknown: true},
		{name: "short", targets: 1, pages: 1, pageWorkers: 8, capacity: 128},
	} {
		b.Run(workload.name, func(b *testing.B) {
			var fixture struct {
				server     *httptest.Server
				args, want []string
				mu         sync.Mutex
				stats      adaptiveHTTPServerStats
				seen       map[string]int
			}
			fixture.seen = make(map[string]int)
			payloads := make(map[string]string)
			for target := 1; target <= workload.targets; target++ {
				mylist := target%2 == 0
				path := "/users/" + strconv.Itoa(target) + "/videos"
				input := "nicovideo.jp/user/" + strconv.Itoa(target)
				if mylist {
					path = "/mylists/" + strconv.Itoa(target)
					input = "nicovideo.jp/mylist/" + strconv.Itoa(target)
				}
				fixture.args = append(fixture.args, input)
				for page := 1; page <= workload.pages; page++ {
					ids := []string{"sm" + strconv.Itoa(target*workload.pages+page), "sm1"}
					fixture.want = append(fixture.want, ids...)
					payload := httpCommandPagePayload(mylist, workload.pages*100, ids)
					if workload.unknown {
						payload = strings.Replace(payload, fmt.Sprintf(`"totalCount":%d,`, workload.pages*100), "", 1)
					}
					payloads[path+"?page="+strconv.Itoa(page)] = payload
				}
			}
			slices.Sort(fixture.want)
			fixture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				key := r.URL.Path + "?page=" + r.URL.Query().Get("page")
				fixture.mu.Lock()
				fixture.stats.attempts++
				fixture.seen[key]++
				attempt := fixture.seen[key]
				if attempt > 1 {
					fixture.stats.retries++
				}
				fixture.stats.active++
				active := fixture.stats.active
				fixture.stats.peak = max(fixture.stats.peak, active)
				capacity := workload.capacity
				// Drop/recovery is driven by completed useful work rather than wall time,
				// so each policy must cross the same capacity phases before finishing.
				if workload.drop && fixture.stats.success >= workload.targets*workload.pages/4 && fixture.stats.success < workload.targets*workload.pages/2 {
					capacity = 4
				}
				reject := workload.drop && capacity == 4 && active > capacity && attempt == 1
				if reject {
					fixture.stats.rejected++
				}
				fixture.mu.Unlock()
				defer func() {
					fixture.mu.Lock()
					fixture.stats.active--
					fixture.mu.Unlock()
				}()

				// Explicit synthetic service delay, including congestion above capacity.
				// This models a soft saturation knee rather than a real API's internals.
				if delay := workload.delay + time.Duration(max(0, active-capacity))*2*time.Millisecond; delay > 0 {
					time.Sleep(delay)
				}
				if reject {
					w.Header().Set("Retry-After", "0")
					w.WriteHeader(http.StatusTooManyRequests)
					return
				}
				payload, ok := payloads[key]
				if !ok {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, payload)
				fixture.mu.Lock()
				fixture.stats.success++
				fixture.mu.Unlock()
			}))
			b.Cleanup(fixture.server.Close)
			for _, policy := range []struct {
				name     string
				max      int
				adaptive bool
			}{
				{name: "fixed4", max: 4},
				{name: "fixed8", max: 8},
				{name: "fixed16", max: 16},
				{name: "fixed32", max: 32},
				{name: "fixed48", max: 48},
				{name: "fixed64", max: 64},
				{name: "adaptive32", max: 32, adaptive: true},
				{name: "adaptive64", max: 64, adaptive: true},
				{name: "adaptive_unlimited", adaptive: true},
			} {
				for _, metrics := range []bool{false, true} {
					b.Run(fmt.Sprintf("%s/metrics=%t", policy.name, metrics), func(b *testing.B) {
						b.StopTimer()
						cfg := testFetchConfig(fixture.server.URL)
						cfg.Concurrency, cfg.PageConcurrency = workload.targets, workload.pageWorkers
						cfg.HTTPConcurrency, cfg.AdaptiveHTTPConcurrency, cfg.HTTPMetrics = policy.max, policy.adaptive, metrics
						cfg.HTTPClientTimeout, cfg.Retries = 10*time.Second, 4
						cfg.RateLimit, cfg.MinInterval = workload.rate, workload.minInterval
						deps := newTestRootDeps()
						deps.Stderr = io.Discard
						var output, logs bytes.Buffer
						deps.Stdout = &output
						deps.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
						var attempts, retries, rejected, peak float64
						var elapsed time.Duration
						b.ReportAllocs()
						b.ResetTimer()
						for range b.N {
							fixture.mu.Lock()
							fixture.stats = adaptiveHTTPServerStats{}
							clear(fixture.seen)
							fixture.mu.Unlock()
							output.Reset()
							logs.Reset()
							b.StartTimer()
							started := time.Now()
							err := executeBenchmarkRootCommand(cfg, deps, fixture.args...)
							elapsed += time.Since(started)
							b.StopTimer()
							if err != nil {
								b.Fatalf("synthetic command: %v", err)
							}
							assertHTTPCommandIDs(b, output.String(), httpCommandOutputModes[0], fixture.want)
							fixture.mu.Lock()
							stats := fixture.stats
							fixture.mu.Unlock()
							if stats.active != 0 || (policy.max > 0 && stats.peak > policy.max) || stats.peak > workload.targets*workload.pageWorkers || stats.success != workload.targets*workload.pages {
								b.Fatalf("invalid server totals: %+v; hard max %d", stats, policy.max)
							}
							if metrics {
								event := singleHTTPMetricEvent(b, logs.String())
								assertHTTPMetricCounter(b, event, "attempts", int64(stats.attempts))
								assertHTTPMetricCounter(b, event, "retries", int64(stats.retries))
								assertHTTPMetricCounter(b, event, "http_429", int64(stats.rejected))
								if policy.adaptive {
									assertAdaptiveHTTPMetric(b, event, policy.max)
								}
							} else if len(httpMetricEvents(b, logs.String())) != 0 {
								b.Fatal("metrics disabled but diagnostics were logged")
							}
							attempts += float64(stats.attempts)
							retries += float64(stats.retries)
							rejected += float64(stats.rejected)
							peak += float64(stats.peak)
						}
						b.ReportMetric(attempts/float64(b.N), "attempts/op")
						b.ReportMetric(retries/float64(b.N), "retries/op")
						b.ReportMetric(rejected/float64(b.N), "429/op")
						b.ReportMetric(peak/float64(b.N), "server_peak/op")
						b.ReportMetric(float64(b.N*workload.targets*workload.pages)/elapsed.Seconds(), "pages/s")
					})
				}
			}
		})
	}
}

type adaptiveHTTPWorkload struct {
	minInterval                 time.Duration
	name                        string
	targets, pages, pageWorkers int
	capacity                    int
	delay                       time.Duration
	rate                        float64
	drop, unknown               bool
}

type adaptiveHTTPServerStats struct {
	attempts, retries, rejected int
	active, peak, success       int
}
