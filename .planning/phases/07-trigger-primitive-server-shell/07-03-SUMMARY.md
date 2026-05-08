---
phase: 07-trigger-primitive-server-shell
plan: 03
subsystem: parser
tags: [trigger, parser-builtin, finalize-chain, free-var-visitor, req-walker, trigger-time-globals, cross-file-resolution, byte-identical-duplicates]

# Dependency graph
requires:
  - phase: 07-01
    provides: dag.Trigger struct, dag.TriggerSource minimal interface
  - phase: 07-02
    provides: extension.TriggerSource sealed interface, extension.FakeTriggerSource test stub, kind-keyed factory registry
provides:
  - "trigger(flow=, source=, map=, idempotency_key=, credential=) Starlark builtin (parser-side, parse-time only, no I/O)"
  - "Parser.Triggers() accessor — deterministically sorted []*dag.Trigger by Source.Kind, FlowName, Pos string"
  - "Parser.TriggerWarnings() accessor — deferred warnings drained at boot via slog.Warn"
  - "captureLambdaWithArity(thread, kwargName, val, expectedArity) — single-positional + no-default + no-varargs/kwargs enforcement; surfaces *dag.ParseError at lambda Pos"
  - "Parameterized free-var visitor pkg/parser/req_walk.go::findFreeVarAccesses; pkg/parser/ctx_walk.go::findCtxAccesses now delegates via firstParamNameAt"
  - "validateTriggerFlowNames finalize pass (D-07-12, cross-file resolution)"
  - "validateTriggerReqAccesses finalize pass (D-07-05, req-attribute typo detection)"
  - "warnDuplicateTriggers finalize pass (D-07-13, byte-identical accepted with deferred warning)"
  - "bridge.triggerTimeGlobals 22-key locked StringDict (lambdaTimeGlobals + json + time) + bridge.TriggerTimeGlobals() copy accessor"
  - "8 testdata/triggers/*.star fixtures + parser-side test extension (fakeWebhookExt + fakeTriggerStarlarkValue) reusable by Plan 04+"
affects:
  - 07-04 (TriggerRegistry consumes Parser.Triggers() + TriggerWarnings())
  - 07-05 (server subcommand uses TriggerRegistry to mount HTTP handlers)
  - 07-06 (firewall + rename pass)
  - 07.1 (github.WebhookSource is the first real TriggerSource implementation; reuses fakeTriggerStarlarkValue wrapper shape)

# Tech tracking
tech-stack:
  added:
    - "go.starlark.net/lib/json — used by triggerTimeGlobals (already transitively in toolchain)"
    - "go.starlark.net/lib/time — used by triggerTimeGlobals (already transitively in toolchain)"
  patterns:
    - "Parameterized free-var visitor: findFreeVarAccesses(src, filename, lambdaPos, freeVarName) lets two callers (ctx-walker, req-walker) share one AST re-parse"
    - "First-param extraction via firstParamNameAt — clean separation between 'find lambda + extract first param' and 'walk DotExprs by name'"
    - "Trigger duplicate signature: (FlowName + Source.Kind + Source.MarshalJSON output + CredentialID) — lambda IDs intentionally EXCLUDED so byte-identical lambdas at different positions still count as duplicates"
    - "captureLambdaWithArity wraps captureLambda for callers needing strict arity (vs script/if_cond/action_fn which accept variable arity)"
    - "Trigger lambdas captured at parse time use parseTimeGlobals (DSL primitives, extensions); triggerTimeGlobals is for RUNTIME evaluation at HTTP ingress (Phase 7.1+ wires)"

