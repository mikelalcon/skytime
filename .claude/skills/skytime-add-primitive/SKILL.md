---
name: skytime-add-primitive
description: Use when adding or modifying a Starlark DSL primitive, builtin, kwarg, or dag Node kind in Skytime — any change touching pkg/parser, pkg/dag, pkg/interpreter walkers, tests/fixtures, or docs/reference/builtins.md.
---

# Adding or Modifying a Skytime DSL Primitive

## Study the reference implementations first

Two primitives were added after the core six; their commits are the canonical playbooks:

- **trigger() (v1.43, top-level declaration, NOT a Node):** `git show abe93a9 07786f1 641631a` — builtin + arity-capture, registration in globals, finalize wiring. Fixtures in `pkg/parser/testdata/triggers/`, tests in `pkg/parser/trigger_test.go`.
- **Expression-mode if_cond + result/fail nodes (new Node kinds):** `git show 99615db c95b567 b2af46d` — dag types + marshaling, interpreter walkers, validator pass. Validation in `pkg/parser/ifcond_expr_validate.go`, execution in `pkg/interpreter/walk_result.go` / `walk_fail.go`.

Read `pkg/parser/builtins.go` (builtinFlow at the top is the annotated template) and `pkg/parser/finalize.go` (pass-ordering doc comment) before writing code.

## Step 0 — classify the change

- **(A) New flow-body Node kind** (like `result`/`fail`/`log.*`): full checklist below.
- **(B) New top-level declaration** (like `trigger`): skip the body-walker updates; see "Top-level declarations".
- **(C) New kwarg on an existing builtin**: see "Adding a kwarg".

## Checklist A — new flow-body Node kind

### 1. dag type (pkg/dag)

- New file in `pkg/dag/` (pattern: `pkg/dag/fail.go` — 30 lines). Implement `Kind() string`, `Position() syntax.Position`, and the seal `func (*X) nodeMarker() {}`. The Node interface is sealed in `pkg/dag/node.go`; the marker MUST live in pkg/dag.
- Never store `*starlark.Function` or resolved secrets on the node. Lambdas are referenced by `*dag.CapturedLambda` / lambda ID strings only (D-18 IDs: `sha256(fileBytes)[:4]` hex + `:line:col`, computed by `dag.ComputeLambdaID` in `pkg/dag/lambda.go`).
- If the node produces per-key output (like Result), store keys as an ordered `Keys []string` slice; consumers iterate Keys, never `range` the values map — that's the replay-determinism contract (`pkg/dag/result_node.go`).

### 2. JSON marshaling (pkg/dag/marshal.go)

- Add a `xJSON` alias struct + `MarshalJSON` with a `"kind"` discriminator. Read the file-header comment: **never serialize Pos** (absolute filenames break cross-machine goldens) and never serialize lambda functions — emit lambda IDs at most. `resultJSON` deliberately omits Values/Types because lambda IDs change on ANY byte edit to the source file.
- Pin with a test in `pkg/dag/marshal_test.go` (pattern: `TestActionRef_MarshalJSON_OmitsPos`).

### 3. Parser builtin (pkg/parser/builtins.go)

- Method on `*Parser`: `func (p *Parser) builtinX(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error)`.
- Unpack with `starlark.UnpackArgs`. UnpackArgs cannot tell omitted from zero-value: for kwargs where `""` must be rejected only-when-supplied, use the `hasKwarg(kwargs, "name")` helper (see task_queue handling in builtinFlow/builtinStep).
- Position: `callerPosition(thread)` (CallFrame(1) Lparen). Inside `Thread.Load` the stack is 1 deep — `pkg/parser/load.go` uses `callerPositionOrZero`. Copying the wrong helper silently yields zero positions.
- Every error out of the builtin must be `*dag.ParseError` or `*dag.ValidationError` (PARSE-05). Raw starlark errors are wrapped by `wrapStarlarkError` in `pkg/parser/errors.go`; don't return naked `fmt.Errorf`. Error format is locked: `<file>:<line>:<col>[ [flow > step > action]]: msg` (`pkg/dag/errors.go`, regex-pinned by `posFormatRe` in `pkg/parser/fixtures_test.go`).
- Lambda kwargs: capture via `captureLambda` or `captureLambdaWithArity` (`pkg/parser/lambda_capture.go`). Free variables must bind at module level (`validateFreeVars` in `pkg/parser/linter.go`). If a string kwarg should support `${ctx.x}`, reuse `desugarInterpolation` (`pkg/parser/interpolation.go`) and pass a non-empty disambiguator when multiple interpolated kwargs share one call position.
- Wrap the returned node in the `nodeValue` shim like the other builtins so it can sit in `steps=[...]`.

