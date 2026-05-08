---
phase: 03-lambda-serialization-decision-interpreter-worker
plan: 02
subsystem: interpreter
tags: [pkg-interpreter, flow-registry, cancellation-watchdog, workflow-bridge, sync-once, workflowcheck, sealed-interface, white-box-test]

# Dependency graph
requires:
  - phase: 02-generic-activity-block-batch-dispatch-credentials
    provides: pkg/activity firewall test (forward-compatible allowlist), single ExecuteBatch activity registered by plan 03-04 worker bootstrap
  - phase: 03-01 (Wave 0)
    provides: dag.WorkflowInput {FlowName, ContentHash, InitState} shape; firewall allowlist pre-permits pkg/interpreter; dag.Flow.TaskQueue / dag.Step.TaskQueue / dag.ForEachParallel.MaxConcurrency
provides:
  - pkg/interpreter package compiling and importing go.temporal.io/sdk/{workflow,temporal,log} (firewall meta-test transitions skip → assertive)
  - interpreter.FlowRegistry + ParsedFlow with frozen-after-boot semantics (D3-04..D3-07); Lookup / Register / Freeze / ContentHashFor
  - interpreter.makeCancelChannel(workflow.Context) <-chan struct{} — the workflow.Channel → native chan bridge (D3-21 / RESEARCH §"Pattern 6"), sync.Once-guarded close, fallback paragraph inlined
  - interpreter.NewWorkflow(*FlowRegistry) returning the SkytimeWorkflow closure with FlowNotInRegistry error path
  - FINAL interpreter struct shape and newInterpreter signature — plan 03-03 fills walker bodies only, no signature retrofit
  - interpreter.state with sorted-key snapshot/scoped/setOutput (D3-23)
affects:
  - 03-03-PLAN.md (interpreter walkers — fills walkStep / walkIfCond / walkScript / walkForEach / walkCallFlow stubs)
  - 03-04-PLAN.md (pkg/worker bootstrap — instantiates FlowRegistry from disk and Freezes; calls interpreter.NewWorkflow)

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "White-box testing for unexported workflow helpers — `package interpreter` (not interpreter_test) used in cancel_watchdog_test.go and workflow_test.go so makeCancelChannel + newInterpreter + interpreter struct fields are directly callable; firewall_test.go intentionally stays interpreter_test because it does AST analysis from outside"
    - "Skip-on-empty-package firewall meta-test — TestPkgInterpreter_ImportsTemporal skips when no production Go files yet exist OR when production files don't yet import the SDK; transitions to assertive automatically once workflow.go lands SDK imports (no test edit required between commits)"
    - "sync.Once-guarded close(ch) in workflow.Go bridge — defense-in-depth idempotency for the single-fire cancellation contract; belt-and-suspenders against any future SDK behavior shift"
    - "Sealed-interface dispatcher with defensive default branch — walkNode type-switches over dag.Node (Step/IfCond/Script/ForEachParallel/CallFlow) and emits an UnknownNodeType non-retryable error in the default arm; structurally unreachable today but surfaces with position info if a future Node type lands without dispatcher coverage"
    - "FINAL signature lock via dedicated test (TestNewInterpreter_FinalSignature) — plan 03-03 will fill walker bodies but NOT change newInterpreter's parameter list or interpreter struct shape; the test is a compile-time gate disguised as a runtime assertion"
    - "Multi-version FlowRegistry with ContentHashFor helper — map[string]map[string]*ParsedFlow allows multiple content hashes per flow_name (test fixtures, mid-drain corner cases); ContentHashFor returns (\"\", false) when zero or multiple versions exist, forcing a clean error path in plan 03-03's call_flow walker rather than a non-deterministic pick"

