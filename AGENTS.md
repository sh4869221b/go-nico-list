# AGENTS

Baseline rules for changes in this repository.

## Development Rules (Checklist)
- If you modify Go files, run `gofmt -w <files>` (or `gofmt -w .` to format everything).
- After changes, run `go vet ./...` and confirm there are no errors.
- After changes, run `go test ./...` and confirm there are no errors.
- Use short-lived branches (e.g. `feature/*`) and merge via PR; do not commit directly to `master` unless explicitly requested.
- Use the `branch-helper` skill for tasks that modify the repository unless the user requests otherwise.
- After addressing review feedback, re-request review and ask Codex for a re-review in chat.
- PRs must include an auto-close keyword for related issues (e.g. `Closes #123`).
- PR bodies must be based on `.github/PULL_REQUEST_TEMPLATE.md` and keep all sections.
- When creating PRs with `gh`, always use `--body-file` from the template (avoid `--fill` alone).
- When editing PR bodies with `gh`, use `-F <file>` and rewrite the full body (no `--add-body` flag exists).
- For implementation work, always follow this flow: Implement → Test → Review.
  - If the review has findings, ask numbered questions for any confirmations needed.
  - Once confirmations are resolved, apply fixes and repeat Fix → Test → Review until there are no findings.

## Workflow Notes
- Keep tests deterministic; avoid time-based ordering and use controllable IO (e.g. pipes) when sequencing matters.
- When mocking the niconico API, ensure pagination terminates (e.g. return empty items or 404 for page > 1).
- Avoid interactive editors in automated merges (use `git merge -m` or set `GIT_EDITOR` to a non-interactive command).
- Before merging, wait for all CI checks to complete (use `gh pr checks --watch`) unless explicitly told to skip.

## Design
Refer to `DESIGN.md` for the design overview and responsibility boundaries.
Keep `DESIGN.md` up-to-date with the current code and behavior.
Before any code change, update `DESIGN.md` as needed and get explicit confirmation (OK) before implementing.
If user-facing behavior changes, update `README.md` as well.

## Improvements / Backlog
Record undecided proposals and improvement ideas in `IMPROVEMENTS.md` (not in `DESIGN.md`).
Keep `DESIGN.md` focused on the current, agreed-upon behavior.
Keep `IMPROVEMENTS.md` in English.

## Work Log
Use `WORKLOG.md` to record interrupted work, session status, and next steps.
Move short-lived coordination notes or deferred tasks from `IMPROVEMENTS.md` into `WORKLOG.md` when they are about ongoing execution rather than long-term backlog.
Keep `WORKLOG.md` in English.

## Dependencies
Ask for approval before adding new runtime (production) dependencies.

## Review Questions
When a review has findings, always ask confirmation questions for all findings.
Do not implement changes related to those findings until answers are received.
Number each question to make responses easier.

## Documentation Language
Keep `README.md`, `DESIGN.md`, `AGENTS.md`, and `IMPROVEMENTS.md` in English.