key-files:
  created:
    - pkg/parser/req_walk.go
    - pkg/parser/req_walk_test.go
    - pkg/parser/trigger_test.go
    - pkg/parser/testdata/triggers/valid.star
    - pkg/parser/testdata/triggers/typo.star
    - pkg/parser/testdata/triggers/bad_arity.star
    - pkg/parser/testdata/triggers/unknown_flow.star
    - pkg/parser/testdata/triggers/mutable_closure.star
    - pkg/parser/testdata/triggers/not_a_source.star
    - pkg/parser/testdata/triggers/cross_file_flow.star
    - pkg/parser/testdata/triggers/cross_file_trigger.star
    - pkg/parser/testdata/triggers/duplicate_warn.star
    - .planning/phases/07-trigger-primitive-server-shell/07-03-SUMMARY.md
  modified:
    - pkg/parser/ctx_walk.go (refactored — findCtxAccesses delegates to findFreeVarAccesses via firstParamNameAt)
    - pkg/parser/parser.go (triggers field + triggerWarnings field + Triggers() / TriggerWarnings() accessors + sort import)
    - pkg/parser/builtins.go (extension import + builtinTrigger + // skytime:doc markers; reuses existing posKey helper)
    - pkg/parser/lambda_capture.go (captureLambdaWithArity helper)
    - pkg/parser/globals.go (newParseTimeGlobals binds "trigger" between call_flow and result)
    - pkg/parser/finalize.go (3 new passes wired: validateTriggerFlowNames after resolveCallFlows; validateTriggerReqAccesses after validateLambdaCtxAccesses; warnDuplicateTriggers last)
    - pkg/parser/doc.go (D-07-03 / D-07-04 trigger-lambda contract section)
    - pkg/bridge/lambda_globals.go (triggerTimeGlobals + TriggerTimeGlobals() + json/time imports)
    - pkg/bridge/lambda_globals_test.go (5 new tests)
    - pkg/bridge/doc.go (env-distinction documentation)
    - docs/reference/builtins.md (regenerated to include trigger entry)

key-decisions:
  - "findCtxAccesses delegates to findFreeVarAccesses via a new firstParamNameAt helper — generalization preserves all existing TestCtxWalk_* semantics with a defined two-pass cost (each pass O(file_bytes), bounded)"
  - "validateTriggerReqAccesses queries trig.Source via interface{ ReqSchema() []string } type-assertion (NOT direct field access on dag.TriggerSource which lacks ReqSchema); production parser path goes through extension.TriggerSource which provides it. Defensive error if a future construction bypasses the builtin."
  - "Trigger.posKey reuses the existing posKey helper (originally for preBuiltResults) — same shape (filename:line:col), single source of truth"
  - "warnDuplicateTriggers sig EXCLUDES lambda IDs — byte-identical lambdas at different positions get different D-18 IDs by design (line:col), so including them would mask the very duplicates this pass targets"
  - "triggerTimeGlobals is a 22-key locked surface; expansion requires explicit decision logging in PROJECT.md (mirrors lambdaTimeGlobals' 20-key lock)"
  - "TriggerWarnings accumulate on parser state instead of routing through slog.Default at finalize time — keeps finalize tests deterministic and lets Plan 04's worker boot drain them via slog.Warn at server startup with proper context"

patterns-established:
  - "Free-var visitor protocol: findFreeVarAccesses(src, filename, lambdaPos, freeVarName) is the canonical AST walk for any future per-name DotExpr inspection (e.g., a future env-var walker)"
  - "Trigger captureLambdaWithArity error idiom: kwarg %q lambda must accept exactly N positional parameter(s) (convention: req); got M (locked verbatim)"
  - "Default unmarshalTriggerSource error format from Plan 02 sets precedent: any cross-package var-and-setter seam SHOULD return a clear 'no X registered' error in its default value, not nil-deref"
  - "Test-side TriggerSource wrapper pattern: embed *extension.FakeTriggerSource (or any concrete type) for seal satisfaction via promotion; declare ONLY the starlark.Value methods (String/Type/Freeze/Truth/Hash) on the wrapper. Plan 07.1's GitHub webhook source can reuse this shape verbatim for its parser-side starlark namespace."

requirements-completed: [TRIG-01, TRIG-04]

# Metrics
duration: 13min
completed: 2026-05-08
---

# Phase 07 Plan 03: Parser Trigger Builtin Summary

**Top-level `trigger(...)` Starlark builtin shipped with three-layer parse-time validation (free-var lint + arity-1 enforcement + req-walker), cross-file FlowName resolution at finalize, byte-identical-duplicate-warning, and a generalized free-var AST visitor that lets the existing ctx-walker and the new req-walker share one re-parse.**

## Performance

- **Duration:** ~13 min
- **Started:** 2026-05-08T19:53:00Z
- **Completed:** 2026-05-08T20:06:00Z (approx)
- **Tasks:** 7 (all auto; Task 7 marked TDD but production code already existed → single GREEN commit + 3 deviations)
- **Files:** 13 created, 11 modified

## Accomplishments

### Parser surface (TRIG-01)

The shipped `trigger(...)` builtin signature, callable from any `.star` file at parse time:

```python
trigger(
    flow = "check_user",                                       # required string — must resolve in this parser session (cross-file allowed)
    source = github.webhook(events = ["push"]),                # required extension.TriggerSource value (Phase 7.1+ for real factories)
    map = lambda req: {"repo": req.payload.repository.name},   # required arity-1 lambda; returns dict (workflow input)
    idempotency_key = lambda req: req.headers["X-GitHub-..."],  # required arity-1 lambda; returns string (WorkflowID dedup key)
    credential = "github-app-prod",                            # optional credential ID string
)
```

Returns `starlark.None`; the trigger is captured by side effect onto `parser.triggers`. Two parser-side accessors expose the captured state to downstream consumers:

```go
func (p *Parser) Triggers() []*dag.Trigger        // sorted (Source.Kind, FlowName, Pos string)
func (p *Parser) TriggerWarnings() []string       // deferred warnings; nil when empty
```

### Three-layer parse-time validation (TRIG-04)

| Layer | Mechanism | Where | Error |
|-------|-----------|-------|-------|
| 1 | Free-var lint (Phase 1) | `captureLambda` → `validateFreeVars` | `lambda captures non-module-level variable %q ...` |
| 2 | Arity enforcement | `captureLambdaWithArity` (new wrapper around `captureLambda`) | `kwarg %q lambda must accept exactly %d positional parameter(s) (convention: req); got %d` (plus separate messages for `*args` / `**kwargs` / defaulted positional) |
| 3 | Req-attribute walker | `validateTriggerReqAccesses` (new finalize pass) → `findFreeVarAccesses(...,"req")` | `trigger %s lambda: req has no attribute %q; available: %v (declared by source kind %q)` |

Plus the source-type assertion at builtin-call time:

```go
src, ok := sourceVal.(extension.TriggerSource)
if !ok {
    return nil, &dag.ParseError{Pos: pos, Msg: fmt.Sprintf("trigger.source: expected TriggerSource, got %s", sourceVal.Type())}
}
```

And the cross-file FlowName resolution at finalize (D-07-12):

```go
if _, ok := p.flows[trig.FlowName]; !ok {
    knownFlows := sortedFlowNames(p.flows)
    return &dag.ParseError{Pos: trig.Pos, Msg: fmt.Sprintf("trigger references unknown flow %q; known flows: %v", trig.FlowName, knownFlows)}
}
```

### `triggerTimeGlobals` 22-key inventory (D-07-01)

The 22 keys (locked, frozen at module init):

```
# 20 from lambdaTimeGlobals (D-20)
len, str, int, float, bool, list, dict, tuple,            # type constructors / coercions
fail,                                                      # short-circuit failure
enumerate, zip, range, sorted, reversed,                  # iteration helpers
min, max, sum, any, all, abs,
# + 2 trigger-only additions (D-07-01)
json,                                                      # starlarkjson.Module — encode/decode/indent
time,                                                      # starlarktime.Module — now/parse_duration/etc.
```

`json` and `time` are SAFE in trigger lambdas because trigger lambdas run ONCE at HTTP ingress (Phase 7.1+); their output is persisted into `StartWorkflowOptions` before the workflow starts. No replay ever re-evaluates a trigger lambda — non-determinism is observably safe.

### `findCtxAccesses` → `findFreeVarAccesses` refactor

`pkg/parser/req_walk.go` now houses `findFreeVarAccesses(src, filename, lambdaPos, freeVarName)` — the parameterized AST visitor. `pkg/parser/ctx_walk.go::findCtxAccesses` extracts the lambda's first positional parameter via the new `firstParamNameAt` helper and delegates to `findFreeVarAccesses`. The two-pass cost is bounded (each pass O(file_bytes)).

**Regression-guard strategy:** `TestCtxWalk_*` (5 tests) ran identically on the refactored implementation — zero adjustments. Four new tests (`TestFindFreeVarAccesses_CtxName/ReqName/WrongName/NoMatchingLambda`) exercise the generalized signature directly.

### Finalize chain order — exact position of the three new passes

```
1.   resolveCallFlows               (D-16, existing)
1.5. validateTriggerFlowNames       (D-07-12, NEW — runs after resolveCallFlows so unknown-flow errors don't get masked by downstream lints)
2.   lintMixedIdempotency           (D2-05, existing)
2.5. lintBlockFnIdempotency         (D4.1-11, existing)
3.   lintBlockSize                  (D2-07, existing)
4.   lintEmptyTaskQueue             (D3-19, existing)
5.   validateLambdaCtxAccesses      (D4-02, existing)
5.25 validateTriggerReqAccesses     (D-07-05, NEW — runs after ctx-walker so workflow-lambda typos surface first)
5.5. validateResultPlacement        (D4.2-04, existing)
6.   validateIfCondExpressionShape  (D4.2-09 + D4.2-11, existing)
7.   validateActionRefKwargs        (D-11, existing)
8.   warnDuplicateTriggers          (D-07-13, NEW — runs LAST, never errors, accumulates warnings on p.triggerWarnings)
```

### Duplicate-trigger `sig` struct composition

```go
type sig struct {
    flowName     string
    sourceKind   string
    sourceBytes  string  // trig.Source.MarshalJSON() output
    credentialID string
}
```

**Lambda IDs are intentionally EXCLUDED.** Byte-identical lambdas at different positions get different D-18 IDs (line:col). Including them would mask the very duplicates this pass targets — duplicate_warn.star's two triggers each have lambdas that hash differently but represent identical behavior.

### Test-harness shape (Plan 04 will reuse)

```go
type fakeTriggerStarlarkValue struct {
    *extension.FakeTriggerSource  // promotes Kind/ReqSchema/MarshalJSON/triggerSourceMarker
}
// Wrapper-only methods (starlark.Value contract):
func (f *fakeTriggerStarlarkValue) String() string
func (f *fakeTriggerStarlarkValue) Type() string
func (f *fakeTriggerStarlarkValue) Freeze()
func (f *fakeTriggerStarlarkValue) Truth() starlark.Bool
func (f *fakeTriggerStarlarkValue) Hash() (uint32, error)

var _ starlark.Value = (*fakeTriggerStarlarkValue)(nil)
var _ extension.TriggerSource = (*fakeTriggerStarlarkValue)(nil)
```

`fakeWebhookExt` is a parser-side test extension exposing `fake.webhook(req_fields=[...])` returning a `*fakeTriggerStarlarkValue`. The wrapper pattern is the canonical shape for any future TriggerSource that needs to flow through Starlark — Plan 07.1's `github.WebhookSource` will reuse this verbatim.

## Test Coverage for TRIG-01 + TRIG-04 + D-07-12 + D-07-13

`pkg/parser/trigger_test.go` ships 9 test functions (black-box, `package parser_test`):

| VALIDATION.md row                                | Test in this file                |
|--------------------------------------------------|----------------------------------|
| TRIG-01 trigger(...) parses without I/O          | `TestBuiltinTrigger`             |
| TRIG-01 Captured Trigger has correct fields      | `TestBuiltinTrigger_Fields`      |
| TRIG-04 Unknown flow → position-aware error      | `TestTrigger_UnknownFlow`        |
| TRIG-04 Source not a TriggerSource → error       | `TestTrigger_BadSource`          |
| TRIG-04 req.<field> typo → valid-list error      | `TestTrigger_ReqAttrTypo`        |
| TRIG-04 Lambda arity wrong → error               | `TestTrigger_BadArity`           |
| TRIG-04 Mutable closure → free-var lint surfaces | `TestTrigger_MutableClosure`     |
| D-07-12 Cross-file trigger.FlowName resolution   | `TestTrigger_CrossFileFlow`      |
| D-07-13 Byte-identical duplicates → warning      | `TestTrigger_DuplicateWarn`      |

Plus 4 unit tests on the generalized visitor (`pkg/parser/req_walk_test.go::TestFindFreeVarAccesses_*`) and 5 unit tests on the trigger-time globals (`pkg/bridge/lambda_globals_test.go::TestTriggerTimeGlobals*`). The pre-existing `TestCtxWalk_*` suite ran unchanged after the visitor refactor — regression guard for the `findCtxAccesses` delegation chain.

## Task Commits

Each task was committed atomically:

1. **Task 1: Refactor ctx_walk → req_walk parameterized visitor** — `3ff24be` (refactor)
2. **Task 2: triggerTimeGlobals (lambdaTimeGlobals + json + time)** — `e612367` (feat)
3. **Task 3: builtinTrigger + captureLambdaWithArity + accessors** — `abe93a9` (feat)
4. **Task 4: register trigger() in newParseTimeGlobals + doc** — `07786f1` (feat)
5. **Task 5: wire trigger validators into finalize** — `641631a` (feat)
6. **Task 6: 9 testdata fixtures** — `2f59c68` (test)
7. **Task 7: trigger_test.go + 3 auto-fix deviations** — `c128b8c` (test)

## Decisions Made

- **`findCtxAccesses` delegates rather than being deleted.** The existing callers (`validateLambdaCtxAccesses` and friends) already use the typed `ctxAccess` slice. Reusing the type via a thin conversion in `findCtxAccesses` keeps the call-site signatures stable and confines the refactor to internal mechanics.
- **`validateTriggerReqAccesses` type-asserts against `interface{ ReqSchema() []string }`.** `trig.Source` is the dag-local `dag.TriggerSource` interface (only `Kind` + `MarshalJSON`); `ReqSchema` lives on the larger `extension.TriggerSource`. The type assertion provides a clear error path for any future construction site that bypasses the parser builtin (which the assertion guarantees holds in practice).
- **`posKey` is reused, not duplicated.** The pre-existing `posKey(syntax.Position) string` (originally for `preBuiltResults`) has the exact shape (`filename:line:col`) needed for the trigger map key. Single source of truth for "stable per-call-site Starlark string key".
- **Trigger lambdas at parse time use `parseTimeGlobals` (rich), not `triggerTimeGlobals`.** The constraint Phase 7.1+ enforces is that the RUNTIME evaluation of trigger lambdas happens against `triggerTimeGlobals`. Parse-time lambda compilation is unchanged from script/if_cond/action_fn — captured lambdas resolve free vars against whichever predeclared env was active at parse time, then the runtime supplies its own env at call time. This matches the existing Phase 1+ architecture verbatim.
- **`warnDuplicateTriggers` accumulates warnings on parser state, not via `slog.Default` at finalize.** Tests need deterministic state to inspect; routing through `slog.Default` would force tests to install a custom handler. The Plan 04 worker boot owns the slog drain.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 — Bug] `mutable_closure.star` fixture would not have triggered Phase 1's free-var lint**
- **Found during:** Task 7 (trigger_test.go RED phase)
- **Issue:** Plan's fixture had `counter = [0]` at module-level (col 1), then `lambda req: counter[0]` capturing it. Phase 1's `validateFreeVars`/`isModuleLevelBinding` accepts ALL module-level bindings (it does NOT distinguish mutable from immutable; module-level mutability is a Starlark-resolver-level question, not a free-var-lint question). The lint fires for locals captured from inside an enclosing `def` body — not for module-level reads.
- **Fix:** Rewrote the fixture to wrap the trigger declaration in a `def _build_trigger(): ... _build_trigger()` so the captured `local_var` is non-module-level. The lambda `lambda req: local_var` now genuinely fails the lint with `lambda captures non-module-level variable "local_var" ...`.
- **Files modified:** `pkg/parser/testdata/triggers/mutable_closure.star` (rewrite).
- **Verification:** `TestTrigger_MutableClosure` passes; the test asserts the error message references either the variable name OR the canonical free-var-lint terminology (`free var`, `module-level`, `non-module-level`).

