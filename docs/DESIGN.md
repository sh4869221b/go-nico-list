# Design: go-nico-list

This document summarizes the behavior required for a coding agent to reproduce the same program.

## Purpose and Scope
- Purpose: Provide a CLI that fetches video IDs from a niconico userID, filters them, and outputs the list.
- In scope: fetching, filtering, sorting, output, error handling, tests.
- Out of scope: UI, persistence, auth, config files, i18n.

## Architecture

```
main.go
  └─ cmd.ExecuteContext(ctx)
        └─ cmd/root.go (CLI: flags/IO/validation)
              └─ internal/niconico (domain: fetch/retry/sort)
```

### Refactoring guardrails
- Behavior and output must remain identical during refactors.
- Refactors only change internal structure (function boundaries, helpers, file splits).
- CLI/user-facing flags, outputs, logs, exit codes, and progress behavior are unchanged.
- Domain API behavior (filters, pagination, retry/ratelimit semantics) is unchanged.

### Responsibilities
- `main.go`:
  - Resolve version (build info / ldflags).
  - Create a cancelable context and pass it to `cmd.ExecuteContext`.
- `cmd/`:
  - Cobra commands/flags.
  - Input validation (`concurrency`, `retries`, dates).
  - Progress to stderr, results to stdout.
  - Call into `internal/niconico` and aggregate results.
- `internal/niconico/`:
  - API response types (`nico_data.go`).
  - Domain logic for fetch/retry/sort (`client.go`).

## Documentation
- `README.md` remains at the repository root.
- `docs/README.ja.md` holds the Japanese README.
- `docs/DESIGN.md` is the canonical design overview.
- `CONTRIBUTING.md` remains at the repository root.

## Input and Output

### Input
- Arguments: `nicovideo.jp/user/<id>` URL (scheme optional).
  - Regex: `((http(s)?://)?(www\.)?)nicovideo\.jp/user/(?P<userID>\d{1,9})(/video)?`
  - `userID` is **1–9 digits**.
  - Regex is **partial match** (valid if the input contains a match).
  - If multiple matches exist, use the **first `nicovideo.jp/user/<id>`**.
  - `user/<id>` without the domain and plain digits are treated as invalid inputs.
- Flags:
  - `--comment` (default `0`): minimum comment count.
  - `--dateafter` (default `10000101`) / `--datebefore` (default `99991231`): `YYYYMMDD`.
    - Parsed by `time.Parse("20060102", ...)` (UTC).
  - `--tab` (default `false`), `--url` (default `false`): output formatting.
  - `--concurrency` (default `3`): concurrent requests.
  - `--rate-limit` (default `0`): maximum requests per second (float; `0` disables).
  - `--min-interval` (default `0s`): minimum interval between requests (`0` disables).
  - `--max-pages` (default `0`): maximum number of pages to fetch (`0` disables).
  - `--max-videos` (default `0`): maximum number of filtered IDs to collect (`0` disables).
  - `--timeout` (default `10s`): HTTP client timeout.
  - `--retries` (default `10`): retry count.
  - `--logfile` (default `""`): log file path (empty = stderr, set = file output).

### Output
- stdout: list of video IDs (with prefixes based on `tab`/`url`).
  - `tabStr` is **9 tabs**, `urlStr` is `https://www.nicovideo.jp/watch/`.
  - Prefix order is **tab → url**.
  - Lines are joined by `"\n"` and printed with a trailing newline (`fmt.Fprintln`).
  - If no IDs are retrieved, stdout prints **nothing**.
  - Duplicate IDs are **not deduplicated**.
- stderr: progress bar (log output switches to file when `--logfile` is set).
- Invalid userIDs only produce a warning and do not fail; valid IDs (if present) still output results.

## CLI additions (v0.22.0)
- Input sources:
  - `--input-file <path>` and/or `--stdin` read newline-separated inputs.
- Validation and exit codes:
  - `--strict` returns non-zero if any invalid input is present (even if other inputs succeed).
  - If `--strict` and `--best-effort` are both set, **`--strict` takes precedence**.
  - `--best-effort` exits 0 even when fetch errors occur, while still logging errors.
- Output behavior:
  - `--dedupe` removes duplicate IDs **before** sorting/output.
  - Run summary is emitted to stderr after processing (even on non-zero exit codes).
    - Format: `summary inputs=<n> valid=<n> invalid=<n> fetch_ok=<n> fetch_err=<n> output_count=<n>`.
    - `output_count` uses the **deduped** count.
  - `--json` emits a minimal schema to stdout (single JSON object; line output is disabled).
    - Summary still prints to stderr.
    - Schema:
      - `inputs`: `{ "total": n, "valid": n, "invalid": n }`
      - `invalid`: list of invalid input strings
      - `users`: list of `{ "user_id": "<id>", "items": ["sm1"], "error": "" }`
      - `errors`: list of fetch error messages (order is nondeterministic)
      - `output_count`: count of `items` after dedupe (if enabled)
      - `items`: flattened list of IDs (raw `sm*` IDs; `--url`/`--tab` do not affect JSON)
