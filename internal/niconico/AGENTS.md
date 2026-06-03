# NICONICO DOMAIN GUIDANCE

Scope: files under `internal/niconico/`.

## Boundary
- Keep this package focused on niconico domain behavior: API request construction, response decoding, pagination, filtering, retry, rate limiting, and raw video ID sorting.
- Do not add Cobra, terminal, stdout/stderr, progress, CLI flag parsing, or exit-code behavior here; those belong in `cmd/`.
- Public entry points are `GetVideoList`, `GetMylistVideoList`, `NiconicoSort`, `NewRateLimiter`, and `RateLimiter.Wait`.

## Key Files
- `client.go`: user/mylist fetch entry points, shared collection loop, retry/backoff, `Retry-After`, rate limiting, response body handling, and sorting.
- `nico_data.go`: decode target for user-video API responses and fixture contract tests.
- `client_test.go`: low-level behavior coverage for fetch, retry, backoff, cancellation, timeout, 404 handling, and rate limiting.
- `nico_data_contract_test.go`: fixture-backed API shape contract.
- `fuzz_test.go`: panic-safety coverage for sorting and JSON decode boundaries.
- `e2e_test.go`: opt-in live API coverage behind the `e2e` build tag and `GO_NICO_LIST_E2E_USER_ID`.
- `benchmark_test.go`: `NiconicoSort` performance baseline.

## Fetch Invariants
- User endpoint format is `<baseURL>/users/<userID>/videos?pageSize=100&page=<n>`.
- Mylist endpoint format is `<baseURL>/mylists/<mylistID>?pageSize=100&page=<n>`.
- Requests must set `X-Frontend-Id: 6` and `Accept: */*`.
- Pagination starts at page 1 and stops on 404, empty items, `maxPages`, or `maxVideos`.
- `maxPages` and `maxVideos` are best-effort caps and should not produce errors by themselves.
- Context cancellation or deadline during fetch is treated by collection as a clean empty result.
- HTTP 200 with non-200 `meta.status` logs a warning and continues as a successful response.
- Returned IDs stay raw `sm*` values; formatting is a `cmd` concern.

## Retry And Body Handling
- Retry any HTTP status other than 200 or 404.
- Honor HTTP 429 `Retry-After`; wait for the longer of `Retry-After` and computed backoff/interval delay.
- Apply global rate limiting before every request attempt, including retries.
- Exponential backoff starts at 100ms and is capped at 30s.
- Do not sleep after the final attempt.
- Always close response bodies on discarded responses and early status-handling paths, including 404 and retry statuses.
- Preserve context-aware sleeps so cancellation interrupts backoff and rate-limit waits.

## Filtering And Sorting
- Include only items with `comment > commentCount`.
- Include dates from `dateafter` through `datebefore` inclusive; the current implementation uses an exclusive `beforeDate.AddDate(0, 0, 1)` upper bound.
- `NiconicoSort` sorts by the numeric part of video IDs using a total-order fallback for malformed or very large values.
- Do not format, dedupe, or prefix IDs in this package.

## Testing Patterns
- Use `httptest.Server` for network behavior and keep pagination finite.
- Add fixture fields intentionally. If live API shape changes, update `testdata/` and `nico_data_contract_test.go` together.
- For retry, rate-limit, and backoff tests, use existing injectable time/sleep hooks instead of wall-clock waits.
- Fuzz tests should protect against panics, not encode detailed business assertions.
- E2E tests are opt-in only and must not be required for normal CI.