**2. [Rule 1 — Bug] `cross_file_flow.star` exported a leading-underscore symbol that go.starlark.net rejects**
- **Found during:** Task 7 (`TestTrigger_CrossFileFlow` failed with `load: names with leading underscores are not exported: _loaded`).
- **Issue:** Plan's fixture used `_loaded = True` and the loader did `load("cross_file_flow.star", "_loaded")`. go.starlark.net's resolver enforces "names with leading underscores are not exported" so the load fails before reaching the trigger.
- **Fix:** Renamed the constant to `marker` (no leading underscore) in `cross_file_flow.star`; updated `cross_file_trigger.star` to load and trivially reference `marker`.
- **Files modified:** `pkg/parser/testdata/triggers/cross_file_flow.star`, `pkg/parser/testdata/triggers/cross_file_trigger.star`.
- **Verification:** `TestTrigger_CrossFileFlow` passes; the trigger resolves `flow="check_user"` against the loaded file's flow declaration.

**3. [Rule 3 — Blocking] `docs/reference/builtins.md` drift gate broke after adding the new `trigger()` builtin**
- **Found during:** Task 7 (full-suite regression — `TestDocgenDrift` flagged the diff).
- **Issue:** The builtin shipped with full `// skytime:doc` markers (Task 3), so `cmd/skytime-docgen` wants to render a new `## trigger` section in `docs/reference/builtins.md`. The drift test compares the generated bytes against the committed file and fails on any divergence.
- **Fix:** Ran `go generate ./pkg/parser/` (which invokes `cmd/skytime-docgen`); committed the regenerated `docs/reference/builtins.md` (+36 lines for the `trigger` entry).
- **Files modified:** `docs/reference/builtins.md`.
- **Verification:** `go test ./tests/ -run TestDocgenDrift` exits 0; full `go test ./...` exits 0.

