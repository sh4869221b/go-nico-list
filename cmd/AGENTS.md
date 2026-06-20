# CMD PACKAGE GUIDE

## OVERVIEW
`cmd/` owns the Cobra command surface: flags, runtime config, input streams, stdout/stderr behavior, progress, logging, concurrency, summaries, and command-level tests.

## WHERE TO LOOK
| Task | Location | Notes |
| --- | --- | --- |
| Public command entry | `root_flags.go` | `Execute`, `ExecuteContext`, and package `Version`. |
| Flags/defaults/deps | `root_config.go` | `RootConfig`, `RootDeps`, `DefaultConfig`, `DefaultDeps`, `NewRootCommand`. |
| Runner | `root_run.go` | Validation, goroutine fan-out, fetch aggregation, summary, output, final error. |
| Input targets | `input_target.go` | Partial-match user/mylist regexes and named submatch extraction. |
| Input streams/progress | `root_io.go` | Args/input-file/stdin merge, scanner limit, progress auto-disable. |
| JSON output | `root_json.go` | JSON schema, target ordering, normalized item output. |
| Test harness | `root_test_helpers_test.go` | Shared `httptest`, command runner, buffers, fake readers/writers. |

## BOUNDARIES
- Keep niconico HTTP, pagination, retry, rate-limit, filtering, and raw ID sorting in `internal/niconico`.
- Keep command concerns here: flag validation, input source handling, output formatting, exit-code selection, logging destination, progress visibility, and dependency injection.
- `RootConfig` should remain the single place for flag values and runtime defaults.
- `RootDeps` should remain the injection surface for IO, logging, progress factories, input files, and terminal detection.
- `runRootCmdWithConfig` is the command runner. Split helpers only when they preserve observable CLI behavior.

## OUTPUT RULES
- stdout is only data: line output or one JSON object.
- stderr is for progress, warnings, summaries, Cobra error text, and logs unless `--logfile` is set.
- `--json` disables line output; `--url` does not alter JSON `items`.
- `--strict` takes precedence over `--best-effort`.
- Fetch errors can still produce partial stdout results.
- Invalid inputs are skipped by default; `--strict` makes them non-zero while preserving valid results.
- Progress auto-disables on non-TTY stderr; `--no-progress` overrides `--progress`.

## TEST PATTERNS
- Add command tests as focused `root_*_test.go` files near the behavior they cover.
- Use `newTestRootConfig`, `newTestRootDeps`, `executeTestRootCommand`, or `testRunner` from `root_test_helpers_test.go`.
- Use `httptest.Server` for fetch-facing command tests instead of live API calls.
- Assert stdout and stderr separately; summaries belong on stderr.
- For concurrency-sensitive tests, use controllable synchronization (`sync.Once`, channels, context cancellation) instead of sleeping.
- Cover writer, reader, and close errors when touching IO paths.
- Keep JSON tests decoding the payload rather than comparing large raw strings.

## GOTCHAS
- Input regexes are partial matches and prefer the first matching target pattern.
- `bufio.Scanner` input lines are capped at 1 MiB.
- Fetch/log order and first returned fetch error are nondeterministic because targets run concurrently.
- Cobra should not print usage for validation failures.
- If `--logfile` is set, log output moves to the file but Cobra can still print the returned error to stderr.
