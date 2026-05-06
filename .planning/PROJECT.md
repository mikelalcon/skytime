# Skytime

## What This Is

Skytime is a Go library that lets teams declare durable workflows in Starlark and execute them on Temporal. The core insight: split workflow authoring into two tiers — library developers write Go *extensions* (typed I/O wrappers, reusable across customers), while consultant/integrator teams compose those extensions in `.star` files specialized per customer. The boundary between Starlark (parse-time, deterministic graph generation) and Temporal (execution-time, durable orchestration) is absolute and architectural — no string compilation, no dynamic activities, no context bleed.

## Core Value

A consultant team can take an extension catalog and a customer brief, write a `.star` file, and have a production-grade durable workflow running on Temporal — without touching Go and without giving up Temporal's retry/timeout/child-workflow guarantees.

## Requirements

### Validated

- ✓ Starlark DSL with naked primitives (`flow`, `step`, `if_cond`, `script`, `for_each_parallel`, `call_flow`) backed by Go AST nodes — Phase 1
- ✓ Extension SDK contract — `Extension.Initialize` returns a `*starlarkstruct.Module` once per parser at Register time; ops return `ActionRef` intents; no path to import `go.temporal.io/sdk/activity` — Phase 1
- ✓ Starlark execution bridge with dot-notation state access (`ctx.req.repo_name`) via recursive `*starlarkstruct.Struct` conversion with deterministic key order — Phase 1
- ✓ Two-environment split — locked 20-key `lambdaTimeGlobals` strict subset + `fail()`, distinct from richer parse-time globals — Phase 1
- ✓ Lambda capture with content-hash IDs (`sha256(fileBytes)[:8] + ":" + line + ":" + col`) and free-variable lint that rejects mutable closures — Phase 1
- ✓ Sealed `Credential` interface with `BearerCredential`, `BasicCredential`, `APIKeyCredential` kinds and redacted `String()`; pluggable `CredentialHandler` contract laid (worker wiring deferred to Phase 3) — Phase 1
- ✓ Position-aware error spine (`ParseError`, `ValidationError`) formatted `<file>:<line>:<col>: <msg>`; parser never panics on malformed input — Phase 1
- ✓ `load()` resolution with relative + absolute paths, `.git`-ancestor root walk, traversal rejection — Phase 1
- ✓ Generic Temporal activity (`ExecuteBatch`) dispatches all extension I/O — Phase 2
- ✓ Block-batched I/O: idempotent batches share one activity invocation; mixed batches rejected at parse time (Policy D) — Phase 2
- ✓ Just-in-time credential resolution inside the activity with per-worker TTL cache + retry-aware bypass — Phase 2
- ✓ Type-level secret protection via `Secret` wrapper (`String`/`GoString`/`MarshalJSON`/`MarshalText`/`Format` redact; explicit `.Reveal()` for unwrap) — Phase 2 (replaced original regex-scrubber design)
- ✓ Architectural firewall: only `pkg/activity` may import `go.temporal.io/sdk/...`; enforced by AST-walking firewall test + inversion-bug meta-test — Phase 2 (allow-list expanded to `[activity, interpreter, worker]` in Phase 3)
- ✓ Generic Temporal interpreter (`SkytimeWorkflow`) that walks any `dag.Flow`; five node walkers (step / if_cond / script / for_each_parallel / call_flow); `if_cond` and `script` produce ZERO Temporal history events; `workflowcheck`-clean — Phase 3
- ✓ Lambda serialization: Option B (re-parse on workflow start with content-hash-keyed registry); only `LambdaID` strings cross history; versioning handled operationally via Temporal Build IDs — Phase 3
- ✓ Cancellation watchdog wires `workflow.Context.Done()` to `thread.Cancel` via native `chan struct{}` bridge with `sync.Once`-wrapped close; lambdas never see `workflow.Context` — Phase 3
- ✓ `for_each_parallel` with bounded fan-out (default 10), errgroup-style cancel-siblings on non-retryable, stable index-order results — Phase 3
- ✓ `call_flow` invokes Temporal child workflow; parent's RetryPolicy and `TypedSearchAttributes` propagate to children — Phase 3
- ✓ Worker bootstrap with three named client constructors (`NewCloudClient` / `NewSelfHostedClient` / `NewDevClient`); filesystem registry boot from `--rootdir` with frozen-after-boot semantics; `Worker.Stop` `sync.Once`-wrapped against double-call panic — Phase 3
- ✓ Build ID-based versioning with build-time-injected default (`-ldflags "-X .../defaultBuildID=$(git rev-parse HEAD)"`); worker-level versioning is **opt-in** via `WorkerOptions.UseBuildIDVersioning` — production deployers register a Build ID compatibility set on the task queue first, dev/CLI runs leave it false; WorkflowInput on the wire is `{FlowName, ContentHash, InitState}` — Phase 3
- ✓ DSL retrofits for runtime: `task_queue` kwarg on `flow()` and `step()` (precedence step > flow > worker default); `max_concurrency` kwarg on `for_each_parallel()` — Phase 3
- ✓ Static validation tier (`pkg/validator` + filled-in `parser/finalize.go`) sharing the parser with the runtime; `[flow > step > action]` error attribution via `dag.ValidationError.Action`; D4-02 `ctx.<name>` AST re-parse visitor (load-bearing finding: `*starlark.Function` does not expose AST, so visitor re-parses cached file bytes via `(*syntax.FileOptions).Parse` and matches lambdas by position); D-11 kwarg cross-validate filled in — Phase 4
- ✓ CI corpus differential test (`tests/differential_test.go`) running every `.star` in `examples/skeleton/` through static `validator.Validate` AND a dry-run interpreter (mock `OperationDispatch` returning `OkResult{}` for every kind); drift fails CI — Phase 4
- ✓ CLI tree under `cmd/skytime/` (thin) + `pkg/cli` (reusable cobra root) with `validate`, `run`, `dev-server` subcommands; functional options (`cli.WithExtensions`, `cli.WithCredentialHandler`); Starlark-first error rendering with `--debug` as the only path to Go internals — Phase 4
- ✓ `skytime run` embedded transient worker with connection-variant routing (`--api-key`→cloud, mTLS triplet→self-hosted, otherwise dev) and per-step slog progress streaming — Phase 4
- ✓ `skytime dev-server` subprocess wrapper around `temporal server start-dev` (NOT embedded Temporalite); SIGINT/SIGTERM forwarded; missing-binary install instructions — Phase 4
- ✓ Cobra/charm-log firewall — `cobra`/`pflag`/`charm.land/log/v2` reachable only from `{cmd/skytime, pkg/cli}`, enforced by AST-walking `tests/firewall_cli_test.go` plus non-vacuous `TestPkgCli_ImportsCobra` — Phase 4
- ✓ Baked-in HTTP extension (`pkg/extension/builtin/http`, Go stdlib `net/http` only) with D4-14 idempotence (get/head=true, post/put/delete=false; deliberately diverges from RFC-7231 PUT/DELETE for v1 simplicity) — Phase 4
- ✓ `examples/skeleton/{simple_check,parallel_fanout}.star` — 2-flow corpus exercising every primitive (sequential step, block batch, `if_cond`, `script`, `for_each_parallel`, `call_flow`) — Phase 4
- ✓ Empty-CredentialID bypass — `pkg/activity` per-action loop short-circuits the resolver call when `dag.ActionRef.CredentialID == ""`; operation receives `nil` credential. Closes the noopCredentialHandler retry-storm audit item from quick 260501-p7c — Phase 4
- ✓ Bazel-style colored CLI output — `skytime run` default output renders interpreter slog events (`flow_start` / `step_dispatch` / `step_complete` / `branch` / `flow_complete`) as a Bazel-style step list with `[skytime]` banner, `[N/M]` counters, kind-aligned labels, ✓/✗ status markers; `--verbose` persistent flag toggles SDK INFO/DEBUG visibility through charm-log — Phase 4
- ✓ HTTP non-2xx auto-fail — `pkg/extension/builtin/http` returns classified errors for non-2xx responses (4xx → NonRetryable via `extension.ErrNonRetryable` sentinel; 5xx → retryable plain error); interpreter `walkStep` extracts `status=N` summary on success (reflection on typed Output, JSON-key fallback for round-tripped `RawOperationOutput`) and surfaces `NonRetryableErrResult` from the activity result slice as a workflow failure; CLI renderer prints `[skytime] flow failed step I/M (reason)` with red marker on `err_count > 0`; example corpus uses `/repos/octocat/Hello-World`; end-to-end happy + unhappy `skytime run` smokes pinned with process-group teardown — Phase 4 (quick 260502-onc)
- ✓ Dynamic action construction via Starlark lambdas — `step(action_fn = lambda ctx: ext.op(...))` returns a single `*ActionRef`; `step(block_fn = lambda ctx: [ext.op(...) for ...])` returns `[]*ActionRef`; mutually exclusive with `action`/`block`; strict return contract via `temporal.NewNonRetryableApplicationError` carrying lambda position; lambda-returned ActionRefs explicitly Frozen before activity dispatch; empty `block_fn` short-circuits with `step_complete summary="empty batch"` — Phase 04.1 (D4.1-06/07/08)
- ✓ Parser-time string interpolation — `${ctx.expr}` in any string kwarg desugars at parse time to `lambda ctx: "..." + str(ctx.expr) + "..."` via synthesized-source path through `(*syntax.FileOptions).Parse` + `starlark.ExecFileOptions` (no hand-built AST); doubled `$$` escape; multi-line and empty `${}` reject; supported in `step(name=)`, `flow(name=)`, `script(id=)`, and any string-typed action factory kwarg; `${ctx.typo}` surfaces as `*dag.ValidationError` at parse time via D4-02 walker (`CapturedLambda.BodyPos` routing) — Phase 04.1 (D4.1-01..05, D4.1-15/16)
- ✓ block_fn idempotency contract — parse-time best-effort classifier (`pkg/parser/block_fn_lint.go::classifyBlockFn`) walks ONLY outermost CallExprs that produce ActionRef elements; homogeneous typed → parse pass, heterogeneous typed → reject (mirrors D2-05 message), opaque → defer to runtime; activity-side runtime fallback re-runs mixed-idempotency check on `[]ActionRef` returned by `block_fn` and rejects with `NonRetryableErr`; D2-07 size cap (50) extends to runtime — Phase 04.1 (D4.1-10..13)
- ✓ Workflow-side `resolveKwargs` — walks `ActionRef.Kwargs *starlark.Dict` in `Items()` insertion order (deterministic by language contract, NOT a Go map needing sort), evaluates `*StarlarkLambda` values via `i.evalLambda`, returns NEW frozen `*starlark.Dict` with resolved `starlark.String` values; static-only kwargs return original dict unchanged (allocation-free fast path); fail() callsite preserved through `*starlark.EvalError.CallStack` walk that skips `<builtin>` frames and surfaces the deepest user `.star` position — Phase 04.1 (D4.1-14, D4.1-08)
- ✓ Multi-line live CLI progress block — cursor-up + line-clear ANSI (`\x1b[1A\x1b[2K`; preserves scrollback, no alt-screen-buffer); inline 10-frame braille spinner `⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏` at 100ms cadence; truncation cap at 10 active rows with `... and N more`; `--verbose` forces static line-per-event mode; non-TTY (piped/CI) → static lines via `golang.org/x/term.IsTerminal`; Windows `//go:build !windows` stub falls back to static; single render goroutine + buffered events channel size 64 (slog Handle ships, goroutine owns all writes); `flow_complete` drains via `Close()` — Phase 04.1 (D4.1-17..21)
- ✓ Example corpus rewrite + dryrun extension — `examples/skeleton/{simple_check,parallel_fanout}.star` demonstrate `${ctx.repo}` interpolation + `step(action_fn=...)` + `step(block_fn=...)` driven by `--input` flag (URL hits change with input); dryrun dispatch handles `*StarlarkLambda` kwargs and lambda-built ActionRefs; differential corpus test continues to pass — Phase 04.1 (D4.1-23..25)
- ✓ PROJECT.md "no string compilation" carve-out — parser-time syntactic sugar that desugars to native Starlark lambdas is permitted; runtime template engines (CEL, Jinja, etc.) remain forbidden; extending the carve-out beyond parser-time desugaring requires a new ADR — Phase 04.1 (D4.1-22)
- ✓ Expression-mode `if_cond` with strict-equality result binding — opt-in `output_alias` makes `if_cond` an expression that binds a typed value back into workflow state when both branches end with `result(value={...})`; per-key strict-equality validation across branches at parse time (no LUB; explicit `float(x)`/`int(x)`/`str(x)` casts handle widening) with `Opaque` defers for unknowable types; new top-level `fail("msg")` node-builtin enables asymmetric branch shapes (one branch terminates, the other binds); runtime walker adds two last-node cases (`*dag.Result` binds to ctx via `bindResultToState`, `*dag.Fail` raises `NonRetryableErr` via `raiseFail`); existing procedural `if_cond` flows compile and run identically (additive, opt-in) — Phase 04.2 (DSL-14, DSL-15, VAL-05)
- ✓ Documentation set + source-driven reference generator — first complete v1 docs: README.md (mid-size repo front door with verbatim-embedded `docs/getting-started.md` tutorial per D-06), `docs/architecture.md` (parse/execute split + ASCII boundary diagram + lambda-capture model + Hard Rules, required reading for both audiences per D-15), `docs/getting-started.md` (5-10 min tutorial against `expression_if.star::check_user`), `docs/reference/cli.md` (4 shipped subcommands hand-written), `docs/reference/builtins.md` (auto-generated, 8 builtins in registration order), `docs/for-flow-authors/{README.md,extensions/http.md}` + `docs/for-extension-developers/README.md` (audience landings, two top-level guides per D-03/D-04), `examples/README.md` (3-fixture index); plus `cmd/skytime-docgen` — stdlib-only Go binary (no cobra/charm-log/lipgloss; library-root firewall preserved) walking `pkg/parser/{globals,builtins}.go` AST + `// skytime:doc` markers (D-11) through `text/template` rendering, wired via `//go:generate` directive in `pkg/parser/generate.go`, drift-gated by `tests/docgen_drift_test.go::TestDocgenDrift`; plain markdown only (D-01, no static-site infra); coverage v1 = 8 builtins + 4 subcommands + bundled HTTP extension (mkdocs/extension-auto-gen/CLI-auto-gen deferred per D-19) — Phase 04.3 (D-01..D-19)
- ✓ Tier-3 E2E test harness (`temporal_test`) — `pkg/testing` ships parse-time `tester.{workflow,mock_action,run}` builtins gated by `parser.WithTestMode()`; mock-lambda environment extends locked 20-key `lambdaTimeGlobals` with `ok(value=)`/`err(msg=)`/`nonretryable(msg=)` return-shape builders (D5-C2/C4); 3-tier MockRegistry match precedence (exact+regex > exact > wildcard, most-recent-wins; cross-extension `*` rejected per D5-B3) with file-scope and per-test stack frames; `MockOperationOutput` JSON-stable wire wrapper (Open Q4); `attempt` 1-indexed counter keyed per-(flow,step,action_idx) (TEST-03); ExecuteBatch-shaped router callback wired via `OnActivity(...).Return(callback)` against `testsuite.TestWorkflowEnvironment`; replay-determinism always-on (D5-D1) — every test runs twice via `interpreter.RunOnceCapturing` + `EventCapture`; first-divergent-event reporter walks back to nearest `step_dispatch` event (extended with `pos`+`name` KV pairs) for D5-D3 flow-callsite attribution alongside test callsite; `assert.*` from `go.starlark.net/starlarktest` injected at parse time + per-subtest `*testing.T` reporter (TEST-05); `pkgtesting.Run(t, dir, opts...)` Go-level foundation API + `pkg/testing.RunCLI(dir)` cobra-side adapter; `skytime test <dir>` cobra subcommand wraps the runner with `--run <regex>` filter (regex against `<file_basename>.<test_name>`) + `--format=json` records mirroring stdlib `cmd/test2json` schema (RFC3339Nano UTC) + default human format (`--- PASS:`/`--- FAIL:` + per-file footer + summary); recursive `*_test.star` discovery via `filepath.WalkDir`; `def test_*()` enumeration via top-level Starlark module walk (zero-arg requirement); single-file scope only — `load()` across files deferred to v2 (`pkg/testing/runner.go`); production worker's `bootRegistry` skips `*_test.star` per latent gap fix (mirrors Go's `*_test.go` build-time exclusion); subprocess E2E pins CLI-03 explicit "no Go stack frames in default output" guarantee; new `examples/skeleton/simple_check_test.star` demo fixture exercises file-scope mock + per-test override + retry semantics; `docs/for-flow-authors/testing-tutorial.md` step-by-step walkthrough + `docs/for-flow-authors/testing.md` reference manual (with v1-limitation callouts: no `load()`, no `expects_failure`, mock-keyed-on-extension-name not local-var); main README links the testing surface from front door — Phase 5 (TEST-01..05, CLI-03)

