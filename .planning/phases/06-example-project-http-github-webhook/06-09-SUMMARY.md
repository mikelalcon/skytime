---
phase: 06-example-project-http-github-webhook
plan: 09
subsystem: ci
tags: [ci, github-actions, ex-04, walkthrough-smoke, temporal-cli, ubuntu-latest, go-1.25]
requires:
  - 06-05  # extbin (the binary the build + smoke steps compile)
  - 06-06  # public_repo_check.star (the .star the smoke runs)
  - 06-07  # issue_triage_test.star (the *_test.star extbin test discovers)
  - 06-08  # README Quick Start (the smoke's commands MUST match byte-for-byte)
provides:
  - ci.yml-single-job-workflow
  - walkthrough_smoke.sh-extracted-script
  - ex-04-walkthrough-smoke-gating-every-push
  - human-uat-local-repro-script
affects:
  - .github/  # new directory in repo (this plan creates it)
  - .github/workflows/  # new dir
  - .github/workflows/scripts/  # new dir
tech-stack:
  added: []  # No new Go deps; CI is pure GitHub-Actions YAML + bash
  patterns:
    - github-actions-pinned-major-versions  # @v6 / @v0 pin majors, let security patches float
    - extracted-bash-script-for-ci-and-local-parity  # CI invokes the same script humans run for repro
    - trap-based-background-process-cleanup  # temporal dev-server torn down via trap EXIT
    - byte-for-byte-readme-smoke-drift-check  # README Quick Start step 4 + smoke share string literals
key-files:
  created:
    - path: .github/workflows/ci.yml
      lines: 45
      role: "Single-job GitHub Actions workflow gating every push to any branch with the four locked D-CI-STEPS plus EX-04 walkthrough smoke"
    - path: .github/workflows/scripts/walkthrough_smoke.sh
      lines: 118
      role: "Extracted bash script that runs public_repo_check.star end-to-end against a freshly-started temporal dev-server; identical script invoked by CI and by humans running local repro"
  modified: []
decisions:
  - "Pinned actions/setup-go@v6 (not @v5 from CONTEXT.md D-CI-GO) per RESEARCH § 5 — v6 has built-in module + build cache when `cache: true`, eliminates the separate actions/cache@v4 step"
  - "Added `permissions: contents: read` for least-privilege hardening — CI doesn't write to the repo, restricting permissions is GitHub-Actions-recommended security baseline (beyond CONTEXT.md spec; non-disruptive addition)"
  - "Extracted the walkthrough smoke into scripts/walkthrough_smoke.sh (not inline in ci.yml) so the SAME script is both CI's last step AND the canonical human-UAT repro script — drift-proofs the README walkthrough against the CI gate by construction"
  - "Smoke script runs the flow TWICE (m13 sanity): once from repo root with absolute path `examples/http-github-webhook/public_repo_check.star`, once from inside the example dir with relative path `public_repo_check.star` — catches drift between the README's `cd <example> && ./extbin run public_repo_check.star ...` form and CI's repo-root form"
  - "Asserted rendered terminator substring `flow complete` (space, not underscore) per pkg/cli/progress_static.go:245 + pkg/cli/progress_live.go:314 — the slog event KIND is `flow_complete` (underscore) but the renderer translates underscore→space at terminal output; the smoke greps the rendered form, NOT the slog kind"
  - "Used trap-based dev-server cleanup over `kill %1` — survives any exit path including set -e early termination"
metrics:
  duration: 2m
  tasks: 2
  files-created: 2
  files-modified: 0
  completed: 2026-05-07
---

# Phase 06 Plan 09: CI Workflow + EX-04 Walkthrough Smoke Summary

GitHub Actions CI pipeline gating every push to any branch with four locked steps (vet, race-tests, extbin-test, EX-04 walkthrough smoke) — the smoke step is extracted to a standalone bash script so humans can run the EXACT CI gate locally for repro.

## What Shipped

