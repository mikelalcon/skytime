# Skytime Architecture

Skytime is a Go library that lets teams declare durable workflows in Starlark
and execute them on Temporal. The boundary between Starlark (parse-time,
deterministic graph generation) and Temporal (execution-time, durable
orchestration) is **absolute and architectural** — required for the safety
properties summarized by the project's three "no" rules: no string
compilation, no dynamic activities, no context bleed.
This page exists because every other architectural choice — extension contract,
lambda capture, error rendering, the entire CLI — is downstream of that one
boundary. Read it before any other doc; both flow-authors writing `.star` files
and extension developers writing Go need the mental model that follows.

## The Parse/Execute Split

Skytime splits workflow authoring into two phases that do not communicate via
shared mutable state, runtime callbacks, or escape hatches. The artifact handed
across the boundary is a typed, JSON-serializable `dag.Flow` plus a registry of
captured lambdas keyed by content-hash IDs — nothing more.

**At parse-time, Skytime:**

- Loads a `.star` file via `pkg/parser` (`Parser.ParseFile`), running the
  Starlark interpreter against a strict predeclared environment built by
  `pkg/parser/globals.go::newParseTimeGlobals`.
- Captures every `lambda` it encounters as a `*dag.CapturedLambda` — the raw
  `*starlark.Function` plus the position metadata required by AST-walking
  validators. See `pkg/dag/lambda.go::CapturedLambda`.
- Validates kwargs against extension Go schemas (reflection-based, single-pass)
  and rejects free-variable closures over mutable state (D-19 frozen-only rule).
- Constructs the `*dag.Flow` DAG: a sealed sum of `Step`, `IfCond`, `Script`,
  `ForEachParallel`, `CallFlow`, `Result`, and `Fail` nodes. The DAG is plain
  data — no callbacks, no I/O handles, no `workflow.Context`.
- Performs **no I/O whatsoever**. `Parser.Parse` is a pure function of file
  contents. This property leaves the door open for hot-reload (deferred) and is
  the reason cosmetic edits to `.star` files generate fresh content-hash IDs.

**At execution-time, Skytime:**

- Boots `SkytimeWorkflow` (registered by `pkg/interpreter`, see
  `pkg/interpreter/workflow.go::NewWorkflow`) inside a Temporal worker.
- Receives a `WorkflowInput{FlowName, ContentHash, InitState}` over Temporal
  history — `*starlark.Function` values never cross the wire (D3-01..D3-05).
