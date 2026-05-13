---
phase: quick-260512-w7c
plan: 01
subsystem: parser/interpolation
tags: [bugfix, parser, interpolation, lambda-id, regression-pin, phase-04.1-followup]
requires:
  - pkg/parser/desugarInterpolation
  - pkg/parser/captureLambdaAtPosition
  - pkg/parser/desugarActionRefKwargs
  - pkg/dag/ComputeLambdaID
  - pkg/interpreter/resolve_kwargs.evalLambda
provides:
  - Multi-kwarg interpolation safety — each `${ctx.expr}` kwarg on the same factory call gets a distinct D-18 lambda ID
  - Two regression backstops (parser-tier + example-tier) that fail deterministically if the disambiguator threading is reverted
affects:
  - pkg/parser/builtins.go (desugarActionRefKwargs + 4 other call sites)
  - pkg/parser/builtins_log.go (log builtin)
  - pkg/parser/interpolation.go (desugarInterpolation signature)
  - pkg/parser/lambda_capture.go (captureLambdaAtPosition signature + ID computation)
  - pkg/parser/result_value_capture.go (one captureLambdaAtPosition caller)
  - pkg/parser/interpolation_test.go (11 call-site updates)
tech-stack:
  added: []
  patterns:
    - "kwarg-key-as-disambiguator: append `:<key>` to base D-18 ID when capturing lambdas synthesized from per-kwarg interpolations sharing one ActionRef.Pos"
key-files:
  created:
    - pkg/parser/interpolation_collision_test.go
    - examples/http-github-webhook/public_repo_check_smoke_test.go
  modified:
    - pkg/parser/interpolation.go
    - pkg/parser/lambda_capture.go
    - pkg/parser/builtins.go
    - pkg/parser/builtins_log.go
    - pkg/parser/result_value_capture.go
    - pkg/parser/interpolation_test.go
decisions:
  - id: D-quick-260512-w7c-A
    summary: "Disambiguator appended to ID (vs. encoded into hash input) preserves the D-18 8-hex-prefix layout and keeps empty-disambiguator IDs byte-identical to pre-fix output. Zero churn for the five non-kwargs call sites (flow.name, step.name, script.id, fail.msg, log.msg) and the result.value capture path."
  - id: D-quick-260512-w7c-B
    summary: "Kwarg KEY is the disambiguator (vs. a synthetic counter). Stable across reparses (deterministic per D-DETERMINISM), grep-friendly when reading lambda IDs in logs, and naturally distinct because Starlark dict keys are unique by language rule."
metrics:
  duration: ~7min
  tasks: 4
  files_modified: 6
  files_created: 2
  completed_date: "2026-05-13"
---

# Quick 260512-w7c: Fix Phase 04.1 interpolation lambda-ID collision Summary

**One-liner:** Multi-`${ctx.expr}`-kwarg ActionRef calls now produce distinct D-18 lambda IDs per kwarg key, fixing a latent last-wins overwrite in `p.lambdas` that caused both `owner` and `repo` interpolated kwargs to resolve to the same value at workflow runtime.

## Root Cause

