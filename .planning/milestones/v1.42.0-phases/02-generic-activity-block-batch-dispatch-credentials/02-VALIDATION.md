---
phase: 2
slug: generic-activity-block-batch-dispatch-credentials
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-04-28
---

# Phase 2 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` + `github.com/stretchr/testify@v1.11.1` + `go.temporal.io/sdk/testsuite` |
| **Config file** | `go.mod` (test deps inline); no separate config |
| **Quick run command** | `go test ./pkg/activity/... ./pkg/dag/... ./pkg/extension/... -count=1` (~5s for the three packages Phase 2 modifies) |
| **Full suite command** | `go test ./... -race -count=1` (race detector required for credential cache concurrency tests) |
| **Phase gate** | Full suite green + `go vet ./...` clean + `go build ./...` clean |
| **Estimated runtime** | ~30s full suite, <5s per-package |

---

## Sampling Rate

- **After every task commit:** `go test ./pkg/{package-touched}/... -count=1` (<5s)
- **After every plan wave:** `go test ./pkg/activity/... ./pkg/dag/... ./pkg/extension/... -race -count=1` (<10s)
- **Before `/gsd:verify-work`:** `go test ./... -race -count=1` and `go vet ./...` and `go build ./...` all green
- **Max feedback latency:** 30 seconds

---

## Per-Task Verification Map