### 4. Register the name (pkg/parser/globals.go)

Add `"x": starlark.NewBuiltin("x", p.builtinX)` to `newParseTimeGlobals`. This env is disjoint from the lambda-time env — do NOT touch `pkg/bridge/lambda_globals.go`: it is locked at exactly 20 keys (22 for triggers) by `TestLambdaTimeGlobalsLocked` / `TestTriggerTimeGlobalsLocked` in `pkg/bridge/lambda_globals_test.go`; expanding it requires a PROJECT.md decision.

### 5. Finalize passes and body walkers (the expensive part)

There is NO shared visitor. If your node can CONTAIN other nodes (like IfCond.Then/Else, ForEachParallel.Steps), you must add a recursion arm to every hand-rolled walker:

| Walker | File |
|---|---|
| walkResolveCallFlows | pkg/parser/finalize.go |
| walkValidateActionRefKwargs | pkg/parser/finalize.go |
| walkBodyForLogSteps | pkg/parser/finalize.go |
| walkLintMixedIdempotency, walkLintBlockSize, walkLintEmptyTaskQueue | pkg/parser/linter.go |
| walkLintBlockFnIdempotency | pkg/parser/block_fn_lint.go |
| walkBodyForCtxValidation | pkg/parser/state_schema.go |
| walkValidateResultPlacement | pkg/parser/builtins.go |
| walkValidateIfCondExpression | pkg/parser/ifcond_expr_validate.go |

Leaf-like nodes only need arms where they carry lambdas/ActionRefs (ctx validation, kwarg cross-validation).

If you add a NEW finalize pass, slot position is a pinned contract (`finalize()` doc comment in `pkg/parser/finalize.go`; order pinned by ordering tests in `pkg/parser/finalize_test.go` — e.g. `TestFinalize_LintOrder_CallFlowResolutionShortCircuits`, `TestValidateIfCondExpressionShape_FinalizeOrdering` — and `pkg/parser/block_fn_lint_test.go`): call_flow resolution → trigger flow names → lints → ctx-walk → req-walk → result placement → log placement → if_cond expression shape → ActionRef kwargs → duplicate-trigger warnings (always last, always nil). Inserting in the wrong slot changes which single error surfaces and breaks tests.

Note: ctx validation re-parses the ORIGINAL cached file bytes (`p.fileBytes`) and matches lambdas by exact (Filename,Line,Col) of the `lambda` keyword; synthesized lambdas carry `BodyPos` into `<interp:...>` files — every walker must prefer `BodyPos` when valid.

### 6. Interpreter execution (pkg/interpreter)

- Add a `case *dag.X:` arm to `walkNode` in `pkg/interpreter/workflow.go` and a `walk_x.go` file. The `default:` arm returns a NonRetryable "unknown node kind" error — a new Node kind without an arm fails every workflow that contains it, deterministically.
- Follow the named-return + defer pattern for `step_dispatch`/`step_complete` slog events (see `walk_script.go`). Any early return must assign the named `err` or completion reports status=ok on failure.
- If the node is a side-channel that must NOT count toward `[N/M]` step numbering (like LogStep), mirror the non-log pre-count in `walkBody` (`workflow.go`, `nonLogTotal` loop) — otherwise step numbering drifts and replay tests fail.
- Determinism rules: no `range` over Go maps (use sorted keys or ordered slices), no `time.Now`/`rand`/native `go` — only `workflow.Now`/`workflow.Go`. Lambda eval goes through `evalLambda` → `bridge.CallLambda` (fresh thread, 10M step cap, zero history events). `pkg/interpreter/replay_determinism_test.go` byte-compares two runs.
- `pkg/parser` must never import `go.temporal.io/*` — `TestNoTemporalImportsInParserPackage` in `pkg/parser/parser_test.go`. Temporal SDK imports are allowlisted to pkg/{activity,interpreter,worker,cli,testing,extension/receiver,extension/schedules} by `pkg/activity/firewall_test.go`.

### 7. Fixtures and goldens

