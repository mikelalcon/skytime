---
name: skytime-architecture
description: Load before starting ANY non-trivial task in the Skytime repo (Starlark-to-Temporal workflow library) — gives the package map, .star→DAG→worker→ExecuteBatch data flow, hard invariants (parse/execute split, replay determinism, credential firewall, import allowlists), glossary, and a where-to-look file index.
---

# Skytime Architecture Orientation

Skytime is a Go library: flow authors declare durable workflows in `.star` (Starlark) files; a
Temporal worker executes them. A `.star` file is parsed into a deterministic pure-data DAG at
parse time; the worker walks that DAG at execution time. The parse/execute boundary is absolute:
no string compilation, no dynamic activities, no context bleed. Read `docs/architecture.md` for
the full narrative; this skill is the fast index.

## Package map

| Package | Role |
|---|---|
| `pkg/parser` | `.star` → `pkg/dag` nodes. Builtins (`flow`, `step`, `if_cond`, `script`, `for_each_parallel`, `call_flow`, `trigger`, `result`, `fail`, `log.{info,warn,error,debug}`) registered in `pkg/parser/globals.go`, implemented in `pkg/parser/builtins.go` (`log.*` in `pkg/parser/builtins_log.go`). Never imports Temporal. |
| `pkg/dag` | Sealed pure-data node types (`pkg/dag/node.go`), `CapturedLambda` + `ComputeLambdaID` (`pkg/dag/lambda.go`), `ActionRef` (`pkg/dag/action.go`), JSON marshaling rules (`pkg/dag/marshal.go`), `ParseError`/`ValidationError` (`pkg/dag/errors.go`). |
| `pkg/validator` | Thin façade over the parser (`pkg/validator/validator.go`) + dry-run interpreter (`pkg/validator/dryrun/`). Powers `skytime validate`. |
| `pkg/bridge` | Starlark↔Go value conversion and lambda evaluation (`lambda_call.go`); the locked lambda-time/trigger-time predeclared environments (`lambda_globals.go`). Must NOT import Temporal. |
| `pkg/interpreter` | The single `SkytimeWorkflow` (`workflow.go::NewWorkflow`), DAG walkers (`walk_*.go`), frozen `FlowRegistry`/`TriggerRegistry` (`registry.go`), replay-capture harness (`replay_helper.go`). |
| `pkg/worker` | Worker boot: re-parses every non-`*_test.star` file under RootDir (`boot.go::bootRegistry`), registers exactly one workflow ("SkytimeWorkflow") + one activity ("ExecuteBatch") (`worker.go`). |
| `pkg/activity` | The only extension-I/O Temporal adapter: `ExecuteBatch` (`execute_batch.go`), batch re-validation (`validate_batch.go`), JIT credential resolve + TTL cache (`action_executor.go`, `credential_cache.go`, `classify.go`). |
| `pkg/extension` | Temporal-free extension SDK: `Extension`/`OperationSpec` (`extension.go`, `operation.go`), redacting `Secret` (`secret.go`), `CredentialHandler` (`handler.go`), sealed `TriggerSource` (`trigger.go`). Sub-packages: `builtin/http`, `builtin/core` (cron), `receiver` (webhooks), `schedules` (cron reconciler), `credfile`, `testing`. |
| `pkg/testing` | Tier-3 harness: `*_test.star` files with `tester.workflow/mock_action/run` (`runner.go`, `router.go`, `cli_run.go`). |
| `pkg/cli` | Cobra CLI (only package allowed to import cobra/lipgloss/charm-log): `root.go`, `validate.go`, `run.go`, `server.go`, `test.go`, `info.go`, `cron_plan.go`, `dev_temporal.go`. Dashboard/SSE under `pkg/cli/server/web/`. |
| `tests/` | Cross-tree gates: import firewalls, grep gates, docgen drift, differential corpus, subprocess E2Es. Lives outside `pkg/` deliberately. |
| `cmd/skytime`, `cmd/skytime-docgen` | Binaries. `examples/skeleton` (differential corpus), `examples/http-github-webhook` (two-tier reference project). |

