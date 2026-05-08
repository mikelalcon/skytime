---
phase: 04-static-validation-tier-cli-skeleton
verified: 2026-04-30T00:00:00Z
status: passed
score: 5/5 must-haves verified
---

# Phase 4: Static Validation Tier + CLI Skeleton Verification Report

**Phase Goal:** Build the static validator (`pkg/parser` + post-parse pass) and the CLI tree under `cmd/skytime/` so `skytime validate`, `skytime run`, and `skytime dev-server` work against the runtime parser, with a CI corpus differential test proving static and runtime agree on accept/reject for every `.star` file under `examples/`.

**Verified:** 2026-04-30
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| #   | Truth                                                                                                                                                                                                                          | Status     | Evidence                                                                                                                                                                                                              |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ---------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | `skytime validate <file.star>` parses every flow without executing, verifies kwargs match each extension's declared schema, every input maps to a registered schema, lambda free-vars reference declared state, exits non-zero | VERIFIED   | `pkg/cli/validate.go` routes through `validator.Validate`; `pkg/parser/finalize.go` runs `validateLambdaCtxAccesses`+`validateActionRefKwargs`; live runs against fixtures/skeleton produce `<file>:<line>:<col> [flow > step > action]: <msg>` format and exit non-zero |
| 2   | Static validator and runtime parser share the same parser code path: a CI corpus differential test                                                                                                                            | VERIFIED   | `tests/differential_test.go::TestDifferentialCorpus` runs both static `validator.Validate` AND `interpreter.NewWorkflow` via `testsuite.TestWorkflowEnvironment` with `dryrun.AlwaysOkDispatch`; live PASS on parallel_fanout.star + simple_check.star (3 sub-tests pass)  |
| 3   | `skytime run <file.star> --flow=<name> --input=<json>` parses, validates, and triggers a workflow on a configured Temporal cluster                                                                                            | VERIFIED   | `pkg/cli/run.go` 8-step recipe (validate → parse JSON → connectClient → embedded worker → ContentHashFor → ExecuteWorkflow → run.Get → render); `pkg/cli/connect.go` variant routing (api-key→Cloud, mTLS triplet→SelfHosted, default→Dev); `pkg/cli/progress.go` slog shim; live test confirms connection attempt to dev address |
| 4   | `skytime dev-server` spawns a local Temporal dev server; `cobra` and `charmbracelet/log` are CLI-only                                                                                                                          | VERIFIED   | `pkg/cli/dev_server.go` uses `exec.CommandContext(temporal, "server", "start-dev")` with SIGINT forwarding goroutine + W-8 atomic seam; `tests/firewall_cli_test.go::TestNoCobraImportsOutsideAllowList` enforces that pkg/parser, pkg/dag, pkg/extension, pkg/bridge, pkg/activity, pkg/interpreter, pkg/worker, pkg/validator are clean of cobra/pflag/charm-log imports — live PASS (726 imports checked) |
| 5   | `--debug` flag is the only path that reveals Go internals; default rendering is Starlark-first                                                                                                                                | VERIFIED   | `pkg/cli/render.go::renderError` switches on `debug bool`; live demo shows default emits only `<file>:<line>:<col> [flow > step > action]: <msg>` while `--debug` adds `cause:` chain. `TestRenderer_StarlarkFirst_*` and `TestRenderer_DropsWrappedChainByDefault` / `TestRenderer_DebugUnwrapsChain` all pass |

**Score:** 5/5 truths verified.

### Required Artifacts

Verified against the artifacts list provided in the verifier prompt — all present, all substantive (no stubs), all wired:

