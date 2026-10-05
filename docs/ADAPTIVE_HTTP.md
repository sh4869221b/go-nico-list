# Experimental adaptive HTTP concurrency

This implements the controller and CLI integration for #301 on the measurement foundation from PR #315. It remains opt-in. A successful safety/test pass is not a claim that automatic tuning beats a well-chosen fixed cap. The larger #313 roadmap and its adoption/evaluation gates remain separate.

## Use and rollback

```sh
# No configured adaptive ceiling (equivalent to --http-concurrency 0):
go-nico-list --input-file users.txt --adaptive-http-concurrency --http-metrics
# Optionally impose an explicit hard ceiling:
go-nico-list --input-file users.txt --adaptive-http-concurrency --http-concurrency 32 --http-metrics
# Disable adaptive mode and use a fixed operating point while keeping diagnostics:
go-nico-list --input-file users.txt --http-concurrency 32 --http-metrics
```

The example ceiling is illustrative, not a recommendation for the live API. Adaptive mode accepts the default `--http-concurrency 0`, meaning no configured hard ceiling, and starts at 8. A positive value is an optional hard ceiling and starts at min(8, ceiling); negative values are invalid. Adaptive mode retains a positive current admission limit and never changes target/page worker settings or configured pacing. The default 3×1 workers cannot supply eight concurrent requests. With no adaptive flag, prior fixed/unlimited behavior remains intact.

## Policy

The controller observes completed HTTP work separately from optional full tracing. Service time excludes admission, retry/cooldown/rate waiting and JSON decode. Only complete HTTP 200 bodies with a successful close supply successful-service latency. Failed body reads reach the controller even if headers said 200. Decode errors and non-200 meta warnings do not become network congestion. Caller cancellation is neutral; a client timeout with a live caller remains an eligible error.

- Bounds: minimum 1, initial 8 without a configured maximum or min(8,max) with a positive optional maximum. A positive maximum is immutable; zero means no configured maximum. Shrink drains current work.
- Eligible window: at most 32 successful/overload observations, minimum 20 successes for p95. Neutral 404/cancel/other4xx outcomes do not erase valid observations by consuming this budget. Total completed counts remain scalar diagnostics.
- Time: evaluate a qualified partial window after one second. Sparse windows may stay open longer, bounded in memory and by service-scaled freshness. This permits real limit 1 / 100ms operation to collect 20 samples instead of getting stuck forever. Post-idle samples cannot turn an expired population into healthy evidence.
- Growth: two healthy windows, queued work, completion-sampled actual utilization ≥80%, rate-wait fraction ≤10%, no cooldown, and at least 25ms since an ordinary resize. Add 1 below 8; otherwise add 4, clamped only when a positive maximum is configured. Integer growth must not wrap.
- Decrease: two windows with p95 >1.5×the successful baseline, or meaningful overload pressure (at least 4 errors, at least 10% of 20+ eligible observations), halve the limit. Isolated failures/outliers hold rather than decrease.
- Baseline: healthy downward smoothing has weight 0.2. Pressure cannot ratchet the reference upward. Four stable error-free floor windows can rebaseline after a genuine latency step; there are no calibration requests or persisted learning.

Removing the configured ceiling adds no hidden numeric replacement cap, extra workers, memory/file-descriptor guard, probes or ceiling-sized allocation. Worker supply and user pacing remain effective constraints. Unlimited adaptive mode does not guarantee prevention of out-of-memory or file-descriptor exhaustion.

These coefficients are internal experimental choices. Their comparison and remaining performance failures are recorded below; they are not universal tuning advice.

## 429, deadlines and recovery

429 feedback acts at header receipt before body Close. A new congestion episode halves the current limit, including generation invalidation at the floor, and extends a shared pause to honor Retry-After/applicable backoff. Old in-flight responses can extend the deadline but cannot repeatedly halve the same generation.