## Data flow (end to end)

1. **Parse**: `pkg/parser/parser.go` executes the `.star` file against a locked predeclared env
   (no `set`/`while`/recursion). Builtins construct dag nodes; lambdas are captured with
   content-hash IDs (`sha256(fileBytes)[:4]` hex + `:line:col` — `pkg/dag/lambda.go`).
   `${ctx.x}` string interpolation desugars into synthesized lambdas (`pkg/parser/interpolation.go`).
2. **Finalize**: 13 ordered validation passes in `pkg/parser/finalize.go` (call_flow resolution →
   trigger flow-name check → lints → ctx-typo walk → trigger req-walk → result/log placement →
   if_cond expression shape → kwarg cross-validation → duplicate-trigger warnings). First error
   wins; pass ORDER is pinned by ordering tests in `pkg/parser/finalize_test.go` (e.g.
   `TestFinalize_LintOrder_CallFlowResolutionShortCircuits`) and `pkg/parser/block_fn_lint_test.go`.
3. **Boot**: `pkg/worker/boot.go::bootRegistry` re-parses all non-test `.star` files under RootDir
   (sorted paths, sha256 content hash per file), builds frozen `FlowRegistry` + `TriggerRegistry`.
   Lambdas are re-created here — they never cross the Temporal wire ("Option B").
4. **Start**: workflow input is `dag.WorkflowInput{FlowName, ContentHash, InitState}`. Registry
   miss (any byte edit to the file changes the hash) → non-retryable `FlowNotInRegistry`.
5. **Walk**: `pkg/interpreter/workflow.go` walks the DAG. Lambda evals go through
   `pkg/bridge/lambda_call.go` (fresh Starlark thread, 10M step cap, zero Temporal history events).
   State enters lambdas as a frozen `ctx` struct with sorted keys (`pkg/bridge/struct.go`).
6. **Dispatch**: each `step` becomes ONE `ExecuteBatch` activity call carrying `[]*dag.ActionRef`.
   `for_each_parallel` fans out via `workflow.Go` on shallow interpreter copies
   (`pkg/interpreter/walk_foreach.go`); `call_flow` spawns a child SkytimeWorkflow.
7. **Execute**: `pkg/activity/execute_batch.go` re-validates the batch (≤50 actions, homogeneous
   idempotency, non-idempotent alone), resolves credentials just-in-time per action via
   `extension.CredentialHandler` (TTL cache; bypassed+invalidated when activity Attempt > 1),
   decodes kwargs, calls the extension's `OperationFunc`.
8. **Bind**: results flow back into workflow state; `result(...)` keys bind in source insertion
   order (`Result.Keys` — never range the map).
9. **Triggers** (v1.43): webhooks enter through `pkg/extension/receiver/handler.go`
   (raw-bytes HMAC → lambda → `flowlaunch.Execute` with WorkflowID `{flow}/{posHash8}/{idempotency_key}`,
   REJECT_DUPLICATE); cron via `pkg/extension/schedules/` reconciling Temporal Schedules
   (IDs `skytime/{flow}/{posHash8}` only), with `scheduled_time` injected by
   `pkg/interpreter/cron_input.go`.

## Non-negotiable invariants (each is test-enforced)

- **Parse/execute split**: `pkg/parser` never imports `go.temporal.io/*` —
  `TestNoTemporalImportsInParserPackage` (`pkg/parser/parser_test.go`). `pkg/bridge` is also
  Temporal-free. Never add string compilation or a dynamic activity path.
- **Temporal import allowlist**: only `pkg/{activity,interpreter,worker,cli,testing}` and
  `pkg/extension/{receiver,schedules}` may import the SDK —
  `pkg/activity/firewall_test.go::TestNoTemporalImportsOutsideAllowList` walks the whole module.