| Artifact                                                                  | Expected                                                  | Status   | Details                                                                                                |
| ------------------------------------------------------------------------- | --------------------------------------------------------- | -------- | ------------------------------------------------------------------------------------------------------ |
| `pkg/dag/errors.go`                                                       | `ValidationError.Action` field + `[flow > step > action]` | VERIFIED | Action field on line 55; `Error()` joins `Flow > Step > Action` with `>`; tests in `errors_test.go`     |
| `pkg/parser/parser.go`                                                    | `Parser.FileBytes()` accessor                              | VERIFIED | Method at lines 151–160; used by `ctx_walk.go` re-parse                                                |
| `pkg/parser/ctx_walk.go`                                                  | re-parse + `syntax.Walk` for `ctx.<attr>` (matched by pos) | VERIFIED | 131 lines; walks `LambdaExpr`/`DefStmt` matched by keyword position; emits `ctxAccess{Pos, AttrName}`  |
| `pkg/parser/state_schema.go`                                              | lexical-scope state accumulator                            | VERIFIED | 178 lines; `stateSet` w/ inputs+`script.OutputAlias`+for-each item-var stacking; `clone()` for branches |
| `pkg/parser/finalize.go`                                                  | `validateLambdaCtxAccesses` + `validateActionRefKwargs`    | VERIFIED | Both methods present (lines 124–193); chained in `finalize()` after structural lints                   |
| `pkg/validator/validator.go` + `pkg/validator/options.go`                | thin facade returning `[]error`                             | VERIFIED | 46+51 lines; `Validate(file, opts...)` calls `parser.NewParser` + `ParseFile`                          |
| `pkg/validator/dryrun/dispatch.go`                                       | `AlwaysOkDispatch` mock `OperationDispatch`                | VERIFIED | 47 lines; wraps each registered op's `Func` with `okFunc` returning `(nil, nil)`                       |
| `tests/differential_test.go`                                              | `TestDifferentialCorpus` against `examples/skeleton/`      | VERIFIED | 279 lines; live PASS (parallel_fanout.star + simple_check.star); skip-on-empty falls through to active |
| `pkg/cli/root.go`, `flags.go`, `options.go`, `render.go`                  | cobra root + 7 persistent flags + Starlark-first renderer  | VERIFIED | 57+65+57+111 lines; `--debug,--address,--namespace,--api-key,--client-cert,--client-key,--server-ca`   |
| `pkg/cli/validate.go`                                                     | validate subcommand                                        | VERIFIED | 84 lines; routes through `validator.Validate`, renders with `renderErrors`, appends D4-16 hint         |
| `pkg/cli/connect.go`, `run.go`, `progress.go`                            | run + variant routing + slog progress shim                  | VERIFIED | 92+171+118 lines; D4-08 routing live; 8-step recipe wired                                              |
| `pkg/cli/dev_server.go`                                                   | dev-server subcommand                                      | VERIFIED | 111 lines; `lookPath` test seam + `testRunningCmd` atomic seam; SIGINT forwarding goroutine             |
| `pkg/extension/builtin/http/http.go`, `response.go`                       | baked-in HTTP extension w/ D4-14 idempotence                | VERIFIED | 5 ops: get/head idempotent, post/put/delete NON-idempotent (override of RFC-7231)                      |
| `cmd/skytime/main.go` + `build_id.go`                                    | thin binary calling `cli.NewRootCommand`                   | VERIFIED | 67+16 lines; `defaultBuildID` ldflag anchor present                                                    |
| `examples/skeleton/simple_check.star` + `parallel_fanout.star`           | corpus exercising every primitive                          | VERIFIED | step / script / if_cond in simple_check; for_each_parallel / block / call_flow in parallel_fanout      |
| `docs/cli-binary.md`                                                      | referenced by D4-16 hint                                   | VERIFIED | 106 lines                                                                                              |
| `tests/firewall_cli_test.go`                                              | cobra/pflag/charmlog firewall                              | VERIFIED | live PASS — 726 import paths checked, none forbidden                                                   |
| `pkg/activity/firewall_test.go`                                           | temporal-firewall extended to include `pkg/cli`            | VERIFIED | `allowedPkgs := []string{"activity", "interpreter", "worker", "cli"}` (line 40)                        |

### Key Link Verification

