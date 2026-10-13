# Experimental automatic concurrency

`--adaptive-concurrency` uses the HTTP feedback controller from #301 and the measurement foundation from PR #315 to adjust target, per-target page, and command-wide HTTP concurrency. It remains opt-in. A successful safety/test pass is not a claim that automatic tuning beats well-chosen fixed settings. The larger #313 roadmap and its adoption/evaluation gates remain separate.

## Use and rollback

```sh
# Automatically adjust targets, pages and HTTP with one option:
go-nico-list --input-file users.txt --adaptive-concurrency
# Fix page concurrency only; targets and HTTP remain automatic:
go-nico-list --input-file users.txt --adaptive-concurrency --page-concurrency 4
# Fix HTTP concurrency; unspecified target/page workers still adjust:
go-nico-list --input-file users.txt --adaptive-concurrency --http-concurrency 8 --http-metrics
# Explicit zero disables the HTTP cap while target/page workers still adjust:
go-nico-list --input-file users.txt --adaptive-concurrency --http-concurrency 0
# Disable adaptive mode and use a fixed operating point while keeping diagnostics:
go-nico-list --input-file users.txt --http-concurrency 32 --http-metrics
```

The example values are illustrative, not recommendations for the live API. Omitting all three concurrency options enables automatic control of all three layers. The HTTP controller starts at 8 with no configured maximum and supplies changing budgets for target/page workers. Blocked schedulers contribute pending demand to the feedback so growth does not require extra workers above the current budget. Schedulers start workers lazily as work becomes available, respond to budget changes, and let running work drain on shrink.

Explicit `--concurrency`/`-n`, `--page-concurrency`, or `--http-concurrency` fixes only that layer. Explicit flags are recognized even when equal to defaults, independently of flag order. Positive HTTP values fix HTTP admission and serve as the feedback controller's maximum for tuning unspecified workers, with initial feedback limit `min(8, value)`. Explicit HTTP zero removes HTTP admission limits while feedback still tunes unspecified workers. Negative HTTP values are invalid. Rate/min-interval settings remain independent. With no automatic flag, prior target/page defaults and fixed/unlimited HTTP behavior remain intact.

Page 1 discovers the page range before parallel work starts within a target. Only the remaining bounded pages from `totalCount` can run concurrently; unknown totals remain sequential until an empty page or HTTP 404. Explicit page concurrency 1 preserves its existing sequential termination behavior. Short runs, low input/page supply or configured pacing may leave the controller unchanged. Automatic control cannot guarantee a mathematical optimum or faster completion.

## Policy

The controller observes completed HTTP work separately from optional full tracing. Service time excludes admission, retry/cooldown/rate waiting and JSON decode. Only complete HTTP 200 bodies with a successful close supply successful-service latency. Failed body reads reach the controller even if headers said 200. Decode errors and non-200 meta warnings do not become network congestion. Caller cancellation is neutral; a client timeout with a live caller remains an eligible error.

- Feedback bounds: minimum 1, initial 8 without a configured maximum or min(8,max) with explicitly fixed positive HTTP concurrency. A positive maximum is immutable; zero means no configured feedback maximum. Unspecified HTTP admission follows this positive feedback limit, while explicit HTTP admission stays fixed or unlimited. Automatic worker budgets follow the feedback limit directly, including its minimum of 1. Shrink drains current work.
- Eligible window: at most 32 successful/overload observations, minimum 20 successes for p95. Neutral 404/cancel/other4xx outcomes do not erase valid observations by consuming this budget. Total completed counts remain scalar diagnostics.
- Time: evaluate a qualified partial window after one second. Sparse windows may stay open longer, bounded in memory and by service-scaled freshness. This permits real limit 1 / 100ms operation to collect 20 samples instead of getting stuck forever. Post-idle samples cannot turn an expired population into healthy evidence.
- Growth: two healthy windows, queued work, completion-sampled actual utilization ≥80%, rate-wait fraction ≤10%, no cooldown, and at least 25ms since an ordinary resize. Add 1 below 8; otherwise add 4, clamped only when a positive maximum is configured. Integer growth must not wrap.
- Decrease: two windows with p95 >1.5×the successful baseline, or meaningful overload pressure (at least 4 errors, at least 10% of 20+ eligible observations), halve the limit. Isolated failures/outliers hold rather than decrease.
- Baseline: healthy downward smoothing has weight 0.2. Pressure cannot ratchet the reference upward. Four stable error-free floor windows can rebaseline after a genuine latency step; there are no calibration requests or persisted learning.