- Looks up the parsed flow in a content-hash-keyed `FlowRegistry` (the worker
  re-parses every `.star` file at boot — see "Deterministic Re-parse on
  Workflow Start" below).
- Walks the DAG node-by-node via `walkBody`, dispatching `Step` actions to the
  single generic `pkg/activity::ExecuteBatch` activity, evaluating `IfCond` /
  `Script` / `ForEachParallel.items` lambdas inline through
  `pkg/bridge::CallLambda`, and emitting zero Temporal history events for
  pure-data nodes (`script`, `if_cond` decisions).
- Honors retries, timeouts, and cancellation via Temporal's native primitives.
  Lambdas never see `workflow.Context`; cancellation reaches them through a
  bridged native channel — covered in "No Context Bleed" below.

The split's payoff is that **both phases are independently testable and
independently reasonable about**. A library developer can run a `.star` file
through `validator.Validate` without booting Temporal. A workflow author can
inspect what their `.star` parses to via `skytime info <file>`. The runtime
walker is a pure DAG interpreter that cannot be tricked into evaluating user
strings.

## The Boundary Diagram

```
                           PARSE TIME (no I/O, deterministic)
                                            │
   user .star file ──► pkg/parser ──► dag.Flow (typed, JSON-serializable)
   (consultant team)    (Starlark        with captured *starlark.Function
                         interpreter)    instances keyed by stable content-hash
                                            │
                                            │
   ════════════════════════════════════════ │ ════════════════════════════
                                            │      WORKFLOW BOUNDARY
   ════════════════════════════════════════ │ ════════════════════════════
                                            │
                                            ▼
                            EXECUTE TIME (durable, history-recorded)
                                            │
   pkg/interpreter ──► workflow.Go ──► pkg/bridge ──► pkg/activity ──► extension I/O
   (SkytimeWorkflow)    (deterministic     (lambda eval)   (ExecuteBatch    (real HTTP/
                        fan-out via                         single generic    DB/queue
                        workflow.Selector)                  activity)         calls)

   The workflow re-parses the .star file at start (Option B, Phase 3) so
   *starlark.Function values survive Temporal serialization. Only LambdaID
   strings cross history. content-hash keys + Build ID versioning let
   in-flight workflows complete on the original code while new starts use
   updated code.
```

**How to read this diagram.** Time flows top-to-bottom. The double-line
divider is the architectural firewall — the meta-test
`pkg/activity/firewall_test.go::TestNoTemporalImportsOutsideAllowList`
enforces that only `pkg/activity`, `pkg/interpreter`, and `pkg/worker` may
import any `go.temporal.io/sdk/*` path. Everything above the line builds
without Temporal at all and runs in plain `go test`. Everything below the
line lives inside a workflow goroutine subject to Temporal's deterministic
runner. The diagram's arrows are calls; the absence of any arrow back across
the boundary is the point.

## Why Starlark at Parse Time

Starlark is the right language for parse-time graph construction because of
four properties that map exactly onto Skytime's safety contract.

**Deterministic by construction.** Starlark has no clock, no PRNG, no
filesystem, and no network — by language design (Bazel inherits this from its
hermetic-build heritage). A flow that parses to one DAG today parses to the
same DAG tomorrow on a different machine, so Temporal's replay determinism is
guaranteed at the parse boundary.

**Sandboxed at lambda time.** The lambda-time predeclared environment is a
strictly locked **20-key subset** of the parse-time environment, registered in
`pkg/bridge/lambda_globals.go`. Workflow lambdas see `len`, `min`, `max`,
`str`, `int`, dict/list constructors, and a small set of pure helpers — they
do **not** see `load()`, extension factories, or the DSL primitives. Compare
to `pkg/parser/globals.go::newParseTimeGlobals`, which is the richer parse-time
env. The two are intentionally distinct (PARSE-03, D-20).

**Expressive enough that no string DSL is needed.** Lambdas plus
`*starlarkstruct.Struct` injection give consultants a real expression language
for data access (`ctx.req.repo_name`) without resurrecting the parse-time
evaluation surface that string parsers would create. PROJECT.md's "Starlark
over CEL or custom DSL" Key Decision: *"Lambdas + struct injection give
expressive data access without a string-parsing risk surface."*

**Parser-pure.** `Parser.Parse` is a function of file contents and registered
extensions, period. There is no global state, no I/O, no clock leak, no
"first invocation wins" caching. This is what makes the deterministic re-parse
strategy (next sections) work: the same bytes always produce the same DAG,
and the same DAG always serializes to the same JSON.

## Why Temporal at Execution Time

Temporal is the right execution-time substrate because Skytime's value
proposition — *production-grade durable workflows without writing Go* — is
exactly what Temporal already solves at the SDK level. Everything below the
parse/execute boundary diagram runs at execution-time inside a Temporal
worker; everything above runs at parse-time in a plain Go test.

**Durable.** Workflow history is the source of truth. A worker dies
mid-`for_each_parallel` and a fresh worker resumes from history with the
already-completed branches replayed and the in-flight branches restarted.
The interpreter is a stateless walker; the state lives in Temporal's
event-sourced log.

**Retryable end-to-end.** Every step has a `RetryPolicy` and `StartToClose`
timeout, defaults flow from `Worker → Flow → Step` (precedence step > flow >
worker default, DSL retrofits in Phase 3), and `if_cond` / `script` produce
**zero** Temporal history events — they are pure DAG decisions. The single
generic `pkg/activity::ExecuteBatch` is the only activity, so all retry
configuration lives in one place.

**Composable.** `call_flow(name=..., inputs={...})` invokes a Temporal child
workflow. Parent `RetryPolicy` and `TypedSearchAttributes` propagate to
children (Phase 3). Skytime reuses Temporal's child-workflow primitive instead
of inventing one.

The entry point is `pkg/interpreter::SkytimeWorkflow` (registered via
`workflow.RegisterOptions{Name: "SkytimeWorkflow"}`, constructed by
`NewWorkflow(registry)`), and it is the **only** workflow Skytime ever
registers. Every flow, every child workflow, every retry, runs through this
one entry. That is what lets the firewall stay tight.

## Lambda Capture Model

Lambdas are captured at parse time and must survive Temporal's serialization
boundary. The model that solves this is dead simple: capture the Go pointer
in memory at parse, key it by a content-hash-derived ID, and re-parse on
workflow start to repopulate the pointer table. Only the ID strings cross
the wire.

The captured shape lives in `pkg/dag/lambda.go::CapturedLambda`:

```go
type CapturedLambda struct {
    ID       string                // sha256(fileBytes)[:8] + ":" + line + ":" + col
    Fn       *starlark.Function    // in-memory only — never JSON-serialized
    Pos      syntax.Position       // def-site of the `lambda` keyword
    BodyPos  syntax.Position       // body location (= Pos for hand-written;
                                   //                differs for synthesized lambdas)
    FreeVars starlark.StringDict   // frozen module-level constants (D-19)
}
```

**D-18 stable IDs.** The `ID` is `sha256(fileBytes)[:8] + ":" + line + ":" +
col` — see `dag.ComputeLambdaID`. The hash prefix is over the **file bytes**,
not a canonicalized AST. This means cosmetic edits (whitespace, comments)
intentionally change IDs. That is acceptable because the same file always
hashes to the same prefix, so re-parsing the same `.star` regenerates the
same ID set deterministically.

**Why the AST is not used.** `*starlark.Function.funcode` is unexported; the
syntax tree is discarded after compilation. Any walker that needs to inspect
lambda bodies (the D4-02 `ctx.<name>` validator, the D4.1-11 `block_fn`
classifier) re-parses cached file bytes via `(*syntax.FileOptions).Parse`
plus `syntax.Walk`, then matches lambdas by `(Filename, Line, Col)` against
`CapturedLambda.Pos` / `BodyPos`. This is a load-bearing finding from
Phase 4, not an implementation choice we can revisit cheaply.

**The carve-outs.** Two explicit exceptions to the "no string compilation"
rule rely on the lambda-capture model and **only** the lambda-capture model:

> *Parser-time syntactic sugar that desugars to native Starlark lambdas
> (e.g., `${ctx.expr}` → `lambda ctx: str(ctx.expr)`) is not string
> compilation. The runtime evaluation surface remains lambda-only. This
> carve-out exists for ergonomic step naming and string kwargs; runtime
> template engines (CEL, Jinja, etc.) remain forbidden. Extending this
> carve-out beyond parser-time desugaring requires a new ADR.* (Phase 04.1,
> D4.1-22)

> *The parse-time top-level `fail("msg")` builtin (D4.2-05) is a parse-time
> syntactic primitive that emits a workflow-failure node — it does NOT
> introduce a runtime evaluation surface beyond the existing lambda
> contract. The MessageFn lambda (when `${ctx.expr}` interpolation is
> present) evaluates per the standard `CapturedLambda` + `bridge.CallLambda`
> path established in Phase 1; the same desugarer used by D4.1-22 is reused
> verbatim. See `pkg/parser/doc.go` for dual `fail()` semantics (parse-time
> node-emit vs. lambda-time `fail` global).* (Phase 04.2, D4.2-05)

Both carve-outs desugar at **parse time** to a real `*starlark.Function`
that the existing CapturedLambda + `bridge.CallLambda` path then evaluates
at execute-time. There is no "interpolation engine" or "fail expression
evaluator" at runtime — only the same lambda path that has existed since
Phase 1.

## Deterministic Re-parse on Workflow Start

Phase 3 chose **Option B (re-parse on workflow start)** over a custom
Temporal `DataConverter`. The decision is logged in PROJECT.md Key Decisions:
*"Lambda serialization via re-parse on workflow start (Option B) + Build IDs.
No custom DataConverter needed; only LambdaID strings cross history; 'fix a
.star bug' handled by Temporal's Worker Versioning mechanism (drain old
workers gradually)."*

**The mechanism.** At worker boot, `pkg/worker::bootRegistry` walks the
filesystem under `--rootdir`, sorts paths for cross-platform determinism,
hashes every `.star` file, and registers each `(flow_name, content_hash) →
*ParsedFlow` entry into a `FlowRegistry`. The registry is **frozen after
boot** — no runtime mutation. See `pkg/interpreter/registry.go`.

**The wire format.** `WorkflowInput` carries `{FlowName, ContentHash,
InitState}`. The interpreter looks up `(FlowName, ContentHash) → *ParsedFlow`
on every workflow start; lambda IDs are resolved against
`ParsedFlow.Lambdas` on every per-step evaluation via
`bridge.CallLambda(captured.Fn, state, opts)`. Only `LambdaID` strings ever
appear in Temporal history.

**Versioning is operational.** Running workflows complete on the original
code; new starts use updated code. The mechanism is Temporal's Worker
Versioning (Build IDs): production deployers register a Build ID
compatibility set on the task queue and run a fresh worker pool against the
new Build ID. In-flight workflows continue draining against the old Build ID.
The `WorkerOptions.UseBuildIDVersioning` flag is **opt-in** (default false)
so dev / CLI runs against `skytime dev-server` work without any
task-queue-versioning ceremony.

**Why Option B and not a custom DataConverter.** A custom DataConverter
would have to either (a) serialize the `*starlark.Function`'s compiled
bytecode (which is unexported, not JSON-friendly, and tightly coupled to
the `go.starlark.net` patch level), or (b) round-trip through source text,
which is what Option B already does — except Option B does it at boot
once per worker, not on every history event. The frozen-after-boot
registry trades a slightly richer worker startup for a clean
no-DataConverter Temporal client. Replay determinism is verified by the
`pkg/interpreter::TestReplayDeterminism` test that runs the same workflow
twice and asserts byte-identical history.

## The Three "No" Rules

These three rules are stated negatively because they are easier to violate
than to uphold. Each rule has a concrete code-level enforcement and a
clear architectural rationale.

### No String Compilation

> *No string compilation (no CEL, no string parsers for conditionals/data
> mapping) — only native Starlark lambdas.* (PROJECT.md, "Strict directives
> from the spec")

The carve-out — verbatim from PROJECT.md — is bounded:

> *Parser-time syntactic sugar that desugars to native Starlark lambdas
> (e.g., `${ctx.expr}` → `lambda ctx: str(ctx.expr)`) is not string
> compilation. The runtime evaluation surface remains lambda-only. This
> carve-out exists for ergonomic step naming and string kwargs; runtime
> template engines (CEL, Jinja, etc.) remain forbidden. Extending this
> carve-out beyond parser-time desugaring requires a new ADR.* (Phase 04.1,
> D4.1-22)

**Why the rule.** String parsers re-introduce the parse-time evaluation
surface that lambdas eliminate. A CEL expression, a Jinja template, a
`{{ ... }}` template-substitution mini-language — each is a separate
parser, a separate AST, a separate set of bugs, a separate security
surface. Skytime declines all of them. The only string-shaped
ergonomic-ism is `${ctx.expr}` interpolation in string kwargs, which
desugars **at parse time** into a real lambda via
`pkg/parser/builtins.go::desugarInterpolation`. The runtime sees a
`*StarlarkLambda` like any other; the interpolation engine is the same
`bridge.CallLambda` everything else uses.

### No Dynamic Activities

Extensions are plain Go functions; they never import
`go.temporal.io/sdk/activity`. The firewall is enforced by an
AST-walking test in `pkg/activity/firewall_test.go::
TestNoTemporalImportsInExtensionPackage`, plus a non-vacuous
inverse meta-test that proves the firewall would catch a violation if
one were introduced.

**Why the rule.** Dynamic activity registration would couple extensions
to Temporal's lifecycle (registration order, worker boot timing, retry
configuration), break the single-generic-activity batching model
(`pkg/activity::ExecuteBatch` is the **one** activity, see ACT-01), and
prevent plain-Go testing of extensions (extensions today run inside
plain `go test`, no Temporal worker required). Crossing the firewall
would cascade: activities couple to workflows, workflows couple to
clients, and a "simple" extension change requires a Temporal cluster
to test.

The firewall's allow-list is `{activity, interpreter, worker}` — three
packages own the Temporal SDK surface. Every other `pkg/*` directory
is forbidden from importing it. The CLI firewall
(`tests/firewall_cli_test.go`) extends this to `cobra`, `pflag`,
`charm.land/log/v2`, `charm.land/lipgloss/v2` — reachable only from
`{cmd/skytime, pkg/cli}`.

### No Context Bleed

Never pass `workflow.Context` into a Starlark thread; never pass a
Starlark `*starlark.Thread` over the network into an activity. The
enforcement lives in `pkg/bridge::CallLambda`, which constructs a
**fresh** `*starlark.Thread` on every invocation (Pitfall #1: never
reuse threads), and in `pkg/interpreter/cancel_watchdog.go`, which
bridges `workflow.Context.Done()` to a native `chan struct{}` via a
`workflow.Go` reader with a `sync.Once`-wrapped close (D3-21).

**Why the rule.** Any path that lets `workflow.Context` leak into
Starlark breaks the workflowcheck contract (no native goroutines,
no `time.*`, no `rand.*` inside workflow code), because Starlark has
no way at execution-time to advertise that contract to the Go-side
`vet`-style checker.
Conversely, letting a `*starlark.Thread` cross the activity boundary
would mean the lambda's per-call mutable state (resolved free
variables, exec-step counter, cancel hook) becomes a shared resource
between workflow and activity goroutines — an instant
non-determinism bug. The bridge's discipline is: **lambdas evaluate
inside the workflow goroutine, on a fresh thread per call, with a
bridged cancel channel and no `workflow.Context` reference**.