- **Extensions never import `pkg/activity`** (dependency direction is activity→extension only).
  Extensions can't build `temporal.ApplicationError`; they signal non-retryable by wrapping
  `extension.ErrNonRetryable`. Plain errors are RETRYABLE by default.
- **Determinism**: no unsorted Go map iteration anywhere in workflow code (use `state.sortedKeys`,
  `Result.Keys`, or Starlark `Dict.Items()` insertion order). No native `go` / `time.Now` / `rand`
  in `pkg/interpreter` — only `workflow.Go` / `workflow.Now`; the single sanctioned native channel
  lives in `pkg/interpreter/cancel_watchdog.go`. Pinned by
  `pkg/interpreter/replay_determinism_test.go`.
- **Credentials never enter workflow state**: dag types and JSON carry credential ID strings only;
  secrets resolve JIT inside the activity or receiver. `extension.Secret` redacts under every
  formatter; raw value only via `Reveal()` (audited grep). Gates:
  `tests/firewall_credential_redaction_test.go` (bans `%+v`/`%#v` in credential packages).
- **One workflow, one activity**: the worker registers exactly "SkytimeWorkflow" and
  "ExecuteBatch" (`pkg/worker/worker.go`). `client.ExecuteWorkflow` may appear in exactly 2
  production files — `tests/firewall_execute_workflow_test.go` fails on ANY new call site.
- **Locked lambda env**: `bridge.LambdaTimeGlobals()` is exactly 20 keys (no time/random/IO),
  trigger-time is 22 — `pkg/bridge/lambda_globals_test.go::TestLambdaTimeGlobalsLocked` /
  `TestTriggerTimeGlobalsLocked`.
  Expansion requires a `.planning/PROJECT.md` decision.
- **Typed errors only**: every malformed `.star` surfaces as `*dag.ParseError` /
  `*dag.ValidationError` formatted `<file>:<line>:<col>[ [flow > step > action]]: msg` — never a
  raw Starlark error or panic (`pkg/parser/errors.go` recover + wrap).
- **Batch semantics**: retryable mid-batch error → return `(nil, err)` so Temporal retries the
  WHOLE (idempotent) batch; non-retryable → return full result list with `nil` error so it does
  NOT retry (`pkg/activity/execute_batch.go`).

## Glossary

- **flow / step**: DSL primitives; a step carries exactly ONE of `action` / `block` /
  `action_fn` / `block_fn` (4-way mutual exclusion, `pkg/parser/builtins.go`).
- **ActionRef**: pure-data reference to one extension operation (`kind` = `"ext.op"`, frozen
  kwargs dict, credential ID string) — `pkg/dag/action.go`.
- **block batching**: a `step(block=[...])` of ≤50 idempotent ActionRefs runs as one
  `ExecuteBatch` activity invocation; non-idempotent ops always batch alone.
- **Lambda content-hash ID (D-18)**: `sha256(wholeFileBytes)[:4]hex:line:col` — any edit anywhere
  in a `.star` file changes EVERY lambda ID in it, and changes the flow's content hash (running
  workflows then fail `FlowNotInRegistry`; Build IDs are the drain mechanism, default "dev").
- **Option B re-parse**: lambdas are never serialized; the worker re-parses source at boot and
  workflows look up `(flow_name, content_hash)` in the frozen registry.
- **TriggerSource**: sealed interface (`pkg/extension/trigger.go`) behind `trigger(...)`;
  concrete kinds: `http.webhook`, `github.webhook` (example), `core.cron`. Deliberately NOT a
  `dag.Node` — flow-body walkers must never grow a Trigger arm.
- **Firewall tests**: AST/grep gates (in `tests/` and per-package `firewall_test.go` files) that
  pin import graphs, call-site counts, and banned literals. They fail on ADDITIONS, and most have
  non-vacuity canaries — don't delete an "unused" allowed call site.