`pkg/parser/builtins.go::desugarActionRefKwargs` calls `p.desugarInterpolation(raw, ar.Pos)` for every kwarg whose value contains `$`. The desugarer threads `ar.Pos` (the ActionRef's call position) through `captureLambdaAtPosition`, where `dag.ComputeLambdaID(fileBytes, ar.Pos)` produces an ID of the form `sha256(fileBytes)[:4]:line:col`. For the SAME call site, `ar.Pos` is identical for every kwarg → every synthesized lambda hashes to the SAME ID → `p.lambdas[id] = captured` is last-wins → the first kwarg's lambda is silently overwritten by the second's.

At workflow-resolve time, `pkg/interpreter/resolve_kwargs.go` does `i.evalLambda(captured.ID)`, which performs an ID-keyed lookup into `i.parsed.Lambdas` — so BOTH `owner` and `repo` kwargs of `gh.list_open_issues(owner="${ctx.rp.owner}", repo="${ctx.rp.repo}")` resolved to the LAST-captured lambda (`repo`'s body), and both evaluated to `"Hello-World"`. The downstream GitHub call hit `GET /repos/Hello-World/Hello-World/issues` and 404'd — visible in CI's `walkthrough_smoke.sh` but invisible to any `go test ./...` invocation prior to this fix.

## The Fix

A surgical, additive signature change preserving byte-identical IDs for the five non-kwargs callers:

```
// pkg/parser/interpolation.go
func (p *Parser) desugarInterpolation(raw string, openPos syntax.Position, disambiguator string) (*dag.CapturedLambda, error)

// pkg/parser/lambda_capture.go
func (p *Parser) captureLambdaAtPosition(fn *starlark.Function, userPos, bodyPos syntax.Position, disambiguator string) (*dag.CapturedLambda, error) {
    ...
    id := dag.ComputeLambdaID(fileBytes, userPos)
    if disambiguator != "" {
        id = id + ":" + disambiguator
    }
    ...
}
```

`desugarActionRefKwargs` (pkg/parser/builtins.go) extracts the kwarg key as `string(keyStr)` (with a defensive D-11 internal-error guard for non-string keys) and passes it as the disambiguator. All five other call sites — `builtinFlow.name`, `builtinStep.name`, `builtinFail.msg`, `builtinScript.id`, `builtinLog.msg` — and `result_value_capture.captureLambdaAtPosition` pass `""`. Empty disambiguator skips the append branch, so the ID format and value for those call sites are byte-identical to the pre-fix output.

## Test Coverage Added

Two new files; both fail deterministically when Task 1's disambiguator-append branch is stashed (verified manually during execution by editing `pkg/parser/lambda_capture.go` to `_ = disambiguator` and re-running each test).

### Parser-tier — `pkg/parser/interpolation_collision_test.go`

- **`TestInterpolation_MultiKwarg_DistinctLambdaIDs`** — Parses a minimal flow with `fake.op(a="${ctx.x.foo}", b="${ctx.x.bar}")`, walks to the ActionRef's Kwargs, extracts both `*StarlarkLambda` instances, and asserts:
  - `a.ID != b.ID` (with collision-message attribution)
  - Both IDs registered in `p.Lambdas()`
- **`TestInterpolation_MultiKwarg_EvaluateToDistinctValues`** — Same parse, then evaluates each captured lambda via `bridge.CallLambda` against state `{x:{foo:"AAA",bar:"BBB"}}`. Asserts `a → "AAA"`, `b → "BBB"`, and `a != b`. Belt-and-suspenders for direct-eval correctness (the ID-distinctness invariant alone implies the ID-keyed interpreter lookup works).

A `multiKwargExtension` test helper is inlined: a single `op` builtin that accepts arbitrary kwargs and packs them into the ActionRef.Kwargs dict, mirroring the github extension's `newOpBuiltin` shape. Distinct from the existing `fakeExtension` (which only exposes single-kwarg ops).

### Example-tier — `examples/http-github-webhook/public_repo_check_smoke_test.go`

- **`TestPublicRepoCheck_KwargLambdasResolveDistinctly`** — Parses the REAL `public_repo_check.star` with the example's registered extension set, walks to `if_cond.then[0]`'s block step (the two `gh.list_open_issues` + `gh.list_prs` calls), and for each ActionRef asserts:
  - `owner` and `repo` kwargs are `*StarlarkLambda` instances
  - Their IDs differ
  - They evaluate to `"octocat"` and `"Hello-World"` respectively against the rp state struct
  - The two results differ

This places the EX-04 walkthrough's exact failure mode under `go test ./...` so any future regression fails locally without needing a running Temporal server.

## Verification Outcomes

| Check                                                                                | Status                          |
| ------------------------------------------------------------------------------------ | ------------------------------- |
| `go build ./...`                                                                     | PASS (clean)                    |
| `go test ./pkg/... -race -count=1`                                                   | PASS (21 packages, no regression) |
| `go test ./pkg/parser/ -run TestInterpolation_MultiKwarg -race -count=1`             | PASS (2/2 subtests)             |
| `go test ./examples/http-github-webhook/ -run TestPublicRepoCheck_KwargLambdasResolveDistinctly -race -count=1` | PASS                            |
| `go test ./... -race -count=1` (full repo)                                           | PASS (28 packages, zero failures) |
| `grep -nE 'desugarInterpolation\(' pkg/parser/*.go` — every prod+test site 3-arg     | PASS (17 matches, all 3-arg)    |
| `grep -nE 'captureLambdaAtPosition\(' pkg/parser/*.go` — every prod site 4-arg       | PASS (3 matches, all 4-arg)     |

**Stash-and-rerun verification:** During execution, `pkg/parser/lambda_capture.go`'s disambiguator-append was temporarily replaced with `_ = disambiguator` to confirm BOTH new tests fail with the exact collision-message attribution. Output captured:

- Parser test: `lambda IDs for kwargs 'a' and 'b' collide: a="93f0a822:5:30" b="93f0a822:5:30"`
- Example smoke: `action[0] owner+repo lambda IDs collide: owner="0027a336:53:44" repo="0027a336:53:44"` and `action[1] owner+repo lambda IDs collide: owner="0027a336:54:36" repo="0027a336:54:36"` (lines 53 + 54 of public_repo_check.star — list_open_issues + list_prs)

After verification, the fix was restored verbatim and tests returned to PASS.

## Live Temporal Smoke

Temporal dev server was confirmed listening on `localhost:7233` (`nc -z` succeeded). The walkthrough was executed against the live server:

```
/tmp/extbin run examples/http-github-webhook/public_repo_check.star \
  --flow public_repo_check \
  --input '{"repo":"octocat/Hello-World"}' \
  --address=localhost:7233
```

**Outcome — PASS:**

```
[skytime] flow public_repo_check  4 steps  starting
[1/4] script               rp  ✓ 0ms
[2/4] step                 Inspect octocat/Hello-World  ✓ 275ms
[3/4] script               pop  ✓ 0ms
[4/4] if_cond              cond ▶ then
    [4a/4a] step                 2 actions  ✓ 2128ms  2 ok
[4/4] if_cond              cond  ✓ 2128ms
[skytime] flow complete  4/4 steps  total 2403ms
```

`2 actions  ✓  2 ok` confirms both `list_open_issues` and `list_prs` hit the CORRECT GitHub endpoints (`octocat/Hello-World`) and returned 200 — the user-visible failure mode is gone.

## Files Modified

**Production code (pkg/parser):**

- `pkg/parser/interpolation.go` — `desugarInterpolation` gains `disambiguator string` parameter; threads through to `captureLambdaAtPosition`.
- `pkg/parser/lambda_capture.go` — `captureLambdaAtPosition` gains `disambiguator string`; appends `":"+disambiguator` to base D-18 ID when non-empty. Empty preserves historical format.
- `pkg/parser/builtins.go` — `desugarActionRefKwargs` extracts `keyStr` from each kwarg key (with D-11 defensive error guard), passes `string(keyStr)` to `desugarInterpolation`. Four other call sites (`builtinFlow.name`, `builtinStep.name`, `builtinFail.msg`, `builtinScript.id`) pass `""`.
- `pkg/parser/builtins_log.go` — Log builtin (`builtinLog.msg`) passes `""`.
- `pkg/parser/result_value_capture.go` — `result.value` lambda capture passes `""`.

**Tests:**

- `pkg/parser/interpolation_test.go` — 11 existing test call-site updates (all pass `""` as the new third argument; preserves stable-ID assertions).
- `pkg/parser/interpolation_collision_test.go` (NEW) — Two regression subtests + inline `multiKwargExtension` helper.
- `examples/http-github-webhook/public_repo_check_smoke_test.go` (NEW) — One regression test parsing the real EX-04 flow.

## Commits

| Task | Hash      | Subject                                                                |
| ---- | --------- | ---------------------------------------------------------------------- |
| 1    | `5b0af66` | fix(parser): disambiguate interpolation lambda IDs by kwarg key        |
| 2    | `34ac83b` | test(parser): pin multi-kwarg interpolation lambda ID disambiguation   |
| 3    | `b34cb91` | test(examples): in-suite smoke for public_repo_check kwarg lambda resolution |

Task 4 was verification-only — no separate commit per the plan.

## Self-Check: PASSED

- All 8 files (2 created, 6 modified) verified on disk.
- All 3 commits (`5b0af66`, `34ac83b`, `b34cb91`) verified via `git log --oneline --all`.
- All verification tables above re-checked against test output captured during execution.
