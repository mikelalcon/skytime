# Phase 4: Static Validation Tier + CLI Skeleton - Context

**Gathered:** 2026-05-01
**Status:** Ready for planning

<domain>
## Phase Boundary

Build the static validator and the CLI tree (`cmd/skytime/{validate,run,dev-server}`) so consultants can lint and trigger flows without writing Go. The validator MUST share the parse path with the runtime via a differential corpus test; the CLI MUST keep `cobra` and `charmbracelet/log/v2` out of the library root (CLI-05).

Phase 4 owns 7 v1 requirements: VAL-01, VAL-02, VAL-03, CLI-01, CLI-02, CLI-04, CLI-05. Phase 5 owns `skytime test` (CLI-03) and the Tier-3 mock harness — the dry-run interpreter seam Phase 4 lays MUST compose with Phase 5's Starlark-mock layer.

What's already in place (do not redo):
- D-04 typed errors with `Position()` and `<file>:<line>:<col>: <msg>` formatting (`pkg/dag/errors.go`)
- D-06 `*slog.Logger` interface in the library; CLI wires charm-log as the slog handler
- D-11 reflection-based kwarg validation via `extension.UnpackOperationKwargs`
- D-19 free-var lint (Starlark-language-level: module-level bindings only)
- D2-05 / D2-07 / D3-19 lint passes already wired in `parser.finalize`
- D3-04 `WorkflowInput = {FlowName, ContentHash, InitState}`
- D3-07 worker boots from a filesystem `--rootdir`
- D3-17 three named client constructors (`NewCloudClient` / `NewSelfHostedClient` / `NewDevClient`)
- D3-20 BuildID via `-ldflags "-X .../defaultBuildID=$(git rev-parse HEAD)"`
- AST-walking firewall test (Phase 2) — Phase 4 extends the allow-list for `cobra` / `charmbracelet/log/v2` to `{cmd/skytime, pkg/cli}`

</domain>

<decisions>
## Implementation Decisions

### Validator Architecture

- **D4-01: Validation logic split — checks in `parser/finalize.go`, facade in `pkg/validator`.** The new finalize-pass checks (kwarg cross-validate beyond the per-call `UnpackOperationKwargs`, free-vars-reference-declared-state, lambda-time globals re-assertion) are added to `parser/finalize.go` alongside `lintMixedIdempotency` / `lintBlockSize` / `lintEmptyTaskQueue`. They fill the documented `validateActionRefKwargs` no-op stub. A new `pkg/validator` package is a thin facade: `Validate(file string, opts ...Option) []error` calls `parser.ParseFile` and returns the typed errors. `pkg/validator` also owns the dry-run interpreter seam (D4-03). Cleanest split: parser owns the checks; validator owns the CLI-facing surface and the differential-test seam.

- **D4-02: "Lambda free vars reference declared state" = AST walk for `ctx.<name>` attribute accesses.** Beyond the Phase 1 D-19 Starlark-language check (free vars must bind to module-level constants), Phase 4 adds a check that EVERY `ctx.<name>` attribute access inside a captured lambda resolves to a name in the lexically-visible state schema at that lambda's position. The state schema accumulates body-walking:
  - At flow entry: state = keys of `flow(inputs={...})`
  - After `script(output_alias=X, fn=lambda ctx: {...})`: state += {X}
  - Inside `for_each_parallel(items=..., item="row", steps=[...])`: state += {row} for the duration of `steps`
  - Branches inside `if_cond(then=[...], else_=[...])` see the same pre-branch state; outputs from `then` are NOT visible in `else_` and vice versa
  Implementation requires an AST visitor over `*starlark.Function.Funcode.AST` (or equivalent) that surfaces every `ctx.<attr>` reference. Reject with `*dag.ValidationError` carrying the lambda's position when a name is missing.