**Total deviations:** 3 auto-fixed (2 fixture bugs, 1 doc-regeneration blocking). No architectural or scope changes — all three are pure execution mechanics caught by the test suite and corrected immediately.

### Non-deviation worth noting

The plan's Task 1 acceptance criterion says `validateTriggerReqAccesses` and `checkTriggerLambdaReq` should appear in `pkg/parser/req_walk.go` and `go build ./pkg/parser/...` should exit 0. Those methods reference `p.triggers`, which Task 3 ostensibly adds. To keep Task 1 buildable in isolation, the `triggers` map field + `NewParser` initialization were added in Task 1's commit (the rest of the field/accessor work — `triggerWarnings`, `Triggers()`, `TriggerWarnings()` — landed in Task 3 as planned). This is a minor task-ordering pull-forward, not a deviation; the final shape matches the plan.

## Issues Encountered

- **Plan-vs-codebase nit: `posKey` already existed.** The plan instructed creating `posKey` near `wrapBuiltinError`. Task 3's initial paste introduced a duplicate (compile error: `posKey redeclared in this block` — line 1466 vs line 960). Removed the duplicate, kept the existing helper. Plan acceptance criterion `grep -nE 'func posKey\(pos syntax\.Position\) string' pkg/parser/builtins.go` returns exactly one match (the existing one) — same effect.
- **Plan-vs-codebase nit: `extension.InitOptions` does not exist.** The plan suggested `Initialize(thread *starlark.Thread, _ extension.InitOptions)` for the test extension. The actual `extension.Extension` interface is `Initialize(thread *starlark.Thread, kwargs []starlark.Tuple)`. Adjusted to the real signature.
- **Trigger lambda env at parse time.** The plan's Task 4 was clear that `triggerTimeGlobals` is a runtime-only env (Phase 7.1+ wires it during HTTP receive). At parse time, trigger lambdas use the rich `parseTimeGlobals` like every other captured lambda. Confirmed by a brief read of `pkg/bridge/lambda_call.go::CallLambda` — it takes the predeclared env as a parameter, so swapping envs between parse-time compile and runtime invoke is a per-call decision. No additional plumbing needed in this plan.

