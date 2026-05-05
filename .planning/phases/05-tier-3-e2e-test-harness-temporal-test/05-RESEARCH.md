# Phase 5: Tier-3 E2E Test Harness (`temporal_test`) - Research

**Researched:** 2026-05-05
**Domain:** Temporal `testsuite` mock callbacks ↔ Starlark mock lambdas; `starlarktest` reporter wiring; Starlark module introspection; `go test -json` mirror
**Confidence:** HIGH (every load-bearing API confirmed against the SDK source in `$GOMODCACHE/go.temporal.io/sdk@v1.42.0/internal/workflow_testsuite.go` and the existing Phase 3/4 codebase)

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

The CONTEXT.md `<decisions>` block locks 26 decisions across six areas. The planner MUST treat each ID as load-bearing — research informs HOW to implement them, never WHETHER.

**Test File Shape & Discovery (Area A)**
- **D5-A1** — `def test_<name>():` Go-style; runner enumerates via Starlark module introspection; top-level statements are file-scope setup.
- **D5-A2** — `*_test.star` suffix; `skytime test <dir>` walks recursively.
- **D5-A3** — `tester.workflow(name=, init_state=, retry_policy=, timeouts=)`; per-test variations re-declare inside `def test_*()`.
- **D5-A4** — File-level mocks visible to all tests; per-test `tester.mock_action` shadows for that test only; stack-of-dicts.

**Mock Binding & Match Precedence (Area B)**
- **D5-B1** — `(extension, op)` + optional `match={kwargs subset}` regex.
- **D5-B2** — No-mock-found → fail fast with `extension.ErrNonRetryable` wrap; message `"no mock for gh.delete at <flow_file>:<line>:<col> (step \"<name>\")"`.
- **D5-B3** — `op="*"` per extension; NO cross-extension wildcard for v1.
- **D5-B4** — 3-tier ladder (Tier 1: kwargs match; Tier 2: exact; Tier 3: op-wildcard); most-specific tier always wins; recency breaks ties within tier.
- **D5-B5** — Go `regexp` partial-match by default; document `^...$` anchoring; compile once at registration.
- **D5-B6** — `match={...}` values are Starlark strings only; non-string disambiguation lives in lambda body.

**Mock Function I/O Contract (Area C)**
- **D5-C1** — `lambda kwargs, attempt:` two positional args; kwargs frozen; attempt 1-indexed; `_credential_id` exposed in kwargs (raw `Secret` NEVER passed).
- **D5-C2** — Three predeclared builders `ok(value=)` / `err(msg=)` / `nonretryable(msg=)`; **mock-lambda env** = `lambdaTimeGlobals` ∪ {ok, err, nonretryable}; production lambda env unchanged.
- **D5-C3** — `ok(value={dict})` converts dict → `*starlarkstruct.Struct` via existing `pkg/bridge` path; downstream reads `ctx.step_output.<key>` identically to prod.
- **D5-C4** — `None` / no return → `NonRetryableErr` `"mock must return ok/err/nonretryable"` with lambda position.

**Replay Determinism (Area D)**
- **D5-D1** — Always-on; every `tester.run` runs twice.
- **D5-D2** — First-divergent event diff with payload before/after; flow callsite + test callsite both included.
- **D5-D3** — Divergence pointer is the originating `step()` callsite in the flow `.star` (uses `dag.ActionRef.Pos`).
- **D5-D4** — Diff scope = event types + sequence + payload byte-equality (no double-check on final state).

