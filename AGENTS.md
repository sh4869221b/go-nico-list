# PROJECT KNOWLEDGE BASE

**Generated from init-deep discovery:** 2026-06-03
**Discovery base commit:** 4e6c57c

## OVERVIEW
`go-nico-list` is a Go 1.26.1 CLI that fetches niconico user/mylist video IDs, filters and sorts them, and writes machine-readable results to stdout while keeping progress, logs, summaries, and errors on stderr.

## STRUCTURE
```
go-nico-list/
|-- main.go                  # version resolution and cancellation-aware bootstrap
|-- cmd/                     # Cobra flags, IO, validation, output, command tests
|-- internal/niconico/        # API response types, fetch/retry/rate-limit/sort domain logic
|-- docs/DESIGN.md           # canonical behavior and responsibility map
|-- docs/README.ja.md        # Japanese README
|-- scripts/                 # generated third-party notice tooling
|-- .github/workflows/       # CI, release, latest-deps, generated-file refresh
`-- .golangci.yml            # enabled linters
```

## WHERE TO LOOK
| Task | Location | Notes |
| --- | --- | --- |
| CLI entrypoint | `main.go`, `cmd/root_flags.go` | `main` only wires version/context into `cmd.ExecuteContext`. |
| Flags and defaults | `cmd/root_config.go` | `RootConfig`, `RootDeps`, `NewRootCommand`, normalization. |
| Runtime flow | `cmd/root_run.go` | validation, input stream, goroutines, summary, output, error selection. |
| Input parsing | `cmd/input_target.go`, `cmd/root_io.go` | URL regexes, stdin/input-file scanner, progress gating. |
| JSON output | `cmd/root_json.go` | target ordering, flattened items, `--tab`/`--url` exclusion from JSON. |
| API/domain logic | `internal/niconico/client.go` | fetch, pagination, retry, rate limit, response evaluation, sorting. |
| API schema | `internal/niconico/nico_data.go` | `NicoData` decode target for fixtures and live API responses. |
| Current behavior spec | `docs/DESIGN.md` | Update before user-facing/design-boundary implementation and get explicit OK. |
| User docs | `README.md`, `docs/README.ja.md` | Update when user-facing behavior changes. |
| PR shape | `.github/PULL_REQUEST_TEMPLATE.md` | Keep all sections; use `gh pr create --body-file`. |
| CI parity | `.github/workflows/ci.yml` | Generated drift, notices, gofmt, vet, lint, test, race. |

## CODE MAP
| Symbol | Type | Location | Role |
| --- | --- | --- | --- |
| `main` | function | `main.go` | Resolve version and start the Cobra command with signal cancellation. |
| `RootConfig` | struct | `cmd/root_config.go` | All flag values and runtime defaults. |
| `RootDeps` | struct | `cmd/root_config.go` | Injectable IO/logger/progress/file dependencies for tests. |
| `NewRootCommand` | function | `cmd/root_config.go` | Builds the root Cobra command and attaches `RunE`. |
| `runRootCmdWithConfig` | function | `cmd/root_run.go` | Main command runner and aggregation boundary. |
| `streamInputsWithConfig` | function | `cmd/root_io.go` | Merges args, `--input-file`, and `--stdin` into a target stream. |
| `buildJSONOutput` | function | `cmd/root_json.go` | Builds the stdout JSON payload. |
| `parseInputTarget` | function | `cmd/input_target.go` | Extracts first matching niconico user/mylist target. |
| `GetVideoList` | function | `internal/niconico/client.go` | Fetches filtered user video IDs. |
| `GetMylistVideoList` | function | `internal/niconico/client.go` | Fetches filtered mylist video IDs. |
| `collectVideoList` | function | `internal/niconico/client.go` | Shared pagination/filter/cap loop. |
| `retriesRequest` | function | `internal/niconico/client.go` | HTTP retry, rate-limit, timeout, and retry-after handling. |
| `NiconicoSort` | function | `internal/niconico/client.go` | Sorts raw `sm*` IDs by numeric part. |
| `RateLimiter.Wait` | method | `internal/niconico/client.go` | Global request pacing across concurrent fetches and retries. |

## DEVELOPMENT RULES
- Before implementation, create or confirm a GitHub Issue. Keep one issue scoped to one fix or one feature.
- Use `branch-helper` for repository-modifying tasks unless explicitly told otherwise.
- Work on a short-lived branch and merge through a PR using squash merge. Never push implementation commits directly to `master`.
- Create the PR right after the initial implementation commit so the fix log can be maintained in the PR body.
- PR bodies must use `.github/PULL_REQUEST_TEMPLATE.md`, keep all sections, and include an auto-close keyword such as `Closes #123`.
- When editing PR bodies with `gh`, rewrite the full body with `-F <file>`.
- Maintain the PR fix log after each correction pass.
- For implementation work, follow Implement -> Test -> Review. If review findings need confirmation, ask numbered questions first; after answers, repeat Fix -> Test -> Review until there are no findings.
- After addressing review feedback, ask Codex for a re-review in chat.
- Avoid interactive editors in automated merges. Use non-interactive `gh pr merge --squash` patterns and set `GIT_EDITOR` when needed.
- Before merging, wait for all CI checks to complete with `gh pr checks --watch` unless explicitly told to skip.
- When handling multiple PRs, create all PRs first, then confirm CI and merge together at the end.

