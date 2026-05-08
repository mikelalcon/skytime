---
phase: 07-trigger-primitive-server-shell
verified: 2026-05-08T21:01:14Z
status: human_needed
score: 5/5 must-haves verified (3 SIGTERM signal-loop tests deferred to Phase 7.1 with named t.Skip stubs; manual smoke needed for end-to-end signal handling)
human_verification:
  - test: "End-to-end SIGTERM drain on a real `skytime server` with in-flight workflows"
    expected: "First SIGTERM drains workflows up to --drain-timeout, exits 0 on clean drain. Second SIGTERM during drain forces immediate exit (status 1). Drain timeout expiry exits 1 with error message."
    why_human: "Plan 07-05 SUMMARY documents that 3 signal-loop tests (TestServerCmd_DrainOnSIGTERM, TestServerCmd_DrainTimeoutExpiry, TestServerCmd_SecondSignalForceExit) ship with t.Skip(\"TODO(phase-7.1)\") because pkg/cli black-box tests cannot reach pkg/worker.sdkWorkerNew test seam. The worker.WithSDKFactory Option lands in 7.1 to drop the skips. The testDrainHook six-stage seam, source-grep gates, and unit-testable subset (range validation, banner, JSON log) are green; the actual signal-loop end-to-end behavior must be smoke-tested manually until 7.1."
  - test: "Manual smoke: `temporal server start-dev` + `go run ./cmd/skytime server --rootdir=examples/http-github-webhook/ --task-queue=demo --address=localhost:7233`"
    expected: "Server prints `starting server`, `registered flows` (sorted), `registered triggers` (sorted by Source.Kind, FlowName, Pos). Press Ctrl-C → observe `server draining`, then `drain complete`, exit 0."
    why_human: "Validates SERVER-03 startup banner ordering and SIGINT drain end-to-end against a live Temporal cluster — covered by VALIDATION.md § Manual-Only Verifications. Unit-level TestServerCmd_BannerSorted exercises printStartupBanner via NewWorkerForTest fixture, but the live-process printout is the visible operator surface."
---

# Phase 7: Trigger Primitive + Server Shell Verification Report

**Phase Goal:** Establish the foundation for triggers and durable worker mode in one phase. Add the top-level `trigger(...)` Starlark builtin, the new `dag.Trigger` node type with stable JSON marshaling, the `TriggerSource` value type returned by extension factories, and parse-time validation. Extend the boot registry to walk `--rootdir` and register flows AND triggers from the same `.star` files. Ship the `skytime server` subcommand shell as a long-lived process with sorted startup banner, SIGTERM drain, and configurable `--drain-timeout`. Rename `skytime dev-server` → `skytime dev-temporal` (hard rename, no alias).

**Verified:** 2026-05-08T21:01:14Z
**Status:** human_needed — all automated must-haves pass; signal-loop end-to-end deferred to Phase 7.1 + manual smoke
**Re-verification:** No (initial verification)

## Goal Achievement

### Observable Truths (from ROADMAP.md success_criteria)