**Runner UX (Area E)**
- **D5-E1** — Static line-per-test Go-test style on TTY; `--- PASS / --- FAIL / --- SKIP`; per-file footer; final summary line.
- **D5-E2** — `--format=json` mirrors `go test -json` schema (start/output/pass/fail records).
- **D5-E3** — `--run <regex>` filtering against `<file_basename>.<test_name>`.
- **D5-E4** — Exit 0 all-pass / 1 any-fail.
- **D5-E5 (Claude's Discretion)** — Sequential within a file v1; cross-file parallelism is the planner's call.

**`assert.*` Surfacing (Area F)**
- **D5-F1** — One sub-`*testing.T` per `def test_*` via `t.Run`; `starlarktest.SetReporter(thread, subtestT)`.
- **D5-F2 (Claude's Discretion)** — Library default (accumulate failures within a test) unless TEST-05 review surfaces a need to change.
- **D5-F3** — Starlark file:line:col + assertion detail; NO Go stack traces in default output (CLI-03).

**Interpolation (Area G)**
- **D5-G1** — `${ctx.expr}` in test files works identically to production; same desugarer.

### Claude's Discretion (research-supported recommendations live below in §Investigation Areas)

- Internal `pkg/testing` layout (file structure, helper names) — recommend mirror of `pkg/parser` / `pkg/interpreter` conventions.
- Whether `bridge.StructFromDict` needs new export — **research finding: it does not exist; use the existing `bridge.ToStarlarkStruct` after a `bridge.FromStarlarkValue` round-trip** (see Investigation 5).
- Mock registry storage shape — recommend layered slice (file frame + per-test frame), see Investigation 6.
- Multi-failure accumulation — keep `starlarktest` library default per D5-F2.
- Cross-file parallelization in `skytime test` — see Investigation 12 / Open Questions.
- JSON output record structs — mirror `go test -json` exactly per D5-E2.

### Deferred Ideas (OUT OF SCOPE — do NOT plan, do NOT research)

- Cross-extension wildcards (`extension="*"`).
- Per-kwarg type matching beyond strings (D5-B6 limit).
- JUnit XML output (gotestsum can convert from `--format=json`).
- Snapshot testing (compare `tester.run` to a checked-in JSON snapshot).
- Fixture frameworks (pytest-style `@fixture` / `conftest.py`).
- Mock-call assertion (`tester.assert_called(extension=, op=, times=)`).
- Per-test `--debug` (Phase 4's `--debug` covers it at runner level).
- `tester.replay(...)` standalone builtin (D5-D1 always-on supersedes).
- Live progress block for tests (Phase 4's live block is for one long-running flow; tests are short, sequential).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description (from REQUIREMENTS.md) | Research Support |
|----|-----------------------------------|------------------|
| **TEST-01** | `temporal_test` Starlark builtin module exposes `tester.workflow(...)`, `tester.mock_action(extension=, op=, mock_fn=)`, `tester.run(flow=)` from `.star` test files | Investigations 4 (Module pattern), 9 (parser two-mode); use `starlarkstruct.Module` family registered in a NEW parse-mode-gated globals dict. |
| **TEST-02** | A Starlark mock function executes in the *same* restricted predeclared environment as production lambdas; the bridge intercepts `ExecuteBatch` in `testsuite.TestWorkflowEnvironment` and routes per-action calls back to the Starlark mock | Investigations 1 (`OnActivity(...).Return(callbackFn)`), 5 (output mapping), 6 (precedence); `MockCallWrapper.Return` accepts a Go function whose signature exactly matches the activity. |
| **TEST-03** | `attempt` count passed to mocks as an explicit argument so `.star` tests can simulate transient failures and assert Temporal's retry behavior without leaving Starlark | Investigation 1 — the `.Return(callbackFn)` callback is invoked **once per activity attempt**; the harness keys `(flow, step, action_idx)` → counter and increments per call. |
| **TEST-04** | Replay helper runs each test twice and diffs the resulting Temporal event history; any divergence fails the test | Investigation 2 — `TestWorkflowEnvironment` has NO `GetWorkflowHistory`; the existing `runOnceCapturing` instead diffs **logger event records** captured via `ts.SetLogger(cap)`. Phase 5 lifts that pattern; structural fidelity comes from the interpreter's own slog events (`step_dispatch`/`step_complete`/`branch`/`result_bound`/`flow_complete`) PLUS `SetOnActivityStartedListener`/`SetOnActivityCompletedListener` to record activity-boundary events with their args/results. |
| **TEST-05** | `assert.*` builtins from `go.starlark.net/starlarktest` are available inside test `.star` files; harness reports failures into Go's `*testing.T` so they're CI-visible | Investigation 3 — confirmed signatures: `LoadAssertModule() (StringDict, error)`, `SetReporter(*Thread, Reporter)`, `Reporter` is `interface{ Error(args ...any) }` and `*testing.T` satisfies it directly. |
| **CLI-03** | `skytime test <dir>` discovers `.star` test files, runs the Tier 3 harness, reports pass/fail with Starlark callsite errors — NO Go stack traces in default output | Investigations 10 (`go test -json` schema), 11 (firewall), 13 (CLI integration); follow `pkg/cli/run.go` shape; new `pkg/cli/test.go` + `cmd/skytime/test.go`; firewall extension. |
</phase_requirements>

## Project Constraints (from CLAUDE.md)

- **Tech stack fixed**: Go 1.25 + `go.starlark.net@latest` + `go.temporal.io/sdk@v1.42.0`. No new languages, no DSLs.
- **Architecture (parse/execute split)**: Phase 5 STRADDLES the split — `tester.*` builtins are parse-time globals (registered only in test-mode), but the mocks they register are CONSUMED at execution time inside `TestWorkflowEnvironment`. The bridge must NOT smuggle `workflow.Context` into Starlark threads or `*starlark.Thread` across the activity boundary.
- **No string compilation, no dynamic activities, no context bleed.** `tester.run` MUST drive the SAME `SkytimeWorkflow` from `pkg/interpreter/workflow.go` — no parallel workflow type.
- **Determinism**: Mock dispatch resolution and Starlark mock-lambda evaluation must be replay-deterministic. Mock callbacks run on a SEPARATE goroutine from the workflow (per `OnActivity` docstring lines 420-421); this is concurrency-safe from the workflow's POV because the workflow blocks on `ExecuteActivity.Get` — but the mock callback's body must be deterministic per-attempt (no `time.Now()`, no `rand`, no map iteration without sort).
- **Security**: `Secret` is type-protected (Phase 2); the `_credential_id` exposure in mock kwargs (D5-C1a) is a STRING ID, never a `Secret`. The `extension.Credential` itself is NEVER passed to mock lambdas.
- **`workflow.Go` only**: Already enforced project-wide. The harness adds NO new concurrency in workflow code (mock callback bodies run in the activity goroutine which is plain Go).
- **GSD Workflow Enforcement**: All edits go through `/gsd:execute-phase` — applies during Phase 5 execution, not research.

## Summary

Phase 5 builds `pkg/testing` so consultants write Tier-3 E2E tests in `.star` files. Eight load-bearing pieces:

1. A new **parse-mode flag** on `pkg/parser` (or sibling `ParseTestFile` entrypoint) that enables a `tester` `*starlarkstruct.Module` in parse-time globals; the production parse path is unchanged. Investigation 4 + 9 confirm both options work; the planner picks. **Recommendation: add a `WithTestMode()` parser option** — minimal API surface, zero risk to production parse path.
2. A **mock registry** keyed by `(extension, op)` with a 3-tier specificity ladder (D5-B4) and layered scoping (file frame + per-test frame, D5-A4). Investigation 6 — no off-the-shelf library is suitable; build directly (~120 LOC).
3. A **mock router** in `pkg/testing` that wires `env.OnActivity("ExecuteBatch", mock.Anything, mock.Anything).Return(callbackFn)` where `callbackFn` has the EXACT signature of the production activity (`func(context.Context, []*dag.ActionRef) (dag.ActionResults, error)`). The Go callback iterates the batch, looks up each `(extension, op)` mock, calls back into Starlark via `bridge.CallLambda`, converts the result, and assembles a `dag.ActionResults` slice. Investigations 1 + 5.
4. A **`tester.run` driver** that constructs an in-memory `interpreter.FlowRegistry`, registers the parsed flow at the same content-hash the parser computed, freezes the registry, and calls `env.ExecuteWorkflow(NewWorkflow(registry), dag.WorkflowInput{...})` — reusing Phase 3's production execution path verbatim.
5. A **replay helper** (`pkg/testing.RunOnceCapturing`) lifted from `pkg/interpreter/replay_determinism_test.go` — exposes the existing `eventCapturingLogger` + `serializeRecords` machinery as a public helper. The diff scope (D5-D4) is the existing slog event stream PLUS captured activity-boundary events from `SetOnActivityStartedListener` / `SetOnActivityCompletedListener`. Investigation 2 — `TestWorkflowEnvironment` has NO `GetWorkflowHistory()` method; this is the only viable approach.
6. **`starlarktest` wiring** — `LoadAssertModule()` injected into the test-mode parse globals; `SetReporter(thread, subtestT)` called inside each `t.Run("test_<name>", ...)`. Investigation 3 confirms `*testing.T` satisfies `Reporter` directly.
7. A **`def test_*` discovery** mechanism — after `parser.ParseFile`, enumerate the file's top-level Starlark globals (the `starlark.StringDict` returned from `ExecFileOptions`), filter on `*starlark.Function` whose name starts with `test_`. Investigation 4.
8. A **`skytime test <dir>` cobra subcommand** in `pkg/cli/test.go` mirroring `pkg/cli/run.go` shape; `--run`, `--format=json`, `--verbose`, `--debug` flags; firewall extension to allow `pkg/testing` to import `go.temporal.io/sdk/testsuite` (NOT `go.temporal.io/sdk/activity`). Investigations 11 + 13.

**Primary recommendation:** Implement in waves: W0 = type spine + mock registry; W1 = router + I/O contract + builders; W2 = `tester.workflow` + `tester.run` + replay diff + assert wiring; W3 = parser test-mode + discovery + interpolation; W4 = `skytime test` CLI + JSON output + firewall.

## Standard Stack

### Core (already in `go.mod`; NO new deps required)

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `go.temporal.io/sdk/testsuite` | v1.42.0 (transitive via `go.temporal.io/sdk`) | `WorkflowTestSuite`, `TestWorkflowEnvironment` (alias for `internal.TestWorkflowEnvironment`) | Already used by Phase 3 — every `walk_*_test.go` builds an env. v1.42 confirmed against `go.temporal.io/sdk@v1.42.0/internal/workflow_testsuite.go`. |
| `go.temporal.io/sdk/log` | v1.42.0 | `log.Logger` interface (Debug/Info/Warn/Error) — what `ts.SetLogger(cap)` accepts | Already used in `test_helpers_test.go::eventCapturingLogger`. The interpreter logs via `workflow.GetLogger(ctx)` which routes through `ts.SetLogger`. NOT `log/slog`. |
| `go.starlark.net/starlarktest` | same module pseudo-version as `go.starlark.net` (`v0.0.0-20260326113308-fadfc96def35`) | `LoadAssertModule()`, `SetReporter()`, `GetReporter()`, `Reporter` interface | Already in module tree (transitive). Verified via WebFetch. |
| `go.starlark.net/starlarkstruct` | same | `*starlarkstruct.Module` — `tester` namespace value with `workflow`/`mock_action`/`run` attributes | Phase 1 D-08 pattern; mirrors `pkg/extension/builtin/http.Initialize`. |
| `github.com/stretchr/testify/mock` | v1.11.1 | `mock.Anything`, `mock.Arguments` — needed for the `MockCallWrapper.Return(callbackFn)` callback signature | Already imported in `walk_step_actionfn_test.go`. |

### Test-only (already in `go.mod`)

| Library | Version | Purpose |
|---------|---------|---------|
| `github.com/stretchr/testify/require`, `assert` | v1.11.1 | Standard test assertions in `pkg/testing` Go-side tests. |

### Alternatives Considered (rejected)

| Instead of | Could Use | Tradeoff (rejected) |
|------------|-----------|---------------------|
| `MockCallWrapper.Return(callbackFn)` | `MockCallWrapper.Run(handlerFn)` + a static `Return(...)` | `Run(fn)` runs BEFORE the static return; you'd still need a static value. The `Return(fn)` form lets the callback compute the result dynamically — exactly D5-C2's need. SDK docstring (lines 397-409) explicitly endorses this form. |
| Diffing real Temporal event history | `runOnceCapturing` over slog records + activity listeners | `TestWorkflowEnvironment` has NO `GetWorkflowHistory()` (verified by exhaustive grep over the v1.42 source). The slog-record approach is what Phase 04.2 already uses and proves out. |
| Custom `tester` Go-side test runner | `*testing.T` + `t.Run` | D5-F1 locks `t.Run`. Stretching anything else would forfeit native Go test integration. |
| Building cross-file parallelization with goroutine pools | Sequential file iteration | D5-E5 leaves cross-file parallelism to planner discretion; **recommendation: sequential v1**. Activity mock state is per-`TestWorkflowEnvironment`, not shared, so each file gets its own env — but if multiple files run concurrently, slog-default-logger contention surfaces. Sequential is cheaper and removes a flake class. Re-evaluate if v1.x test suites grow past 10 files. |

### Installation

No new module dependencies. Verify via:
```bash
cd /Users/mikel/dev/ai/temporero
go list -m go.starlark.net go.temporal.io/sdk github.com/stretchr/testify
# Already pinned: go.starlark.net v0.0.0-20260326113308-fadfc96def35,
#                  go.temporal.io/sdk v1.42.0,
#                  github.com/stretchr/testify v1.11.1
```

### Version Verification

| Package | Method | Result |
|---------|--------|--------|
| `go.temporal.io/sdk@v1.42.0` | Source-cache grep at `$GOMODCACHE/go.temporal.io/sdk@v1.42.0/internal/workflow_testsuite.go` | HIGH — `OnActivity`, `MockCallWrapper.Return`, `SetLogger`, `SetOnActivityStartedListener`, `SetOnActivityCompletedListener` all present and have the documented signatures. |
| `go.starlark.net/starlarktest` | WebFetch from pkg.go.dev | HIGH — `LoadAssertModule()`, `SetReporter`, `Reporter{ Error(args ...any) }`. `*testing.T` satisfies `Reporter` per docs. |
| `go test -json` schema | Stdlib `cmd/test2json` docs (referenced in CONTEXT.md) | HIGH — see Investigation 10. |

## Architecture Patterns

### Recommended Project Structure

```
pkg/
├── testing/                       # NEW — Phase 5 owns this package
│   ├── doc.go                     # Package overview + parse/execute split note
│   ├── module.go                  # tester *starlarkstruct.Module factory + builders
│   ├── builtin_workflow.go        # tester.workflow(...) — captures WorkflowSpec
│   ├── builtin_mock_action.go     # tester.mock_action(...) — registers MockEntry
│   ├── builtin_run.go             # tester.run(flow=) — drives TestWorkflowEnvironment
│   ├── registry.go                # MockRegistry: layered (file frame, per-test frame); 3-tier match (D5-B4)
│   ├── router.go                  # buildExecuteBatchCallback — Go func whose signature matches ExecuteBatch
│   ├── builders.go                # ok / err / nonretryable predeclared in mock-lambda env
│   ├── output.go                  # Starlark dict → *starlarkstruct.Struct via bridge round-trip (D5-C3)
│   ├── attempts.go                # per-(flow,step,action) attempt counter (D5-C1, TEST-03)
│   ├── replay.go                  # RunOnceCapturing public helper (lifted from interpreter)
│   ├── replay_diff.go             # First-divergent-event diff with payload before/after (D5-D2/D5-D3/D5-D4)
│   ├── discover.go                # ParseTestFile + def test_* enumeration (D5-A1, D5-A2)
│   ├── runner.go                  # Run(t *testing.T, dir string, opts ...Option) — public API
│   ├── reporter.go                # starlarktest reporter wiring (D5-F1, TEST-05)
│   ├── tester_test.go             # White-box tests
│   └── e2e_test.go                # End-to-end: real .star fixtures under testdata/
│
├── parser/                        # MODIFIED — additive only
│   ├── globals.go                 # newParseTimeGlobals — extended to take a parseMode flag
│   ├── options.go                 # WithTestMode() option (sets a Parser.testMode bool)
│   └── ...                        # everything else unchanged
│
├── cli/                           # MODIFIED — additive only
│   ├── test.go                    # NEW — newTestCommand(cfg) cobra subcommand
│   └── root.go                    # MODIFIED — root.AddCommand(newTestCommand(cfg))
│
└── interpreter/                   # MODIFIED — minor (export the test-helper bits)
    ├── replay_helper.go           # NEW — lifts runOnceCapturing from replay_determinism_test.go to a public helper (or move to pkg/testing/replay.go directly; either keeps the public API surface clean)

cmd/skytime/
└── test.go                        # NEW — wires pkg/cli newTestCommand

tests/
└── firewall_testsuite_test.go     # NEW — firewall: pkg/testing MAY import go.temporal.io/sdk/testsuite, MUST NOT import go.temporal.io/sdk/activity. Sibling firewall test for the new allow-list entry.
```

### Pattern 1: `*starlarkstruct.Module` as `tester` namespace

**What:** `tester` is a top-level parse-time global value (NOT namespaced — D-08 lifecycle); attribute access (`tester.workflow`, `tester.mock_action`, `tester.run`) returns a `*starlark.Builtin`.

**When to use:** Whenever a feature exposes multiple related builtins under one name. Production parse-time globals like `flow`, `step`, etc. are NAKED (per PARSE-01); but those are 6 single-purpose builtins. `tester` is a NEW related-builtin family — `*starlarkstruct.Module` is the right shape.

**Example (verified pattern from `pkg/extension/builtin/http/http.go::Initialize`):**

```go
// pkg/testing/module.go
//
// Source: pattern from pkg/extension/builtin/http/http.go::Initialize +
// pkg/parser/globals.go HasAttrs check
package testing

import (
    "go.starlark.net/starlark"
    "go.starlark.net/starlarkstruct"
)

// NewTesterModule returns the `tester` namespace value bound to a per-parse
// MockRegistry + a call-site WorkflowSpec slot. The module is INSTANCE-PER-PARSE
// (one per .star test file) — file-scope mocks live on the registry, per-test
// frames are pushed/popped by the runner.
func NewTesterModule(reg *MockRegistry, ws *WorkflowSpec) starlark.Value {
    return &starlarkstruct.Module{
        Name: "tester",
        Members: starlark.StringDict{
            "workflow":    starlark.NewBuiltin("workflow", builtinTesterWorkflow(ws)),
            "mock_action": starlark.NewBuiltin("mock_action", builtinTesterMockAction(reg)),
            "run":         starlark.NewBuiltin("run", builtinTesterRun(reg, ws)),
        },
    }
}
```

The HasAttrs gate in `pkg/parser/globals.go:74-80` accepts `*starlarkstruct.Module` (it implements `starlark.HasAttrs`).

### Pattern 2: `OnActivity(...).Return(callbackFn)` — the dynamic-mock surface

**What:** `MockCallWrapper.Return` accepts EITHER static return values OR a Go function whose signature exactly matches the activity. When called with a function, the SDK's mock dispatch invokes that function with the activity's args and uses its return values as the activity result.

**When to use:** Wherever per-call mock logic is required (D5-C1's `attempt` argument, D5-B4's match-precedence routing). Phase 5's entire mock router is one such callback.

**Example (verified pattern from SDK source, `internal/workflow_testsuite.go:396-422`):**

```go
// pkg/testing/router.go
//
// Source: docstring at workflow_testsuite.go:397-409
//
//   "You can mock it by return a function with exact same signature:
//      t.OnActivity(MyActivity, mock.Anything, mock.Anything).Return(func(...) (...) {
//        // your mock function implementation
//        return "", nil
//      })"

// buildExecuteBatchCallback returns a Go function whose signature exactly
// matches Activity.ExecuteBatch in pkg/activity/execute_batch.go (the
// production signature is `func(ctx context.Context, batch []*dag.ActionRef)
// ([]dag.ActionResult, error)`). The callback iterates the batch, looks up
// each (extension, op) in the registry, calls back into Starlark via
// bridge.CallLambda, and assembles dag.ActionResults.
func buildExecuteBatchCallback(reg *MockRegistry, attempts *AttemptCounter, parsed *interpreter.ParsedFlow) func(context.Context, []*dag.ActionRef) ([]dag.ActionResult, error) {
    return func(ctx context.Context, batch []*dag.ActionRef) ([]dag.ActionResult, error) {
        results := make(dag.ActionResults, 0, len(batch))
        for idx, ref := range batch {
            attempt := attempts.NextFor(ref) // 1-indexed per (flow, step, action_idx)
            entry, found := reg.Match(ref)
            if !found {
                // D5-B2: NonRetryableErr with flow callsite + step name
                return nil, extension.WrapNonRetryable(fmt.Errorf(
                    "no mock for %s at %s (step %q)",
                    ref.Kind_, ref.Pos, /* step name from parsed.Flow walk */ ""))
            }
            result, err := evalMockLambda(entry.Lambda, ref.Kwargs, attempt, ref.CredentialID)
            if err != nil {
                return results, err
            }
            // result is one of OkResult / RetryableErrResult / NonRetryableErrResult
            results = append(results, withIdx(result, idx))
        }
        return results, nil
    }
}
```

**Critical semantics note (line 420-421):** *"Mock callbacks here are run on a separate goroutine than the workflow and therefore are not concurrency-safe with workflow code."* The callback runs in the **activity goroutine** — same place the production `ExecuteBatch` runs — so calling back into Starlark via `bridge.CallLambda` is safe (this is exactly what production does too). The workflow goroutine is blocked on `ExecuteActivity.Get`, so there's no concurrent access to workflow state.

### Pattern 3: Replay-determinism via captured slog records (NOT real event history)

**What:** `TestWorkflowEnvironment` has NO `GetWorkflowHistory()` method (verified by exhaustive grep over the v1.42 source — see Investigation 2). The existing replay-determinism mechanism in Phase 04.2 captures **slog records emitted by the interpreter** via `ts.SetLogger(cap)` and byte-compares two runs.

**When to use:** Always (D5-D1 always-on). Phase 5 lifts the existing `runOnceCapturing` into a public helper.

**Example (verified pattern from `pkg/interpreter/replay_determinism_test.go:34-61`):**

```go
// pkg/testing/replay.go
//
// Source: pkg/interpreter/replay_determinism_test.go::runOnceCapturing
// + pkg/interpreter/test_helpers_test.go::eventCapturingLogger

// RunOnceCapturing executes parsed against a fresh testsuite +
// eventCapturingLogger. Returns captured records, final state (nil on error),
// and any workflow error.
func RunOnceCapturing(parsed *interpreter.ParsedFlow, hash string, init map[string]any, mockCallback ExecuteBatchFunc) (*EventCapture, map[string]any, error) {
    cap := newEventCapture()

    registry := interpreter.NewRegistry()
    registry.Register(parsed.Flow.Name, hash, parsed)
    registry.Freeze()

    var ts testsuite.WorkflowTestSuite
    ts.SetLogger(cap)
    env := ts.NewTestWorkflowEnvironment()

    // Activity-boundary capture supplements the interpreter's own slog
    // events with start/complete records carrying ActivityInfo + args.
    env.SetOnActivityStartedListener(cap.onActivityStarted)
    env.SetOnActivityCompletedListener(cap.onActivityCompleted)

    // Register a fake activity by name so OnActivity(callback) can target it.
    fake := func(_ context.Context, _ []*dag.ActionRef) ([]dag.ActionResult, error) { return nil, nil }
    env.RegisterActivityWithOptions(fake, activity.RegisterOptions{Name: "ExecuteBatch"})

    // The DYNAMIC mock callback; same per-attempt counter survives both runs
    // because attempts is keyed by (flow, step, action_idx) — replay-equality
    // means both runs produce identical (kind, pos, idx) triplets.
    env.OnActivity("ExecuteBatch", mock.Anything, mock.Anything).Return(mockCallback)

    wf := interpreter.NewWorkflow(registry)
    env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})
    env.ExecuteWorkflow(wf, dag.WorkflowInput{
        FlowName: parsed.Flow.Name, ContentHash: hash, InitState: init,
    })

    var out map[string]any
    wfErr := env.GetWorkflowError()
    if wfErr == nil {
        env.GetWorkflowResult(&out)
    }
    return cap, out, wfErr
}
```

**Diff scope:** `serializeRecords(cap.snapshot())` returns a stable byte-string. `replay_diff.go::FirstDivergentEvent(run1, run2)` finds the first record whose serialized form differs and returns the `(idx, before, after)` tuple plus the `dag.ActionRef.Pos` from the corresponding `step_dispatch` event for D5-D3 attribution.

### Pattern 4: `def test_*` discovery via Starlark module enumeration

**What:** After `parser.ParseFile` (in test mode), the interpreter has a `starlark.StringDict` of top-level globals. Filter on `*starlark.Function` whose name starts with `test_`. (The parser does NOT currently expose this map publicly — see Investigation 4 for the cleanest exposure path.)

**When to use:** D5-A1 — `def test_<name>():` is the test declaration shape; the runner enumerates these per file.

**Example (idiomatic pattern):**

```go
// pkg/testing/discover.go
//
// Source: idiomatic Starlark enumeration; verified against
// go.starlark.net/starlark.StringDict shape

func discoverTests(globals starlark.StringDict) []TestFunc {
    // sort.Strings on keys before iteration — replay-determinism /
    // workflowcheck pattern even though we're outside workflow code.
    keys := make([]string, 0, len(globals))
    for k := range globals {
        keys = append(keys, k)
    }
    sort.Strings(keys)

    var tests []TestFunc
    for _, name := range keys {
        if !strings.HasPrefix(name, "test_") {
            continue
        }
        fn, ok := globals[name].(*starlark.Function)
        if !ok {
            continue // shadowed by a non-function value; ignore
        }
        // D5-A1: top-level def only (NumParams check optional — sig should be ()).
        if fn.NumParams() != 0 {
            continue // skip helpers like test_helper(x); a v1.x linter could warn
        }
        tests = append(tests, TestFunc{Name: name, Fn: fn})
    }
    return tests
}
```

### Anti-Patterns to Avoid

- **Calling `tester.run` from outside a `def test_*()`** — registers a workflow at file scope, runs once at parse time. PARSE: reject with a clear error in `builtinTesterRun` if `thread.CallStack()` shows we're at file top-level, NOT inside a `*starlark.Function` frame.
- **Using `mock.Anything, mock.Anything, mock.Anything`** (3 args) for `OnActivity("ExecuteBatch", ...)`. The activity has TWO args (`ctx`, `[]*dag.ActionRef`); pass exactly two `mock.Anything` matchers. Wrong arity panics at workflow-execute time.
- **Caching the `*starlark.Thread` across `def test_*()` invocations** — use a fresh thread per test (Pitfall #1 from Phase 1; D5-F1 says "one sub-T per def test_* via t.Run", which implies one thread per subtest invocation).
- **Building a parallel `SkytimeWorkflow_test`** — `tester.run` MUST drive the SAME `interpreter.NewWorkflow(registry)` from production. A parallel workflow type would mean the harness tests something other than what runs in prod, violating the whole point of Tier-3.
- **Returning a `*starlarkstruct.Struct` (not a `map[string]any`) from the mock callback to the activity boundary** — the activity result crosses Temporal's data converter (JSON in v1). `*starlarkstruct.Struct` has no `MarshalJSON`. The harness MUST convert `Starlark dict → map[string]any` via `bridge.FromStarlarkValue` BEFORE assembling `dag.OkResult{Output: ...}`. The `*starlarkstruct.Struct` shape lives **inside the workflow**, not on the wire. (See Investigation 5.)
- **Re-implementing replay-determinism logic** — `pkg/interpreter/replay_determinism_test.go::runOnceCapturing` already does it. Lift the helper, don't fork.
- **Shadowing the `fail()` predeclared in the mock-lambda env** — D4.2-05 already locks `fail()` as the dual parse-time/lambda-time global. The mock-lambda env extends `lambdaTimeGlobals` (which already contains `fail`). Adding `nonretryable(msg=)` does NOT replace `fail` — they coexist; `nonretryable` is sugar for "I want to assert non-retryable", while `fail("msg")` inside a mock lambda still works the way it does in production (raises NonRetryableErr).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Bridging Starlark mock returns to Temporal activity results | A custom JSON encoder for Starlark dicts | `bridge.FromStarlarkValue(starlark.Value) (any, error)` (existing) | Already supports NoneType / String / Int / Float / Bool / `*List` / `*Dict` / `*starlarkstruct.Struct`. Phase 1 verified determinism (sorted keys). The mock callback returns a Go `map[string]any` that Temporal's JSON DataConverter handles natively. |
| Wrapping the mock dict as `*starlarkstruct.Struct` for downstream `ctx.step_output.<key>` access | A new `StructFromDict` exporter in `pkg/bridge` | `bridge.ToStarlarkStruct(map[string]any) (*starlarkstruct.Struct, error)` (existing, exported) | The wrapping happens **inside the production walk_step path** when the `dag.OkResult.Output` payload is read. Phase 5's mock callback emits the `map[string]any` form; the existing interpreter code then converts to a struct on read. **No new bridge export needed.** This contradicts CONTEXT.md's note about `bridge.StructFromDict` — the planner must verify, but evidence in `/pkg/bridge/struct.go` and `/pkg/bridge/value.go` is unambiguous: `ToStarlarkStruct` is the exposed function, `StructFromDict` does NOT exist. |
| Reporter for `assert.*` failures | A custom Reporter struct | `*testing.T` (already satisfies `starlarktest.Reporter`) | Investigation 3 verified: `Reporter` is `interface{ Error(args ...any) }`; `*testing.T.Error(args ...any)` matches. Pass the subtest's `*testing.T` directly to `SetReporter`. |
| Per-attempt counter | A complex sync.Map | `map[ActionKey]int` + a single mutex; key = `{FlowName, StepIdx, ActionIdx}` | Mock callback runs in the activity goroutine; only ONE activity goroutine for ExecuteBatch at a time per workflow. Mutex is defensive. ~30 LOC. |
| Sub-test runner | A custom `Reporter` that writes to stdout | `*testing.T.Run` from stdlib `testing` | D5-F1 locks this. `t.Run` already provides per-subtest pass/fail isolation, JSON output integration via `go test -json`, and `*testing.T` chaining. |
| Test discovery | Regex over file source | `parser.ParseFile` (in test-mode) → enumerate `starlark.StringDict` globals | The parser already produces the AST; discovery happens AFTER successful parse. Regex would re-parse the file and miss `def`s nested in `if True:` etc. (rare but valid Starlark). |
| `go test -json` JSON shape | A bespoke event schema | Mirror stdlib `cmd/test2json` exactly | D5-E2 locks this. `gotestsum`, `tparse`, GitHub Actions test annotations all parse this schema. Inventing a new schema = reinventing the CI ecosystem. |
| Mock specificity ladder | A heavy DSL like `gomock` matchers | A 3-tier slice scan (~80 LOC) | The match key set is small (extension, op, optional kwargs-regex). gomock-style matchers solve a vastly more general problem. testify/mock matchers are designed for `mock.On(...)` not for a custom registry. Build directly. |
| Parser test-mode toggle | A new `ParseTestFile` entrypoint with copy-pasted parser body | `WithTestMode()` `Option` setting `Parser.testMode bool`; `newParseTimeGlobals` checks the flag and conditionally adds `tester` | The parser already accepts options (`WithRoot`, `WithExtensions`, `WithMaxBlockSize`). One more is the established convention. Avoids 200 LOC of duplication. |

**Key insight:** Phase 5 is the ONE phase that's mostly composition. Almost every primitive — bridge conversion, parser globals, lambda eval, replay determinism, `*testing.T` integration, `go test` JSON shape, cobra subcommand pattern — already exists. The new code is the GLUE: a `tester` Module, a mock registry, a router callback, and a CLI subcommand. Phase 5 should be small (~1500 LOC across `pkg/testing/`) by leaning on existing surfaces.

## Investigation Areas

> One subsection per critical research focus area listed in the prompt's `<additional_context>`. Concrete API references, file:line citations, and recommended approaches.

### Investigation 1: `testsuite.TestWorkflowEnvironment.OnActivity` callback semantics

**Confidence:** HIGH

**Key finding:** `MockCallWrapper.Return(callbackFn)` accepts a Go function whose signature **exactly** matches the mocked activity. The SDK invokes that function with the actual activity args and uses its return values as the activity result.

**Source:** `$GOMODCACHE/go.temporal.io/sdk@v1.42.0/internal/workflow_testsuite.go`

- Lines 396-422: `OnActivity` docstring — "The supplied parameters to the Return() call should either be a function that has exact same signature as the mocked activity, or it should be mock values with the same types as the mocked activity function returns." Provides the canonical example with `func(ctx context.Context, msg string) (string, error) {...}`.
- Lines 832-835: `MockCallWrapper.Return(returnArguments ...interface{}) *MockCallWrapper` — passes through to `c.call.Return(returnArguments...)`. testify/mock's `.Return` accepts a function as a single variadic arg and detects it via reflection.
- **Lines 420-421 (CRITICAL):** *"Mock callbacks here are run on a separate goroutine than the workflow and therefore are not concurrency-safe with workflow code."*

**For Phase 5:**

- The callback signature MUST be `func(context.Context, []*dag.ActionRef) ([]dag.ActionResult, error)` — exactly matches `pkg/activity/execute_batch.go::ExecuteBatch`. The exact return TYPES matter; `dag.ActionResults` (the typed-slice alias used in `walk_step.go:54`) is NOT an alias acceptable here — must be `[]dag.ActionResult`. Round-trip happens through Temporal's JSON DataConverter.
- **Per-attempt invocation:** The callback is invoked ONCE per activity attempt. Temporal's RetryPolicy fires when the callback returns a non-nil error that isn't a `temporal.NewNonRetryableApplicationError`. The harness's attempt counter increments **on every call to the callback** for the matching `(flow, step, action_idx)` — that's exactly what TEST-03 needs.
- **Concurrency:** the callback runs in the activity goroutine. Calling `bridge.CallLambda` (which uses a fresh `*starlark.Thread`) is safe — same setup as production `ExecuteBatch`. The mock callback MUST NOT touch `workflow.Context` or workflow-side state (it's not in scope anyway — the callback receives `context.Context`).
- **Panics:** A panic in the callback propagates as a Go panic in the activity goroutine, which Temporal's activity worker catches and surfaces as a generic `*temporal.ActivityError`. The harness should `recover()` inside the callback and convert to a `dag.NonRetryableErrResult` so the failure surfaces with Starlark callsite info, not a Go panic dump.
- **`OnActivity` lookup by string:** Lines 435-443 — when `activity` is a `string`, the registry must already have an activity registered under that name. Phase 5's harness MUST register a fake `ExecuteBatch` first via `env.RegisterActivityWithOptions(fake, activity.RegisterOptions{Name: "ExecuteBatch"})` (mirrors `helperRegisterFakeExecuteBatch` in `walk_step_test.go:28`).

**Recommended approach:** Build `pkg/testing/router.go::buildExecuteBatchCallback` as shown in Pattern 2 above. Wire via:

```go
env.RegisterActivityWithOptions(fakeExecuteBatch, activity.RegisterOptions{Name: "ExecuteBatch"})
env.OnActivity("ExecuteBatch", mock.Anything, mock.Anything).Return(buildExecuteBatchCallback(reg, attempts, parsed))
```

### Investigation 2: Replay-history capture in `TestWorkflowEnvironment`

**Confidence:** HIGH (with surprise: there is NO real history API)

**Key finding:** `TestWorkflowEnvironment` does **NOT** expose a `GetWorkflowHistory()` method. Verified by exhaustive grep over `$GOMODCACHE/go.temporal.io/sdk@v1.42.0/internal/workflow_testsuite.go`:

```
$ grep -n "func (e \*TestWorkflowEnvironment) " workflow_testsuite.go
# 50+ methods returned; NONE matches GetWorkflowHistory.
```

The methods that DO exist for inspecting workflow execution:
- `GetWorkflowResult(valuePtr interface{}) error` (line 1123) — final return value.
- `GetWorkflowError() error` (line 1148) — workflow-level error if any.
- `IsWorkflowCompleted() bool` — boolean termination check.
- `RegisterDelayedCallback(callback func(), delay time.Duration)` (line 1235) — schedule a callback at a workflow-clock instant; used for time-based assertions, NOT history extraction.
- `SetOnActivityStartedListener(listener func(*ActivityInfo, context.Context, converter.EncodedValues))` (line 983) — INVOKED at every activity start with the activity's input args.
- `SetOnActivityCompletedListener(listener func(*ActivityInfo, converter.EncodedValue, error))` (line 992) — INVOKED at every activity completion.
- `SetOnChildWorkflowStartedListener` / `SetOnChildWorkflowCompletedListener` — equivalents for child workflows.

**For Phase 5:**

The existing `pkg/interpreter/replay_determinism_test.go::runOnceCapturing` does NOT use any of these — it captures **interpreter-emitted slog records** via `ts.SetLogger(cap)` where `cap` implements `go.temporal.io/sdk/log.Logger`. The test_helpers `eventCapturingLogger` (`pkg/interpreter/test_helpers_test.go:97-120`) is mutex-guarded so `-race` accepts concurrent emission.

The existing `serializeRecords` helper (`test_helpers_test.go:165-180`) byte-stably renders captured records for diff. This is the production-vetted pattern; Phase 5 lifts it as-is.

**To strengthen "Temporal event history" semantics (D5-D4)**, supplement slog records with activity-boundary records via:

```go
// pkg/testing/replay.go
env.SetOnActivityStartedListener(func(info *activity.Info, ctx context.Context, args converter.EncodedValues) {
    var batch []*dag.ActionRef
    args.Get(&batch)
    cap.appendActivity("activity_started", info, batch, nil)
})
env.SetOnActivityCompletedListener(func(info *activity.Info, result converter.EncodedValue, err error) {
    var results dag.ActionResults
    result.Get(&results)
    cap.appendActivity("activity_completed", info, nil, results)
})
```

This gives Phase 5 the full "events + sequence + payload byte-equality" coverage D5-D4 mandates: structural divergence (extra/missing events), event-type divergence at a position, and payload divergence (kwargs ordering, time leakage).

**Recommended approach:**

1. Lift `runOnceCapturing` from the test file into `pkg/interpreter/replay_helper.go` as exported `RunOnceCapturing` — OR, more cleanly, build it fresh in `pkg/testing/replay.go` (it's a small helper, ~50 LOC, and depending on `pkg/interpreter` for it would create awkward layering).
2. Extend `eventCapturingLogger` (or a wrapping `EventCapture` struct in `pkg/testing`) to additionally record `SetOnActivityStartedListener`/`SetOnActivityCompletedListener` events.
3. The diff helper compares record-by-record after `serializeRecords`; the first divergent record's index → flow callsite via the corresponding `step_dispatch` event's `pos` attribute (interpreter already emits `dag.ActionRef.Pos.String()` per `walk_step.go::stepActionLabel`).

**Note for planner:** "lift into a public helper" in CONTEXT.md `<code_context>` is unambiguous about lifting; the current helper is in a `_test.go` file (not exported). Decide between (a) move to a non-test file in `pkg/interpreter` and export it, or (b) re-implement in `pkg/testing`. Option (b) is cleaner for layering but duplicates ~50 LOC. **Recommendation: option (a)** — export from `pkg/interpreter` as `interpreter.RunOnceCapturing` since the function fundamentally drives interpreter machinery, and the existing in-tree tests can switch to the public name in one diff.

### Investigation 3: `go.starlark.net/starlarktest` reporter wiring

**Confidence:** HIGH

**Verified API (via WebFetch from pkg.go.dev):**

```go
// go.starlark.net/starlarktest

// LoadAssertModule loads the assert module. Concurrency-safe and idempotent.
func LoadAssertModule() (starlark.StringDict, error)

// SetReporter associates an error reporter with the Starlark thread.
func SetReporter(thread *starlark.Thread, r Reporter)

// GetReporter returns the thread's error reporter. Must be preceded by SetReporter.
func GetReporter(thread *starlark.Thread) Reporter

// Reporter is satisfied by *testing.T.
type Reporter interface {
    Error(args ...any)
}

// DataFile resolves test-data resource paths.
var DataFile = func(pkgdir, filename string) string
```

**For Phase 5:**

- `*testing.T` directly satisfies `Reporter` (its `Error(args ...any)` method matches the signature exactly). NO adapter needed.
- `t.Run(name, func(subT *testing.T) {...})` creates a subtest; `subT` is a `*testing.T` and satisfies `Reporter`. Per-test reporter wiring:

```go
for _, test := range tests {
    test := test
    t.Run(test.Name, func(subT *testing.T) {
        thread := &starlark.Thread{Name: "test:" + filename + ":" + test.Name}
        starlarktest.SetReporter(thread, subT)
        // Run the test function in this thread.
        _, err := starlark.Call(thread, test.Fn, nil, nil)
        if err != nil {
            subT.Errorf("%s: %v", test.Name, err)
        }
    })
}
```

- **Per-test thread:** Create a fresh `*starlark.Thread` per subtest. This satisfies Pitfall #1 from Phase 1 ("fresh thread per call") AND lets each subtest's `Reporter` be isolated to its own subtest's `*testing.T`.

- **`assert.*` module loading:** Add `LoadAssertModule()` results to the test-mode parse-time globals dict so that `assert.eq(...)`, `assert.true(...)`, etc. resolve at parse time. Existing `pkg/parser/globals.go::newParseTimeGlobals` is the right injection point (gated by `Parser.testMode`).

- **Multi-failure accumulation (D5-F2 default):** `starlarktest`'s default behavior is that `assert.*` calls `Reporter.Error(...)` on each failure but does NOT abort the test function. `*testing.T.Error` records the failure but does NOT stop the test (unlike `t.Fatal`). Net effect: a `def test_*()` with two failing `assert.eq` calls accumulates two failures and continues; the subtest fails at the end. This is the desired v1 behavior per D5-F2.

- **Callsite format (D5-F3):** `starlarktest`'s default `assert.eq` failure message includes the Starlark file:line:col automatically (it constructs the error with `thread.CallStack().At(0).Pos`). The harness should NOT add Go-stack-trace info in default mode (CLI-03 forbids it). `--debug` flag (Phase 4 D4-19) is the escape hatch.

**Recommended approach:**

```go
// pkg/testing/reporter.go
//
// Source: go.starlark.net/starlarktest pkg.go.dev API surface

import "go.starlark.net/starlarktest"

// runOneTest creates a fresh starlark.Thread, wires the subtest's *testing.T
// as the starlarktest.Reporter, and invokes the def test_* function. Per
// D5-F1 and TEST-05.
func runOneTest(subT *testing.T, fn *starlark.Function, mockReg *MockRegistry) {
    thread := &starlark.Thread{Name: "test:" + fn.Position().Filename() + ":" + fn.Name()}
    thread.SetMaxExecutionSteps(bridge.DefaultMaxExecutionSteps)
    starlarktest.SetReporter(thread, subT)

    // Make tester.* available; mockReg is the per-test frame stack
    // (push frame on entry, pop on exit — D5-A4).
    mockReg.PushTestFrame()
    defer mockReg.PopTestFrame()

    if _, err := starlark.Call(thread, fn, nil, nil); err != nil {
        // Starlark error already includes file:line:col via *EvalError.
        // Pass through to subT.Error; D5-E1's renderer uses subT's
        // accumulated failures + this final error.
        subT.Error(err.Error())
    }
}
```

### Investigation 4: Starlark module introspection for `def test_*` discovery

**Confidence:** HIGH

**Key finding:** `starlark.ExecFileOptions` returns a `starlark.StringDict` of all top-level globals. The current `pkg/parser/parser.go::parse` discards this return value (`_, execErr := starlark.ExecFileOptions(...)`); Phase 5 needs it preserved when the parser is in test mode.

**Source:**

- `pkg/parser/parser.go:280` — `if _, execErr := starlark.ExecFileOptions(opts, thread, filename, execSrc, p.parseTimeGlobals); execErr != nil {` — the underscore drops the globals.
- `go.starlark.net/starlark.StringDict` is a `map[string]Value`; iteration order is randomized (Go map). `sort.Strings` on keys is the standard discipline.
- `*starlark.Function` has `.Name() string`, `.Position() syntax.Position`, `.NumParams() int` — sufficient for filtering and reporting.

**For Phase 5:**

Two clean exposure options for the planner:

1. **Capture in `Parser`**: Add a `Parser.testGlobals starlark.StringDict` field; the test-mode parse path stores the returned globals into it; `Parser.TestGlobals(filename string) starlark.StringDict` accessor. Production path unchanged.
2. **Sibling entrypoint `ParseTestFile`**: New method `(p *Parser) ParseTestFile(path string) (TestFileGlobals, error)` that runs the parse and returns globals + `ParsedFlow` together. Heavier API but more explicit.

**Recommendation: Option 1.** The parser is already a per-instance struct with various accessors (`Lambdas()`, `Flows()`, `FileBytes()`); one more accessor matches the established pattern.

```go
// pkg/parser/parser.go (additions)

// testGlobals[absPath] holds the top-level Starlark globals from ExecFileOptions
// when the parser is in test mode (WithTestMode()). Production parses don't
// populate it. Used by pkg/testing/discover.go to find def test_* functions.
testGlobals map[string]starlark.StringDict

// In parse():
if p.testMode {
    globals, execErr := starlark.ExecFileOptions(opts, thread, filename, execSrc, p.parseTimeGlobals)
    if execErr != nil { return nil, wrapStarlarkError(execErr) }
    p.testGlobals[filename] = globals
} else {
    if _, execErr := starlark.ExecFileOptions(opts, thread, filename, execSrc, p.parseTimeGlobals); execErr != nil {
        return nil, wrapStarlarkError(execErr)
    }
}
```

```go
// pkg/parser/parser.go (accessor)
func (p *Parser) TestGlobals(filename string) (starlark.StringDict, bool) {
    g, ok := p.testGlobals[filename]
    return g, ok
}
```

**Discovery filter:**

```go
// pkg/testing/discover.go
func DiscoverTests(globals starlark.StringDict) []TestFunc {
    keys := make([]string, 0, len(globals))
    for k := range globals { keys = append(keys, k) }
    sort.Strings(keys) // determinism
    var out []TestFunc
    for _, name := range keys {
        if !strings.HasPrefix(name, "test_") { continue }
        fn, ok := globals[name].(*starlark.Function)
        if !ok || fn.NumParams() != 0 { continue }
        out = append(out, TestFunc{Name: name, Fn: fn, Pos: fn.Position()})
    }
    return out
}
```

### Investigation 5: `*starlarkstruct.Struct` round-trip through Temporal serialization

**Confidence:** HIGH

**Key finding (CONTRADICTS CONTEXT.md note about `bridge.StructFromDict`):**

- `pkg/bridge/struct.go` exposes `ToStarlarkStruct(map[string]any) (*starlarkstruct.Struct, error)` (Go map → Struct).
- `pkg/bridge/value.go` exposes `FromStarlarkValue(starlark.Value) (any, error)` (any Starlark value → Go).
- **`bridge.StructFromDict` does NOT exist.** A grep over `pkg/bridge/` returned zero matches for "StructFromDict".

**Critical for D5-C3:** `*starlarkstruct.Struct` does NOT implement `MarshalJSON` (verified — `starlarkstruct.Struct` is an unexported `*starlark.Value` with attribute access only). It cannot cross the Temporal data converter as-is. The mock callback's return value MUST be converted to a Go map BEFORE assembly into `dag.OkResult.Output`.

**The round-trip:**

```
Starlark dict (mock lambda return value: ok(value={"login": "octocat"}))
    ↓ bridge.FromStarlarkValue (existing)
map[string]any  {"login": "octocat"}
    ↓ wrap in a custom OperationOutput shape — see below
dag.OkResult{Idx: i, Output: <wrapper>}
    ↓ Temporal JSON DataConverter
JSON bytes on the wire
    ↓ ActionResults.UnmarshalJSON (pkg/dag/result_marshal.go:251)
dag.OkResult{Idx: i, Output: dag.RawOperationOutput{Bytes: <json>}}
    ↓ pkg/interpreter/walk_step.go::extractStatusSummary (existing pattern)
JSON unmarshal probe — reads the map
    ↓ THIS IS WHERE THE STRUCT IS BUILT (existing prod code):
        the interpreter's downstream ctx-write path calls bridge.ToStarlarkStruct
        on the map[string]any to give the lambda dot-notation access.
```

**For Phase 5:**

- The mock callback returns a `dag.OkResult` whose `Output` field is an `OperationOutput` carrying a `map[string]any`. Look at `pkg/extension/builtin/http/response.go::HTTPResponse` for the typed shape — Phase 5 should NOT define a new typed Output, it should use a generic wrapper:

```go
// pkg/testing/output.go

// MockOperationOutput wraps a map for any (extension, op) — schemaless because
// the mock can return arbitrary shape. JSON-marshals as the map directly.
type MockOperationOutput struct {
    Value map[string]any
}

func (MockOperationOutput) IsOperationOutput() {}

func (m MockOperationOutput) MarshalJSON() ([]byte, error) {
    return json.Marshal(m.Value)
}
```

- After the round-trip, the interpreter sees `RawOperationOutput{Bytes: <json>}` — exactly what production HTTP responses look like. The downstream `ctx.<step_output>.<key>` path is identical to production.

- **For non-dict `value=`:** D5-C2 says `ok(value=...)` accepts any value, not just dicts. Convert via `FromStarlarkValue` to the most natural Go type, then marshal. Lists → `[]any`, scalars → primitives, etc.

**Recommended approach:**

1. Build `pkg/testing/output.go` with `MockOperationOutput` (above) — single JSON-marshalable wrapper.
2. Mock callback's per-action assembly:

```go
// pkg/testing/router.go (inside the callback, per-action)

// Inside the OnActivity callback, after evalMockLambda returns one of
// {ok|err|nonretryable}:
switch v := mockResult.(type) {
case okValue:
    goValue, err := bridge.FromStarlarkValue(v.Inner)
    if err != nil { /* nonretryable: return type unsupported */ }
    var output dag.OperationOutput
    switch g := goValue.(type) {
    case map[string]any:
        output = MockOperationOutput{Value: g}
    case nil:
        output = nil // legal per pkg/extension/operation.go
    default:
        // Wrap a non-map value into a single-key map so the Output marshals.
        output = MockOperationOutput{Value: map[string]any{"value": g}}
    }
    results = append(results, dag.OkResult{Idx: idx, Output: output})
case errValue:
    results = append(results, dag.RetryableErrResult{Idx: idx, Err: errors.New(v.Msg)})
case nonretryableValue:
    results = append(results, dag.NonRetryableErrResult{Idx: idx, Err: extension.WrapNonRetryable(errors.New(v.Msg))})
}
```

3. **No change to `pkg/bridge`.** The CONTEXT.md `<decisions>` D5-C3 implementation note ("confirm whether `bridge.StructFromDict` is exported; if not, expose it") is resolved: `bridge.ToStarlarkStruct` is the right surface; it's already exported; it's invoked by the EXISTING interpreter ctx-write path, not by Phase 5's mock callback.

### Investigation 6: Mock match precedence — implementation patterns

**Confidence:** HIGH (no off-the-shelf solution; build directly)

**Survey:**

- `gomock` matchers are stub-replacement-time matchers, not multi-tier specificity. Wrong shape.
- `testify/mock`'s `mock.Call` matches on argument lists, not on a multi-tier ladder. The On-call ordering rule is similar to D5-B4 (recency wins) but doesn't natively support `(extension, op, kwargs-regex)` triple keys.
- No Go library of which I'm aware implements the specific 3-tier ladder D5-B4 describes.

**Recommendation: Build directly.** The data structure is small; the algorithm is straightforward.

```go
// pkg/testing/registry.go

// MockEntry is one registered (extension, op) → mock_fn binding.
type MockEntry struct {
    Extension string                       // "gh"
    Op        string                       // "get" or "*"
    Match     map[string]*regexp.Regexp    // compiled at registration; nil = no filter
    Lambda    *dag.CapturedLambda          // mock_fn body (CapturedLambda for D-18 ID + Pos)
    RegisterPos syntax.Position            // file:line:col of the tester.mock_action call (for tie-breaking)
    RegisteredAt time.Time                 // monotonic order within a tier (recency)
}

// Frame is one scoping layer: the file frame is the bottom; per-test frames
// stack on top. Each frame holds zero or more MockEntry by registration order.
// D5-A4: per-test entries shadow file entries with the same (Extension, Op,
// Match-key-set), but precedence (D5-B4) is computed on the FLATTENED list.
type Frame struct{ Entries []MockEntry }

// MockRegistry is the per-test-file registry. It owns a stack of frames.
type MockRegistry struct {
    file   Frame
    perTest []Frame  // stack — top is the active test
}

// PushTestFrame / PopTestFrame manage the per-test layer (D5-A4).
func (r *MockRegistry) PushTestFrame() { r.perTest = append(r.perTest, Frame{}) }
func (r *MockRegistry) PopTestFrame()  { r.perTest = r.perTest[:len(r.perTest)-1] }

// Add records a new MockEntry into the active frame (per-test if any, else file).
func (r *MockRegistry) Add(e MockEntry) {
    if len(r.perTest) > 0 {
        top := &r.perTest[len(r.perTest)-1]
        top.Entries = append(top.Entries, e)
        return
    }
    r.file.Entries = append(r.file.Entries, e)
}

// Match implements D5-B4. Iteration order: TOP-OF-STACK first (per-test),
// then file. Within each frame: most-recent-FIRST. Returns the FIRST MockEntry
// whose (Extension, Op, kwargs-regex) matches, walked TIER-BY-TIER:
//   Tier 1: (ext, op) + match-regex matches kwargs
//   Tier 2: (ext, op) exact, no match
//   Tier 3: (ext, "*") wildcard, with optional match
func (r *MockRegistry) Match(ref *dag.ActionRef) (MockEntry, bool) {
    ext, op := splitExtOp(ref.Kind_)         // "gh.get" → ("gh", "get")
    kwargs := refKwargsAsStringMap(ref.Kwargs) // *starlark.Dict → map[string]string for regex match

    // Iterate frames TOP-DOWN, then within frame iterate ENTRIES MOST-RECENT-FIRST.
    framesInOrder := append([]Frame{}, reverseFrames(r.perTest)...)
    framesInOrder = append(framesInOrder, r.file)

    for tier := 1; tier <= 3; tier++ {
        for _, frame := range framesInOrder {
            for i := len(frame.Entries) - 1; i >= 0; i-- {
                e := frame.Entries[i]
                switch tier {
                case 1:
                    if e.Extension == ext && e.Op == op && matchKwargs(e.Match, kwargs) && len(e.Match) > 0 {
                        return e, true
                    }
                case 2:
                    if e.Extension == ext && e.Op == op && len(e.Match) == 0 {
                        return e, true
                    }
                case 3:
                    if e.Extension == ext && e.Op == "*" && matchKwargs(e.Match, kwargs) {
                        return e, true
                    }
                }
            }
        }
    }
    return MockEntry{}, false
}
```

**Key design points:**

1. **Match-regex compilation at registration time** (D5-B5). `regexp.MustCompile` panics on bad pattern; convert to `regexp.Compile` in `tester.mock_action` and surface a `*dag.ValidationError` at parse time.
2. **Recency = registration order within a tier.** Implementation: iterate the frame's `Entries` slice from end to start. No explicit timestamp needed; slice-append order encodes recency.
3. **Frame ordering = lexical scope.** Per-test on top of file; matches D5-A4's stack-of-dicts mental model.
4. **No cross-extension wildcard.** D5-B3 forbids `extension="*"`; reject at registration with a clear error.

**Test coverage targets:**

- File-level `(gh, get, no match)` shadowed by per-test `(gh, get, no match)` — per-test wins (recency in tier 2).
- File-level `(gh, *)` not shadowed by file-level `(gh, get, match=...)` because tier 1 always beats tier 3.
- `(gh, get, no match)` registered AFTER `(gh, get, path=^/users/.*$)` — tier 1 still wins because tier-by-tier walk goes Tier 1 → Tier 2 → Tier 3.

### Investigation 7: `pkg/interpreter/replay_determinism_test.go::runOnceCapturing` — full body

**Confidence:** HIGH (file fully read)

**The current shape (verbatim from the file):**

```go
// pkg/interpreter/replay_determinism_test.go:34-61
func runOnceCapturing(t *testing.T, parsed *ParsedFlow, hash string, init map[string]any) (*eventCapturingLogger, map[string]any, error) {
    t.Helper()

    cap := newEventCapturingLogger()

    registry := NewRegistry()
    require.NoError(t, registry.Register(parsed.Flow.Name, hash, parsed))
    registry.Freeze()

    var ts testsuite.WorkflowTestSuite
    ts.SetLogger(cap)
    env := ts.NewTestWorkflowEnvironment()
    wf := NewWorkflow(registry)
    env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})
    env.ExecuteWorkflow(wf, dag.WorkflowInput{
        FlowName:    parsed.Flow.Name,
        ContentHash: hash,
        InitState:   init,
    })
    require.True(t, env.IsWorkflowCompleted())

    wfErr := env.GetWorkflowError()
    var out map[string]any
    if wfErr == nil {
        require.NoError(t, env.GetWorkflowResult(&out))
    }
    return cap, out, wfErr
}
```

**What's lift-able vs. what needs reshaping:**

- ✅ **Lift verbatim:** the registry build, `ts.SetLogger`, `env.ExecuteWorkflow`, `GetWorkflowResult` extraction.
- ⚠️ **Reshape:** the `*testing.T` dependency. Public helper should NOT couple to `*testing.T` — a Phase 5 caller wants to call `RunOnceCapturing` from inside a `def test_*()` invoked via `t.Run`, but the helper itself shouldn't fail tests via `require.NoError`. **Return the error instead; let the caller handle it.**
- ⚠️ **Add:** activity registration + mock callback wiring. The current helper has NO activity stubs — it's pure for `if_cond` / `script` flows that emit ZERO activities. Phase 5 needs the helper to accept a mock callback (the `ExecuteBatch` Return-fn) so `step` flows work.
- ⚠️ **Add:** activity-boundary listeners (Investigation 2) for richer event capture.

**Proposed public shape:**

```go
// pkg/interpreter/replay_helper.go (NEW — moved out of _test.go)

// RunOnceCapturing executes parsed against a fresh TestWorkflowEnvironment.
// The mockExecuteBatch callback (per Investigation 1) handles activity calls;
// pass a nil callback for flows that emit no activities (if_cond/script-only).
//
// Used by Phase 5's tester.run + replay diff (TEST-02, TEST-04). Also used
// internally by the existing TestReplay_* tests, which migrate to call this
// public helper instead of the file-private runOnceCapturing.
func RunOnceCapturing(parsed *ParsedFlow, hash string, init map[string]any,
    mockExecuteBatch func(context.Context, []*dag.ActionRef) ([]dag.ActionResult, error),
) (*EventCapture, map[string]any, error) {
    ec := newEventCapture()
    registry := NewRegistry()
    if err := registry.Register(parsed.Flow.Name, hash, parsed); err != nil {
        return nil, nil, err
    }
    registry.Freeze()

    var ts testsuite.WorkflowTestSuite
    ts.SetLogger(ec)
    env := ts.NewTestWorkflowEnvironment()
    env.SetOnActivityStartedListener(ec.onActivityStarted)
    env.SetOnActivityCompletedListener(ec.onActivityCompleted)

    if mockExecuteBatch != nil {
        fake := func(_ context.Context, _ []*dag.ActionRef) ([]dag.ActionResult, error) { return nil, nil }
        env.RegisterActivityWithOptions(fake, sdkactivity.RegisterOptions{Name: "ExecuteBatch"})
        env.OnActivity("ExecuteBatch", mock.Anything, mock.Anything).Return(mockExecuteBatch)
    }

    wf := NewWorkflow(registry)
    env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})
    env.ExecuteWorkflow(wf, dag.WorkflowInput{
        FlowName: parsed.Flow.Name, ContentHash: hash, InitState: init,
    })
    if !env.IsWorkflowCompleted() {
        return ec, nil, errors.New("workflow did not complete")
    }
    var out map[string]any
    if env.GetWorkflowError() == nil {
        env.GetWorkflowResult(&out)
    }
    return ec, out, env.GetWorkflowError()
}
```

`EventCapture` (also lifted/exported) wraps the `eventCapturingLogger` plus activity-listener storage. Private members behind a public interface (`Snapshot()`, `Serialize()`, `FirstDivergence(other *EventCapture) *Divergence`).

**Migration:** The existing `TestReplay_*` tests in `pkg/interpreter/replay_determinism_test.go` switch from the private `runOnceCapturing` to `interpreter.RunOnceCapturing` (no behavioral change). Phase 5's `pkg/testing/replay.go` simply imports `interpreter.RunOnceCapturing` and `interpreter.EventCapture`.

### Investigation 8: `pkg/interpreter/walk_step_actionfn_test.go` — `env.OnActivity` static-Return pattern

**Confidence:** HIGH (file fully read)

**Current pattern (`walk_step_actionfn_test.go:152-158`):**

```go
env.OnActivity("ExecuteBatch", mock.Anything, mock.Anything).
    Run(func(args mock.Arguments) {
        refs := args.Get(1).([]*dag.ActionRef)
        capturedRefs = make([]*dag.ActionRef, len(refs))
        copy(capturedRefs, refs)
    }).
    Return(dag.ActionResults{dag.OkResult{Idx: 0, Output: nil}}, nil)
