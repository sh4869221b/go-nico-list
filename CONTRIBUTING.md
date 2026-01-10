# Contributing

Thanks for taking the time to contribute!

## Prerequisites

- Go (see `go.mod` for the required version)

## Setup

```bash
go mod download
```

## Quick check

```bash
go run ./... --help
```

## Local development

Run these before opening a PR:

```bash
gofmt -w .
go vet ./...
go test ./...
```

## Third-party notices

If your change updates dependencies (changes `go.mod` / `go.sum`), run:

```bash
go mod tidy
```

If your change updates dependencies (changes `go.mod` / `go.sum`), also update:

```bash
bash scripts/gen-third-party-notices.sh
```

Requires bash and network access to fetch module license metadata.

## Project structure

- `main.go`: bootstrap (version, cancelable context)
- `cmd/`: CLI (flags, IO, validation, concurrency, exit codes)
- `internal/niconico/`: domain logic (fetch, retry, sorting, types)

If the user-facing behavior changes, update `README.md` and `DESIGN.md`.
If the idea is not finalized yet, put it in `IMPROVEMENTS.md` instead of `DESIGN.md`.
Keep `WORKLOG.md` local-only (git-ignored); do not commit it.

## Pull requests

- Keep diffs small and focused.
- Avoid adding new runtime dependencies without prior discussion.
- Releases are performed by the maintainer.
- CI runs gofmt, go vet, go test, and go test -race on all branches.
- Include an auto-close keyword for related issues (e.g. `Closes #123`) in the PR body.
- After addressing review feedback, request a Codex re-review in chat.
- When using `gh pr create`, always use `--body-file` with `.github/PULL_REQUEST_TEMPLATE.md` to avoid literal `\n` in the description.

## Release process

1. Ensure master is green and up to date.
2. If a versioned milestone is complete, release using the same version number.
3. Create and push a version tag: `vX.Y.Z`.
4. GitHub Actions runs the release workflow, verifying generated files (`go mod tidy`, `go generate ./...`) and running gofmt/go vet/go test/go test -race.
5. The workflow regenerates `THIRD_PARTY_NOTICES.md` and fails if it is out of date.
6. GoReleaser publishes the GitHub Release and uploads artifacts.
7. After the release workflow succeeds, close the milestone.

## Branch strategy

- Use `master` as the only long-lived branch.
- Create short-lived branches (e.g. `feature/*`) and merge via PR into `master`.
- Tags for releases (`vX.Y.Z`) are cut from `master`.

## Review criteria

- CI must be green (gofmt, go vet, go test, go test -race).
- If behavior changes, add or update tests; otherwise explain why in the PR.
- If user-facing behavior changes, update README.md and DESIGN.md.
- If dependencies change, run `go mod tidy` and update `THIRD_PARTY_NOTICES.md`.
- Breaking changes must be called out in the PR and docs.

## Commit messages

- Use `type: summary` (Conventional Commits style), e.g. `fix: handle empty output`.
- Allowed types: `feat`, `fix`, `docs`, `ci`, `chore`, `refactor`, `test`, `build`.
- Keep the summary on the first line.
- For complex changes (multiple files or behavior changes), add a blank line, then details from line 3 onward.
- When Codex is used, add the trailer line: `AI-Assisted: Codex`.

Example:

```
refactor: split client package

- move fetch/retry/sort into internal/niconico
- update tests to match new entry points
```

## Reporting issues

Please use the issue templates and include:

- your OS / shell
- tool version (`go-nico-list --version`)
- the exact command you ran
- whether the issue is reproducible
- expected vs actual behavior
- any relevant logs (stderr or `--logfile` output)
