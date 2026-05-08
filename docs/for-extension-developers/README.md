# For Extension Developers

> You write Go extensions — typed I/O wrappers that wrap real services
> (HTTP, GitHub, Slack, internal APIs). Your work is reusable across
> customers; flow authors compose your extensions in `.star` files.

If you're a Go developer building the catalog of capabilities a flow-author
team will compose into customer-specific flows — this is your entry point. Extensions are plain Go: you implement an interface,
declare a few `OperationSpec`s with typed kwargs, and a single generic
Temporal activity dispatches them. No `workflow.Context` plumbing, no
per-extension activity registration, no string-compilation surface.

## Start Here

1. **[Skytime Architecture](../architecture.md)** — required reading
   for both audiences (Extension Developers *and* Flow Authors). The
   constraints on extensions (no `go.temporal.io/sdk/activity` import,
   no `workflow.Context` access, `Idempotent *bool` required) all come
   from the parse/execute split that page describes. Read it first.
2. **[Extension SDK source](../../pkg/extension/extension.go)** — the
   `Extension` interface (`Name`, `Initialize`, `Operations`) is the
   contract you implement. It's small, well-commented, and the
   single source of truth — read it end-to-end. Sibling files
   `pkg/extension/spec.go` and `pkg/extension/credential.go` define
   `OperationSpec` and the sealed `Credential` interface.
3. **[Bundled HTTP Extension](../../pkg/extension/builtin/http/http.go)**
   — the canonical reference implementation. Five operations
   (`get`/`head`/`post`/`put`/`delete`), an `endpoint()` factory that
   binds `base_url` + `credential`, and the D4-14 idempotence policy.
   Read it end-to-end before writing your own; it's the cleanest
   example of the patterns you'll repeat.

## Reference Material

- **[Custom CLI Guide](../cli-binary.md)** — wiring your extension
  into a standalone `skytime`-compatible binary via
  `cli.NewRootCommand(WithExtensions(...), WithCredentialHandler(...))`.
  Walks through the 30-line `main.go` shape and the credential
  handler contract.
- **[Bundled HTTP Extension Reference](../for-flow-authors/extensions/http.md)**
  — what flow-authors see when they use `http.endpoint(...)`. Useful
  so you understand the consumer perspective before designing your
  own surface; the section structure (Setup → Operations → Idempotence
  → Response Classification → Credentials → Common Patterns) is the
  template a future docgen extension-walker will reproduce.
- **[CLI Reference](../reference/cli.md)** — exit-code semantics,
  Starlark-first error rendering. Your extension's errors flow through
  `pkg/cli/render.go`; understanding the bracketed VAL-03 format and
  the `--debug` Unwrap-chain walk helps you design extension errors
  that render cleanly.
- **[Builtins Reference](../reference/builtins.md)** — the DSL surface
  flow-authors call into your extension from.

## Credential resolution: `pkg/extension/credfile/`

Skytime ships a file-based credential resolver at
[`pkg/extension/credfile/`](../../pkg/extension/credfile/). Use it
from your custom CLI binary via `cli.WithCredentialHandler(credfile.New(...))`.

**Schema** — TOML with explicit `type` tag per credential:

```toml
[credentials.github_token]
type  = "bearer"
token = "ghp_..."

[credentials.basic_id]
type     = "basic"
username = "..."
password = "..."

[credentials.apikey_id]
type  = "apikey"
key   = "X-API-Key"   # header NAME
value = "..."         # header VALUE (the secret)
```

**Default path** — `$HOME/.skytime-credentials`. Override via
`credfile.New(credfile.WithPath("/etc/skytime/credentials.toml"))`.

**Security** — On Linux/macOS, the resolver warns when the file is
world-readable (`mode & 0o044 != 0`) and refuses to load in strict
mode (`credfile.WithStrictMode()`). Always `chmod 600
~/.skytime-credentials` after copying the example template.