- **Tier-1 / Tier-3**: Tier-1 = static validation (`skytime validate`); Tier-3 = `.star` E2E tests
  (`tester.run` executes the production workflow TWICE and diffs event streams). Tier-2
  (Starlark unit tests) is deferred to v2.

## Where do I look for X

- Add/change a DSL builtin → `pkg/parser/globals.go` (registration) + `pkg/parser/builtins.go`,
  then regenerate docs (below).
- New dag node kind → `pkg/dag/node.go` (sealed marker) + ~10 hand-rolled walkers in
  `pkg/parser/{finalize.go,linter.go,state_schema.go,ifcond_expr_validate.go}` and
  `pkg/interpreter/workflow.go` walkNode. There is NO shared visitor.
- Lambda capture rules / free-var lint → `pkg/parser/lambda_capture.go`, `pkg/parser/linter.go`.
- `${ctx.x}` interpolation → `pkg/parser/interpolation.go`.
- `ctx.<attr>` type checking → `pkg/parser/ctx_walk.go` + `pkg/parser/state_schema.go`.
- Runtime step dispatch / kwargs resolution → `pkg/interpreter/walk_step.go`,
  `pkg/interpreter/resolve_kwargs.go`.
- Retry/non-retryable classification → `pkg/activity/classify.go`, `pkg/extension/error.go`.
- Writing an extension → `pkg/extension/{extension.go,operation.go,registry.go}`; copy the
  pattern from `examples/http-github-webhook/extensions/` or `pkg/extension/builtin/http/`.
- Webhook receive path / HMAC → `pkg/extension/receiver/{handler.go,signature.go,workflow_id.go}`.
- Cron → `pkg/extension/builtin/core/cron.go` + `pkg/extension/schedules/{schedules.go,id.go}`.
- `skytime server` boot/drain order → `pkg/cli/server.go`; dashboard/SSE →
  `pkg/cli/server/web/{mount.go,handlers.go,events/,deliveries/,flowlaunch/}`.
- Tier-3 test harness / mock matching → `pkg/testing/{runner.go,router.go,registry.go}`.
- Parse fixtures → `tests/fixtures/{valid,invalid}` (consumed by `pkg/parser/fixtures_test.go`,
  NOT by `tests/`); invalid fixtures assert `# expects:` substrings.
- Planning/decision history → `.planning/PROJECT.md`, `.planning/STATE.md`. Decision IDs
  (D4-13, D-07-22, ...) are cross-referenced verbatim in code comments and test names — never
  rename them. Repo `CLAUDE.md` routes edits through `/gsd:*` commands.

## Drift gates: regenerate AND verify

Three golden/drift layers fail CI on innocent-looking edits:

1. **Parser goldens** (`tests/fixtures/valid/*.golden.json`): after changing parser output or a
   valid fixture, run `UPDATE_GOLDEN=1 go test ./pkg/parser/... -run TestValidFixtures`.
   Verify: re-run WITHOUT the env var and confirm it passes; `git diff tests/fixtures/valid/`
   should show only intended changes.
2. **Builtins reference** (`docs/reference/builtins.md`): after touching any builtin signature or
   `// skytime:doc` marker, run `go generate ./pkg/parser/` and commit the regenerated file.
   Verify: `go test ./tests/ -run TestDocgenDrift -count=1` passes. Never hand-edit builtins.md.
3. **Dashboard golden**: after editing `pkg/cli/server/web/dashboard.html` or `templates.go`, run
   `GSD_UPDATE_GOLDEN=1 go test -run TestTemplate_DashboardGolden ./pkg/cli/server/web`.
   Verify: re-run without the env var.

