---
phase: 02-generic-activity-block-batch-dispatch-credentials
plan: 03
subsystem: pkg-activity
tags: [execute-batch, integration-tests, secret-leak-prevention, json-wire-format, action-result-roundtrip]

# Dependency graph
requires:
  - phase: 02-generic-activity-block-batch-dispatch-credentials
    provides: "Wave 0 type spine (Secret, ActionResult, OperationOutput, ErrUnknownCredential, FakeCredentialHandler, OperationFunc narrowing, OperationSpec.DefaultTimeout, DecodeKwargsFromDict, parser linter passes); Wave 1 pkg/activity foundations (Activity struct, OperationDispatch, credentialCache, BatchProgress + heartbeatEmitter, attemptFunc seam, classifyResolveError, options + functional New, firewall test)"
provides:
  - "*Activity.ExecuteBatch — single Temporal activity entry point (ACT-01)"
  - "*Activity.runAction — per-action runner with credential resolve + per-action timeout + kwargs decode + spec.Func dispatch (ACT-04, ACT-06)"
  - "*Activity.validateBatch — defense-in-depth batch validation (ACT-03)"
  - "isRetryable helper — temporal.ApplicationError.NonRetryable() inspection driving D2-13 short-circuit vs D2-14 full-list semantics"
  - "dag.ActionResults typed slice with JSON discriminator round-trip (ACT-02 wire format)"
  - "dag.RawOperationOutput — placeholder OperationOutput for OkResult.Output post-decode"
  - "dag.UnmarshalActionResult — single-payload entry point"
  - "ActionRef.UnmarshalJSON + goValueToStarlark — required for Temporal activity input round-trip"
  - "Each ActionResult kind gets MarshalJSON with kind discriminator (\"Ok\" / \"RetryableErr\" / \"NonRetryableErr\" / \"Skipped\")"
affects: [phase-03-interpreter, phase-06-example-extensions]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Discriminator-based JSON envelope for sealed-sum types (ActionResult kinds + ActionResults typed slice)"
    - "Test seam pattern: withHeartbeatEmitter + withAttemptFunc bypass realHeartbeatEmitter / defaultAttemptFunc when running outside TestActivityEnvironment (Temporal SDK panics on activity.GetInfo / activity.RecordHeartbeat with non-activity contexts)"
    - "Recover-wrapped activity.GetLogger call inside ExecuteBatch — best-effort logging that no-ops when called outside an activity context (unit-test invocations of ExecuteBatch directly)"
    - "Defense-in-depth at runtime — validateBatch re-checks every parser-side invariant (D2-05, D2-06, D2-07) in case a bug or hand-built batch slips through"

key-files:
  created:
    - "pkg/activity/validate_batch.go (93 LOC) — defensive batch validation"
    - "pkg/activity/validate_batch_test.go (186 LOC) — 10 validation tests"
    - "pkg/activity/action_executor.go (114 LOC) — runAction + decodeActionRefKwargs"
    - "pkg/activity/action_executor_test.go (332 LOC) — 11 unit tests"
    - "pkg/activity/execute_batch.go (139 LOC) — ExecuteBatch + isRetryable"
    - "pkg/activity/execute_batch_test.go (607 LOC) — 13 integration tests"
    - "pkg/dag/result_marshal.go (277 LOC) — ActionResult JSON wire format + ActionResults typed slice"
    - "pkg/dag/result_marshal_test.go (168 LOC) — 12 marshal/round-trip tests"
  modified:
    - "pkg/dag/marshal.go — appended ActionRef.UnmarshalJSON + goValueToStarlark (~70 LOC) for activity input round-trip"
    - "pkg/activity/options_test.go — TestNew_NoExecuteBatchYet → TestNew_HasExecuteBatch (Wave-2 pin)"

