# Testing Tutorial — A GitHub API Flow End-to-End

This tutorial walks through writing a complete Tier-3 test suite for a flow
that hits the GitHub API. By the end you'll know how to:

- Mock the `http` extension at file-scope and per-test
- Drive a workflow with `tester.workflow(...)` + `tester.run(...)`
- Exercise retry semantics with the `attempt` counter
- Read both human and JSON `skytime test` output
- Filter test runs by regex
- Recognize the most common authoring pitfalls

The worked example is `examples/skeleton/simple_check.star` (a GitHub-API
flow that fetches a repo, derives a status, and conditionally fetches
branches) and its companion test file `examples/skeleton/simple_check_test.star`.
You can read along, or follow the steps yourself by deleting the test
file first.

For the *reference* — every kwarg, every error message, every D5-locked
decision — see [`testing.md`](testing.md). This tutorial is the
narrative companion.

---

## What you'll need

- A built `skytime` binary on `$PATH` (or invocable via its absolute path).
  From the repo root: `go install ./cmd/skytime` puts it at `$GOBIN/skytime`
  (typically `$HOME/go/bin/skytime`).
- The Skytime repo cloned locally (the tutorial paths are repo-relative).
- A working install of Go ≥ 1.25 (only needed if you want to rebuild).

You do **not** need a running Temporal server. The test harness runs
flows inside `testsuite.TestWorkflowEnvironment` (in-process, no
network), and mocks the single generic `ExecuteBatch` activity so no
actual HTTP requests fire.

---

## The flow under test

`examples/skeleton/simple_check.star` declares one flow, `simple_check`:

```python
gh = http.endpoint(base_url = "https://api.github.com")

flow(
    name = "simple_check",
    inputs = {"repo": "string"},
    steps = [
        step(
            name = "Get repo ${ctx.repo}",
            action = gh.get(path = "/repos/${ctx.repo}"),
        ),
        script(
            id = "extract_status",
            fn = lambda ctx: {"healthy": True, "repo": ctx.repo},
            output_alias = "health",
        ),
        if_cond(
            cond = lambda ctx: ctx.health,
            then = [
                step(
                    name = "Get branches for ${ctx.repo}",
                    action = gh.get(path = "/repos/${ctx.repo}/branches"),
                ),
            ],
            else_ = [
                step(
                    name = "Re-fetch ${ctx.repo}",
                    action = gh.get(path = "/repos/${ctx.repo}"),
                ),
            ],
        ),
    ],
)
```

Three primitives in play:

1. A `step` that calls `gh.get(...)` against `/repos/{repo}`.
2. A `script` that derives a `health` field from the response.
3. An `if_cond` that branches on `health`, fetching `/branches` on the
   `then` arm or re-fetching `/repos/{repo}` on `else_`.

The `gh.get(...)` calls are what we'll mock. **Note:** `gh =
http.endpoint(...)` binds `gh` *locally*; dispatch still routes
through the registered extension named `http`. We'll come back to
this.

---

## Step 0 — Verify the discovery path works

Before authoring anything, confirm the runner can find `*_test.star`
files in the example directory. If you've already deleted
`simple_check_test.star`:

```bash
$ skytime test ./examples/skeleton/
no *_test.star files under ./examples/skeleton/
```

That's the empty-discovery message. The walker recurses
(`filepath.WalkDir` under the hood) and reports nothing because no
file matches the `*_test.star` suffix. This is exit code 0, by
design — an empty test directory isn't a failure. Now let's create a
test file.

---

## Step 1 — A test file with one passing test

Create `examples/skeleton/simple_check_test.star` with the absolute
minimum — the flow declared inline (v1 cannot `load()` across files;
see [`testing.md`](testing.md)), one mock, one workflow declaration,
one test:

```python
"""simple_check_test.star — Tier-3 tests for simple_check.star."""

gh = http.endpoint(base_url = "https://api.github.com")

flow(
    name = "simple_check",
    inputs = {"repo": "string"},
    steps = [
        step(
            name = "Get repo ${ctx.repo}",
            action = gh.get(path = "/repos/${ctx.repo}"),
        ),
        script(
            id = "extract_status",
            fn = lambda ctx: {"healthy": True, "repo": ctx.repo},
            output_alias = "health",
        ),
        if_cond(
            cond = lambda ctx: ctx.health,
            then = [
                step(
                    name = "Get branches for ${ctx.repo}",
                    action = gh.get(path = "/repos/${ctx.repo}/branches"),
                ),
            ],
            else_ = [
                step(
                    name = "Re-fetch ${ctx.repo}",
                    action = gh.get(path = "/repos/${ctx.repo}"),
                ),
            ],
        ),
    ],
)

