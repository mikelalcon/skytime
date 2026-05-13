# Skytime

> Starlark-defined durable workflows on Temporal — a strict parse/execute
> split that gives flow authors a safe DSL on top of Temporal's
> retry/timeout/child-workflow guarantees.

Skytime is a Go library for declaring durable workflows in Starlark
and executing them on Temporal. A `.star` file is parsed into a
deterministic DAG; Temporal walks that DAG at execution time. The two
phases share no runtime — no string compilation, no dynamic activities,
no context bleed.

The two-tier authoring model:

- **Library/extension developers** write Go *extensions* — typed I/O wrappers, reusable across customers
- **Flow authors** compose those extensions in `.star` files specialized per customer

A flow-author team can take an extension catalog and a customer brief,
write a `.star` file, and have a production-grade durable workflow
running on Temporal — without touching Go and without giving up
Temporal's retry/timeout/child-workflow guarantees.

## What This Is

- Six naked DSL primitives (`flow`, `step`, `if_cond`, `script`, `for_each_parallel`, `call_flow`) with parse-time DAG generation
- Lambda capture with content-hash IDs; lambdas evaluate inside the workflow against state injected as nested struct types
- Single generic Temporal activity (`ExecuteBatch`) batches idempotent extension I/O; non-idempotent ops execute one-per-invocation
- Just-in-time credential resolution — secrets never enter workflow state
- Static validation tier (`skytime validate`) sharing the parser with the runtime; CI corpus differential test pins parse + dryrun agreement
- E2E test harness (`skytime test`) — Tier-3 `temporal_test` mocks the single generic activity inside `testsuite.TestWorkflowEnvironment`, runs every test twice for replay-determinism. See the [Testing Tutorial](docs/for-flow-authors/testing-tutorial.md).
- Embedded HTTP extension; example GitHub + Webhook extensions in `examples/http-github-webhook/`; bring-your-own for everything else
- Bazel-style colored CLI: `skytime run --flow=... --input=...` streams progress live; multi-line block on TTY, static line-per-event under `--verbose`
- Expression-mode `if_cond` with strict-equality result-binding (Phase 04.2 — the most distinctive parse-time type system feature)
- Long-lived `skytime server` (Phase 7): drain-on-SIGTERM Temporal worker + HTTP webhook receiver + cron triggers via Temporal Schedules + a stdlib-only teaching dashboard at `GET /` with live workflow list, recent webhook deliveries, and a manual trigger form — see [`docs/walkthroughs/dashboard.md`](docs/walkthroughs/dashboard.md)

## What This Is Not

