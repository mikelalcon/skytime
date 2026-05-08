---
phase: 02-generic-activity-block-batch-dispatch-credentials
verified: 2026-04-28
status: passed
score: 6/6 ACT requirements verified · 27/27 VALIDATION.md tests passing · 341 tests across full repo
---

# Phase 2: Generic Activity + Block-Batch Dispatch + Credentials — Verification Report

**Phase Goal:** Build the single Temporal activity (`ExecuteBatch`) that dispatches all extension I/O, batches idempotent actions homogeneously (mixed-batch rejected at parse time per D2-05), returns a structured per-action result list, resolves credentials JIT inside the activity with per-worker TTL cache + retry-aware bypass — testable standalone with hand-built `[]ActionRef` inputs. **No Temporal workflow yet** — that's Phase 3.

**Verified:** 2026-04-28
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

Phase 2 delivered the single Temporal activity (`ExecuteBatch`) and every supporting concern (typed results, output marker, secret wrapper, per-worker credential cache with retry bypass, heartbeat emission, cancellation handling, defensive validation) — entirely without any Temporal workflow code. The 6 v1 requirements assigned to this phase (ACT-01..06) each have ≥1 passing automated test. The architectural firewall holds: `grep -rl "go.temporal.io/sdk" pkg/ | grep -v pkg/activity` returns nothing, and the meta-test confirms `pkg/activity` does in fact import the SDK (catching the inversion bug).

### Observable Truths (mapped to ROADMAP.md Phase 2 success criteria)

| ROADMAP SC | Truth | Evidence |
|------------|-------|----------|
| 1. Hand-built `[]ActionRef` against stub extensions and fake resolver yields `[]ActionResult` of correct shape | `TestExecuteBatch_HappyPath_SingleAction`, `TestExecuteBatch_NonRetryableMidBatch_ReturnsAllResults`, `TestExecuteBatch_RetryableMidBatch_ShortCircuits` all pass | `go test ./pkg/activity` |
| 2. Non-idempotent action runs one-per-invocation; idempotent batches batch | `TestExecuteBatch_DefensivelyRejectsMixedBatch`, `TestExecuteBatch_SingleNonIdempotentAction_Allowed`, `TestLinter_MixedIdempotency_*` all pass | parser-time + activity-time defense |
| 3. Fake-secret never appears in error/heartbeat/result (3 channels) | `TestExecuteBatch_ACT05_SecretNeverLeaks` passes | type-level via `Secret` wrapper — no regex scrubber per amended ACT-05 |
| 4. `StartToCloseTimeout` = sum-of-timeouts + headroom; heartbeats often enough | `TestExecuteBatch_HappyPath_Heartbeats`, `TestActionExecutor_PerActionTimeout`, `TestBatchProgress_JSONSerializable` all pass | per-action `context.WithTimeout`; heartbeat between every action |
| 5. Workflow state contains only credential string IDs | `TestExecuteBatch_HandlerInvokedJIT` confirms `Resolve` invoked at activity time, not parse time; `TestExecuteBatch_ACT05_SecretNeverLeaks` confirms no secret in returned payloads | JIT resolution + Secret-typed fields |

## Requirements Coverage

All 6 ACT requirements have at least one passing test:

| Req | Test(s) | Result |
|-----|---------|--------|
| ACT-01 | `TestExecuteBatch_HappyPath_SingleAction` · `TestExecuteBatch_RegistersWithExplicitName` | ✅ PASS |
| ACT-02 | `TestActionResult_SealedSum` (4 sub-tests) · `TestExecuteBatch_HappyPath_Heartbeats` · `TestExecuteBatch_NonRetryableMidBatch_ReturnsAllResults` · `TestExecuteBatch_RetryableMidBatch_ShortCircuits` | ✅ PASS |
| ACT-03 | `TestExecuteBatch_DefensivelyRejectsMixedBatch` · `TestExecuteBatch_DefensivelyRejectsOversizedBatch` · `TestExecuteBatch_SingleNonIdempotentAction_Allowed` · `TestLinter_MixedIdempotency_Rejects` (+ NestedInIfCond, NestedInForEachParallel) · `TestLinter_BlockSizeCap_DefaultRejects51` · `TestLinter_BlockSizeCap_CustomCap` | ✅ PASS |
| ACT-04 | `TestExecuteBatch_HandlerInvokedJIT` · `TestCredentialCache_HitsAfterFirstResolve` · `TestCredentialCache_ExpiresAfterTTL` · `TestCredentialCache_RaceParallelBatches` (-race) · `TestClassifyResolveError_TableDriven` (7 sub-tests) · `TestClassifyResolveError_UnknownCredentialIsNonRetryable` · `TestClassifyResolveError_OtherErrorsAreRetryable` · `TestExecuteBatch_RetryAttempt_BypassesCache` | ✅ PASS |
| ACT-05 (amended) | `TestSecret_FullRedactionMatrix` (covers `%v`/`%+v`/`%#v`/`%s`) · `TestCredentials_RedactedInAllFormats` (bearer/basic/apikey) · `TestSecret_Reveal` · `TestExecuteBatch_ACT05_SecretNeverLeaks` (3-channel: error msg + heartbeat payload + result payload) | ✅ PASS |
| ACT-06 | `TestExecuteBatch_HappyPath_Heartbeats` · `TestBatchProgress_JSONSerializable` · `TestBatchProgress_NoFunctionsOrChannels` · `TestActionExecutor_PerActionTimeout` · `TestExecuteBatch_CancellationStopsBetweenActions` (bypasses TestActivityEnvironment per Plan 02-03 design) | ✅ PASS |