# File-scope: every test below inherits this default mock.
tester.mock_action(
    extension = "http",
    op = "get",
    mock_fn = lambda kwargs, attempt: ok(value = {"name": "Hello-World"}),
)

tester.workflow(
    name = "simple_check",
    init_state = {"repo": "octocat/Hello-World"},
)

def test_happy_path():
    """Inherits the file-scope mock; every gh.get returns 200."""
    tester.run(flow = "simple_check")
```

A handful of points to absorb here:

**File-scope vs def-scope.** `tester.mock_action(...)` and
`tester.workflow(...)` at module scope are *file-scope*: the runner
applies them to every `def test_*()` in the file unless overridden.
Top-level statements run once, before any test executes.

**`extension = "http"`, not `"gh"`.** This is the most common authoring
mistake. `gh = http.endpoint(...)` is a Starlark-local binding — the
ActionRef the workflow dispatches still has `Extension="http", Op="get"`.
If you write `extension="gh"` here, the runtime will reject every call
with `no mock for http.get at <pos>`. The hint in the error message
*always* shows the actual registered extension name — use it.

**`init_state` shape matches `inputs={...}`.** The flow declares
`inputs = {"repo": "string"}`, so `init_state` MUST provide a `repo`
string. Mismatched shapes surface as parse-time errors before the
workflow runs.

Run it:

```bash
$ skytime test ./examples/skeleton/
--- PASS: test_happy_path (0.07s)
PASS  simple_check_test.star  1 tests  (0.07s)
PASS  1 files  1 tests  (0.07s)
```

Three lines of output:

1. `--- PASS:` — the per-test result line. Mirrors `go test`'s shape.
2. `PASS  simple_check_test.star  1 tests` — the per-file footer.
3. `PASS  1 files  1 tests` — the all-files summary.

Process exit code is `0`. You can confirm with `echo $?`.

---

## Step 2 — A second test with a per-test mock override

Now add a second test that returns a *different* response, just for
that test, by re-registering the same `(extension, op)` pair *inside*
the function body:

```python
def test_alice_repo():
    """Override the file-scope mock for THIS test only."""
    tester.mock_action(
        extension = "http",
        op = "get",
        mock_fn = lambda kwargs, attempt: ok(value = {"name": "alice-repo"}),
    )
    tester.run(flow = "simple_check")
```

**Mock scope is a stack.** When the runner enters a `def test_*()`, it
pushes a fresh frame onto the registry stack. Any
`tester.mock_action(...)` calls inside the function go on this frame.
On exit, the frame is popped — so per-test mocks never leak into the
next test.

When the runner looks up a mock for an action, it walks the frames
from top (most recent) to bottom (file-scope), picking the most
specific match per the precedence ladder. A per-test mock for the same
`(extension, op)` shadows the file-scope one for that test only.

```bash
$ skytime test ./examples/skeleton/
--- PASS: test_alice_repo (0.00s)
--- PASS: test_happy_path (0.07s)
PASS  simple_check_test.star  2 tests  (0.07s)
PASS  1 files  2 tests  (0.07s)
```

Test order is **alphabetical by name**, not declaration order — this
is replay-determinism (D5-A1): the runner sorts discovered tests so
the same suite produces the same event sequence on every run.

---

## Step 3 — Exercising retry semantics with `attempt`

The mock function takes two positional args: `kwargs` (the resolved
action kwargs) and `attempt` (1-indexed retry count). On a retryable
failure (`err(msg=...)`), Temporal's RetryPolicy fires, and the next
call to the same mock receives `attempt + 1`.

```python
def test_with_retry():
    """First attempt fails (retryable), second succeeds — exercises
    the per-(flow, step, action_idx) attempt counter."""
    tester.mock_action(
        extension = "http",
        op = "get",
        mock_fn = lambda kwargs, attempt: (
            err(msg = "transient") if attempt == 1
            else ok(value = {"name": "ok-after-retry"})
        ),
    )
    tester.run(flow = "simple_check")
