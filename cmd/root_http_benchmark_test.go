package cmd

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"
)

// BenchmarkHTTPCommand uses real loopback HTTP and immutable synthetic payloads.
// The short case exposes fixed setup/diagnostic costs. The supplied mixed case
// has three targets, five known pages each, and 100 items per page. Its twelve
// later-page candidates can saturate either positive cap without artificial
// sleeps, a live API, or scheduler-dependent ordering assertions. The large
// mixed case supplies 120 pages and 12,000 IDs, allowing service p99 reporting.
//
// Run, for example:
//
//	go test ./cmd -run '^$' -bench '^BenchmarkHTTPCommand$' -benchmem -benchtime=10x -count=5
//
// Each configuration is checked for exact ID multiplicities outside timing.
// Timed iterations include fresh command setup, HTTP, output, and structured
// logger serialization to io.Discard, including the final metrics event.
func BenchmarkHTTPCommand(b *testing.B) {
	for _, workload := range []struct {
		name         string
		pages        int
		itemsPerPage int
		short        bool
	}{
		{name: "short", pages: 1, itemsPerPage: 1, short: true},
		{name: "supplied_mixed", pages: 5, itemsPerPage: 100},
		{name: "supplied_large", pages: 40, itemsPerPage: 100},
	} {
		b.Run(workload.name, func(b *testing.B) {
			server, args, want := newHTTPCommandFixture(b, workload.pages, workload.itemsPerPage)
			if workload.short {
				args, want = args[:1], want[:1]
			}
			for _, mode := range httpCommandOutputModes {
				for _, cap := range []int{0, 2, 8} {
					for _, metrics := range []bool{false, true} {
						b.Run(fmt.Sprintf("%s/cap=%d/metrics=%t", mode.name, cap, metrics), func(b *testing.B) {
							b.StopTimer()
							cfg := testFetchConfig(server.URL)
							cfg.Concurrency, cfg.PageConcurrency = 3, 4
							cfg.HTTPConcurrency, cfg.HTTPMetrics = cap, metrics
							cfg.JSONOutput, cfg.NoSortOutput = mode.json, mode.noSort
							cfg.HTTPClientTimeout = 10 * time.Second
							deps := newTestRootDeps()
							deps.Stderr = io.Discard
							deps.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil)) //nolint:sloglint // Benchmark includes structured diagnostic serialization.

							var validation bytes.Buffer
							deps.Stdout = &validation
							if err := executeBenchmarkRootCommand(cfg, deps, args...); err != nil {
								b.Fatalf("fixture validation command: %v", err)
							}
							assertHTTPCommandIDs(b, validation.String(), mode, want)
							deps.Stdout = io.Discard

							b.ReportAllocs()
							b.ResetTimer()
							b.StartTimer()
							for i := 0; i < b.N; i++ {
								if err := executeBenchmarkRootCommand(cfg, deps, args...); err != nil {
									b.Fatalf("command: %v", err)
								}
							}
						})
					}
				}
			}
		})
	}
}