| Req ID | Behavior | Test Type | Automated Command | File Exists | Status |
|--------|----------|-----------|-------------------|-------------|--------|
| **ACT-01** | `ExecuteBatch(ctx, []ActionRef) ([]ActionResult, error)` is single activity dispatching all I/O | integration | `go test ./pkg/activity -run TestExecuteBatch_HappyPath_SingleAction -count=1` | ❌ W0 | ⬜ pending |
| **ACT-01** | Activity registers via `worker.RegisterActivityWithOptions(impl.ExecuteBatch, RegisterOptions{Name: "ExecuteBatch"})` | integration | `go test ./pkg/activity -run TestExecuteBatch_RegistersWithExplicitName -count=1` | ❌ W0 | ⬜ pending |
| **ACT-02** | `ActionResult` sealed sum: `OkResult` / `RetryableErrResult` / `NonRetryableErrResult` / `SkippedResult` | unit (table) | `go test ./pkg/dag -run TestActionResult_SealedSum -count=1` | ❌ W0 | ⬜ pending |
| **ACT-02** | Activity emits `OkResult{Idx, Output}` for happy-path actions | integration | `go test ./pkg/activity -run TestExecuteBatch_HappyPath_Heartbeats -count=1` | ❌ W0 | ⬜ pending |
| **ACT-02** | Non-retryable mid-batch failure → full `[]ActionResult` (Ok / NonRetryableErr / Skipped) per D2-14 | integration | `go test ./pkg/activity -run TestExecuteBatch_NonRetryableMidBatch_ReturnsAllResults -count=1` | ❌ W0 | ⬜ pending |
| **ACT-02** | Retryable mid-batch failure short-circuits with error per D2-13 | integration | `go test ./pkg/activity -run TestExecuteBatch_RetryableMidBatch_ShortCircuits -count=1` | ❌ W0 | ⬜ pending |
| **ACT-03** | Mixed-idempotency batch rejected at parse time (D2-05) | unit (parser fixture) | `go test ./pkg/parser -run TestLinter_MixedIdempotency_Rejects -count=1` | ❌ W0 | ⬜ pending |
| **ACT-03** | Mixed-idempotency batch defensively rejected at activity boundary | unit | `go test ./pkg/activity -run TestExecuteBatch_DefensivelyRejectsMixedBatch -count=1` | ❌ W0 | ⬜ pending |
| **ACT-03** | Block-size cap (50) enforced at parse time (D2-07) | unit (parser fixture) | `go test ./pkg/parser -run TestLinter_BlockSizeCap_Rejects -count=1` | ❌ W0 | ⬜ pending |
| **ACT-03** | Block-size cap defensively enforced at activity boundary | unit | `go test ./pkg/activity -run TestExecuteBatch_DefensivelyRejectsOversizedBatch -count=1` | ❌ W0 | ⬜ pending |
| **ACT-03** | Single non-idempotent action (block of 1) is allowed | integration | `go test ./pkg/activity -run TestExecuteBatch_SingleNonIdempotentAction_Allowed -count=1` | ❌ W0 | ⬜ pending |
| **ACT-04** | `CredentialHandler.Resolve` invoked from inside activity (JIT, not parse time) | integration | `go test ./pkg/activity -run TestExecuteBatch_HandlerInvokedJIT -count=1` | ❌ W0 | ⬜ pending |
| **ACT-04** | Resolved credential cached on first call (D2-10) | unit | `go test ./pkg/activity -run TestCredentialCache_HitsAfterFirstResolve -count=1` | ❌ W0 | ⬜ pending |
| **ACT-04** | Cache entries expire after TTL | unit (injectable clock) | `go test ./pkg/activity -run TestCredentialCache_ExpiresAfterTTL -count=1` | ❌ W0 | ⬜ pending |
| **ACT-04** | Cache safe under parallel access | unit (race) | `go test ./pkg/activity -run TestCredentialCache_RaceParallelBatches -race -count=1` | ❌ W0 | ⬜ pending |
| **ACT-04** | `errors.Is(err, ErrUnknownCredential)` → `NonRetryableErrResult` (D2-12) | unit (table) | `go test ./pkg/activity -run TestClassifyResolveError_TableDriven -count=1` | ❌ W0 | ⬜ pending |
| **ACT-04** | Other handler errors → `RetryableErrResult` (D2-12) | unit (table) | (same test as above) | ❌ W0 | ⬜ pending |
| **ACT-04** | Cache bypassed when `Attempt > 1` per D2-11 | unit (attemptFn injection) | `go test ./pkg/activity -run TestExecuteBatch_RetryAttempt_BypassesCache -count=1` | ❌ W0 | ⬜ pending |
| **ACT-05** | `Secret.String()`, `GoString()`, `MarshalJSON()`, `MarshalText()`, `Format()` all redact across `%s`/`%v`/`%+v`/`%#v` | unit (verb table) | `go test ./pkg/extension -run TestSecret_FullRedactionMatrix -count=1` | ❌ W0 | ⬜ pending |
| **ACT-05** | `BearerCredential` / `BasicCredential` / `APIKeyCredential` redact through every formatter | unit | `go test ./pkg/extension -run TestCredentials_RedactedInAllFormats -count=1` | ❌ W0 | ⬜ pending |
| **ACT-05** | Integration: known fake-secret never appears in any returned `ActionResult` or heartbeat | integration | `go test ./pkg/activity -run TestExecuteBatch_ACT05_SecretNeverLeaks -count=1` | ❌ W0 | ⬜ pending |
| **ACT-05** | `Secret.Reveal()` returns the raw value (greppable unwrap) | unit | `go test ./pkg/extension -run TestSecret_Reveal -count=1` | ❌ W0 | ⬜ pending |
| **ACT-06** | Activity heartbeats between every action (D2-16) | integration (listener) | `go test ./pkg/activity -run TestExecuteBatch_HappyPath_Heartbeats -count=1` | ❌ W0 | ⬜ pending |
| **ACT-06** | Heartbeat payload is `BatchProgress{Action, Total}` and JSON-serializable | unit | `go test ./pkg/activity -run TestBatchProgress_JSONSerializable -count=1` | ❌ W0 | ⬜ pending |
| **ACT-06** | Per-action timeout enforced via `context.WithTimeout(activityCtx, opTimeout)` | unit | `go test ./pkg/activity -run TestActionExecutor_PerActionTimeout -count=1` | ❌ W0 | ⬜ pending |
| **ACT-06** | Cancellation between actions stops the loop with `SkippedResult` placeholders | integration | `go test ./pkg/activity -run TestExecuteBatch_CancellationStopsBetweenActions -count=1` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

**Toolchain & dependencies:**
- [ ] Add `go.temporal.io/sdk@v1.42.0` to `go.mod` (`go get go.temporal.io/sdk@v1.42.0`); commit `go.mod` + `go.sum`
- [ ] Verify `go mod tidy` succeeds; verify `go vet ./...` clean

