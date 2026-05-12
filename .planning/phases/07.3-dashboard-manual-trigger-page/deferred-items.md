# Phase 07.3 — Deferred Items

Out-of-scope discoveries during execution that should be addressed separately.

## 2026-05-12 — Pre-existing TestNoDevServerLiteralRemains failure

**Discovered during:** Plan 07.3-00 overall verification (Task 3)
**Test:** `tests/dev_server_grep_test.go::TestNoDevServerLiteralRemains`
**Failing file:** `docs/walkthroughs/cron-schedules-smoke.sh:192`
**Root cause:** A stale `# dev-server's visibility store...` comment inside the Phase 7.2 cron-smoke script still contains the literal `dev-server`, which the D-07-22 grep gate (added during Phase 7 rename to `dev-temporal`) forbids outside the allow-list (`.planning/`, `CHANGELOG.md`, `tests/dev_server_grep_test.go`).

**Verified pre-existing:** `git stash && go test ./tests/ -run TestNoDevServerLiteralRemains` reproduced the failure on `d41ffe5` (the parent commit before any Plan 07.3-00 work landed). Confirmed introduced in `afead57 fix(07.2.1): make cron smoke macOS-portable and add LOG-02 verification`.

**Not caused by Plan 07.3-00:** Plan 07.3-00 creates only Wave 0 SKIP stubs + a dashboard skeleton; no edits to `cron-schedules-smoke.sh` or anywhere that mentions `dev-server`. Per execute-plan SCOPE BOUNDARY rule, this is out-of-scope: auto-fixing it would expand the commit and mask the actual Phase 7.2.1 hygiene gap.

**Recommended fix (separate task):** Rename line 192 of `docs/walkthroughs/cron-schedules-smoke.sh` from `# dev-server's visibility store can lag...` to `# temporal dev server's visibility store can lag...` (matches D-07-22 allowed phrasing). Single one-line edit + the test will pass.