- Not a hosted SaaS. Skytime is a Go library; deployment is your problem.
- Not a CEL/JSONPath/Jinja replacement. Lambdas only — runtime template engines are explicitly forbidden (parser-time syntactic sugar like `${ctx.expr}` is permitted, see [docs/architecture.md](docs/architecture.md#no-string-compilation)).
- Not a Temporal alternative. Skytime sits ON TOP of Temporal; you still need a Temporal cluster (cloud or self-hosted; `skytime dev-temporal` for local).
- Not a workflow versioning helper. Use Temporal's Build IDs + content-addressed flows; Skytime-specific versioning is out of scope for v1.
- Not a production observability UI. A minimal teaching dashboard ships with `skytime server` so the durability story is visually demoable (workflow list + webhook deliveries + manual trigger; stdlib `net/http` + `html/template` only); production observability lives in the Temporal Web UI.
- Not Starlark-the-language with full Python compatibility — `lambda` and `def` are usable; some stdlib (time, random, I/O) is NOT in the lambda-time predeclared environment.
- Not Tier 2 (Starlark unit tests) in v1. Tier 1 (static validation) and Tier 3 (E2E with `temporal_test`) ship in v1; pure-Starlark unit testing of `def` blocks moves to v2.

## Status

Pre-v1; foundational architecture stable. Phase progress:

- ✓ **Phase 1** — Type spine, extension contract, parser/bridge foundations (22/22 reqs)
- ✓ **Phase 2** — Generic activity, block-batch dispatch, credentials (6/6 reqs)
- ✓ **Phase 3** — Lambda-serialization decision (Option B: re-parse on workflow start), interpreter, worker (10/10 reqs)
- ✓ **Phase 4** — Static validation, CLI skeleton (`skytime run`/`validate`/`info`/`dev-temporal`), HTTP extension (7/7 reqs)
- ✓ **Phase 04.1** — Dynamic step kwargs, `${ctx.expr}` interpolation, multi-line live progress (7/7 reqs)
- ✓ **Phase 04.2** — Expression-mode `if_cond` with strict-equality result-binding (3/3 reqs)
- ✓ **Phase 04.3** — Documentation set + source-driven reference generator
- ✓ **Phase 5** — Tier-3 E2E test harness (`temporal_test` Starlark builtin) (5/5 reqs)
- ✓ **Phase 6** — Example project: HTTP + GitHub + Webhook extensions, five `.star` flows, Tier-3 test, credfile resolver, README walkthrough, CI (4/4 reqs)

**v1 readiness:** 64/64 v1 requirements complete
(see [.planning/REQUIREMENTS.md](.planning/REQUIREMENTS.md)).

**Production-readiness:** the runtime path has full test coverage
including replay-determinism assertions and `workflowcheck` analysis.
Use it for non-critical workloads today; audit the credential-resolver
wiring in your CLI binary before production rollout.

## Getting Started

> **Canonical version:** [`docs/getting-started.md`](docs/getting-started.md).
> The same content is embedded below for readers evaluating the
> project; the canonical file may diverge slightly between releases
> (run `git diff docs/getting-started.md README.md` to check).

Skytime is a Go library + CLI that lets you declare durable Temporal
workflows in Starlark. This tutorial walks you from `git clone` to a
successfully-executed workflow in about 5–10 minutes.

By the end you'll have:

- The `skytime` binary built and running
- A local Temporal dev server humming
- One workflow run end-to-end with both a happy path AND a deliberately
  failing path so you see how errors surface
- A working understanding of the four DSL features the canonical
  example exercises: `${ctx.expr}` interpolation, expression-mode
  `if_cond`, `result()` binding, and top-level `fail()`

The example we'll run is
[`examples/skeleton/expression_if.star`](examples/skeleton/expression_if.star),
specifically the `check_user` flow — a handful of lines of Starlark
that demonstrate the project's most distinctive features in one place.

## Prerequisites

| Requirement | Version | Install Hint |
|-------------|---------|--------------|
| Go | **1.25+** | <https://go.dev/dl/> — Skytime's `go.mod` declares `go 1.25` (the Starlark interpreter requires it) |
| `temporal` CLI | latest | <https://docs.temporal.io/cli> — `brew install temporal` (macOS) or follow the install guide for your OS |
| A terminal | — | bash, zsh, fish all fine |

You do **not** need to install Temporal Server separately.
`skytime dev-temporal` wraps `temporal server start-dev` (in-memory,
single binary) — that's enough for this tutorial.

Verify your prereqs:

```sh
go version           # go version go1.25.x or newer
temporal --version   # temporal version vX.Y.Z
```

If `temporal --version` errors, jump back to the install hint above —
the dev-temporal step won't work without it.

## 1. Clone and Build

```sh
git clone https://github.com/mikelalcon/skytime.git
cd skytime
go build -o skytime ./cmd/skytime
```

The build drops a `./skytime` binary in the repo root. You can either
put it on `PATH` (e.g. `mv skytime /usr/local/bin/`) or run it via the
relative path `./skytime` for the rest of this tutorial. Quick sanity
check:

```sh
./skytime --help
```

You should see the cobra usage with subcommands `run`, `validate`,
`info`, `dev-temporal`. If not, double-check Go is on `PATH` and re-run
`go build`.

## 2. Start the Dev Server

Open a fresh terminal and run:

```sh
./skytime dev-temporal
```

This wraps `temporal server start-dev` — Temporal's in-memory dev
cluster. You'll see lines roughly like:

```
Temporal server is starting...
Connecting to localhost:7233 ...
CLI 1.x.y (Server 1.x.y, UI 2.x.y)
Server: localhost:7233
UI:     http://localhost:8233
```

Leave this terminal open. The server runs in the foreground; press
Ctrl-C when you're done. Skytime forwards SIGINT/SIGTERM to the
wrapped subprocess for a clean shutdown. All other commands run in a
*separate* terminal.

> If you see `error: ` + an install block, the `temporal` CLI isn't
> on `PATH` — fix that and re-run.

## 3. Discover the Flows

In a second terminal (the first is occupied by the dev server), list
the flows declared in our example:

```sh
./skytime info examples/skeleton/expression_if.star
```

You'll see a bordered table approximately like:

```
╭────────────────────┬──────────────────────────────────────────────────────────────────────┬──────────────────╮
│ Flow               │ Description                                                          │ Inputs           │
├────────────────────┼──────────────────────────────────────────────────────────────────────┼──────────────────┤
│ procedural_demo    │ Procedural-mode if_cond — branches on script output, fail() in else  │ repo:string      │
│ classify_repo_size │ Expression-mode if_cond binding to classification — both branches…   │ size_bytes:int   │
│ check_user         │ Asymmetric expression-mode — then binds user, else fails with…       │ user_id:string   │
╰────────────────────┴──────────────────────────────────────────────────────────────────────┴──────────────────╯
```

Three flows. We'll run **`check_user`** — it shows expression-mode
`if_cond` with asymmetric branches (one branch binds a value, the
other terminates with `fail()`), plus a downstream step that consumes
the bound value.

> `skytime info` is the runtime equivalent of the auto-generated
> reference: it parses the file (no Temporal connection needed) and
> shows you the public surface a `.star` file exposes.

## 4. Run the Happy Path

```sh
./skytime run examples/skeleton/expression_if.star \
    --flow check_user --input '{"user_id":"octocat"}'
```

Expected output (approximate — colors, ms timings, and exact wording
vary; the renderer prints a Bazel-style step list with `[skytime]`
banners and `[N/M]` counters):

```
[skytime] flow_start check_user
[skytime] [1/2] if_cond ▶ check
[skytime]       result_bound → ctx.user
[skytime] [2/2] step Fetch user octocat ▶ http.get /users/octocat
[skytime]       step_complete (status 200) ✓
[skytime] flow_complete check_user ✓
```

Two events are worth lingering on:

- `result_bound → ctx.user` — the `if_cond`'s then-branch evaluated
  `result(value = {"id": ctx.user_id, "ok": True})`; the resulting
  dict is now visible at `ctx.user` for the downstream step.
- `Fetch user octocat` — the step's `name` kwarg is
  `"Fetch user ${ctx.user.id}"`. The `${ctx.user.id}` interpolation
  desugars at parse time and resolves at workflow-execute time; the
  renderer shows the resolved label.

The final stdout line is the workflow's JSON result. (`run.Get` writes
the result to stdout while progress events render to stderr — pipe
stdout through `jq` if you want to inspect it.)

## 5. Run the Fail Path

Now run the same flow with an empty `user_id`:

```sh
./skytime run examples/skeleton/expression_if.star \
    --flow check_user --input '{"user_id":""}'
```

Expected output:

```
[skytime] flow_start check_user
[skytime] [1/2] if_cond ▶ check
[skytime] flow failed step 1/2 (invalid user_id: '') ✗
```

The else-branch ran `fail("invalid user_id: '${ctx.user_id}'")`. The
`${ctx.user_id}` interpolation resolved to the empty string at
workflow-execute time. The error surfaces as a Temporal
`NonRetryableApplicationError`; the renderer prints it with a red
marker, and `skytime run` exits non-zero (1).

Stop the dev server (Ctrl-C in terminal 1) when you're done
experimenting.

## What Just Happened?

Four DSL features carried that one flow. Here's what each does and
why it's worth understanding before you write your own.

### `${ctx.expr}` Interpolation

Quoted from the example:

```python
fail("invalid user_id: '${ctx.user_id}'")
```

At parse time, the `${ctx.user_id}` marker desugars into a synthesized
lambda equivalent to
`lambda ctx: "invalid user_id: '" + str(ctx.user_id) + "'"`. The lambda
evaluates inside the workflow at fail-time, so the message reflects
the actual runtime `user_id`. Doubled `$$` is the literal-dollar
escape; `${}` (empty) and multi-line `${...}` are rejected at parse.
Importantly this is **parse-time syntactic sugar**, not runtime
template evaluation — see PROJECT.md's D4.1-22 carve-out for the
full rationale (the runtime evaluation surface stays lambda-only;
template engines like CEL and Jinja remain forbidden).

### Expression-Mode `if_cond`

Quoted from the example:

```python
if_cond(
    output_alias = "user",
    cond = lambda ctx: ctx.user_id != "",
    then = [result(value = {"id": ctx.user_id, "ok": True})],
    else_ = [fail("invalid user_id: '${ctx.user_id}'")],
)
```

When `output_alias` is non-empty, `if_cond` becomes a value-producing
expression. Each branch must end in `result()` (binds a value) or
`fail()` (terminates the workflow). The bound value lands at
`ctx.<alias>` for downstream steps. Asymmetric shape is supported:
one branch binds, the other terminates — which is exactly what
`check_user` does. Procedural-mode `if_cond` (no `output_alias`) is
unchanged and keeps working — see flow `procedural_demo` in the same
file for that. Locked by DSL-14 + D4.2-09.

### `result()` Binding to ctx

Quoted from the example:

```python
then = [result(value = {"id": ctx.user_id, "ok": True})],
```

Each value in the dict-literal is captured as a parse-time lambda; at
workflow-execute time, the lambdas evaluate against the live `ctx`
(so `ctx.user_id` resolves to the input you passed via `--input`).
The resulting dict is bound to `ctx.user` (per `output_alias`).
Downstream steps read it as `ctx.user.id`, `ctx.user.ok`, etc.
Strict structural type-equality is checked across branches at parse
time — both branches' result dicts must have the same keys and types
(no LUB / subtype rules; explicit `float(x)`, `int(x)`, `str(x)` casts
handle widening). Locked by DSL-15 + D4.2-13 (typed state schema).

### `fail()` with Interpolation

Quoted from the example:

```python
else_ = [fail("invalid user_id: '${ctx.user_id}'")],
```

Top-level `fail()` is a parse-time builtin that emits a workflow-failure
node into the DAG. At runtime it raises a Temporal
`NonRetryableApplicationError` carrying the interpolated message,
visible in the renderer's "flow failed" line. This is *distinct* from
the lambda-time `fail()` global available inside lambda bodies
(used inside `script(fn=...)` for instance) — see `pkg/parser/doc.go`
for the dual `fail()` reference. The carve-out in PROJECT.md's
D4.2-05 explains the interaction: top-level `fail()` is a parse-time
syntactic primitive that emits a node; the MessageFn lambda (when
`${ctx.expr}` interpolation is present) evaluates per the standard
`CapturedLambda` + `bridge.CallLambda` path. No new runtime evaluation
surface beyond the lambda contract.

## Where to Go Next

Where to go next depends on what you want to do:

- **Understand the design** —
  [`docs/architecture.md`](docs/architecture.md) explains the
  parse/execute split (the project's foundational design choice) with
  an ASCII boundary diagram. Required reading for both audiences.
- **Look up DSL syntax** —
  [`docs/reference/builtins.md`](docs/reference/builtins.md) is the
  auto-generated reference for every Starlark builtin (`flow`, `step`,
  `script`, `if_cond`, `result`, `fail`, `for_each_parallel`,
  `call_flow`).
- **CLI reference** — [`docs/reference/cli.md`](docs/reference/cli.md)
  documents `skytime run` connection variants, `skytime validate`
  exit codes, `skytime info`, `skytime dev-temporal`, and more.
- **Browse more examples** —
  [`examples/README.md`](examples/README.md) indexes every
  skeleton fixture (`simple_check.star`, `parallel_fanout.star`,
  `expression_if.star`) with a one-line summary and a "what features
  it exercises" pointer.
- **Try the rich example project** —
  [`examples/http-github-webhook/README.md`](examples/http-github-webhook/README.md)
  walks through the dogfooding example: three extensions
  (HTTP + GitHub + Webhook), five `.star` flows covering every DSL
  primitive, a Tier-3 test, and the canonical `cmd/extbin` custom
  binary. From `git clone` to a real workflow run in under five commands.
- **See the durability story in a browser** —
  [`docs/walkthroughs/dashboard.md`](docs/walkthroughs/dashboard.md)
  spins up `skytime server`, fires a webhook, triggers a workflow
  manually from the dashboard form, and walks a crash-recovery demo
  with live updates over Server-Sent Events. Stdlib only — no JS
  framework, no external CSS, no bundler.
- **Write a flow yourself** —
  [`docs/for-flow-authors/README.md`](docs/for-flow-authors/README.md)
  is the landing page for flow authors composing extensions.
  Start with the bundled HTTP extension reference at
  [`docs/for-flow-authors/extensions/http.md`](docs/for-flow-authors/extensions/http.md).
- **Write tests for your flows** — Skytime ships a Tier-3 E2E test
  harness (`skytime test <dir>`). The
  [Testing Tutorial](docs/for-flow-authors/testing-tutorial.md) walks
  through building a test suite for a GitHub-API flow from scratch
  (file-scope mocks → per-test override → retry semantics → JSON
  output → regex filter). For the full surface, see the
  [Testing Reference](docs/for-flow-authors/testing.md) — every
  `tester.*` builtin, `assert.*` family, mock precedence rules, and
  v1 limitations.
- **Build an extension** —
  [`docs/for-extension-developers/README.md`](docs/for-extension-developers/README.md)
  is the landing page for Go developers writing extensions.
- **Build a custom CLI** —
  [`docs/cli-binary.md`](docs/cli-binary.md) shows how to register your
  own extensions via `cli.NewRootCommand(WithExtensions(...))`.

## Project Layout

```
skytime/
├── cmd/
│   ├── skytime/            # the CLI binary (cmd/skytime/main.go is a thin wrapper around pkg/cli)
│   └── skytime-docgen/     # AST-driven generator for docs/reference/builtins.md
├── pkg/
│   ├── parser/             # Starlark → dag.Flow parser (D-09: pure function of file bytes)
│   ├── bridge/             # *starlarkstruct.Struct construction + CallLambda
│   ├── dag/                # Pure data types (Flow, Step, IfCond, Script, ForEachParallel, CallFlow, Result, Fail, ActionRef)
│   ├── extension/          # Extension SDK + Credential types + per-parser registry
│   │   └── builtin/http/   # Bundled HTTP extension (5 ops; D4-14 idempotence)
│   ├── activity/           # Single generic ExecuteBatch activity (Temporal-specific code lives here)
│   ├── interpreter/        # SkytimeWorkflow — the generic interpreter
│   ├── worker/             # Worker bootstrap (3 named client constructors)
│   ├── validator/          # Thin facade for static validation; AlwaysOkDispatch dryrun
│   └── cli/                # Reusable cobra root (NewRootCommand + WithExtensions/WithCredentialHandler)
├── examples/skeleton/      # 3 hand-curated .star fixtures (CI-tested via tests/differential_test.go)
├── docs/                   # Hand-written narrative + auto-gen reference
│   ├── architecture.md     # Required reading: parse/execute split + boundary diagram
│   ├── getting-started.md  # The canonical 5-10 min tutorial
│   ├── reference/          # docs/reference/builtins.md is auto-generated (cmd/skytime-docgen)
│   ├── for-flow-authors/   # Flow authors
│   ├── for-extension-developers/  # Go developers
│   └── cli-binary.md       # Building binaries with custom extensions
└── tests/                  # Cross-package firewalls + corpus differential tests
```

## Contributing

Skytime is a single-author project right now. Issues and PRs welcome.

Run the full test suite before opening a PR:

```sh
go build ./...
go vet ./...
go test -race ./...
go generate ./pkg/parser/  # refresh docs/reference/builtins.md if you touched a builtin
```

The codebase enforces a few firewalls via tests under
[`tests/`](tests/):

- `tests/firewall_cli_test.go` — only `pkg/cli` and `cmd/skytime` may import cobra/charm-log/lipgloss
- `pkg/activity/firewall_test.go` — only `pkg/activity`, `pkg/interpreter`, `pkg/worker`, `pkg/cli` may import `go.temporal.io/sdk/...`
- `tests/differential_test.go` — every `examples/skeleton/*.star` runs through both static `validator.Validate` AND a dry-run interpreter; drift fails CI
- `tests/docgen_drift_test.go` — `docs/reference/builtins.md` must match a fresh `go generate ./pkg/parser/` run; drift fails CI

Adding a new DSL builtin? Add a `// skytime:doc summary="..."`
block above the `builtinXxx` function in `pkg/parser/builtins.go`
and run `go generate ./pkg/parser/`. The reference doc updates
automatically.