### Active

- [ ] Example project with HTTP + GitHub + Webhook extensions exercising every primitive (retries, credentials, parallel for-each, child workflow); plus a reusable `pkg/extension/credfile/` library credential resolver (TOML, default `$HOME/.skytime-credentials`); plus `.github/workflows/ci.yml` running the project green-check on every push
- [ ] Compatibility with Temporal Cloud and self-hosted Temporal clusters (BYO cluster, plus dev-server helper for examples) — verified by Phase 3's three named client constructors and Phase 4's `skytime run` variant routing; example project (Phase 6) demonstrates end-to-end

### Out of Scope

- **Hot-reload of `.star` files** — design must not preclude it, but no implementation in v1
- **Plugin / gRPC / out-of-process extensions** — only static or dynamic-local Go extensions in v1
- **Web UI / dashboard** — Temporal's UI is sufficient for v1 visibility
- **Multi-tenant hosted SaaS** — Skytime is a library; productizing it as a service is a separate decision
- **CEL or string-based expressions** — explicitly rejected; lambdas only (parser-time syntactic sugar that desugars to lambdas — e.g., `${ctx.expr}` → `lambda ctx: str(ctx.expr)` — is permitted per D4.1-22 carve-out; runtime template engines remain forbidden)
- **Starlark unit-test tier** (Tier 2 in spec) — deferred; Static (Tier 1) and Starlark E2E (Tier 3) ship in v1, pure-Starlark unit testing of `def` blocks moves to v2
- **Workflow versioning helpers** — Temporal patching primitives are available to advanced users, but no Skytime-specific versioning API in v1