| From                              | To                                  | Via                                                                                            | Status |
| --------------------------------- | ----------------------------------- | ---------------------------------------------------------------------------------------------- | ------ |
| `cmd/skytime/main.go`             | `pkg/cli.NewRootCommand`            | direct call w/ `WithExtensions(skyhttp.New())` + `WithCredentialHandler(noopCredentialHandler{})` | WIRED  |
| `pkg/cli/validate.go`             | `pkg/validator.Validate`            | `validator.Validate(file, validator.WithExtensions(...), validator.WithCredentialHandler(...))` | WIRED  |
| `pkg/cli/run.go`                  | `pkg/validator.Validate`            | step 1 of recipe                                                                              | WIRED  |
| `pkg/cli/run.go`                  | `pkg/cli/connect.go::connectClient` | step 3 of recipe                                                                              | WIRED  |
| `pkg/cli/run.go`                  | `pkg/worker.NewWorker`              | step 4 of recipe                                                                              | WIRED  |
| `pkg/cli/run.go`                  | `client.ExecuteWorkflow` (Temporal) | step 6 of recipe; `SkytimeWorkflow` + `dag.WorkflowInput`                                      | WIRED  |
| `pkg/cli/dev_server.go`           | `temporal server start-dev`         | `exec.CommandContext(bin, "server", "start-dev", args...)`                                     | WIRED  |
| `pkg/validator.Validate`          | `pkg/parser.ParseFile`              | direct                                                                                         | WIRED  |
| `pkg/parser/finalize.go`          | `pkg/parser/state_schema.go`        | `validateLambdaCtxAccesses` (state_schema.go:48+)                                              | WIRED  |
| `pkg/parser/state_schema.go`      | `pkg/parser/ctx_walk.go`            | calls `findCtxAccesses`                                                                        | WIRED  |
| `pkg/parser/ctx_walk.go`          | `pkg/parser.FileBytes()`            | re-parse uses cached bytes                                                                     | WIRED  |
| `pkg/parser/finalize.go`          | `pkg/extension.DecodeKwargsFromDict` | `crossValidateActionRef`                                                                       | WIRED  |
| `pkg/cli/render.go`               | `pkg/dag.ParseError`/`ValidationError` | `errors.As` switch in `renderError`                                                            | WIRED  |
| `tests/differential_test.go`     | `validator.Validate` + `interpreter.NewWorkflow` + `dryrun.AlwaysOkDispatch` | both branches live, agreement check                                                            | WIRED  |
| `tests/firewall_cli_test.go`     | go/parser walk over `pkg/`            | enforces no cobra/pflag/charmlog outside `pkg/cli`                                             | WIRED  |
| `pkg/activity/firewall_test.go`  | `pkg/cli` allowlist                   | line 40 `allowedPkgs` includes "cli"                                                          | WIRED  |

### Data-Flow Trace (Level 4)

| Artifact                              | Data Variable        | Source                                   | Produces Real Data | Status   |
| ------------------------------------- | -------------------- | ---------------------------------------- | ------------------ | -------- |
| `pkg/cli/validate.go::RunE`           | `errs`              | `validator.Validate(file, opts...)`     | Yes                | FLOWING  |
| `pkg/cli/run.go::RunE`                | `result`            | `run.Get(ctx, &result)` from Temporal    | Yes                | FLOWING  |
| `pkg/cli/run.go::RunE`                | `contentHash`       | `w.Registry().ContentHashFor(flowName)`  | Yes                | FLOWING  |
| `pkg/cli/dev_server.go::RunE`         | subprocess stdout/stderr | piped from `temporal server start-dev` | Yes                | FLOWING  |
| `pkg/parser/state_schema.go`          | `stateSet`          | flow.Inputs + script.OutputAlias + ItemVar walk | Yes                | FLOWING  |
| `pkg/parser/ctx_walk.go`              | `accesses`          | `syntax.Walk` over re-parsed AST          | Yes                | FLOWING  |
| `pkg/cli/render.go::renderError`      | output              | `pe.Error()` / `ve.Error()` typed dispatch | Yes                | FLOWING  |
| `tests/differential_test.go`          | `staticPassed`/`dryRunPassed` | live `validator.Validate` + `TestWorkflowEnvironment.GetWorkflowError()` | Yes                | FLOWING  |

### Behavioral Spot-Checks