```

The first `gh.get(...)` call returns `err(msg="transient")`. Temporal
retries (the default in-test RetryPolicy is short — milliseconds, not
seconds), and the mock is reinvoked with `attempt = 2`, this time
returning `ok(...)`. The workflow proceeds to the script, the
`if_cond`, and another `gh.get(...)` for the `then` branch — also
mocked, also `attempt = 1` for *that* (flow, step, action) tuple
because the counter is keyed per-action, not per-test.

The kwarg keyword is **`msg=`**, not `message=`. This applies to both
`err(msg="...")` and `nonretryable(msg="...")`.

---

## Step 4 — Inspecting failure output

So far every test passes. Let's deliberately break one to see what the
failure render looks like — useful for understanding `skytime test`
output before you encounter your first real failure.

Add this test (and remember to remove it before committing):

```python
def test_intentionally_failing():
    """For tutorial purposes: this test will fail."""
    assert.eq("octocat", "octodog")
```

`assert.eq` is from `go.starlark.net/starlarktest` — wired in by the
parser when `WithTestMode()` is active. Failures route to the active
subtest's reporter and surface as indented detail under the per-test
`--- FAIL:` line.

```bash
$ skytime test ./examples/skeleton/
--- PASS: test_alice_repo (0.00s)
--- PASS: test_happy_path (0.07s)
--- FAIL: test_intentionally_failing (0.00s)
    assertion failed at simple_check_test.star:NN:5
      expected: "octocat"
      got:      "octodog"
--- PASS: test_with_retry (0.00s)
FAIL  simple_check_test.star  4 tests  1 failed  (0.07s)
FAIL  1 files  4 tests  1 failed  (0.07s)
```

Things to note:

- The **per-test FAIL line** lists the test name + elapsed time.
- Indented detail beneath gives the **Starlark callsite**
  (`<file>:<line>:<col>`) and the assertion delta. **No Go stack
  frames** — that's CLI-03, deliberately enforced by the
  `TestTestCommand_DefaultOutput_NoGoStackTraces` regression test.
- The **per-file footer** flips to `FAIL  ... N failed`.
- The **summary** flips to `FAIL  ...`.
- **Process exit code is 1** when any test failed (`echo $?` confirms).

If a workflow fails (not an assertion — a real `nonretryable()` mock
or a `fail()` inside a flow), the indented detail looks slightly
different — it carries the workflow error message and Temporal's
non-retryable wrapper:

```
--- FAIL: test_repo_not_found (0.00s)
    tester.run (run 1): workflow error: workflow execution error
    (type: SkytimeWorkflow, ...): 404 Not Found:
    non-retryable (type: errMsg, retryable: true)
```

Workflow-level failures from negative-path mocks can't currently be
declared as *expected* — see "v1 limitations" below. Remove the
deliberately-failing test before moving on:

```python
# (delete the test_intentionally_failing block from your file)
```

---

## Step 5 — JSON output and regex filtering

For CI dashboards, `gotestsum`-style consumers, or just programmatic
processing, use `--format=json`:

```bash
$ skytime test --format=json ./examples/skeleton/
{"Time":"2026-05-06T02:15:58.095756Z","Action":"start","Package":"simple_check_test.star","Test":"test_alice_repo"}
{"Time":"2026-05-06T02:15:58.095808Z","Action":"run","Package":"simple_check_test.star","Test":"test_alice_repo"}
{"Time":"2026-05-06T02:15:58.154594Z","Action":"pass","Package":"simple_check_test.star","Test":"test_alice_repo","Elapsed":0.058779666}
{"Time":"...","Action":"start","Package":"simple_check_test.star","Test":"test_happy_path"}
... (continues)
```

The schema mirrors the Go stdlib `cmd/test2json` shape (D5-E2):

| Field     | Always present? | Type     | Notes                                       |
|-----------|-----------------|----------|---------------------------------------------|
| `Time`    | Yes             | string   | RFC3339 nanosecond UTC                       |
| `Action`  | Yes             | string   | `start`/`run`/`pass`/`fail`/`skip`/`output` |
| `Package` | Yes             | string   | The `*_test.star` basename                   |
| `Test`    | Most actions    | string   | The `def test_*()` name                      |
| `Elapsed` | Terminal actions | float   | Seconds                                      |
| `Output`  | `output` action | string   | Failure detail text                          |

Per-test sequence is `start → run → (pass|fail|skip)` with optional
`output` records carrying failure detail before the terminal action.
Stream-parseable line by line.

Use **`--run`** to filter tests by regex against
`<file_basename_without_ext>.<test_name>`:

```bash
$ skytime test --run "test_happy" ./examples/skeleton/
--- PASS: test_happy_path (0.06s)
PASS  simple_check_test.star  1 tests  (0.06s)
PASS  1 files  1 tests  (0.06s)

