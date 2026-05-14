---
status: resolved
phase: 07-trigger-primitive-server-shell
source: [07-VERIFICATION.md]
started: 2026-05-08T21:01:14Z
updated: 2026-05-13T19:30:00Z
resolution: "Closed by Phase 7.1's worker.WithSDKFactory Option landing. The 3 signal-loop tests (TestServerCmd_DrainOnSIGTERM, TestServerCmd_DrainTimeoutExpiry, TestServerCmd_SecondSignalForceExit) were unskipped and now PASS (07.1-VERIFICATION.md line 49). The live-banner SIGINT smoke is exercised continuously by dashboard-smoke.sh (Phase 7.3, 10/10 PASS on 2026-05-13) and cron-schedules-smoke.sh (Phase 7.2). Auto-resolved during v1.43.0 milestone closing audit."
---

## Current Test

[resolved 2026-05-13 — see frontmatter `resolution` field]

## Tests

### 1. End-to-end SIGTERM drain on a real `skytime server` with in-flight workflows
expected: First SIGTERM drains workflows up to --drain-timeout, exits 0 on clean drain. Second SIGTERM during drain forces immediate exit (status 1). Drain timeout expiry exits 1 with error message.
why_human: Plan 07-05 ships 3 signal-loop tests (TestServerCmd_DrainOnSIGTERM, TestServerCmd_DrainTimeoutExpiry, TestServerCmd_SecondSignalForceExit) with t.Skip("TODO(phase-7.1)") because pkg/cli black-box tests cannot reach pkg/worker.sdkWorkerNew test seam. The worker.WithSDKFactory Option lands in 7.1 to drop the skips. The testDrainHook six-stage seam, source-grep gates, and unit-testable subset (range validation, banner, JSON log) are green; the actual signal-loop end-to-end behavior must be smoke-tested manually until 7.1.
result: [resolved 2026-05-13 — 3 t.Skip stubs dropped in Phase 7.1's worker.WithSDKFactory landing; all three tests now PASS under -race]

### 2. Manual smoke: live banner ordering + SIGINT drain
expected: `temporal server start-dev` + `go run ./cmd/skytime server --rootdir=examples/http-github-webhook/ --task-queue=demo --address=localhost:7233` prints `starting server`, `registered flows` (sorted), `registered triggers` (sorted by Source.Kind, FlowName, Pos). Press Ctrl-C → observe `server draining`, then `drain complete`, exit 0.
why_human: Validates SERVER-03 startup banner ordering and SIGINT drain end-to-end against a live Temporal cluster. Unit-level TestServerCmd_BannerSorted exercises printStartupBanner via NewWorkerForTest fixture, but the live-process printout is the visible operator surface.
result: [resolved 2026-05-13 — exercised continuously by dashboard-smoke.sh (10/10 PASS) and cron-schedules-smoke.sh; live banner ordering verified against `temporal server start-dev`]

## Summary

total: 2
passed: 2
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps
