# Skytime Examples

The `examples/skeleton/` directory contains hand-curated `.star` fixtures
that exercise every shipped Skytime DSL feature. They serve three
purposes:

1. **Tutorial material** — readers run them to see Skytime work
   end-to-end against a local Temporal dev server.
2. **Regression coverage** — `tests/differential_test.go` runs every
   `.star` here through both the static validator AND a dry-run
   interpreter; drift fails CI (D4-07, VAL-02).
3. **Reference snippets** — copy-paste-modify starting points for real
   production flows.

Each fixture is small (≤30 LOC of declarative code) and focused on a
single concept. Read the files in this order if you're learning the DSL:

1. [`simple_check.star`](skeleton/simple_check.star) — sequential flow
   with a procedural `if_cond`
2. [`parallel_fanout.star`](skeleton/parallel_fanout.star) — parallel
   fan-out + dynamic batching + `call_flow`
3. [`expression_if.star`](skeleton/expression_if.star) — expression-mode
   `if_cond` + `result()` binding (canonical tutorial)

## How to Run Any Example

All examples run against a local Temporal dev server. One-time setup:

```sh
# Terminal 1: start the dev server (requires `temporal` CLI on PATH)
skytime dev-temporal
```

Then in a second terminal, run any example:

```sh
# Terminal 2:
skytime run examples/skeleton/<file>.star --flow <flow-name> --input '<json>'
```