| #   | Truth                                                                                                                                                                                                                              | Status              | Evidence                                                                                                                                                                                                                                                                                                                                                                                                |
| --- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | A consultant can write `trigger(flow=, source=, map=, idempotency_key=, credential=)` at top level, parse without I/O, and inspect the resulting `*dag.Trigger` (FlowName, Source, MapLambda, IdempotencyLambda, CredentialID).    | ✓ VERIFIED          | `pkg/parser/builtins.go::builtinTrigger` (lines 1403-1462) constructs `*dag.Trigger` from kwargs, type-asserts `sourceVal.(extension.TriggerSource)`, captures lambdas with arity-1 enforcement, registers in `p.triggers`. `TestBuiltinTrigger` and `TestBuiltinTrigger_Fields` pass under `-race`. `pkg/dag/trigger.go::Trigger` struct has all 5 expected fields plus `Pos` and `frozen`.             |
| 2   | A malformed trigger (unknown flow name, missing required source kwarg, source not a TriggerSource, malformed lambda free vars) produces a `<file>:<line>:<col>: <msg>` error at parse time and never panics.                       | ✓ VERIFIED          | 5 negative tests pass: `TestTrigger_UnknownFlow`, `TestTrigger_BadSource`, `TestTrigger_ReqAttrTypo`, `TestTrigger_BadArity`, `TestTrigger_MutableClosure`. Three-layer validation: (1) free-var lint via `validateFreeVars`, (2) arity via `captureLambdaWithArity`, (3) req-attribute via `validateTriggerReqAccesses` finalize pass + cross-file flow resolution via `validateTriggerFlowNames`.      |
| 3   | `skytime server --rootdir=... --task-queue=... --address=...` starts, walks rootdir for `*.star` (skip `*_test.star`), prints `registered flows: [...]` and `registered triggers: [...]` in name-sorted order, runs Temporal worker, stays up until SIGTERM. | ✓ VERIFIED          | `pkg/cli/server.go` shipped with `skytime server` subcommand, all 6 expected flags, calls `connectClient(cfg)` (D4-08 routing), `worker.NewWorker`, `printStartupBanner` (3 sorted slog records), `w.Start()`, then `signal.Notify` loop. `TestServerCmd_Flags` + `TestServerCmd_BannerSorted` pass. `pkg/worker/boot.go::bootRegistry` returns `(FlowRegistry, TriggerRegistry, error)` from one parser session; `TestBootRegistry_RegistersTriggers` + `TestBootRegistry_SkipsTestFiles` pass. |
| 4   | SIGTERM during in-flight workflows waits up to `--drain-timeout` (default 30s), exits 0 on clean drain or 1 with timeout message if forced.                                                                                        | ⚠️ PARTIAL          | Code shipped: two-signal escalation in `server.go` lines 144-180 with `signal.Notify` (NOT NotifyContext per Pitfall 5); `WorkerStopTimeout` threaded into `sdkworker.Options`. Range validation tests (`TestServerCmd_DrainTimeoutRangeCheck_*`) pass. **3 signal-loop tests deferred to Phase 7.1 with t.Skip + TODO** (`TestServerCmd_DrainOnSIGTERM`, `TestServerCmd_DrainTimeoutExpiry`, `TestServerCmd_SecondSignalForceExit`) pending `worker.WithSDKFactory` seam. Manual smoke covers end-to-end. |
| 5   | `skytime dev-temporal` exists and works identically to prior `skytime dev-server`; `skytime dev-server` no longer exists; README/docs/walkthrough_smoke.sh reference new name with no stale `dev-server` invocations remaining.    | ✓ VERIFIED          | `pkg/cli/dev_temporal.go` exists; `pkg/cli/dev_server.go` removed (verified absent on disk via `ls`). `go run ./cmd/skytime --help` shows `dev-temporal` (not `dev-server`). `tests/dev_server_grep_test.go::TestNoDevServerLiteralRemains` scans 369 tracked files, asserts zero literal `dev-server` outside allow-list (`.planning/` + CHANGELOG.md + self-exclusion). 4 `TestDevTemporalCmd_*` tests pass.      |

