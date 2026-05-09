# Getting Started with Skytime

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
[`examples/skeleton/expression_if.star`](../examples/skeleton/expression_if.star),
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

## Triggering flows from real events

The basic walkthrough above invoked your flow via `skytime run`. To
trigger flows from external events (GitHub webhooks, cron schedules,
etc.), see the long-form walkthrough at
[`docs/walkthroughs/github-webhook.md`](walkthroughs/github-webhook.md).
It demonstrates Temporal's durability story end-to-end: kill the
worker between activities, restart, watch the workflow continue from
event history.

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

## Next Steps

You've now seen Skytime end-to-end. Where to go next depends on what
you want to do:

- **Understand the design** —
  [`docs/architecture.md`](architecture.md) explains the parse/execute
  split (the project's foundational design choice) with an ASCII
  boundary diagram. Required reading for both audiences.
- **Look up DSL syntax** —
  [`docs/reference/builtins.md`](reference/builtins.md) is the
  auto-generated reference for every Starlark builtin (`flow`, `step`,
  `script`, `if_cond`, `result`, `fail`, `for_each_parallel`,
  `call_flow`).
- **CLI reference** — [`docs/reference/cli.md`](reference/cli.md)
  documents `skytime run` connection variants, `skytime validate`
  exit codes, `skytime info`, `skytime dev-temporal`, and more.
- **Browse more examples** —
  [`examples/README.md`](../examples/README.md) indexes every
  skeleton fixture (`simple_check.star`, `parallel_fanout.star`,
  `expression_if.star`) with a one-line summary and a "what features
  it exercises" pointer.
- **Write a flow yourself** —
  [`docs/for-flow-authors/README.md`](for-flow-authors/README.md) is
  the landing page for flow authors composing extensions.
- **Build an extension** —
  [`docs/for-extension-developers/README.md`](for-extension-developers/README.md)
  is the landing page for Go developers writing extensions.
- **Build a custom CLI** —
  [`docs/cli-binary.md`](cli-binary.md) shows how to register your own
  extensions via `cli.NewRootCommand(WithExtensions(...))`.
