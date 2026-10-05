# go-nico-list

Command line tool to fetch video IDs from niconico user pages and mylists.

[Japanese README](docs/README.ja.md)

## Overview
Fetches video IDs from one or more `nicovideo.jp/user/<id>` or `nicovideo.jp/mylist/<id>` pages, filters them by comment count and date range, sorts the results, and prints them to stdout.

## Install

### Go install
```bash
go install github.com/sh4869221b/go-nico-list@latest
```

### Prebuilt binaries
Prebuilt binaries are available on the GitHub Releases page.

## Usage

```bash
go-nico-list [nicovideo.jp/user/<id>|nicovideo.jp/mylist/<id>...] [flags]
```

Examples:

```bash
go-nico-list nicovideo.jp/user/12345
go-nico-list https://www.nicovideo.jp/user/12345/video --url
go-nico-list nicovideo.jp/user/1 nicovideo.jp/mylist/847130 --concurrency 10
go-nico-list --input-file users.txt
cat users.txt | go-nico-list --stdin
```

## Output
- One video ID per line (example: `sm123`).
- With `--url`, each line is prefixed with `https://www.nicovideo.jp/watch/`.
- With `--json`, stdout is a single JSON object (line output is disabled).

## Exit status
- `0`: no fetch errors (invalid inputs are skipped; may produce no output).
- non-zero: at least one fetch failed (any successfully retrieved IDs are still printed).
- Validation errors (for example, `--concurrency < 1`) return non-zero.
- `context.Canceled` / `context.DeadlineExceeded` during fetch is treated as a successful empty result.

## Flags

| Flag | Description | Default |
| --- | --- | --- |
| `-c, --comment` | lower comment limit number | `0` |
| `-a, --dateafter` | date `YYYYMMDD` after | `10000101` |
| `-b, --datebefore` | date `YYYYMMDD` before | `99991231` |
| `-u, --url` | output id add url | `false` |
| `-n, --concurrency` | number of concurrent requests | `3` |
| `--page-concurrency` | number of concurrent page requests per target | `1` |
| `--http-concurrency` | maximum command-wide HTTP requests (0 means no additional maximum) | `0` |
| `--adaptive-http-concurrency` | adapt HTTP concurrency with an optional `--http-concurrency` maximum | `false` |
| `--http-metrics` | log aggregate HTTP performance metrics | `false` |
| `--rate-limit` | maximum requests per second (0 disables) | `0` |
| `--min-interval` | minimum interval between requests | `0s` |
| `--timeout` | HTTP client timeout | `10s` |
| `--retries` | number of retries for requests | `10` |
| `--input-file` | read inputs from file (newline-separated) | `""` |
| `--stdin` | read inputs from stdin (newline-separated) | `false` |
| `--logfile` | log output file path | `""` |
| `--progress` | force enable progress output | `false` |
| `--no-progress` | disable progress output | `false` |
| `--strict` | return non-zero if any input is invalid | `false` |
| `--best-effort` | always exit 0 while logging fetch errors | `false` |
| `--dedupe` | remove duplicate output IDs before output | `false` |
| `--no-sort` | skip sorting output IDs for faster output | `false` |
| `--json` | emit JSON output to stdout | `false` |