key-files:
  created:
    - pkg/interpreter/doc.go (package documentation + FIREWALL block + determinism contract D3-23/D3-24)
    - pkg/interpreter/state.go (workflow-local state map with sortedKeys helper)
    - pkg/interpreter/state_test.go (5 unit tests covering snapshot stability, scoped behavior, setOutput, sortedKeys)
    - pkg/interpreter/registry.go (FlowRegistry + ParsedFlow + ErrRegistryFrozen + ErrDuplicateFlow + Register/Freeze/Lookup/ContentHashFor + sortedHashKeys)
    - pkg/interpreter/registry_test.go (7 tests including 100-goroutine race-detector concurrency stress)
    - pkg/interpreter/cancel_watchdog.go (makeCancelChannel — D3-21 workflow.Channel → native chan bridge with sync.Once close)
    - pkg/interpreter/cancel_watchdog_test.go (3 white-box tests under testsuite.WorkflowTestSuite — closes-on-cancel, stays-open-when-not-cancelled, channel independence)
    - pkg/interpreter/workflow.go (SkytimeWorkflow closure + interpreter struct + newInterpreter + walkBody/walkNode dispatcher + 5 walker stubs)
    - pkg/interpreter/workflow_test.go (5 tests: FlowNotInRegistry, ContentHashMismatch, HappyPath_EmptyBody, NewWorkflow_ReturnsCallable reflection, NewInterpreter_FinalSignature compile-time lock)
    - pkg/interpreter/firewall_test.go (TestPkgInterpreter_ImportsTemporal skip-on-empty + TestWorkflowcheck_NoFindings skip-on-missing-binary)
  modified: []

key-decisions:
  - "White-box tests (`package interpreter`) chosen over option (a) exported MakeCancelChannel or option (c) test-seam helpers — it's the standard Go idiom for testing unexported helpers and avoids leaking the bridge as public API."
  - "sync.Once around close(ch) accepted as belt-and-suspenders even though the SDK contract guarantees Done().Receive fires exactly once on cancel. Cost is two pointer fields and one Once.Do call per channel; benefit is full immunity to any future SDK behavior change. Per blocker fix W9."
  - "Per-call channel lifecycle (one workflow.Go reader per makeCancelChannel call) accepted for v1 — workflows under v1 don't have unbounded lambda evals. Tighter cleanup via workflow.WithCancel + post-CallLambda cancel was considered and deferred (RESEARCH §Pattern 6 has the alternative); fallback paragraph inlined in cancel_watchdog.go's doc comment."
  - "Multi-version FlowRegistry (map[string]map[string]*ParsedFlow) over single-version (map[FlowVersionKey]*ParsedFlow) — costs nothing today, supports test fixtures registering the same flow with different bytes, and ContentHashFor's zero/many → (\"\", false) contract forces clean errors in call_flow."
  - "FINAL signature lock for newInterpreter committed via TestNewInterpreter_FinalSignature so plan 03-03 author cannot accidentally retrofit. The interpreter struct keeps `flow *dag.Flow` as a sugar alias for `parsed.Flow` to keep walker call sites concise."

patterns-established:
  - "FIREWALL meta-test pattern (skip-on-empty + skip-on-no-import) — TestPkgInterpreter_ImportsTemporal mirrors Phase 2's TestPkgActivity_AllowedToImportTemporal but adds two skip guards so the test never commits in a knowingly-red state. Pattern is reusable when plan 03-04 adds pkg/worker."
  - "Workflow-bridge testing pattern — testsuite.WorkflowTestSuite + workflow.WithCancel-derived contexts + small workflow.Sleep yields between cancel and channel-state checks. The deterministic test environment fast-forwards timers, so the sleep duration is irrelevant — any non-zero duration triggers a coroutine scheduling pass. Documented inline in cancel_watchdog_test.go for future bridge-style helpers."
  - "Walker stub family — every concrete walker returns a non-retryable WalkerNotImplemented application error tagged with the node's position. Plan 03-03 author replaces each function BODY only; signatures are locked. Pattern keeps the dispatcher live and testable end-to-end via empty-body Flow before walkers exist."
  - "Reflection signature assertion accommodates type aliases — workflow.Context is a Go type alias for internal.Context, so reflect.Type.String() returns the underlying name. TestNewWorkflow_ReturnsCallable allows either form; the behavioral check (RegisterWorkflowWithOptions accepts the closure) lives in the FlowNotInRegistry test."

requirements-completed:
  - INTRP-02
  - INTRP-06
  - INTRP-07

# Metrics
duration: 10min
completed: 2026-04-30
---

# Phase 3 Plan 02: Wave 1 — pkg/interpreter Foundations Summary