## User Setup Required

None — pure parser-side work. Phase 7.1+ HTTP receiver will be where users configure trigger sources (URLs, secrets, etc.).

## Self-Check: PASSED

- File `pkg/parser/req_walk.go`: FOUND
- File `pkg/parser/req_walk_test.go`: FOUND
- File `pkg/parser/trigger_test.go`: FOUND
- File `pkg/parser/testdata/triggers/valid.star`: FOUND
- File `pkg/parser/testdata/triggers/typo.star`: FOUND
- File `pkg/parser/testdata/triggers/bad_arity.star`: FOUND
- File `pkg/parser/testdata/triggers/unknown_flow.star`: FOUND
- File `pkg/parser/testdata/triggers/mutable_closure.star`: FOUND
- File `pkg/parser/testdata/triggers/not_a_source.star`: FOUND
- File `pkg/parser/testdata/triggers/cross_file_flow.star`: FOUND
- File `pkg/parser/testdata/triggers/cross_file_trigger.star`: FOUND
- File `pkg/parser/testdata/triggers/duplicate_warn.star`: FOUND
- Modifications to pkg/parser/{ctx_walk.go, parser.go, builtins.go, lambda_capture.go, globals.go, finalize.go, doc.go}: FOUND
- Modifications to pkg/bridge/{lambda_globals.go, lambda_globals_test.go, doc.go}: FOUND
- Regenerated docs/reference/builtins.md: FOUND
- Commit `3ff24be` (Task 1): FOUND
- Commit `e612367` (Task 2): FOUND
- Commit `abe93a9` (Task 3): FOUND
- Commit `07786f1` (Task 4): FOUND
- Commit `641631a` (Task 5): FOUND
- Commit `2f59c68` (Task 6): FOUND
- Commit `c128b8c` (Task 7): FOUND
- `go build ./...`: PASS
- `go vet ./...`: PASS
- `go test ./... -count=1 -race`: PASS (full repo green)
- `go test ./pkg/parser/ -run 'TestBuiltinTrigger|TestTrigger_' -count=1 -race`: PASS (9 tests)
- `go test ./pkg/parser/ -run 'TestFindFreeVarAccesses|TestCtxWalk_' -count=1 -race`: PASS (regression-guard tests for the visitor refactor)
- `go test ./pkg/bridge/ -run 'TestTriggerTimeGlobals|TestLambdaTimeGlobalsLocked' -count=1 -race`: PASS (locked-set + copy + json/time integration)
- `go test ./tests/ -run TestDocgenDrift -count=1`: PASS (post-regen)

