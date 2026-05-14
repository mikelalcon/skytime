# Phase 7: Trigger primitive + server shell - Context

**Gathered:** 2026-05-08
**Status:** Ready for planning

<domain>
## Phase Boundary

Phase 7 lays the foundation for triggers and durable worker mode in one focused phase. Three deliverables, no HTTP receiver yet:

1. **Starlark `trigger(...)` primitive** — top-level parser builtin (NOT a kwarg on `flow()`), separate from `flow()`. Captures references and lambdas at parse time, performs no I/O. Validated at parse-time with position-aware errors (`<file>:<line>:<col>: <msg>`). Free-variable lint extended to lambda arity + req-attribute walk.
2. **`dag.Trigger` node + `TriggerSource` extension type** — new sealed DAG node with stable JSON marshaling (`{kind, FlowName, Source, MapLambda, IdempotencyLambda, CredentialID}`). New sealed `extension.TriggerSource` marker interface so any extension can return one of these from a module attribute.
3. **`skytime server` subcommand shell** — long-running process: boots `bootRegistry` against `--rootdir`, prints registered flows AND triggers in name-sorted order, starts a Temporal worker on `--task-queue`, drains in-flight workflows on SIGINT/SIGTERM up to `--drain-timeout` (default 30s), exits cleanly. Plus the `dev-server` → `dev-temporal` rename across code + docs + CI smoke scripts (pre-1.0, hard rename, no alias).

Out of scope this phase: HTTP listener, source factories (`github.webhook`, generic webhook, cron), dashboard, idempotency mapping at ingress, `Schedule` reconciliation. All ship in 7.1+.

</domain>

<key_constraints>
## Load-Bearing Constraints (do not violate)

- **CREDENTIALS NEVER SERIALIZED.** A resolved `extension.Secret` value MUST NEVER appear in `dag.Trigger.Source` JSON, in the `TriggerRegistry`, in Temporal history, in any log line at any verbosity, or in any error rendering. Triggers store the credential **ID string** (same contract as `dag.ActionRef.CredentialID`). Resolution happens just-in-time inside the receiver in 7.1 — and even there, the resolved `Secret` is wrapped via the existing `extension.Secret` redacting type. This rule overrides every convenience consideration.
- **Sealed interfaces only.** `dag.Trigger` satisfies the existing `dag.Node` seal (`nodeMarker()`). `extension.TriggerSource` introduces a new sealed marker (`triggerSourceMarker()`) — only types within `pkg/extension` (or sub-packages it owns) can satisfy it. Mirrors `dag.Node`'s seal pattern verbatim.
- **No I/O at parse time.** Trigger parsing follows the same purity contract as `flow()`: attribute lookup, lambda capture, position recording. No network, no filesystem reads beyond `load()` resolution, no extension calls beyond the one-time `Initialize`.
- **Determinism in user-visible ordering.** `registered flows: [...]` and `registered triggers: [...]` startup logs are name-sorted (lexicographic). Boot scans `--rootdir` in sorted-path order (matches `pkg/worker/boot.go` precedent). Trigger registry iterates deterministically.

</key_constraints>

<decisions>
## Implementation Decisions

### Trigger lambda contract

