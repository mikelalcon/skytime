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

## Plan 07.2-02 (schedules-package)

### Pre-existing E2E test failures (re-confirmed, NOT introduced by this plan)

Re-verified during Plan 02 final verification (`go test ./... -count=1 -race`). Same two failures (`TestE2E_SkytimeRun_Happy`, `TestE2E_SkytimeRun_Unhappy`) with identical `FlowNotInRegistry` symptom. Confirmed via `git stash -u && go test ./tests/ -run TestE2E_SkytimeRun_Happy` against the post-Plan-01 HEAD (which still contains the failure). Plan 02 introduced no code under `pkg/cli/`, `pkg/worker/`, or `tests/` — only `pkg/extension/schedules/` (new), `pkg/extension/builtin/core/cron.go` (added `NewCronSourceForTest` test helper), and `pkg/activity/firewall_test.go` (one allowlist entry). None of these touch the worker registry or build-ID plumbing.

**Owner:** Same as Plan 01 — out of scope.

## Plan 07.2-03 (cli-wiring)

### Pre-existing E2E test failures (re-confirmed, NOT introduced by this plan)

Re-verified during Plan 03 final `go test ./... -count=1 -race`. Same two failures (`TestE2E_SkytimeRun_Happy`, `TestE2E_SkytimeRun_Unhappy`) with identical `FlowNotInRegistry` symptom. Plan 03 added: `pkg/cli/options.go` (new option), `pkg/cli/options_test.go` (new), `pkg/cli/server.go` (--cron-reconcile flag + banner extension), `pkg/cli/server_test.go` (5 tests), `pkg/cli/cron_plan.go` (new), `pkg/cli/cron_plan_test.go` (new), `pkg/cli/registries.go` (new), `pkg/cli/registries_test.go` (new), `pkg/cli/root.go` (one AddCommand line), `pkg/cli/root_test.go` (one presence test), `pkg/worker/boot.go` (one public wrapper, no behavior change), `cmd/skytime/main.go` (registered skycore.New()), `examples/http-github-webhook/cmd/extbin/main.go` (registered skycore.New()). None of these touch the run subcommand's worker registry resolution or build-ID plumbing — the E2E tests fail with the identical `content_hash` mismatch as Plan 01/02.

**Owner:** Same as Plan 01 — out of scope.