Notes:
- Inputs can be provided via arguments, `--input-file`, and `--stdin` (newline-separated).
- Input lines from `--input-file` and `--stdin` are limited to 1 MiB per line; longer lines fail with an input read error.
- Each input must contain `nicovideo.jp/user/<id>` or `nicovideo.jp/mylist/<id>` (scheme optional). Plain digits or paths without the domain are treated as invalid inputs and skipped.
- Results are written to stdout; progress and logs are written to stderr. Use `--logfile` to redirect logs to a file.
- Setting `concurrency`, `page-concurrency`, or `retries` to a value less than 1, or `timeout` to a value less than or equal to 0, will cause a runtime error.
- `--dateafter` must be on or before `--datebefore`; inverted ranges return a validation error.
- Each target is fetched without a user-configurable page or video limit until the API's natural termination condition.
- When the API reports `totalCount`, page 1 defines a bounded page range and `--page-concurrency` controls concurrent requests for the remaining pages. When `totalCount` is unavailable, pages are fetched sequentially until an empty page or HTTP 404.
- There are no replacement fetch limits. Large targets can therefore take longer, issue more requests, and produce more output; global rate limiting, retry handling, and context cancellation still apply.
- Responses with HTTP status other than 200/404 after retries are treated as fetch errors.
- HTTP 200 responses with `meta.status != 200` are logged as warnings but still processed.
- `--page-concurrency` controls concurrent page requests inside each input target only when the API reports `totalCount`. Without an additional HTTP cap, the maximum in-flight request count is roughly `--concurrency * --page-concurrency` in that bounded-page path.
- Rate limiting applies globally to all requests (including retries). HTTP 429 `Retry-After` is honored when present. Use `--rate-limit` or `--min-interval` with high concurrency to reduce API load.
- Progress is auto-disabled when stderr is not a TTY. Use `--progress` to force-enable or `--no-progress` to disable (takes precedence).
- A run summary is printed to stderr after processing (even when the exit code is non-zero).
- `--strict` makes invalid inputs return a non-zero exit code while still outputting valid results.
- `--best-effort` forces exit code 0 even when fetch errors occur (errors are still logged).
- Normal line output sorts IDs by numeric video ID unless `--no-sort` is set.
- `--dedupe` removes duplicate video IDs before sorting/output. With `--no-sort`, the first occurrence that reaches the writer is kept.
- `--no-sort` is an unordered fast mode for line output: input target order, page order, and API item order are not guaranteed. Results are written as soon as target fetches finish.
- `--json` emits a single JSON object to stdout. `--url` does not affect JSON `items`, and the summary still prints to stderr.
- In JSON output, `targets` include `type` (`user` or `mylist`) and `id`, sorted by type and numeric id in ascending order.

## HTTP performance diagnostics

```bash
# Measure the existing settings without adding an HTTP cap.
go-nico-list --input-file users.txt --http-metrics

# Bound HTTP work across all targets/pages/retries and save diagnostics in the log.
go-nico-list --input-file users.txt -n 8 --page-concurrency 4 \
  --http-concurrency 8 --http-metrics --logfile run.jsonl
```

`--http-concurrency 0` means no additional maximum; negative values are invalid. Without adaptive mode, zero preserves the existing uncapped behavior. A positive cap includes response body reading and closing. Retry waits release their slots, and existing rate limits still apply. Without `--adaptive-http-concurrency`, this is a fixed cap. It does not increase the supply of target/page workers.

`--http-metrics` adds one `http_metrics` structured log event at the end of an initialized execution. It contains attempts, dispatched retries, status/error counts, connection reuse, in-flight/reservation peaks, elapsed time and wait/network/body/decode timings. It does not change stdout, JSON results, the summary or exit status. The metrics event contains no request URLs, target IDs or response content; ordinary existing error logs are unchanged.

Timing percentiles use the most recent 1024 samples per metric, with sample counts; p95 requires at least 20 samples and p99 at least 100. Trace intervals overlap and must not be added to obtain elapsed time. See [measurement definitions and reproducible local benchmarks](docs/HTTP_METRICS.md).

## Experimental adaptive HTTP concurrency

```bash
go-nico-list --input-file users.txt -n 16 --page-concurrency 8 \
  --adaptive-http-concurrency --http-metrics
```

Adaptive mode works without a configured ceiling: omit `--http-concurrency` or set it to `0`. It starts at 8 and retains a positive, automatically adjusted admission limit. A positive `--http-concurrency` remains an optional hard ceiling and lowers the initial limit if it is below 8. The controller grows gradually with healthy, sufficiently supplied work and decreases after sustained latency or retry pressure. It never changes target/page concurrency or your rate/min-interval settings. Low worker supply, explicit rate limits and short runs can leave the limit unchanged; automatic tuning is not a promise of faster completion.

Removing the configured ceiling adds no hidden replacement cap or memory/file-descriptor resource guard. Available workers still bound actual work; unlimited adaptive mode does not guarantee prevention of out-of-memory or file-descriptor exhaustion.

A 429 in adaptive mode pauses new requests command-wide for the applicable `Retry-After`/retry deadline, lets existing requests finish, and resumes with paced recovery. Fixed mode keeps the existing per-request retry policy. Output, filtering, partial results and exit behavior stay unchanged.