$ skytime test --run "^simple_check_test\.test_(happy|with_retry)$" ./examples/skeleton/
--- PASS: test_happy_path (0.07s)
--- PASS: test_with_retry (0.00s)
PASS  simple_check_test.star  2 tests  (0.07s)
PASS  1 files  2 tests  (0.07s)
```

The pattern is a **Go regexp** (RE2 — no PCRE backreferences). Anchor
with `^` / `$` for whole-name matches; default is partial.

---

## Step 6 — Path forms

`skytime test` accepts either a directory or a single file path:

```bash
# Directory — recursive walk for *_test.star files
skytime test ./examples/skeleton/
skytime test ./examples/                  # one level up — finds the same file
skytime test .                            # whole repo (any depth)

# Single-file path (must end in _test.star)
skytime test ./examples/skeleton/simple_check_test.star
```

A non-test file path (e.g. `skytime test foo.star`) is rejected with
`path foo.star is not a *_test.star file`. Multiple positional args
are not yet supported in v1; if you want to run only certain tests,
combine a single-directory arg with `--run`.

---

## Common pitfalls

A condensed list of what most often trips up first-time authors. The
[reference](testing.md) has the full inventory.

| Pitfall | Symptom | Fix |
|---------|---------|-----|
| Using local var name as `extension` | `no mock for http.get at <pos>` (note the `http`) | Use the registered extension name (`http`), not the Starlark-local binding (`gh`) |
| `message=` instead of `msg=` | `unexpected keyword argument "message"` at workflow time | Both `err(msg=...)` and `nonretryable(msg=...)` use `msg=` |
| `load()` from sibling `.star` file | "no flow named X" or parser error | v1 limitation. Redeclare the flow inline in the test file. Cross-file `load()` is on the v2 roadmap. |
| `tester.run` at file scope | Parse error: `must be called inside a def test_*()` | Wrap every `tester.run(...)` in a `def test_*()` function |
| Forgotten `return` in mock_fn | `mock must return ok/err/nonretryable` (non-retryable) | Mock lambdas MUST return one of `ok(...)`, `err(...)`, `nonretryable(...)`. Returning `None` (i.e. no return) is rejected. |
| Helper named `def test_helper(x):` | Silently skipped from discovery | Discovery requires zero parameters. Name helpers without the `test_` prefix (e.g. `def helper(x):`). |
| `tester.workflow(init_state={"x": "${ctx.input.y}"})` | Parse-time error | `${ctx.expr}` interpolation is parse-time-evaluated against runtime ctx; init_state IS the seed, so ctx isn't in scope yet. Use literal values in init_state. |

---

## v1 limitations (worth knowing up front)

1. **No `load()` across files.** The flow under test must be declared
   inline in the same `*_test.star` file. Practical workaround: copy
   the flow definition. Cross-file `load()` is slated for v2.
2. **No `expects_failure` assertion.** Every workflow failure
   propagates as a test failure. To assert that a workflow *should*
   fail (e.g. that a 404 surfaces as a non-retryable error), today
   you'd need to extract the assertion into a Starlark function and
   use `assert.fails(fn, regex)` outside `tester.run`. Negative-path
   workflow assertions are slated for v2.
3. **Multi-positional args not supported.** `skytime test path1 path2`
   is rejected. Use a parent directory + `--run` regex, or invoke
   `skytime test` once per path.
4. **No parallelism within a file.** Tests run sequentially within a
   file, files run sequentially across the discovered set
   (D5-E5). This is intentional for v1 — Starlark thread isolation
   plus replay-determinism are easier to reason about with a single
   serial walk.

---

## Where to next

- **Reference manual:** [`testing.md`](testing.md) — every kwarg,
  every return shape, every locked decision (D5-A1, D5-B4, D5-D1, etc.).
- **CLI reference:** [`docs/reference/cli.md ## skytime test`](../reference/cli.md#skytime-test)
  — every flag and exit code for `skytime test`.
- **Architecture:** [`docs/architecture.md`](../architecture.md) — how
  the harness intercepts the single generic `ExecuteBatch` activity
  and routes per-action calls back to Starlark mocks.
- **HTTP extension reference:** [`extensions/http.md`](extensions/http.md)
  — the production extension. Your mocks should match its `op` names
  (`get`/`head`/`post`/`put`/`delete`) and kwarg shapes.
- **Worked example:** [`../../examples/skeleton/simple_check_test.star`](../../examples/skeleton/simple_check_test.star)
  — the full test suite this tutorial built. Use as a template.