```

The test combines `.Run(observer)` (capture inputs as side effect) with `.Return(staticValues)`. Both observers and static returns fire on every activity invocation.

**Phase 5's modification:** Replace `.Return(staticValues)` with `.Return(callbackFn)` per Investigation 1. The `.Run(observer)` callback is unnecessary because the callback receives the args directly:

```go
// Phase 5 shape:
env.OnActivity("ExecuteBatch", mock.Anything, mock.Anything).
    Return(func(ctx context.Context, batch []*dag.ActionRef) ([]dag.ActionResult, error) {
        // batch IS the captured refs; no separate Run observer needed.
        // Per-action: look up mock entry, evalLambda(kwargs, attempt), assemble result.
        return assembleResults(reg, attempts, batch)
    })
```

**Critical correctness note:** `args.Get(1).([]*dag.ActionRef)` works because the activity's second parameter is `[]*dag.ActionRef`. The `.Return(callbackFn)` form receives the same parameter at the same position; the callback's signature must match `func(context.Context, []*dag.ActionRef) ([]dag.ActionResult, error)` exactly — including pointer-slice-of-pointer (NOT `dag.ActionResults` in the position where `[]dag.ActionResult` is expected, since the SDK reflects on exact types).

### Investigation 9: Parser two-mode design (test mode vs. production mode)

**Confidence:** HIGH

**Current parser entrypoint:**

```go
// pkg/parser/parser.go:200-217
func (p *Parser) ParseFile(path string) (map[string]*dag.Flow, error) { ... }
func (p *Parser) ParseSource(filename string, src []byte) (map[string]*dag.Flow, error) { ... }
```

Both call `p.parse(filename, src)`. Test files would go through the same `parse` body but need:

1. The `tester` `*starlarkstruct.Module` registered in `parseTimeGlobals` BEFORE `ExecFileOptions`.
2. The `assert.*` module from `starlarktest.LoadAssertModule()` registered in `parseTimeGlobals`.
3. The top-level globals returned by `ExecFileOptions` PRESERVED (Investigation 4).

**CONTEXT.md `<code_context>` says:** *"`tester` Module registered as a NEW `starlark.NewBuiltin` family; ONLY when the parse mode is 'test' (NOT for production flow files). The parser distinguishes via a flag or a separate `ParseTest` entrypoint."*

**Two implementations:**

| Option | Pros | Cons |
|--------|------|------|
| **(A) `WithTestMode()` Option + same entrypoints** | Smaller API surface; production callers don't change; test-mode is a one-line opt-in. | The same `Parser` instance can be either test or production, not both. Caveat: tests run on per-file `Parser` instances anyway (cf. `parseSrcAsFlow`), so this is fine in practice. |
| **(B) `ParseTestFile`/`ParseTestSource` sibling entrypoints** | Crisp boundary; impossible to accidentally parse production files in test mode. | Duplicates the entrypoint surface; the test-mode path still has to call `p.parse` so the body is shared anyway. |

**Recommendation: Option (A) — `WithTestMode()` parser option.** Mirrors existing pattern (`WithRoot`, `WithExtensions`, `WithMaxBlockSize`, `WithExecutionStepLimit`).

```go
// pkg/parser/options.go (addition)
func WithTestMode() Option {
    return func(p *Parser) error { p.testMode = true; return nil }
}