**Worked example** — see
[`examples/http-github-webhook/cmd/extbin/main.go`](../../examples/http-github-webhook/cmd/extbin/main.go),
which wires `credfile.New(...)` into a custom CLI binary alongside three
registered extensions.

## Hard Rules (architecture firewall)

The library enforces these via AST-walking firewall tests under
[`tests/`](../../tests/) and
[`pkg/activity/firewall_test.go`](../../pkg/activity/firewall_test.go).
If you violate one, the firewall test fails and the build breaks —
these aren't suggestions.

- **No `import "go.temporal.io/sdk/activity"`** in your extension
  package. Extensions are plain Go; the single generic `ExecuteBatch`
  activity in `pkg/activity` dispatches them. Importing the activity
  package would tempt you to call `activity.GetInfo()` and bleed
  Temporal context into the extension layer.
- **`OperationFunc` takes `context.Context`, not `workflow.Context`**.
  No `workflow.GetInfo`, no `workflow.NewTimer`, no
  `workflow.ExecuteActivity`. Your function runs inside the activity
  stack frame, against a stdlib `context.Context`. Heartbeating and
  cancellation flow through that context.
- **`Idempotent *bool`** is REQUIRED on every `OperationSpec`. It's a
  `*bool` (not `bool`) so you can't forget — `nil` causes
  `parser.Register(...)` to return `ErrIdempotentRequired`. Set it
  truthfully: idempotent operations can be block-batched with other
  idempotent ops; non-idempotent ones execute one-at-a-time. (See
  D4-14 for the bundled HTTP extension's RFC-7231 divergence on
  PUT/DELETE — a deliberate v1 conservatism, not a bug.)
- **`Credential` values come in as parameters, never returned.** The
  `CredentialHandler` resolves the secret JIT inside the activity
  stack frame. Your extension receives a typed `Credential` value
  (Bearer/Basic/APIKey from `pkg/extension/credential.go`); the only
  thing that ever reaches workflow state is the credential ID.
  Workflow state is durable; secrets are not allowed to be durable.

## Building Your First Extension

Mirror the structure of
[`pkg/extension/builtin/http/http.go`](../../pkg/extension/builtin/http/http.go):

1. Define your operation kwargs as Go structs with
   `star:"name,required?"` struct tags. Reflection-driven decoding
   (`pkg/extension/schema.go::DecodeKwargsFromDict`) reads these tags.
2. Implement `Operations() map[string]*OperationSpec`. Each
   `OperationSpec` carries a `Name`, `Idempotent *bool`, `KwargsType`
   (a `reflect.Type` of your kwargs struct), an `OperationFunc`, and
   an optional `DefaultTimeout`.
3. Implement `Initialize(thread, kwargs)` returning a
   `*starlarkstruct.Module` whose attributes are `*starlark.Builtin`s
   that produce `*dag.ActionRef` intents. Factory attributes (like
   HTTP's `endpoint`) close over the credential ID and base URL and
   inject them into the resulting `ActionRef.Kwargs`.
4. Wire into your CLI binary via `cli.WithExtensions(myExt.New())` —
   see [docs/cli-binary.md](../cli-binary.md) for the full
   `main.go` example. If your extension uses credentials, also pass
   `cli.WithCredentialHandler(yourHandler)`.

## Writing Flows Against Your Extension?

If you want to use extensions you (or others) have built, that's the
flow-author tier:

→ [For Flow Authors](../for-flow-authors/README.md)

The two tiers are separated by the parse/execute boundary — your Go
code runs at parse time (registering operations and binding the
`Initialize`-returned module as a Starlark global) and inside the
single generic activity (executing operations against `context.Context`
with the credential resolved JIT). The Starlark side never sees Go
beyond the typed kwargs/output shapes you declare.

This is the **two-tier authoring model** in action: extensions are
reusable across customers (your Go code), flows are specialized per
customer (the flow-author team's `.star` files), and the parse/execute
boundary keeps the two tiers honest.