CI (`.github/workflows/ci.yml`) runs: `go vet ./...`, `go test -race ./... -count=1` (needs the
Temporal CLI on PATH for e2e; those tests SKIP when it's absent — green is not proof they ran),
`extbin test ./examples/http-github-webhook/`, the walkthrough smoke script, and a standalone
build+test of `docs/for-extension-developers/snippets/` (its own Go module — must never require
the main module).

## Common mistakes

- **Editing any tracked file with the literal "dev-server"** (even a comment) →
  `tests/dev_server_grep_test.go::TestNoDevServerLiteralRemains` fails repo-wide. Write
  "dev-temporal" or "temporal dev server". Allowed only in `.planning/` and `CHANGELOG.md`.
- **Trusting the repo-root binaries**: `./skytime`, `./extbin`, `./skytime-docgen` at repo root
  are stale local builds (`./skytime --help` still shows pre-rename `dev-server`). Always
  `go run ./cmd/skytime` or rebuild.
- **Adding a Temporal/cobra import in the wrong package** → an AST firewall fails with a
  file-list error naming the violator (`TestNoTemporalImportsOutsideAllowList`,
  `tests/firewall_cli_test.go::TestNoCobraImportsOutsideAllowList`,
  `tests/firewall_web_stdlib_test.go`). Don't "fix" by editing the allowlist without a decision.
- **Sorting keys "for determinism"** in `pkg/interpreter/resolve_kwargs.go` or `walk_log.go`:
  `Dict.Items()` is already insertion-ordered; adding a sort CHANGES activity inputs and breaks
  replay of in-flight histories. Explicit warning comment in `resolve_kwargs.go`.
- **Editing a `.star` fixture and being surprised**: every lambda ID in the file changes
  (whole-file hash) and golden JSONs mismatch — the failure message names the regen command.
  Fixtures under `examples/skeleton/` additionally feed `tests/differential_test.go`: static
  validator and dry-run must AGREE (add intentionally-failing flows to `expectedErrFlows`).
- **`tester.mock_action(extension=...)` with a local variable name** (e.g. `"gh"`) instead of the
  registered `Extension.Name()` (`"github"`) → mock silently never matches; the run fails with
  `no mock for <ext.op> at <file:line:col> (step "name")`.
- **New early return in a `pkg/interpreter` walker** without assigning the named `err` → the
  deferred `step_complete` event reports status=ok for a failure; replay diff tests catch it late.
- **Editing a code fence in `docs/for-extension-developers/temporal-auth.md`** without the
  byte-identical edit in `docs/for-extension-developers/snippets/*.go` (or vice versa) →
  `snippets/drift_test.go` fails (TrimSpace-only normalization).
- **Reordering H2 headings** in `docs/walkthroughs/{dashboard,github-webhook}.md` →
  `tests/walkthrough_*_headings_test.go` pin them byte-exact (including em-dashes/backticks).
- **Renaming locked strings**: drain hook stage names (`pkg/cli/server.go`), receiver
  `error_class` constants (`pkg/extension/receiver/status.go`), panic messages
  (`receiver.Deps.validate`) — black-box tests pin the exact strings.
- **Believing `%+v` is harmless logging** near credentials → redaction firewall fails; and
  `activity.GetLogger(ctx)` PANICS outside a real activity context (see the defer/recover guard
  in `pkg/activity/execute_batch.go` before adding logging there).

## First moves for any task

1. Identify the layer from the package map; read that package's `doc.go` first.
2. Grep `tests/` and the package's `*firewall*_test.go` for gates covering your files BEFORE
   editing.
3. After edits: `go test ./pkg/<changed>/... -count=1`, then `go test ./tests/ -count=1` to catch
   cross-tree gates, then the drift-regen procedures above if parser/docs/dashboard changed.
4. Check `.planning/STATE.md` and `PROJECT.md` before "fixing" anything odd-looking — most
   oddities are locked decisions with a D-xx ID (e.g. the 8-hex-vs-`[:4]` doc comment in
   `pkg/dag/lambda.go` is consistent; do not "fix" one side).