## Next Phase Readiness

- **Plan 04 (TriggerRegistry + worker boot):** unblocked. `Parser.Triggers()` returns deterministically-sorted `[]*dag.Trigger` and `Parser.TriggerWarnings()` returns the deferred-warning slice. The boot loop drains warnings via `slog.Warn` at server startup. Plan 04 owns the in-process registry shape and the iteration grouping by `Source.Kind()`.
- **Plan 05 (server subcommand):** indirectly ready via the chain.
- **Plan 06 (firewall + rename):** indirectly ready; Plan 06's `%+v` / `%#v` grep gate against `*dag.Trigger` and any `TriggerSource` concrete type is the credential-redaction final-line-of-defense.
- **Plan 07.1 (real HTTP webhook receiver):** unblocked at the parser layer. The trigger primitive is shipped; runtime consumers can use `bridge.TriggerTimeGlobals()` to compile a lambda env at HTTP ingress that includes `json` + `time`. The first real `TriggerSource` (likely `github.WebhookSource`) reuses the `fakeTriggerStarlarkValue` wrapper shape — embed the concrete type for seal satisfaction, declare the starlark.Value methods on the wrapper.
- **No blockers.** Plan 03 leaves the parser surface fully exercised end-to-end via the 9-test fixture corpus. Plan 04 begins from a green parser session with both `Triggers()` and `TriggerWarnings()` accessors live.

---
*Phase: 07-trigger-primitive-server-shell*
*Completed: 2026-05-08*