Omitting a feedback maximum adds no hidden numeric replacement cap, memory/file-descriptor guard, probes or maximum-sized allocation. Automatic workers grow only as work and feedback permit. Input/page supply and user pacing remain effective constraints. This mode does not guarantee prevention of out-of-memory or file-descriptor exhaustion.

These coefficients are internal experimental choices. Their comparison and remaining performance failures are recorded below; they are not universal tuning advice.

## 429, deadlines and recovery

429 feedback acts at header receipt before body Close. A new congestion episode halves the current limit, including generation invalidation at the floor, and extends a shared pause to honor Retry-After/applicable backoff. Old in-flight responses can extend the deadline but cannot repeatedly halve the same generation.

Healthy growth does not consume a congestion episode: only a protective decrease sets the cutoff for deduplicating older responses, including ordinary latency/error decreases.

Already-dispatched HTTP work completes normally. New dispatches stop. Cooldown and retry waiting retain no HTTP slot; a rate-waiter rechecks the pause epoch before dispatch, even if an entire pause elapsed during its wait. Stale pacing grants are reacquired rather than accumulated for a later burst. When HTTP admission is automatic, already-granted reservations also check the new actual-in-flight limit after shrink. Explicit HTTP concurrency remains fixed, while the feedback decrease affects unspecified worker layers.

Retry-status deadlines begin at header receipt in adaptive mode. A slow body close consumes that deadline rather than restarting the full wait afterward. Transport-error deadlines begin after cleanup. Fixed mode preserves its existing retry waiting behavior.

After a pause, the first current-limit dispatches are spaced by baseline/current, clamped to 25–250ms, and remain subject to independent user pacing. Fresh healthy windows are required before increasing. Admission remains FIFO once queued; retry/cooldown sleepers have no global arrival-order promise. Context cancellation releases waiters, and no permanent controller goroutine survives a command.

## Diagnostics

`--http-metrics` adds `http.adaptive` to the existing final aggregate. It includes feedback initial/current/max, resize counts, attempts/successes/eligible errors/429, stale completions, sample windows, successful p50/p95 and baseline, actual pending/in-flight, cooldown/recovery, last_reason, and a ring of the last 32 decisions (including holds). The adaptive `max` and admission `hard_max` fields are zero when no maximum is configured; the current feedback limit remains positive. An explicitly fixed HTTP limit can differ from the current feedback limit. History is bounded and is not a full per-request audit log.

Without that flag, controller observations still run but full httptrace, diagnostic windows and final serialization stay off. No extra stdout, schema, summary, progress or error-output changes are introduced. Snapshots contain no URL, target ID, headers, credentials or response bodies.

## Safety verification

Deterministic tests exercise growth/bounds, low samples, realistic sequential floor recovery, latency steps, synchronized long-latency batches, stale evidence after idle, neutral-heavy observation streams, error pressure, rate/worker-limited holds,429 generations, finite recovery pacing and detached bounded snapshots. Integration tests cover slow 429 Close, header-time Retry-After, pauses inside rate waits, stale 429 extensions, shrink with outstanding reservations, client versus caller timeouts, failed bodies, cancellation and many-worker completion after cooldown.

CLI regression tests cover all output modes, unknown counts, duplicate inputs, dedupe, partial results, strict/best-effort, progress, input/output failures, diagnostic routing and command isolation. Automatic scheduling tests cover flag-only operation, explicit per-layer overrides (including default values and HTTP zero), worker growth/draining, cancellation and bounded page ranges. Negative settings remain validation errors. No live niconico service is contacted by the tests or measurements.

## Historical evaluation scope

The recorded evaluation below predates automatic target/page scheduling. It used fixed 16 targets×8 page workers, enough to supply a 64-request ceiling. Positive `--http-concurrency` values then acted as adaptive HTTP ceilings; in the current interface, explicitly supplied positive values instead fix HTTP admission. These historical results therefore do not measure flag-only automatic scheduling or the current mixed-override behavior.