// pkg/parser/parser.go (Parser struct addition)
testMode bool
testGlobals map[string]starlark.StringDict // see Investigation 4

// pkg/parser/globals.go::newParseTimeGlobals — gated additions
func newParseTimeGlobals(p *Parser, thread *starlark.Thread) (starlark.StringDict, error) {
    g := starlark.StringDict{ /* existing 8 entries */ }
    // Existing: register extensions.
    for name, ext := range p.registry.All() { ... }

    if p.testMode {
        // Inject `tester` module — Phase 5.
        g["tester"] = pkgtesting.NewTesterModule(p.testRegistry, p.testWorkflowSpec)
        // Inject `assert.*` from starlarktest — TEST-05.
        assertGlobals, err := starlarktest.LoadAssertModule()
        if err != nil { return nil, fmt.Errorf("starlarktest.LoadAssertModule: %w", err) }
        for k, v := range assertGlobals {
            if _, exists := g[k]; exists {
                return nil, fmt.Errorf("test-mode global collision: %q", k)
            }
            g[k] = v
        }
    }
    return g, nil
}
```

**`pkg/parser` cannot import `pkg/testing` directly** (testing → parser is the natural direction; parser → testing creates a cycle). The fix: define a small interface in `pkg/parser` that `pkg/testing` implements, OR have `pkg/cli/test.go` construct the `tester` module and inject it via a parser option. **Recommendation: parser option** — cleaner cycle break.

```go
// pkg/parser/options.go
func WithTestModule(modBuilder func(*Parser, *starlark.Thread) starlark.Value) Option {
    return func(p *Parser) error { p.testModuleBuilder = modBuilder; return nil }
}
```

`pkg/cli/test.go` builds the tester module per parser instance and passes it via `WithTestModule(...)` + `WithTestMode()`.

### Investigation 10: `go test -json` schema (D5-E2)

**Confidence:** HIGH

**Reference:** Stdlib `cmd/test2json` documentation — emits one line of JSON per event during test execution.

**Schema (verified from stdlib docs):**

```go
type TestEvent struct {
    Time    time.Time   // RFC3339-encoded; encoder's choice
    Action  string      // see action values below
    Package string      // import path of the package or .star file path
    Test    string      // test name, blank for non-test events
    Elapsed float64     // seconds, only for pass/fail/skip
    Output  string      // for output events
}
```

**Action values (definitive list):**

| Action | When emitted | Required fields |
|--------|--------------|-----------------|
| `start` | Test execution begins | `Action`, `Package`, `Test` (if a test) |
| `run` | Test starts running (after `start` for parallel scheduling) | `Action`, `Package`, `Test` |
| `pause` | Test pauses (parallel-related) | `Action`, `Package`, `Test` |
| `cont` | Test resumes | `Action`, `Package`, `Test` |
| `pass` | Test passes | `Action`, `Package`, `Test`, `Elapsed` |
| `bench` | Benchmark line | `Action`, `Package`, `Output` |
| `fail` | Test fails | `Action`, `Package`, `Test`, `Elapsed` |
| `output` | Test produces text output (printed line) | `Action`, `Package`, `Test`, `Output` |
| `skip` | Test skipped (`t.Skip`) | `Action`, `Package`, `Test`, `Elapsed` |

**For Phase 5 (D5-E2):**

- **Map `Package` to** the `.star` test file basename (e.g., `users_test.star`). NOT the directory; matches CONTEXT.md's example `{"action":"start","package":"users_test.star","test":"test_existing_user"}`.
- **Map `Test` to** the `def test_*` symbol name (e.g., `test_existing_user`).
- **`elapsed`** is a float64 seconds with sub-second precision (e.g., `0.04`).
- **`output`** lines should be raw human-readable text — the STATIC `--- PASS:` / `--- FAIL:` lines from D5-E1 plus assertion-failure detail. Use `fmt.Sprintf` and emit one `output` event per line printed (matches `go test -json`'s line-oriented behavior).
- **`bench`/`pause`/`cont` are not emitted by Phase 5** (no benchmarks; sequential-within-file means no parallel scheduling that would require pause/cont).

**Recommended approach:**

```go
// pkg/cli/test.go (or pkg/testing/output_json.go)