Healthy growth does not consume a congestion episode: only a protective decrease sets the cutoff for deduplicating older responses, including ordinary latency/error decreases.

Already-dispatched HTTP work completes normally. New dispatches stop. Cooldown and retry waiting retain no HTTP slot; a rate-waiter rechecks the pause epoch before dispatch, even if an entire pause elapsed during its wait. Stale pacing grants are reacquired rather than accumulated for a later burst. Already-granted reservations also check the new actual-in-flight limit after shrink.

Retry-status deadlines begin at header receipt in adaptive mode. A slow body close consumes that deadline rather than restarting the full wait afterward. Transport-error deadlines begin after cleanup. Fixed mode preserves its existing retry waiting behavior.

After a pause, the first current-limit dispatches are spaced by baseline/current, clamped to 25–250ms, and remain subject to independent user pacing. Fresh healthy windows are required before increasing. Admission remains FIFO once queued; retry/cooldown sleepers have no global arrival-order promise. Context cancellation releases waiters, and no permanent controller goroutine survives a command.

## Diagnostics

`--http-metrics` adds `http.adaptive` to the existing final aggregate. It includes initial/current/max, resize counts, attempts/successes/eligible errors/429, stale completions, sample windows, successful p50/p95 and baseline, actual pending/in-flight, cooldown/recovery, last_reason, and a ring of the last 32 decisions (including holds). The adaptive `max` and admission `hard_max` fields are zero when no maximum is configured; the current adaptive limit remains positive. History is bounded and is not a full per-request audit log.

Without that flag, controller observations still run but full httptrace, diagnostic windows and final serialization stay off. No extra stdout, schema, summary, progress or error-output changes are introduced. Snapshots contain no URL, target ID, headers, credentials or response bodies.

## Safety verification

Deterministic tests exercise growth/bounds, low samples, realistic sequential floor recovery, latency steps, synchronized long-latency batches, stale evidence after idle, neutral-heavy observation streams, error pressure, rate/worker-limited holds,429 generations, finite recovery pacing and detached bounded snapshots. Integration tests cover slow 429 Close, header-time Retry-After, pauses inside rate waits, stale 429 extensions, shrink with outstanding reservations, client versus caller timeouts, failed bodies, cancellation and many-worker completion after cooldown.

CLI regression tests cover all output modes, unknown counts, duplicate inputs, dedupe, partial results, strict/best-effort, progress, input/output failures, diagnostic routing and command isolation. Default-zero and explicit-zero adaptive settings are exercised with and without metrics alongside optional positive caps; negative settings remain validation errors. No live niconico service is contacted by the tests or measurements.

## Evaluation scope

The synthetic server uses 16 targets×8 page workers, enough to supply a 64-request ceiling. Each page has two synthetic IDs; known totals select a fixed page range. This isolates HTTP scheduling and deliberately does not model a full 100-item production response. Cases include a stable capacity knee, capacity drop/recovery with 429, high latency, explicit rate limitation, low worker supply, unknown counts, short commands and a fresh-process short test helper. These sustained comparisons use sorted line output; other output modes are covered by functional regression tests rather than the full performance matrix. Every iteration checks the complete output ID multiset, applicable positive hard cap, worker-supply bound and server totals. Synthetic delay is intentional input, not sleep-based test ordering.

The historical matrix below compares fixed caps 4/8/16/32/48/64 and adaptive ceilings 32/64. The current benchmark also includes `adaptive_unlimited`; its measurements must be reported separately from the historical bounded-only matrix. Fixed 4 is a conservative zero429 reference for a phase whose capacity drops to 4; a faster fixed run with hundreds of rejections is not automatically an acceptable load-matched baseline. Adaptive transient feedback can still produce 429s, so fewer 429s alone does not prove it is best.

Each final comparison uses five repeats. The command benchmark includes command construction, first-page discovery and final output; the separate fresh-process benchmark uses the test executable as a CLI helper, not a release binary. In-process runs can reuse the shared default HTTP transport; the helper process starts with a fresh client transport. Allocation numbers for that process benchmark cover the parent harness, not child RSS. Results do not establish real-service capacity.