- **Valid:** add `tests/fixtures/valid/NN-name.star`. If you also add a `.golden.json`, regenerate with:
  `UPDATE_GOLDEN=1 go test ./pkg/parser/... -run TestValidFixtures`
  Verify: re-run WITHOUT the env var and confirm `ok`. Goldens exclude lambda-bearing fields on purpose (whole-file-hash IDs churn on any edit) — don't add them.
- **Invalid:** add `tests/fixtures/invalid/NN-name.star` whose FIRST line is `# expects: <error substring>`. `TestInvalidFixtures` asserts the substring, the typed-error contract, and the D-04 position format.
- **Unit fixtures:** `pkg/parser/testdata/` (e.g. `testdata/triggers/`) for fixtures driven by a dedicated `*_test.go`, not by the generic fixture runner.
- Any edit to an EXISTING valid fixture changes every lambda ID in that file — expect golden churn; regenerate, don't hand-edit.

### 8. Differential corpus

`tests/differential_test.go::TestDifferentialCorpus` walks ALL `.star` under `examples/skeleton/` (including `*_test.star`) and requires static validator and dry-run interpreter to AGREE accept/reject, with no panics. If you add a demo fixture there:
- flows that `fail()` under stub inputs go in the `expectedErrFlows` map;
- new extension needs go in `corpusExtensions(t)`;
- add an index entry to `examples/README.md` (fixed section template).
Verify: `go test ./tests/ -run TestDifferentialCorpus -count=1`

### 9. Docgen

- Add `// skytime:doc` markers directly above the builtin func (copy the builtinFlow block in `pkg/parser/builtins.go`: summary, returns, since, example, see, param_X/desc_X pairs).
- Regenerate: `go generate ./pkg/parser/` (runs `cmd/skytime-docgen` → `docs/reference/builtins.md`). Commit the regenerated file.
- Verify: `go test ./tests/ -run TestDocgenDrift -count=1`. Never hand-edit builtins.md — nothing stops you at edit time, but the drift test fails CI.

### 10. Tier-3 harness and examples

- `pkg/testing` (tester.run) executes the PRODUCTION workflow twice through the production parser — new builtins flow through automatically, but the double-run replay diff will catch any nondeterminism in your walker.
- A genuinely new primitive extends the DSL primitive set: `examples/http-github-webhook/flows_test.go::TestFlows_CoverageMatrix` pins that example flows collectively cover every primitive, and the example README coverage table is kept in sync BY HAND.
- Update user docs: `docs/for-flow-authors/` and possibly `docs/getting-started.md` (canonical) + README.md (embedded copy, manual sync per D-06).

### 11. Full gate

Run what CI runs (`.github/workflows/ci.yml`): `go vet ./...` then `go test -race ./... -count=1`. The e2e steps additionally need the `temporal` CLI on PATH; they skip locally when absent — green local run does not prove e2e ran.

## Checklist B — top-level declarations (trigger-shaped)

Do NOT make it a `dag.Node` (no `nodeMarker`) — flow-body walkers must never grow arms for it (`pkg/dag/trigger.go` doc explains why). Instead: builtin registers into a parser-session map keyed by `posKey(pos)` (like `p.triggers`), parser grows sorted accessor(s) (`Triggers()` returns copies; sort deterministically), finalize gains dedicated validators wired in the pinned order (`pkg/parser/req_walk.go`), a parallel frozen registry in `pkg/interpreter/registry.go`, and boot wiring in `pkg/worker/boot.go` (which skips `*_test.star`). Lambdas with a fixed signature go through `captureLambdaWithArity` (rejects defaults, *args, **kwargs).

## Checklist C — adding a kwarg to an existing builtin

1. Add to the `UnpackArgs` list (`name?` for optional). If empty-string must be rejected only when supplied, add a `hasKwarg` presence check.
2. Thread into the dag type + `xJSON` marshal struct (`omitempty` for optional) — expect golden regen (step 7).
3. If the interpreter consumes it, update the walker + tests.
4. Add `param_X`/`desc_X` docgen markers + `go generate ./pkg/parser/` (step 9).
5. Add/extend fixtures. If the kwarg accepts `${ctx...}`, remember interpolation lambda IDs get a `:kwargKey` suffix — changing disambiguator behavior breaks historical ID rehydration.

## Common mistakes (test that fails → what it means)

