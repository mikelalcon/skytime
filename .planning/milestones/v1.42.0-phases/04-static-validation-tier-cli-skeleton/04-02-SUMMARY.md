---
phase: 04-static-validation-tier-cli-skeleton
plan: 02
subsystem: parser
tags: [validator, ast, starlark, syntax-walk, ctx-access, kwarg-validation, finalize, tdd]

# Dependency graph
requires:
  - phase: 01-type-spine-extension-contract-parser-bridge-foundations
    provides: dag.ValidationError, parser.finalize chain, validateActionRefKwargs no-op stub, lambda capture (D-19), splitKind helper, defaultFileOptions, fakeExtension
  - phase: 04-static-validation-tier-cli-skeleton (plan 01)
    provides: Parser.FileBytes() accessor, dag.ValidationError.Action field, package skeletons
provides:
  - "findCtxAccesses(src, filename, lambdaPos) AST re-parse visitor — Phase 4 D4-02 load-bearing primitive"
  - "stateSet accumulator with add/has/clone/sortedKeys for D4-02 stacking rules (flow inputs, += script.OutputAlias, += for_each.ItemVar, if_cond branches see same pre-branch state)"
  - "validateLambdaCtxAccesses finalize pass — rejects ctx.<name> references not in lexically-visible state schema with *dag.ValidationError"
  - "validateActionRefKwargs filled (was no-op stub) — D-11 cross-validate every dag.ActionRef.Kwargs against registered OperationSpec via DecodeKwargsFromDict, populates ValidationError.Action"
  - "Finalize pass chain ordering: resolveCallFlows → lintMixedIdempotency → lintBlockSize → lintEmptyTaskQueue → validateLambdaCtxAccesses → validateActionRefKwargs (D4-02 before D-11 cross-validate per CONTEXT D4-01)"
  - "Shared testhelpers_test.go (package parser white-box) with stringPtr helper for syntax.MakePosition"
affects: [04-03, 04-04, 04-05, 04-06, 04-07, 05, 06]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "AST re-parse via FileBytes()[filename] + (*syntax.FileOptions).Parse to recover *syntax.File when *starlark.Function discards AST after compilation"
    - "Position match by (Filename, Line, Col) equality — distinguishes two lambdas on same line by column (Pitfall #1)"
    - "Top-level-attr-only collection on chained DotExpr (ctx.req.repo_name.length yields only 'req') — state schema names are flat keys"
    - "Walker recursion uniformity: walkBodyForCtxValidation + walkValidateActionRefKwargs follow walkLintMixedIdempotency / walkLintBlockSize / walkResolveCallFlows shape so future readers see one walk pattern"
    - "stateSet clone-before-fork idiom for if_cond branches — pre-branch state preserved, branch-local additions never leak"
    - "Finalize pass insertion ordering: structural state errors (D4-02) surface before kwarg-shape errors (D-11) so consultants fix root cause first"

key-files:
  created:
    - "pkg/parser/ctx_walk.go"
    - "pkg/parser/ctx_walk_test.go"
    - "pkg/parser/state_schema.go"
    - "pkg/parser/state_schema_test.go"
    - "pkg/parser/finalize_xvalidate_test.go"
    - "pkg/parser/testhelpers_test.go"
  modified:
    - "pkg/parser/finalize.go (validateLambdaCtxAccesses inserted into chain; validateActionRefKwargs filled with cross-validate logic; doc comment updated for new pass count)"
    - "pkg/parser/linter_test.go (TestFreeVars_ModuleConstAllowed + TestFreeVars_ModuleLevelDefAllowed fixtures: declare {\"v\":\"int\"} as flow input so D4-02 sees the name in state schema; D-19 invariant under test unchanged)"