- **D4-03: Dry-run interpreter seam = test-only mock `OperationDispatch`.** The differential corpus test (VAL-02) is a Go test in `pkg/validator` (or `pkg/parser`) that, for every `.star` file in `examples/`, runs:
  1. `parser.ParseFile + validator.Validate` → set of errors A
  2. The full `pkg/interpreter.SkytimeWorkflow` against `testsuite.TestWorkflowEnvironment` with a mock `OperationDispatch` that returns `OkResult{}` for every call → set of errors B
  Asserts A and B agree on accept/reject. The mock dispatch lives in a test helper (`pkg/activity/testing/` or `pkg/interpreter/testing/`); NOT exposed as a CLI flag or public library API. Composes with Phase 5: same dispatch-replacement seam, different mocks (Starlark-driven instead of always-OK).

- **D4-04: `dag.ValidationError` gains an `Action string` field.** Format becomes `<file>:<line>:<col> [flow > step > action]: <msg>` when fields are non-empty (each segment dropped when blank). Lints that know the offending action populate `Action` (e.g., kwarg-mismatch on `github.create_issue` sets `Action="github.create_issue"`); lints scoped to the step level leave it empty. `pkg/dag/errors.go` `Error()` is updated to render the bracket; the CLI's renderer reads the same format. Phase 1-3 callers that don't set `Action` keep working — the bracket simply omits the action segment.

### `skytime run` Execution Model

- **D4-05: Embedded transient worker.** `skytime run <file.star> --flow=X --input=...` constructs a worker in-process via `pkg/worker.NewWorker` against the connected client (D3-17 routing), calls `client.ExecuteWorkflow` with `WorkflowInput{FlowName: X, ContentHash: <hash of file>, InitState: parsed JSON}`, follows execution to completion, prints the result, and shuts the worker down. Single-binary UX for the Phase 6 README walkthrough goal ("git clone to executed flow in <5 commands"). The Phase 6 README MUST clarify that `run` is a dev-mode convenience; production uses long-running worker processes registering against the cluster.

- **D4-06: Per-step progress + final result.** As each Step / IfCond branch / Script / ForEachParallel fan-out / CallFlow child workflow executes, the CLI prints a structured event line (Starlark-position-aware: `[flow:step at file:line] dispatching github.create_issue`). Activity dispatches print start/end with elapsed time. `print()` output from lambdas (D3-22 routes to `workflow.GetLogger`) is teed to the CLI. Final result + status. NOT a full Temporal event-history dump — implementation likely uses a `slog.Handler` shim that filters `pkg/interpreter` and `pkg/activity` log records. Mid-weight: feels live without being noisy.

- **D4-07: `--input=<json>` validated through the same input-schema check as static `validate`.** `skytime run` parses the JSON into `map[string]any`, then runs the same input-schema validation pass that `skytime validate` uses (the VAL-01 "every input maps to a registered schema" check). Mismatches surface as the same typed `*dag.ValidationError` shape (no separate runtime-only error path). Single source of truth; CLI errors look the same whether you ran `validate` or `run`.

- **D4-08: Connection via flags + env vars; variant chosen by which flags are present.** Flags: `--address`, `--namespace`, `--api-key`, plus mTLS triplet `--client-cert`, `--client-key`, `--server-ca`. Env-var fallbacks: `SKYTIME_TEMPORAL_ADDRESS`, `SKYTIME_TEMPORAL_NAMESPACE`, `SKYTIME_TEMPORAL_API_KEY`, `SKYTIME_TEMPORAL_CLIENT_CERT`, etc. Variant routing:
  - `--api-key` set → `worker.NewCloudClient`
  - mTLS triplet set → `worker.NewSelfHostedClient`
  - Otherwise → `worker.NewDevClient` (defaults: `localhost:7233`, namespace `default`)
  No config file (no koanf) in v1; revisit if flag count grows. Flags shared across `run` / `validate` (validate ignores them) / `dev-server` (dev-server ignores them).

### `skytime dev-server` Strategy

- **D4-09: Shell out to `temporal server start-dev` subprocess.** `skytime dev-server` does NOT embed Temporalite as a Go dep. It locates the `temporal` binary via `exec.LookPath`, spawns it as a subprocess attached to the terminal, and streams its stdout/stderr to the CLI's stdout/stderr. Documented prerequisite in Phase 6 README: `brew install temporal` / `curl -sSf https://temporal.download/cli.sh | sh` / `go install go.temporal.io/server/cmd/...@latest`. **This decision UPDATES Phase 3 CONTEXT.md `<specifics>` note that said "we don't spawn the dev server in-process for v1"** — we DO spawn (as a subprocess); the original Phase 3 note was about embedding-as-Go-dep, which we're not doing.

