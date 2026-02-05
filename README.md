# go-nico-list

Command line tool to fetch video IDs from a niconico user page.

[Japanese README](docs/README.ja.md)

## Overview
Fetches video IDs from one or more `nicovideo.jp/user/<id>` pages, filters them by comment count and date range, sorts the results, and prints them to stdout.

## Install

### Go install
```bash
go install github.com/sh4869221b/go-nico-list@latest
```

### Prebuilt binaries
Prebuilt binaries are available on the GitHub Releases page.

## Usage

```bash
go-nico-list [nicovideo.jp/user/<id>...] [flags]
```

Examples:

```bash
go-nico-list nicovideo.jp/user/12345
go-nico-list https://www.nicovideo.jp/user/12345/video --url
go-nico-list nicovideo.jp/user/1 nicovideo.jp/user/2 --concurrency 10
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
| `--dedupe` | remove duplicate output IDs before sorting | `false` |
| `--json` | emit JSON output to stdout | `false` |

Notes:
- Inputs can be provided via arguments, `--input-file`, and `--stdin` (newline-separated).
- Each input must contain `nicovideo.jp/user/<id>` (scheme optional). Plain digits or `user/<id>` without the domain are treated as invalid inputs and skipped.
- Results are written to stdout; progress and logs are written to stderr. Use `--logfile` to redirect logs to a file.
- Setting `concurrency` or `retries` to a value less than 1 will cause a runtime error.
- `--max-pages` and `--max-videos` are safety caps; `0` disables them.
- When a safety cap is hit, fetching stops early and returns best-effort results without error.
- Responses with HTTP status other than 200/404 after retries are treated as fetch errors.
- HTTP 200 responses with `meta.status != 200` are logged as warnings but still processed.
- Rate limiting applies globally to all requests (including retries). HTTP 429 `Retry-After` is honored when present.
- Progress is auto-disabled when stderr is not a TTY. Use `--progress` to force-enable or `--no-progress` to disable (takes precedence).
- A run summary is printed to stderr after processing (even when the exit code is non-zero).
- `--strict` makes invalid inputs return a non-zero exit code while still outputting valid results.
- `--best-effort` forces exit code 0 even when fetch errors occur (errors are still logged).
- `--dedupe` removes duplicate video IDs before sorting/output.
- `--json` emits a single JSON object to stdout. `--tab`/`--url` do not affect JSON `items`, and the summary still prints to stderr.

## Design
This project separates the CLI layer from the domain logic so each part is easier to test and maintain.

- `main.go`: resolves the version and bootstraps the CLI with a cancellation-aware context.
- `cmd/`: Cobra command definitions, flags, and input/output handling (stdout/stderr separation).
- `internal/niconico/`: core domain logic (fetching video lists, retries, sorting) and API response types.

### Flow
1. The CLI parses flags and user IDs.
2. The command layer calls `internal/niconico` to fetch and filter video IDs.
3. Results are sorted and printed; progress is written to stderr.

## CI
GitHub Actions runs on every push and pull request (all branches) and enforces:
- `gofmt` (format + diff check)
- `go vet ./...`
- `go test -count=1 ./...`
- `go test -race -count=1 ./...`

## Contributing
See `CONTRIBUTING.md`.

## Release
Releases are published by tagging a version and pushing it to GitHub.

1. Create a tag like `vX.Y.Z`.
2. Push the tag to GitHub.
3. GitHub Actions runs the release workflow (verifies `go mod tidy`/`go generate ./...`, runs gofmt/go vet/go test/go test -race, and checks third-party notices).
4. GoReleaser publishes the GitHub Release and uploads artifacts.
5. Close the milestone after the release workflow succeeds.

Notes:
- When a versioned milestone is complete, release using the same version number.
