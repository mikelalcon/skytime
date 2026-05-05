# HTTP Extension

The bundled HTTP extension at `pkg/extension/builtin/http/` is shipped with the
`skytime` binary out of the box. It uses Go stdlib `net/http` (no third-party
HTTP client), declares 5 operations (`get`, `head`, `post`, `put`, `delete`)
with idempotence policy locked per **D4-14**, and treats 4xx as non-retryable +
5xx as retryable.

This page covers what flow authors need to know. For Go-side extension
authoring (writing your own extension), see
[docs/for-extension-developers/README.md](../../for-extension-developers/README.md).

Source-of-truth: `pkg/extension/builtin/http/http.go`. Verify any behavior
described below by jumping there.

## Setup: the `endpoint()` factory

The entry point is `http.endpoint(...)`. It binds a base URL (and optionally a
credential ID) to a Starlark namespace whose attributes are the 5 operations:

```python
gh = http.endpoint(
    base_url = "https://api.github.com",
    credential = "github_token",  # optional; omit for anonymous endpoints
)

flow(
    name = "fetch_user",
    inputs = {"user_id": "string"},
    steps = [
        step(
            name = "Fetch ${ctx.user_id}",
            action = gh.get(path = "/users/${ctx.user_id}"),
        ),
    ],
)
```

What's happening under the hood:

- `http.endpoint(...)` returns a `*starlarkstruct.Module`.
- Its 5 attributes (`get`, `head`, `post`, `put`, `delete`) are
  `*starlark.Builtin` closures captured at parse time over `base_url` and
  `credential`.
- Each operation invocation produces an `*ActionRef` whose `Kwargs` dict
  embeds `base_url` so the activity-side `OperationFunc` can reconstruct the
  full URL without round-tripping through the closure.

You can declare multiple endpoints in one `.star` file, each with its own
base URL and credential:

```python
gh    = http.endpoint(base_url = "https://api.github.com", credential = "github_token")
slack = http.endpoint(base_url = "https://slack.com/api",  credential = "slack_token")
```

Use `gh.get(...)` for GitHub calls and `slack.post(...)` for Slack — they're
distinct namespaces with distinct credentials.

## Operations

The 5 operations are ordered as they appear in
`pkg/extension/builtin/http/http.go::Operations` (source order — not
alphabetical).

### get

**Idempotent:** **true** (matches RFC-7231)

**Kwargs (`GetArgs`):**

| Name      | Type                | Required | Description                                        |
|-----------|---------------------|----------|----------------------------------------------------|
| `path`    | string              | yes      | Request path appended to the endpoint's `base_url` |
| `headers` | dict[string,string] | no       | Optional request headers                           |

**Example:**

```python
gh.get(path = "/users/${ctx.user_id}")

gh.get(
    path    = "/repos/octocat/Hello-World",
    headers = {"Accept": "application/vnd.github.v3+json"},
)
```

**Notes:** GET responses are batchable; multiple `gh.get(...)` calls inside
`step(block=[...])` execute in a single activity invocation with one heartbeat
per action.

### head

**Idempotent:** **true** (matches RFC-7231)

**Kwargs (`GetArgs`):** identical to `get` — `path` (required), `headers`
(optional). HEAD requests carry no body and the response body is discarded by
the server, so the typed `HTTPResponse.Body` will be empty bytes; only the
status code and headers are meaningful.

**Example:**

```python
gh.head(path = "/repos/${ctx.repo}")
```

**Notes:** Useful for existence checks and cache-validation flows. Like `get`,
batchable inside `step(block=[...])`.

### post

**Idempotent:** **false** (matches RFC-7231)

**Kwargs (`BodyArgs`):**

| Name      | Type                | Required | Description                                        |
|-----------|---------------------|----------|----------------------------------------------------|
| `path`    | string              | yes      | Request path appended to the endpoint's `base_url` |
| `body`    | string              | no       | Request body (raw bytes serialized as a string)    |
| `headers` | dict[string,string] | no       | Optional request headers                           |