type jsonEvent struct {
    Time    time.Time `json:"Time"`
    Action  string    `json:"Action"`
    Package string    `json:"Package"`
    Test    string    `json:"Test,omitempty"`
    Elapsed float64   `json:"Elapsed,omitempty"`
    Output  string    `json:"Output,omitempty"`
}

func emitJSON(w io.Writer, e jsonEvent) {
    b, _ := json.Marshal(e)
    w.Write(b); w.Write([]byte{'\n'})
}
```

CI consumers (gotestsum, tparse, GitHub Actions test annotations) read this schema verbatim. No JUnit conversion needed — gotestsum offers `--junitfile` with this input.

### Investigation 11: Firewall update (Phase 4 → Phase 5)

**Confidence:** HIGH

**Phase 4 introduced two firewall tests:**

1. **`tests/firewall_cli_test.go`** — `pkg/cli` is the only library-side package permitted to import cobra/pflag/charm-log/lipgloss.
2. **`pkg/activity/firewall_test.go::TestNoTemporalImportsOutsideAllowList`** — only `pkg/{activity, interpreter, worker, cli}` may import `go.temporal.io/sdk/*`.

**Phase 5 needs to extend (1) the temporal-firewall allow-list:**

- `pkg/testing` MUST import `go.temporal.io/sdk/testsuite` (the entire harness depends on `TestWorkflowEnvironment`).
- `pkg/testing` MUST import `go.temporal.io/sdk/activity` indirectly via its consumption of `interpreter.RunOnceCapturing` — wait, NO. `pkg/testing` imports `pkg/interpreter` which already imports activity. The firewall checks DIRECT imports per file. **`pkg/testing` will need to import `go.temporal.io/sdk/activity` if it directly calls `RegisterActivityWithOptions(activity.RegisterOptions{Name: "ExecuteBatch"})`** — but the `activity.RegisterOptions` type is in `go.temporal.io/sdk/activity`. So yes, an import is required.

**Phase 5 firewall changes:**

```go
// pkg/activity/firewall_test.go
allowedPkgs := []string{"activity", "interpreter", "worker", "cli", "testing"}  // ADD "testing"
```

**The decision rationale (from CONTEXT.md `<code_context>`):** "*`pkg/testing` may import `go.temporal.io/sdk/testsuite` (NEW allow-list entry) but NOT `go.temporal.io/sdk/activity`. Update the activity-firewall allow-list and add a sibling firewall test for the testsuite import.*"

**HOWEVER, my finding above contradicts:** `pkg/testing` likely DOES need `go.temporal.io/sdk/activity` for `activity.RegisterOptions`. **The planner should check** whether `RegisterActivityWithOptions` can be called via `testsuite`-only imports. Let me verify:

```
$ grep -n "activity.RegisterOptions\|sdkactivity" $GOMODCACHE/go.temporal.io/sdk@v1.42.0/internal/workflow_testsuite.go | head -5
# checked — RegisterActivityWithOptions takes an internal.RegisterActivityOptions which is ALIASED in go.temporal.io/sdk/activity as "activity.RegisterOptions". The public path is through the activity package.
```

**Resolution:** `pkg/testing` MUST import `go.temporal.io/sdk/activity` for `activity.RegisterOptions`. The CONTEXT.md note is WRONG on this specific point. Recommendation: **expand the firewall allow-list to include `testing` for ALL `go.temporal.io/sdk/*` paths** (the firewall test allows the WHOLE prefix once a package is in `allowedPkgs`).

**Sibling firewall test (NEW):** Add `tests/firewall_testsuite_test.go` with two parts:

```go
// tests/firewall_testsuite_test.go

// TestPkgTesting_ImportsTestsuite — non-vacuous: pkg/testing must
// eventually import go.temporal.io/sdk/testsuite (otherwise the allow-list
// is pointless). Skip-on-empty until W2 lands router.go. Mirrors
// TestPkgCli_ImportsCobra in pattern.
func TestPkgTesting_ImportsTestsuite(t *testing.T) { ... }

// TestPkgTesting_DoesNotImportSDKWorker — the harness must NOT register
// itself as a separate activity worker; only TestWorkflowEnvironment is
// permissible. (Relaxed; planner judgment if too restrictive.)
```

### Investigation 12: Validation Architecture (Nyquist)

**Confidence:** HIGH

`workflow.nyquist_validation` in `.planning/config.json` is `true` (verified). Phase 5 IS itself a testing tier, so the meta-pyramid is:

| Tier | Tests | What's exercised | Sampling |
|------|-------|------------------|----------|
| **Unit** | `pkg/testing/registry_test.go`, `pkg/testing/output_test.go`, `pkg/testing/builders_test.go`, `pkg/testing/discover_test.go` | Mock registry tier-1/2/3 precedence; layered scope (push/pop frames); recency tie-breaks; output wrapping; `def test_*` enumeration; assert-module loading | Per task commit |
| **Integration** | `pkg/testing/router_test.go`, `pkg/testing/replay_test.go`, `pkg/testing/runner_test.go` | `OnActivity` callback invoked once per attempt; `attempt` increments; mock callback returns flow into `dag.ActionResults`; replay-twice byte-stable; `*testing.T` reporter wiring; subtest isolation | Per wave merge |
| **E2E** | `pkg/testing/e2e_test.go` (with `testdata/*.star` fixtures); `tests/skytime_test_e2e_test.go` (subprocess invoking `skytime test <dir>`) | Real `.star` test files run via real `tester.workflow` + `tester.mock_action` + `tester.run`; subprocess `skytime test --format=json` produces correct JSON event stream | Per phase gate |

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `testify/{require,assert,mock}` v1.11.1 |
| Config file | (none — Go stdlib reads `*_test.go` automatically; `_test.star` discovery is Phase 5's own concern) |
| Quick run command | `go test -race -count=1 ./pkg/testing/...` |
| Full suite command | `go test -race -count=1 ./...` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| TEST-01 | `tester.workflow/mock_action/run` are parse-time globals available in test mode | Integration | `go test -race ./pkg/testing -run TestTesterModule_RegistersBuiltins` | ❌ Wave 0 |
| TEST-01 | Calling `tester.run` outside `def test_*` is rejected | Unit | `go test -race ./pkg/testing -run TestTesterRun_OutsideDefTest_RejectsAtParse` | ❌ Wave 0 |
| TEST-02 | Mock callback receives `[]*dag.ActionRef` and returns `[]dag.ActionResult` for `gh.get` | Integration | `go test -race ./pkg/testing -run TestRouter_DispatchesToMatchingMockLambda` | ❌ Wave 1 |
| TEST-02 | Mock-lambda env = `lambdaTimeGlobals` ∪ {ok, err, nonretryable}; production env unchanged | Unit | `go test -race ./pkg/testing -run TestMockLambdaEnv_IsLambdaTimePlusBuilders` | ❌ Wave 1 |
| TEST-02 | No-mock-found returns `extension.ErrNonRetryable` with flow callsite + step name | Integration | `go test -race ./pkg/testing -run TestRouter_NoMockFound_FailsFast` | ❌ Wave 1 |
| TEST-02 | 3-tier match precedence resolves correctly (tier 1 always wins; recency within tier) | Unit | `go test -race ./pkg/testing -run TestRegistry_TierPrecedence_TestVectors` | ❌ Wave 0 |
| TEST-03 | `attempt` is 1 on first call, 2 on retry, 3 on third retry (Temporal RetryPolicy) | Integration | `go test -race ./pkg/testing -run TestAttempts_IncrementOnRetry` | ❌ Wave 1 |
| TEST-04 | Two consecutive `tester.run` calls produce byte-equal event sequences | Integration | `go test -race ./pkg/testing -run TestReplay_DeterministicEventSequence` | ❌ Wave 2 |
| TEST-04 | Replay divergence reports first-divergent event with payload before/after; flow callsite + test callsite | Integration | `go test -race ./pkg/testing -run TestReplay_DivergenceReportFormat` | ❌ Wave 2 |
| TEST-05 | `assert.eq("octocat", actual)` failure surfaces in `*testing.T.Error` with Starlark file:line:col | Integration | `go test -race ./pkg/testing -run TestAssert_FailureSurfacesInSubtestT` | ❌ Wave 2 |
| TEST-05 | Multiple `assert.*` failures in one `def test_*` accumulate (library default) | Integration | `go test -race ./pkg/testing -run TestAssert_AccumulatesMultipleFailuresInSubtest` | ❌ Wave 2 |
| CLI-03 | `skytime test <dir>` discovers `*_test.star`, runs harness, exits 0 on all-pass | E2E | `go test -race ./tests -run TestSkytimeTestE2E_HappyPath` | ❌ Wave 4 |
| CLI-03 | `skytime test <dir>` exits 1 on any-fail | E2E | `go test -race ./tests -run TestSkytimeTestE2E_FailureExitNonzero` | ❌ Wave 4 |
| CLI-03 | `skytime test --run 'users_test\.test_existing'` filters tests | Integration | `go test -race ./pkg/cli -run TestTestCommand_RunFilter` | ❌ Wave 4 |
| CLI-03 | `skytime test --format=json` emits `go test -json`-compatible records | E2E | `go test -race ./tests -run TestSkytimeTestE2E_JSONFormat` | ❌ Wave 4 |
| CLI-03 | Default output has NO Go stack traces (CLI-03 explicit) | Integration | `go test -race ./pkg/cli -run TestTestCommand_DefaultOutput_NoGoStackTraces` | ❌ Wave 4 |

### Sampling Rate

- **Per task commit:** `go test -race ./pkg/testing/... -count=1`
- **Per wave merge:** `go test -race ./... -count=1`
- **Phase gate:** Full suite green + `tests/firewall_testsuite_test.go` green + `tests/firewall_cli_test.go` still green + `pkg/activity/firewall_test.go` still green (testing added to allow-list).

### Wave 0 Gaps

- [ ] `pkg/testing/doc.go` — package overview + parse/execute split note (the `tester` builtins are parse-time but the mocks are consumed at execute time via `TestWorkflowEnvironment`).
- [ ] `pkg/testing/registry.go` + `pkg/testing/registry_test.go` — `MockRegistry`, `MockEntry`, `Frame`, `PushTestFrame`, `PopTestFrame`, `Match`, `Add`, 3-tier ladder, regex compile-at-registration. Unit tests cover all D5-B4 cases.
- [ ] `pkg/testing/output.go` + `pkg/testing/output_test.go` — `MockOperationOutput`; tests verify JSON round-trip + `RawOperationOutput` decode-side compatibility.
- [ ] `pkg/parser/options.go` extension — `WithTestMode()`, `WithTestModule(...)`. Test that production parser path is unchanged when neither option is supplied.
- [ ] `pkg/parser/parser.go` extension — `Parser.testMode`, `Parser.testGlobals`, `Parser.TestGlobals(filename)`. Test that test mode preserves globals.
- [ ] `tests/firewall_testsuite_test.go` — `TestNoTemporalImportsOutsideAllowList` extended (`testing` added); new `TestPkgTesting_ImportsTestsuite` non-vacuous meta-test (skip-on-empty until Wave 1).
- [ ] Framework install: NONE — all dependencies already in `go.mod`.

## Common Pitfalls

### Pitfall 1: Using `mock.Anything, mock.Anything, mock.Anything` for `OnActivity("ExecuteBatch", ...)`

**What goes wrong:** Wrong arity panics at workflow-execute time. Activity has TWO args (`ctx`, `[]*dag.ActionRef`).

**Why it happens:** Copy-paste from a 3-arg activity example.

**How to avoid:** Always pass exactly two `mock.Anything` matchers — one for `context.Context`, one for `[]*dag.ActionRef`.

**Warning signs:** `panic: argument count mismatch` or `panic: assigning argument *[]dag.ActionResult to argument 0` at `env.ExecuteWorkflow` time.

### Pitfall 2: Returning `*starlarkstruct.Struct` from the mock callback

**What goes wrong:** Temporal's JSON DataConverter can't marshal `*starlarkstruct.Struct`; the activity's reply errors silently or panics.

**Why it happens:** D5-C3 says "downstream code in the flow reads `ctx.step_output.login` dot-notation identically to real activity output" — but that downstream conversion happens in `pkg/interpreter`'s ctx-write path, NOT in the mock callback.

**How to avoid:** The mock callback returns `dag.OkResult{Output: MockOperationOutput{Value: map[string]any{...}}}`. The Struct conversion happens implicitly downstream when the interpreter writes to `ctx`.

**Warning signs:** `json: unsupported type *starlarkstruct.Struct` or workflow stalls waiting for an activity that never returns.

### Pitfall 3: `bridge.StructFromDict` (NONEXISTENT) vs. `bridge.ToStarlarkStruct` (existing)

**What goes wrong:** Planner reads CONTEXT.md's "confirm whether `bridge.StructFromDict` is exported" and tries to find/export it. The function does NOT exist.

**Why it happens:** CONTEXT.md `<decisions>` D5-C3 implementation note speculates about a function name that doesn't match the codebase.

**How to avoid:** Use `bridge.FromStarlarkValue` (Starlark → Go) and `bridge.ToStarlarkStruct` (Go map → Starlark Struct) — both exported, both proven through Phase 1-4. No new bridge export needed; the round-trip happens through Temporal's JSON serializer.

**Warning signs:** Compile error `undefined: bridge.StructFromDict`.

### Pitfall 4: `tester.run` invoked at file scope

**What goes wrong:** `tester.run(flow="users")` at the top of a test file (NOT inside a `def test_*()`) executes the workflow at parse time, before discovery has happened. The runner then re-runs it inside a subtest, doubling work and corrupting attempt counters.

**Why it happens:** Consultants new to the runner conflate "set up shared state" (file-scope) with "run a test" (per-test).

**How to avoid:** `builtinTesterRun` checks `thread.CallStack()`; if the call frame is the file's top-level (frame 0 is the user file, no enclosing `*starlark.Function`), reject with a `*dag.ValidationError` at parse time: `"tester.run must be called inside a def test_*() function (at <pos>)"`.

**Warning signs:** Parse-time errors at unexpected lines; mocks "double-fire"; replay-twice diff failing on attempt counters.

### Pitfall 5: Global `slog` default during concurrent test files

**What goes wrong:** If Phase 5 ever introduces cross-file parallelism (`go test -parallel N` on the harness's runner test), and the runner uses `slog.Default()` for any logging, output races on the shared logger.

**Why it happens:** `slog.Default()` is process-global; the production runner uses charm-log routed through `pkg/cli`, which is NOT process-global per-test.

**How to avoid:** Each test file gets its own `eventCapturingLogger` (which IS mutex-guarded — already the case in `test_helpers_test.go::eventCapturingLogger`). Do NOT pass loggers through `slog.Default()` in the harness internals; thread the logger explicitly.

**Warning signs:** Race detector flags `slog.Logger.Handler` access; sporadic CI test ordering changes outcomes.

### Pitfall 6: Forgetting to compile the `match=` regex at registration time

**What goes wrong:** Per-call recompile causes O(N) regex compilation per test run; with N actions, this is noticeable on real test suites.

**Why it happens:** Regex compilation in the match path is a tempting one-liner.

**How to avoid:** D5-B5 mandates: "Compile each pattern once at registration; cache `*regexp.Regexp` to avoid per-call recompile." Implementation: `tester.mock_action` calls `regexp.Compile`, stores `*regexp.Regexp` on `MockEntry.Match[key]`. Match path is a regex `MatchString` lookup — O(1) compilation.

**Warning signs:** Profiling shows `regexp.Compile` in the hot path; large test suites slow.

### Pitfall 7: Workflow-side state caching `*starlark.Function` mock lambdas across runs

**What goes wrong:** Phase 3's lambda registry is the source of truth for `*starlark.Function` — frozen at boot, looked up by D-18 ID. If a Phase 5 mock lambda is registered with a fresh content-hash on each `tester.run`, the registry must be REBUILT per run, otherwise replay-2 looks up Run-1's lambda.

**Why it happens:** Default to "register once, reuse" thinking.

**How to avoid:** `tester.run` builds a FRESH `interpreter.FlowRegistry` per call; the registry includes both the production parsed flow AND the per-test mock lambdas (under their D-18 IDs from `dag.ComputeLambdaID(srcBytes, mockFn.Position())`). Two consecutive runs allocate two registries — the workflow code looks up by content-hash + lambda-ID, deterministic per run.

**Warning signs:** Replay-twice test fails with "lambda ID not found"; mocks from a previous test leak into the next.

### Pitfall 8: Mock callback panics not converted to NonRetryable

**What goes wrong:** A panic in the mock callback (e.g., a programmer error in the harness) propagates through Temporal's activity worker as a generic `*temporal.ActivityError`; the user sees a Go stack trace, violating CLI-03.

**Why it happens:** Panic-recovery boundaries are easy to miss.

**How to avoid:** The top of the mock callback function has `defer func() { if r := recover(); r != nil { ... return panic-as-NonRetryableErrResult ... }() }`. Surface the panic message + Starlark callsite (if recoverable) as a `dag.NonRetryableErrResult{Err: extension.WrapNonRetryable(...)}`.

**Warning signs:** Tests fail with Go stack traces when a Starlark mock has a bug.

## Code Examples

> Verified patterns from existing project code.

### Example 1: Existing `runOnceCapturing` (lift target)

```go
// Source: pkg/interpreter/replay_determinism_test.go:34-61
func runOnceCapturing(t *testing.T, parsed *ParsedFlow, hash string, init map[string]any) (*eventCapturingLogger, map[string]any, error) {
    cap := newEventCapturingLogger()
    registry := NewRegistry()
    require.NoError(t, registry.Register(parsed.Flow.Name, hash, parsed))
    registry.Freeze()

    var ts testsuite.WorkflowTestSuite
    ts.SetLogger(cap)
    env := ts.NewTestWorkflowEnvironment()
    wf := NewWorkflow(registry)
    env.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})
    env.ExecuteWorkflow(wf, dag.WorkflowInput{
        FlowName: parsed.Flow.Name, ContentHash: hash, InitState: init,
    })
    require.True(t, env.IsWorkflowCompleted())

    wfErr := env.GetWorkflowError()
    var out map[string]any
    if wfErr == nil {
        require.NoError(t, env.GetWorkflowResult(&out))
    }
    return cap, out, wfErr
}
```

### Example 2: Existing `OnActivity` static-Return pattern (Phase 4 → Phase 5 target for replacement)

```go
// Source: pkg/interpreter/walk_step_actionfn_test.go:152-158
env.OnActivity("ExecuteBatch", mock.Anything, mock.Anything).
    Run(func(args mock.Arguments) {
        refs := args.Get(1).([]*dag.ActionRef)
        capturedRefs = make([]*dag.ActionRef, len(refs))
        copy(capturedRefs, refs)
    }).
    Return(dag.ActionResults{dag.OkResult{Idx: 0, Output: nil}}, nil)
```

### Example 3: `*starlarkstruct.Module` registration (template for `tester` Module)

```go
// Source: pkg/parser/globals.go:65-81 (extension pattern)
for name, ext := range p.registry.All() {
    modVal, err := ext.Initialize(thread, nil)
    if err != nil {
        return nil, fmt.Errorf("initialize extension %q: %w", name, err)
    }
    if _, ok := modVal.(starlark.HasAttrs); !ok {
        return nil, fmt.Errorf("extension %q: ... not attribute-bearing", name)
    }
    g[name] = modVal
}
```

The `tester` Module follows the same pattern, registered conditionally on `p.testMode`.

### Example 4: `helperRegisterFakeExecuteBatch` (template for harness's activity registration)

```go
// Source: pkg/interpreter/walk_step_test.go:28-33
func helperRegisterFakeExecuteBatch(env *testsuite.TestWorkflowEnvironment) {
    fake := func(_ context.Context, _ []*dag.ActionRef) ([]dag.ActionResult, error) {
        return nil, nil
    }
    env.RegisterActivityWithOptions(fake, activity.RegisterOptions{Name: "ExecuteBatch"})
}
```

### Example 5: Bridge round-trip helpers (already exported)

```go
// Source: pkg/bridge/struct.go:25-41
func ToStarlarkStruct(m map[string]any) (*starlarkstruct.Struct, error)

// Source: pkg/bridge/value.go:17-78
func FromStarlarkValue(v starlark.Value) (any, error)
```

The mock callback's per-action body uses `bridge.FromStarlarkValue(mockReturnValue)` to convert the Starlark dict to a Go map; the production interpreter's ctx-write path uses `bridge.ToStarlarkStruct(map)` to give the lambda dot access. Symmetric, prod-vetted.

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `MockCallWrapper.Run(observer) + Return(static)` (Phase 3-4) | `MockCallWrapper.Return(callbackFn)` (Phase 5) | Phase 5 introduces dynamic per-call mock logic | Replaces 2-line static return with a per-call function; enables `attempt` counter and Starlark mock dispatch |
| File-private `runOnceCapturing` in `_test.go` | Public `interpreter.RunOnceCapturing` (or `pkg/testing/replay.RunOnceCapturing`) | Phase 5 lifts the helper | Existing tests migrate; Phase 5 reuses without copying |
| Single parser entrypoint (`ParseFile`) | Same entrypoint with `WithTestMode()` opt-in | Phase 5 adds parser option | Production callers unaffected; test files explicitly opt in |
| Two-environment lambda eval (parse-time + lambda-time, D-20) | THREE environments (adds mock-lambda env = lambda-time + {ok, err, nonretryable}) | Phase 5 D5-C2 | Production lambda-time env is UNCHANGED; new env is sibling, not extension |
| `walk_step.go::extractStatusSummary` reads HTTP-shaped `Output` via reflection or RawOperationOutput JSON probe | Same — Phase 5 emits `MockOperationOutput{Value: map[string]any}` which the existing code reads symmetrically | Phase 5 mock outputs match production round-trip shape | Zero changes to interpreter `walk_step.go` |

**Deprecated/outdated:**

- `bridge.StructFromDict` — speculative function name that does NOT exist in the codebase. CONTEXT.md `<decisions>` D5-C3 mentions it; the planner should ignore that suggestion and use `bridge.ToStarlarkStruct` / `bridge.FromStarlarkValue` instead.

## Environment Availability

> Phase 5 has NO new external runtime dependencies — Step 2.6 audit:

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| `go.temporal.io/sdk/testsuite` (for `TestWorkflowEnvironment`) | Mock router, replay helper | ✓ | v1.42.0 (transitive) | — |
| `go.starlark.net/starlarktest` (for `LoadAssertModule`, `Reporter`) | `assert.*` wiring | ✓ | pseudo-version pinned via `go.starlark.net@latest` | — |
| Go 1.25.x | Build/toolchain | ✓ | go 1.25.8 (per `go.mod`) | — |
| `temporal server start-dev` | Running real flows from `examples/skeleton/` (NOT Phase 5 tests, which use `TestWorkflowEnvironment` only) | N/A — Phase 5 does not invoke a live server | — | — |

**Missing dependencies with no fallback:** None.
**Missing dependencies with fallback:** None.

Phase 5 is purely code/test additions inside the existing module tree. No new go.mod requirements.

## Validation Architecture

(See Investigation 12 for the full pyramid + Test Framework + Phase Requirements → Test Map + Sampling Rate + Wave 0 Gaps. Repeated verbatim under that heading per researcher template guidance to keep the rubric scannable.)

## Open Questions for Planner

1. **Lift target for `runOnceCapturing` — `pkg/interpreter` (option A) vs. `pkg/testing` (option B)?**
   - What we know: option A keeps the helper close to its current call sites and minimizes duplication. Option B keeps Phase 5's package self-contained but requires duplicating ~50 LOC.
   - What's unclear: whether `pkg/interpreter`'s public surface should grow a "test helper" function, philosophically.
   - Recommendation: Option A. Name it `interpreter.RunOnceCapturing`; keep `EventCapture` exported alongside. Existing `replay_determinism_test.go` migrates cleanly.

2. **Cross-file test parallelization (D5-E5 Claude's Discretion)?**
   - What we know: each `*_test.star` gets its own `TestWorkflowEnvironment`; testsuite state is per-env. Sequential is safest; parallel offers wall-time savings.
   - What's unclear: whether the global slog default contention surfaces in practice given `eventCapturingLogger`'s mutex.
   - Recommendation: Sequential v1 across files (D5-E5 leans this way; planner discretion). Re-evaluate at v1.x if test counts grow.

3. **Where does the `tester` Module construct live — `pkg/testing` or `pkg/parser`?**
   - What we know: parser cannot directly import `pkg/testing` without cycle risk (testing imports interpreter which depends on parser indirectly through the registry).
   - What's unclear: whether to break the cycle via interface (parser exposes `WithTestModule(builderFn Option)`), via re-rooting the constructor in `pkg/cli`, or via a separate `pkg/parser/testmod` sub-package.
   - Recommendation: `WithTestModule(builderFn)` parser option. The CLI builds the module per parse session and injects via the option. Cleanest cycle break; keeps `pkg/parser` test-agnostic.

4. **`MockOperationOutput` field name — `Value`, `Payload`, or none?**
   - What we know: the mock callback wraps Go-level data into an `OperationOutput`; downstream code reads it via `RawOperationOutput.Bytes` JSON probe.
   - What's unclear: whether the wrapper field name leaks into JSON. The current `extractStatusSummary` probes `{"status": int}`; it doesn't care about wrapper field name.
   - Recommendation: omit the wrapper field name from JSON (custom `MarshalJSON` returns the value-map directly). Then the JSON wire format is `{"login": "octocat"}` indistinguishably from a typed extension Output.

5. **What happens if a `def test_*()` calls `tester.workflow` more than once?**
   - What we know: D5-A3 says re-declarations are how per-test variations of `init_state` work — last-write-wins.
   - What's unclear: whether the runner should warn on multiple `tester.workflow` calls in the same test or silently take the last.
   - Recommendation: silently take the last; document the behavior in `docs/for-flow-authors/testing.md`. Add a v1.x linter that warns on >1 calls if it surfaces real bug-class confusion.

6. **JSON output's `Time` field — what timezone, what precision?**
   - What we know: stdlib `cmd/test2json` emits RFC3339Nano in the local timezone.
   - What's unclear: whether tooling like gotestsum cares.
   - Recommendation: RFC3339Nano in UTC for reproducibility (`time.Now().UTC().Format(time.RFC3339Nano)`); document under D5-E2.

7. **Should `pkg/testing` expose a Go-level runner API (`pkgtesting.Run(t, dir, opts...)`) for `go test`-driven integration?**
   - What we know: Phase 6 will likely want a way to embed `temporal_test`-driven `.star` tests inside a `go test` package, similar to how `examples/skeleton/` runs through `tests/differential_test.go`.
   - What's unclear: whether v1 ships the Go-level API or limits Phase 5 to the `skytime test` subprocess path.
   - Recommendation: ship `pkgtesting.Run(t, dir, opts...)` as the FOUNDATION; `skytime test` is a thin CLI wrapper around it. Mirrors `pkg/cli` ↔ `cmd/skytime` separation. Phase 6 then has a clean integration point.

8. **Firewall update — does `pkg/testing` need `go.temporal.io/sdk/activity` import?**
   - What we know: `RegisterActivityWithOptions(fake, activity.RegisterOptions{Name: "ExecuteBatch"})` requires `activity.RegisterOptions` from the `activity` package.
   - What's unclear: whether CONTEXT.md's "may import testsuite but NOT activity" guidance was stricter than the implementation requires.
   - Recommendation: expand allow-list to permit `pkg/testing` to import `go.temporal.io/sdk/*` (entire prefix) — same as the existing `cli`/`activity`/`interpreter`/`worker` allow. The "no activity import" rule was originally about EXTENSION packages, not the harness.

## Sources

### Primary (HIGH confidence)

- `$GOMODCACHE/go.temporal.io/sdk@v1.42.0/internal/workflow_testsuite.go` — `OnActivity` (lines 396-449), `MockCallWrapper.Return` (lines 832-835), `MockCallWrapper.Run` (lines 814-817), `SetOnActivityStartedListener` (line 983), `SetOnActivityCompletedListener` (line 992), `GetWorkflowResult` (line 1123), `GetWorkflowError` (line 1148), `RegisterDelayedCallback` (line 1235). Exhaustive method list confirms NO `GetWorkflowHistory` exists.
- `pkg/interpreter/replay_determinism_test.go` — full file read; lines 34-61 are the lift target.
- `pkg/interpreter/walk_step_actionfn_test.go` — full file read; lines 152-158 are the existing OnActivity static-Return pattern.
- `pkg/interpreter/test_helpers_test.go` — full file read; `parseSrcAsFlow`, `eventCapturingLogger`, `serializeRecords`, `findEventRecords`.
- `pkg/parser/parser.go` — full file read; `Parser` struct, options, `parse()`, `ExecFileOptions` invocation at line 280.
- `pkg/parser/globals.go` — full file read; `newParseTimeGlobals`, HasAttrs gate.
- `pkg/bridge/struct.go` — full file read; `ToStarlarkStruct` exported (no `StructFromDict`).
- `pkg/bridge/value.go` — full file read; `FromStarlarkValue` exported.
- `pkg/dag/result.go` + `pkg/dag/result_marshal.go` — `ActionResult` sealed sum, JSON wire format with discriminator, `RawOperationOutput` for round-trip.
- `pkg/interpreter/workflow.go` + `walk_step.go` — `NewWorkflow(registry)`, `extractStatusSummary` reading `RawOperationOutput.Bytes`.
- `pkg/extension/testing/fake_handler.go` — `FakeCredentialHandler` (Phase 5 reuse target).
- `tests/firewall_cli_test.go` + `pkg/activity/firewall_test.go` — firewall test patterns to extend.
- `pkg.go.dev: go.starlark.net/starlarktest` (WebFetch) — verified `LoadAssertModule()`, `SetReporter`, `GetReporter`, `Reporter{ Error(args ...any) }`, "*testing.T satisfies it directly".

### Secondary (MEDIUM confidence)

- Stdlib `cmd/test2json` schema reference (D5-E2 mirror target). Action values + field names cross-checked against the stdlib documentation; gotestsum and tparse parsers verify the field names by consuming the schema.

### Tertiary (LOW confidence)

None — every load-bearing claim is backed by source-cache or in-tree code reads.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — every dependency is in `go.mod` and the relevant API surfaces verified against installed source.
- Architecture: HIGH — Phase 5 is composition over existing primitives; no new architectural choices.
- Pitfalls: HIGH — Pitfalls 1-3 verified against API source; Pitfalls 4-8 are deductions from Phase 1-4 conventions.
- `OnActivity(callbackFn)` semantics: HIGH — read SDK source verbatim.
- `TestWorkflowEnvironment` history API: HIGH — exhaustive method list confirms NO `GetWorkflowHistory`.
- `starlarktest` reporter wiring: HIGH — pkg.go.dev signatures confirmed.
- `bridge.StructFromDict` non-existence: HIGH — exhaustive grep, contradicts CONTEXT.md note.
- Firewall scope: MEDIUM — recommendation to allow `pkg/testing` ALL `go.temporal.io/sdk/*` paths is more permissive than CONTEXT.md's narrower note; planner should validate.

**Research date:** 2026-05-05
**Valid until:** 2026-06-05 (30 days — stable Go SDK API; underlying Temporal SDK v1.42 release date 2026-04-08 means surface should not move within the validity window).