## CHANGE RULES
- Keep changes atomic and limited to the issue scope.
- Do not mix CLI and domain logic. `cmd/` handles flags, IO, validation, output, concurrency, and exit behavior; `internal/niconico/` handles API fetch/retry/sort/types.
- Refactors must preserve flags, stdout/stderr, log messages, summaries, exit codes, progress behavior, pagination, retry, and rate-limit semantics unless the issue explicitly changes them.
- Before user-facing behavior or responsibility-boundary changes, update `docs/DESIGN.md` as needed and get explicit OK before implementation.
- For internal-only changes, such as CI, docs, or process-only updates, `docs/DESIGN.md` changes are not required.
- If user-facing behavior changes, update `README.md` and `docs/README.ja.md` as appropriate.
- Keep undecided proposals out of `docs/DESIGN.md`; record backlog ideas in GitHub Issues.
- Keep `README.md`, `docs/DESIGN.md`, and `AGENTS.md` in English.
- Keep `WORKLOG.md` local-only, English, and uncommitted.

## TESTING
- If Go files change, run `gofmt -w <files>`.
- After changes, run:
```bash
go vet ./...
go test ./...
go test -race ./...
```
- Before opening a PR, verify generated-file drift:
```bash
go mod tidy
go generate ./...
git diff --exit-code
```
- CI also runs:
```bash
golangci-lint run ./...
bash scripts/gen-third-party-notices.sh
git diff --exit-code -- THIRD_PARTY_NOTICES.md
```
- For local work, run `bash scripts/gen-third-party-notices.sh` when dependencies changed or when doing full PR parity verification.
- Optional targeted layers live in `CONTRIBUTING.md`: contract fixture decode, fuzz smoke, E2E with `GO_NICO_LIST_E2E_USER_ID`, and the sort benchmark.

## TEST PATTERNS
- Keep tests deterministic; avoid time-based ordering unless controlled with injectable time, pipes, contexts, or test servers.
- CLI tests are split by behavior under `cmd/root_*_test.go` and use helpers from `cmd/root_test_helpers_test.go`.
- Use `httptest.Server` for command/API integration tests and assert stdout, stderr, and returned errors separately.
- When mocking niconico pagination, terminate page > 1 with empty items or 404.
- Contract fixtures live under `internal/niconico/testdata/` and are decoded by `internal/niconico/nico_data_contract_test.go`.
- Fuzz tests cover parse, regex submatch, sort, and JSON unmarshal panic safety.
- E2E tests are opt-in behind the `e2e` build tag and `GO_NICO_LIST_E2E_USER_ID`.

## ANTI-PATTERNS
- Do not add runtime dependencies without prior approval.
- Do not commit `WORKLOG.md`, `.omo/`, `.codex/`, local caches, or continuation artifacts.
- Do not write progress, logs, summaries, or errors to stdout. Data output belongs on stdout; operational output belongs on stderr or the logfile.
- Do not assume fetch/log order is deterministic; concurrency makes first error and log order nondeterministic.
- Do not leave response bodies unclosed on early HTTP-status handling paths.
- Do not bypass `Retry-After`, global rate limiting, context cancellation, or safety caps when touching fetch logic.
- Do not update release tags or push directly to `master`; both are protected by repository rulesets.
- When a versioned milestone is completed, release using the same version number and close the milestone after the release workflow succeeds.

## COMMIT MESSAGES
- Use `type: summary` with allowed types `feat`, `fix`, `docs`, `ci`, `chore`, `refactor`, `test`, `build`.
- For complex changes, add details after a blank line.
- When Codex is used, add:
```
AI-Assisted: Codex
```