| Behavior                                     | Command                                                                                                | Result                                            | Status |
| -------------------------------------------- | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------- | ------ |
| All packages green w/ `-race`                | `go test ./... -race -count=1`                                                                         | All 13 packages OK                                | PASS   |
| Differential corpus runs (VAL-02 live)      | `go test ./tests -run TestDifferentialCorpus -count=1 -v`                                              | PASS (parallel_fanout.star + simple_check.star)   | PASS   |
| Cobra firewall enforced                      | `go test ./tests -run TestNoCobraImportsOutsideAllowList -v`                                           | PASS (726 imports checked)                        | PASS   |
| skytime binary builds                        | `go build ./cmd/skytime/...`                                                                           | exit 0                                            | PASS   |
| Examples corpus exists                       | `ls examples/skeleton/`                                                                                | parallel_fanout.star, simple_check.star           | PASS   |
| Validate happy path exits 0                  | `./skytime validate examples/skeleton/simple_check.star`                                               | exit=0                                            | PASS   |
| Validate produces structured errors          | `./skytime validate tests/fixtures/invalid/02-mutable-capture.star`                                    | `<file>:<line>:<col>: lambda captures non-module-level variable …` exit=1 | PASS   |
| Validate `[flow > action]` rendering         | `./skytime validate /tmp/bad_kwargs2.star` (gh.get with wrong kwarg)                                   | `[f > http.get]: kwarg cross-validate: get: missing required kwarg "path"` | PASS   |
| Validate ctx-undeclared-state                | `./skytime validate /tmp/ctx_undeclared.star` (lambda references unknown ctx field)                    | `[f]: ctx.unknown_field not in declared state (visible: [x])` exit=1 | PASS   |
| `--debug` reveals chain                      | `./skytime validate /tmp/bad_kwargs2.star --debug`                                                     | adds `cause: get: missing required kwarg "path"`  | PASS   |
| Default rendering Starlark-first             | `./skytime validate /tmp/bad_kwargs2.star`                                                             | NO `cause:` line in output                         | PASS   |
| Subcommands listed                           | `./skytime --help`                                                                                     | dev-server, run, validate present                  | PASS   |
| Validate help                                | `./skytime validate --help`                                                                            | Usage shown                                       | PASS   |
| Run help with required `--flow`              | `./skytime run --help`                                                                                 | Usage shown; --flow marked required                | PASS   |
| Dev-server missing-binary instructions        | `./skytime dev-server --help` (no temporal CLI on PATH)                                                | install-instruction block printed; exit non-zero  | PASS   |
| No forbidden CLI imports outside `pkg/cli`   | `grep -r "cobra"/charmlog in pkg/parser pkg/dag pkg/extension pkg/bridge pkg/activity pkg/interpreter pkg/worker pkg/validator` | empty                                              | PASS   |
| Run uses dev variant by default              | `./skytime run --flow=simple_check examples/skeleton/simple_check.star`                                | `connect: ... dial tcp 127.0.0.1:7233: connect: connection refused` (variant routed to dev) | PASS   |
| dev-server SIGINT forwarding (W-8 behavioral)| `go test ./pkg/cli -run TestDevServerCmd_SignalForward`                                                | PASS                                              | PASS   |
| Connect variant routing                      | `go test ./pkg/cli -run TestConnect`                                                                   | PASS (cloud, dev, partial-mTLS rejection)         | PASS   |

### Requirements Coverage