**Type-spine additions in `pkg/dag`:**
- [ ] `pkg/dag/result.go` + `pkg/dag/result_test.go` — `ActionResult` sealed sum interface + 4 concrete kinds (`OkResult`, `RetryableErrResult`, `NonRetryableErrResult`, `SkippedResult`); compile-time seal assertion in test
- [ ] `pkg/dag/output.go` + `pkg/dag/output_test.go` — `OperationOutput` sealed marker interface; compile-time seal assertion in test

**Type-spine additions in `pkg/extension`:**
- [ ] `pkg/extension/secret.go` + `pkg/extension/secret_test.go` — `Secret` wrapper type with `String()` / `GoString()` / `MarshalJSON()` / `MarshalText()` / `Format()` all redacting; `Reveal()` accessor; `NewSecret(string)` constructor
- [ ] `pkg/extension/handler.go` (modify) — add `var ErrUnknownCredential = errors.New("unknown credential")` exported sentinel
- [ ] `pkg/extension/credential.go` (modify) — refactor `BearerCredential.Token`, `BasicCredential.Password`, `APIKeyCredential.Key` to use `Secret` type
- [ ] `pkg/extension/operation.go` (modify) — narrow `OperationFunc` return type from `output any` to `output OperationOutput`; add `OperationSpec.DefaultTimeout time.Duration` field (zero = no timeout, configurable per op)
- [ ] `pkg/extension/testing/fake_handler.go` + `_test.go` — sub-package with `FakeCredentialHandler{Creds map[string]Credential}` for shared use across `pkg/activity` and future Phase 5/6 tests

**Parser linter additions:**
- [ ] `pkg/parser/linter.go` (modify) — new pass `checkBlockIdempotency` rejecting mixed batches with friendly fix-suggestion error
- [ ] `pkg/parser/linter.go` (modify) — new pass `checkBlockSize` rejecting blocks > 50 actions; configurable via `parser.WithMaxBlockSize(N)` option
- [ ] `pkg/parser/options.go` (modify) — add `WithMaxBlockSize(int)` option
- [ ] `pkg/parser/linter_test.go` (modify) — tests for both new lints
- [ ] `tests/fixtures/invalid/09-mixed-idempotency.star` — fixture with `# expects: cannot mix idempotent and non-idempotent`
- [ ] `tests/fixtures/invalid/10-block-oversized.star` — fixture with `# expects: block has N actions; maximum is`

**`pkg/activity` package (new):**
- [ ] `pkg/activity/doc.go` — package docs explaining the firewall, the import allowlist (only this package may import `go.temporal.io/sdk/activity`)
- [ ] `pkg/activity/dispatch.go` — `OperationDispatch map[string]extension.OperationSpec` type alias; helpers if needed
- [ ] `pkg/activity/credential_cache.go` + `_test.go` — per-worker cache with TTL using `sync.RWMutex` + `map[string]cachedEntry`; race-condition test
- [ ] `pkg/activity/heartbeat.go` + `_test.go` — `BatchProgress{Action, Total}` struct + `recordHeartbeat` helper; JSON serializability test
- [ ] `pkg/activity/attempt.go` + `_test.go` — `attemptFunc` injection seam (`defaultAttemptFunc` reads `activity.GetInfo(ctx).Attempt`; tests inject stubs for retry-aware bypass tests)
- [ ] `pkg/activity/classify.go` + `_test.go` — `classifyResolveError(err)` helper returning `(retryable bool, applicationError *temporal.ApplicationError)`
- [ ] `pkg/activity/execute_batch.go` + `_test.go` — main `ExecuteBatch(ctx, []ActionRef) ([]ActionResult, error)` activity implementation; integration tests via `testsuite.TestActivityEnvironment`
- [ ] `pkg/activity/options.go` + `_test.go` — worker registration options (`WithCredentialHandler`, `WithCredentialCacheTTL`, `WithOperationDispatch`)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|

*None for Phase 2 — every requirement is exercise-able via `go test`. End-to-end Temporal-cluster integration is Phase 3+ (interpreter wires the activity into a workflow); Phase 2 stays in-process Go using `testsuite.TestActivityEnvironment`.*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