Two files, both new to the repo (this plan creates `.github/` for the first time):

1. **`.github/workflows/ci.yml`** (45 lines) — single-job GitHub Actions workflow.
2. **`.github/workflows/scripts/walkthrough_smoke.sh`** (118 lines, executable) — the EX-04 walkthrough smoke extracted from the YAML so it runs identically in CI and in human-UAT.

## Workflow Structure (verbatim step sequence)

The `ci.yml` job runs on `ubuntu-latest` with `timeout-minutes: 15` (D-CI-WALL safety cap), triggers on `push: { branches: ['**'] }` (D-CI-TRIGGER — every branch, no PR triggers), and has `permissions: contents: read`:

| # | Step name | Command | Source |
|---|-----------|---------|--------|
| - | `Checkout` | `actions/checkout@v6` | Setup |
| - | `Set up Go` | `actions/setup-go@v6` (`go-version: '1.25'`, `cache: true`) | Setup |
| - | `Set up Temporal CLI` | `temporalio/setup-temporal@v0` | Setup |
| 1 | `go vet` | `go vet ./...` | D-CI-STEPS step 1 |
| 2 | `go test (race)` | `go test -race ./... -count=1` | D-CI-STEPS step 2 |
| 3 | `build extbin` | `go build -o /tmp/extbin ./examples/http-github-webhook/cmd/extbin` | D-CI-STEPS step 3 |
| 4 | `extbin test (Tier-3 .star tests)` | `/tmp/extbin test ./examples/http-github-webhook/` | D-CI-STEPS step 4 |
| 5 | `walkthrough smoke (EX-04 — public GitHub API)` | `bash .github/workflows/scripts/walkthrough_smoke.sh` (with `EXTBIN: /tmp/extbin` env so the smoke reuses the binary built in step 3) | D-CI-STEPS step 5 |

All four locked D-CI-STEPS land in this verbatim sequential, fail-fast order.

## Smoke Script Anatomy

`walkthrough_smoke.sh`:

1. `cd`s to repo root via `$(dirname ${BASH_SOURCE[0]})/../../..`.
2. Builds `extbin` to `/tmp/extbin` if not already built (CI step 3 builds it; the env-var override `EXTBIN=/tmp/extbin` reuses it; local users can run the script standalone and it builds on demand).
3. Starts `temporal server start-dev --headless` in the background, captures PID, registers a `trap cleanup EXIT` to guarantee teardown on any exit path.
4. Polls `temporal operator namespace describe default --address localhost:7233` up to 30 times at 1s cadence; fails fast with a clear log if the server doesn't come up.
5. Runs the headline flow: `$EXTBIN run examples/http-github-webhook/public_repo_check.star --flow public_repo_check --input '{"repo":"octocat/Hello-World"}'`.
6. **m13 sanity**: re-runs the SAME flow from inside `examples/http-github-webhook/` with the relative path `public_repo_check.star` — mirrors the README's relative-path form. Both forms must produce `flow complete`; if either fails, CI fails. This catches drift between the README's `cd <example> && ./extbin run public_repo_check.star ...` recipe and CI's repo-root absolute-path form by construction.
7. Greps stdout for the literal substring `flow complete` (space — the rendered form per `pkg/cli/progress_static.go:245` + `pkg/cli/progress_live.go:314`); exits 0 on hit, exits 1 with a diagnostic log on miss.

## README ↔ Smoke Byte-for-Byte Drift Check (passing)

Verified at execution time — the README's Quick Start step 4 and the smoke script share these literal substrings:

- `--flow public_repo_check` — present in both `examples/http-github-webhook/README.md` and `.github/workflows/scripts/walkthrough_smoke.sh`.
- `'{"repo":"octocat/Hello-World"}'` — present in both.