The controller has one global latency reference. Payload-size changes, new connections or a lasting network change can resemble queueing pressure; the diagnostics can distinguish some causes, but the controller remains conservative. No per-endpoint model or universal optimum is implied.

## Local results and acceptance status (2026-10-05)

These are historical measurements from before the pressure-cutoff correction described above and before support for unlimited adaptive mode. The measured adaptive policies all used positive ceilings; these results do not measure unlimited adaptive performance. They remain a baseline record, not measurements of the corrected controller. In particular,429/drop behavior can change after the correction; new research comparisons identify the corrected control separately. The earlier delivered artifact is superseded for use because of that known429 edge case, while its bytes/results are retained for comparison.

Base: `a9e8446cc00d5ad299998a3e66f87cf294d05ef6`. Linux/amd64, Go 1.27.1, AMD EPYC 9V74, benchmark GOMAXPROCS 9. Results are local synthetic measurements, not live API predictions. The full matrix has 56 configurations (seven workloads×eight policies), five one-operation repeats each. All 280 runs passed ID-multiset, completed-work and hard-cap assertions. Metrics were off for the main matrix.

**Functional/safety gates pass; the proposed within 10% throughput adoption gate does not. Keep the feature experimental and opt-in. Do not close #301/#313 as fully accepted or change defaults on these results.**

### Main comparisons

Times below are medians in seconds, with min–max ranges. The reference is the fastest tested fixed cap within the same ceiling, except drop/recovery where both the zero-rejection and faster high-rejection references are shown explicitly.

| Workload | Policy | Median seconds [range] | Median 429s | Interpretation |
| --- | --- | ---: | ---: | --- |
| stable_capacity | fixed32 | 1.136 [1.125–1.141] | 0 | Fastest fixed; zero 429 |
| stable_capacity | adaptive32 | 1.269 [1.263–1.319] | 0 | Same ceiling 32 |
| stable_capacity | adaptive64 | 1.581 [1.574–1.625] | 0 | Ceiling 64; still compare with faster fixed32 |
| capacity_drop_recovery_429 | fixed4 | 4.499 [4.452–4.506] | 0 | Conservative zero 429 reference |
| capacity_drop_recovery_429 | fixed32 | 2.556 [2.544–2.587] | 555 | Faster, but substantial rejected load |
| capacity_drop_recovery_429 | adaptive32 | 3.814 [3.740–4.024] | 56 | Transient rejection remains |
| capacity_drop_recovery_429 | adaptive64 | 3.829 [3.757–3.911] | 56 | Transient rejection remains |
| high_latency | fixed32 | 0.867 [0.865–0.869] | 0 | Fixed reference for ceiling 32 |
| high_latency | fixed64 | 0.462 [0.459–0.462] | 0 | Fixed reference for ceiling64 |
| high_latency | adaptive32 | 1.629 [1.583–1.630] | 0 | Convergence cost |
| high_latency | adaptive64 | 1.581 [1.580–1.630] | 0 | Did not reach 64 before workload ended |
| rate_bound | fixed32 | 0.765 [0.765–0.766] | 0 | User pacing dominates |
| rate_bound | adaptive32 | 0.765 [0.765–0.766] | 0 | No increases/decreases |
| low_worker_supply | fixed4 | 0.352 [0.351–0.359] | 0 | Three supplied workers |
| low_worker_supply | adaptive32 | 0.356 [0.352–0.360] | 0 | Initial limit 8 held; actual peak 3 |
| unknown_count | fixed16 | 0.191 [0.191–0.199] | 0 | Sequential pages; 16 target candidates |
| unknown_count | adaptive32 | 0.230 [0.225–0.233] | 0 | Learns toward available supply |