key-decisions:
  - "ActionRef.Pos NOT used for cross-validate error position — Step.Pos is the closest guaranteed-present syntax-tree node; ActionRef.Pos may be zero when callers hand-build, and the enclosing Step is always present in walks"
  - "First-param name read dynamically (paramName helper) instead of hard-coded 'ctx' — TestCtxWalk_TwoLambdasSameLine fixture proves correctness for `lambda c: c.right`"
  - "checkLambdaCtx silently skips empty lambdaID / missing fileBytes / missing p.lambdas[id] — defensive: earlier finalize passes (resolveCallFlows, lambda capture) surface these conditions first with better attribution"
  - "validateLambdaCtxAccesses inserted BEFORE validateActionRefKwargs (CONTEXT D4-01 ordering) — structural state errors (typo on ctx.<name>) are higher-leverage than kwarg-shape errors"
  - "ItemsLambdaID validated against PRE-loop state — the items producer cannot reference its own item-var (TestFinalize_CtxAccess_ForEachItemsLambdaCannotSeeItem pins this)"
  - "Pre-existing TestFreeVars_ModuleConstAllowed / TestFreeVars_ModuleLevelDefAllowed fixtures updated (Rule 1) — they used ctx.v with empty inputs which D4-02 now correctly rejects; updating fixtures preserves D-19 intent without weakening D4-02"

patterns-established:
  - "Pattern: AST visitor seam via FileBytes() — Parser.FileBytes() (W0) + findCtxAccesses (W1) is the canonical way to recover syntax-tree info that *starlark.Function discards. Future passes that need AST detail (e.g., D4-02 extensions, lambda-output validation) reuse this same path"
  - "Pattern: stateSet accumulator with clone-on-branch — generic enough to extend for future scope-aware validations (e.g., comprehension-introduced names, with-statement bindings)"
  - "Pattern: Finalize pass extension — new lints append to finalize() with documented ordering and matching walk-helper naming (walkXxxForYyy). Symmetric with Phase 1/2/3 lints"
  - "Pattern: White-box test files (package parser) for cross-package fixture access — finalize_xvalidate_test.go demonstrates declaring a minimal local extension (xvalExt) inline rather than threading through external fixtures"

requirements-completed: [VAL-01]

# Metrics
duration: 6min
completed: 2026-05-01
---

# Phase 4 Plan 02: Static Validator Core Logic Summary

**D4-02 ctx.<name> walker (re-parse + state-schema accumulator) + D-11 kwarg cross-validate (defense in depth) — fills the load-bearing static-validation logic in pkg/parser/finalize.go.**

## Performance

- **Duration:** ~6 min (388 s wall-clock)
- **Started:** 2026-05-01T20:00:31Z
- **Completed:** 2026-05-01T20:06:59Z
- **Tasks:** 3 (all TDD: 6 commits — 3 RED, 3 GREEN)
- **Files modified:** 8 (6 created, 2 modified)

## Accomplishments

- **D4-02 ctx.<name> attribute walker (load-bearing VAL-01).** `findCtxAccesses(src, filename, lambdaPos)` re-parses cached file bytes via `defaultFileOptions().Parse`, locates the matching `*syntax.LambdaExpr` or `*syntax.DefStmt` by position, and walks the body collecting every `<firstParam>.<attr>` access. Workaround for the fact that `*starlark.Function` discards its AST after compilation (04-RESEARCH critical finding).
- **State-schema accumulator with D4-02 stacking semantics.** `validateLambdaCtxAccesses` walks every flow's body recursively, accumulating the visible state-name set per the rules: flow inputs at entry; += `script.OutputAlias` after each script; += `for_each_parallel.ItemVar` inside the fan-out body; `if_cond` branches see the same pre-branch state (clone-on-fork). `items=lambda` validated against pre-loop state (cannot see its own item-var).
- **D-11 kwarg cross-validate filled.** `validateActionRefKwargs` (was a no-op stub) now walks every `dag.ActionRef`, looks up its `OperationSpec.KwargsType`, allocates a zero-value target, and re-runs `extension.DecodeKwargsFromDict`. Catches hand-built ActionRefs (test fixtures, future programmatic callers) where the per-call extension factory was bypassed.
- **Phase 1/2/3 fixture corpus continues to pass cleanly** — full repo `go test ./...` exits 0; no regressions introduced.