**27 of 27 VALIDATION.md tests exist and pass.**

## Locked Decisions Honored

| Decision | Status | Verification |
|----------|--------|-------------|
| D2-01 ActionResult sealed sum in pkg/dag | ✅ | `pkg/dag/result.go` defines all 4 kinds; `TestActionResult_SealedSum` confirms |
| D2-02 SkippedResult defined but unused in v1 | ✅ | Variant exists; only emitted on D2-14 non-retryable mid-batch path (post-fail placeholders) |
| D2-03 OperationOutput sealed marker | ✅ | `pkg/dag/output.go` |
| D2-04 OperationFunc returns OperationOutput | ✅ | `pkg/extension/operation.go` updated; consumers compile |
| D2-05 Mixed-idempotency parse-time reject | ✅ | `lintMixedIdempotency` in `pkg/parser/linter.go` |
| D2-06 Splitting in Phase 3 (not Phase 2) | ✅ | Activity treats every received batch as homogeneous; no splitting logic |
| D2-07 Block-size cap = 50 + WithMaxBlockSize | ✅ | `pkg/parser/options.go`; `lintBlockSize` |
| D2-08 Secret wrapper | ✅ | `pkg/extension/secret.go` with `String`/`GoString`/`MarshalJSON`/`MarshalText`/`Format` (5 methods); `Reveal()` for explicit unwrap |
| D2-09 NO regex scrubber in v1 | ✅ | No regex code; ACT-05 amended in REQUIREMENTS.md |
| D2-10 Per-worker cache 5-min TTL | ✅ | `pkg/activity/credential_cache.go` uses `sync.RWMutex` + plain map |
| D2-11 Cache bypass on Attempt > 1 | ✅ | `TestExecuteBatch_RetryAttempt_BypassesCache` passes; cache `Invalidate` called from `ExecuteBatch` |
| D2-12 ErrUnknownCredential → non-retryable | ✅ | `classify.go` table-driven test covers wrapped and double-wrapped paths |
| D2-13 Retryable mid-batch returns error | ✅ | `TestExecuteBatch_RetryableMidBatch_ShortCircuits` |
| D2-14 Non-retryable mid-batch returns full list | ✅ | `TestExecuteBatch_NonRetryableMidBatch_ReturnsAllResults` |
| D2-15 Per-action timeout via context.WithTimeout | ✅ | `TestActionExecutor_PerActionTimeout` |
| D2-16 Heartbeat between every action | ✅ | `TestExecuteBatch_HappyPath_Heartbeats` |
| D2-17 OperationDispatch passed at registration | ✅ | `pkg/activity/dispatch.go` + `WithOperationDispatch` option |
| D2-18 Package name pkg/activity | ✅ | All Phase 2 code under `pkg/activity/` |

## Architectural Invariants

| Check | Result |
|-------|--------|
| `go test ./... -race -count=1` | ✅ all packages green (341 PASS, 0 FAIL, 0 SKIP) |
| `go vet ./...` | ✅ clean |
| `go build ./...` | ✅ clean |
| `grep -rl "go.temporal.io/sdk" pkg/ \| grep -v pkg/activity` | ✅ empty (firewall holds) |
| `TestNoTemporalImportsOutsidePkgActivity` | ✅ PASS |
| `TestPkgActivity_AllowedToImportTemporal` (inversion-bug catcher) | ✅ PASS |
| `extension.DecodeKwargsFromDict` exists and is called from `pkg/activity/action_executor.go` | ✅ confirmed (9 sub-tests pass) |

## Test Suite Summary

| Package | Tests Pass |
|---------|-----------|
| pkg/activity | ~80 (incl. 46 new in Plan 02-03) |
| pkg/bridge | unchanged from Phase 1 |
| pkg/dag | ~75 (incl. 12 new for ActionResult marshaling) |
| pkg/extension | ~80 (incl. Secret matrix, credential redaction, DecodeKwargsFromDict) |
| pkg/extension/testing | (FakeCredentialHandler tests) |
| pkg/parser | unchanged from Phase 1 + 2 new lints |
| **Total** | **341 PASS · 0 FAIL · 0 SKIP** |

## Gap Analysis

**No gaps found.** Every ACT requirement has passing tests; every D2-XX locked decision is honored in code; the architectural firewall is enforced both negatively (no imports leak) and positively (the SDK is actually used).

## Notes

1. **Plan 02-02 had a stream-timeout incident** — the executor agent ran out of API time before committing Task 3 and writing the SUMMARY. The orchestrator finished both per the workflow's spot-check fallback (filesystem state + tests confirmed completion). All work was correct; the gap was procedural.

2. **Phase 2 PLAN.md files were initially orphaned** — the planner agent reported committing them at hash `30d6760` but the commit didn't actually land in main. The orchestrator committed them as housekeeping (`63fc988: docs(02): commit Phase 2 plans`).

3. **ACT-05 was amended at discuss-phase time** (2026-04-27) to drop the regex scrubber requirement in favor of the type-level `Secret` wrapper (Option C from the discussion). The amendment is reflected in REQUIREMENTS.md line 57. Phase 2 implements the amended version.

## Status

**PASSED.** Phase 2 may be marked complete. Phase 3 (interpreter + lambda serialization decision + worker bootstrap) can now begin building on `pkg/activity.ExecuteBatch` as the single Temporal activity it dispatches to.
