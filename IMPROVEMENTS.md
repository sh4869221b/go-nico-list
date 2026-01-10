# Improvements / backlog

This file tracks non-breaking improvement ideas discussed during review.

## Context

- Primary usage: Windows PowerShell
- Workflow: pass hundreds of niconico user IDs, pipe results to a file, then feed the file to `yt-dlp -a`.

## Ideas

Moved to GitHub Issues:

1. #110 Add input file / stdin support
2. #111 Emit a run summary to stderr
3. #112 Optional "always succeed" mode (best-effort)
4. #113 Optional deduplication
5. #114 Higher-level test layers (integration/contract/e2e/fuzz)
6. #115 Align retry/cancel/progress/sort behavior
7. #116 Explicit request rate limiting
8. #117 Auto-disable progress bar in non-TTY pipelines
9. #118 Safety caps for large users (max pages/videos)
10. #119 Strict input mode
11. #120 JSON output mode for automation
12. #121 Adopt go-licenses for third-party notices
13. #122 Resolve unknown license test-only modules
