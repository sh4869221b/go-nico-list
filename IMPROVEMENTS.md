# Improvements / backlog

This file tracks non-breaking improvement ideas discussed during review.

## Context

- Primary usage: Windows PowerShell
- Workflow: pass hundreds of niconico user IDs, pipe results to a file, then feed the file to `yt-dlp -a`.

## Ideas

1. Add input file / stdin support
   - Problem: passing hundreds/thousands of arguments can hit Windows command-line length limits.
   - Proposal: add `--input-file <path>` and/or `--stdin` to read newline-separated inputs.

2. Emit a run summary to stderr
   - Problem: when the exit code is ignored, silent partial failures or invalid inputs are easy to miss.
   - Proposal: print a one-line summary (or optionally JSON): total inputs, valid/invalid, fetch success/fail, output lines.

3. Optional "always succeed" mode
   - Problem: for best-effort pipelines, non-zero exit may be undesirable even if some results exist.
   - Proposal: add `--best-effort` to always exit 0 while still logging fetch errors and producing partial output.

4. Optional deduplication
   - Problem: duplicate IDs increase downstream work (`yt-dlp`).
   - Proposal: add `--dedupe` to remove duplicates before sorting/output.

5. Add higher-level test layers (opt-in)
   - Problem: unit tests alone may miss CLI wiring bugs, concurrency issues, and upstream API changes.
   - Proposal:
     - Integration: run the Cobra command against `httptest`; validate stdout/stderr and exit codes.
     - Contract: pin representative API JSON in `testdata/` and validate unmarshal into `NicoData`.
     - E2E (real API): `//go:build e2e` tests that hit the real endpoint, excluded from default CI.
     - Fuzz: URL parsing / JSON handling / sorting should never panic on arbitrary inputs.
     - Stress/perf: `go test -race`, `-count`, and `-bench` for regressions and baselines.

6. Align retry/cancel/progress/sort behavior
   - Problem: current behavior may diverge from the intended contract and can cause hidden failures or unnecessary load.
   - Observations:
     - `retriesRequest`: can return `err == nil` with a non-200/404 final status; may return a response whose body was already closed.
     - Backoff: sleep is not context-aware and may still sleep after the last attempt.
     - Progress: the progress bar is updated from multiple goroutines (race risk depends on library guarantees).
     - Sort: `NiconicoSort` compares the numeric part as zero-padded strings; IDs longer than 8 digits may misorder.
   - Error output: `cobra.CheckErr` formatting may not be strictly "message only" in all cases.
   - Meta status: `meta.status` is ignored; HTTP 200 responses that embed API-level errors could be misclassified as success.
   - Proposal:
      - Retry: if non-200/404 persists after retries, return an error and never return a closed body.

     - Backoff: skip sleeping after the final attempt; interrupt sleep via `ctx.Done()`.
     - Progress: serialize or guard progress updates.
     - Sort: change to numeric sorting, or document the limitation.
     - Errors: if strict "message only" is required, ensure validation errors match that contract.
   - Open questions:
     1. Should `retriesRequest` return an error when retries are exhausted and the status is neither 200 nor 404?
     2. Should backoff sleep be context-cancelable and skipped after the last attempt?
     3. Should progress bar updates be serialized/guarded for race-safety?
      4. Do we need strict "message-only" stderr output for all validation errors?
      5. Should `NiconicoSort` switch to numeric sorting (not string padding)?
      6. Should non-200 `meta.status` be logged (warn/error) even when HTTP is 200?
 
 7. Add explicit request rate limiting

   - Problem: the only global control today is concurrency; unknown external rate limits can still be hit.
   - Proposal: add `--rate-limit <req/s>` and/or `--min-interval <duration>`; honor `429` and `Retry-After` when present.

8. Auto-disable progress bar in non-TTY pipelines
   - Problem: progress output can pollute piped logs/files.
   - Proposal: disable progress by default when stderr is not a TTY; add `--no-progress` to force-disable (and optionally `--progress` to force-enable).

9. Add safety caps for large users
   - Problem: very large accounts can cause unexpectedly long runs and heavy API load.
   - Proposal: add `--max-pages` and/or `--max-videos` to stop early with a best-effort partial result.

10. Add strict input mode
   - Problem: current behavior skips invalid inputs; some workflows prefer failing fast.
   - Proposal: add `--strict` to return non-zero if any invalid input is present (while still logging which inputs were invalid).

11. Add JSON output mode for automation
   - Problem: downstream tooling may prefer structured output over plain lines.
   - Proposal: add `--json` to emit per-user results, errors, and summary counts (keep default line output for `yt-dlp` workflows).

12. Consider adopting `go-licenses` for third-party notices
   - Problem: keeping third-party notices accurate (and bundling license texts for binary releases) can become manual and error-prone.
   - Proposal:
     - Generate `THIRD_PARTY_NOTICES.md` via `go-licenses report` (optionally with a Markdown template).
     - Collect license texts via `go-licenses save` into a `LICENSES/` (or `.cache/licenses/`) directory and include it in release archives.
     - Optionally add `go-licenses check` in CI to fail on `unknown`/restricted/reciprocal licenses.
   - Notes:
     - Prefer excluding test-only dependencies unless explicitly needed (`go-licenses` defaults to non-test packages).
     - Treat this as a dev tool (not a runtime dependency); pin the tool version for reproducibility.

13. Resolve "unknown license" test-only modules in the dependency graph
   - Problem: `go mod` can pull in test-only modules (via transitive `*_test.go` deps) that have unclear or missing license files (example: `github.com/chengxilo/virtualterm`).
     - This can create noise in `THIRD_PARTY_NOTICES.md` and complicate release compliance decisions.
   - Proposal:
     - Prefer generating notices from build dependencies only (exclude test dependencies by default).
     - If a module is still required and its license is unclear, verify the upstream license and record it explicitly (or avoid the dependency).
     - Use `go-licenses check` (or similar policy gates) to detect and fail on `unknown` licenses in CI (opt-in).