The controller runs even without `--http-metrics`. Only that diagnostics flag adds adaptive state and decision reasons to the final `http_metrics` event; adaptive mode alone adds no diagnostic output. State is per command, with no background probes or persisted learning. This feature is experimental and opt-in; local synthetic comparisons do not establish the best setting for the live API. See [policy, evaluation and limitations](docs/ADAPTIVE_HTTP.md).

## Design
This project separates the CLI layer from the domain logic so each part is easier to test and maintain.

- `main.go`: resolves the version and bootstraps the CLI with a cancellation-aware context.
- `cmd/`: Cobra command definitions, flags, and input/output handling (stdout/stderr separation).
- `internal/niconico/`: core domain logic (fetching video lists, retries, sorting) and API response types.

### Flow
1. The CLI parses flags and user/mylist IDs.
2. The command layer calls `internal/niconico` to fetch and filter video IDs from each target.
3. Results are sorted and printed; progress is written to stderr.

## CI
GitHub Actions runs on pull requests to `master` and pushes to `master`, and enforces:
- repository ruleset protection on `master` (PR-only updates with required `go-ci` status checks)
- generated file checks (`go mod tidy`, `go generate ./...`, `git diff --exit-code`)
- `gofmt` (format + diff check)
- `go vet ./...`
- `golangci-lint run ./...`
- `go test -count=1 ./...`
- `go test -race -count=1 ./...`
- GitHub Actions references are pinned to commit SHAs in workflow files.

## Test layers
- Integration-style command wiring tests: `cmd/root_test.go` (`httptest` + stdout/stderr/exit-code checks).
- Contract tests: `internal/niconico/nico_data_contract_test.go` (fixture decode from `internal/niconico/testdata/`).
- Fuzz tests: `internal/niconico/fuzz_test.go`, `cmd/root_fuzz_test.go` (sorting/JSON/url-parse panic safety).
- E2E tests (opt-in): `internal/niconico/e2e_test.go` with `-tags=e2e`.
- Benchmarks (opt-in): `cmd/root_benchmark_test.go`, `internal/niconico/benchmark_test.go`.

Opt-in commands:

```bash
go test ./internal/niconico -run TestNicoDataContract -count=1
go test ./cmd -run=^$ -fuzz=FuzzParseInputTargetNoPanic -fuzztime=10s
go test ./cmd -run=^$ -fuzz=FuzzSubmatchByNameNoPanic -fuzztime=10s
go test ./internal/niconico -run=^$ -fuzz=FuzzNiconicoSortNoPanic -fuzztime=10s
go test ./internal/niconico -run=^$ -fuzz=FuzzNicoDataUnmarshalNoPanic -fuzztime=10s
GO_NICO_LIST_E2E_USER_ID=<user-id> go test -tags=e2e ./internal/niconico -run TestGetVideoListE2E -count=1
go test ./cmd -run=^$ -bench='BenchmarkRunRootCmdLargeFanIn(LineOutput|JSONOutput)' -benchmem -count=5
go test ./internal/niconico -run=^$ -bench=BenchmarkNiconicoSort -benchmem -count=1
```

Latest local sort/no-sort benchmark sample:

Environment: linux/amd64, AMD Ryzen 7 7700X 8-Core Processor, `go test ./cmd -run=^$ -bench='BenchmarkRunRootCmdLargeFanIn(LineOutput|JSONOutput)' -benchmem -count=5`.
Numbers below use median `ns/op`; lower is better.

| Benchmark | Sort | No sort | Change |
| --- | ---: | ---: | ---: |
| Line output large fan-in | 632,022 ns/op | 601,919 ns/op | 4.8% faster |
| JSON output large fan-in | 653,449 ns/op | 618,076 ns/op | 5.4% faster |

## Contributing
See `CONTRIBUTING.md`.

## Release
Releases are published by tagging a version and pushing it to GitHub.

1. Create a tag like `vX.Y.Z`.
2. Push the tag to GitHub.
3. GitHub Actions runs the release workflow (verifies `go mod tidy`/`go generate ./...` and runs gofmt/go vet/golangci-lint/go test/go test -race).
4. GoReleaser generates `THIRD_PARTY_NOTICES.md`, publishes the GitHub Release, and uploads artifacts.
5. Close the milestone after the release workflow succeeds.

Notes:
- When a versioned milestone is complete, release using the same version number.
- Release tags (`vX.Y.Z`) are governed by a repository tag ruleset.