- Stable capacity: adaptive32 is 11.7% slower; adaptive64 is 39.1% slower. The 10% gate is missed, including the smaller-ceiling comparison.
- High latency: adaptive32 is 88.0% slower than fixed32; adaptive64 is 242.6% slower than fixed64. With 512 requests and conservative sample requirements, startup/convergence dominates. The controller cannot infer available capacity without evidence.
- Drop/recovery: adaptive32 takes 3.814s with 56 rejected attempts, versus 4.499s/zero rejections at fixed4, or 2.556s/555 rejections at fixed32. Fewer rejections than an aggressive fixed run does not establish optimality or satisfy a zero-rejection constraint.
- Unknown counts: adaptive32 is 20.6% slower than fixed16 while eventually using the available 16 target candidates. Page scheduling remains sequential as required.

The drop fixture changes its capacity after fixed amounts of completed useful work, so every policy traverses the same phases. It rejects only an initial attempt above reduced capacity and uses Retry-After 0, exercising the 100ms fallback. Real positive Retry-After values can make the shared pause cost larger; that behavior is correct and is tested separately.

### Bounded coefficient exploration

Three internal configurations were compared with three exploratory repeats per workload before final validation. These are not additional public knobs:

| Window / ordinary interval / growth | Stable median s | Drop median s /429s | High-latency median s |
| --- | ---: | ---: | ---: |
|64 /250ms /+4 |1.855 |3.804 /35 |2.148 |
|32 /100ms /+4 |1.419 |4.440 /66 |1.582 |
|32 /25ms /+1 below8, otherwise+4 |1.256 |3.758 /55 |1.625 |

The final choice improves stable convergence and avoids large floor-recovery jumps, but increases transient rejected load relative to the slowest candidate. The final five-repeat measurements above supersede these exploratory timing estimates. No isolated best run is used to claim a universal optimum.

### Diagnostics and short-process checks

Five diagnostics-enabled repeats verified end state and observations. Rate-bound and supply-limited cases made zero increases/decreases and retained current 8; actual peaks were 1–2 and 3, respectively. Drop/recovery runs recorded 22 increases and 5 decreases and finished at 32 with no active/reserved/pending work. Stable diagnostics runs finished at 32; one of five recorded an extra latency-driven decrease. This illustrates sensitivity to timing noise and is another reason to keep the feature experimental.

Fresh-process helper medians: fixed8 3.128ms [3.061–4.017], adaptive32 3.232ms [3.102–3.287], adaptive64 3.152ms [2.937–3.178]. These are five samples of ten process launches each, with a fresh transport per child. The helper is the Go test executable, not the release binary; parent allocation counts are not child RSS. One-operation in-process short samples are noisy and not used as an adoption claim.

### What remains

The controller, safety integration and local regression gates are implemented. Broad performance acceptance is still open. Improving cold-start convergence without causing a larger initial load burst needs a separate measured design decision; do not silently add an initial-concurrency flag, change the initial 8 or change defaults. Live-service load tests and publication/merge are separate authorization steps.

### Reproduction

```sh
go test ./cmd -run '^$' -bench '^BenchmarkAdaptiveHTTPCommand/.*/.*/metrics=false$' -benchmem -benchtime=1x -count=5 -timeout=15m
go test ./cmd -run '^$' -bench '^BenchmarkAdaptiveHTTPCommand/(stable_capacity|capacity_drop_recovery_429|rate_bound|low_worker_supply)/adaptive32/metrics=true$' -benchmem -benchtime=1x -count=5
go test ./cmd -run '^$' -bench '^BenchmarkAdaptiveHTTPShortProcess$' -benchmem -benchtime=10x -count=5
```

Sustained comparisons take several minutes. Preserve all repeats and ID/load checks when modifying parameters. Raw final matrices, telemetry, coefficient comparisons and the intentionally interrupted pre-neutral-fix pass are retained in the delivery for auditability; that partial pass is not part of the final statistics.