- **D-07-01: Predeclared globals for `map` and `idempotency_key` lambdas.** Locked 20-key `lambdaTimeGlobals` (Phase 1 D-20) **plus** `json.*` and `time.now()`. Trigger lambdas run ONCE at HTTP ingress (not in workflow replay) so non-deterministic globals are safe; reusing the locked-set baseline keeps the contract auditable. Define a new `triggerTimeGlobals` constant in `pkg/bridge` that extends `lambdaTimeGlobals` with these two additions. Document in `pkg/bridge/doc.go` why the surface differs.
- **D-07-02: Payload injection.** Both lambdas receive a single positional `req` parameter modeled as `*starlarkstruct.Struct` (recursive Go-map → Struct conversion already shipped in `pkg/bridge`). Source-specific payload fields hang off `req` — for HTTP-shaped sources `req.payload` (parsed body) and `req.headers` (request headers); for cron (later) `req.scheduled_time`, `req.workflow_attempt`. Reuses the `ctx`-style dot-notation consultants already learned (Phase 1 D-08).
- **D-07-03: Both lambdas symmetric — `lambda req:`.** `map(req)` and `idempotency_key(req)` use the same single-positional shape. Each `TriggerSource` declares which `req.*` fields it provides (per source kind) so the parser's req-attribute walk (D-07-05) can validate references. **Deviation from ROADMAP success-criterion-1 illustrative example** (which shows `lambda payload, headers`) — read the success criterion as illustrative; lock the actual signature here. Update REQUIREMENTS.md TRIG-01 wording during planning if needed.
- **D-07-04: No determinism requirement.** Map and idempotency_key run at HTTP ingress (Phase 7.1+), before `client.ExecuteWorkflow`. Their result is the workflow input — frozen at that point — so non-determinism (e.g., `time.now()`) is observably safe. Document explicitly in `pkg/parser/doc.go` so future readers don't conflate trigger lambdas with the workflow lambda contract.
- **D-07-05: Parse-time lambda validation.** Three-layer check, position-aware errors at every layer:
  1. Free-variable lint (Phase 1 contract — reject mutable closures).
  2. Arity check (lambda must accept exactly 1 positional, named anything but the convention is `req`).
  3. **`req.<field>` attribute walk via D4-02 visitor pattern** (Phase 4 `pkg/parser/ctx_walk.go`). Reuses the cached-file-bytes re-parse approach (`*starlark.Function` does not expose AST; re-parse via `(*syntax.FileOptions).Parse` and match by position). Each `TriggerSource` declares a `ReqSchema()` method (or constant) listing valid `req.*` fields per source kind; visitor errors with valid-field list on typos.

### TriggerSource Go-side semantics

