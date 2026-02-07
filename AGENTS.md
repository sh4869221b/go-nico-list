# AGENTS

Baseline rules for changes in this repository.

## Development Rules (Checklist)
- If you modify Go files, run `gofmt -w <files>` (or `gofmt -w .` to format everything).
- After changes, run `go vet ./...` and confirm there are no errors.
- After changes, run `go test ./...` and confirm there are no errors.
- Before implementation, create (or confirm) a GitHub Issue that tracks the work.
- Use short-lived branches (e.g. `feature/*`) and merge via PR using **squash merge**; do not commit directly to `master` unless explicitly requested.
- Implement only on the issue branch and merge via PR; never push implementation commits directly to `master`.
- Use the `branch-helper` skill for tasks that modify the repository unless the user requests otherwise.
- After addressing review feedback, ask Codex for a re-review in chat.
- PRs must include an auto-close keyword for related issues (e.g. `Closes #123`).
- PR bodies must be based on `.github/PULL_REQUEST_TEMPLATE.md` and keep all sections.
- When creating PRs with `gh`, always use `--body-file` from the template (avoid `--fill` alone).
- Create the PR right after the initial implementation commit so fix logs can be tracked in the PR body.
- Maintain a running fix log in the PR body after each correction pass.
- When editing PR bodies with `gh`, use `-F <file>` and rewrite the full body (no `--add-body` flag exists).
- For implementation work, always follow this flow: Implement → Test → Review.
  - If the review has findings, ask numbered questions for any confirmations needed.
  - Once confirmations are resolved, apply fixes and repeat Fix → Test → Review until there are no findings.

## Workflow Notes
- Keep tests deterministic; avoid time-based ordering and use controllable IO (e.g. pipes) when sequencing matters.
- When mocking the niconico API, ensure pagination terminates (e.g. return empty items or 404 for page > 1).
- Avoid interactive editors in automated merges (use `gh pr merge --squash` and set `GIT_EDITOR` to a non-interactive command when needed).
- Before merging, wait for all CI checks to complete (use `gh pr checks --watch`) unless explicitly told to skip.
- When a versioned milestone is completed, release using the same version number; after the release workflow succeeds, close the milestone.

## Design
Refer to `docs/DESIGN.md` for the design overview and responsibility boundaries.
Keep `docs/DESIGN.md` up-to-date with the current code and behavior.
Before any code change, update `docs/DESIGN.md` as needed and get explicit confirmation (OK) before implementing.
If user-facing behavior changes, update `README.md` as well.

## Improvements / Backlog
Keep `docs/DESIGN.md` focused on the current, agreed-upon behavior.
Do not add undecided proposals to `docs/DESIGN.md`.
Record undecided proposals and improvement ideas in GitHub Issues.

## Work Log
Use `WORKLOG.md` (git-ignored) to record interrupted work, session status, and next steps.
Do not commit `WORKLOG.md`; keep it local-only.
Keep `WORKLOG.md` in English.

## Dependencies
Ask for approval before adding new runtime (production) dependencies.

## Review Questions
When a review has findings, always ask confirmation questions for all findings.
Do not implement changes related to those findings until answers are received.
Number each question to make responses easier.

## Documentation Language
Keep `README.md`, `docs/DESIGN.md`, and `AGENTS.md` in English.