## Two-Tier Authoring Model

Library/extension developers and workflow authors live on opposite sides
of the parse/execute boundary. The directory tree is the audience map:
`pkg/extension` is the Go authoring surface; `examples/skeleton/*.star`
are the Starlark authoring surface; the boundary diagram above is the
single illustration of how the two surfaces meet.

**Library/extension developers (Go).** Author packages under
`pkg/extension/builtin/<name>` (the bundled HTTP extension lives at
`pkg/extension/builtin/http`). An extension exports
`Extension.Initialize` returning a `*starlarkstruct.Module` and registers
operation specs whose `Func` is a plain Go function — no Temporal
imports, no activity coupling, no global state. Operations return
`*dag.ActionRef` intents (the Command Pattern) that the activity layer
later dispatches via `ExecuteBatch`. Extensions are reusable across
customers.

**Consultant / integrator teams (Starlark).** Author `.star` files
that compose registered extensions into customer-specific flows.
Compose with `flow(...)`, `step(...)`, `script(...)`, `if_cond(...)`,
`for_each_parallel(...)`, and `call_flow(...)`. Use `${ctx.expr}` in
string kwargs, lambdas in `cond=`, `fn=`, `action_fn=`, and `block_fn=`.
Validate with `skytime validate`, run with `skytime run`, inspect with
`skytime info`.

The Go layer ships once and is reusable across customer engagements;
the Starlark layer is per-customer and lives in the customer's
repository (or a per-customer directory of the consulting firm's
mono-repo). A two-tier model in one library; the boundary IS the
audience split.

## Where to Go Next

- Flow authors → [docs/for-flow-authors/README.md](for-flow-authors/README.md) (audience landing — created in Phase 04.3 plan 09)
- Extension developers → [docs/for-extension-developers/README.md](for-extension-developers/README.md) (audience landing — created in Phase 04.3 plan 09)
- Builtin reference (auto-generated from source) → [docs/reference/builtins.md](reference/builtins.md)
- Getting started tutorial → [docs/getting-started.md](getting-started.md)
- CLI reference → [docs/cli-binary.md](cli-binary.md)

If you only have time for one more page, read the builtin reference at
[docs/reference/builtins.md](reference/builtins.md) — it lists every parse-time
primitive (`flow`, `step`, `script`, `if_cond`, `for_each_parallel`,
`call_flow`, `result`, `fail`) with its kwargs, types, and examples,
generated directly from the same `pkg/parser` AST this document cites.
