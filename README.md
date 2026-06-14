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
- With `--tab`, each line is prefixed with tabs.
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
| `-t, --tab` | id tab separated flag | `false` |
| `-u, --url` | output id add url | `false` |
| `-n, --concurrency` | number of concurrent requests | `3` |
| `--page-concurrency` | number of concurrent page requests per target | `1` |
| `--rate-limit` | maximum requests per second (0 disables) | `0` |
| `--min-interval` | minimum interval between requests | `0s` |
| `--max-pages` | maximum number of pages to fetch | `0` |
| `--max-videos` | maximum number of filtered IDs to collect | `0` |
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
- `--max-pages` and `--max-videos` are safety caps; `0` disables them.
- When a safety cap is hit, fetching stops early and returns best-effort results without error.
- Responses with HTTP status other than 200/404 after retries are treated as fetch errors.
- HTTP 200 responses with `meta.status != 200` are logged as warnings but still processed.
- `--page-concurrency` controls concurrent page requests inside each input target. The maximum in-flight request count is roughly `--concurrency * --page-concurrency`.
- Rate limiting applies globally to all requests (including retries). HTTP 429 `Retry-After` is honored when present. Use `--rate-limit` or `--min-interval` with high concurrency to reduce API load.
- Progress is auto-disabled when stderr is not a TTY. Use `--progress` to force-enable or `--no-progress` to disable (takes precedence).
- A run summary is printed to stderr after processing (even when the exit code is non-zero).
- `--strict` makes invalid inputs return a non-zero exit code while still outputting valid results.
- `--best-effort` forces exit code 0 even when fetch errors occur (errors are still logged).
- `--dedupe` removes duplicate video IDs before sorting/output.
- `--no-sort` skips sorting the flattened output ID list for speed; after optional dedupe, IDs remain in input target order, preserving each target's fetched order.
- `--json` emits a single JSON object to stdout. `--tab`/`--url` do not affect JSON `items`, and the summary still prints to stderr.
- In JSON output, `targets` include `type` (`user` or `mylist`) and `id`, sorted by type and numeric id in ascending order.

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
