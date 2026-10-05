# HTTP metrics and fixed concurrency

This milestone implements the measurement and fixed-cap foundation for #300. It does not implement the adaptive controller in #301 or complete the entire #313 roadmap.

## Reading an event

Enable `--http-metrics`; the existing JSON logger emits `msg: "http_metrics"` with one `http` object. `http.admission` describes reservations and `http.metrics` contains observations. The event is emitted after initialized execution finishes, including partial failures. Validation or logger initialization errors occur before collection starts and produce no metrics event. Logging retains the existing logger's write-error behavior.

- `admission.hard_max` is the configured cap; zero means no extra cap. `limit` is the current cap (fixed in this milestone).
- `reserved` includes rate-waiting work that already owns a slot. `pending` counts requests waiting to acquire a slot. Peak values cover the command lifetime. At normal completion both current values are zero.
- `metrics.in_flight` contains current, peak and time-weighted average actual HTTP work. It starts at an application `Do` invocation and ends only after the body closes or transport-error cleanup. The average includes initial scheduling and final output time, including zero-activity intervals.
- `elapsed_seconds` begins when the command creates its control object and ends at the diagnostic snapshot. It includes scheduling and final output; it excludes process startup, flag/date validation, logger initialization and diagnostic serialization. It is not a steady-state-only duration.
- `counters.attempts` counts actual application `http.Client.Do` calls. `retries` counts only dispatched attempts after the first logical request attempt. Canceled retry sleeps do not count as retries. Redirect hops and transport-internal retries are not extra application attempts.
- `pages`, `page_success` and `page_errors` describe logical fetches. A 404 is a successfully observed natural end. Cancellation can increment page_errors even when existing collection behavior returns a clean empty result.
- `http_200`, `http_404`, `http_429`, `http_other_4xx`, `http_5xx` and `http_other` classify final response headers per application attempt. Header success is not page success.
- `transport_errors`, `body_errors`, `close_errors` and `decode_errors` are separate. `cancellations` is the context-cancellation/deadline subset of transport errors; `body_cancellations` and `wait_cancellations` describe separate boundaries. Categories can overlap and must not be summed into a disjoint failure total. Existing close errors remain nonfatal.
- `trace_dropped` counts starts omitted because a trace phase became ambiguous or exceeded its bound. Overlapping DNS/TLS starts and duplicate simultaneous dial keys cannot be paired reliably, so that phase is disabled for the rest of the attempt. Distinct dial keys are tracked independently up to 64 pending starts. Completed observations remain valid; omissions are not zero-duration samples.
- `connections` and `connections_reused` count GotConn trace events. Redirects or transport retries can produce more connection events than application attempts. `connection_reuse_ratio` is omitted without events.
- `body_bytes` counts bytes actually read by page fetches, including partial failed reads; discarded retry/404 bodies are closed without adding unread bytes.

## Durations

All units are seconds. Each duration has `total_count`, cumulative `total_seconds` and `mean_seconds`, plus `window_count` and recent-window quantiles. Each ring stores at most 1024 observations. p50 needs one observation, p95 needs 20 and p99 needs 100; absent values indicate insufficient samples. All-run means and recent-window quantiles describe different populations.

- `service`: Do entry through body-close/transport cleanup; excludes admission, rate/backoff waits and decode.
- `semaphore_wait`: admission call duration, including lock/queue acquisition, not only blocked time.
- `backoff_wait`: observed retry wait, interrupted if canceled.
- `rate_wait`: incremental pacing wait. With no cap, the original combined rate/backoff reservation is preserved: the first observed interval up to the requested backoff is attributed to backoff, and only the remainder to rate waiting. With a cap, retry sleep precedes admission and rate waiting observes the remaining pacing constraint. Relative ordering of concurrent rate reservations is not guaranteed.
- `connection_acquire`, `dns`, `connect`, `tls`: observed trace event intervals. A fresh trace is attached to each application attempt. Missing events produce no samples; reused connections usually have no DNS/connect/TLS sample.
- `ttfb`: application-attempt start through its first first-byte event. Redirects do not restart this particular interval.
- `body_read`: io.ReadAll call, including waiting for body bytes. `body_close`: body Close call. `decode`: page parser execution.

Service, body and trace intervals overlap. Never sum these distributions to reconstruct wall time. Network time can include local client/transport scheduling and callback overhead; these are application observations, not packet-level measurements.

## Safety and compatibility

Both new flags default off. Fixed cap alone avoids trace/metric allocation. One command shares admission across all target and page workers, user/mylist, line/JSON/unordered and retries. Slots last through body close and are released before retry waiting. Pending admission is FIFO and cancellation-aware. Internal drain-on-shrink is tested but no automatic adjustment runs.

Normal output/schema, filtering, natural empty/404 termination, partial results, summary, progress and error precedence remain unchanged. Metrics contain fixed aggregate labels only, without request URLs, target IDs, credentials or response contents. Existing unrelated operational logs keep their existing contents.

A low peak with no queue is evidence of low utilization, not proof of its cause. Small workloads, unknown counts, target/page worker supply, rate constraints and final output can all explain it. This milestone deliberately does not claim to diagnose queue starvation or pick an optimal concurrency.