key-decisions:
  - "JSON wire format for ActionResult is a Rule 2 deviation — Temporal's default DataConverter uses encoding/json which cannot fill `[]ActionResult` (slice of interface) without help. Each kind gets MarshalJSON with kind discriminator + new ActionResults typed slice with UnmarshalJSON. Without this, the activity contract literally cannot round-trip; this is critical functionality, not optional."
  - "ActionRef gets UnmarshalJSON for the same Rule 2 reason — testsuite encodes []*dag.ActionRef on the workflow side and decodes on the activity side; without UnmarshalJSON the Kind_ field is silently lost (default reflection maps `kind` JSON key to `Kind` Go field, not `Kind_`)."
  - "OkResult.Output round-trips as RawOperationOutput placeholder. The concrete OperationOutput type cannot be recovered by JSON unmarshal alone (no discriminator on the typed Output struct). Phase 3 will know the op-specific output type via the dispatch table and re-decode RawOperationOutput.Bytes into it. For Phase 2 tests, the OkResult wrapper is what's asserted via require.IsType — Output's concrete type is not pinned."
  - "TestExecuteBatch_HappyPath_Heartbeats deviates from RESEARCH.md §Example 2 — uses withHeartbeatEmitter seam instead of SetOnActivityHeartbeatListener. Temporal SDK v1.42.0 documents the listener is throttled (\"may not get called for every heartbeat\" — workflow_testsuite.go:267). The plan's \"exactly 2 heartbeats\" assertion is impossible to make non-flaky against the listener; the fake emitter (defined in 02-02 for exactly this purpose) captures every emit() call deterministically. The test still drives ExecuteBatch through TestActivityEnvironment so the activity contract is exercised end-to-end."
  - "TestExecuteBatch_CancellationStopsBetweenActions explicitly bypasses TestActivityEnvironment (per locked design in plan) AND injects withHeartbeatEmitter(&fakeHeartbeatEmitter{}) — without it, realHeartbeatEmitter.emit panics on activity.RecordHeartbeat with a non-activity context. Same withAttemptFunc stub is used to bypass defaultAttemptFunc's activity.GetInfo call. The test exercises the production ExecuteBatch logic with a stdlib cancellable context; both seams together let the unit-style test run against the real activity body."
  - "ExecuteBatch returns (results, nil) on cancellation, NOT (results, ctx.Err()) — D2-14 contract per locked test design. The plan's example code at lines 762-768 shows `return results, ctx.Err()` but the locked test asserts `require.NoError(t, err)`. Locked test wins. Cancellation is graceful, not a failure."

requirements-completed: [ACT-01, ACT-02, ACT-03, ACT-04, ACT-05, ACT-06]

# Metrics
duration: ~50min
completed: 2026-04-28
---

# Phase 2 Plan 03: Wave 2 ExecuteBatch Integration Summary

**Wave 2 lands the single Temporal activity entry point — `*Activity.ExecuteBatch` — that ties together every Wave 0 (types) and Wave 1 (cache, heartbeat, attempt seam, classify, options) building block. Phase 3's interpreter can now call `workflow.ExecuteActivity(ctx, "ExecuteBatch", batch)` and rely on documented per-action result semantics (D2-13/D2-14), JIT credential resolution with retry-aware cache bypass (D2-11), per-action heartbeats (D2-16), per-action timeouts (D2-15), defensive batch validation (D2-05/06/07), and three-channel secret redaction (D2-08/D2-09 / ACT-05). All six ACT requirements (ACT-01 through ACT-06) are satisfied with passing tests under `go test ./... -race -count=1`.**

## Performance

- **Duration:** ~50 min
- **Tasks:** 3/3 complete (autonomous, no checkpoints)
- **Files created:** 8
- **Files modified:** 2
- **Tests added:** 46 (10 validation + 11 runAction + 13 integration + 12 dag marshal/round-trip)

## Accomplishments

### ExecuteBatch — the single Temporal-activity-boundary

```go
func (a *Activity) ExecuteBatch(ctx context.Context, batch []*dag.ActionRef) ([]dag.ActionResult, error)
```

The orchestrator: validateBatch (defense in depth) → cache-bypass-on-retry (D2-11) → per-action loop with cancellation check (D2-14 graceful stop) → runAction (per-action runner) → classify (D2-13 short-circuit retryable / D2-14 full-list non-retryable) → heartbeat between every action (D2-16) → return.