**Example:**

```python
gh.post(
    path    = "/repos/${ctx.repo}/issues",
    body    = '{"title": "${ctx.title}", "body": "${ctx.body}"}',
    headers = {"Content-Type": "application/json"},
)
```

**Notes:** POST is non-idempotent, so it cannot be batched alongside idempotent
ops in the same `step(block=[...])` (Policy D, parse-time reject). Use a
sequential `step(action=...)` for each POST.

### put

**Idempotent:** **false** — **diverges from RFC-7231** (which considers PUT
idempotent). See **Idempotence Policy (D4-14)** below for the rationale.

**Kwargs (`BodyArgs`):** identical to `post` — `path` (required), `body`
(optional), `headers` (optional). `body` is optional because some PUT endpoints
take cache-invalidation semantics with no payload.

**Example:**

```python
gh.put(
    path = "/repos/${ctx.repo}/contents/${ctx.file}",
    body = '{"message":"update","content":"${ctx.b64}"}',
)
```

**Notes:** Like `post`, cannot share a `step(block=[...])` with idempotent ops.

### delete

**Idempotent:** **false** — **diverges from RFC-7231** (which considers DELETE
idempotent). See **Idempotence Policy (D4-14)** below for the rationale.

**Kwargs (`GetArgs`):** identical to `get` — `path` (required), `headers`
(optional). DELETE has no body in the bundled extension's schema.

**Example:**

```python
gh.delete(path = "/repos/${ctx.repo}/issues/${ctx.issue_id}")
```

**Notes:** Like `post`/`put`, cannot share a `step(block=[...])` with
idempotent ops.

## Idempotence Policy (D4-14)

| Operation | Idempotent | Notes                                                       |
|-----------|------------|-------------------------------------------------------------|
| `get`     | **true**   | Matches RFC-7231                                            |
| `head`    | **true**   | Matches RFC-7231                                            |
| `post`    | **false**  | Matches RFC-7231                                            |
| `put`     | **false**  | **Diverges** from RFC-7231 (which considers PUT idempotent) |
| `delete`  | **false**  | **Diverges** from RFC-7231 (which considers DELETE idempotent) |

Source: `pkg/extension/builtin/http/http.go::Operations`.

### Why diverge?

PUT and DELETE are technically idempotent per the HTTP spec, but in practice
(consultant flows hitting GitHub, Slack, internal APIs) the consequences of an
accidental retry of a PUT/DELETE are usually worse than the cost of executing
them one-action-per-activity. The v1 stance is the conservative one: treat
them as side-effecting.

This is **D4-14** — a locked Phase 4 decision. The bundled extension's
behavior is pinned by `TestExtension_OperationsIdempotenceMatchesD4_14`; any
future change requires a CONTEXT.md amendment.

Consultants writing their **own** extensions (Phase 6: GitHub, Slack, etc.)
declare `Idempotent *bool` per operation in their `OperationSpec` map; nothing
forces them to follow the bundled HTTP extension's choice. See
[docs/for-extension-developers/README.md](../../for-extension-developers/README.md).

### Mixed-idempotency batches reject

Batching a non-idempotent op with an idempotent one in the same
`step(block=[...])` is rejected at parse time (Policy D, **D2-05**) so the
activity layer never sees a mixed batch. Workaround: split into two `step(...)`
calls.

```python
# REJECTED at parse time — mixed idempotency:
step(
    block = [
        gh.get(path  = "/repos/${ctx.repo}"),    # idempotent
        gh.post(path = "/repos/${ctx.repo}/issues", body = "..."),  # non-idempotent
    ],
)

# OK — split into two steps:
step(action = gh.get(path  = "/repos/${ctx.repo}"))
step(action = gh.post(path = "/repos/${ctx.repo}/issues", body = "..."))
```

## Response Classification (4xx vs 5xx)