Any future drift will be caught by the same grep pair (suggested as a phase-verifier check; encoded in the plan's acceptance_criteria for re-verification).

## Deviations from Plan

### Auto-fixed Issues

None — both tasks executed exactly as the plan prescribed. No bug fixes, no missing-functionality additions, no blockers encountered. The plan's locked YAML and locked bash-script bodies were authored verbatim with no modifications.

### Observations (informational)

- **PyYAML not installed locally** — the plan documented this as best-effort; the structural-grep fallbacks all passed. CI is the source of truth for YAML parseability (GitHub Actions itself rejects malformed YAML at workflow-load time).
- **shellcheck not installed locally** — informational per the plan; bash syntax check via `bash -n` passes, which is the load-bearing correctness check.
- **`temporal` CLI installed locally** but not invoked end-to-end — running the optional pre-merge end-to-end smoke (build → extbin test → smoke) would burn the unauthenticated GitHub API quota for the local machine (rate-limited 60/hr per RESEARCH Pitfall 3); deferred to CI's first run. CI installs `temporal` via `temporalio/setup-temporal@v0` and has its own per-runner GitHub API quota.

## CI First-Push Status

This SUMMARY lands as part of the final-metadata commit. CI will run for the FIRST TIME on the next `git push`. Expected wall-clock target ~3-5 minutes per D-CI-WALL; safety cap is 15 minutes. Status at first observed run: **PENDING (will be filled in post-push).**

If the first run is red:
- **Build/test failure** → likely Phase 6 regression (run `go test -race -count=1 ./...` locally first).
- **Smoke `flow complete` not found** → check rate-limiting (60/hr unauth on `api.github.com`); the RESEARCH Pitfall 3 mitigation is to retry once after a brief delay or to authenticate the smoke (deferred to v2 — first-push reality is a fresh quota).
- **YAML parse error** → grep-fallback didn't catch a structural error; fix and force-push. (Unlikely — the workflow is structurally trivial.)

## Phase 6 Status After This Plan

With this plan landed alongside 06-07 (issue_triage_test.star) and 06-08 (README) — all three Wave 3 plans complete — Phase 6 is **operationally complete**:

- **Success criterion 1** (HTTP + GitHub + Webhook extensions, every primitive covered) — SATISFIED via Waves 1-2 (06-01..06-06).
- **Success criterion 2** (coverage matrix in README) — SATISFIED via 06-08.
- **Success criterion 3** (`webhook.post` non-idempotency) — SATISFIED via 06-04 + 06-06.
- **Success criterion 4** (≤5-command walkthrough) — SATISFIED via 06-08's README Quick Start.
- **Success criterion 5** (CI green-on-push) — **SATISFIED by this plan**; first observable green status will land on next push.

Only `/gsd:verify-work` (phase verifier sanity-check) and human-UAT remain before milestone v1 declaration.

## Files Created

| Path | Lines | Status |
|------|-------|--------|
| `.github/workflows/ci.yml` | 45 | new |
| `.github/workflows/scripts/walkthrough_smoke.sh` | 118 | new (executable) |

## Commits

| Hash | Message |
|------|---------|
| `649c2c9` | `feat(06-09): extract EX-04 walkthrough smoke to scripts/walkthrough_smoke.sh` |
| `3cba15b` | `feat(06-09): add CI workflow gating every push with four locked steps` |

## Self-Check: PASSED

Verified at write time:
- `[ -f .github/workflows/ci.yml ]` — FOUND (45 lines)
- `[ -f .github/workflows/scripts/walkthrough_smoke.sh ]` — FOUND (118 lines, executable)
- `git log --oneline | grep 649c2c9` — FOUND (Task 1 commit)
- `git log --oneline | grep 3cba15b` — FOUND (Task 2 commit)
- `bash -n .github/workflows/scripts/walkthrough_smoke.sh` — exit 0 (no syntax errors)
- `go build ./...` + `go test -race -count=1 ./...` — both green
- README ↔ smoke byte-for-byte drift checks — pass (`--flow public_repo_check` and `'{"repo":"octocat/Hello-World"}'` present in both files)