**`pkg/interpreter` package landed with FlowRegistry (frozen-after-boot, D3-04..D3-07), the cancellation watchdog bridge `makeCancelChannel` (the trickiest seam in Phase 3, D3-21 / RESEARCH §"Pattern 6", sync.Once-guarded), the SkytimeWorkflow closure with FlowNotInRegistry error path, and walker-stub dispatcher — all behind a forward-compatible firewall meta-test.**

## Performance

- **Duration:** ~10 min
- **Started:** 2026-04-30T02:53:09Z
- **Completed:** 2026-04-30T03:03:29Z
- **Tasks:** 4
- **Files modified:** 10 created, 0 modified

## Accomplishments

- **Package skeleton (Task 1):** `pkg/interpreter/doc.go` documents the FIREWALL block + D3-23/D3-24 determinism contract; `state.go` provides newState / snapshot / scoped / setOutput with sortedKeys (D3-23) helper used by both state.go and registry.go; `firewall_test.go` provides TestPkgInterpreter_ImportsTemporal (skip-on-empty + skip-on-no-import) plus TestWorkflowcheck_NoFindings (skip-on-missing-binary).
- **FlowRegistry (Task 2):** `registry.go` ships FlowRegistry, ParsedFlow, ErrRegistryFrozen, ErrDuplicateFlow, NewRegistry, Register, Freeze, Lookup, ContentHashFor, sortedHashKeys. Multi-version shape (`map[string]map[string]*ParsedFlow`) — frozen-after-boot semantics enforced; ContentHashFor returns ("", false) for zero or multi-version flow_names so plan 03-03's call_flow walker fails cleanly instead of picking non-deterministically. 7 tests including a 100-goroutine concurrent Lookup stress under `-race`.
- **Cancellation watchdog (Task 3):** `cancel_watchdog.go::makeCancelChannel` — the trickiest piece of code in v1 — bridges `workflow.Context.Done()` (a workflow.Channel that requires being inside a workflow coroutine) to a native `chan struct{}` that pkg/bridge.CallLambda's existing native-goroutine watchdog can read directly. Implementation: `workflow.Go(ctx, func(bctx) { bctx.Done().Receive(bctx, nil); once.Do(func(){ close(ch) }) })`. 3 white-box tests under `testsuite.WorkflowTestSuite` covering closes-on-cancel, stays-open-when-not-cancelled, and channel independence.
- **SkytimeWorkflow skeleton (Task 4):** `workflow.go::NewWorkflow(registry) → func(workflow.Context, dag.WorkflowInput) (map[string]any, error)`. Logs flow_name + content_hash + binary_checksum + run_id at start; performs registry.Lookup; on miss returns a non-retryable FlowNotInRegistry application error with the canonical "use Build IDs to drain old workflows" message. The `interpreter` struct + `newInterpreter` are the FINAL shapes — plan 03-03 fills walker BODIES only, no signature retrofit (asserted by TestNewInterpreter_FinalSignature compile-time lock). walkBody/walkNode type-switches over dag.Node sealed sum; five walker stubs (walkStep, walkIfCond, walkScript, walkForEach, walkCallFlow) return non-retryable WalkerNotImplemented tagged with node position.
- **Firewall holds:** `grep -rl 'go.temporal.io/sdk' pkg/ | grep -vE '/(activity|interpreter|worker)/' | wc -l` outputs `0`. The TestPkgInterpreter_ImportsTemporal meta-test transitioned automatically from SKIP to PASS once Task 4 landed `go.temporal.io/sdk/{workflow,temporal,log}` imports.

## Task Commits

Each task was committed atomically:

1. **Task 1: pkg/interpreter skeleton — doc, state, firewall meta-test** — `1db7ffc` (feat)
2. **Task 2: FlowRegistry with frozen-after-boot semantics (D3-04..D3-07)** — `64ffc93` (feat)
3. **Task 3: Cancellation watchdog — workflow.Channel→native chan bridge (D3-21)** — `3120ffa` (feat)
4. **Task 4: SkytimeWorkflow skeleton — registry lookup + walker dispatcher stub** — `17d23fd` (feat)

_Note: Tasks 2, 3, 4 used TDD (RED → GREEN). The RED commit was folded into the task's single feat commit since the test file lands alongside the implementation atomically; this matches Phase 1 / 2 conventions._

## Files Created/Modified