| Status | Result                                              | Behavior                                                                                    |
|--------|-----------------------------------------------------|---------------------------------------------------------------------------------------------|
| 2xx    | `OkResult`                                          | Output struct includes `status: int` (e.g., 200, 204), `body`, `headers`                    |
| 3xx    | (followed automatically by `net/http`)              | Within the activity timeout; transparent to flow authors                                    |
| 4xx    | `NonRetryableErr` (`extension.ErrNonRetryable` wrap)| Workflow fails immediately; renderer prints `flow failed step I/M (reason)` with red marker |
| 5xx    | retryable error (plain wrap)                        | Temporal's RetryPolicy fires (default 3 attempts with exponential backoff)                  |

The 4xx → non-retryable / 5xx → retryable split is the **quick-260502-onc**
behavior. The error message includes the HTTP status code and the response
body (truncated to ~200 bytes) for diagnostic clarity. Source:
`pkg/extension/builtin/http/http.go::doHTTP`.

### Override retry behavior per step

A flow author can explicitly suppress retries on a step that would normally
retry on 5xx — useful when a destructive operation must not be retried even on
transient server errors:

```python
step(
    action = gh.put(path = "/repos/owner/repo", body = "{...}"),
    retry  = retry_policy(max_attempts = 1),  # don't retry even on 5xx
)
```

Conversely, a step that should retry more aggressively can raise the cap:

```python
step(
    action = gh.get(path = "/repos/${ctx.repo}"),
    retry  = retry_policy(max_attempts = 10),
)
```

## Credentials

The `credential=` kwarg on `http.endpoint(...)` accepts a string ID:

```python
gh = http.endpoint(
    base_url   = "https://api.github.com",
    credential = "github_token",  # ID, not the secret itself
)
```

The ID is the only thing that ever lives in workflow state. At
workflow-execute time, the generic activity invokes the registered
`CredentialHandler.Resolve(ctx, id)` to fetch the live secret —
**just-in-time**, inside the activity stack frame. The resolved
`extension.Credential` value never escapes the activity; the operation
receives it via parameter and never returns it.

This is the architectural invariant: **credentials never enter workflow state.**
See [docs/architecture.md](../../architecture.md) for the parse/execute split
and the no-context-bleed rationale, and
[docs/cli-binary.md](../../cli-binary.md) for how to wire your own
`CredentialHandler` (vault lookup, env vars, etc.).

### Empty / omitted credential

```python
gh_anon = http.endpoint(base_url = "https://api.github.com")
# or:
gh_anon = http.endpoint(base_url = "https://api.github.com", credential = "")
```

Both forms produce an **anonymous endpoint** — no resolver call. The activity
short-circuits the empty-ID lookup and passes `nil` to the operation's `cred`
parameter. Useful for public APIs (e.g., open GitHub APIs that don't need a
token, or internal services accessible without auth).

### Credential kinds and how the HTTP extension applies them

The `Resolve(ctx, id)` callback returns one of three sealed
`extension.Credential` kinds. The HTTP extension routes them as follows
(`pkg/extension/builtin/http/http.go::applyCredential`):

| Credential kind     | HTTP behavior                                                              |
|---------------------|----------------------------------------------------------------------------|
| `BearerCredential`  | `Authorization: Bearer <token>`                                            |
| `BasicCredential`   | `Authorization: Basic base64(user:password)` via `http.Request.SetBasicAuth`|
| `APIKeyCredential`  | `req.Header.Set(c.HeaderName, c.Key)` (default `HeaderName` = `Authorization`)|

A `nil` credential is a no-op (anonymous endpoint).

## Common Patterns

### GET with `${ctx.expr}` interpolation

```python
gh = http.endpoint(base_url = "https://api.github.com")

flow(
    name   = "fetch_user",
    inputs = {"user_id": "string"},
    steps  = [
        step(
            name   = "Fetch ${ctx.user_id}",
            action = gh.get(path = "/users/${ctx.user_id}"),
        ),
    ],
)
```