- **D-07-06: Sealed marker interface.** `type TriggerSource interface { triggerSourceMarker() }` in `pkg/extension/trigger.go`. Concrete types (e.g., a stub `fakeTriggerSource` in parser tests; `github.WebhookSource` in 7.1) live wherever their owning extension does. The parser stores `TriggerSource` as opaque payload on `dag.Trigger.Source`. Runtime (7.1+) type-switches in the HTTP router. Mirrors `dag.Node`'s seal pattern.
- **D-07-07: Pkg layout.** `pkg/extension/trigger.go` (alongside `pkg/extension/extension.go`, `credential.go`, `secret.go`). Same package as `Extension` and `OperationSpec` — source factories sit alongside operation factories as parallel SDK shapes.
- **D-07-08: Source factories live under their owning extension's namespace.** GitHub triggers MUST live in the `github.*` namespace (e.g., `github.webhook(events=[...], secret_credential="...")`) — NOT under a separate `triggers.*` extension. Each extension that ships triggers exposes them as module attributes returning a `TriggerSource`. This **deviates from the v1.43 draft plan / REQUIREMENTS.md TRIG-07/08 wording** (which says `triggers.github_webhook(...)` shipping in `pkg/extension/builtin/triggers/`). The draft is overridden — extensions own their trigger sources end-to-end. REQUIREMENTS.md TRIG-07/08 wording will need to be updated during planning. Phase 7 ships only the SDK contract; Phase 7.1 ships the first real factory under `github.*`. Generic HTTP webhook will live under a `webhook` extension (already exists in `examples/http-github-webhook/extensions/webhook/`); cron's owning extension is TBD in 7.2.
- **D-07-09: `dag.Trigger.Source` JSON shape.** Two-field discriminated form: `{ "kind": "github.webhook", "config": { ... source-specific fields ... } }`. Each concrete `TriggerSource` type implements its own `MarshalJSON` producing this shape. Round-trip via a kind-keyed registry of unmarshal funcs (small map in `pkg/dag` or `pkg/extension`; populated by source factories during their extension's registration). **CRITICAL: `config` MUST contain only the credential ID string (or empty); the resolved `extension.Secret` value is NEVER reachable from this JSON path.** Mirrors `dag.ActionRef`'s position-exclusion contract (`pkg/dag/marshal.go` already deliberately strips `Pos` for cross-machine stability — extend the convention to strip secrets).
- **D-07-10: Source.config redaction in error messages and logs.** Any error rendering or log line that surfaces a `dag.Trigger` (parse errors, validation errors, startup banner) MUST render Source via the same JSON path so the credential-ID-only contract holds. No bypass paths via `fmt.Sprintf("%+v", trigger)` or `Stringer` overrides that leak. Add a firewall test that walks `pkg/dag/trigger.go` AST and asserts no `%+v` / `%#v` formatting verbs against `Trigger` or its fields. ([Claude's discretion: implementation detail of the firewall test left to planning.])

### Trigger registry shape & boot integration

- **D-07-11: Sibling `TriggerRegistry`.** New `interpreter.NewTriggerRegistry()` parallel to `FlowRegistry`. Same content-hash discipline (per-file hash so Phase 7.1's HTTP routing reload story matches flow versioning). Same frozen-after-boot semantics. `bootRegistry` returns `(*FlowRegistry, *TriggerRegistry, error)`. **Worker API change:** `NewWorker` accepts both registries (or a composite struct); update call sites in `pkg/cli/run.go`, `examples/http-github-webhook/cmd/extbin/`, and any test fixtures.
- **D-07-12: Cross-file `trigger.FlowName` validation at parse-finalize.** Parser collects all flows + triggers across the boot's `.star` files first, then in finalize: every `trigger.FlowName` MUST resolve to a known flow. Allows `trigger(flow="check_user", ...)` to live in a different file from `flow(name="check_user", ...)`. Position-aware error if unknown ("trigger references unknown flow 'X'; known flows: [list]"). Implementation-wise, finalize already runs (`pkg/parser/finalize.go`) — extend it.
- **D-07-13: Allow duplicate (flow, source-kind) pairs.** Two triggers can fire the same flow from the same source kind with different configs (e.g., one `github.webhook(events=["push"])` and another `github.webhook(events=["issues"])` both targeting `check_user`). The HTTP router de-dups handler mounts in 7.1; the registry stores both. Parse-finalize does NOT reject this. Detect literal duplicates (byte-identical config) only as a friendly warning during boot, not an error.
- **D-07-14: Phase 7 parser tests use a test-only stub `TriggerSource`.** No production-shipped throwaway factory. `pkg/parser/trigger_test.go` (or similar) defines `type fakeTriggerSource struct { kind string; reqFields []string }` with `triggerSourceMarker()` and a minimal `ReqSchema()`. Phase 7.1 ships `github.WebhookSource`, `webhook.PostEndpointSource`, etc. The success criterion 1 unit-test reads the stub.

### Server lifecycle on SIGTERM/SIGINT

- **D-07-15: SIGINT and SIGTERM identical → drain.** Both initiate graceful drain up to `--drain-timeout`. Second signal of either kind (during drain) forces immediate `worker.Stop()` and `os.Exit(1)` with a "drain interrupted" message. Single signal-handling goroutine using `signal.NotifyContext` (consistent with `cmd/skytime/main.go` pattern).
- **D-07-16: Behavior on `--drain-timeout` expiry.** Call `sdkworker.Worker.Stop()` (force-cancels in-flight activity polls; Temporal preserves workflow state on the server, so workflows resume on the next worker start — this IS the durability story). Log a charm-log-formatted timeout message: `"[skytime] drain-timeout exceeded; N workflows forced; restart resumes from event history"`. Exit code 1 to make orchestrator-driven shutdowns observable in CI/k8s.
- **D-07-17: `--drain-timeout` flag.** Default 30s (Kubernetes `terminationGracePeriodSeconds` default). Type: `time.Duration` via cobra `pflag.Duration`. Range-check at flag-parse time: minimum 1s, maximum 1h (anything beyond an hour suggests misuse — log a warning if exceeded but accept).
- **D-07-18: `--addr` flag in Phase 7.** Accepted on the `skytime server` command line (so users wiring start scripts now have a stable surface), but UNUSED in Phase 7. At startup, log: `"[skytime] note: --addr=X has no effect until Phase 7.1 ships the HTTP receiver"`. Phase 7.1 just removes the warning. Default value: `":8080"` so users don't have to set it preemptively.
- **D-07-19: `--task-queue`, `--temporal`, `--credfile` flags.** Reuse `connectClient(cfg)` from `pkg/cli/run.go` (D4-08 variant routing: `--api-key` → cloud; mTLS triplet → self-hosted; otherwise dev). Required: `--rootdir`, `--task-queue`, `--temporal`. Optional: `--addr`, `--credfile`. The `--credfile` flag is accepted but its lift-into-`pkg/cli` (`cli.WithCredfile` Option) is owned by Phase 7.4 (CLI-08). Phase 7's `skytime server` reads `--credfile` and threads it to `cfg.credHandler` ONLY if a credential handler is wired via `cli.WithCredentialHandler` — otherwise the flag is rejected with a clear error pointing at the binary configuration. Custom binaries (extbin) supply their own handler via Option (current pattern); the flag is a no-op-when-no-handler.
- **D-07-20: Startup log format — both formats, default charm-log + JSON via `--json-log`.** Default startup output uses the existing charm-log Bazel-style renderer (`[skytime]` banner, sorted name lists). Lines like:
  ```
  [skytime] starting server (rootdir=examples/http-github-webhook/, task-queue=demo, temporal=localhost:7233)
  [skytime] registered 3 flows: [batch_label_issues, check_user, weekly_digest]
  [skytime] registered 2 triggers: [github.webhook → check_user, webhook.post → batch_label_issues]
  [skytime] worker started; ctrl-c or SIGTERM to drain
  ```
  A new `--json-log` boolean flag on `skytime server` swaps the slog handler to JSON (existing `slog.NewJSONHandler` against the same writers). When `--json-log` is set, every line above becomes a structured log record (`event=server_starting`, `event=registered_flows flows=[...]`, etc.). Implementation reuses the existing `cfg.sdkLogger` routing pattern from `pkg/cli/run.go`. Names are sorted lexicographically. Trigger lines show `source-kind → flow-name` arrow form for at-a-glance scanning.

### `dev-server` → `dev-temporal` rename

- **D-07-21: Hard rename, single commit.** Pre-1.0 (per v1.43 draft plan): no deprecation alias. Touch (non-exhaustive — full list comes from planner's grep):
  - `pkg/cli/dev_server.go` → `pkg/cli/dev_temporal.go` (file + symbol rename: `newDevServerCommand` → `newDevTemporalCommand`)
  - `pkg/cli/dev_server_test.go` → `pkg/cli/dev_temporal_test.go` (rename + update fixtures + update test names)
  - `pkg/cli/root.go` (`AddCommand(newDevServerCommand(cfg))` → `AddCommand(newDevTemporalCommand(cfg))`)
  - All `dev-server` literal-string references in: README.md, docs/getting-started.md, docs/reference/cli.md, docs/for-flow-authors/*.md, examples/*/README.md, .github/workflows/scripts/walkthrough_smoke.sh, examples/http-github-webhook/cmd/extbin/main.go (subcommand-list comment), `cmd/skytime/main.go` (top comment)
  - Doc-gen drift test fixtures (`tests/docgen_drift_test.go`) — the rename will surface here; regenerate with `go generate ./...`
  - Smoke script byte-for-byte parity per Phase 6's commitment — update reference output too
- **D-07-22: Rename verification.** Add a CI check (or a one-off grep test) that fails if any tracked file contains the literal string `dev-server` after the rename (excluding git history, `.planning/` archive paths, and `/CHANGELOG.md` if present). Single source of truth.

### Claude's Discretion

The planner has flexibility on these (defaults captured during discussion; deviate only if a stronger reason emerges during planning):
- **Internal naming:** specific function names (`newDevTemporalCommand`, `bootRegistryWithTriggers`, `validateTriggerLambda`, etc.) — pick consistent with existing precedent.
- **Connection-flag retry behavior:** `connectClient` reuse from `skytime run` is locked, but whether `skytime server` adds long-connection retry logic on initial connect is open — recommend YES (long-running process should retry initial connect with bounded exponential backoff for, say, 30s before giving up) but the planner can defer to 7.1 if scope pressure.
- **Test layout for `skytime server`:** use the existing `dev_server_test.go` test-seam pattern (override `lookPath`, `testRunningCmd atomic.Pointer`) where applicable for signal-forwarding tests against the worker's drain. Test seams may need new names (`testWorkerDrain` etc.).
- **Boot ordering when extension registration fails:** preserve existing `pkg/parser/globals.go` behavior (extension `Initialize` errors abort boot with a clear message). New consideration: a TriggerSource factory whose `ReqSchema()` returns an empty list — accept (means no req fields available) or reject (likely a bug)? Recommend accept-with-warning.
- **Firewall test for credential redaction:** the planner picks a feasible AST-walking implementation that asserts no `%+v` / `%#v` against `*dag.Trigger` or any `TriggerSource` concrete type. Defer the exact mechanism (go/ast walker pattern from `tests/firewall_*_test.go`) to plan tasks.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope and design lock
- `.planning/ROADMAP.md` § Phase 7 — goal, success criteria (5), requirements (TRIG-01..05, SERVER-01..03, CLI-13)
- `.planning/REQUIREMENTS.md` § "Trigger Primitive + Sources" / § "Server Subcommand" / § "CLI Surface" — full requirement texts; NOTE D-07-08 deviation from TRIG-07/08 wording
- `.planning/v1.43-DRAFT-PLAN.md` § "Phase 7" + § "Locked Design Decisions" + § "Open Items for /gsd:discuss-phase 7" — the items in "Open Items" map 1:1 to D-07-01..D-07-20 above
- `.planning/PROJECT.md` § Constraints + § "Strict directives from the spec" — credentials never serialized, no I/O at parse, no context bleed, deterministic ordering

### DAG node + JSON marshaling patterns
- `pkg/dag/node.go` — sealed `Node` interface (`nodeMarker()`); `dag.Trigger` mirrors this
- `pkg/dag/marshal.go` — JSON marshaling pattern; `Pos` deliberately stripped for cross-machine stability — extend convention to credentials
- `pkg/dag/action.go` — `dag.ActionRef` precedent for `CredentialID` field, opaque `Kwargs *starlark.Dict` payload, `Freeze()` recursion
- `pkg/dag/flow.go` — `dag.Flow` shape; Trigger's `FlowName` resolves against the flow registry

### Parser builtin + lambda + validation
- `pkg/parser/builtins.go` — all existing builtin handlers (`builtinFlow`, `builtinStep`, etc.); `builtinTrigger` follows the same factory pattern
- `pkg/parser/globals.go` — parse-time predeclared globals registration (`newParseTimeGlobals`); add `"trigger"` entry
- `pkg/parser/lambda_capture.go` — Phase 1 free-var lint (`CapturedLambda`, content-hash IDs); reused for trigger lambdas
- `pkg/parser/ctx_walk.go` — D4-02 ctx.<name> AST visitor pattern; **reuse for `req.<field>` attribute walk** (D-07-05). The cached-file-bytes re-parse via `(*syntax.FileOptions).Parse` is load-bearing — `*starlark.Function` does NOT expose its AST.
- `pkg/parser/finalize.go` — parse-finalize validation pass; extend with cross-file trigger.FlowName check (D-07-12)
- `pkg/parser/interpolation.go` — `${ctx.expr}` interpolation precedent (Phase 04.1); not directly reused but pattern guides any future interpolation in trigger config kwargs
- `pkg/bridge/lambda_globals.go` (likely path; verify in planning) — `lambdaTimeGlobals` 20-key locked set; new `triggerTimeGlobals` extends it (D-07-01)

### Extension SDK contract
- `pkg/extension/extension.go` — `Extension` interface; `Initialize` lifecycle ("ONCE per parser at Register time")
- `pkg/extension/credential.go` + `pkg/extension/secret.go` — credential resolution + `Secret` wrapping; trigger source `secret_credential` kwarg uses the same path in 7.1+
- `pkg/extension/operation.go` — `OperationSpec` precedent; `TriggerSource` is parallel to operation factories

### Worker boot + registry
- `pkg/worker/boot.go` — `bootRegistry` walks rootdir, sorts paths, computes content_hash, parses each file; **extend signature to `(*FlowRegistry, *TriggerRegistry, error)`** (D-07-11). Test-mode skip of `*_test.star` carries over.
- `pkg/worker/worker.go` — `NewWorker` shape; takes `WorkerOptions{RootDir, Extensions, CredentialHandler, ...}` — extend WorkerOptions or add a TriggerRegistry parameter
- `pkg/worker/options.go` — WorkerOptions precedent
- `pkg/interpreter/registry.go` — `FlowRegistry` shape (mu/frozen/byFlow); **mirror for `TriggerRegistry`**

### CLI subcommand wiring
- `pkg/cli/root.go` — `NewRootCommand` registers subcommands; add `newServerCommand(cfg)` and rename `newDevServerCommand` → `newDevTemporalCommand`
- `pkg/cli/dev_server.go` — file to RENAME to `dev_temporal.go`; symbol rename `newDevServerCommand` → `newDevTemporalCommand`
- `pkg/cli/dev_server_test.go` — file to RENAME (test seams `lookPath`, `testRunningCmd atomic.Pointer` carry over verbatim)
- `pkg/cli/run.go` — `connectClient(cfg)` variant routing reused by `skytime server`
- `pkg/cli/connect.go` — `connectClient` implementation; D4-08 routing
- `pkg/cli/options.go` — functional `Option` pattern (`WithExtensions`, `WithCredentialHandler`); `cli.WithCredfile` lift is Phase 7.4, NOT this phase
- `pkg/cli/render.go` (likely; verify path) — charm-log Bazel-style renderer; reused for startup banner (D-07-20)
- `pkg/cli/flags.go` — flag-binding helpers; `--addr`, `--task-queue`, `--temporal`, `--credfile`, `--drain-timeout`, `--json-log` registered here
- `pkg/cli/firewall_cli_test.go` (verify path; also `tests/firewall_cli_test.go`) — cobra/charm-log import firewall; `pkg/cli` is the only allowed package — Phase 7 stays within `pkg/cli`

### Binary entrypoints
- `cmd/skytime/main.go` — root binary; minimal; new `server` subcommand inherited automatically via `NewRootCommand`
- `examples/http-github-webhook/cmd/extbin/main.go` — custom-binary precedent; `lazyCredfileHandler` lift is Phase 7.4 (NOT this phase). Phase 7 leaves extbin's main.go shape unchanged except for the `dev-server` → `dev-temporal` mention in its top comment.

### Documentation rename targets
- `README.md` — rename `dev-server` mentions
- `docs/getting-started.md` — tutorial walkthrough; subprocess invocation
- `docs/reference/cli.md` — hand-written subcommand reference
- `docs/for-flow-authors/README.md` (and any `extensions/*.md` referencing `dev-server`)
- `examples/http-github-webhook/README.md` — major update; the `gh webhook forward` walkthrough comes in 7.1 but the rename ships now
- `.github/workflows/scripts/walkthrough_smoke.sh` — CI smoke; byte-for-byte command parity per Phase 6 commitment
- `tests/docgen_drift_test.go` — drift gate; will catch any missed rename in auto-generated docs

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets

- **`pkg/parser/builtins.go::builtinFlow / builtinStep`** — every Phase 7 trigger builtin (`builtinTrigger`) follows this exact factory pattern: `func (p *Parser) builtinTrigger(thread, fn, args, kwargs)` with `wrapBuiltinError` for position-aware error wrapping.
- **`pkg/parser/lambda_capture.go::CapturedLambda`** — already captures lambdas with content-hash IDs and free-var lint. Trigger map/idempotency_key lambdas use the same machinery; the only addition is the arity check (D-07-05) and `req.<field>` attribute walk extension.
- **`pkg/parser/ctx_walk.go`** — the D4-02 `ctx.<name>` visitor (Phase 4) re-parses cached file bytes via `(*syntax.FileOptions).Parse` and matches lambdas by position. **Direct reuse target** for `req.<field>` validation (D-07-05). Generalize the visitor to accept a free-var name + valid-attribute-list constructor; current ctx-walker becomes one caller, trigger-req-walker becomes another.
- **`pkg/dag/marshal.go`** — JSON marshaling that strips `Pos` for cross-machine stability. Extend the strip-list convention to credentials (no need for new code, just discipline + the firewall test in D-07-10).
- **`pkg/interpreter/registry.go::FlowRegistry`** — full `mu sync.RWMutex` + `frozen` + `byFlow map[string]map[string]*ParsedFlow` shape. **`TriggerRegistry` mirrors it verbatim** with `byTrigger map[string][]*dag.Trigger` (or similar — exact key choice is planning-time).
- **`pkg/worker/boot.go::bootRegistry`** — file-walk + sort + content-hash + per-file parse loop. Extend signature to return `(*FlowRegistry, *TriggerRegistry, error)`; the inner parse loop's parser session already collects everything from every file.
- **`pkg/cli/dev_server.go::newDevServerCommand`** — entire signal-forwarding + subprocess pattern. NOT directly reused by `skytime server` (server runs the worker in-process, not as a subprocess), but the `signal.NotifyContext` + `sigCh` + atomic `testRunningCmd` test-seam idiom is the precedent for `skytime server`'s drain mechanism.
- **`pkg/cli/run.go::connectClient`** — D4-08 variant routing (--api-key → cloud; mTLS triplet → self-hosted; otherwise dev). `skytime server` reuses verbatim.
- **`pkg/cli/render.go::buildRoutedSlogLoggerWithHandle`** (verify path) — charm-log Bazel-style routing pattern for `skytime run`. Same logger reused for the server startup banner.
- **`tests/firewall_*_test.go`** — AST-walking import firewalls. Pattern reused for D-07-10's credential-redaction firewall.

### Established Patterns

- **Position-aware errors everywhere.** `<file>:<line>:<col>: <msg>`. `wrapBuiltinError` in `pkg/parser/builtins.go` is the wrapping helper; every Phase 7 builtin uses it.
- **`Freeze()` recursion through nested values.** `dag.Trigger.Freeze()` recursively freezes the captured `Source` (TriggerSource implementations expose their own `Freeze()`), `MapLambda`, and `IdempotencyLambda`. Mirrors `dag.ActionRef.Freeze()`.
- **Frozen-after-boot registries with `sync.RWMutex`.** Both registries follow `FlowRegistry`'s lifecycle.
- **Test-only stub types in `_test.go` files.** Established in `pkg/extension/registry_test.go` (fake extensions), `pkg/parser/fixtures_test.go` (fixtures). Phase 7's test-stub `fakeTriggerSource` slots in here.
- **Sorted-path FS walks for determinism.** `pkg/worker/boot.go::bootRegistry` already does `sort.Strings(starFiles)`. New trigger walk shares the loop.
- **`sync.Once`-wrapped Stop methods.** `pkg/worker/worker.go::Worker.Stop` precedent. The server subcommand's drain-then-stop logic should also be `sync.Once` against the second-signal escalation path (D-07-15).

### Integration Points

- **`pkg/parser/globals.go::newParseTimeGlobals`** — add one line: `"trigger": starlark.NewBuiltin("trigger", p.builtinTrigger)`.
- **`pkg/parser/finalize.go`** — extend with the cross-file trigger.FlowName check (D-07-12). The finalize pass already has access to the parser's flow-name set.
- **`pkg/worker/boot.go::bootRegistry`** — extend the parse loop to also collect triggers via a new parser accessor (`p.Triggers()` mirroring `p.Flows()`).
- **`pkg/worker/worker.go::NewWorker`** — accept the new TriggerRegistry; store on the `Worker` struct; expose via a new `Triggers()` accessor (parallel to `Registry()`).
- **`pkg/cli/root.go::NewRootCommand`** — `root.AddCommand(newServerCommand(cfg))` plus the dev-server → dev-temporal rename in the existing line.
- **`cmd/skytime/main.go`** — gains `server` subcommand for free (no edits needed beyond the doc comment listing subcommands).
- **`examples/http-github-webhook/cmd/extbin/main.go`** — gains `server` subcommand for free; only the top doc-comment subcommand list needs updating.
- **`tests/docgen_drift_test.go`** — will fail until docs are regenerated post-rename. `go generate ./...` clears it.

</code_context>

<specifics>
## Specific Ideas

- **Firewall test scope.** Beyond the cobra/charm-log firewall (`tests/firewall_cli_test.go`) which Phase 7 must continue to pass, add a NEW firewall test for credential redaction (D-07-10). Implementation: AST-walk `pkg/dag/trigger.go` and any source-extension package; assert no fmt verb against `*dag.Trigger` or `TriggerSource` types is `%+v` or `%#v`. Reuse the AST walker pattern from existing firewall tests.
- **Drain-timeout edge case to test.** Submit a workflow with a deliberately slow activity (test-only mock that sleeps for `--drain-timeout * 2`); SIGTERM the server; verify exit code 1 + timeout message printed + Temporal history shows the workflow as still-pending (resumable on next worker start). This proves the durability story end-to-end at unit-test scope.
- **`registered triggers: [...]` example output format.** Sorted alphabetically by source-kind first, then by flow-name. Concrete:
  ```
  [skytime] registered 2 triggers: [github.webhook → check_user, github.webhook → batch_label_issues]
  ```
  vs the equivalent JSON when `--json-log` is set:
  ```json
  {"time":"2026-05-08T...","level":"INFO","msg":"registered triggers","count":2,"triggers":[{"source":"github.webhook","flow":"batch_label_issues"},{"source":"github.webhook","flow":"check_user"}]}
  ```
- **Test corpus for parse-time `req.<field>` walk.** Build a Phase 7 fixture file `pkg/parser/testdata/triggers/`:
  - `valid.star` — `trigger(map=lambda req: {"x": req.payload.foo})` against a stub source declaring `req.payload.foo`. Parses clean.
  - `typo.star` — same but `req.payloud.foo`. Errors with valid-field list.
  - `bad_arity.star` — `lambda req, headers: ...` (two positionals). Errors at parse time.
  - `unknown_flow.star` — `trigger(flow="missing", ...)` with no matching flow. Errors at parse-finalize.
  - `mutable_closure.star` — Phase 1 free-var lint catches; reused.

</specifics>

<deferred>
## Deferred Ideas

Captured during discussion but belong to other phases or are out of scope:

- **`triggers.github_webhook` namespace under a separate `triggers` extension.** Per D-07-08, this design is REJECTED in favor of source factories living under their owning extension (e.g., `github.webhook(...)`). REQUIREMENTS.md TRIG-07/08 wording will need a small update during planning. Not a deferred idea — an active deviation.
- **Source factory implementations.** `github.webhook(events=, secret_credential=)`, generic webhook (under `webhook` extension), and `cron(...)` source factories ship in 7.1 (HTTP) and 7.2 (cron). Phase 7's `TriggerSource` SDK contract enables them.
- **HTTP listener + signature validation.** Phase 7.1 (TRIG-06..10).
- **Idempotency mapping (`X-GitHub-Delivery` → WorkflowID with REJECT_DUPLICATE).** Phase 7.1.
- **Cron `req` shape.** Phase 7.2 will declare `req.scheduled_time`, `req.workflow_attempt`, etc. — Phase 7's req-attribute walker just needs to be source-extensible (each `TriggerSource` declares its own `ReqSchema()`).
- **`cli.WithCredfile(path)` Option lift from extbin into pkg/cli.** Phase 7.4 (CLI-08).
- **`cli.WithBuildID(string)` Option.** Phase 7.4 (CLI-09).
- **Dashboard, manual trigger UI, recent webhook deliveries ring buffer.** Phase 7.3.
- **Auth integration docs (`docs/for-extension-developers/temporal-auth.md`).** Phase 7.5.
- **JSON `--json-log` flag everywhere.** Phase 7's `skytime server` introduces this flag as a localized addition. Lifting `--json-log` to the root command and routing all subcommand output through it is OUT OF SCOPE — defer to a separate task or v1.44+.
- **Long-connection retry backoff on `connectClient` for `skytime server`.** Captured as Claude's discretion; recommend YES for Phase 7 but planner can defer to 7.1 if scope pressure.

</deferred>

---

*Phase: 07-trigger-primitive-server-shell*
*Context gathered: 2026-05-08*
