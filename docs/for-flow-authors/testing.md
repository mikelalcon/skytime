# Testing Flows with `temporal_test`

Skytime's Tier-3 test harness lets flow authors write end-to-end tests in `.star`
files, then run them with `skytime test <dir>`. Tests execute against the
in-process `testsuite.TestWorkflowEnvironment` (no Temporal server required),
mock the single generic `ExecuteBatch` activity, route per-action calls back to
Starlark mock lambdas in the same restricted predeclared environment as
production lambdas, and run every test twice — diffing Temporal event histories
to catch non-determinism (D5-D1).

This page is for flow authors. For Go-side test integration (calling
`pkgtesting.Run(t, dir)` from a `*_test.go` in your example project), see
[docs/for-extension-developers/README.md](../for-extension-developers/README.md).

Source-of-truth:

- `pkg/testing/module.go` — the `tester` Starlark module + builtin registrations.
- `pkg/parser/parser.go::WithTestMode` — the parser flag that gates `tester.*` and `assert.*` injection.
- `pkg/testing/runner.go` — the runner; `pkgtesting.Run(t, dir)` is the Go-level entry point, `pkgtesting.RunCLI(dir)` is the cobra-side adapter.

> Note: this file is the manual reference for the `tester.*` builtins. The
> auto-generated [`docs/reference/builtins.md`](../reference/builtins.md) covers
> production-only builtins (`flow`, `step`, `if_cond`, ...); `tester.*` lives
> outside `cmd/skytime-docgen`'s single-file walker and is documented here
> instead. Multi-file docgen integration is a post-v1 follow-up (Plan 06
> deviation D5-docs-builtins-marker-location).

---

## File-naming convention

A test file MUST be named `*_test.star`. `skytime test <dir>` walks `<dir>`
recursively (`filepath.WalkDir`) and treats only files matching this suffix as
test files (D5-A2). Place test files alongside production `.star` files:

```
flows/
  users.star
  users_test.star      # tests for users.star
  orders.star
  orders_test.star     # tests for orders.star
```

A single-file path (`skytime test flows/users_test.star`) is also accepted; the
runner short-circuits the walk for that one file.

Inside the file, declare each test as a top-level `def test_<name>():`
function (D5-A1, RESEARCH Pattern 4):

- The `def` MUST be at module scope (not nested inside another `def`).
- The function name MUST start with `test_`.
- The function MUST take **zero** parameters. Helpers like
  `def test_helper(x):` are silently skipped during discovery (the suffix
  filter requires `NumParams() == 0`).

Top-level statements are file-scope setup, run once before any `def test_*()`
is invoked. Typical use: shared `tester.mock_action(...)` calls that every
test in the file inherits.

---

## tester.workflow

Declares the workflow under test. Signature (D5-A3):

```python
tester.workflow(
    name = "users",
    init_state = {"user_id": "octocat"},
    retry_policy = {...},   # optional; reserved for forthcoming Temporal RetryPolicy mapping
    timeouts = {...},       # optional; reserved for StartToCloseTimeout / etc.
)
```

The `name` kwarg picks one of the flows declared in the SAME `*_test.star`
file. `init_state` is the workflow's seed — typed-shape per the flow's
`inputs={...}` declaration.