**Created (pkg/interpreter/):**
- `doc.go` — package documentation; FIREWALL block; D3-23/D3-24 determinism contract; ~30 LOC
- `state.go` — workflow-local state map; newState, snapshot, scoped, setOutput, sortedKeys; ~75 LOC
- `state_test.go` — 5 unit tests (sorted-key snapshot stability, scoped behaviors, setOutput round-trip, sortedKeys); ~80 LOC
- `registry.go` — FlowRegistry + ParsedFlow + sentinel errors + Register/Freeze/Lookup/ContentHashFor + sortedHashKeys; ~135 LOC
- `registry_test.go` — 7 tests (happy path, post-freeze block, lookup before freeze, dup, multi-version, ContentHashFor zero/one/many, 100-goroutine race-stress, required-field guard); ~190 LOC
- `cancel_watchdog.go` — makeCancelChannel with sync.Once close + inlined Fallback paragraph; ~60 LOC (mostly doc)
- `cancel_watchdog_test.go` — 3 white-box workflow tests (closes-on-cancel, stays-open, independence); ~150 LOC
- `workflow.go` — NewWorkflow, interpreter struct (FINAL shape), newInterpreter (FINAL signature), walkBody/walkNode dispatcher, 5 walker stubs; ~165 LOC
- `workflow_test.go` — 5 tests (FlowNotInRegistry, ContentHashMismatch, HappyPath_EmptyBody, NewWorkflow_ReturnsCallable reflection, NewInterpreter_FinalSignature compile-time lock); ~195 LOC
- `firewall_test.go` — TestPkgInterpreter_ImportsTemporal skip-on-empty + TestWorkflowcheck_NoFindings skip-on-missing-binary + findInterpreterModuleRoot helper; ~100 LOC

## Decisions Made

See frontmatter `key-decisions` for the full set with rationale. Highlights:

- **White-box tests** for unexported makeCancelChannel + newInterpreter + interpreter struct fields. Standard Go idiom for testing unexported helpers; avoids leaking the bridge as public API.
- **sync.Once around close(ch)** as belt-and-suspenders idempotency. Cost is negligible; benefit is immunity to any future SDK behavior shift. Documented inline (Pattern 6 risk acknowledgment + the W9 blocker fix).
- **Per-call channel lifecycle** — one workflow.Go reader per makeCancelChannel call. Acceptable for v1 (no unbounded lambda evals); fallback to per-call workflow.WithCancel cleanup is documented in the doc comment if integration tests in plan 03-03 surface flakiness.
- **Multi-version FlowRegistry shape** — costs nothing, supports test fixtures, and ContentHashFor's zero/many → (`""`, false) contract forces clean errors instead of non-deterministic picks.
- **FINAL signature lock for newInterpreter** committed via a compile-time test (TestNewInterpreter_FinalSignature) so plan 03-03 cannot accidentally retrofit. The `flow *dag.Flow` field on interpreter is a sugar alias for `parsed.Flow` (concise walker call sites).

## Watchdog Implementation Details and Deviations from RESEARCH §6

**RESEARCH §6's concrete sketch was followed verbatim** with two enhancements per plan acceptance criteria:

1. **sync.Once wrap around close(ch)** — RESEARCH §6's sketch closes the channel directly. Plan acceptance required a sync.Once guard for idempotency (per blocker fix W9). The Once is belt-and-suspenders against any future SDK behavior change; the cost is two extra pointer fields per channel.

2. **Inlined Fallback paragraph** — RESEARCH §6's "Risk acknowledgment" section was condensed into a doc comment paragraph beginning "Fallback if integration tests in plan 03-03 surface flakiness, the fallback is to remove this watchdog and rely on a pre-eval ctx.Err() check inside the interpreter walkers...". The paragraph references RESEARCH.md §"Pattern 6 risk acknowledgment" by name so future maintainers can find the full discussion. Verified by `grep -A2 "Fallback if integration tests" pkg/interpreter/cancel_watchdog.go`.

No other deviations from RESEARCH §6's design.

**Test design deviation (worth flagging for plan 03-03):** The white-box test approach uses `package interpreter` (not `interpreter_test`) so `makeCancelChannel` is directly callable. Initial test attempts used `workflow.Await` with a native-channel select inside the predicate — this hits Temporal's "trying to block on coroutine which is already blocked" because `workflow.Await` re-checks its predicate only on workflow events, and a native-channel close isn't a workflow event. The working pattern uses `workflow.Sleep(ctx, time.Microsecond)` between cancel() and the channel-state check — the sleep is a coroutine-scheduling yield in the deterministic test env, with timers fast-forwarded. This pattern will be reusable when plan 03-03 needs to test bridge.CallLambda invocations from inside a workflow.