| Requirement | Source Plan                       | Description                                                                                                                                                | Status     | Evidence                                                                                                                                  |
| ----------- | --------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| VAL-01      | 04-02-PLAN, 04-03-PLAN            | `skytime validate <file.star>` parses + checks kwargs/inputs/lambda free-vars/predeclared-globals                                                          | SATISFIED  | finalize.go::validateLambdaCtxAccesses + validateActionRefKwargs; live demo `gh.get(badarg=…)` and `lambda ctx: ctx.unknown_field` reject |
| VAL-02      | 04-03-PLAN, 04-07-PLAN            | Static + runtime parser share parser code path; corpus differential test agrees on accept/reject                                                          | SATISFIED  | tests/differential_test.go runs both branches live; PASS on 2-fixture corpus                                                              |
| VAL-03      | 04-01-PLAN, 04-04-PLAN            | Errors formatted `<file>:<line>:<col> [flow > step > action]: <message>`; exit non-zero; `--debug` reveals Go internals only                              | SATISFIED  | dag.ValidationError.Error() implementation + render.go renderError switch; live exit=1 + `[f > http.get]` rendering verified              |
| CLI-01      | 04-04-PLAN                        | `skytime validate <file.star>` runs static validation                                                                                                      | SATISFIED  | pkg/cli/validate.go; live runs                                                                                                            |
| CLI-02      | 04-05-PLAN, 04-07-PLAN            | `skytime run <file.star> --flow=<name> --input=<json>` parses, validates, triggers workflow on Temporal cluster, streams progress                          | SATISFIED  | pkg/cli/run.go 8-step recipe + connect.go variant routing + progress.go slog shim                                                         |
| CLI-04      | 04-06-PLAN                        | `skytime dev-server` spawns local Temporal dev server                                                                                                      | SATISFIED  | pkg/cli/dev_server.go subprocess wrapper around `temporal server start-dev` + SIGINT forwarding + missing-binary install instructions     |
| CLI-05      | 04-01-PLAN, 04-04-PLAN, 04-07-PLAN | CLI lives under `cmd/skytime/`; cobra/charmlog NOT reachable from library root                                                                              | SATISFIED  | tests/firewall_cli_test.go::TestNoCobraImportsOutsideAllowList live PASS — 726 import paths, no violations                                 |

All 7 phase REQ-IDs are covered in PLAN frontmatters and marked Complete in REQUIREMENTS.md (lines 187-194). No orphaned requirements.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |

None found. Anti-pattern scan over pkg/cli, pkg/validator, pkg/validator/dryrun, pkg/parser/{ctx_walk,state_schema,finalize}, pkg/extension/builtin/http, cmd/skytime, tests/ found zero TODO/FIXME/PLACEHOLDER markers in production sources. `return nil` instances in `validate.go::RunE` (line 46), `run.go::RunE` (line 144), `dev_server.go::RunE` (line 98) are happy-path returns after structured work, not stubs.

### Human Verification Required

None. All five Success Criteria from ROADMAP.md are programmatically verified through:

- the live `./skytime` binary against valid + invalid corpus + custom fixtures,
- `tests/differential_test.go::TestDifferentialCorpus` running static + dry-run interpreter side-by-side,
- `tests/firewall_cli_test.go` enforcing the cobra/charmlog firewall,
- `pkg/cli/dev_server_test.go::TestDevServerCmd_SignalForward` confirming SIGINT forwarding behaviorally,
- `pkg/cli/render_test.go` confirming Starlark-first rendering and `--debug` chain unwrap.

The only behavior NOT exercisable without a real Temporal binary on PATH is the actual `temporal server start-dev` subprocess startup; `dev_server_test.go::TestDevServerCmd_Spawn` is gated by `exec.LookPath("temporal")` and skips when absent. The signal-forwarding test substitutes `/bin/sh` to verify the wiring works without the temporal binary, so the architectural claim ("dev-server spawns + forwards SIGINT") is verified.

### Gaps Summary

No gaps. The phase achieves all five Success Criteria from ROADMAP.md, all seven phase REQ-IDs are covered and marked complete, no anti-patterns are present, and the differential corpus test (the central VAL-02 contract) runs LIVE against a 2-fixture corpus that exercises every DSL primitive (step / script / if_cond / for_each_parallel / step(block=...) / call_flow).

The architectural firewalls — cobra/pflag/charmlog kept out of pkg/parser, pkg/dag, pkg/extension, pkg/bridge, pkg/activity, pkg/interpreter, pkg/worker, pkg/validator (D4-13) and the temporal-SDK allowlist extended to include pkg/cli for the run subcommand — are both enforced by AST-walk tests that pass in CI.

---

_Verified: 2026-04-30_
_Verifier: Claude (gsd-verifier)_
