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

## Pull requests

- Keep diffs small and focused.
- Avoid adding new runtime dependencies without prior discussion.
- Releases are performed by the maintainer.
- CI runs gofmt, go vet, go test, and go test -race on all branches.

## Release process

1. Ensure master is green and up to date.
2. Create and push a version tag: `vX.Y.Z`.
3. GitHub Actions runs the release workflow, including gofmt/go vet/go test/go test -race.
4. The workflow regenerates `THIRD_PARTY_NOTICES.md` and fails if it is out of date.
5. GoReleaser publishes the GitHub Release and uploads artifacts.

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