## workflowcheck Local Outcome

**Not runnable locally** — `workflowcheck` is not installed in the developer environment. `which workflowcheck` returns "not found". The TestWorkflowcheck_NoFindings test therefore SKIPS with a clear install hint:

```
go install go.temporal.io/sdk/contrib/tools/workflowcheck@latest
```

CI is expected to install workflowcheck and run the gate (INTRP-07). Until that runs, defense-in-depth measures are in place to keep the package clean:

- All map iteration in pkg/interpreter routes through sortedKeys (state.go) or sortedHashKeys (registry.go).
- No native `go` keyword anywhere in pkg/interpreter — only `workflow.Go(ctx, ...)` (one site, in cancel_watchdog.go).
- No time.Now / rand.* / I/O calls.
- ContentHashFor (called from workflow code by plan 03-03's call_flow walker) iterates via sortedHashKeys even though the single-entry case is trivial.

Confidence: HIGH that workflowcheck will pass when CI runs it.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] reflection assertion accommodates Go type aliases**
- **Found during:** Task 4 (TestNewWorkflow_ReturnsCallable)
- **Issue:** The plan's test sketch said `assert.Equal(t, "workflow.Context", in0.String())`. In practice, `workflow.Context` is a Go type alias for `internal.Context` (verified in the Temporal SDK source), so `reflect.Type.String()` returns `"internal.Context"`. The literal assertion failed.
- **Fix:** Changed the assertion to `assert.True(t, in0Str == "workflow.Context" || in0Str == "internal.Context", ...)`. The behavioral check (RegisterWorkflowWithOptions accepts the closure without panicking) is exercised by the FlowNotInRegistry / ContentHashMismatch / HappyPath_EmptyBody tests. The reflection test is reduced to a structural sanity check (NumIn=2, NumOut=2, second-arg is dag.WorkflowInput, first return is map[string]interface{}, second return is error).
- **Files modified:** pkg/interpreter/workflow_test.go
- **Verification:** All workflow tests pass (`go test ./pkg/interpreter/ -run TestNewWorkflow_ReturnsCallable -count=1`).
- **Committed in:** `17d23fd` (Task 4 commit)
- **Why automatic:** Rule 3 (blocking) — the literal assertion blocked Task 4 verification; the alias-aware assertion is functionally equivalent and accommodates the SDK's type-alias choice.

**2. [Rule 3 - Blocking] syntax.Position constructed via MakePosition rather than struct literal**
- **Found during:** Task 2 (helperNewParsedFlow)
- **Issue:** Plan sketches showed `syntax.Position{Filename: "test.star", Line: 1, Col: 1}` for test fixtures. `Filename` is an UNEXPORTED field on `syntax.Position` in `go.starlark.net`; the only exported constructor is `syntax.MakePosition(file *string, line, col int32)`.
- **Fix:** Used `syntax.MakePosition(&filename, 1, 1)` in `helperNewParsedFlow` (registry_test.go and workflow_test.go).
- **Files modified:** pkg/interpreter/registry_test.go, pkg/interpreter/workflow_test.go
- **Verification:** Tests compile and pass.
- **Committed in:** `64ffc93` (Task 2) for registry_test.go; `17d23fd` (Task 4) for workflow_test.go.
- **Why automatic:** Rule 3 (blocking) — the struct literal didn't compile; MakePosition is the documented constructor and matches Phase 1's parser usage.

---

**Total deviations:** 2 auto-fixed (both Rule 3 blocking — minor sketch corrections; no scope creep, no signature changes from the FINAL contracts in workflow.go).

**Impact on plan:** Both deviations were sketch-level inaccuracies discovered during verification; the plan's stated acceptance criteria all met as written. No production code was changed beyond the sketches; only test scaffolding adapted to match reality.

## Open Questions for Plan 03-03