`${ctx.user_id}` desugars at parse time into a lambda — see
[docs/reference/builtins.md](../../reference/builtins.md) for the
interpolation contract and the dollar-sign escape rule (`$$` → literal `$`).

### POST with a body dict

```python
gh = http.endpoint(base_url = "https://api.github.com", credential = "github_token")

flow(
    name   = "open_issue",
    inputs = {"repo": "string", "title": "string", "body": "string"},
    steps  = [
        step(
            name   = "Create issue in ${ctx.repo}",
            action = gh.post(
                path    = "/repos/${ctx.repo}/issues",
                body    = '{"title": "${ctx.title}", "body": "${ctx.body}"}',
                headers = {"Content-Type": "application/json"},
            ),
        ),
    ],
)
```

`body` is a string. Build JSON with str-concat or interpolation inside the
string for simple cases. For richer cases (escaping, nested objects), build
the body inside an `action_fn` lambda that returns the `*ActionRef`:

```python
step(action_fn = lambda ctx: gh.post(
    path = "/repos/" + ctx.repo + "/issues",
    body = ctx.payload,  # already a JSON-encoded string from a prior script
))
```

### Block-batch idempotent reads

```python
gh = http.endpoint(base_url = "https://api.github.com")

flow(
    name  = "fanout_reads",
    steps = [
        step(
            block = [
                gh.get(path = "/repos/octocat/Hello-World"),
                gh.get(path = "/repos/octocat/Spoon-Knife"),
                gh.get(path = "/users/octocat"),
            ],
        ),
    ],
)
```

All three execute in a single activity invocation — heartbeats between each
action so long batches don't trip Temporal's heartbeat timeout. Adding any
non-idempotent op (`post`/`put`/`delete`) makes the parser reject the batch
(Policy D).

For a runtime-built batch over a list, use `block_fn`:

```python
step(
    name     = "Read ${ctx.repo} surface",
    block_fn = lambda ctx: [
        gh.get(path = "/repos/" + ctx.repo),
        gh.get(path = "/repos/" + ctx.repo + "/branches"),
        gh.get(path = "/repos/" + ctx.repo + "/contributors"),
    ],
)
```

The parser's best-effort static classifier accepts homogeneous-idempotent
`block_fn` returns at parse time; mixed-idempotency batches surfacing only at
runtime are caught by the activity-side fallback.

### Top-level `fail()` on a missing input

```python
gh = http.endpoint(base_url = "https://api.github.com")

flow(
    name   = "guard",
    inputs = {"repo": "string"},
    steps  = [
        if_cond(
            cond  = lambda ctx: ctx.repo != "",
            then  = [step(action = gh.get(path = "/repos/${ctx.repo}"))],
            else_ = [fail("repo input is required")],
        ),
    ],
)
```

See [docs/reference/builtins.md](../../reference/builtins.md) for the dual
top-level vs lambda-time `fail()` semantics — the parse-time builtin emits a
workflow-failure node; the lambda-time global raises inside a running lambda.

## See Also

- **DSL syntax** — [docs/reference/builtins.md](../../reference/builtins.md)
  for `step`, `if_cond`, `for_each_parallel`, and `${ctx.expr}` interpolation
- **Architecture** — [docs/architecture.md](../../architecture.md) for the
  parse/execute split and the credential-resolution boundary
- **Custom CLI** — [docs/cli-binary.md](../../cli-binary.md) for wiring your
  own `CredentialHandler` and registering extensions
- **Examples** — [examples/README.md](../../../examples/README.md) for
  runnable `.star` files (`expression_if.star`, `simple_check.star`,
  `parallel_fanout.star`)
- **Source** — `pkg/extension/builtin/http/http.go` (operations, factory,
  `GetArgs` / `BodyArgs` schemas, `doHTTP` dispatch, `applyCredential` routing)