### Per-action runner — runAction

```go
func (a *Activity) runAction(ctx context.Context, idx int, ref *dag.ActionRef) (dag.OperationOutput, error)
```

Five steps: dispatch lookup (defense in depth) → credential resolve via cache with bypass=Attempt>1 (D2-11) → resolve-error classification (D2-12) → per-action timeout via context.WithTimeout when DefaultTimeout > 0 (D2-15) → kwargs decode via extension.DecodeKwargsFromDict (Wave 0) → spec.Func call. Op errors returned UNWRAPPED for ExecuteBatch to classify.

### Defensive batch validation — validateBatch

```go
func (a *Activity) validateBatch(batch []*dag.ActionRef) error
```

Six failure modes, six distinct *temporal.ApplicationError Type strings (EmptyBatch / BatchTooLarge / UnknownOperation / MissingIdempotent / MixedIdempotency / MultiNonIdempotent). Each is NonRetryable — these are configuration / contract violations, never transient.

### JSON wire format for ActionResult — Rule 2 deviation

Temporal's default DataConverter uses encoding/json. Without explicit MarshalJSON / UnmarshalJSON support, `[]ActionResult` (slice of interface) cannot round-trip — the test contract `var results dag.ActionResults; encoded.Get(&results)` would fail. This is critical activity-contract functionality, not optional polish. Lands as:

- Each ActionResult kind: MarshalJSON with `"kind": "<Kind>"` discriminator
- New `dag.ActionResults` typed slice with UnmarshalJSON dispatching on kind
- New `dag.RawOperationOutput` placeholder for OkResult.Output post-decode
- New `dag.UnmarshalActionResult` exported for single-payload decoding
- New `ActionRef.UnmarshalJSON` + `goValueToStarlark` for activity INPUT round-trip (the testsuite encodes the workflow's `[]*ActionRef` arg before delivering it to the activity body)

### Three-channel ACT-05 secret-leak prevention

`TestExecuteBatch_ACT05_SecretNeverLeaks` injects `phantom-token-XYZ-abc123-do-not-leak` as a Secret-wrapped BearerCredential and an op that fails non-retryably with `%s` AND `%+v` formatting of the credential. Three independent leak channels asserted:

1. **ActionResult.Err string** — Credential.String() redacts (Phase 1 D-09)
2. **Heartbeat payload bytes** — captured via SetOnActivityHeartbeatListener; serialized via json.Marshal of BatchProgress; bytes-contains scan for the secret
3. **Encoded result bytes** — `json.Marshal(results)` re-renders what crossed the wire; bytes-contains scan

All three pass. The Secret wrapper's five formatter methods (String / GoString / Format / MarshalJSON / MarshalText) close the leak surface — there's no path from a Secret-typed field to a redacted-aware formatter that emits the raw bytes.

### Cancellation cooperation

`TestExecuteBatch_CancellationStopsBetweenActions` (DESIGN LOCKED, BYPASSES TestActivityEnvironment) verifies the locked D2-14-extended contract: cancellation is graceful — return SkippedResult placeholders for unrun indexes, return (results, nil) — not an error. Test exercises the production ExecuteBatch logic with a stdlib cancellable context, plus withHeartbeatEmitter + withAttemptFunc stubs to keep realHeartbeatEmitter / defaultAttemptFunc from panicking on a non-activity context.

## Task Commits

| # | Task | Commit | Files |
|---|------|--------|-------|
| 1 | Defensive batch validation (validate_batch.go) | `f3b9dd2` | validate_batch.go (93) + validate_batch_test.go (186) |
| 2 | Per-action runner (action_executor.go) | `8168c4a` | action_executor.go (114) + action_executor_test.go (332) |
| 3 | ExecuteBatch + integration tests + JSON wire format | `9f9c264` | execute_batch.go (139) + execute_batch_test.go (607) + result_marshal.go (277) + result_marshal_test.go (168) + marshal.go (+70) + options_test.go (TestNew_HasExecuteBatch pin) |

## Files Created/Modified

### Created (8 files)

- `pkg/activity/validate_batch.go` — `validateBatch` + 6 error Type constants
- `pkg/activity/validate_batch_test.go` — 10 tests covering all validation paths + happy-path AtLimit + SingleNonIdempotent_OK
- `pkg/activity/action_executor.go` — `runAction` + `decodeActionRefKwargs`
- `pkg/activity/action_executor_test.go` — 11 tests (HappyPath / PerActionTimeout / Resolve* / RetryAttemptForcesBypass / UnknownOp / OpReturnsNonAppError / OpReturnsAppError / KwargsDecode_*)
- `pkg/activity/execute_batch.go` — `ExecuteBatch` + `isRetryable`
- `pkg/activity/execute_batch_test.go` — 13 tests (RegistersWithExplicitName / HappyPath_SingleAction / HappyPath_Heartbeats / NonRetryableMidBatch / RetryableMidBatch / DefensivelyRejects* / HandlerInvokedJIT / RetryAttempt_BypassesCache / ACT05_SecretNeverLeaks / CancellationStopsBetweenActions / SingleNonIdempotentAction_Allowed / ActionExecutor_PerActionTimeout)
- `pkg/dag/result_marshal.go` — ActionResult kinds' MarshalJSON + ActionResults typed slice + RawOperationOutput + UnmarshalActionResult + errMsg/errString helpers
- `pkg/dag/result_marshal_test.go` — 12 tests covering each kind's marshal output + round-trip + unknown-kind fail + nil/null edges

### Modified (2 files)

- `pkg/dag/marshal.go` — appended `ActionRef.UnmarshalJSON` + `goValueToStarlark` (~70 LOC; added `fmt` import)
- `pkg/activity/options_test.go` — `TestNew_NoExecuteBatchYet` → `TestNew_HasExecuteBatch` (assertion inverted to pin post-Wave-2 state)

## Decisions Made

| Decision | Rationale |
| --- | --- |
| JSON wire format for ActionResult (Rule 2 deviation) | Temporal's default DataConverter is encoding/json. Without MarshalJSON/UnmarshalJSON on the sealed-sum types, `[]ActionResult` cannot round-trip — the activity contract is literally unworkable. Each kind gets a `"kind"` discriminator; new `ActionResults` typed slice has UnmarshalJSON to dispatch. Critical functionality, not optional polish. |
| ActionRef.UnmarshalJSON (Rule 2 deviation) | testsuite encodes the activity's input `[]*ActionRef` and decodes on the activity side. Without UnmarshalJSON, the JSON keys (`kind`, `kwargs`, `credential_id` from MarshalJSON) don't match the Go field names (`Kind_`, `Kwargs`, `CredentialID`) — every field comes back zero-valued. The empty-Kind_ then fails validateBatch with UnknownOperation `""`. UnmarshalJSON + a primitive-value `goValueToStarlark` rebuild the Starlark Dict from the JSON object. |
| OkResult.Output decodes to RawOperationOutput placeholder | The concrete OperationOutput type can't be recovered from JSON alone (no per-output discriminator). For Phase 2 tests `require.IsType(OkResult{}, results[0])` is satisfied by the wrapper alone — Output's concrete type is not pinned. Phase 3's interpreter will know the op-specific output type via the dispatch table and re-decode `RawOperationOutput.Bytes` into it. |
| TestExecuteBatch_HappyPath_Heartbeats uses withHeartbeatEmitter (deviation from RESEARCH §Example 2) | Temporal SDK v1.42.0 documents that SetOnActivityHeartbeatListener is throttled — "may not get called for every heartbeat recorded ... due to internal caching by the activity system" (workflow_testsuite.go:267). The plan's "exactly 2 heartbeats" assertion is impossible to make non-flaky against the throttled listener. The unexported `withHeartbeatEmitter` seam (built in 02-02 for exactly this purpose) injects a fakeHeartbeatEmitter that captures every emit() call deterministically. The test still drives ExecuteBatch through TestActivityEnvironment so the activity contract is exercised end-to-end; only the heartbeat assertion uses the fake. |
| Cancellation returns (results, nil) — not (results, ctx.Err()) | Plan example code at lines 762-768 shows `return results, ctx.Err()`; the locked test design at line 1062 asserts `require.NoError(t, err)`. Locked test wins. Cancellation is graceful, not a failure — the SkippedResult placeholders ARE the signal the workflow consumer needs to act on, not an error. |
| TestExecuteBatch_CancellationStopsBetweenActions injects BOTH withHeartbeatEmitter AND withAttemptFunc | The plan's locked design bypasses TestActivityEnvironment to exercise the production ExecuteBatch logic with a stdlib cancellable context. realHeartbeatEmitter calls activity.RecordHeartbeat which panics outside an activity context; defaultAttemptFunc calls activity.GetInfo which also panics. Both unexported seams are needed to make the test run; the seams are documented for exactly this case in heartbeat.go and attempt.go. |
| activity.GetLogger call wrapped in recover() inside ExecuteBatch | The retry-attempt log line uses activity.GetLogger which requires a real activity context. Inside TestActivityEnvironment it works; inside the cancellation test (which bypasses testsuite) it would panic. The recover wrap makes the log line best-effort — production behavior unchanged, unit-test invocations safe. The cancellation test does not enter the retry branch (Attempt=1) so this never fires there, but the wrap is defense in depth for any future unit test that exercises the retry path directly. |

## Deviations from Plan

### Auto-added Critical Functionality (Rule 2)

**1. [Rule 2 — Wire-format gap] JSON marshal/unmarshal for dag.ActionResult sealed sum**

- **Found during:** Task 3 (first integration test run)
- **Issue:** `var results []dag.ActionResult; encoded.Get(&results)` cannot work via stock encoding/json — interface slices can't be filled by Unmarshal without a discriminator scheme. The Temporal activity contract literally requires this round-trip.
- **Fix:** Each kind gets MarshalJSON with `"kind"` discriminator; new `dag.ActionResults` typed slice with UnmarshalJSON; new `dag.RawOperationOutput` placeholder for OkResult.Output; new `dag.UnmarshalActionResult` for single-payload decoding.
- **Files added:** `pkg/dag/result_marshal.go` (277 LOC) + `pkg/dag/result_marshal_test.go` (168 LOC)
- **Verification:** 12 marshal/round-trip tests pass; the integration tests round-trip `dag.ActionResults` cleanly through Temporal's converter.
- **Committed in:** `9f9c264` (Task 3)

**2. [Rule 2 — Wire-format gap] ActionRef.UnmarshalJSON + goValueToStarlark**

- **Found during:** Task 3 (first integration test run — symptom: `unknown operation ""`)
- **Issue:** ActionRef has MarshalJSON (emits `kind`/`kwargs`/`credential_id`) but no UnmarshalJSON. testsuite encodes the input `[]*ActionRef` and decodes on the activity side; without UnmarshalJSON, the JSON keys don't match Go field names (`Kind_` ≠ `Kind`, `Kwargs *starlark.Dict` ≠ `map[string]any`), so every field comes back zero-valued. The empty `Kind_` then fails validateBatch with UnknownOperation `""`.
- **Fix:** Appended UnmarshalJSON to pkg/dag/marshal.go reading the same `actionRefJSON` envelope produced by MarshalJSON; reconstructs `*starlark.Dict` from JSON object via new `goValueToStarlark` (handles primitives + falls through to String coercion for nested types). Pos field is NOT recovered (was never serialized — see file header on cross-machine stability); zero syntax.Position is acceptable for the runtime path where attribution is by action index.
- **Files modified:** `pkg/dag/marshal.go` (~70 LOC appended; `fmt` import added)
- **Verification:** All 13 integration tests pass; ActionRef round-trips through testsuite cleanly.
- **Committed in:** `9f9c264` (Task 3)

### Test-Design Deviation (Rule 1)

**3. [Rule 1 — Plan example incorrect for v1.42.0 SDK] TestExecuteBatch_HappyPath_Heartbeats uses fake emitter not testsuite listener**

- **Found during:** Task 3 (test run — symptom: 1 heartbeat captured, 2 expected)
- **Issue:** The plan's RESEARCH.md §Example 2 sketch uses SetOnActivityHeartbeatListener to capture heartbeats. Temporal SDK v1.42.0 documents that the listener is throttled — "may not get called for every heartbeat recorded ... due to internal caching by the activity system" (workflow_testsuite.go:267). Two heartbeats emitted within microseconds (which is what happens with our trivial fake ops) get batched into one delivery. The "exactly 2 heartbeats" assertion is impossible to make non-flaky against the listener.
- **Fix:** Use the unexported `withHeartbeatEmitter` seam (built in 02-02 for exactly this purpose) to inject a fakeHeartbeatEmitter that captures every emit() call deterministically. The test still drives ExecuteBatch through TestActivityEnvironment so the activity contract is exercised end-to-end; the heartbeat assertion uses the fake emitter's snapshot.
- **Files modified:** `pkg/activity/execute_batch_test.go` (TestExecuteBatch_HappyPath_Heartbeats body)
- **Verification:** Test passes; D2-16 "every action" semantic is now pinned at the emitter contract (the source of truth) rather than at Temporal's deliberately-throttled wire protocol.
- **Committed in:** `9f9c264` (Task 3)

### Pre-existing Sentinel Removed

**4. [Rule 3 — Blocking] TestNew_NoExecuteBatchYet → TestNew_HasExecuteBatch**

- **Found during:** Task 3
- **Issue:** Plan 02-02 added `TestNew_NoExecuteBatchYet` as a scope-creep guard asserting *Activity has NO ExecuteBatch method (Wave 1 stops at the building blocks). Wave 2 lands ExecuteBatch — the assertion would fail.
- **Fix:** Inverted assertion + renamed to `TestNew_HasExecuteBatch`; pinned the post-Wave-2 state. Comment documents the inversion.
- **Files modified:** `pkg/activity/options_test.go`
- **Committed in:** `9f9c264` (Task 3)

## Authentication Gates

None.

## Confirmation: All Six ACT Requirements Satisfied

| Req | Verified by |
|-----|-------------|
| ACT-01 (single generic activity) | `TestExecuteBatch_RegistersWithExplicitName` — `env.RegisterActivityWithOptions(impl.ExecuteBatch, sdkactivity.RegisterOptions{Name: "ExecuteBatch"})` succeeds + activity dispatches a 1-action batch by-name. |
| ACT-02 (sealed-sum result list) | `TestExecuteBatch_HappyPath_SingleAction` (OkResult), `TestExecuteBatch_NonRetryableMidBatch_ReturnsAllResults` (Ok / NonRetryable / Skipped sequence), `TestActionResults_RoundTrip_AllKinds` (all 4 kinds round-trip through JSON). |
| ACT-03 (no-batching for non-idempotent + block cap) | `TestExecuteBatch_DefensivelyRejectsMixedBatch`, `TestExecuteBatch_DefensivelyRejectsOversizedBatch`, `TestValidateBatch_NonIdempotentMulti_NonRetryable`, `TestExecuteBatch_SingleNonIdempotentAction_Allowed`. |
| ACT-04 (JIT credential resolution + per-worker cache + retry-aware bypass) | `TestExecuteBatch_HandlerInvokedJIT` (handler.calls=0 before ExecuteActivity, 1 after), `TestExecuteBatch_RetryAttempt_BypassesCache` (Attempt=2 + warm cache → handler called twice). |
| ACT-05 (Secret type + multi-channel leak prevention) | `TestExecuteBatch_ACT05_SecretNeverLeaks` — three independent leak channels checked: error message, heartbeat payload bytes, encoded result bytes. All three pass with the 32-character `phantom-token-XYZ-abc123-do-not-leak` fake secret. |
| ACT-06 (per-action heartbeat + per-action timeout) | `TestExecuteBatch_HappyPath_Heartbeats` (exactly 2 heartbeats with payloads {1,2} and {2,2} via fake emitter), `TestActionExecutor_PerActionTimeout` (5ms DefaultTimeout fires, op respects ctx.Done(), surfaces context deadline exceeded). |

## Test Suite Status

| Verify command | Result |
|---|---|
| `go test ./pkg/activity/... -count=1` | All 47 tests pass |
| `go test ./... -count=1` | All packages green |
| `go test ./... -race -count=1` | All packages green under race detector |
| `go vet ./...` | Clean |
| `go build ./...` | Clean |
| `go test ./pkg/activity/ -run TestNoTemporalImportsOutsidePkgActivity -count=1` | Firewall passes — only pkg/activity imports go.temporal.io/sdk/* |
| `grep -rn "go.temporal.io/sdk" pkg/dag/ pkg/extension/ pkg/parser/ pkg/bridge/` | Empty — firewall holds |

## What Phase 3 Inherits

- `*Activity.ExecuteBatch` registered as `"ExecuteBatch"` — Phase 3 calls `workflow.ExecuteActivity(ctx, "ExecuteBatch", batch)` with documented retry/non-retry semantics.
- `dag.ActionResults` typed slice for decoding the activity's return — `var results dag.ActionResults; encoded.Get(&results)` round-trips through JSON.
- `dag.RawOperationOutput` for OkResult.Output post-decode — Phase 3 dispatch-table-driven re-decode into the typed Output struct of each op.
- Three-channel secret redaction guarantee — no scrubber needed; type-level Secret wrapper closes the leak surface.
- Per-worker credential cache with retry-aware bypass — Phase 3 just constructs `activity.New(dispatch, handler)` and the cache works automatically.
- Defense-in-depth batch validation — Phase 3's parser linter is the primary D2-05/06/07 gate; ExecuteBatch re-checks at runtime.
- isRetryable helper exposed for Phase 3 tests that exercise retry paths.

## Self-Check: PASSED

All success criteria verified:

- [x] All 3 tasks executed per plan acceptance criteria
- [x] Each task committed individually (`f3b9dd2`, `8168c4a`, `9f9c264`)
- [x] `go test ./... -race -count=1` exits 0
- [x] `go vet ./...` clean
- [x] `go build ./...` clean
- [x] All 6 ACT requirements have passing tests (table above)
- [x] `TestExecuteBatch_HappyPath_SingleAction` passes
- [x] `TestExecuteBatch_HappyPath_Heartbeats` passes (via fake emitter — see deviation 3)
- [x] `TestExecuteBatch_NonRetryableMidBatch_ReturnsAllResults` passes (D2-14)
- [x] `TestExecuteBatch_RetryableMidBatch_ShortCircuits` passes (D2-13)
- [x] `TestExecuteBatch_DefensivelyRejectsMixedBatch` passes
- [x] `TestExecuteBatch_DefensivelyRejectsOversizedBatch` passes
- [x] `TestExecuteBatch_HandlerInvokedJIT` passes
- [x] `TestExecuteBatch_RetryAttempt_BypassesCache` passes (D2-11)
- [x] `TestExecuteBatch_ACT05_SecretNeverLeaks` passes (3-channel)
- [x] `TestExecuteBatch_CancellationStopsBetweenActions` passes (bypass-TestActivityEnvironment, locked design)
- [x] `TestActionExecutor_PerActionTimeout` passes
- [x] `TestExecuteBatch_RegistersWithExplicitName` passes
- [x] Firewall holds: no `go.temporal.io/sdk/...` imports outside pkg/activity
- [x] pkg/activity/action_executor.go calls extension.DecodeKwargsFromDict (Plan 02-01 Wave 0 reuse — no new pkg/extension exports)

**Phase 2 complete. ExecuteBatch is the single Temporal-activity boundary; Phase 3's interpreter can call it via `workflow.ExecuteActivity(ctx, "ExecuteBatch", batch)` with documented retry/non-retry semantics.**
