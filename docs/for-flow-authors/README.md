# For Flow Authors

> You write `.star` files specialized per customer. You compose
> extensions, write lambdas, define flows. You don't touch Go (mostly).

If you're a Starlark consultant taking an extension catalog and a
customer brief and turning them into a production-grade durable
workflow on Temporal — this is your entry point. Skytime's whole
reason for existing is to let you do that without leaving Starlark and
without giving up Temporal's retry, timeout, and child-workflow
guarantees.

## Start Here

1. **[Getting Started Tutorial](../getting-started.md)** — 5–10 minute
   walkthrough. The single best entry point: `git clone` → `skytime
   dev-server` → `skytime run examples/skeleton/expression_if.star
   --flow check_user --input '{"user_id":"octocat"}'` → see the colored
   live progress block, then trigger the fail path. You'll know what
   Skytime feels like in under ten minutes.
2. **[Skytime Architecture](../architecture.md)** — required reading
   for both audiences (Flow Authors *and* Extension Developers). It
   explains the parse/execute split and *why* the constraints below
   exist. Skim it once before you start writing real flows; come back
   to it the first time something surprises you.
3. **[Builtins Reference](../reference/builtins.md)** — every DSL
   builtin documented (auto-generated from `pkg/parser/builtins.go` —
   if it diverges from the source, CI catches it). The 8 v1 builtins
   are `flow`, `step`, `if_cond`, `script`, `for_each_parallel`,
   `call_flow`, `result`, `fail`.

## Reference Material

- **[CLI Reference](../reference/cli.md)** — `skytime run`, `validate`,
  `info`, `dev-server`: flag tables, env-var fallbacks, exit codes,
  variant-routing prose for Cloud / self-hosted / dev mode
- **[Examples](../../examples/README.md)** — runnable `.star` fixtures
  (`simple_check`, `parallel_fanout`, `expression_if`) with
  feature checklists, expected output, and the contributor guide for
  adding new ones
- **[Bundled HTTP Extension](extensions/http.md)** — the one extension
  that ships with `skytime` out of the box: `endpoint()`, the 5 ops
  (`get`/`head`/`post`/`put`/`delete`), the D4-14 idempotence policy,
  4xx/5xx classification, JIT credential resolution

## Common Tasks

- **Run a flow against the dev server** — see
  [Getting Started](../getting-started.md). Two terminals: dev-server
  in one, `skytime run` in the other.
- **Validate without running** — `skytime validate <file.star>`
  ([cli ref](../reference/cli.md#skytime-validate)). Catches kwarg
  shape errors, undefined `ctx.<name>` references, mismatched
  `result()` keys/types between `if_cond` branches — all before any
  Temporal traffic.
- **Discover what's in a `.star` file** — `skytime info <file.star>`
  ([cli ref](../reference/cli.md#skytime-info)). Prints a bordered
  table of `Flow | Description | Inputs`, in source-declaration order.
- **Connect to Temporal Cloud / self-hosted** — see
  [skytime run variants](../reference/cli.md#skytime-run). API key →
  Cloud; mTLS cert+key pair → self-hosted; otherwise → embedded dev
  client. Fully driven by flags or `SKYTIME_TEMPORAL_*` env vars.
- **Build a binary with custom extensions** — that's the OTHER tier;
  hand the brief to your Go-side teammate (or read
  [docs/cli-binary.md](../cli-binary.md) yourself if you want to see
  how the wiring looks). The full audience landing for that work is
  [For Extension Developers](../for-extension-developers/README.md).

## What You Cannot Do (and Why)

These constraints are architectural, not stylistic — they're load-bearing
for Temporal's replay guarantees and Skytime's security posture.

- **No string-based expressions** (CEL, Jinja, JSONPath) inside `.star`
  files. There is one carve-out — parser-time `${ctx.expr}` interpolation
  in string kwargs — and that's a syntactic sugar that desugars to a
  Starlark lambda before any evaluation happens. See
  [no string compilation](../architecture.md#no-string-compilation)
  for the full rule and the carve-out's verbatim wording.
- **No native goroutines** in lambdas. The only legal way to spawn
  concurrent work inside a workflow is through Skytime's deterministic
  primitives (`for_each_parallel`, `block_fn` block-batches). See
  [no context bleed](../architecture.md#no-context-bleed).
- **No I/O at parse time.** Your `.star` file is pure data — it builds
  a DAG. All I/O happens through registered extensions, inside the
  single generic activity, at execution time. If you want to talk to
  GitHub, use an HTTP extension; you don't write `requests.get(...)`
  in a lambda.

The
[Skytime Architecture](../architecture.md) page explains why these
rules exist and how they map to the parse/execute split.

## Building Extensions Yourself?

If you need an extension that doesn't exist yet — Slack, an internal
API, AWS, a custom internal service — you'll cross over to the other
tier briefly:

→ [For Extension Developers](../for-extension-developers/README.md)

Many flow-author teams pair with a Go-side teammate who builds
extensions; the consultant team writes the `.star` files. This is the
**two-tier authoring model** in action: extensions are reusable across
customers, flows are specialized per customer, and the parse/execute
boundary keeps the two tiers honest.

If your team only has flow authors, the
[cli-binary.md](../cli-binary.md) walkthrough is short enough to
follow on a quiet afternoon — but it does require Go.