- Progress:
  - Progress output is auto-disabled on non-TTY stderr.
  - `--progress` forces progress on even when stderr is not a TTY.
  - `--no-progress` always disables progress output and takes precedence when both flags are set.

## Flow
1. `cmd/root.go` extracts userIDs using the regex.
2. For each userID, a goroutine calls `internal/niconico.GetVideoList`.
3. Aggregate and sort IDs, then print to stdout.

## Errors and Exit Codes
- Validation errors (`concurrency`/`retries`/date format): **non-zero exit**.
  - Print **only the error message** to stderr; no usage output.
- If any fetch errors occur: **log all errors** to the log destination and still output any retrieved IDs, exit **non-zero**.
  - Errors include HTTP/IO/JSON failures from fetch operations.
  - Log order is **nondeterministic** due to concurrency.
  - The command returns **one** fetch error (the first observed); which error is returned is **nondeterministic** due to concurrency.
  - Cobra prints that error message to stderr even when `--logfile` is set.
- If all fetches succeed: output results to stdout (exit 0).
  - `context.Canceled/DeadlineExceeded` during fetch is treated as a successful empty result; the command may exit 0 with partial or no output.

## Core Logic

### Fetch (`internal/niconico.GetVideoList`)
- Endpoint:
  - `https://nvapi.nicovideo.jp/v3/users/<userID>/videos?pageSize=100&page=<n>`
- Request headers:
  - `X-Frontend-Id: 6`
  - `Accept: */*`
- Pagination: fetch until API page end.
- `max-pages` stops after the given number of pages (best-effort, no error).
- `max-videos` stops after collecting the given number of filtered IDs (best-effort, no error).
- Filters:
  - `comment > commentCount`
  - `registeredAt` >= `dateafter`
  - `registeredAt` <= `datebefore` (inclusive via an exclusive upper bound: `registeredAt < beforeDate.AddDate(0,0,1)`)
- `StatusNotFound` stops fetching.
- `context.Canceled/DeadlineExceeded` returns empty result without error.
- HTTP 200 responses with `meta.status != 200` are logged as warnings and treated as successful responses.
- On errors during fetch, return partial results plus error (caller logs and continues).

### Retry (`internal/niconico.retriesRequest`)
- Retry on anything other than HTTP 200/404.
- When retries are exhausted and the final status is not 200/404, return an error and do not return a closed body.
- Exponential backoff starting at `100ms`, max `30s`.
- Skip backoff sleep after the final attempt; backoff sleep is canceled by `ctx.Done()`.
- Apply global rate limiting before each request (including retries).
- When both `--rate-limit` and `--min-interval` are set, use the stricter limit (max of `min-interval` and `1/rate-limit`).
- On HTTP 429 with `Retry-After`, wait for the longer of `Retry-After` and the computed backoff/interval delay.

### Sort (`internal/niconico.NiconicoSort`)
- Remove a fixed prefix length (`sm` + optional tab/url) and compare with `"%08s"` padding.
- `tab`/`url` prefixes are ignored using `tabStr`/`urlStr` lengths.

## Concurrency
- `concurrency` limits goroutines via a semaphore.
- Aggregated `[]string` is guarded by a mutex.
- Progress bar updates are serialized for race safety.

## Logging
- JSON logger via `slog`.
- Log all fetch errors; duplicates are **allowed**.
- With `--logfile`, switch logging destination to file:
  - `os.OpenFile` with `O_APPEND|O_CREATE|O_WRONLY`, permissions `0644`.
  - Failure to open the file is a non-zero error; the error is printed to stderr by Cobra (no logger output).

## Version
- `main.go` resolves `Version` and passes it to `cmd.Version`.
- `ExecuteContext` sets `rootCmd.Version` before execution.

## Tests
- CLI tests: `cmd/root_test.go` (validation, output, progress, logfile).
- Domain tests: `internal/niconico/client_test.go` (fetch/retry/sort).

## Release process (CI)
- Release is triggered by pushing a `vX.Y.Z` tag to GitHub.
- The release workflow verifies generated files (`go mod tidy`, `go generate ./...`).
- The release workflow runs quality gates (gofmt, go vet, go test, go test -race).
- `THIRD_PARTY_NOTICES.md` is kept in sync via `scripts/gen-third-party-notices.sh` and verified in CI.
- GoReleaser builds and publishes artifacts for supported OSes.

## Branch strategy
- Use `master` as the only long-lived branch.
- Create short-lived branches (e.g. `feature/*`) and merge via **squash PR** into `master`.
- Releases are tagged from `master` (`vX.Y.Z`).

## Change Guidelines
- Do not mix CLI and domain logic.
- Keep `cmd` limited to IO and parameter handling.
- Keep tests aligned with external API behavior changes.