## Task Commits

Each task TDD-paired (test → feat); plan metadata committed separately at the end:

1. **Task 1 RED: ctx-walk visitor failing tests** — `a6c4b69` (test)
2. **Task 1 GREEN: findCtxAccesses implementation** — `e284cee` (feat)
3. **Task 2 RED: state-schema accumulator failing tests** — `abb0055` (test)
4. **Task 2 GREEN: validateLambdaCtxAccesses + finalize wiring + Rule 1 fixture fix** — `92a1d43` (feat)
5. **Task 3 RED: kwarg cross-validate failing test** — `4ee05a9` (test)
6. **Task 3 GREEN: validateActionRefKwargs filled** — `1a27f91` (feat)

**Plan metadata:** (final commit will include this SUMMARY.md, STATE.md, ROADMAP.md, REQUIREMENTS.md)

## Files Created/Modified

**Created:**
- `pkg/parser/ctx_walk.go` — `findCtxAccesses(src, filename, lambdaPos) ([]ctxAccess, error)` + `positionsEqual` + `paramName` helpers
- `pkg/parser/ctx_walk_test.go` — 4 tests: FindsAttrAccesses (lambda + def, multi-attr), TwoLambdasSameLine (Pitfall #1), NestedAttrCollectsTopOnly (top-level only), LambdaNotFound (defensive empty)
- `pkg/parser/state_schema.go` — `stateSet` type + `validateLambdaCtxAccesses` + `walkBodyForCtxValidation` + `checkLambdaCtx`
- `pkg/parser/state_schema_test.go` — 7 tests: AccumulatesScopes (set semantics), Valid (positive end-to-end), RejectsUnknown (typo path), ForEachItemVar (item visible inside Steps), IfCondBranchesIsolated (cross-branch leakage rejection), ForEachItemsLambdaPreLoop (positive items=lambda), ForEachItemsLambdaCannotSeeItem (negative items=lambda referencing own item-var)
- `pkg/parser/finalize_xvalidate_test.go` — 2 tests: KwargCrossValidate (negative, missing required), KwargCrossValidate_NoFalsePositive (positive)
- `pkg/parser/testhelpers_test.go` — Shared `stringPtr` helper (white-box, package parser)

**Modified:**
- `pkg/parser/finalize.go` — Inserted `validateLambdaCtxAccesses` call before `validateActionRefKwargs`; filled `validateActionRefKwargs` with `walkValidateActionRefKwargs` + `crossValidateActionRef`; added `reflect`, `go.starlark.net/syntax`, `pkg/extension` imports; doc comment updated for 6 numbered passes
- `pkg/parser/linter_test.go` — Two pre-existing fixtures (`TestFreeVars_ModuleConstAllowed`, `TestFreeVars_ModuleLevelDefAllowed`) updated: declared `{"v":"int"}` as a flow input so D4-02 sees the name in state schema; D-19 invariant under test unchanged (Rule 1 fixture-alignment)

## Decisions Made

- **ActionRef cross-validate position uses Step.Pos, not ActionRef.Pos** — `ActionRef.Pos` is zero when callers hand-build (the test path that motivates this defense-in-depth), so the enclosing `*dag.Step.Pos` is the closest guaranteed-present syntax-tree node.
- **First-param name read dynamically via `paramName` helper** — `TestCtxWalk_TwoLambdasSameLine` proves the visitor handles `lambda c: c.right` (param named `c`, not `ctx`), so hard-coding "ctx" would have been a false simplification.
- **Defensive skips in `checkLambdaCtx`** — empty `lambdaID`, missing `p.lambdas[id]`, or missing `fileBytes[filename]` all silently skip rather than error. Earlier finalize passes (resolveCallFlows, lambda capture) surface these conditions first with better attribution; this pass should not double-report.
- **Pass ordering: D4-02 BEFORE D-11 cross-validate** (per CONTEXT D4-01) — structural state errors are more user-leverage than kwarg-shape errors. A typo in `ctx.repo_nme` should surface before a downstream kwarg mismatch in some other action ref.
- **`ItemsLambdaID` validated against PRE-loop state** — codifies the semantic that the items producer cannot reference its own item-var. Two tests pin this (positive + negative).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Pre-existing free-vars fixtures violated D4-02 strictness**
- **Found during:** Task 2 (after wiring `validateLambdaCtxAccesses` into finalize)
- **Issue:** `TestFreeVars_ModuleConstAllowed` and `TestFreeVars_ModuleLevelDefAllowed` in `pkg/parser/linter_test.go` declared `inputs={}` while their lambdas referenced `ctx.v`. Under D-19 alone this parsed cleanly (the test's intent was to validate module-level free vars). Under the new D4-02 pass, `ctx.v` is correctly rejected because `v` is not in the state schema.
- **Fix:** Updated both fixtures to declare `inputs={"v":"int"}`. The D-19 invariant under test (module-level `helper`/`MAX` are valid free vars) is unchanged; both tests now pass D-19 + D4-02 together.
- **Files modified:** `pkg/parser/linter_test.go`
- **Verification:** `go test ./pkg/parser -run TestFreeVars -count=1 -v` — all four FreeVars subtests pass.
- **Committed in:** `92a1d43` (Task 2 GREEN commit)

---

**Total deviations:** 1 auto-fixed (1 bug — pre-existing fixtures incompatible with new stricter validator)
**Impact on plan:** Single Rule-1 fixture-alignment fix preserves both the D-19 invariant under test and the new D4-02 invariant. No scope creep; no architectural change. The fixtures were unrealistic (consultants would never write `ctx.v` against `inputs={}`); the update makes them representative.

## Issues Encountered

None — TDD cycle ran smoothly. Each RED phase failed for the expected reason (compile errors from undefined symbols, then the negative cross-validate test failed against the no-op stub). Each GREEN phase passed all subtests on first run.

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- **VAL-01 load-bearing logic in place.** The static validator can now reject `ctx.<typo>` references and hand-built ActionRefs with kwarg-schema mismatches. Both surfaces are wired into `parser.finalize` so any caller (`pkg/validator`, the eventual `skytime validate` CLI) gets the checks for free.
- **`pkg/validator` facade not yet built.** Plan 04-03 (and onward) is expected to land the thin `pkg/validator.Validate(file, opts...) []error` facade plus the dry-run interpreter seam (D4-03) for the differential corpus test (VAL-02). Both ride on top of the finalize-pass primitives this plan landed.
- **Differential corpus test deferred.** VAL-02's "static + dry-run agree on accept/reject" test is owned by a later wave; the static side it consumes is now feature-complete.
- **No blockers.** Full repo `go test ./...` exits 0; `go vet` clean; firewall tests untouched.

## Self-Check: PASSED

- All 6 task commits present in `git log` (verified: `a6c4b69`, `e284cee`, `abb0055`, `92a1d43`, `4ee05a9`, `1a27f91`).
- All 6 created files present on disk (`pkg/parser/ctx_walk.go`, `ctx_walk_test.go`, `state_schema.go`, `state_schema_test.go`, `finalize_xvalidate_test.go`, `testhelpers_test.go`).
- Both modified files contain expected changes (`pkg/parser/finalize.go`: `validateLambdaCtxAccesses` call + cross-validate body; `pkg/parser/linter_test.go`: two `inputs={"v":"int"}` updates).
- Verification gate: `go test ./pkg/parser -run "TestCtxWalk_|TestStateSchema_|TestFinalize_CtxAccess_|TestFinalize_KwargCrossValidate" -count=1` — 13/13 PASS.
- Regression gate: `go test ./... -count=1` — full repo green.

---
*Phase: 04-static-validation-tier-cli-skeleton*
*Completed: 2026-05-01*