Each page had two synthetic IDs; known totals selected a fixed page range. This isolated HTTP scheduling and deliberately did not model a full 100-item production response. Cases included a stable capacity knee, capacity drop/recovery with 429, high latency, explicit rate limitation, low worker supply, unknown counts, short commands and a fresh-process short test helper. These sustained comparisons used sorted line output; other output modes were covered by functional regression tests rather than the full performance matrix. Every iteration checked the complete output ID multiset, applicable positive hard cap, worker-supply bound and server totals. Synthetic delay was intentional input, not sleep-based test ordering.

The historical matrix below compares fixed caps 4/8/16/32/48/64 and adaptive ceilings 32/64; its `adaptive32`/`adaptive64` labels retain the original names. Current benchmark cases `adaptive_http_ceiling32`, `adaptive_http_ceiling64`, and `adaptive_http_unlimited` inject synthetic constructor settings with fixed worker supply to isolate HTTP feedback; they do not represent explicit `--http-concurrency`, which now fixes HTTP admission. The `adaptive_concurrency` case exercises the public flag alone with automatic target/page/HTTP scheduling. Any later measurements must be reported separately. Fixed 4 is a conservative zero429 reference for a phase whose capacity drops to 4; a faster fixed run with hundreds of rejections is not automatically an acceptable load-matched baseline. Adaptive transient feedback can still produce 429s, so fewer 429s alone does not prove it is best.

Each final comparison uses five repeats. The command benchmark includes command construction, first-page discovery and final output; the separate fresh-process benchmark uses the test executable as a CLI helper, not a release binary. In-process runs can reuse the shared default HTTP transport; the helper process starts with a fresh client transport. Allocation numbers for that process benchmark cover the parent harness, not child RSS. Results do not establish real-service capacity.

The controller has one global latency reference. Payload-size changes, new connections or a lasting network change can resemble queueing pressure; the diagnostics can distinguish some causes, but the controller remains conservative. No per-endpoint model or universal optimum is implied.

## Local results and acceptance status (2026-10-05)

These are historical measurements from before the pressure-cutoff correction described above, support for unlimited adaptive mode, and automatic target/page scheduling. The measured adaptive policies all used the former positive-ceiling semantics; these results do not measure the current option behavior. They remain a baseline record, not measurements of the current controller and scheduling. In particular,429/drop behavior can change after the correction; new research comparisons identify the corrected control separately. The earlier delivered artifact is superseded for use because of that known429 edge case, while its bytes/results are retained for comparison.

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

The controller and automatic worker scheduling are implemented. Broad performance acceptance is still open, and the historical matrix does not establish performance of the expanded option. Improving cold-start convergence without causing a larger initial load burst needs a separate measured design decision; do not silently add an initial-concurrency flag, change the initial 8 or change defaults. Live-service load tests and publication/merge are separate authorization steps.

### Reproduction

Run the historical revision above to reproduce the recorded matrix and its former adaptive-ceiling semantics. On the current revision, the full matrix includes both the synthetic HTTP feedback cases and the public `adaptive_concurrency` case; results require separate labels.

```sh
go test ./cmd -run '^$' -bench '^BenchmarkAdaptiveHTTPCommand/.*/.*/metrics=false$' -benchmem -benchtime=1x -count=5 -timeout=15m
go test ./cmd -run '^$' -bench '^BenchmarkAdaptiveHTTPCommand/(stable_capacity|capacity_drop_recovery_429|rate_bound|low_worker_supply)/adaptive_http_ceiling32/metrics=true$' -benchmem -benchtime=1x -count=5
go test ./cmd -run '^$' -bench '^BenchmarkAdaptiveHTTPCommand/(stable_capacity|capacity_drop_recovery_429|rate_bound|low_worker_supply)/adaptive_concurrency/metrics=true$' -benchmem -benchtime=1x -count=5
```

Sustained comparisons take several minutes. Preserve all repeats and ID/load checks when modifying parameters. Raw final matrices, telemetry, coefficient comparisons and the intentionally interrupted pre-neutral-fix pass are retained in the delivery for auditability; that partial pass is not part of the final statistics.
