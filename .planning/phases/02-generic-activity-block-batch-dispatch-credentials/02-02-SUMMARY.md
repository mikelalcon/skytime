# Plan 02-02: pkg/activity Foundations — Summary

**Status:** Complete
**Tasks:** 3/3
**Duration:** ~75 min (executor stream timeout on Task 3 commit; orchestrator finished the commit + SUMMARY based on filesystem and test verification per the workflow's spot-check fallback protocol)

## Commits

| Commit | Subject |
|--------|---------|
| `ba17612` | feat(02-02): pkg/activity skeleton + dispatch + heartbeat + attempt + firewall |
| `01819ed` | feat(02-02): credential cache with TTL + retry-aware bypass + classify helper |
| `23d8063` | feat(02-02): pkg/activity functional options + activity struct + sanity tests |

(Plus housekeeping: `63fc988` committed the three Phase 2 PLAN.md files that the planner agent's worktree never landed in main.)

## What This Plan Delivered

The `pkg/activity` package's building blocks — everything `ExecuteBatch` (Wave 2) wires together. No `ExecuteBatch` activity yet by design.

### Files Created

| File | Purpose |
|------|---------|
| `pkg/activity/doc.go` | Package documentation; firewall rule (this is the ONLY package allowed to import `go.temporal.io/sdk/...`) |
| `pkg/activity/dispatch.go` | `OperationDispatch map[string]extension.OperationSpec` type alias (built at parser-finalize-time per D2-17) |
| `pkg/activity/heartbeat.go` | `BatchProgress{Action, Total}` heartbeat payload + `HeartbeatEmitter` interface; default emitter calls `activity.RecordHeartbeat` |
| `pkg/activity/attempt.go` | `attemptFunc` injection seam; `defaultAttemptFunc(ctx)` reads `activity.GetInfo(ctx).Attempt`; tests inject stubs |
| `pkg/activity/credential_cache.go` | Per-worker cache: `sync.RWMutex` + `map[string]cachedEntry`, lazy TTL eviction, default 5-min TTL, `Invalidate` method for retry-aware bypass (D2-11) |
| `pkg/activity/classify.go` | `classifyResolveError(err) (retryable bool, applicationError *temporal.ApplicationError)` — `errors.Is(err, ErrUnknownCredential)` → non-retryable per D2-12 |
| `pkg/activity/options.go` | `Activity` struct + functional options (`WithCredentialHandler`, `WithCredentialCacheTTL`, `WithMaxBlockSize`, package-internal `withAttemptFunc`, `withHeartbeatEmitter`, `withClock` for testability); `New(...)` constructor with required-option validation |
| `pkg/activity/firewall_test.go` | Two AST-walking tests:<br>• `TestNoTemporalImportsOutsidePkgActivity` — fails if any non-pkg/activity file imports `go.temporal.io/sdk/...`<br>• `TestPkgActivity_AllowedToImportTemporal` — META-TEST asserting pkg/activity DOES import the SDK (catches the inversion bug "firewall is a no-op because pkg/activity doesn't actually use the SDK") |

### Tests (all passing under `-race -count=1`)

20 tests across the package, including:

- `TestBatchProgress_NoFunctionsOrChannels` (JSON-serializable)
- `TestFakeHeartbeatEmitter_CapturesCalls`
- `TestCredentialCache_HitsAfterFirstResolve`
- `TestCredentialCache_ExpiresAfterTTL` (injectable clock)
- `TestCredentialCache_RaceParallelBatches` (8 goroutines × 3 IDs × 50 iterations; asserts handler call count `>= 3 && <= 48` per the relaxed bound from checker feedback)
- `TestCredentialCache_Invalidate` (D2-11 retry bypass plumbing)
- `TestClassifyResolveError_TableDriven` (5 cases incl. `ErrUnknownCredential` → non-retryable, generic error → retryable, wrapped `ErrUnknownCredential` via `%w` → non-retryable)
- `TestNew_Defaults`, `TestWithCredentialCacheTTL`, `TestWithMaxBlockSize`, `TestWithAttemptFunc_Internal`, `TestWithHeartbeatEmitter_Internal`
- `TestNew_NilHandler_Errors`, `TestNew_NilDispatch_Errors`, `TestNew_OptionErrorIsWrapped`, `TestNew_OptionsAppliedInOrder`, `TestNew_NoExecuteBatchYet` (compile-time guard that ExecuteBatch isn't accidentally landed yet)
- `TestNoTemporalImportsOutsidePkgActivity`, `TestPkgActivity_AllowedToImportTemporal`

## Locked Decisions Honored

| Decision | Implementation |
|----------|----------------|
| D2-10 per-worker cache, 5-min TTL | `credential_cache.go` with `sync.RWMutex` + plain map; default TTL is `defaultCacheTTL = 5 * time.Minute` |
| D2-11 cache bypass on `Attempt > 1` | `Invalidate(ids ...string)` method ready to call from `ExecuteBatch` (Wave 2 wires it) |
| D2-12 error classification | `classify.go` with table-driven test |
| D2-16 heartbeat between every action | `BatchProgress` payload + `HeartbeatEmitter` interface; default routes to `activity.RecordHeartbeat` |
| D2-17 OperationDispatch passed in at registration | `WithCredentialHandler` + `WithMaxBlockSize` options on `Activity` constructor |
| D2-18 package name `pkg/activity` | All files under `pkg/activity/` |
| Firewall (project-wide invariant) | Two complementary tests |
| `attemptFunc` injection seam | `withAttemptFunc(...)` package-private option (the public `New` doesn't expose it; tests in the same package can inject) — works around the v1.42.0 `TestActivityEnvironment` Attempt-hardcoding gap |

## Validation Status

| Verify command | Result |
|---------------|--------|
| `go test ./pkg/activity/... -race -count=1` | ✅ 20 tests pass |
| `go test ./... -race -count=1` (full repo) | ✅ all packages green |
| `go vet ./...` | ✅ clean |
| `go build ./...` | ✅ clean |
| `grep -r 'go.temporal.io/sdk' pkg/ \| grep -v 'pkg/activity'` | ✅ empty (firewall holds; only pkg/activity imports the SDK) |

## Notes / Deviations

1. **Stream timeout near end of Task 3.** The executor agent ran 63 tool calls; the API stream timed out before the final `git commit` of `options.go` + `options_test.go` and the SUMMARY write. Verified completion via filesystem state (files present, all tests passing). The orchestrator made the final commit and wrote this SUMMARY based on git history + test output. No code changes were made by the orchestrator beyond the missing commit.

2. **Phase 2 PLAN.md files were committed separately** as `63fc988`. They had been written by the planner agent but never landed in main; this housekeeping commit fixes the gap so future replays of `/gsd:execute-phase 2` see the plans on disk and in git history.

## What Wave 2 (Plan 02-03) Inherits

- A working `Activity` struct with all collaborators wired (cache, heartbeat, attempt seam, classifier).
- `Invalidate(...)` method on the cache, ready for the retry-aware-bypass branch in `ExecuteBatch`.
- `BatchProgress` payload type, ready to be emitted between actions.
- `classifyResolveError` for the `Resolve` failure path.
- An import firewall that fails CI if Temporal leaks anywhere outside `pkg/activity`.

Plan 02-03 will write `validate_batch.go`, `action_executor.go`, and `execute_batch.go` and ship the full `ExecuteBatch` activity.

## Requirements Progress

- ACT-04: Cache + classifier + handler interface in place → unit tests green; full integration test in 02-03
- ACT-06: Heartbeat emitter + attempt seam in place → integration test in 02-03

ACT-01, ACT-02, ACT-03, ACT-05 land in 02-03.