**Score:** 5/5 truths verified at the must-have level (truth #4 is partial because the signal-loop end-to-end is deferred to Phase 7.1 by design — the unit-testable subset and manual smoke pathway are both green).

### Required Artifacts

| Artifact                                            | Expected                                                          | Status      | Details                                                                                                       |
| --------------------------------------------------- | ----------------------------------------------------------------- | ----------- | ------------------------------------------------------------------------------------------------------------- |
| `pkg/dag/trigger.go`                                | dag.Trigger struct + dag.TriggerSource interface                  | ✓ VERIFIED  | 102 lines; struct fields match spec (Pos, FlowName, Source, MapLambda, IdempotencyLambda, CredentialID, frozen). |
| `pkg/dag/marshal.go`                                | triggerJSON wire shape + Marshal/Unmarshal + cross-package seam   | ✓ VERIFIED  | RegisterTriggerSourceUnmarshaler + unmarshalTriggerSource declared; Pos excluded from JSON.                   |
| `pkg/extension/trigger.go`                          | sealed extension.TriggerSource (Kind, ReqSchema, MarshalJSON, triggerSourceMarker) | ✓ VERIFIED | Compile-time `var _ dag.TriggerSource = TriggerSource(nil)` bridge present.                                  |
| `pkg/extension/trigger_unmarshal.go`                | kind-keyed factory registry + init() wiring to dag                | ✓ VERIFIED  | `dag.RegisterTriggerSourceUnmarshaler(extensionTriggerUnmarshaler)` called from package init.                  |
| `pkg/extension/triggersource_fake.go`               | FakeTriggerSource test stub                                       | ✓ VERIFIED  | Lives in package extension (not sub-package) per Plan 02 deviation; satisfies the seal.                       |
| `pkg/parser/builtins.go::builtinTrigger`            | 5-kwarg builtin emitting *dag.Trigger; registered in parseTimeGlobals | ✓ VERIFIED | Lines 1403-1462; type-asserts extension.TriggerSource; uses captureLambdaWithArity(arity=1).                  |
| `pkg/parser/finalize.go`                            | 3 new finalize passes: validateTriggerFlowNames, validateTriggerReqAccesses, warnDuplicateTriggers | ✓ VERIFIED | Lines 85, 103, 115 — wired in correct order per Plan 03.                                                       |
| `pkg/parser/req_walk.go`                            | findFreeVarAccesses parameterized visitor                         | ✓ VERIFIED  | 9713 bytes; ctx_walk.go now delegates via firstParamNameAt.                                                   |
| `pkg/parser/trigger_test.go`                        | 9 black-box trigger tests covering TRIG-01/04 + D-07-12/13       | ✓ VERIFIED  | All 9 functions present and pass: TestBuiltinTrigger, TestBuiltinTrigger_Fields, TestTrigger_{UnknownFlow,BadSource,ReqAttrTypo,BadArity,MutableClosure,CrossFileFlow,DuplicateWarn}. |
| `pkg/parser/testdata/triggers/`                     | 9 .star fixtures (valid, typo, bad_arity, unknown_flow, mutable_closure, not_a_source, cross_file_flow, cross_file_trigger, duplicate_warn) | ✓ VERIFIED | `ls` confirms all 9 files.                                                                                    |
| `pkg/interpreter/registry.go::TriggerRegistry`      | sealed registry parallel to FlowRegistry; Register/Freeze/All/ByContentHash | ✓ VERIFIED | Lines 165-275 (approx); ErrTriggerRegistryFrozen sentinel; sorted on Freeze by (Source.Kind, FlowName, Pos.String). |
| `pkg/worker/boot.go::bootRegistry`                  | extended signature returning (FlowRegistry, TriggerRegistry, error); slog drain of parser warnings | ✓ VERIFIED | Line 45 signature; lines 155-180 wire trigger registration + Freeze + slog.Default().Warn drain.              |
| `pkg/worker/worker.go::Worker.Triggers()`           | accessor exposing TriggerRegistry; WorkerStopTimeout threaded into sdkworker.Options | ✓ VERIFIED | Worker struct has triggers field; lines 67-68 thread WorkerStopTimeout when non-zero.                         |
| `pkg/worker/options.go::WorkerStopTimeout`          | field + 30s default constant + applyDefaults branch               | ✓ VERIFIED  | defaultWorkerStopTimeout=30s; applyDefaults supplies default when zero.                                       |
| `pkg/cli/server.go`                                 | skytime server subcommand: 6 flags, signal-loop, drain, banner    | ✓ VERIFIED  | 227 lines; all 6 stage names present (worker_started, signal_received, drain_started, drain_completed, drain_timeout, drain_forced); testDrainHook + testForceExit seams shipped. |
| `pkg/cli/server_test.go`                            | 11 tests for SERVER-01..03 (4 skipped per design)                 | ✓ VERIFIED  | TestServerCmd_Flags + DrainTimeoutRangeCheck_{Zero,Negative,AboveOneHour} + ConnectClient + BannerSorted + JSONLog all PASS; SelfHosted + DrainOnSIGTERM + DrainTimeoutExpiry + SecondSignalForceExit SKIP per VALIDATION.md verification map. |
| `pkg/cli/render.go::setupServerLogging`             | charm-log default; JSON via slog.NewJSONHandler when jsonMode=true | ✓ VERIFIED  | Function present; bypasses progressHandler routing per Pitfall 7.                                              |
| `pkg/cli/dev_temporal.go` (renamed from dev_server) | newDevTemporalCommand; cobra Use="dev-temporal"; no alias         | ✓ VERIFIED  | git mv preserves rename similarity. `go run ./cmd/skytime --help` shows `dev-temporal Spawn a local Temporal dev server`. |
| `tests/firewall_credential_redaction_test.go`       | AST walker rejecting %+v / %#v in pkg/dag, pkg/extension, pkg/extension/builtin | ✓ VERIFIED | `TestCredentialRedactionFirewall` PASS — scanned 3 dirs, 0 violations. `TestCredentialRedactionFirewall_AcceptsCleanCode` PASS (positive regression). |
| `tests/dev_server_grep_test.go`                     | git ls-files gate against `dev-server` literal; allow-list = .planning/ + CHANGELOG.md + self | ✓ VERIFIED | `TestNoDevServerLiteralRemains` PASS — scanned 369 tracked files, 0 hits. `TestNoDevServerLiteralRemains_AllowListIsEffective` PASS. |

### Key Link Verification

| From                                              | To                                              | Via                                                                       | Status     | Details                                                                                                            |
| ------------------------------------------------- | ----------------------------------------------- | ------------------------------------------------------------------------- | ---------- | ------------------------------------------------------------------------------------------------------------------ |
| `parser.builtinTrigger`                           | `extension.TriggerSource`                       | `sourceVal.(extension.TriggerSource)` type assertion                      | ✓ WIRED    | Line 1426 of pkg/parser/builtins.go; ParseError on assertion failure with file:line:col.                          |
| `parser.builtinTrigger`                           | `dag.Trigger`                                   | `&dag.Trigger{Pos, FlowName, Source, MapLambda, IdempotencyLambda, CredentialID}` | ✓ WIRED   | Lines 1444-1451 construct *dag.Trigger; appended to p.triggers.                                                   |
| `pkg/extension` init                              | `pkg/dag` unmarshal seam                        | `dag.RegisterTriggerSourceUnmarshaler(extensionTriggerUnmarshaler)` from init() | ✓ WIRED  | trigger_unmarshal.go installs dispatcher at package init time.                                                    |
| `parser.finalize`                                 | trigger validation passes                       | calls validateTriggerFlowNames, validateTriggerReqAccesses, warnDuplicateTriggers in correct order | ✓ WIRED | Lines 85/103/115 of finalize.go.                                                                                  |
| `worker.bootRegistry`                             | `interpreter.TriggerRegistry`                   | `trigReg.Register(hash, trig)` per parsed trigger; `trigReg.Freeze()` after | ✓ WIRED  | Lines 155-172 of pkg/worker/boot.go.                                                                              |
| `worker.bootRegistry`                             | parser warnings → slog                          | `for _, w := range p.TriggerWarnings(): slog.Default().Warn(...)`        | ✓ WIRED    | Lines 176-178; `TestBootRegistry_DrainsParserWarnings` confirms slog text contains "duplicate trigger".            |
| `worker.NewWorker`                                | `sdkworker.Options.WorkerStopTimeout`           | `if opts.WorkerStopTimeout > 0 { sdkOpts.WorkerStopTimeout = opts.WorkerStopTimeout }` | ✓ WIRED | Lines 67-68 of pkg/worker/worker.go; `TestNewWorker_WorkerStopTimeoutDefault/Custom` PASS.                         |
| `cli.newServerCommand`                            | `worker.NewWorker(c, WorkerOptions{WorkerStopTimeout: drainTimeout, ...})` | direct construction; `connectClient(cfg)` reused | ✓ WIRED | Lines 100-116 of pkg/cli/server.go.                                                                               |
| `cli.newServerCommand`                            | `printStartupBanner` → `w.FlowNames()` + `w.Triggers().All()` | three slog.Info records before w.Start()                       | ✓ WIRED    | Line 126 (printStartupBanner call) + lines 205-226 (banner body). `TestServerCmd_BannerSorted` PASS.              |
| `cli.newServerCommand`                            | `signal.Notify(sigCh, SIGINT, SIGTERM)` two-signal escalation | buffered channel size 2; first signal → drain; second → testForceExit; drainCtx timeout | ⚠️ PARTIAL | Code shipped at lines 144-180; **3 end-to-end tests skipped pending Phase 7.1 worker.WithSDKFactory** (see truth #4). Manual smoke covers it. |
| root.go                                           | `newDevTemporalCommand(cfg)`                    | `cmd.AddCommand(newDevTemporalCommand(cfg))`                              | ✓ WIRED    | `TestRoot_HasDevTemporalSubcommand` + `TestRootCommand_BareInvocationPrintsHelp` both PASS.                       |

### Data-Flow Trace (Level 4)

| Artifact                          | Data Variable                  | Source                                                | Produces Real Data                                                                                  | Status     |
| --------------------------------- | ------------------------------ | ----------------------------------------------------- | --------------------------------------------------------------------------------------------------- | ---------- |
| `printStartupBanner` flows record | `flowNames []string`           | `w.FlowNames()` → `FlowRegistry.FlowNames()` → range over `r.byFlow` | DB-equivalent: live registry populated by bootRegistry from parsed .star files                   | ✓ FLOWING  |
| `printStartupBanner` triggers     | `trigs []*dag.Trigger`         | `w.Triggers().All()` → `TriggerRegistry.All()` → snapshot of `r.triggers` | live registry populated by bootRegistry's trigger-registration loop                              | ✓ FLOWING  |
| `dag.Trigger` JSON marshal        | Source envelope                | `TriggerSource.MarshalJSON()` (delegated per concrete type) | Plan 02 ships extension.FakeTriggerSource MarshalJSON producing {kind, config: {req_fields, credential_id}}. Phase 7 has no real production sources yet (first lands in 7.1). | ✓ FLOWING for fakes; first real source ships 7.1 |
| `validateTriggerReqAccesses`      | per-trigger lambda req walks   | `findFreeVarAccesses(src, filename, lambdaPos, "req")` reads source file bytes | Real AST walks against actual source files; no static returns                                     | ✓ FLOWING  |
| `slog.Default().Warn`             | parser duplicate-trigger warns | `p.TriggerWarnings()` accumulates from `warnDuplicateTriggers` finalize pass | Real warnings drained at boot-time; `TestBootRegistry_DrainsParserWarnings` captures via buffered handler | ✓ FLOWING |

### Behavioral Spot-Checks

| Behavior                                                            | Command                                                                                                | Result                                                                                                       | Status |
| ------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------ | ------ |
| Build clean                                                         | `go build ./...`                                                                                       | (no output, exit 0)                                                                                          | ✓ PASS |
| Vet clean                                                           | `go vet ./...`                                                                                         | (no output, exit 0)                                                                                          | ✓ PASS |
| Full test suite passes under -race                                  | `go test ./... -count=1 -race`                                                                         | All 20 packages PASS                                                                                         | ✓ PASS |
| Trigger parser tests pass                                           | `go test ./pkg/parser/ -run 'TestBuiltinTrigger\|TestTrigger_'`                                        | 9/9 PASS                                                                                                     | ✓ PASS |
| Server cmd tests pass (4 skipped per design)                        | `go test ./pkg/cli/ -run TestServerCmd_`                                                               | 7 PASS, 4 SKIP (DrainOnSIGTERM, DrainTimeoutExpiry, SecondSignalForceExit, ConnectClient_SelfHosted)         | ✓ PASS |
| Boot registry registers triggers                                    | `go test ./pkg/worker/ -run TestBootRegistry_`                                                         | 10/10 PASS                                                                                                   | ✓ PASS |
| Firewall credential-redaction enforced                              | `go test ./tests/ -run TestCredentialRedactionFirewall`                                                | 3 target dirs scanned, 0 violations                                                                          | ✓ PASS |
| dev-server grep gate green                                          | `go test ./tests/ -run TestNoDevServerLiteralRemains`                                                  | 369 tracked files scanned, 0 hits outside allow-list                                                         | ✓ PASS |
| skytime help shows dev-temporal + server (no dev-server)            | `go run ./cmd/skytime --help`                                                                          | Lists `dev-temporal Spawn a local Temporal dev server` and `server Run a long-lived Skytime worker (drain-on-SIGTERM)` | ✓ PASS |
| skytime server flags inventory complete                             | `go run ./cmd/skytime server --help`                                                                   | Shows --addr, --credfile, --drain-timeout, --json-log, --rootdir, --task-queue                                | ✓ PASS |
| End-to-end signal-loop drain on real Temporal cluster               | `temporal server start-dev` + `go run ./cmd/skytime server --rootdir=... --address=...` + Ctrl-C       | (deferred to manual smoke per VALIDATION.md)                                                                 | ? SKIP — see Human Verification |

### Requirements Coverage

| Requirement | Source Plan         | Description                                                                                                                                              | Status       | Evidence                                                                                                                  |
| ----------- | ------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------ | ------------------------------------------------------------------------------------------------------------------------- |
| TRIG-01     | 07-03               | `.star` file declares trigger() with 5 kwargs as top-level builtin; references captured; no I/O                                                          | ✓ SATISFIED  | TestBuiltinTrigger + TestBuiltinTrigger_Fields PASS; pkg/parser/builtins.go::builtinTrigger lines 1403-1462                |
| TRIG-02     | 07-02               | Extensions can return `TriggerSource` value type; parser stores opaque payload; runtime unpacks via type-switch                                          | ✓ SATISFIED  | pkg/extension/trigger.go sealed interface; FakeTriggerSource stub; init-time wiring to dag                                |
| TRIG-03     | 07-01               | dag.Trigger node with stable JSON marshaling (Kind, FlowName, Source, MapLambda, IdempotencyLambda, CredentialID); round-trip                            | ✓ SATISFIED  | pkg/dag/trigger.go + marshal.go; 8 trigger_test.go tests including round-trip + Pos exclusion + credential redaction       |
| TRIG-04     | 07-03               | Parse-time validation rejects malformed triggers with position-aware errors                                                                              | ✓ SATISFIED  | TestTrigger_UnknownFlow, BadSource, ReqAttrTypo, BadArity, MutableClosure all PASS; three-layer validation present       |
| TRIG-05     | 07-04               | Boot registry walks --rootdir for *.star (skip *_test.star), parses each, registers flows AND triggers from the same scan                                | ✓ SATISFIED  | TestBootRegistry_RegistersTriggers, SkipsTestFiles, TriggerInLoadedFile, NoTriggers, DrainsParserWarnings PASS              |
| SERVER-01   | 07-05               | `skytime server` long-running subcommand with --rootdir, --task-queue, --temporal/--address, --addr, --credfile flags                                    | ✓ SATISFIED  | All 6 flags registered; TestServerCmd_Flags PASS; --addr accepted, warns "no effect until Phase 7.1"; connect uses global --address (existing convention; --temporal in REQUIREMENTS prose maps to --address persistent flag from D4-08) |
| SERVER-02   | 07-05               | SIGTERM gracefully drains in-flight workflows up to --drain-timeout (default 30s, K8s convention); forces shutdown after timeout                         | ⚠️ PARTIAL  | Code shipped + range validation tests PASS + WorkerStopTimeout threaded; **3 signal-loop end-to-end tests deferred to Phase 7.1** (named t.Skip stubs preserve verification map). Manual smoke required. |
| SERVER-03   | 07-05               | Startup logs registered flows AND triggers in deterministic sorted order                                                                                 | ✓ SATISFIED  | TestServerCmd_BannerSorted PASS; printStartupBanner emits 3 sorted slog records; flows sorted via FlowNames(), triggers sorted via TriggerRegistry.Freeze() |
| CLI-13      | 07-06               | `skytime dev-server` renamed to `skytime dev-temporal` across code, docs, CI scripts; no deprecation alias                                               | ✓ SATISFIED  | git mv preserved similarity; 369 tracked files scanned, 0 dev-server literals outside allow-list (.planning/ + CHANGELOG.md + self). 4 TestDevTemporalCmd_* PASS. TestRoot_HasDevTemporalSubcommand PASS. |

**No orphaned requirements.** All 9 requirement IDs declared in PLAN frontmatters across 6 plans align with REQUIREMENTS.md Phase 7 mapping.

### Anti-Patterns Found

| File                                                  | Line   | Pattern                                                        | Severity | Impact                                                                                                  |
| ----------------------------------------------------- | ------ | -------------------------------------------------------------- | -------- | ------------------------------------------------------------------------------------------------------- |
| `pkg/cli/server_test.go`                              | 173, 180, 187 | `t.Skip("TODO(phase-7.1): ...")` on 3 signal-loop tests | ℹ️ Info  | Documented design decision per 07-05-SUMMARY.md. Phase 7.1 worker.WithSDKFactory drops the skips. Function names match VALIDATION.md verification map for forward compatibility. |
| `pkg/cli/server_test.go`                              | 149    | `t.Skip` on TestServerCmd_ConnectClient_SelfHosted             | ℹ️ Info  | Covered by pkg/cli/connect_test.go for the run subcommand's identical mTLS path; mTLS PEM fixtures here add zero marginal coverage. |
| `pkg/worker/boot_test.go` (per Plan 04 SUMMARY)       | inline | `TODO(phase-7.1): refactor fakeWebhookExt + fakeTriggerStarlarkValue to pkg/extension/testing` | ℹ️ Info  | Helper duplication accepted (Option B) — factoring belongs to a quick that owns test infrastructure shape, not load-bearing Phase 7 plan. |

**No blockers.** No hardcoded empty data, no stub returns, no TODO/FIXME outside test seams. Skipped tests have explicit forward-compat rationale.

### Human Verification Required

#### 1. End-to-end SIGTERM drain on real Temporal cluster

**Test:**
```bash
# Terminal 1
temporal server start-dev

# Terminal 2 (with examples/http-github-webhook/ containing flows+triggers)
go run ./cmd/skytime server --rootdir=examples/http-github-webhook/ --task-queue=demo --address=localhost:7233
```
Press Ctrl-C with no in-flight workflows. Then again with one in-flight workflow.

**Expected:**
1. Server prints startup banner: `starting server`, `registered flows`, `registered triggers` (last two sorted alphabetically by flow name and by Source.Kind+FlowName respectively).
2. First Ctrl-C → `server draining; second SIGINT/SIGTERM forces immediate exit` → `drain complete` → exit 0.
3. Second Ctrl-C during drain (with in-flight workflow) → `drain interrupted by second signal; forcing exit` → exit 1.
4. Drain timeout (artificial: set `--drain-timeout=2s` and start a workflow that takes >2s) → `drain-timeout exceeded; restart resumes from event history` → exit 1.

**Why human:** Plan 07-05 SUMMARY documents that 3 signal-loop tests (TestServerCmd_DrainOnSIGTERM, TestServerCmd_DrainTimeoutExpiry, TestServerCmd_SecondSignalForceExit) ship with `t.Skip("TODO(phase-7.1)")` because pkg/cli black-box tests cannot reach the pkg/worker.sdkWorkerNew test seam. The `worker.WithSDKFactory` Option lands in Phase 7.1 to drop the skips. The testDrainHook six-stage seam, source-grep gates, and unit-testable subset (range validation, banner, JSON log) are green; the actual end-to-end signal-loop must be smoke-tested manually until 7.1.

#### 2. Manual smoke validation of startup banner ordering

**Test:** Run the same command as #1 and visually confirm the output ordering.

**Expected:**
- `registered flows` line: alphabetical sort by flow name.
- `registered triggers` line: sorted by (Source.Kind, FlowName, Pos.String) tuple per Plan 04's TriggerRegistry.Freeze.

**Why human:** Validates SERVER-03 against the live operator surface. Unit-level TestServerCmd_BannerSorted exercises printStartupBanner via NewWorkerForTest fixture, but the live-process slog rendering through charm-log is the visible-by-operator surface and confirms end-to-end determinism.

### Gaps Summary

**No blocker gaps.** All 9 requirement IDs satisfied by code + tests with one design-by-decision deferral:

- **Truth #4 / SERVER-02 partial:** 3 SIGTERM signal-loop end-to-end tests ship with `t.Skip("TODO(phase-7.1)")` pending the `worker.WithSDKFactory(fn)` Option that lets pkg/cli black-box tests inject a fake SDK worker. The test names are LOCKED to preserve VALIDATION.md's verification map; the testDrainHook six-stage names are LOCKED for forward-compat. Range validation, two-signal escalation code, drain context, and force-exit seam are all shipped and unit-testable. Manual smoke covers the gap until Phase 7.1.

This is documented in 07-05-SUMMARY.md (lines 51-56 + 240-244) and 07-CONTEXT.md as a known forward-compat handoff, not a missing implementation.

---

_Verified: 2026-05-08T21:01:14Z_
_Verifier: Claude (gsd-verifier)_