Every flow declares an `inputs={"name": "type"}` schema; supply matching
JSON via `--input`. Run [`skytime info <file>.star`](../docs/reference/cli.md#skytime-info)
to see what flows + inputs each file declares without opening the
source.

Don't have the `temporal` CLI? See
[docs/reference/cli.md#skytime-dev-temporal](../docs/reference/cli.md#skytime-dev-temporal)
for install instructions, or run examples through your own Temporal
Cloud or self-hosted cluster (see
[docs/reference/cli.md#skytime-run](../docs/reference/cli.md#skytime-run)
for connection variants — the same `.star` files run unchanged against
all three).

## simple_check.star

Sequential flow with procedural `if_cond` + `${ctx.expr}` interpolation
against a single repo input.

### What It Demonstrates

- `flow(name, inputs, steps)` — DSL-01
- `step(name, action)` — DSL-02
- `script(id, fn, output_alias)` — DSL-05
- `if_cond(cond, then, else_)` (procedural mode) — DSL-04
- `${ctx.repo}` parse-time interpolation in step `name` and action
  kwargs — DSL-13
- `http.endpoint(...) + http.get(...)` — bundled HTTP extension

### How to Run

```sh
skytime run examples/skeleton/simple_check.star \
    --flow simple_check --input '{"repo":"octocat/Hello-World"}'
```

### Expected Output

A multi-line live progress block:
`flow_start` → `step_dispatch` (Get repo octocat/Hello-World) →
`step_complete (status 200)` → `step_dispatch` (script: extract_status) →
`step_complete` → `branch: then` → `step_dispatch` (Get branches for
octocat/Hello-World) → `step_complete (status 200)` →
`flow_complete`. Each `step_dispatch` row interpolates the repo name
because the step `name` kwarg uses `${ctx.repo}`.

### See Also

- [`docs/reference/builtins.md`](../docs/reference/builtins.md) for
  `flow`, `step`, `script`, `if_cond` syntax
- [`docs/for-flow-authors/extensions/http.md`](../docs/for-flow-authors/extensions/http.md)
  for HTTP-extension semantics (D4-14 idempotence policy: `get`/`head`
  are idempotent)
- [`docs/architecture.md`](../docs/architecture.md) — parse/execute
  split + lambda capture model

## parallel_fanout.star

Parallel fan-out over a runtime-built list of repos, each repo
expanding into a homogeneous idempotent block batch via `block_fn`.
Two flows in one file: a helper `check_one` and the parent
`parallel_fanout` that fans it out.

### What It Demonstrates

- `for_each_parallel(items, item, steps)` — DSL-06; `items` is a
  lambda over a runtime list
- `step(block_fn=lambda ctx: [...])` — DSL-12; runtime-built
  homogeneous block batch (all `gh.get` → idempotent=true, accepted by
  the parser's best-effort static analysis per D4.1-11)
- `call_flow(name, inputs)` — invokes a Temporal child workflow per
  iteration
- `${ctx.repo}` interpolation in step names; runtime concatenation
  (`"/repos/" + ctx.repo`) inside lambdas

### How to Run

```sh
skytime run examples/skeleton/parallel_fanout.star \
    --flow parallel_fanout \
    --input '{"repos":["octocat/Hello-World","torvalds/linux","golang/go"]}'
```

### Expected Output

Live progress block opens a `▶ for_each_parallel` block scope; per
input repo a `▶ call_flow` child opens, then a `▶ step (block_fn)`
inside `check_one` opens with three indented child rows (one per
`gh.get`); each completes with `status 200`; the block closes with
`✓`; the `for_each_parallel` footer closes with the total elapsed.

### See Also

- [`docs/reference/builtins.md`](../docs/reference/builtins.md) for
  `for_each_parallel`, `step(block_fn=...)`, `call_flow`
- [`docs/for-flow-authors/extensions/http.md`](../docs/for-flow-authors/extensions/http.md)
  — homogeneous-idempotent batch policy (Policy D, D2-05)

## expression_if.star (canonical tutorial)

Three flows demonstrating expression-mode `if_cond` with `result()`
binding, dual `fail()` semantics, and asymmetric branches.

> **This is the canonical tutorial example.** The full step-by-step
> walkthrough is at [`docs/getting-started.md`](../docs/getting-started.md).
> The entry below is the index summary.

### What It Demonstrates

Three flows in one file:

1. **`procedural_demo`** — procedural-mode `if_cond` preserved verbatim
   from Phase 4 + 04.1 (Phase 04.2 is additive, opt-in). Demonstrates
   that `fail()` inside a procedural-mode branch is allowed (D4.2-07).
2. **`classify_repo_size`** — expression-mode `if_cond` where BOTH
   branches end in `result(value={...})` with strict-equality keys and
   types (DSL-14, VAL-05); a downstream `script` reads
   `ctx.classification` to prove typed-state propagation (D4.2-13).
3. **`check_user`** — asymmetric expression-mode: then-branch
   `result()`, else-branch top-level
   `fail("invalid user_id: '${ctx.user_id}'")` with `${ctx.expr}`
   interpolation (DSL-15, D4.2-05).

Features exercised: `if_cond(output_alias=...)`,
`result(value=dict-literal)`, top-level `fail("msg")` with
`${ctx.expr}` interpolation, downstream `ctx.<alias>` access through
the typed state schema.

### How to Run

Happy path (the canonical demo — also walked through in
[`docs/getting-started.md`](../docs/getting-started.md)):

```sh
skytime run examples/skeleton/expression_if.star \
    --flow check_user --input '{"user_id":"octocat"}'
```

Fail path (interpolated `fail()` message):

```sh
skytime run examples/skeleton/expression_if.star \
    --flow check_user --input '{"user_id":""}'
```

Additional flows in the same file:

```sh
# Procedural-mode if_cond + fail() in else-branch
skytime run examples/skeleton/expression_if.star \
    --flow procedural_demo --input '{"repo":"octocat/Hello-World"}'

# Expression-mode if_cond binding to ctx.classification
skytime run examples/skeleton/expression_if.star \
    --flow classify_repo_size --input '{"size_bytes":2000000}'
```

### Expected Output

**Happy path (`check_user --input '{"user_id":"octocat"}'`):** a
`result_bound` event binds `{"id":"octocat","ok":true}` to `ctx.user`;
the downstream step interpolates `${ctx.user.id}` and performs
`GET /users/octocat`; `flow_complete` with status 200.

**Fail path (`check_user --input '{"user_id":""}'`):** the else-branch
runs `fail()` which raises a `NonRetryableErr` with the interpolated
message; the renderer prints
`[skytime] flow failed step 1/2 (invalid user_id: '')` with a red `✗`
marker.

### See Also

- **Tutorial:** [`docs/getting-started.md`](../docs/getting-started.md)
  — full walkthrough of `expression_if.star --flow check_user`
- **DSL syntax:** [`docs/reference/builtins.md`](../docs/reference/builtins.md)
  for `if_cond`, `result`, `fail`, and the strict-equality rule
- **Why this design:** [`docs/architecture.md`](../docs/architecture.md)
  — parse/execute split, lambda capture, the no-string-compilation
  carve-out for `${ctx.expr}` (D4.1-22)

## Adding a New Example

Drop a new `.star` file into `examples/skeleton/`. Two things happen
automatically:

1. **Differential test pickup** —
   `tests/differential_test.go::TestDifferentialCorpus` walks
   `examples/skeleton/` and runs every `.star` file through both the
   static validator AND a dry-run interpreter (with all extension
   actions mocked via `pkg/validator/dryrun.AlwaysOkDispatch`). Both
   sides must agree on accept/reject (D4-07, VAL-02). Drift fails CI.
   To register a new extension that the corpus uses, append it to
   `corpusExtensions(t)` in `tests/differential_test.go`.

2. **Indexing** — update this README with a section matching the
   template above (What It Demonstrates / How to Run / Expected
   Output / See Also).

If the new fixture's stub-driven dry-run path is expected to raise
`fail()` (top-level, not inside a lambda), add the flow name to
`expectedErrFlows` in `tests/differential_test.go` so the differential
test accepts the deterministic `*temporal.ApplicationError` instead of
asserting `NoError`.

Keep examples small (≤30 LOC declarative). One concept per fixture. If
a fixture exercises multiple primitives, mention each in the "What It
Demonstrates" bullets so readers can find it by feature.

## See Also

- [`docs/getting-started.md`](../docs/getting-started.md) — canonical
  tutorial walking through `expression_if.star`
- [`docs/reference/builtins.md`](../docs/reference/builtins.md) —
  every DSL builtin documented
- [`docs/reference/cli.md`](../docs/reference/cli.md) — `skytime` CLI
  reference (`run`, `validate`, `info`, `server`, `dev-temporal`)
- [`docs/architecture.md`](../docs/architecture.md) — parse/execute
  split, the project's foundational design
- [`docs/for-flow-authors/`](../docs/for-flow-authors/) — landing page
  for flow authors
- [`docs/for-extension-developers/`](../docs/for-extension-developers/)
  — landing page for Go extension developers