## Reproducible evaluation

All fixtures are local httptest servers or controlled fake transports. No live niconico API workload is included. Baseline and measurement comparisons include command construction and initial page discovery in each timed operation; they do not include operating-system process startup. Results and limitations are recorded below after validation.

### Local results (2026-10-05)

Base: `6d89dc9076756be161cf26101d521325d7d33429`. Environment: Linux/amd64, Go 1.27.1, AMD EPYC 9V74, benchmark GOMAXPROCS 9. These are local synthetic results, not live API performance or concurrency recommendations. Each reported setting has five repeats.

#### Existing workload: both flags off

The unchanged existing benchmark fixture has three mixed targets, 300 output IDs and six HTTP requests. Each benchmark sample used Go's default one-second target. Median command time:

| Mode | Master (ms) | New flags off (ms) | Change |
| --- | ---: | ---: | ---: |
| LineOutput | 1.138 | 1.081 | -5.0% |
| LineOutputNoSort | 1.059 | 1.097 | +3.5% |
| JSONOutput | 1.172 | 1.221 | +4.2% |
| JSONOutputNoSort | 1.103 | 1.087 | -1.4% |

Changes from −5.0% to +4.2% are within the initial 5% investigation threshold; this is not evidence of a speedup. The default request/rate path remains uninstrumented.

#### Metrics cost and fixed caps

The new matrix covers short (one request/one ID), supplied mixed (15 requests/1,500 IDs), and supplied large (120 requests/12,000 IDs), four output modes, caps 0/2/8 and metrics off/on: 72 settings, five repeats at 10 operations per sample. A second line-output pass used 300ms per sample. IDs were checked outside timing for each setting.

The 300ms pass showed substantial scheduling noise, including apparent negative overhead and one +25.1% large uncapped result. Rather than select favorable samples, the large uncapped case was repeated with a one-second target. The following stronger repeats and the short pass summarize the meaningful cost:

| Workload | Cap | Metrics off median [range] ms | Metrics on median [range] ms | Change |
| --- | ---: | ---: | ---: | ---: |
| Short, one request | 0 | 0.142 [0.130–0.152] | 0.184 [0.177–0.194] | +29.6% |
| Large, zero added latency | 0 | 20.151 [19.747–22.783] | 20.884 [20.617–21.449] | +3.6% |
| Large, controlled 2ms service delay | 8 | 53.018 [52.329–54.655] | 53.128 [52.062–53.457] | +0.2% |

Enabled metrics are measurably expensive for very short commands: the short uncapped sample adds about 42 microseconds and 7.9KB. Short cap-only/on comparisons varied from +19% to +69%; no negligible-overhead claim is made for these cases. Lazy bounded buffers reduced the initial roughly 110KB short-command allocation increment to about 7.5–7.9KB in the stronger pass. The large repeated uncapped comparison is +3.6%; controlled delayed HTTP is +0.2%, both within the initial threshold. These are workload-specific observations with overlapping ranges, not guarantees.

No fixed cap is recommended from these loopback timings. Worker supply, service capacity, delays and scheduling differ from the real service. No live API requests were made, and adaptive tuning remains unimplemented.

#### One observable larger run

`go test ./cmd -run '^TestHTTPMetricsLocalReport$' -count=1 -v` is a reproducible aggregate diagnostic example. One recorded run fetched 120 pages/12,000 IDs, with 120 attempts, zero retries/429/errors, peak actual in-flight 8, peak pending 4, and final reserved/pending/in-flight all zero. Service p50/p95/p99 were 0.613/3.199/5.140ms from 120 samples; 108/120 connection events reused connections. Elapsed measurement was 26.536ms. This single-run latency sample illustrates the schema, not a statistically established latency claim.

#### Reproduction

```sh
go test ./cmd -run '^$' -bench 'BenchmarkRunRootCmdLargeFanIn(LineOutput|JSONOutput)' -benchmem -count=5
go test ./cmd -run '^$' -bench '^BenchmarkHTTPCommand$' -benchmem -benchtime=10x -count=5
go test ./cmd -run '^$' -bench '^BenchmarkHTTPCommand/(short|supplied_mixed|supplied_large)/line/' -benchmem -benchtime=300ms -count=5
go test ./cmd -run '^$' -bench '^BenchmarkHTTPCommand/supplied_large/line/cap=0/' -benchmem -benchtime=1s -count=5
go test ./cmd -run '^$' -bench '^BenchmarkHTTPNetworkBoundMeasurement$' -benchmem -benchtime=1s -count=5
go test ./cmd -run '^TestHTTPMetricsLocalReport$' -count=1 -v
```

All timed iterations create a fresh command and include initial page discovery, output, and enabled diagnostic serialization. Server setup is outside timing. Process startup and the compiler are outside timing. Metrics use bounded memory independent of run length, but the application's existing output/result storage is unchanged and can grow with result count. Retry/rate/cancellation scenarios are correctness tests, not comprehensive performance models. The proposed high-concurrency production matrix and real-service validation remain future opt-in work.