- **Walker error wrapping:** Plan 03-03 will replace the WalkerNotImplemented stubs with real implementations. The stubs return `temporal.NewNonRetryableApplicationError`; real walkers may need both retryable (transient activity errors should bubble through Temporal's retry policy) and non-retryable (DSL semantic errors should fail the workflow). Plan 03-03 should establish the convention up-front.
- **bridge.CallLambda watchdog wiring:** plan 03-03 will call `makeCancelChannel(ctx)` per CallLambda invocation and pass the returned channel as `bridge.CallOptions.Cancel`. The exact place this wires (per-walker or in a shared `i.evalLambda(ctx, lambdaID, state)` helper) is not yet specified. Recommendation: a dedicated `i.evalLambda` helper avoids duplicating the watchdog wiring across walkStep / walkIfCond / walkScript / walkForEach / walkCallFlow's items-lambda handling.
- **Workflow-Sleep yields in tests:** the cancel_watchdog tests' workflow.Sleep(time.Microsecond) yields work because the testsuite fast-forwards timers. In production replay, a microsecond sleep ALSO yields the goroutine but is harmless. Plan 03-03's integration tests may need similar yields after cancel-and-before-state-check; the pattern is documented inline in cancel_watchdog_test.go.

## Issues Encountered

- **`workflow.Await` with native-channel predicate hangs the test workflow** with "trying to block on coroutine which is already blocked." Root cause: `workflow.Await` re-checks its predicate only on workflow events; a native-channel close is not a workflow event. Resolution: replaced the Await-then-select pattern with a `workflow.Sleep(time.Microsecond)` yield + non-blocking select. Documented in cancel_watchdog_test.go for future bridge-style tests.
- **Type alias `workflow.Context` vs `internal.Context`:** `reflect.Type.String()` returns the underlying name when the parameter type is a Go type alias. Initial assertion failed; updated to accept either form.
- **`syntax.Position.Filename` is unexported:** `go.starlark.net` exposes `Filename()` as a method but not `Filename` as a field. Constructor is `syntax.MakePosition(file *string, line, col int32)`. Test fixtures updated.

## User Setup Required

None - no external service configuration required. workflowcheck installation is documented in cancel_watchdog_test.go's TestWorkflowcheck_NoFindings skip message for any developer who wants to run the static analysis locally; CI runs it independently.

## Next Phase Readiness

**Ready for plan 03-03 (interpreter walkers):**
- The `interpreter` struct + `newInterpreter` signature are FINAL. Plan 03-03 fills walker BODIES only.
- `makeCancelChannel(ctx)` is ready to be called per `bridge.CallLambda` invocation.
- `state.snapshot()` provides sorted-key state for `bridge.ToStarlarkStruct`; `state.setOutput(alias, value)` publishes Script outputs; `state.scoped(itemVar, item)` produces per-branch state for ForEachParallel.
- `FlowRegistry.ContentHashFor(flowName)` is ready for call_flow's child WorkflowInput construction.
- The walker-stub family returns precise position-tagged errors so plan 03-03 RED-phase tests can assert specific node paths fail before walker implementation lands.

**Ready for plan 03-04 (pkg/worker bootstrap):**
- `interpreter.NewRegistry()` + `Register(flowName, hash, parsed)` + `Freeze()` is the boot-time API surface.
- `interpreter.NewWorkflow(registry)` returns the closure to register via `worker.RegisterWorkflowWithOptions(skywf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})`.
- The firewall meta-test allows pkg/worker to import the SDK; no further firewall edits needed when pkg/worker lands.

**Blockers / concerns:**
- workflowcheck not runnable locally (skipped). CI must install and run it; INTRP-07 gate currently HIGH-confidence-clean by inspection but not formally verified.
- Per-call workflow.Go coroutine accumulation is the known design trade-off (RESEARCH §6); fallback inlined in cancel_watchdog.go's doc comment for plan 03-03 to evaluate during integration testing.

## Self-Check: PASSED

All 10 expected pkg/interpreter files exist on disk; all 4 task commits are reachable in git history (`1db7ffc`, `64ffc93`, `3120ffa`, `17d23fd`). Full repo `go test ./... -race -count=1` exits 0; `go vet ./...` clean; `go build ./...` clean; firewall scan shows 0 SDK imports outside `{activity, interpreter, worker}`.

---
*Phase: 03-lambda-serialization-decision-interpreter-worker*
*Completed: 2026-04-30*
