# Deferred Items — Phase 07.2

Issues discovered during execution that are out-of-scope for the current plan.

## Plan 07.2-01 (core-cron-factory)

### Pre-existing E2E test failures (NOT introduced by this plan)

Discovered during Task 3 verification (`go test ./... -count=1 -race`).

**Failing tests:**
- `tests/e2e_skytime_run_test.go::TestE2E_SkytimeRun_Happy`
- `tests/e2e_skytime_run_test.go::TestE2E_SkytimeRun_Unhappy`

**Symptom:** Both fail with `flow simple_check@<hash> not found in worker registry; use Build IDs to drain old workflows (type: FlowNotInRegistry, retryable: false)`.

**Confirmed pre-existing:** Reproduced on Phase 07.2 main HEAD (`git stash` prior to Task 3 edits) — same failure mode. No code in `pkg/extension/builtin/core/`, `pkg/parser/trigger_test.go`, or `tests/firewall_credential_redaction_test.go` (the Plan 07.2-01 surface) touches the worker registry or build-ID plumbing.

**Likely cause (hypothesis only):** Stale `content_hash` mismatch between the .star file bytes loaded by the embedded transient worker vs. the workflow already in temporal dev-server history — probably stale state in `~/.temporal/...` from a prior run.

**Owner:** Out of scope for Plan 07.2-01. Belongs to a quick fix in Phase 04/05 territory (worker boot / build ID) — file a `/gsd:quick` if this blocks future work.