> **v1 limitation — `load()` across files is not supported.**
> The flow under test MUST be declared inline in the same `*_test.star`
> file. `load("./users.star", "users")` from a test file will not expose
> the flow object to `tester.workflow`/`tester.run`. See
> `pkg/testing/runner.go` ("Single-file scope only — load() across files
> is a Phase 6 concern"). For now, redeclare (or copy-paste) the flow
> definition into the test file. Cross-file `load()` is on the v2
> roadmap.

**Per-test variation:** redeclare `tester.workflow(...)` inside a `def test_*()`
to override file-level defaults for that test only. The runner uses
last-write-wins semantics (RESEARCH Open Q5):

```python
# File scope: defaults for every test below.
tester.workflow(name = "users", init_state = {"user_id": "octocat"})

def test_existing_user():
    # Inherits the file-scope spec.
    tester.run(flow = "users")

def test_default_user():
    # Override init_state for this test only.
    tester.workflow(name = "users", init_state = {"user_id": "default-user"})
    tester.run(flow = "users")
```

`retry_policy` and `timeouts` are accepted at the kwargs surface and reserved
for Plan 04+ Temporal mapping; consult `pkg/testing/builtin_workflow.go` for the
current behavior.

---

## tester.mock_action

Registers a mock for one `(extension, op)` pair. Signature (D5-B1):

```python
tester.mock_action(
    extension = "gh",
    op        = "get",
    match     = {"path": "/users/octocat"},  # optional; D5-B5 partial regex
    mock_fn   = lambda kwargs, attempt: ok(value = {"login": "octocat"}),
)
```

The mock fires whenever the production flow dispatches a matching action via
the single generic `ExecuteBatch` activity. The runner intercepts the call at
the testsuite level and routes the per-action invocation through your
`mock_fn` lambda.

> **Pitfall: `extension` is the *registered* name, not a local variable.**
> If your flow does `gh = http.endpoint(base_url=...)`, the symbol `gh`
> is just a Starlark-local binding — dispatch still routes through the
> `http` extension. Mock with `extension="http"`, NOT `extension="gh"`.
> An incorrect extension name surfaces at workflow time as
> `no mock for http.get at <pos>` (note: `http`, not `gh`); use that
> hint to confirm the registered name.

### Mock scope (D5-A4 stack)

- **File-scope mocks** (top-level `tester.mock_action(...)` calls) are visible
  to every `def test_*()` in the file.
- **Per-test mocks** (`tester.mock_action(...)` inside a `def test_*()`)
  shadow the file-level mock for the same `(extension, op)` for that test
  only. The runner pushes a per-test frame on entry to each `def test_*()`
  and pops it on exit.

```python
# File-scope: every test sees this default mock.
tester.mock_action(extension="gh", op="get",
    mock_fn=lambda kwargs, attempt: ok(value={"login": "octocat"}))

def test_existing_user():
    # Uses the file-scope mock.
    tester.run(flow="users")

def test_other_user():
    # Override for THIS test only.
    tester.mock_action(extension="gh", op="get",
        mock_fn=lambda kwargs, attempt: ok(value={"login": "alice"}))
    tester.run(flow="users")
```

### Match precedence (D5-B4 specificity ladder)

When more than one mock could match a given action call, the runner picks the
most-specific one. The ladder, from highest to lowest priority:

1. `(extension, op)` exact + `match={...}` regex matched
2. `(extension, op)` exact, no `match` constraint
3. `(extension, "*")` wildcard

Within the same tier, **most-recently-registered wins** — so a per-test
`(gh, get)` shadows a file-level `(gh, get)`. Cross-extension wildcards
(`extension="*"`) are NOT supported in v1 (D5-B3).

### Match key semantics (D5-B5, D5-B6)

- `match` values are Starlark **strings** interpreted as Go regular expressions.
  Non-string values (ints, dicts) are rejected at registration time.
- Match is **partial** by default: `match={"path": "/users/octocat"}` matches
  any call whose `kwargs["path"]` contains `/users/octocat`. Anchor with
  `^...$` for exact-match: `match={"path": "^/users/octocat$"}`.
- Patterns are compiled once at `tester.mock_action` registration; bad patterns
  surface as parse-time errors (so a typo aborts before `skytime test` even
  runs the workflow).

### Wildcard `op="*"` (D5-B3)

```python
# Catch any gh.* call not matched by a more-specific mock.
tester.mock_action(extension="gh", op="*",
    mock_fn=lambda kwargs, attempt: ok(value={"caught": "by wildcard"}))
```

Useful for "fail loudly on anything not stubbed": pair the wildcard with
`nonretryable(msg=...)` to surface unintended calls.

---

## Mock function I/O contract

Mock lambdas receive two positional args (D5-C1):

```python
mock_fn = lambda kwargs, attempt: ok(value={...})
```

- `kwargs` — a frozen Starlark dict of resolved action kwargs (post-`${ctx.expr}`
  interpolation, post-credential-resolution). Values are strings/ints/lists/
  dicts as the action's schema dictates.
- `attempt` — 1-indexed retry count. First call = `1`; on a retryable failure,
  the next call is `attempt = 2`. Lets tests simulate transient failures:

  ```python
  mock_fn = lambda kwargs, attempt: (
      err(msg="transient") if attempt < 3
      else ok(value={"login": "octocat"})
  )
  ```

The mock lambda body runs in a **mock-lambda env** that extends the locked
20-key `lambdaTimeGlobals` (D1-20) with three return-shape builders:

- `ok(value=...)` → workflow gets `OkResult{Output: <bridge-converted value>}`.
  `value` is optional; omitting it returns an empty result. Dicts become
  `*starlarkstruct.Struct` so the downstream `${ctx.step_output.field}` access
  works the same as production output.
- `err(msg="...")` → retryable error. The workflow's `RetryPolicy` fires;
  the next call to this mock will see `attempt + 1`.
- `nonretryable(msg="...")` → non-retryable error. The workflow fails
  immediately, and the test surfaces the failure as a Starlark callsite
  (no Go stack frames in default output).

A mock lambda MUST return one of these three shapes (D5-C4). Returning
`None` (i.e., a forgotten `return`) raises a non-retryable error with the
verbatim message `mock must return ok/err/nonretryable`.

### Credentials inside the mock (D5-C1a)

If the action declared a credential, the mock receives the resolved
**credential ID** in `kwargs["_credential_id"]` — never the raw `Secret`
value. Tests can assert on routing without ever touching real secrets:

```python
def test_routes_through_github_token():
    tester.mock_action(extension="gh", op="get",
        mock_fn=lambda kwargs, attempt: (
            ok(value={"ok": True}) if kwargs["_credential_id"] == "github_token"
            else nonretryable(msg="wrong credential routed")
        ))
    tester.run(flow="users")
```

---

## tester.run

Drives the workflow under the registered mocks. Signature:

```python
def test_existing_user():
    tester.run(flow = "users")
```

`tester.run` MUST be called inside a `def test_*()` function (D5 Pitfall 4) —
file-scope calls are rejected with the verbatim message:

    must be called inside a def test_*() function (at <pos>)

The runner executes the named flow **twice** (D5-D1 always-on replay) with the
same mock registry and same `init_state`. Any divergence in the Temporal event
history fails the test with a Starlark-callsite-aware diff message (D5-D2):

```
event 7 (ActivityTaskScheduled) diverged:
  run1.payload.kwargs.path = "/users/octocat"
  run2.payload.kwargs.path = "/users/foo"
flow callsite: users_flow.star:14:5 (step "fetch user")
test callsite: users_test.star:23:5 (tester.run)
```

The flow callsite (D5-D3) points at the originating `step()` in your `.star`
file — exactly where the divergent action was emitted — so triage is one jump
from the diff.

> **v1 limitation — no `expects_failure` assertion.**
> Every workflow failure (whether from `nonretryable()`, `fail()`, an
> assertion mismatch inside an action, or any other workflow-side
> error) propagates as a test failure — the test under whose
> `tester.run` the failure occurred is marked `--- FAIL:`. There is no
> built-in `tester.expects_failure(...)` block in v1. Negative-path
> tests can still be written by extracting the *behavior* under test
> into a Starlark function and using `assert.fails(fn, regex)` against
> it directly — but you cannot today wrap `tester.run(...)` in such a
> construct. Negative-path workflow assertions are slated for v2.

---

## assert.*

The standard `go.starlark.net/starlarktest` `assert.*` builtins are available
inside `*_test.star` files when the parser is in test mode (D5-F1, TEST-05).
Failures route to the active subtest's reporter and surface in `skytime test`
output as indented detail under `--- FAIL:` lines (D5-F3):

```
--- FAIL: test_default_user (0.03s)
    assertion failed at users_test.star:31:5
      expected: "octocat"
      got:      "default-user"
```

Available helpers include:

- `assert.eq(want, got)` / `assert.ne(...)`
- `assert.true(cond)` / `assert.false(cond)`
- `assert.lt(a, b)` / `assert.le(...)` / `assert.gt(...)` / `assert.ge(...)`
- `assert.contains(haystack, needle)`
- `assert.fails(fn, regex)` — assert that `fn()` raises an error matching `regex`.

Multiple `assert.*` failures within one `def test_*()` **accumulate** (library
default, D5-F2): a single test with two failing `assert.eq` calls produces two
indented detail blocks under one `--- FAIL:` line. The test fails at the end
if any accumulated.

---

## ${ctx.expr} interpolation in tests

Test files share the parser path with production `.star` files, so
`${ctx.expr}` interpolation works inside test-file string kwargs identically
(D5-G1). Two places where this matters:

- **Inside flow declarations** (the production code under test): same as
  production — `step(name="Fetch ${ctx.user_id}", action=gh.get(...))` works.
- **Inside `mock_fn` lambda bodies**: the lambda evaluates at workflow time
  with full `ctx`, so `${...}` strings inside the returned `value=...` dict
  follow the standard lambda-time semantics.

One caveat: `tester.workflow(init_state={"x": "${ctx.input.y}"})` will surface
as a parse-time error per D4-03 — `ctx` is not in scope at the `init_state`
declaration site (init_state IS the seed; `ctx` doesn't exist yet).

---

## Running tests

The runner has two entry points sharing the same internals:

### `skytime test <dir>` — CLI

See [`docs/reference/cli.md ## skytime test`](../reference/cli.md#skytime-test)
for the full reference. Quick summary:

```
skytime test examples/http-github-slack/
skytime test examples/http-github-slack/ --run '^users_test\.test_existing'
skytime test examples/http-github-slack/ --format=json
```

Exit codes (D5-E4):

- `0` — every test passed (or no `*_test.star` files were found).
- `1` — one or more tests failed.
- `2` — usage error (zero or multiple positional args).

Default human output (D5-E1) carries Starlark callsites only — **no Go stack
traces** (CLI-03 explicit; pinned by `TestTestCommand_DefaultOutput_NoGoStackTraces`
and the subprocess `TestSkytimeTestE2E_FailureExitNonzero`).

### `pkgtesting.Run(t, dir)` — Go-test integration

For the `*_test.go`-side path (Phase 6 example projects), import
`pkg/testing` and call `Run` from inside a `*testing.T`:

```go
func TestSkytimeFlows(t *testing.T) {
    pkgtesting.Run(t, "testdata/", pkgtesting.WithExtensions(myext.New()))
}
```

Each discovered `def test_*()` becomes a Go subtest via `t.Run`, so per-test
pass/fail granularity flows naturally into `go test -v` output and
`gotestsum`-style consumers.

---

## See Also

- **Tutorial:** [`testing-tutorial.md`](testing-tutorial.md) — step-by-step walkthrough that builds a Tier-3 test suite for a GitHub-API flow from scratch (file-scope mock → per-test override → retry semantics → reading failure output → JSON format / regex filter).
- CLI reference: [`docs/reference/cli.md`](../reference/cli.md) — `## skytime test` section with all flags + exit codes + example.
- Production builtin reference: [`docs/reference/builtins.md`](../reference/builtins.md) — auto-generated reference for `flow`, `step`, `if_cond`, `script`, `for_each_parallel`, `call_flow`, `result`, `fail`. (Note: `tester.*` is documented HERE manually; multi-file docgen integration is deferred per Plan 06 deviation D5-docs-builtins-marker-location.)
- Architecture: [`docs/architecture.md`](../architecture.md) — how the harness intercepts the single generic `ExecuteBatch` activity inside `testsuite.TestWorkflowEnvironment` and routes per-action calls back to Starlark mocks.
- HTTP extension reference: [`docs/for-flow-authors/extensions/http.md`](extensions/http.md) — production extension for which most `.star` test fixtures will mock operations.