- **D4-10: Foreground lifecycle with SIGINT forwarding.** Subprocess attached to the terminal (no daemonization, no PID files). SIGINT/SIGTERM caught by the CLI and forwarded to the subprocess; CLI exits when subprocess exits. Familiar Unix idiom; no zombie risk; matches `temporal server start-dev`'s own behavior. No `start` / `stop` / `status` subcommands in v1.

- **D4-11: Match `temporal server start-dev` defaults; pass-through user flags.** Defaults: frontend `:7233`, UI `:8233`, namespace `default`. CLI accepts `--port`, `--ui-port`, `--namespace`, etc., and forwards them to the subprocess. Skytime does NOT pre-create namespaces, search attributes, or task queues — `skytime run` configures task queues at workflow start time via D3-19. Phase 6 README documents the prerequisite + the default URL.

- **D4-12: Missing `temporal` binary → clear error with install instructions.** `exec.LookPath("temporal")` fails → CLI prints:
  ```
  error: `temporal` CLI not found on PATH.
  Install:
    macOS:   brew install temporal
    script:  curl -sSf https://temporal.download/cli.sh | sh
    Go:      go install go.temporal.io/server/cmd/temporal@latest
  ```
  Exits non-zero. No auto-install in v1.

### CLI Extensibility

- **D4-13: `pkg/cli` is the reusable cobra command tree; `cmd/skytime` is a thin wrapper.** `pkg/cli.NewRootCommand(opts ...Option) *cobra.Command` returns the root command with `validate`, `run`, `dev-server` subcommands wired. `cmd/skytime/main.go` calls it with the canonical built-in extension (D4-14) and `cobra.Execute()`s. Phase 6's example project builds its own `cmd/example-skytime` (or similar) that calls `pkg/cli.NewRootCommand` with HTTP+GitHub+Slack registered. **AST firewall test extension:** the existing test that gates `go.temporal.io/sdk/...` allow-list adds a parallel pass for `github.com/spf13/cobra` and `github.com/charmbracelet/log/v2` — only `cmd/skytime/...` and `pkg/cli/...` may import them. `pkg/parser`, `pkg/dag`, `pkg/extension`, `pkg/bridge`, `pkg/activity`, `pkg/interpreter`, `pkg/worker`, `pkg/validator` MUST NOT.