## Context

- **Tech stack (decided):** Go, `go.starlark.net` for the DSL, `go.temporal.io/sdk` for orchestration, `go.temporal.io/sdk/testsuite` for E2E testing.
- **Two-tier authoring model:** Library/extension developers (Go) and workflow authors ("consultants" who specialize per customer in Starlark). The DSL must be powerful enough that consultants don't drop down to Go for normal customer specialization, and safe enough that Starlark code can't break the host.
- **Architectural separation is non-negotiable:** Parse phase generates a deterministic DAG with no I/O; execution phase walks the DAG inside Temporal. Lambdas captured at parse time are evaluated inside the workflow with state injected as nested structs. This split is the project's whole reason to exist.
- **Strict directives from the spec:**
  - No string compilation (no CEL, no string parsers for conditionals/data mapping) — only native Starlark lambdas.

    > *Parser-time syntactic sugar that desugars to native Starlark lambdas (e.g., `${ctx.expr}` → `lambda ctx: str(ctx.expr)`) is not string compilation. The runtime evaluation surface remains lambda-only. This carve-out exists for ergonomic step naming and string kwargs; runtime template engines (CEL, Jinja, etc.) remain forbidden. Extending this carve-out beyond parser-time desugaring requires a new ADR.* (Phase 04.1, D4.1-22)

    > *The parse-time top-level `fail("msg")` builtin (D4.2-05) is a parse-time syntactic primitive that emits a workflow-failure node — it does NOT introduce a runtime evaluation surface beyond the existing lambda contract. The MessageFn lambda (when `${ctx.expr}` interpolation is present) evaluates per the standard `CapturedLambda` + `bridge.CallLambda` path established in Phase 1; the same desugarer used by D4.1-22 is reused verbatim. See `pkg/parser/doc.go` for dual `fail()` semantics (parse-time node-emit vs. lambda-time `fail` global).* (Phase 04.2, D4.2-05)
  - No dynamic activities — extensions are plain Go functions; they never import `go.temporal.io/sdk/activity`.
  - No context bleed — never pass `workflow.Context` into a Starlark thread, never pass a Starlark `*starlark.Thread` over the network into an activity.