- **`TestValidFixtures` golden mismatch** ("golden mismatch for ... run UPDATE_GOLDEN=1...") after an apparently unrelated fixture edit: whole-file lambda-ID churn or your new JSON field. Regenerate; review the diff — if lambda IDs or absolute paths appear in the golden, your MarshalJSON is wrong.
- **`TestDocgenDrift` fails** ("docs/reference/builtins.md is out of date"): you changed a builtin signature or `skytime:doc` marker without `go generate ./pkg/parser/`, or hand-edited builtins.md.
- **`TestInvalidFixtures` fails with "error must be *dag.ParseError or *dag.ValidationError, got \*errors.errorString"**: your builtin returned a raw error. Wrap it.
- **Workflow fails at runtime with "unknown node kind X"**: you added the dag type but forgot the `walkNode` arm in `pkg/interpreter/workflow.go`.
- **Validator accepts, runtime rejects (or vice versa) → `TestDifferentialCorpus` fails**: parser lint and interpreter/activity defense diverged. Keep limits paired (e.g. `parser.WithMaxBlockSize` vs `activity.WithMaxBlockSize`, both default 50).
- **A finalize ordering test fails after adding a pass** (`TestFinalize_LintOrder_CallFlowResolutionShortCircuits` / `TestValidateIfCondExpressionShape_FinalizeOrdering` in `pkg/parser/finalize_test.go`, or the ordering pins in `pkg/parser/block_fn_lint_test.go`): you changed which error surfaces first. Re-read the ordering contract; move your pass to the slot the doc comment assigns.
- **A lint silently misses nodes nested inside your new container node**: you skipped one of the ~10 walkers in the table. Grep `case *dag.IfCond:` across pkg/parser to find every switch you must extend.
- **`TestReplay_*` / `replay_determinism_test.go` byte-diff failures**: map iteration, time, or goroutine ordering leaked into your walker; or you emitted DEBUG-level slog events (filtered — silently weakens diffing) or dropped path/pos/name attrs on step_dispatch.
- **Step counter drift `[N/M]` in walk_log tests**: new side-channel node not mirrored in walkBody's non-log pre-count.
- **`TestNoTemporalImportsInParserPackage` fails**: you imported temporal (even indirectly) into pkg/parser. Push the logic to pkg/interpreter or pkg/dag.
- **Zero positions in errors**: used `fn.Position()` or the wrong caller-position helper; use `callerPosition(thread)` in builtins.
- **`TestLambdaTimeGlobalsLocked` fails**: you tried to expose your builtin (or a helper) to lambda-time. Don't — parse-time and lambda-time envs are disjoint by design; fixture `tests/fixtures/invalid/07-forbidden-lambda-builtin.star` pins this.
- **`result()`-style cache-miss error** ("result() constructed via non-source path"): builtins that need source-literal arguments (result values, log messages) work by AST re-parse of ORIGINAL bytes, not evaluated values — see `preExecBuildResults` and `assertLogMsgLiteralAt` (`pkg/parser/builtins_log.go`) before imitating.
- **Dashboard golden fails** (`pkg/cli/server/web/templates_test.go`) after UI-adjacent edits: regenerate with `GSD_UPDATE_GOLDEN=1 go test -run TestTemplate_DashboardGolden ./pkg/cli/server/web`, then re-run without the env var.

## Lambda ID stability rules (if your primitive captures lambdas)

- IDs hash the WHOLE original file: `sha256(fileBytes)[:4]` hex (8 hex chars — doc comments saying "[:8]" mean the same thing; don't "fix" either side) + `:line:col`, over ORIGINAL user bytes, never the sentinel-rewritten execSrc.
- Workers re-parse all `.star` at boot and rehydrate lambdas by ID — changing ID composition (e.g. the `:kwargKey` interpolation suffix) breaks in-flight workflows: any content-hash mismatch makes running workflows fail `FlowNotInRegistry`.
- The sentinel rewrite in `preExecBuildResults` must stay length- and newline-preserving; every AST re-parse reads `p.fileBytes` originals, and byte-offset position math assumes ASCII (documented approximation).

## Verification one-liners

```
go test ./pkg/parser/ -run 'TestValidFixtures|TestInvalidFixtures' -count=1
go test ./tests/ -run 'TestDocgenDrift|TestDifferentialCorpus' -count=1
go test ./pkg/interpreter/ -count=1        # replay determinism + walkers
go vet ./... && go test -race ./... -count=1   # full CI-equivalent (slow)
```