- **D4-14: `cmd/skytime` ships with a generic HTTP extension baked in.** A new `pkg/extension/builtin/http` (or `extensions/http` — exact path is Claude's discretion during planning) provides:
  - `http.endpoint(base_url=..., credential=...)` factory returning an HTTP-extension instance
  - `endpoint.get(path=..., headers=...)` — Idempotent (true)
  - `endpoint.post(path=..., body=..., headers=...)` — Idempotent (false)
  - `endpoint.put(path=..., body=..., headers=...)` — Idempotent (false)
  - `endpoint.delete(path=..., headers=...)` — Idempotent (false)
  - `endpoint.head(path=..., headers=...)` — Idempotent (true)
  - Returns `HTTPResponse` (a typed `OperationOutput`) with `Status int`, `Body []byte`, `Headers map[string]string`
  - Accepts `BearerCredential`, `BasicCredential`, `APIKeyCredential` (auto-routed by kind)
  - Uses Go stdlib `net/http`; no third-party HTTP client library
  Phase 6's HTTP extension MAY be more sophisticated (retries, backoff helpers); the Phase 4 baked-in is the lowest-common-denominator that makes `examples/skeleton/` runnable.

- **D4-15: Extension and credential-handler registration via functional options.** `pkg/cli.NewRootCommand(cli.WithExtensions(http.New(...)), cli.WithCredentialHandler(myHandler))`. Mirrors Phase 1's `parser.NewParser` and Phase 3's `worker.NewWorker` patterns. No global registry, no `init()`-time side-effects (Phase 1 D-07 "no global state" extends here).

- **D4-16: No-extensions mode for `validate` = standard `*dag.ParseError` with actionable hint.** When a `.star` file references an extension that isn't registered (e.g., `github.endpoint("admin")` in a binary built without the github extension), the parser already produces a `*dag.ParseError` with position. The CLI's error renderer recognizes the "unknown extension" case and appends:
  ```
  hint: This binary doesn't have the `github` extension registered.
        Build a custom Skytime CLI binary that registers your extensions.
        See: docs/cli-binary.md
  ```
  No `--syntax-only` flag, no auto-stub. Predictable, no special mode.

### Corpus & Differential Test

- **D4-17: Bootstrap minimal `examples/skeleton/`.** Phase 4 lands `examples/skeleton/` containing 2-3 `.star` flows that collectively exercise every primitive (sequential `step`, `block` batch, `if_cond`, `script`, `for_each_parallel`, `call_flow`) using ONLY the baked-in HTTP extension (D4-14). The differential corpus test (VAL-02, D4-03) runs against this directory. Phase 6 expands `examples/` with the full HTTP+GitHub+Slack project; the differential test glob picks up new files automatically. NOT moved into Phase 6's scope — Phase 4 must ship a working differential CI test, and a corpus is the input.

### CLI Error Rendering

- **D4-18: Default error rendering is Starlark-first.** CLI converts every error reaching `cobra.Command.Execute()` via `errors.As` into the typed `*dag.ParseError` / `*dag.ValidationError` and renders the `<file>:<line>:<col> [flow > step > action]: <msg>` format. Wrapped Go errors are dropped from default output. Exit non-zero on any error. Color via charm-log when stdout is a TTY; plain when piped.

- **D4-19: `--debug` flag reveals Go internals.** Sets the slog handler level to debug and includes `Wrapped` chains in error rendering (`fmt.Sprintf("%+v", err)`). Stack traces remain hidden — Skytime errors don't carry stack traces, the option just unwraps. Available on every subcommand. Documented as "the only path that reveals Go internals" per VAL-03 success criterion #5.

### Folded Todos

None — `todo match-phase 4` returned no matches.

### Claude's Discretion

- Exact path of the baked-in HTTP extension (`pkg/extension/builtin/http`, `extensions/http`, `pkg/http_ext`, etc.).
- Exact AST visitor implementation for the `ctx.<name>` attribute walk (D4-02). go.starlark.net exposes `*syntax.Function.Body` ASTs; the visitor walks `*syntax.DotExpr` nodes whose `X` is the lambda's first param identifier.
- Cobra subcommand file layout under `pkg/cli/` (one file per subcommand vs combined).
- Slog handler shim implementation for D4-06 progress streaming (which fields to surface, exact prefix tokens).
- Test helper location for the dry-run interpreter mock (`pkg/activity/testing/` vs `pkg/interpreter/testing/` vs `pkg/validator/internal/dryrun/`).
- Charm-log rendering options (separator characters, color theme).
- Whether the worker's frozen-after-boot constraint (D3-07) needs any tweak for the embedded `skytime run` worker — it likely doesn't, because `run` parses once and never re-walks the disk.
- Schema declaration shape for `flow(inputs={...})` — currently a dict literal; Phase 4 may want richer typing (e.g., `inputs={"repo_name": str, "limit": int}`) or keep the existing list-of-names. If extended, document the schema language as part of the parser builtin.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Project specs
- `.planning/PROJECT.md` — Strict directives (no string compilation, no dynamic activities, no context bleed).
- `.planning/REQUIREMENTS.md` — Phase 4 owns VAL-01..03, CLI-01, CLI-02, CLI-04, CLI-05.
- `.planning/ROADMAP.md` — Phase 4 entry: goal, success criteria, requirements list.

### Prior phase outputs
- `.planning/phases/01-type-spine-extension-contract-parser-bridge-foundations/01-CONTEXT.md` — D-04 (typed errors), D-06 (slog), D-11 (reflection kwarg validation), D-19 (free-var lint), D-20 (lambda-time globals).
- `.planning/phases/02-generic-activity-block-batch-dispatch-credentials/02-CONTEXT.md` — D2-01 ActionResult shape (consumed by dry-run mock), D2-05/D2-07 lints already in `parser/finalize`.
- `.planning/phases/03-lambda-serialization-decision-interpreter-worker/03-CONTEXT.md` — D3-04 WorkflowInput (consumed by `skytime run`), D3-07 RootDir (consumed by validate/run), D3-17 client constructors (consumed by `skytime run`'s connection routing), D3-19 task_queue precedence, D3-20 BuildID. **D4-09 supersedes** D3 `<specifics>` note about not spawning dev server.

### Codebase entry points (read before planning)
- `pkg/parser/parser.go` — `Parser` struct, `NewParser`, `ParseFile`, `ParseSource`.
- `pkg/parser/finalize.go` — finalize pass sequence; `validateActionRefKwargs` is the documented Phase 4 seat.
- `pkg/parser/lambda_capture.go` — `validateFreeVars` (D-19); D4-02's new check sits alongside.
- `pkg/parser/linter.go` — pattern for new finalize lints (`lintMixedIdempotency`, `lintBlockSize`).
- `pkg/parser/errors.go` — `wrapStarlarkError` (used by CLI error rendering).
- `pkg/dag/errors.go` — `ParseError` and `ValidationError`; D4-04 adds `Action` field here.
- `pkg/dag/input.go` — `WorkflowInput{FlowName, ContentHash, InitState}` (consumed by `skytime run`).
- `pkg/dag/flow.go`, `pkg/dag/step.go`, `pkg/dag/control.go` — node types the validator walks for D4-02.
- `pkg/extension/registry.go`, `pkg/extension/operation.go` — schema source for D-11 / D4-02 cross-validate.
- `pkg/worker/boot.go` — `bootRegistry`; the embedded-worker `skytime run` reuses this directly.
- `pkg/worker/client.go` — `NewCloudClient` / `NewSelfHostedClient` / `NewDevClient`; CLI flag routing (D4-08) calls into these.
- `pkg/worker/worker.go` — `NewWorker`, `Worker.Start` / `Worker.Stop`; the embedded-worker `skytime run` orchestrates this lifecycle.
- `pkg/interpreter/workflow.go` — `SkytimeWorkflow` (consumed by the dry-run differential test).
- `pkg/activity/execute_batch.go` — `ExecuteBatch`; the dry-run mock dispatch replaces this.
- `pkg/worker/firewall_test.go` — pattern for the new cobra/charmlog firewall test (D4-13).
- `tests/fixtures/valid/` and `tests/fixtures/invalid/` — Phase 1 fixture corpus (NOT the Phase 4 corpus; `examples/skeleton/` is the differential corpus per D4-17).

### External (CLI-specific)
- `github.com/spf13/cobra` v1.10.2 — root + subcommand pattern, PreRun chain for `load config → init logger → connect Temporal client`.
- `github.com/charmbracelet/log/v2` v2.0.0 — slog handler with TTY detection, color theme.
- `temporal server start-dev` docs — flag list (`--port`, `--ui-port`, `--namespace`, `--ip`, `--db-filename`).
- `go.temporal.io/sdk/client` — `client.ExecuteWorkflow`, `WorkflowRun.Get`, `client.QueryWorkflow` (for D4-06 progress polling).
- `go.starlark.net/syntax` — `*syntax.Function`, `*syntax.DotExpr`, AST walking for D4-02's `ctx.<name>` visitor.

### Project-level research
- `.planning/research/STACK.md` §"Supporting Libraries" / "Development Tools" — cobra, charm-log, koanf rationale.
- `.planning/research/SUMMARY.md` §"Phase 4" if present — original Phase 4 framing (verify alignment).
- `.planning/research/PITFALLS.md` §10 (static/runtime parser unity) — mandates VAL-02 differential test.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets

- **`parser.Parser` is the single parse path.** Both the runtime worker boot (`pkg/worker.bootRegistry`) and the static validator (Phase 4) call `parser.ParseFile` — VAL-02's "share the parser code path" is satisfied by construction.
- **`parser.finalize.validateActionRefKwargs`** — documented no-op stub explicitly reserved for Phase 4. D4-01 fills it.
- **`parser.linter.go` patterns** — `lintMixedIdempotency`, `lintBlockSize`, `lintEmptyTaskQueue` use a uniform recursion-walk shape over `dag.Node` bodies. New Phase 4 lints (D4-02 free-var-state, D4-04 kwarg cross-validate) follow the same idiom.
- **`pkg/worker.NewWorker` + `worker.Worker.Start/Stop`** — the embedded transient worker for `skytime run` (D4-05) reuses these directly. `Worker.Registry()` exposes `ContentHashFor` for the WorkflowInput hash (D3-04).
- **`pkg/worker.NewCloudClient` / `NewSelfHostedClient` / `NewDevClient`** — CLI flag routing (D4-08) dispatches to these by which flags are present.
- **`*dag.ParseError` / `*dag.ValidationError`** with `Position()` — CLI's error renderer (D4-18) reads these via `errors.As`.
- **AST firewall test (`pkg/worker/firewall_test.go`)** — pattern for D4-13's new cobra/charmlog firewall test: walk every Go source file's imports via `go/parser`, fail when disallowed import appears outside the allow-list.

### Established Patterns

- **Sealed interfaces for sum types** (Phase 1 `Credential`, Phase 2 `ActionResult`, `OperationOutput`) — the HTTP extension's `HTTPResponse` (D4-14) is another typed `OperationOutput`.
- **Functional options** for constructor APIs (`parser.NewParser`, `worker.NewWorker`) — `pkg/cli.NewRootCommand` continues this (D4-15).
- **Per-instance registries (no global state)** — Phase 1 D-07; D4-15 keeps this for `pkg/cli`.
- **Co-located `*_test.go` + `tests/fixtures/`** — Phase 4 adds `tests/fixtures/cli/` for CLI integration tests (Claude's discretion); the differential corpus is `examples/skeleton/`.
- **`-ldflags "-X .../defaultBuildID=..."`** (D3-20) — `cmd/skytime/main.go` sets the same variable so CI builds carry the commit SHA as Identity.
- **Atomic per-task commits** — Phase 4 follows the same convention.

### Integration Points

- **`pkg/validator`** is the new package; sits between `pkg/parser` and `cmd/skytime` in the dependency graph. May import `pkg/parser`, `pkg/dag`, `pkg/extension`, `pkg/interpreter` (for the dry-run seam), `pkg/activity` (for the mock dispatch). Must NOT import `cobra` or `charm-log`.
- **`pkg/cli`** is the new public CLI surface. Imports `cobra`, `charm-log`, `pkg/validator`, `pkg/worker`, `pkg/parser`, `pkg/extension`. Single library-side package allowed cobra/charmlog imports.
- **`cmd/skytime`** is the binary. Imports `pkg/cli` and the canonical HTTP extension (D4-14). Sets `defaultBuildID` via `-ldflags`.
- **`examples/skeleton/`** is the corpus + dogfood directory. 2-3 `.star` flows using only the baked-in HTTP extension. Phase 6 expands into `examples/http-github-slack/`.
- **AST firewall test** in `pkg/worker/firewall_test.go` (or a new `tests/firewall_test.go`) — Phase 4 EXTENDS the test to include cobra/charmlog gating.
- **Phase 5 composition point** — the dry-run interpreter seam (D4-03) is the same hook Phase 5's Starlark mock harness will use; `pkg/validator` and Phase 5's `pkg/testing` share the dispatch-replacement helper.

</code_context>

<specifics>
## Specific Ideas

- **HTTP extension feels like `requests`-shaped Starlark.** Consultants writing `.star` should be able to express:
  ```python
  http = http.endpoint(base_url="https://api.github.com", credential="github_admin")
  step(action=http.get(path="/repos/foo/bar/issues"), retry_policy={...})
  ```
  Output `HTTPResponse{Status, Body, Headers}` accessed via `ctx.<output_alias>.status` after a `script`.

- **Progress streaming format (D4-06):** suggest a structured prefix like `[skytime] flow=<name> step=<idx> action=<kind>` so output is greppable. Keep messages on a single line per event when possible; multi-line for `print()` payloads with indentation.

- **`examples/skeleton/` shape (D4-17 detail):** suggest 2 flows:
  1. `simple_check.star` — sequential `step` + `script` + `if_cond` against `http.get`. Exercises sequential, conditional, and script primitives.
  2. `parallel_fanout.star` — `for_each_parallel` over a list of repos, each with a `step(block=[...])` of HTTP gets, then a `call_flow` to a helper. Exercises block-batch, parallel, and child-workflow primitives.
  Two files cover all six primitives. Phase 6 reuses the same files (or extends with GitHub-specific variants).

- **Differential test failure mode (D4-03):** when static and dry-run disagree on accept/reject, the test prints both error sets and the offending file path. CI fails fast on first divergence.

- **`temporal` CLI version pinning (D4-09):** v1 documents the prerequisite as "any reasonably recent `temporal` CLI" and pins the lowest tested version in Phase 6 README. Strict pinning is a v1.x concern.

- **Phase 3 CONTEXT.md note supersession:** the line "we don't spawn the dev server in-process for v1" in Phase 3 `<specifics>` is **intentionally overridden** by D4-09 — we spawn AS A SUBPROCESS, not as an in-process Go embed. The original intent (avoid Temporalite Go dep) is preserved.

- **Library-binary docs:** Phase 4 should produce a short `docs/cli-binary.md` (or similar) that walks consultants through building their own custom CLI binary by importing `pkg/cli`. Referenced from D4-16's error hint. Concrete path is Claude's discretion.

</specifics>

<deferred>
## Deferred Ideas

- **Embed Temporalite as a Go dep** — explicitly NOT in v1 (D4-09). Re-evaluate if the `temporal` CLI install friction becomes a real customer pain point; pulling in temporalite later is purely additive (would live behind a build tag).

- **Config file (`~/.skytime.yaml`)** — koanf-based config loading is deferred. Flags + env vars are sufficient for v1's CLI surface (D4-08). Add when the flag count crosses ~10 or per-environment configs become essential.

- **Auto-install of `temporal` CLI** — explicitly NOT in v1 (D4-12). Keeps the CLI simple and avoids version-pinning headaches.

- **`skytime dev-server start/stop/status` daemon mode** — explicitly NOT in v1 (D4-10). Foreground-only simplifies the v1 surface; background daemon mode is a v1.x add.

- **`--dry-run` CLI flag on `skytime run`** — the dry-run capability exists as a test seam (D4-03), not a user-facing flag. Re-evaluate if consultants ask for "run my flow without hitting any backends" as an authoring loop.

- **Full Temporal event-history dump option** — D4-06 ships per-step progress, not the raw event stream. Add `--event-history` flag in v1.x if customers need it for debugging.

- **`--syntax-only` validate mode** — explicitly NOT in v1 (D4-16). Re-evaluate if "draft .star authoring without registered extensions" becomes a real workflow.

- **Auto-stub unknown extensions in validate** — explicitly NOT in v1 (D4-16). Risks false negatives that mask real misconfigs.

- **JSON / structured error output for CI consumption** — default rendering is Starlark-first text; JSON output (`--format=json`) is a v1.x addition.

- **Cross-flow dataflow analysis** (e.g., proving `script(output_alias=X)`'s declared output keys are actually returned by the lambda) — Phase 4 checks `ctx.<name>` accesses; it does NOT execute lambdas at validate time. Lambda-output verification needs the dry-run interpreter (which Phase 4 ships as a test seam, not a CLI flag). v1.x convergence as authoring tools mature.

- **`charmbracelet/fang` (cobra wrapper)** — STACK research flagged this as worth re-evaluating once it stabilizes. v1 uses plain cobra + charm-log handler.

- **Tier-2 unit tests for `def` blocks** — `TEST-V2-01`; Phase 4's lambda-time globals re-assertion is the foundation.

- **Reviewed Todos (not folded)** — none; `todo match-phase 4` returned an empty matches array.

</deferred>

---

*Phase: 04-static-validation-tier-cli-skeleton*
*Context gathered: 2026-05-01*