- **Distribution shape:** Go library. The example project (with extensions and CLI) is the dogfooding vehicle and the proof-of-life demo, not a separate product.
- **Greenfield:** No existing code, no migration burden. The repository starts empty.

## Constraints

- **Tech stack**: Go + Starlark + Temporal — fixed. No alternative DSLs, expression languages, or orchestrators in scope.
- **Architecture**: Strict parse/execute separation. — Required for the safety properties (no I/O at parse, no Go escape hatch at execute, no context bleed).
- **Quality**: Quality > speed. — This is foundational infrastructure; correct boundaries are hard to fix retroactively.
- **Determinism**: The parsed DAG must be deterministic. — Temporal replay requires that workflow code (and the lambdas embedded in the DAG) produce the same decisions on replay.
- **Security**: Credentials never enter workflow state. — Resolver is invoked just-in-time inside the activity; state holds only credential IDs.
- **Compatibility**: Temporal Cloud and self-hosted servers must both work. — No reliance on cloud-only or self-hosted-only features.

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Starlark over CEL or custom DSL | Lambdas + struct injection give expressive data access without a string-parsing risk surface | — Pending |
| Single generic Temporal activity | Avoids per-extension activity registration and lets us batch I/O blocks to control history size | — Pending |
| Extensions return `ActionRef` intents (Command Pattern) | Keeps the parse phase pure and lets the interpreter route, batch, and mock I/O from one place | — Pending |
| Tier 2 (Starlark unit tests) deferred to v2 | Static + E2E cover the common cases; Tier 2 mainly helps offline `def`-block testing, lower priority for v1 | — Pending |
| Library + example project + CLI (no standalone binary) | Library is the primary product; CLI/example exist to drive development and demos | — Pending |
| Static or dynamic-local Go extensions only in v1 | Plugins/gRPC add complexity that isn't justified before we have one real customer | — Pending |
| Hot-reload deferred but not precluded | Useful eventually; designing the parser as a pure function of file contents leaves the door open | ✓ Good — `Parser.Parse` is a pure function of file contents (Phase 1) |
| `Extension.Initialize` returns a `*starlarkstruct.Module` once per parser at Register time | The user authoring example `gh = github.endpoint("admin")` requires `github` to be a namespace with attributes, not a callable. Resolved via the module-attribute pattern | ✓ Good — verified end-to-end in Phase 1 (Plan 01-05 fixture 07) |
| Lambda IDs use `sha256(fileBytes)` prefix, not canonicalized AST | Cosmetic edits (whitespace, comments) intentionally invalidate IDs; simpler than canonicalization and acceptable for the v1 use case | ✓ Good — Phase 3 picked Option B (re-parse on workflow start) which composes cleanly with content-hash IDs; Build IDs handle versioning operationally |
| Lambda serialization via re-parse on workflow start (Option B) + Build IDs | No custom DataConverter needed; only LambdaID strings cross history; "fix a .star bug" handled by Temporal's Worker Versioning mechanism (drain old workers gradually) | ✓ Good — wired in Phase 3 with content-hash-keyed FlowRegistry frozen at boot; verified by replay-twice test |
| Cancellation watchdog bridges `workflow.Channel` → native `chan struct{}` via `workflow.Go` reader with `sync.Once`-wrapped close | Trickiest piece in v1: lambdas eval synchronously on the main workflow goroutine and need an interrupt mechanism without seeing `workflow.Context` | ✓ Good — integration tests (Wave 3) showed no flakiness; documented fallback to "pre-eval `ctx.Err()` only" not needed |
| Mixed-idempotency block rejection at parse time (Policy D) keeps interpreter dumb | Phase 3 interpreter never sees mixed batches; no splitting logic required | ✓ Good — composes with Phase 2's defensive activity-side rejection |
| Three named client constructors over one ConnectionOptions struct | More discoverable in IDE autocomplete; v1.39 TLS-with-API-key change handled in `NewCloudClient` only | ✓ Good — verified by `TestClientConstructors` |
| `Worker.Stop` `sync.Once`-wrapped against double-call panic | Pitfall #5: Temporal SDK's worker.Stop panics on second call; defensive wrap at the Skytime boundary | ✓ Good — `TestWorker_StopIsIdempotent` verifies |
| Idempotent declaration is a `*bool` field with nil-check at registration (D-12) | Forces extension authors to make a conscious choice; nil = registration error | ✓ Good — verified by `errors.Is(err, ErrIdempotentRequired)` test in Phase 1 |
| Mixed-idempotency blocks rejected at parse time (Policy D, Phase 2) | "Make wrong things impossible" — keeps activity dumb and homogeneous; consultant gets a friendly fix-suggestion error at parse time instead of a runtime surprise | ✓ Good — verified by `TestLinter_MixedIdempotency_*` and defensive activity-side test |
| Type-level secret protection via `Secret` wrapper (no regex scrubber, ACT-05 amended) | Java-style `toString` redaction via Go `Stringer` + Format/MarshalJSON; `.Reveal()` is greppable for audit. Regex deferred — additive in v1.x if a customer incident proves it needed | — Pending — first real customer with a leaky third-party SDK will tell us if regex is needed |
| Per-worker credential cache with retry-aware bypass | 5-min TTL for happy path; `activity.GetInfo(ctx).Attempt > 1` invalidates batch's IDs to handle token rotation cleanly | ✓ Good — `TestExecuteBatch_RetryAttempt_BypassesCache` verifies |
| `pkg/validator` is a thin facade; new lints live in `parser/finalize.go` | Validator owns the CLI surface and the dry-run differential seam; parser owns the actual checks. Single parse path → static and runtime can never disagree. | ✓ Good — `TestDifferentialCorpus` runs static + dry-run agreement on every `examples/skeleton/*.star` (Phase 4) |
| `ctx.<name>` lambda visitor re-parses cached file bytes (does NOT use `*starlark.Function` AST) | `*starlark.Function.funcode` is unexported `*compile.Funcode`; the syntax tree is discarded after compilation. Re-parse via `(*syntax.FileOptions).Parse` + `syntax.Walk` is the only path. | ✓ Good — verified by `TestCtxWalk_*` (Phase 4); pitfall fixture for two-lambdas-same-line passes |
| `skytime dev-server` shells out to `temporal server start-dev` (not embedded Temporalite) | Avoids pulling sqlite + heavy temporal-server transitive deps into `cmd/skytime`; familiar to Temporal users; documented prerequisite in Phase 6 README. Supersedes Phase 3 CONTEXT note about not spawning. | ✓ Good — `TestDevServerCmd_*` verify subprocess wrapper, SIGINT forwarding, and missing-binary install instructions (Phase 4) |
| `pkg/cli` is reusable; `cmd/skytime` is a thin wrapper | Phase 6's example project (and any consultant team's custom CLI) imports `pkg/cli.NewRootCommand` and registers their own extensions via functional options. Single library-side package allowed cobra/charmlog imports — firewall enforces. | ✓ Good — `TestNoCobraImportsOutsideAllowList` + `TestPkgCli_ImportsCobra` (non-vacuous) (Phase 4) |
| HTTP extension PUT/DELETE locked NON-idempotent (D4-14 diverges from RFC-7231) | Even though the HTTP standard considers PUT/DELETE idempotent, the baked-in extension treats them as side-effecting for v1. Consultants writing real GitHub/Slack flows in Phase 6 declare per-op idempotence themselves. | ✓ Good — `TestExtension_OperationsIdempotenceMatchesD4_14` pins this verbatim (Phase 4) |

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd:transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd:complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---
*Last updated: 2026-05-06 after Phase 5 completion (Tier-3 E2E test harness `temporal_test` ships: pkg/testing with `tester.{workflow,mock_action,run}` parse-time builtins, mock-lambda env, 3-tier MockRegistry, replay-determinism always-on, assert.* wired to *testing.T, `skytime test <dir>` cobra subcommand with `--run` regex + `--format=json` mirroring cmd/test2json schema, recursive `*_test.star` discovery, worker bootRegistry skips test files, demo fixture in examples/skeleton/, tutorial + reference docs cross-linked from main README; HUMAN-UAT items 1+2 resolved at close — visual UX validated, exit-code docs corrected to match v1's blanket-exit-1 reality)*
