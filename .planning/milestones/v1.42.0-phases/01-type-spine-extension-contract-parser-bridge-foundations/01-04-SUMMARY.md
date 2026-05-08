---
phase: 01-type-spine-extension-contract-parser-bridge-foundations
plan: 04
subsystem: pkg/bridge
tags: [go, go.starlark.net, starlarkstruct, dsl-09, parse-03, parse-06, d-20, d-21, d-22, fresh-thread, max-execution-steps, print-hook, freeze-cascade, deterministic-iter]

requires:
  - 01-01 (typed error spine, package skeletons)
  - 01-02 (pkg/dag — *dag.CapturedLambda, ComputeLambdaID)
  - 01-03 (pkg/extension contract — referenced as a peer for the no-Temporal firewall pattern; not directly imported)

provides:
  - pkg/bridge/struct.go — ToStarlarkStruct(map[string]any) → *starlarkstruct.Struct (recursive, sorted keys, frozen lists)
  - pkg/bridge/value.go — FromStarlarkValue(starlark.Value) → (any, error) covering NoneType/String/Int/Float/Bool/*List/*Dict/*starlarkstruct.Struct
  - pkg/bridge/freeze.go — MustFreeze test helper for freeze-cascade assertions
  - pkg/bridge/lambda_globals.go — lambdaTimeGlobals (D-20 locked, 20 keys) + LambdaTimeGlobals() exporter (returns a fresh copy) + locally-implemented sumBuiltin
  - pkg/bridge/lambda_call.go — CallLambda(ctx, captured, state, opts) → (starlark.Value, error); fresh thread per call (Pitfall #1), MaxExecutionSteps default 10_000_000 (D-22), PrintSink default to slog.Default() (D-21), optional Cancel watchdog
  - DefaultMaxExecutionSteps constant (uint64 = 10_000_000)
  - CallOptions struct: MaxExecutionSteps, PrintSink, Logger, Cancel — all optional

affects:
  - 01-05 (parser): plan 05 imports bridge.LambdaTimeGlobals() to assert distinctness from parseTimeGlobals (PARSE-03 enforcement); uses bridge.ToStarlarkStruct in fixture tests; can run captured lambdas through bridge.CallLambda for end-to-end fixture verification
  - Phase 3 (interpreter): replaces the default PrintSink with a workflow.GetLogger.Info wrapper and provides a Cancel channel sourced from workflow.Context.Done() (via a workflow.Go watchdog inside the activity)
  - Phase 6 (example extensions): factory builtins return ActionRefs (not lambda-relevant), but lambda authors writing if_cond/script/for_each_parallel rely on the locked D-20 surface plus dot access through nested *starlarkstruct.Structs

tech-stack:
  added:
    - "go.starlark.net/starlarkstruct (used directly by ToStarlarkStruct + FromStarlarkValue for dot access)"
    - "go.starlark.net/syntax (referenced via syntax.PLUS for the local sumBuiltin's binary-op invocation)"
    - "log/slog (D-06 — default Logger fallback for PrintSink)"
  patterns:
    - "sort.Strings(keys) before iterating Go map — deterministic iteration order (Pitfall #3 closed at the bridge boundary)"
    - "Recursive descent for nested map/list values; nested maps become nested *starlarkstruct.Struct so ctx.req.repo_name dot-access works at any depth"
    - "Lists are converted to *starlark.List then Freeze()'d immediately — workflow state is read-only inside lambdas; freeze prevents the lambda mutating its own iteration source"
    - "Closure-init pattern for lambdaTimeGlobals: var lambdaTimeGlobals = func() starlark.StringDict { ... }() — the dict is constructed and frozen exactly once at package init"
    - "Locally-implemented sumBuiltin using starlark.Binary(syntax.PLUS, acc, v) for Int/Float promotion — go.starlark.net's Universe does NOT export sum despite D-20 listing it"
    - "Fresh *starlark.Thread per CallLambda invocation (allocated INSIDE the function body) — Pitfall #1 closed; concurrent test under -race verifies no cross-call leakage"
    - "Watchdog goroutine pattern: defer close(done); select-on-(Cancel, done) — Phase 1 uses native goroutine since CallLambda runs inside the activity, not the workflow goroutine; Phase 3 swaps to workflow.Go inside the workflow watchdog"
    - "PrintSink defaults to slog.Default().InfoContext with 'msg' and 'lambda_id' attributes — Phase 3 replaces with workflow logger"

key-files:
  created:
    - "pkg/bridge/struct.go (84 LOC) — ToStarlarkStruct + toStarlarkValue"
    - "pkg/bridge/value.go (84 LOC) — FromStarlarkValue"
    - "pkg/bridge/freeze.go (32 LOC) — MustFreeze test helper"
    - "pkg/bridge/lambda_globals.go (113 LOC) — lambdaTimeGlobals + LambdaTimeGlobals + sumBuiltin"
    - "pkg/bridge/lambda_call.go (105 LOC) — CallLambda + CallOptions + DefaultMaxExecutionSteps"
    - "pkg/bridge/struct_test.go (175 LOC) — 10 tests"
    - "pkg/bridge/value_test.go (132 LOC) — 11 tests"
    - "pkg/bridge/freeze_test.go (53 LOC) — 3 tests"
    - "pkg/bridge/lambda_globals_test.go (193 LOC) — 9 functions / 22 sub-tests"
    - "pkg/bridge/lambda_call_test.go (215 LOC) — 10 tests"
  modified: []
  deleted: []

key-decisions:
  - "Implemented sum locally as a *starlark.Builtin (sumBuiltin) — go.starlark.net's Universe does NOT export sum despite D-20 listing it. Verified against the published runtime keys: [False None True abs all any bool bytes chr dict dir enumerate fail float getattr hasattr hash int len list max min ord print range repr reversed set sorted str tuple type zip]. sum is absent. The locked D-20 set takes precedence over availability — adding sum locally preserves the spec without altering the locked list. The implementation uses starlark.Binary(syntax.PLUS, ...) for proper Int/Float promotion and supports the (iterable, start=0) signature."
  - "PrintSink default wraps the configured Logger (or slog.Default()) — InfoContext('starlark print', 'msg', payload, 'lambda_id', captured.ID). The lambda_id attribute makes log lines self-locating during Phase 3 debugging without needing to walk the call frame. Phase 3 will swap this for workflow.GetLogger.Info."
  - "Cancel watchdog runs in a native Go goroutine in Phase 1, not workflow.Go — the watchdog is launched from CallLambda which Phase 3 will invoke from inside the activity, NOT from inside the workflow goroutine. Activities can use native goroutines freely; the workflow.Go restriction applies only to code running inside the workflow's deterministic goroutine."
  - "TestLambdaTimeGlobals_PrintNotPredeclared replaces the originally-planned 'print should fail' test — Starlark's Universe always provides print as a language-level builtin and we cannot remove it without forking go.starlark.net. The D-21 contract (route via thread.Print) is honored regardless because Starlark's print always invokes thread.Print when set. Pinned by TestLambdaTimeGlobals_PrintRoutesViaThread which verifies the routing happens."
  - "starlark.Int values are compared by Int64() value rather than struct equality — starlark.Int holds an unexported impl pointer, so testify's deep-equal walks into pointer differences even when the integer values match. Tests cast to starlark.Int and compare via Int64() for stable assertions."
  - "Default Logger fallback chain: PrintSink → opts.Logger → slog.Default() — D-06 says 'library accepts *slog.Logger; defaults to slog.Default()'. The chain lets callers either supply a complete sink (for testing/Phase 3) or just a Logger (for early adopters who only want to plug in a handler)."

patterns-established:
  - "TDD discipline at the workflow level: each task writes test file → confirms RED build/run failure → implements source files → confirms GREEN test pass → commits in a single feat() commit. Three commits, three tasks, all under -race -count=1 before commit."
  - "Greenfield package starts with one doc.go (from plan 01-01) declaring the package; subsequent tasks add functional files alongside without touching doc.go. Keeps file responsibility crisp."
  - "Cross-package fixture builders (makeCapturedLambda) live in the test file, not in a shared testing helper, so each test reads top-to-bottom without crossing files."

requirements-completed: [DSL-09, PARSE-03, PARSE-06]

duration: 9min
completed: 2026-04-27
---

# Phase 01 Plan 04: pkg/bridge — state/lambda bridge Summary

**The state↔starlark conversion + lambda invocation layer Phase 3's interpreter calls on every IfCond/Script/ForEachParallel evaluation. Deterministic ToStarlarkStruct (sort.Strings before iteration), the D-20 locked lambdaTimeGlobals subset (20 keys, including a locally-implemented sum since go.starlark.net's Universe doesn't export it), and a CallLambda hot path that allocates a fresh *starlark.Thread per call, sets MaxExecutionSteps=10_000_000 (D-22), and routes Print via a configurable sink (D-21). 56 tests pass under `-race -count=1`; no Temporal imports anywhere in pkg/bridge.**

## Performance

- **Duration:** ~9 min
- **Started:** 2026-04-27T16:33:37Z
- **Completed:** 2026-04-27T16:42:07Z
- **Tasks:** 3 (all completed atomically)
- **Files created:** 10 (5 implementation + 5 test)
- **LOC:** ~1,186 total (~418 production, ~768 test)
- **Tests:** 43 top-level + 13 sub-tests = 56 tests in pkg/bridge

## Accomplishments

- **DSL-09** ToStarlarkStruct converts a Go state map into *starlarkstruct.Struct with deterministic key order — same input produces byte-equal Starlark output on re-conversion (verified by TestToStarlarkStruct_Deterministic, TestToStarlarkStruct_LargeMap with 100 keys, TestToStarlarkStruct_NestedListDeterminism). Dot access works at any nesting depth — `ctx.req.repo_name + "/issues"` evaluates correctly through CallLambda + ToStarlarkStruct + Starlark's native attribute resolution.
- **PARSE-03 (bridge half)** lambdaTimeGlobals exists as a frozen, package-private starlark.StringDict containing exactly the D-20 list. LambdaTimeGlobals() exports a fresh copy on every call — plan 05's parser will compare it against parseTimeGlobals to assert distinctness (PARSE-03 enforcement). The TestLambdaTimeGlobalsLocked stability gate fails the build if a future contributor adds or removes a key without updating the test, forcing an explicit decision per D-20.
- **PARSE-06** CallLambda allocates a fresh *starlark.Thread per invocation (Pitfall #1 closed). thread.SetMaxExecutionSteps(opts.MaxExecutionSteps) with DefaultMaxExecutionSteps=10_000_000 fallback (D-22). thread.Print routes to opts.PrintSink with a slog.Default() fallback chain (D-21). Optional Cancel channel runs through a watchdog goroutine that calls thread.Cancel.
- **No Temporal imports** anywhere in pkg/bridge — verified by `grep -r 'go.temporal.io' pkg/bridge/` returning zero matches.
- **Concurrent safety** verified: TestCallLambda_ConcurrentSafety runs 50 parallel CallLambda invocations on the same captured lambda with distinct states under -race; each produces its own correct result.
- 56 tests pass under `-race -count=1`; whole-repo `go build ./...` and `go test ./... -race` green.

## ToStarlarkStruct supported types

Phase 1 supports the types Phase 3's interpreter immediately needs:

| Go type           | Starlark type                  | Notes                                      |
| ----------------- | ------------------------------ | ------------------------------------------ |
| `nil`             | `starlark.None`                | —                                          |
| `string`          | `starlark.String`              | —                                          |
| `int`             | `starlark.Int` (via MakeInt)   | —                                          |
| `int64`           | `starlark.Int` (via MakeInt64) | Large values (1<<60+7) round-trip cleanly  |
| `float64`         | `starlark.Float`               | —                                          |
| `bool`            | `starlark.Bool`                | —                                          |
| `map[string]any`  | `*starlarkstruct.Struct`       | Recursive — preserves dot access at depth  |
| `[]any`           | `*starlark.List`               | Frozen at construction time                |
| anything else     | error                          | "unsupported type %T"                      |

Phase 6 may surface a real need for additional types (e.g., `time.Time`, `[]string` as a typed slice). The error path returns a clean message — extension is straightforward.

## D-20 LOCKED lambda-time globals (exactly 20 keys)

```
Type constructors / coercions (8): len  str  int  float  bool  list  dict  tuple
Failure (1):                       fail
Iteration helpers (11):            enumerate  zip  range  sorted  reversed
                                   min  max  sum  any  all  abs
```

**Adding or removing a key requires updating TestLambdaTimeGlobalsLocked**, which forces an explicit decision per D-20. The test is the API stability gate.

EXPLICITLY EXCLUDED by D-20 (verified by TestLambdaTimeGlobals_ForbiddenAbsent):
`print`, `set`, `time`, `random`, `getattr`, `load`, `os`, `open`, `read`, `write`, `now`, `uuid`, `http`.

Note: `print` is not bound in lambdaTimeGlobals, but Starlark's Universe still exposes it as a language-level intrinsic. The D-21 contract (route via thread.Print) is honored regardless because Starlark's `print` always invokes the thread's Print callback when set — verified by TestLambdaTimeGlobals_PrintRoutesViaThread.

## CallLambda invariants

| Invariant                      | Mechanism                                                  | Test                                          |
| ------------------------------ | ---------------------------------------------------------- | --------------------------------------------- |
| Fresh *starlark.Thread per call | `thread := &starlark.Thread{...}` inside CallLambda body  | TestCallLambda_FreshThread, _ConcurrentSafety |
| MaxExecutionSteps default      | `if opts.MaxExecutionSteps == 0 { ... = 10_000_000 }`     | TestCallLambda_DefaultMaxExecutionStepsConstant, _MaxExecutionStepsDefault |
| Print routing                  | `thread.Print = func(...) { opts.PrintSink(ctx, msg) }`   | TestCallLambda_PrintHookRouted, _DefaultPrintRoutesToSlog |
| Cancel watchdog (optional)     | goroutine: `select { case <-Cancel: thread.Cancel(...); case <-done: }` | TestCallLambda_CancelWatchdog                 |
| State conversion errors surface| Returned before thread allocation                          | TestCallLambda_StateConversionError           |
| Zero-value options work        | Each field's "zero means default" path                    | TestCallLambda_ZeroValueOptions               |

## Phase 3 integration points

**PrintSink → workflow.GetLogger:** Phase 3 supplies a CallOptions{ PrintSink: func(ctx, msg) { workflow.GetLogger(workflowCtx).Info(...) } }. The Phase 1 default fallback (slog.Default()) becomes irrelevant in the workflow context.

**Cancel → workflow.Context.Done():** Phase 3 uses a workflow.Go-spawned watchdog inside the workflow goroutine that closes a Go-native channel when workflow.Context.Done() fires; CallLambda (running inside the activity) consumes that channel via opts.Cancel. The Phase 1 native goroutine pattern stays intact at the activity level.

**MaxExecutionSteps:** Phase 3 may pass a parser option or a runtime constant; default 10_000_000 is the per-author override target.

## Custom sum builtin (deviation from "use starlark.Universe")

D-20 lists `sum` in the locked 20-key set. **go.starlark.net's runtime does NOT export `sum` in starlark.Universe** — verified empirically by enumerating Universe keys. The published runtime contains: `[False None True abs all any bool bytes chr dict dir enumerate fail float getattr hasattr hash int len list max min ord print range repr reversed set sorted str tuple type zip]`. Notably absent: `sum`.

The plan's <interfaces> sketch wrote `"sum": starlark.Universe["sum"]` which would have set a nil value and panicked at sd.Freeze() during package init. **[Rule 2 — Auto-add critical functionality]** auto-fixed this by implementing `sumBuiltin` locally as a `*starlark.Builtin`:

```go
var sumBuiltin = starlark.NewBuiltin("sum", func(...) (starlark.Value, error) {
    var iter starlark.Iterable
    var start starlark.Value = starlark.MakeInt(0)
    if err := starlark.UnpackArgs("sum", args, kwargs, "iterable", &iter, "start?", &start); err != nil {
        return nil, err
    }
    acc := start
    it := iter.Iterate()
    defer it.Done()
    var v starlark.Value
    for it.Next(&v) {
        next, err := starlark.Binary(syntax.PLUS, acc, v)
        if err != nil { return nil, fmt.Errorf("sum: %w", err) }
        acc = next
    }
    return acc, nil
})
```

Semantics match Python's `sum(iterable, start=0)`: starts with `start` (default Int(0)), folds using `+`. starlark.Binary(syntax.PLUS, ...) handles Int/Float promotion automatically — verified by TestLambdaTimeGlobals_SumWorks (Int+Int, Int+start, empty iterable, Float promotion).

This deviation **preserves the D-20 locked list verbatim** rather than altering it; the locked set takes precedence over Universe availability.

## Task Commits

| Task | Name                                                                | Commit  | Files                                                                  |
| ---- | ------------------------------------------------------------------- | ------- | ---------------------------------------------------------------------- |
| 1    | ToStarlarkStruct + FromStarlarkValue + MustFreeze (DSL-09)          | b494909 | struct.go, struct_test.go, value.go, value_test.go, freeze.go, freeze_test.go |
| 2    | lambdaTimeGlobals locked subset (D-20 / PARSE-03 bridge half)       | 5083088 | lambda_globals.go, lambda_globals_test.go                              |
| 3    | CallLambda — fresh thread, MaxExecutionSteps, Print hook (PARSE-06) | cf5022b | lambda_call.go, lambda_call_test.go                                    |

All three commits use the standard `git commit` (no `--no-verify`) since this plan ran as a single executor in Wave 2.

## Files Created/Modified

**Source (5 files, ~418 LOC):**
- `pkg/bridge/struct.go` — ToStarlarkStruct + toStarlarkValue (recursive, sorted, frozen lists)
- `pkg/bridge/value.go` — FromStarlarkValue (Phase 3's interpreter consumes lambda outputs through this)
- `pkg/bridge/freeze.go` — MustFreeze test helper for plan 05's parser-side freeze cascade tests
- `pkg/bridge/lambda_globals.go` — lambdaTimeGlobals (D-20 locked, frozen) + LambdaTimeGlobals exporter + sumBuiltin
- `pkg/bridge/lambda_call.go` — CallLambda + CallOptions + DefaultMaxExecutionSteps

**Test (5 files, ~768 LOC):**
- `pkg/bridge/struct_test.go` — 10 tests covering basic types, dot access, determinism, large map, list freezing, unsupported type, nil, integer support, float, nested list determinism
- `pkg/bridge/value_test.go` — 11 tests covering each supported Starlark type + nested struct + unsupported type + non-string dict key
- `pkg/bridge/freeze_test.go` — 3 tests covering Dict, List, and a non-mutable type's defensive path
- `pkg/bridge/lambda_globals_test.go` — 9 functions / 22 sub-tests covering locked list, forbidden absent, all values non-nil, copy semantics, integration sanity, print not predeclared, print routes via thread, sum works, time not available
- `pkg/bridge/lambda_call_test.go` — 10 tests covering fresh thread, dot access, print routing, default slog routing, MaxExecutionSteps, default constant, concurrent safety (50 parallel calls), cancel watchdog, zero-value options, state conversion error

**Module modifications:** None — pkg/bridge depends only on the existing direct deps (go.starlark.net, log/slog stdlib). No transitive growth.

## Decisions Made

(Mirrored in `key-decisions:` frontmatter; expanded here.)

- **sum as a local *starlark.Builtin** — see "Custom sum builtin" section above. The locked D-20 list takes precedence over Universe availability; we implement what's missing rather than altering the spec.
- **PrintSink default wraps Logger with lambda_id attribute** — `slog.Default().InfoContext(ctx, "starlark print", "msg", payload, "lambda_id", captured.ID)`. The lambda_id makes log lines self-locating during Phase 3 debugging. Phase 3 swaps the InfoContext call for `workflow.GetLogger(workflowCtx).Info`.
- **Cancel watchdog uses a native goroutine in Phase 1** — CallLambda runs inside the activity (not the workflow goroutine), so a native goroutine is correct. Phase 3 spawns the watchdog with `workflow.Go` from inside the workflow goroutine; that goroutine signals a Go-native channel that this Phase 1 implementation consumes via `opts.Cancel`.
- **TestLambdaTimeGlobals_PrintNotPredeclared replaces the originally-planned 'print should fail' test** — Starlark's Universe unavoidably exposes `print` as an intrinsic. The D-21 contract (route via thread.Print) is honored regardless. The test pins dict membership; TestLambdaTimeGlobals_PrintRoutesViaThread pins the routing.
- **starlark.Int comparisons via Int64() not testify deep-equal** — starlark.Int's unexported impl pointer breaks `assert.Equal`; comparisons cast to starlark.Int and call Int64() for stable assertions.
- **CallOptions{} (zero value) is a valid argument** — every field has a "zero means default" interpretation. Avoids forcing callers to construct a fully-populated options struct just to invoke the lambda.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 — Auto-add] Implemented `sum` as a local *starlark.Builtin**
- **Found during:** Task 2 — the dict initialization panicked at `sd.Freeze()` because `starlark.Universe["sum"]` returned nil.
- **Issue:** D-20's locked 20-key list includes `sum`, but go.starlark.net's runtime does NOT export `sum` in Universe (verified by enumerating Universe keys: `sum` is absent).
- **Fix:** Added `var sumBuiltin = starlark.NewBuiltin(...)` implementing the standard Python `sum(iterable, start=0)` semantics via `starlark.Binary(syntax.PLUS, ...)` for Int/Float promotion. Bound it in lambdaTimeGlobals as `"sum": sumBuiltin` instead of `starlark.Universe["sum"]`.
- **Files modified:** `pkg/bridge/lambda_globals.go` (added sumBuiltin, syntax import); `pkg/bridge/lambda_globals_test.go` (added TestLambdaTimeGlobals_SumWorks).
- **Commit:** 5083088

**2. [Rule 1 — Bug] Replaced TestLambdaTimeGlobals_PrintNotAvailable test**
- **Found during:** Task 2 — the test asserted `print("hello")` would fail with "undefined" when evaluated against LambdaTimeGlobals(), but Starlark's Universe always exposes print as a language-level intrinsic. The test was wrong, not the implementation.
- **Issue:** The plan's <action> sketch assumed predeclared globals could exclude Universe builtins. They cannot — predeclared adds on top of Universe.
- **Fix:** Replaced the test with two more accurate assertions:
  - `TestLambdaTimeGlobals_PrintNotPredeclared` — pins that `print` is absent from the lambdaTimeGlobals dict (the membership invariant).
  - `TestLambdaTimeGlobals_PrintRoutesViaThread` — pins that print() output flows through thread.Print (the D-21 routing contract).
- **Files modified:** `pkg/bridge/lambda_globals_test.go`
- **Commit:** 5083088

### Auto-added Critical Functionality

(Captured under "1." above — sum builtin.)

---

**Total deviations:** 2 auto-fixed (1 missing critical, 1 spec-bug in test only)
**Impact on plan:** No scope change. Both deviations preserve the plan's intent (D-20 locked list, D-21 routing contract); they correct discrepancies between the plan's <action> sketch and the actual go.starlark.net runtime.

## Issues Encountered

- **`starlark.Int` deep-equal trap** — testify's `assert.Equal` walks the unexported `impl` pointer inside `starlark.Int`, so two equal-valued Ints fail equality when constructed differently. Resolved by casting to `starlark.Int` and comparing via `Int64()` in tests. Documented as a key decision so future test authors don't relearn it.

## Authentication Gates

None — this plan is pure-Go contract definition with no external services.

## User Setup Required

None — every test runs offline against in-memory Starlark fixtures.

## Known Stubs

None. Every contract has at least one passing automated test under `-race`; nothing is deferred to a follow-up plan within the bridge package.

The Phase 3 integration points (workflow logger swap, workflow.Context.Done() Cancel wiring) are NOT stubs in pkg/bridge — they are configuration injection points exposed via CallOptions, with sensible Phase 1 defaults that work for unit tests today.

## Next Phase Readiness

- **Plan 01-05 (parser integration / fixture-based tests) is unblocked.** Plan 05 can:
  - Import `bridge.LambdaTimeGlobals()` and assert distinctness from its own `parseTimeGlobals` (PARSE-03 enforcement)
  - Use `bridge.ToStarlarkStruct` to materialize state for parse-time literal-eval tests
  - Use `bridge.CallLambda` to verify captured lambdas evaluate correctly during fixture-based tests
  - Use `bridge.MustFreeze` to assert freeze cascades on parser-side custom values
- **Phase 3 (interpreter) contract is locked at the bridge surface.** The PrintSink and Cancel hooks are the precise integration points Phase 3 wires.
- **Phase 6 (example extensions)** is unaffected by this plan (extensions return ActionRefs from factories — not lambda-relevant). Extension authors do need to be aware of the D-20 locked surface so they can write `if_cond=lambda ctx: ctx.x > 0` style code, but that's plan 05's concern (parser surface).

No blockers. No concerns.

## Verification Summary

```
go build ./pkg/bridge/...                     → exit 0
go vet ./pkg/bridge/...                       → exit 0
go test ./pkg/bridge/... -race -count=1       → exit 0
go build ./...                                → exit 0
go test ./... -race -count=1                  → exit 0
grep -r 'go.temporal.io' pkg/bridge/          → 0 matches  (architectural firewall holds)
grep -r 'mikelalcon/skytime/pkg/parser' pkg/bridge/ → 0 matches  (downward import flow)
```

Test counts:
- `pkg/bridge/...` total tests: 56 (43 top-level functions + 13 sub-tests in TestLambdaTimeGlobals_ForbiddenAbsent)
- All tests pass under `-race -count=1` with zero data races

## Self-Check: PASSED

Verified all claimed files exist and all claimed commits are present:

- FOUND: pkg/bridge/struct.go
- FOUND: pkg/bridge/struct_test.go
- FOUND: pkg/bridge/value.go
- FOUND: pkg/bridge/value_test.go
- FOUND: pkg/bridge/freeze.go
- FOUND: pkg/bridge/freeze_test.go
- FOUND: pkg/bridge/lambda_globals.go
- FOUND: pkg/bridge/lambda_globals_test.go
- FOUND: pkg/bridge/lambda_call.go
- FOUND: pkg/bridge/lambda_call_test.go
- FOUND: commit b494909 (Task 1)
- FOUND: commit 5083088 (Task 2)
- FOUND: commit cf5022b (Task 3)
- VERIFIED: `go build ./pkg/bridge/...` exits 0
- VERIFIED: `go vet ./pkg/bridge/...` exits 0
- VERIFIED: `go test ./pkg/bridge/... -race -count=1` exits 0 with 56 tests passing
- VERIFIED: `grep -r 'go.temporal.io' pkg/bridge/` returns nothing — no Temporal imports anywhere in pkg/bridge
- VERIFIED: lambdaTimeGlobals contains exactly 20 keys (TestLambdaTimeGlobalsLocked passes)
- VERIFIED: TestCallLambda_ConcurrentSafety passes under -race with 50 parallel calls
- VERIFIED: TestToStarlarkStruct_Deterministic passes — same map twice produces equal output

---
*Phase: 01-type-spine-extension-contract-parser-bridge-foundations*
*Completed: 2026-04-27*
