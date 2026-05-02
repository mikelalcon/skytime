---
phase: quick-260502-onc
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - pkg/extension/builtin/http/http.go
  - pkg/extension/builtin/http/http_test.go
  - pkg/extension/error.go
  - pkg/extension/error_test.go
  - pkg/activity/execute_batch.go
  - pkg/activity/execute_batch_test.go
  - pkg/interpreter/walk_step.go
  - pkg/interpreter/walk_step_test.go
  - pkg/cli/progress.go
  - pkg/cli/progress_test.go
  - examples/skeleton/simple_check.star
  - examples/skeleton/parallel_fanout.star
  - tests/e2e_skytime_run_test.go
autonomous: true
requirements:
  - QUICK-260502-onc-FixA  # http extension auto-fails non-2xx
  - QUICK-260502-onc-FixB  # walk_step status= summary via reflection
  - QUICK-260502-onc-FixC  # progress renderer flow_failed line
  - QUICK-260502-onc-FixD  # corpus update + e2e smoke

must_haves:
  truths:
    - "HTTP non-2xx response from a step action causes the workflow to fail"
    - "4xx HTTP response surfaces as Temporal NonRetryable (no SDK retry)"
    - "5xx HTTP response surfaces as plain Go error (Temporal retries per RetryPolicy)"
    - "2xx HTTP response continues to succeed unchanged with HTTPResponse output"
    - "Successful step renders status=N in the [skytime] step_complete summary"
    - "Failed flow ends with [skytime] flow failed line citing the failing step index and reason"
    - "examples/skeleton/{simple_check,parallel_fanout}.star exercise a real GitHub endpoint that returns 200 (octocat/Hello-World)"
    - "End-to-end happy-path: skytime run on simple_check.star prints status=200 and flow complete, exits 0"
    - "End-to-end unhappy-path: skytime run on a 404-pointing flow prints ✗, HTTP 404, and flow failed; exits non-zero"
  artifacts:
    - path: "pkg/extension/error.go"
      provides: "ErrNonRetryable sentinel — extension-side classification hook"
      contains: "ErrNonRetryable"
    - path: "pkg/extension/builtin/http/http.go"
      provides: "doHTTP wraps non-2xx as classified error (4xx → NonRetryable, 5xx → Retryable)"
      contains: "ErrNonRetryable"
    - path: "pkg/activity/execute_batch.go"
      provides: "isRetryable extended to honor extension.ErrNonRetryable sentinel"
      contains: "extension.ErrNonRetryable"
    - path: "pkg/interpreter/walk_step.go"
      provides: "walkStep populates summary='status=N' on success via reflection on OkResult.Output"
      contains: "extractStatus"
    - path: "pkg/cli/progress.go"
      provides: "progressHandler renders flow_failed line with step idx + reason on err_count > 0"
      contains: "lastErr"
    - path: "examples/skeleton/simple_check.star"
      provides: "Real GitHub endpoint (/repos/octocat/Hello-World) for happy-path demo"
      contains: "octocat/Hello-World"
    - path: "examples/skeleton/parallel_fanout.star"
      provides: "Real GitHub endpoint for parallel fan-out demo"
      contains: "octocat/Hello-World"
    - path: "tests/e2e_skytime_run_test.go"
      provides: "End-to-end happy + unhappy smokes wired through skytime run binary"
      contains: "TestE2E_SkytimeRun_Happy"
  key_links:
    - from: "pkg/extension/builtin/http/http.go (doHTTP)"
      to: "pkg/extension/error.go (ErrNonRetryable)"
      via: "fmt.Errorf %w wrap"
      pattern: "fmt.Errorf.*%w.*ErrNonRetryable"
    - from: "pkg/activity/execute_batch.go (isRetryable)"
      to: "pkg/extension.ErrNonRetryable"
      via: "errors.Is check before default-retryable branch"
      pattern: "errors.Is.*extension.ErrNonRetryable"
    - from: "pkg/interpreter/walk_step.go (defer step_complete)"
      to: "pkg/dag.OkResult.Output"
      via: "reflect.ValueOf field-by-name 'Status'"
      pattern: "reflect.*FieldByName.*Status"
    - from: "pkg/cli/progress.go (renderStepComplete err branch)"
      to: "progressHandler.lastErr (instance state)"
      via: "in-memory record of failing step idx/total/summary"
      pattern: "lastErr"
    - from: "pkg/cli/progress.go (renderFlowComplete)"
      to: "progressHandler.lastErr"
      via: "branch on err_count > 0"
      pattern: "err_count.*lastErr"
---

<objective>
Three layered correctness fixes plus corpus update, all under a single quick task:

  - **Fix A** (HTTP extension): non-2xx responses become first-class workflow
    failures. 4xx maps to NonRetryable (configuration / wrong path / wrong
    auth — no point retrying). 5xx maps to Retryable (transient — let the
    Temporal RetryPolicy do its job). 2xx unchanged: returns HTTPResponse,
    nil.
  - **Fix B** (interpreter): on success, walkStep populates the per-step
    `summary` attr with `status=N` so the Bazel renderer prints
    `✓ 234ms  status=200` instead of `✓ 234ms  ` (empty). Uses Go reflection
    against the OkResult.Output's `Status` field to keep the
    pkg/extension/builtin/http firewall intact (interpreter is a foundation
    package, builtin extensions are leaves — interpreter MUST NOT import
    them).
  - **Fix C** (CLI renderer): when err_count > 0 on flow_complete, print
    `[skytime] flow failed  step I/M (<reason>)  total Nms` in red instead
    of `[skytime] flow complete  N/M steps  total Nms`. Renderer holds the
    last-failure context locally so the interpreter event schema stays
    stable.
  - **Fix D** (corpus + e2e smokes): the differential corpus and Phase 4
    `skytime run` happy-path both rely on `examples/skeleton/{simple_check,
    parallel_fanout}.star` pointing at `/repos/example/repo` — a path that
    returns 404 against api.github.com. With Fix A in place those flows
    would fail at runtime. Switch to `/repos/octocat/Hello-World` (a real
    public GitHub endpoint that has returned 200 for a decade). Add a
    test-binary-driven happy-path AND unhappy-path e2e smoke that prove
    the full Fix A → Fix B → Fix C → Fix D stack against the actual CLI.

Purpose: today a typo in a `.star` file's path produces a successful
workflow with the 404 response body buried inside an OkResult. This is the
worst possible failure mode — silent. Fix A turns the WRONG outcome
(success) into the RIGHT outcome (NonRetryable failure). Fix B makes
success informative. Fix C makes failure visible. Fix D proves the whole
stack at the binary boundary.

**Scope note** (M-6 from checker iteration 1): this plan deliberately bundles
4 tasks / 13 files because Fix A → B → C → D form a hard behavior chain —
A produces the classified error, B propagates it as a workflow failure,
C renders the failure visibly, D verifies the whole chain end-to-end.
Splitting would create unobservable mid-states (e.g., Fix A landing alone
would make the workflow keep walking past failures with no surface change).
The four tasks share a single observable contract: "non-2xx fails loudly."

Output:
  - HTTP extension classifies non-2xx via the existing extension.Err* +
    pkg/activity/classify pattern (mirrors ErrUnknownCredential exactly).
  - Bazel renderer shows `status=200` on success and `flow failed step
    I/M (HTTP 404 ...)` on failure, with red `✗` markers.
  - Demo corpus runs cleanly against real GitHub.
  - E2E test binds the whole stack: build the binary, run two flows,
    grep stderr.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/PROJECT.md
@.planning/STATE.md
@CLAUDE.md

<!-- Existing convention to mirror for Fix A -->
@pkg/extension/handler.go
@pkg/activity/classify.go

<!-- Files Fix A modifies / extends -->
@pkg/extension/builtin/http/http.go
@pkg/extension/builtin/http/http_test.go
@pkg/extension/builtin/http/response.go
@pkg/activity/action_executor.go
@pkg/activity/execute_batch.go
@pkg/activity/execute_batch_test.go

<!-- Files Fix B modifies -->
@pkg/interpreter/walk_step.go
@pkg/interpreter/walk_step_test.go

<!-- Files Fix C modifies -->
@pkg/cli/progress.go
@pkg/cli/progress_test.go

<!-- Files Fix D modifies / verifies -->
@examples/skeleton/simple_check.star
@examples/skeleton/parallel_fanout.star
@tests/differential_test.go

<!-- Firewall + sealed-sum constraints -->
@pkg/dag/result.go
@pkg/dag/output.go

<interfaces>
<!-- Key contracts the executor must respect. Extracted from codebase
     so the executor does not need to re-explore. -->

From pkg/extension/handler.go (the EXISTING sentinel pattern Fix A mirrors):
```go
// ErrUnknownCredential is the sentinel handlers wrap when a credential ID
// is unknown. pkg/activity/classify.go uses errors.Is to surface as
// NonRetryable temporal.ApplicationError. Fix A's ErrNonRetryable follows
// the same shape verbatim.
var ErrUnknownCredential = errors.New("unknown credential")
```

From pkg/activity/classify.go (the existing classifier — NOT modified by Fix A):
```go
// classifyResolveError maps CredentialHandler.Resolve errors to
// *temporal.ApplicationError. Fix A does NOT touch this — it adds
// classification to pkg/activity/execute_batch.go isRetryable instead,
// which is the load-bearing classifier for OperationFunc errors.
```

From pkg/activity/execute_batch.go (Fix A modifies isRetryable):
```go
// CURRENT:
func isRetryable(err error) bool {
    if err == nil { return false }
    var appErr *temporal.ApplicationError
    if errors.As(err, &appErr) { return !appErr.NonRetryable() }
    return true // default: retryable
}
// AFTER FIX A: insert errors.Is(err, extension.ErrNonRetryable) check
// BEFORE the default-retryable branch. When matched: wrap the original
// err with temporal.NewNonRetryableApplicationError so the existing
// D2-13/D2-14 mid-batch dispatch in ExecuteBatch (lines 92–106) routes
// it through the NonRetryable arm and the interpreter sees the
// classified error.
```

From pkg/extension/builtin/http/response.go:
```go
type HTTPResponse struct {
    Status  int               `json:"status"`
    Body    []byte            `json:"body"`
    Headers map[string]string `json:"headers"`
}
func (HTTPResponse) IsOperationOutput() {}
```

From pkg/dag/result.go:
```go
type OkResult struct {
    Idx int
    Output OperationOutput
}
// Fix B reflects on Output's "Status" field by name. OperationOutput is
// a marker interface so reflect must inspect the concrete value.
```

From pkg/dag/output.go:
```go
type OperationOutput interface { IsOperationOutput() }
// CRITICAL: Fix B uses reflect to read Output.Status because pkg/interpreter
// cannot import pkg/extension/builtin/http (firewall — interpreter is
// foundation, builtin/http is leaf).
```

From pkg/cli/progress.go (Fix C modifies):
```go
type progressHandler struct {
    wrapped slog.Handler
    out     io.Writer
    ttyKnown bool
    tty      bool
    // FIX C ADDS: lastErr *failureContext  // reset on flow_start
}
// renderFlowComplete CURRENT:
func (p *progressHandler) renderFlowComplete(a attrMap) error {
    ok := a.int("ok_count")
    errc := a.int("err_count")
    totalMs := a.int("total_ms")
    line := fmt.Sprintf("%s flow complete  %d/%d steps  total %dms",
        p.colorBanner("[skytime]"), ok, ok+errc, totalMs)
    return p.println(line)
}
// AFTER FIX C: branch on errc > 0 — render flow failed using p.lastErr.
```

From pkg/activity/execute_batch.go (existing dispatch — DO NOT change shape):
```go
// Lines 90-106: per-action mid-batch error dispatch. isRetryable is the
// SOLE decision point. Fix A's classifier addition to isRetryable plugs
// into this exactly — no other code in execute_batch.go changes.
out, err := a.runAction(ctx, idx, ref)
if err != nil {
    if isRetryable(err) {
        return nil, err  // D2-13 retryable short-circuit
    }
    results = append(results, dag.NonRetryableErrResult{Idx: idx, Err: err})
    // ... SkippedResult tail ...
    return results, nil
}
```

From tests/differential_test.go (Fix D verifies still passes):
```go
// runDryRun uses dryrun.AlwaysOkDispatch — REPLACES every OperationFunc
// with one that returns OkResult unconditionally. The dry-run path is
// IMMUNE to Fix A's runtime-only doHTTP changes (it never calls doHTTP).
// So tests/differential_test.go continues to pass with the corpus update.
```
</interfaces>
</context>

<tasks>

<!-- ========================================================================== -->
<!-- TASK 1: Fix A — HTTP extension auto-fails non-2xx                          -->
<!-- ========================================================================== -->

<task type="auto" tdd="true">
  <name>Task 1: Fix A — HTTP extension auto-fails non-2xx (TDD: failing tests → impl)</name>
  <files>
    pkg/extension/error.go,
    pkg/extension/error_test.go,
    pkg/extension/builtin/http/http.go,
    pkg/extension/builtin/http/http_test.go,
    pkg/activity/execute_batch.go,
    pkg/activity/execute_batch_test.go
  </files>
  <behavior>
    EXTENSION SENTINEL (pkg/extension/error.go):
    - `var ErrNonRetryable = errors.New("non-retryable")` — exported sentinel
      mirroring ErrUnknownCredential's shape exactly. Doc comment cites the
      pattern: extensions wrap with `fmt.Errorf("%w: ...", ErrNonRetryable)`;
      pkg/activity isRetryable surfaces as NonRetryable temporal.ApplicationError.

    EXTENSION SENTINEL TEST (pkg/extension/error_test.go):
    - Test 1 (TestErrNonRetryable_Sentinel): ErrNonRetryable is non-nil with
      message "non-retryable".
    - Test 2 (TestErrNonRetryable_IsErrorsIsCompatible): mirrors
      handler_test.go's TestErrUnknownCredential_IsErrorsIsCompatible —
      a wrapped error via `fmt.Errorf("%w: HTTP 404", ErrNonRetryable)`
      satisfies errors.Is(wrapped, ErrNonRetryable).

    HTTP EXTENSION TESTS (pkg/extension/builtin/http/http_test.go — NEW tests):
    - Test 3 (TestExtension_Get_404_NonRetryable): httptest.Server returns
      WriteHeader(404) and writes body `{"error":"not found"}`. doGet via
      spec.Func returns (nil, err). err.Error() must contain "HTTP 404".
      errors.Is(err, extension.ErrNonRetryable) must be true.
    - Test 4 (TestExtension_Get_500_Retryable): httptest.Server returns
      WriteHeader(500). doGet returns (nil, err). err.Error() must contain
      "HTTP 500". errors.Is(err, extension.ErrNonRetryable) must be FALSE
      (5xx is retryable — plain wrapped error, no sentinel).
    - Test 5 (TestExtension_Get_2xx_StillSuccess): explicitly assert that
      WriteHeader(200) AND WriteHeader(204) AND WriteHeader(299) all return
      (HTTPResponse, nil) with the correct Status. This is the regression
      guard — Fix A must NOT widen what counts as failure beyond non-2xx.
    - Test 6 (TestExtension_Post_422_NonRetryable): mirror Test 3 against
      doPost with WriteHeader(422). Confirms the non-2xx classification
      applies to the body-bearing branch (asGetArgs vs asBodyArgs paths).

    ACTIVITY CLASSIFIER TEST (pkg/activity/execute_batch_test.go — NEW test):
    - Test 7 (TestIsRetryable_HonorsExtensionErrNonRetryable): unit-tests
      the new branch in isRetryable. Inputs:
        a) plain wrapped: fmt.Errorf("%w: HTTP 404", extension.ErrNonRetryable)
           → isRetryable returns FALSE.
        b) double-wrapped: fmt.Errorf("outer: %w",
           fmt.Errorf("%w: HTTP 404", extension.ErrNonRetryable))
           → isRetryable returns FALSE (defends against future log-context
           wrapping).
        c) plain unrelated: errors.New("network down")
           → isRetryable returns TRUE (default — preserves Phase 2 contract).
        d) NonRetryable temporal.ApplicationError → FALSE (preserves
           existing first-arm behavior).
        e) Retryable temporal.ApplicationError → TRUE (preserves existing).
        f) nil → FALSE (preserves existing).
    - Test 8 (TestExecuteBatch_HTTPNonRetryable_Integration): end-to-end
      through ExecuteBatch. Build a 1-action batch with a fake
      OperationFunc that returns
      `fmt.Errorf("%w: HTTP 404 GET https://x/y", extension.ErrNonRetryable)`.
      Assert: ExecuteBatch returns (results, nil); results[0] is
      dag.NonRetryableErrResult{Idx: 0, Err: <wrapped err>}; the wrapped
      err.Error() contains "HTTP 404"; errors.Is(results[0].Err,
      extension.ErrNonRetryable) is true.

    NOTE: Tests 3-6 must FAIL initially (RED) — they exercise behavior
    Fix A introduces. Tests 7-8 must FAIL initially against the unmodified
    isRetryable. Run `go test ./pkg/extension/... ./pkg/activity/...` to
    confirm the RED state before writing impl.
  </behavior>
  <action>
    RED PHASE (write failing tests first):

    1. Create pkg/extension/error.go:
    ```go
    package extension

    import "errors"

    // ErrNonRetryable is the sentinel extension OperationFuncs wrap when
    // a failure should NOT be retried by Temporal — typically a permanent
    // logical failure (4xx HTTP, contract violation, malformed input that
    // somehow bypassed parse-time validation, etc.).
    //
    // Wrap pattern (mirrors ErrUnknownCredential — see pkg/extension/handler.go):
    //
    //   return nil, fmt.Errorf("HTTP %d %s %s: %s: %w",
    //       resp.StatusCode, method, url, snippet(body), extension.ErrNonRetryable)
    //
    // The activity layer (pkg/activity/execute_batch.go isRetryable) checks
    // errors.Is(err, ErrNonRetryable) and surfaces the failure as a
    // NonRetryable temporal.ApplicationError. Plain unwrapped errors from
    // OperationFuncs continue to default to retryable per Phase 2's
    // transient-failure assumption (D2-13).
    //
    // Why a sentinel and not a typed error: extensions live OUTSIDE the
    // temporal-firewall (only pkg/{activity,interpreter,worker,cli} may
    // import go.temporal.io/sdk/*), so they cannot construct a
    // *temporal.ApplicationError directly. The sentinel + classify-side
    // wrap is the established Phase 2 pattern (ErrUnknownCredential →
    // classifyResolveError); ErrNonRetryable extends it to OperationFunc
    // errors.
    var ErrNonRetryable = errors.New("non-retryable")
    ```

    2. Create pkg/extension/error_test.go (Tests 1–2 above). Mirror the
    structure of handler_test.go::TestErrUnknownCredential_IsErrorsIsCompatible
    verbatim — same %w wrap chain depths.

    3. Add Tests 3–6 to pkg/extension/builtin/http/http_test.go. Pattern
    matches the EXISTING TestExtension_GetSucceedsAgainstHTTPTestServer
    structure (httptest.NewServer + reflect-built args). Imports needed
    beyond what's already there: `errors` and the package alias for
    `github.com/mikelalcon/skytime/pkg/extension` (already imported as
    `extension`).

    4. Add Tests 7–8 to pkg/activity/execute_batch_test.go. Test 7 is a
    pure unit test on isRetryable — no Temporal env needed. Test 8 uses
    the existing ExecuteBatch test scaffolding (look at
    TestExecuteBatch_NonRetryableMidBatch_ReturnsAllResults at line 186
    for the pattern: a fakeExtension dispatch with one op returning the
    error). The new test reuses fakeExtension wiring but the op returns
    `fmt.Errorf("%w: HTTP 404 GET https://x/y", extension.ErrNonRetryable)`
    instead of `temporal.NewNonRetryableApplicationError(...)`.

    5. Confirm RED: run
       `go test ./pkg/extension/... ./pkg/activity/... -count=1 -run 'TestErrNonRetryable|TestExtension_Get_404|TestExtension_Get_500|TestExtension_Get_2xx|TestExtension_Post_422|TestIsRetryable_HonorsExtensionErrNonRetryable|TestExecuteBatch_HTTPNonRetryable_Integration'`.
       Tests 1–2 fail because pkg/extension/error.go doesn't exist — wait,
       step 1 created it, so 1–2 should already PASS. Tests 3–6 fail
       because doHTTP hasn't been changed. Tests 7–8 fail because
       isRetryable hasn't been changed. (Steps 1–2 are GREEN-from-creation;
       3–8 must be RED before step 6.)

    GREEN PHASE (implement):

    6. Modify pkg/extension/builtin/http/http.go doHTTP — after the
       `respBody, err := io.ReadAll(...)` block and BEFORE the
       `respHeaders := ...` block, insert:
    ```go
    // Fix A (quick 260502-onc): non-2xx responses are first-class
    // workflow failures. 4xx → wrap with extension.ErrNonRetryable so
    // the activity classifier surfaces NonRetryable
    // temporal.ApplicationError. 5xx → plain wrapped error so the
    // activity's default-retryable branch lets the Temporal RetryPolicy
    // do its job. 2xx falls through to the success return below
    // unchanged.
    if resp.StatusCode >= 400 {
        bodySnippet := string(respBody)
        if len(bodySnippet) > 200 {
            bodySnippet = bodySnippet[:200] + "..."
        }
        if resp.StatusCode >= 400 && resp.StatusCode < 500 {
            return nil, fmt.Errorf("HTTP %d %s %s: %s: %w",
                resp.StatusCode, method, url, bodySnippet, extension.ErrNonRetryable)
        }
        // 5xx (and any other >=500): plain wrapped error → retryable.
        return nil, fmt.Errorf("HTTP %d %s %s: %s",
            resp.StatusCode, method, url, bodySnippet)
    }
    ```
       Note: the existing `if resp.StatusCode >= 400` is the load-bearing
       gate — 200, 204, 299 etc. fall through to the existing
       `return HTTPResponse{...}, nil` path with zero behavior change.
       The body-snippet truncation prevents log explosion when an API
       returns a multi-KB error response.

    7. Modify pkg/activity/execute_batch.go isRetryable. Insert the new
       errors.Is branch BEFORE the default-retryable return:
    ```go
    func isRetryable(err error) bool {
        if err == nil { return false }
        var appErr *temporal.ApplicationError
        if errors.As(err, &appErr) { return !appErr.NonRetryable() }

        // Fix A (quick 260502-onc): extensions outside the temporal
        // firewall (e.g., pkg/extension/builtin/http) cannot construct a
        // *temporal.ApplicationError directly. The extension.ErrNonRetryable
        // sentinel is the contract: any error wrapping it via fmt.Errorf
        // %w is treated as non-retryable. Plain wrapped errors continue
        // to default retryable per the transient-failure assumption.
        if errors.Is(err, extension.ErrNonRetryable) {
            return false
        }
        return true
    }
    ```
       Add the import to `pkg/activity/execute_batch.go`:
       `"github.com/mikelalcon/skytime/pkg/extension"`.

       Note: pkg/extension is already used elsewhere in pkg/activity
       (action_executor.go, classify.go, dispatch.go, credential_cache.go,
       options.go) but execute_batch.go does NOT currently import it
       (verified by `grep -l '"github.com/mikelalcon/skytime/pkg/extension"'
       pkg/activity/*.go`). The new `errors.Is(err, extension.ErrNonRetryable)`
       branch in `isRetryable` is the first use of pkg/extension in this
       file — the executor must add the import to the import block at the
       top, alphabetically grouped with the existing
       `"github.com/mikelalcon/skytime/pkg/dag"`.

       IMPORTANT: when isRetryable returns false for an
       extension.ErrNonRetryable error, the caller in ExecuteBatch
       (line 99) records `dag.NonRetryableErrResult{Idx: idx, Err: err}`.
       The Err field carries the ORIGINAL wrapped error, so the
       interpreter and ultimately the renderer/CLI see the
       "HTTP 404 GET ..." message. We DO NOT need to wrap into a
       temporal.ApplicationError ourselves at this point because
       ExecuteBatch never returns this error to Temporal — it returns
       (results, nil) per D2-14 — so the wrapping is unnecessary noise.

       This subtlety is the load-bearing decision: the extension sentinel
       is consumed at the ExecuteBatch dispatch layer, not propagated up
       to Temporal. The interpreter's walkStep already handles the
       results slice (currently ignores it via `_ = results`); the future
       v1.x story for surfacing NonRetryableErrResult to flow-level
       failure is out of scope here. For NOW, the workflow surfaces the
       failure because... wait, look more carefully at walk_step.go: when
       ExecuteBatch returns (results, nil), walkStep returns nil and the
       workflow keeps walking. THIS IS A BUG that Fix A surfaces.

       Resolution: in walk_step.go (Task 2 below), iterate `results` after
       ExecuteActivity Get and convert any NonRetryableErrResult to a
       returned error so the workflow fails. This is necessary for Fix C
       (failure rendering) to fire — without it, the flow_complete event
       arrives with err_count=0 and the renderer prints success.

       Task 1 stops at the activity layer; Task 2 wires results → workflow
       failure in walk_step.go.

    8. Run tests 3–8 again — must all PASS now:
       `go test ./pkg/extension/... ./pkg/activity/... -race -count=1`

    9. Sanity: run the FULL pre-existing test set in those packages to
       confirm zero regressions:
       `go test ./pkg/extension/... ./pkg/activity/... -race -count=1`
       Pay particular attention to TestExtension_GetSucceedsAgainstHTTPTestServer
       (200 path) and TestExtension_PostSendsBody (201 path) — these
       MUST still pass; they exercise the 2xx fall-through.
  </action>
  <verify>
    <automated>cd /Users/mikel/dev/ai/temporero &amp;&amp; go test ./pkg/extension/... ./pkg/activity/... -race -count=1</automated>
  </verify>
  <done>
    - pkg/extension/error.go exists with ErrNonRetryable sentinel + doc comment.
    - pkg/extension/error_test.go passes both new tests.
    - http_test.go has 4 new tests covering 404 (NonRetryable), 500 (Retryable),
      2xx fall-through (200/204/299), and 422 on POST (NonRetryable on body branch).
    - All 4 new http tests PASS.
    - execute_batch_test.go has 2 new tests (unit on isRetryable + integration through ExecuteBatch); both PASS.
    - All pre-existing tests in pkg/extension/... and pkg/activity/... still PASS.
    - `go vet ./pkg/extension/... ./pkg/activity/...` clean.
  </done>
</task>

<!-- ========================================================================== -->
<!-- TASK 2: Fix B — walk_step status= summary via reflection + results gating  -->
<!-- ========================================================================== -->

<task type="auto" tdd="true">
  <name>Task 2: Fix B — walkStep populates status=N summary on success AND fails workflow on NonRetryableErrResult</name>
  <files>
    pkg/interpreter/walk_step.go,
    pkg/interpreter/walk_step_test.go
  </files>
  <behavior>
    Two coupled changes — both must land together because they share the
    success/failure plumbing in walkStep's defer.

    CHANGE B-1 (success summary):
    - On the success path (ExecuteActivity returned no error AND results
      contain no NonRetryableErrResult), walkStep extracts the Status
      field from the FIRST OkResult.Output via reflection and sets
      summary = "status=N". For multi-action steps (block batches),
      summary = "<N> ok" (concise — multiple actions don't have a single
      meaningful status).
    - When the OkResult.Output is nil OR has no Status int field
      (extensions whose Output struct doesn't model HTTP status),
      summary stays "" (best-effort — no error, just no annotation).
    - Reflection-based to preserve the firewall: pkg/interpreter does
      NOT import pkg/extension/builtin/http. Verified by the existing
      pkg/interpreter/firewall_test.go family — no new violations.

    CHANGE B-2 (failure routing):
    - After `workflow.ExecuteActivity(...).Get(ctx, &results)` succeeds
      (returns nil error), iterate `results`. If any entry is a
      `dag.NonRetryableErrResult`, walkStep returns a wrapped error
      carrying the .Err message so the workflow fails. The defer
      block sees the error and emits step_complete with status="err"
      and summary=err.Error() (existing behavior preserved).
    - This is REQUIRED for Fix A's NonRetryable HTTP failures to surface
      as workflow failures — without it, ExecuteBatch's (results, nil)
      return per D2-14 makes the workflow walk past the failure and
      finish "successfully" with err_count=0.
    - First NonRetryableErrResult wins (matches the activity's behavior
      of skipping subsequent actions in the same batch). The mid-block
      "first wins" semantic is covered transitively by the helper-level
      test `TestExtractFirstNonRetryable_FirstWins` (see action step 1
      below): it asserts that given `[Ok, NonRetryable{errA},
      NonRetryable{errB}, Skipped]` the helper returns errA, which is
      exactly the property `TestWalkStep_NonRetryableMidBlock_FailsWorkflow`
      would have asserted at the workflow level. The integration test
      `TestWalkStep_NonRetryableResult_FailsWorkflow` then proves the
      helper is wired into walkStep. We chose helper-level coverage for
      mid-block ordering to keep the test count manageable; if a future
      regression touches the wiring (not the helper), add the workflow-
      level mid-block test back.

    TESTS (pkg/interpreter/walk_step_test.go — NEW tests):
    - TestWalkStep_Summary_HTTPStatus: a 1-action batch where the mocked
      ExecuteBatch returns
      `dag.ActionResults{dag.OkResult{Idx: 0, Output: fakeStatusOutput{Status: 200}}}`.
      Use a small test-only `fakeStatusOutput` struct in the same _test.go
      file with a `Status int` field and an `IsOperationOutput()` method.
      Capture the slog records via a custom slog.Handler set as
      slog.SetDefault during the test. Assert: a step_complete record
      exists with `summary` attr equal to "status=200".
    - TestWalkStep_Summary_NoStatusField: same shape but the fake Output
      struct has NO Status field. Assert summary attr is "" (empty
      string, not missing).
    - TestWalkStep_Summary_MultiAction: a 3-action block batch that
      returns 3 OkResults. Assert summary attr is "3 ok".
    - TestWalkStep_NonRetryableResult_FailsWorkflow: a 1-action batch
      where ExecuteBatch returns
      `dag.ActionResults{dag.NonRetryableErrResult{Idx: 0, Err: errors.New("HTTP 404 GET ...")}}`
      with NIL error from ExecuteBatch (mirrors D2-14). Assert:
      env.GetWorkflowError() is non-nil; env.GetWorkflowError().Error()
      contains "HTTP 404"; the captured step_complete record has
      status="err" and summary contains "HTTP 404".
    - TestWalkStep_FirewallStillIntact (compile-time): not a runtime test
      per se; rely on pkg/interpreter/firewall_test.go::TestPkgInterpreter_*
      which run in the same package and would catch any
      `pkg/extension/builtin/http` import. No new test code, just
      verify that go build succeeds without introducing the import.
  </behavior>
  <action>
    RED PHASE:

    1. Add the new tests to pkg/interpreter/walk_step_test.go. Pattern
       to follow:
       - The existing TestWalkStep_HappyPath uses
         `env.OnActivity("ExecuteBatch", ...).Return(...)` to mock results.
       - For Summary tests, after env.ExecuteWorkflow, use the test
         workflow environment's logger introspection. The cleanest path:
         install a custom slog.Handler before env.ExecuteWorkflow that
         records every Info call into a []recordedLog slice; assert on
         the recorded `step_complete` record's `summary` attr.
       - Cleanest seam: env.SetTestTimeout / set a custom logger via
         `env.SetWorkerOptions` if available — but simpler is to set
         slog.SetDefault to a record-all handler and undo via t.Cleanup.
         walk_step.go uses workflow.GetLogger(ctx) which routes through
         the SDK's logger, NOT slog.Default — so we need a different
         capture path.

       SIMPLER capture path: examine pkg/interpreter/walk_step.go — it
       calls `workflow.GetLogger(ctx)`. The Temporal testsuite's
       TestWorkflowEnvironment lets us set a custom logger via
       `env.SetWorkerOptions(worker.Options{Logger: ...})` but Logger
       there is *log.Logger, not slog. Newer SDK supports slog directly.

       Verify the SDK's logger surface: read sdk-go's worker.Options
       fields. If the SDK exposes a slog handler injection point, use
       it. Otherwise, fall back to a custom log.Logger with a writer
       that captures and parses the log line for the step_complete
       attr-key+value pairs.

       FALLBACK if neither works cleanly: instead of capturing slog
       records, refactor a tiny pure-Go helper in walk_step.go:
       `func extractStatusSummary(results dag.ActionResults) string`
       and unit-test it directly. The helper is easy to test and the
       integration with workflow.GetLogger remains unverified at unit
       level — but the existing TestWalkStep_HappyPath proves the
       defer fires; the helper test proves the summary computation;
       end-to-end (Task 4) proves they connect.

       USE THE FALLBACK approach (extract helper + test directly):
       - extractStatusSummary(results dag.ActionResults) string returns
         "status=200" for 1-action with HTTPResponse-shaped Output;
         "3 ok" for multi-action all-OK; "" for nil/empty/no-Status-field.
       - extractFirstNonRetryable(results dag.ActionResults) error returns
         results[i].Err for the first NonRetryableErrResult, or nil if
         none. (Used by walkStep to set the named-return err.)

       So the REVISED tests are:
       - TestExtractStatusSummary_HTTPResponseShape: input
         dag.ActionResults{OkResult{Idx:0, Output: fakeStatusOutput{Status: 200}}};
         expected output "status=200".
       - TestExtractStatusSummary_NoStatusField: input
         dag.ActionResults{OkResult{Idx:0, Output: fakeNoStatusOutput{Foo: "bar"}}};
         expected "".
       - TestExtractStatusSummary_MultiAction: 3 OkResults; expected "3 ok".
       - TestExtractStatusSummary_NilOutput: OkResult{Output: nil}; expected "".
       - TestExtractStatusSummary_PointerOutput: OkResult{Output: &fakeStatusOutputPtr{Status: 404}};
         expected "status=404" (reflect Elem on Ptr).
       - TestExtractStatusSummary_StatusNotInt: a struct with `Status string`
         field; expected "" (Kind != Int).
       - TestExtractFirstNonRetryable_None: all OkResults; returns nil.
       - TestExtractFirstNonRetryable_FirstWins: input
         [Ok, NonRetryable{Err: errA}, NonRetryable{Err: errB}, Skipped];
         returns errA. (Covers the "first NonRetryable in a mixed slice
         wins" property — see BEHAVIOR M-3 note. This is the helper-level
         proxy for the originally-named TestWalkStep_NonRetryableMidBlock_FailsWorkflow.)
       - TestExtractFirstNonRetryable_SkippedIgnored: input
         [Ok, Skipped]; returns nil.

       Plus an integration test against the testsuite that verifies
       walkStep returns an error when the activity returns a
       NonRetryableErrResult (workflow fails):
       - TestWalkStep_NonRetryableResult_FailsWorkflow: as in the
         BEHAVIOR section above; uses env.OnActivity to return
         `dag.ActionResults{dag.NonRetryableErrResult{Idx:0, Err: errors.New("HTTP 404 ...")}}`.
         Assert env.GetWorkflowError() is non-nil and message contains
         "HTTP 404". (The summary capture is left to E2E in Task 4 —
         the unit test for the summary helper proves that part, the
         integration test proves the failure path.)

    2. Confirm RED:
       `go test ./pkg/interpreter/... -count=1 -run 'TestExtractStatusSummary|TestExtractFirstNonRetryable|TestWalkStep_NonRetryableResult_FailsWorkflow'`
       The Extract* tests fail (helpers don't exist). The
       NonRetryableResult test currently passes vacuously OR fails with
       a different reason — confirm what walk_step does today: it ignores
       results (`_ = results`), so the test will PASS today (workflow
       does not error). That makes the test the regression guard — it
       must FAIL initially because we WANT the workflow to fail.
       Confirm the test fails as a require.NotNil(env.GetWorkflowError())
       assertion — i.e., it sees nil error and we expected non-nil.

    GREEN PHASE:

    3. Modify pkg/interpreter/walk_step.go:

       a) Add `import "reflect"` to the import block.

       b) Add the two helpers BELOW activityOptionsForStep:
    ```go
    // extractStatusSummary computes the summary attr for a successful
    // step. Single-action steps with an Output struct exposing an `int`
    // field named "Status" render as "status=N" (the HTTP extension's
    // HTTPResponse shape). Multi-action steps render as "<N> ok"
    // (block batches don't have a single meaningful status). Steps
    // whose Output type does not have an int Status field render as
    // "" (empty — best-effort, no annotation).
    //
    // Reflection-based to preserve the firewall: pkg/interpreter cannot
    // import pkg/extension/builtin/http (interpreter is a foundation
    // package, builtin extensions are leaves). reflect.FieldByName is
    // O(struct-fields) per call — negligible at single-step granularity.
    func extractStatusSummary(results dag.ActionResults) string {
        if len(results) == 0 {
            return ""
        }
        if len(results) > 1 {
            return fmt.Sprintf("%d ok", len(results))
        }
        ok, isOK := results[0].(dag.OkResult)
        if !isOK || ok.Output == nil {
            return ""
        }
        v := reflect.ValueOf(ok.Output)
        if v.Kind() == reflect.Ptr {
            if v.IsNil() {
                return ""
            }
            v = v.Elem()
        }
        if v.Kind() != reflect.Struct {
            return ""
        }
        f := v.FieldByName("Status")
        if !f.IsValid() || !f.CanInterface() || f.Kind() != reflect.Int {
            return ""
        }
        return fmt.Sprintf("status=%d", f.Int())
    }

    // extractFirstNonRetryable scans the per-action result slice and
    // returns the first NonRetryableErrResult's Err, or nil if none is
    // present. Used by walkStep to convert the activity layer's D2-14
    // (results, nil) "soft failure" into a workflow-level failure so
    // the renderer surfaces it.
    func extractFirstNonRetryable(results dag.ActionResults) error {
        for _, r := range results {
            if nr, ok := r.(dag.NonRetryableErrResult); ok {
                return nr.Err
            }
        }
        return nil
    }
    ```

       c) Modify the body of walkStep — replace the `_ = results` line
          with the new dispatch:
    ```go
    if err = workflow.ExecuteActivity(actx, "ExecuteBatch", step.Actions).Get(ctx, &results); err != nil {
        return err
    }
    // D2-14 routing (Fix B / quick 260502-onc): if any per-action result
    // is NonRetryableErrResult, surface as workflow-level failure so the
    // renderer prints flow failed. Without this the activity's
    // (results, nil) return makes the workflow walk past the failure.
    if perActionErr := extractFirstNonRetryable(results); perActionErr != nil {
        return perActionErr
    }
    return nil
    ```
          DELETE the existing `_ = results` line and the comment block
          above it about "Plan 03-03 v1 simplification: per-action
          results are observable in history, but this walker does NOT
          thread them into state" — replace with a Fix B comment
          explaining the new behavior.

       d) Modify the defer block to populate summary on success too:
    ```go
    defer func() {
        status := "ok"
        summary := ""
        if err != nil {
            status = "err"
            summary = err.Error()
        } else {
            summary = extractStatusSummary(results)
        }
        logger.Info("skytime",
            "event", "step_complete",
            "kind", "step",
            "status", status,
            "duration_ms", workflow.Now(ctx).Sub(start).Milliseconds(),
            "idx", idx, "total", total, "path", path,
            "summary", summary,
        )
    }()
    ```
          IMPORTANT: the `results` variable referenced in the defer must
          be in scope. Currently it's declared inside walkStep with
          `var results dag.ActionResults`. The defer captures it by
          closure (Go's defer-in-named-return idiom), so it sees the
          post-ExecuteActivity value. No structural changes needed —
          just ensure `results` is declared at function scope (it
          already is per current file).

    4. Add the test-only fakeStatusOutput types AT THE TOP of
       walk_step_test.go (under the imports):
    ```go
    type fakeStatusOutput struct{ Status int }
    func (fakeStatusOutput) IsOperationOutput() {}

    type fakeStatusOutputPtr struct{ Status int }
    func (*fakeStatusOutputPtr) IsOperationOutput() {}

    type fakeNoStatusOutput struct{ Foo string }
    func (fakeNoStatusOutput) IsOperationOutput() {}

    type fakeStringStatusOutput struct{ Status string }
    func (fakeStringStatusOutput) IsOperationOutput() {}
    ```

    5. Run tests — must all PASS:
       `go test ./pkg/interpreter/... -race -count=1`

    6. Run the firewall test explicitly to confirm no new HTTP import:
       `go test ./pkg/interpreter/... -run TestPkgInterpreter -count=1 -v`
       (also confirm the broader pkg/activity firewall test still passes:
       `go test ./pkg/activity/... -run TestNoTemporalImports -count=1 -v`)
  </action>
  <verify>
    <automated>cd /Users/mikel/dev/ai/temporero &amp;&amp; go test ./pkg/interpreter/... -race -count=1</automated>
  </verify>
  <done>
    - extractStatusSummary helper added to walk_step.go with reflect-based Status read.
    - extractFirstNonRetryable helper added to walk_step.go.
    - walkStep returns extractFirstNonRetryable(results) on the success-from-activity path.
    - walkStep defer block sets summary = extractStatusSummary(results) on success.
    - 8 new helper unit tests (incl. TestExtractFirstNonRetryable_FirstWins covering mid-block first-wins) + 1 integration test PASS.
    - All pre-existing pkg/interpreter tests PASS.
    - `grep -r "pkg/extension/builtin/http" pkg/interpreter/` returns no matches (firewall preserved).
  </done>
</task>

<!-- ========================================================================== -->
<!-- TASK 3: Fix C — progress renderer flow_failed line                          -->
<!-- ========================================================================== -->

<task type="auto" tdd="true">
  <name>Task 3: Fix C — progressHandler renders flow_failed line on err_count > 0</name>
  <files>
    pkg/cli/progress.go,
    pkg/cli/progress_test.go
  </files>
  <behavior>
    The renderer holds the most recent step_complete-with-status="err"
    context in a per-handler `lastErr` field. On flow_complete with
    err_count > 0, it renders the failed line; otherwise, the existing
    "complete" line.

    SCHEMA STABILITY: the interpreter's slog event schema does NOT change.
    No new attrs added to any event. The renderer derives failure context
    from records it already sees (step_complete with status="err").

    LIFECYCLE: lastErr is reset to nil on every flow_start so a long-lived
    handler (one process, multiple workflow executions) doesn't leak
    failure state across runs.

    CONCURRENCY: progressHandler is documented as not safe for concurrent
    use — a single workflow's slog records arrive serially through
    workflow.GetLogger. We do NOT add a mutex.

    TESTS (pkg/cli/progress_test.go — NEW):
    - TestProgress_FlowFailed: send a sequence
      [flow_start, step_dispatch idx=2 total=3, step_complete idx=2 status=err
       summary="HTTP 404 ...", flow_complete err_count=1 ok_count=1 total_ms=300]
      Assert the rendered output contains "[skytime]" and "flow failed"
      and "step 2/3" and "HTTP 404" and "300ms".
      Assert the rendered output does NOT contain "flow complete"
      (the success branch is mutually exclusive).
    - TestProgress_FlowComplete_NoFailure: send
      [flow_start, ..., flow_complete err_count=0 ok_count=3 total_ms=433]
      Assert output contains "flow complete  3/3 steps" and does NOT
      contain "flow failed".
    - TestProgress_LastErrResetsOnFlowStart: send
      [flow_start, step_complete err, flow_complete err_count=1,
       flow_start, flow_complete err_count=0]
      Assert the SECOND flow's render contains "flow complete" and does
      NOT contain "flow failed" (no leak from the first flow's lastErr).
    - TestProgress_FlowFailed_NoLastErr: send a flow_complete with
      err_count=1 BUT no preceding step_complete with status=err
      (degenerate / synthetic case). Assert the render still emits
      "flow failed" and uses a placeholder summary like "(unknown)" or
      "(no per-step error captured)" — pick one and pin it. (This guards
      against renderer panics when the input event sequence is
      malformed.)
  </behavior>
  <action>
    RED PHASE:

    1. Add the 4 new tests to pkg/cli/progress_test.go using the existing
       newProgressHandler + bytes.Buffer pattern (see TestProgress_BazelFormat).
       Pin the exact failed-line shape:
       `[skytime] flow failed  step 2/3 (HTTP 404 ...)  total 300ms`
       (with the colorErr-wrapped "[skytime]" or use colorBanner for the
       banner and rely on red coloring of the body — pick colorBanner +
       colorErr on the inner words for parity with the ✗ marker style).

       Actually, simplest visual: keep the banner color (dim cyan) for
       "[skytime]" parity with the success line, and color the WORD
       "failed" red. This makes the banner column stable across success
       and failure (consultants grep for "[skytime]" prefix) while the
       failure stands out.

       Final line shape:
       `[skytime] flow <RED>failed</RED>  step 2/3 (HTTP 404 ...)  total 300ms`

       For non-TTY (test buffer), the wrap functions are no-ops, so the
       greppable plain-text shape is:
       `[skytime] flow failed  step 2/3 (HTTP 404 ...)  total 300ms`

    2. Confirm RED: `go test ./pkg/cli/... -count=1 -run TestProgress_Flow`
       — the 4 new tests fail (renderFlowComplete still emits the
       success line unconditionally).

    GREEN PHASE:

    3. Modify pkg/cli/progress.go:

       a) Add a small struct type and field to progressHandler:
    ```go
    // failureContext captures the most recent step_complete-with-err
    // record so renderFlowComplete can attribute the failure when
    // err_count > 0. Reset on every flow_start.
    type failureContext struct {
        idx     int64
        total   int64
        summary string
    }
    ```
          Add `lastErr *failureContext` field to progressHandler struct.

       b) Make WithAttrs / WithGroup propagate lastErr (they currently
          construct fresh handlers — preserve lastErr on the new instance):
    ```go
    func (p *progressHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
        return &progressHandler{wrapped: p.wrapped.WithAttrs(attrs), out: p.out, lastErr: p.lastErr}
    }
    func (p *progressHandler) WithGroup(name string) slog.Handler {
        return &progressHandler{wrapped: p.wrapped.WithGroup(name), out: p.out, lastErr: p.lastErr}
    }
    ```
          Note: this is a shallow-copy — both handlers point at the same
          *failureContext. That's fine for our use (a single workflow,
          serial events) and matches the existing ttyKnown/tty
          handling (NOT propagated — let the new handler recompute).

       c) Modify renderFlowStart to clear state:
    ```go
    func (p *progressHandler) renderFlowStart(a attrMap) error {
        p.lastErr = nil  // reset: new flow → no prior failure context
        // ... existing body unchanged ...
    }
    ```

       d) Modify renderStepComplete to record the failure context:
    ```go
    func (p *progressHandler) renderStepComplete(a attrMap) error {
        status := a.str("status")
        // ... existing dur/summary/path/idx extraction ...
        if status == "err" {
            // Capture for renderFlowComplete to attribute the failure.
            // The interpreter does not emit total here, but step_dispatch
            // events for the same step include total — fall back to a
            // best-effort 0 when missing (renderer prints "step 2/0"
            // which is uglier but never crashes).
            p.lastErr = &failureContext{
                idx:     idx,
                total:   a.int("total"),
                summary: summary,
            }
        }
        // ... existing render body unchanged ...
    }
    ```

       e) Modify renderFlowComplete to branch:
    ```go
    func (p *progressHandler) renderFlowComplete(a attrMap) error {
        ok := a.int("ok_count")
        errc := a.int("err_count")
        totalMs := a.int("total_ms")

        if errc > 0 {
            // Failure path. Use lastErr when available; fall back to
            // a placeholder when the renderer received the
            // flow_complete record but no step_complete-with-err
            // preceded it (defensive — should not happen with the
            // current interpreter event ordering, but a malformed
            // event sequence must not crash the renderer).
            idx := int64(0)
            total := ok + errc
            summary := "(no per-step error captured)"
            if p.lastErr != nil {
                idx = p.lastErr.idx
                if p.lastErr.total > 0 {
                    total = p.lastErr.total
                }
                if p.lastErr.summary != "" {
                    summary = p.lastErr.summary
                }
            }
            line := fmt.Sprintf("%s flow %s  step %d/%d (%s)  total %dms",
                p.colorBanner("[skytime]"),
                p.colorErr("failed"),
                idx, total, summary, totalMs)
            return p.println(line)
        }

        line := fmt.Sprintf("%s flow complete  %d/%d steps  total %dms",
            p.colorBanner("[skytime]"), ok, ok+errc, totalMs)
        return p.println(line)
    }
    ```

    4. Run tests — must all PASS:
       `go test ./pkg/cli/... -race -count=1 -run TestProgress`

    5. Run the FULL pkg/cli test set:
       `go test ./pkg/cli/... -race -count=1`
       Confirm zero regressions in flow_start / step_dispatch / branch /
       passthrough / nested-path tests.
  </action>
  <verify>
    <automated>cd /Users/mikel/dev/ai/temporero &amp;&amp; go test ./pkg/cli/... -race -count=1</automated>
  </verify>
  <done>
    - progressHandler has lastErr *failureContext field.
    - renderFlowStart clears lastErr.
    - renderStepComplete records lastErr when status=="err".
    - renderFlowComplete branches on err_count and prints the failed line with red "failed" word.
    - WithAttrs / WithGroup preserve lastErr.
    - 4 new tests PASS (FlowFailed, FlowComplete_NoFailure, LastErrResetsOnFlowStart, FlowFailed_NoLastErr).
    - All pre-existing pkg/cli tests PASS.
  </done>
</task>

<!-- ========================================================================== -->
<!-- TASK 4: Fix D — corpus update + e2e happy + e2e unhappy smokes              -->
<!-- ========================================================================== -->

<task type="auto" tdd="true">
  <name>Task 4: Fix D — corpus update to /repos/octocat/Hello-World + end-to-end smokes (happy + unhappy)</name>
  <files>
    examples/skeleton/simple_check.star,
    examples/skeleton/parallel_fanout.star,
    tests/e2e_skytime_run_test.go
  </files>
  <behavior>
    CORPUS UPDATE:
    - simple_check.star: change every gh.get(path="/repos/example/repo[/...]")
      to gh.get(path="/repos/octocat/Hello-World[/...]"). The endpoints
      remain GET-only (idempotent), so the existing differential test's
      D2-05 lint passes unchanged. Add a docstring sentence noting
      `--input='{"repo_path":"..."}'` is illustrative; v1 doesn't have a
      script-builds-path mechanism that lets `step(action=gh.get(path=
      ctx.repo_path))` work because step kwargs are static at parse time.
    - parallel_fanout.star: the `check_one` helper flow's block-batch
      `gh.get(path="/repos/example/...")` paths swap to
      `/repos/octocat/Hello-World[/...]`. Use 3 distinct sub-paths
      (e.g., `/repos/octocat/Hello-World`, `/repos/octocat/Hello-World/branches`,
      `/repos/octocat/Hello-World/contributors`) to keep the demo
      meaningful (3 different but real endpoints).
    - The differential test (tests/differential_test.go) uses
      AlwaysOkDispatch and never hits real network — corpus paths are
      IRRELEVANT to dryrun behavior; the test must continue to pass
      after the swap purely from the parse + dryrun perspective.

    E2E TEST FILE (tests/e2e_skytime_run_test.go — NEW):
    Two tests, both build the skytime binary into a temp directory and
    invoke it. Both are gated:
      a) Build tag `//go:build !windows` at the top — see SUBPROCESS
         TEARDOWN below for why this is platform-specific. Windows users
         on this project would need WSL or Docker for `temporal server
         start-dev` anyway, so skipping the file on Windows is acceptable.
      b) `t.Skip` when temporal CLI not in PATH or network unreachable.

    Network availability in CI is the real concern; gate behind `t.Skip`
    when:
      a) Build fails (the build step itself catches non-network errors)
      b) The pre-flight `curl -sf https://api.github.com/repos/octocat/Hello-World`
         from within the test fails (no network).

    The smoke endpoint github.com/octocat/Hello-World is a public,
    stable endpoint maintained by GitHub Inc. since 2008 — extremely
    unlikely to change.

    SUBPROCESS TEARDOWN (M-2 from checker iteration 1):
    `temporal server start-dev` spawns child processes (UI server,
    persistence worker, internal gRPC frontend). `Process.Kill()` on
    the direct child only signals the parent shell wrapper — children
    can orphan on test panic, alternate exit paths, or Ctrl-C. The
    e2e test uses Unix process-group semantics:

      1. Set `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}`
         BEFORE `cmd.Start()` — places the temporal subprocess in its
         own process group with PGID == its own PID.
      2. Cleanup function: `syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)`
         (negative PID = whole process group). Wait up to 3s. If still
         alive, escalate to `syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)`.
      3. Cleanup wired BOTH from defer in TestMain AND from a
         signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM) goroutine —
         so a Ctrl-C during a long-running test still tears down the
         subprocess group cleanly.
      4. Doc comment at the top of the test file explaining the
         choice (so future maintainers don't "simplify" it to plain Kill).

    Per-test specs:

    - TestE2E_SkytimeRun_Happy: builds /tmp/skytime via `go build -o`,
      runs `/tmp/skytime run examples/skeleton/simple_check.star
      --flow=simple_check --input='{"repo_path":"octocat/hello"}'`,
      captures stdout + stderr. Asserts:
        a) exit code == 0
        b) stderr contains "status=200"
        c) stderr contains "[skytime] flow complete"
        d) stderr does NOT contain "✗"
        e) stderr does NOT contain "flow failed"
        f) ORDERING (M-5): the byte-offset of "status=200" in stderr
           is LESS THAN the byte-offset of "[skytime] flow complete".
           Defends against accidental partial-success scenarios where
           the renderer might emit a flow-complete line before any
           per-step status was logged (e.g., due to an event-ordering
           regression).
    - TestE2E_SkytimeRun_Unhappy: writes a temp .star file pointing at
      /repos/totally/does-not-exist-xyzzy12345, runs skytime run against
      it. Asserts:
        a) err is non-nil AND of TYPE *exec.ExitError (M-4 — distinguishes
           a real non-zero exit from a context-cancellation timeout, which
           would surface as a different error type and would erroneously
           pass a plain require.Error).
        b) cmd.ProcessState.ExitCode() != -1 (defensive — if the process
           was killed by signal, ExitCode is -1 and ExitError still wraps
           it; we want a TRUE non-zero exit code, not a kill).
        c) stderr contains "✗"
        d) stderr contains "HTTP 404"
        e) stderr contains "[skytime] flow failed"
        f) stderr does NOT contain "[skytime] flow complete"

    Both tests use os/exec to invoke the binary, with a 90s timeout via
    context.WithTimeout. The temp .star file lives in a t.TempDir().
    The binary build also lives in a t.TempDir() so parallel `go test`
    invocations don't collide.

    PRE-FLIGHT NETWORK CHECK: a small helper
    `requireNetwork(t *testing.T)` issues a 5s context-bound stdhttp.Get
    against https://api.github.com/zen (a deliberately tiny GitHub
    endpoint) and t.Skip()s on any error including timeout. This makes
    the test robust to offline CI.

    The dev server: skytime run uses an embedded transient worker per
    Phase 4 plan 04-05 ("skytime run validates first via pkg/validator,
    then connects Temporal..."). Wait — re-read run.go: it uses
    sdkclient.ExecuteWorkflow against a dialled client. Look at
    connect.go to see if there's a mode where no Temporal cluster is
    needed. If `skytime run` REQUIRES a live Temporal cluster, the e2e
    tests need to spin one up — but that's heavy. Check first what
    happens when you run `skytime run` against the default
    127.0.0.1:7233 with no server running: does it fail-fast or hang?
    The dev-server subcommand (Phase 4 plan 04-06) shells out to
    `temporal server start-dev`; this should be the prereq.

    PRAGMATIC: spin up `temporal server start-dev` once in TestMain via
    exec.CommandContext (with Setpgid for clean teardown — see above),
    wait for the gRPC port, run both tests, kill on cleanup. Skip both
    tests if the `temporal` binary is not in PATH (pre-flight check via
    exec.LookPath). This matches how the dev_server_test.go already uses
    lookPath for testability.

    MUST: explicitly set --task-queue=skytime on the run invocation if
    the embedded worker uses a different default. Check run.go line 128:
    `TaskQueue: "skytime"` is hard-coded. Good — no extra flag needed.
  </behavior>
  <action>
    1. CORPUS UPDATE — edit examples/skeleton/simple_check.star:
       - Change `path = "/repos/example/repo"` to
         `path = "/repos/octocat/Hello-World"`.
       - Change `path = "/repos/example/repo/branches"` to
         `path = "/repos/octocat/Hello-World/branches"`.
       - In the docstring, add a note:
         "Note: --input='{\"repo_path\":\"...\"}' is illustrative; v1
         does not yet support `step(action=gh.get(path=ctx.repo_path))`
         (step kwargs are static at parse time). The corpus path is
         hardcoded to /repos/octocat/Hello-World for the happy-path demo;
         v1.x will add script-builds-path when a real consumer needs it."

    2. CORPUS UPDATE — edit examples/skeleton/parallel_fanout.star:
       - In the check_one flow's block batch, replace the three
         /repos/example/{one,two,three} paths with:
           "/repos/octocat/Hello-World"
           "/repos/octocat/Hello-World/branches"
           "/repos/octocat/Hello-World/contributors"
       - These are three real GET endpoints on the same repo, all return
         200 against api.github.com. The block-batch idempotency lint
         (D2-05) is satisfied: all gets, all idempotent per D4-14.
       - No docstring change needed beyond the existing one.

    3. RUN THE DIFFERENTIAL TEST — confirm corpus update doesn't break
       static + dry-run agreement:
       `go test ./tests/... -count=1 -run TestDifferentialCorpus -v`
       Must PASS — AlwaysOkDispatch never hits the network. If this
       fails, the corpus syntax broke; revisit.

    4. CREATE tests/e2e_skytime_run_test.go (the NEW test file). Build tag
       at very top of file (BEFORE the package clause) excludes Windows;
       see M-2 rationale for why process-group teardown is Unix-only:

    ```go
    //go:build !windows

    // Note: subprocess teardown uses process-group kill (Setpgid +
    // syscall.Kill(-pid, ...)) so the `temporal server start-dev`
    // subprocess's children (UI, persistence, frontend gRPC) are
    // reliably reaped on test exit, panic, or interrupt. This is
    // Unix-specific (Setpgid does not exist on Windows). Windows users
    // who want to run these e2e tests must use WSL or Docker, which
    // they would already need to obtain `temporal` itself for
    // start-dev mode. Hence the build tag rather than a runtime skip.

    package skytime_e2e_test

    import (
        "bytes"
        "context"
        "errors"
        "fmt"
        "net/http"
        "os"
        "os/exec"
        "os/signal"
        "path/filepath"
        "strings"
        "sync"
        "syscall"
        "testing"
        "time"

        "github.com/stretchr/testify/require"
    )

    var (
        skytimeBinOnce sync.Once
        skytimeBin     string
        skytimeBinErr  error
        devServerCmd   *exec.Cmd
        devServerOnce  sync.Once
        devServerErr   error
    )

    // teardownDevServer is the centralized cleanup. Idempotent: safe
    // to call from defer AND from a signal handler.
    func teardownDevServer() {
        if devServerCmd == nil || devServerCmd.Process == nil {
            return
        }
        pgid := devServerCmd.Process.Pid // because Setpgid → PGID == PID
        // Step 1: SIGTERM the whole process group.
        _ = syscall.Kill(-pgid, syscall.SIGTERM)

        // Step 2: wait up to 3 seconds for graceful exit.
        done := make(chan error, 1)
        go func() { done <- devServerCmd.Wait() }()
        select {
        case <-done:
            // graceful exit — done.
        case <-time.After(3 * time.Second):
            // Step 3: escalate to SIGKILL on the group.
            _ = syscall.Kill(-pgid, syscall.SIGKILL)
            <-done // reap
        }
    }

    // TestMain spawns `temporal server start-dev` once for the whole
    // package; waits for the gRPC port; tears down on package exit
    // OR on Ctrl-C / SIGTERM via the signal handler.
    // Skips the entire package if temporal isn't installed — the
    // sub-tests then never run, so individual t.Skip() in each test
    // would be redundant.
    func TestMain(m *testing.M) {
        if _, err := exec.LookPath("temporal"); err != nil {
            fmt.Fprintln(os.Stderr, "skipping skytime e2e package: temporal CLI not in PATH")
            os.Exit(0)
        }

        // Wire signal handler BEFORE m.Run so a Ctrl-C mid-test
        // still tears down the dev server. The handler calls
        // teardownDevServer (idempotent with the defer below) and
        // exits with the conventional 128+signal code.
        sigCh := make(chan os.Signal, 1)
        signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
        go func() {
            sig := <-sigCh
            fmt.Fprintf(os.Stderr, "skytime e2e: caught %s, tearing down dev server\n", sig)
            teardownDevServer()
            // Use the conventional 128+N exit code for signal-induced exits.
            n := 0
            switch sig {
            case syscall.SIGINT:
                n = 2
            case syscall.SIGTERM:
                n = 15
            }
            os.Exit(128 + n)
        }()

        code := m.Run()
        teardownDevServer()
        os.Exit(code)
    }

    // ensureBinary builds /tmp/<unique>/skytime once per process.
    func ensureBinary(t *testing.T) string {
        t.Helper()
        skytimeBinOnce.Do(func() {
            tmp, err := os.MkdirTemp("", "skytime-e2e-bin-*")
            if err != nil {
                skytimeBinErr = err
                return
            }
            bin := filepath.Join(tmp, "skytime")
            cmd := exec.Command("go", "build", "-o", bin, "./cmd/skytime")
            cmd.Dir = findModuleRootE2E(t)
            out, err := cmd.CombinedOutput()
            if err != nil {
                skytimeBinErr = fmt.Errorf("go build skytime: %w: %s", err, string(out))
                return
            }
            skytimeBin = bin
        })
        require.NoError(t, skytimeBinErr)
        return skytimeBin
    }

    // ensureDevServer launches temporal server start-dev once in its
    // own process group (Setpgid) so teardown can SIGTERM/SIGKILL
    // the whole subtree (start-dev spawns UI + persistence children).
    // Polls the default gRPC port (7233) for readiness with a 30s timeout.
    func ensureDevServer(t *testing.T) {
        t.Helper()
        devServerOnce.Do(func() {
            devServerCmd = exec.Command(
                "temporal", "server", "start-dev",
                "--ui-port", "0", // disable UI to avoid port collision
            )
            // Critical: Setpgid → child runs in its own process group
            // (PGID == its PID). teardownDevServer relies on this to
            // signal the WHOLE group via syscall.Kill(-pid, ...).
            devServerCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

            if err := devServerCmd.Start(); err != nil {
                devServerErr = fmt.Errorf("start temporal server: %w", err)
                return
            }
            // Poll 127.0.0.1:7233 readiness via the temporal CLI's
            // namespace describe (fastest cheap check).
            deadline := time.Now().Add(30 * time.Second)
            for time.Now().Before(deadline) {
                check := exec.Command("temporal", "operator", "namespace", "describe", "default", "--address", "127.0.0.1:7233")
                if err := check.Run(); err == nil {
                    return
                }
                time.Sleep(500 * time.Millisecond)
            }
            devServerErr = errors.New("temporal server start-dev did not become ready within 30s")
        })
        require.NoError(t, devServerErr)
    }

    // requireNetwork t.Skip()s when api.github.com is unreachable.
    func requireNetwork(t *testing.T) {
        t.Helper()
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/zen", nil)
        resp, err := http.DefaultClient.Do(req)
        if err != nil {
            t.Skipf("network unavailable: %v", err)
        }
        defer resp.Body.Close()
        if resp.StatusCode != 200 {
            t.Skipf("api.github.com returned %d on /zen pre-flight", resp.StatusCode)
        }
    }

    // findModuleRootE2E walks up to find go.mod.
    func findModuleRootE2E(t *testing.T) string {
        t.Helper()
        cwd, err := os.Getwd()
        require.NoError(t, err)
        dir := cwd
        for {
            if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
                return dir
            }
            parent := filepath.Dir(dir)
            if parent == dir {
                t.Fatalf("could not find go.mod walking up from %s", cwd)
            }
            dir = parent
        }
    }

    // TestE2E_SkytimeRun_Happy: full stack — binary build → dev-server →
    // skytime run examples/skeleton/simple_check.star → assert
    // status=200 + flow complete + no ✗ + correct ordering.
    func TestE2E_SkytimeRun_Happy(t *testing.T) {
        requireNetwork(t)
        ensureDevServer(t)
        bin := ensureBinary(t)
        root := findModuleRootE2E(t)

        ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
        defer cancel()

        var stdout, stderr bytes.Buffer
        cmd := exec.CommandContext(ctx, bin,
            "run",
            filepath.Join(root, "examples", "skeleton", "simple_check.star"),
            "--flow", "simple_check",
            "--input", `{"repo_path":"octocat/hello"}`,
            "--address", "127.0.0.1:7233",
        )
        cmd.Stdout = &stdout
        cmd.Stderr = &stderr
        err := cmd.Run()

        t.Logf("stdout: %s", stdout.String())
        t.Logf("stderr: %s", stderr.String())

        require.NoError(t, err, "skytime run failed: %s", stderr.String())

        out := stderr.String()
        require.Contains(t, out, "status=200",
            "Fix B summary missing — expected status=200")
        require.Contains(t, out, "[skytime] flow complete",
            "Fix C complete line missing")
        require.NotContains(t, out, "✗",
            "happy path must not render ✗ failure marker")
        require.NotContains(t, out, "flow failed",
            "happy path must not render flow failed")

        // M-5 (checker iteration 1): ordering guard. The status=200
        // summary MUST appear in the stream BEFORE the flow_complete
        // line. require.Contains alone is order-insensitive — without
        // this guard a partial-success regression where flow_complete
        // emits before any step_complete could pass silently.
        statusIdx := strings.Index(out, "status=200")
        completeIdx := strings.Index(out, "[skytime] flow complete")
        require.True(t,
            statusIdx >= 0 && completeIdx >= 0 && statusIdx < completeIdx,
            "status=200 must appear before flow complete (got status=%d, complete=%d)",
            statusIdx, completeIdx)
    }

    // TestE2E_SkytimeRun_Unhappy: build a temp .star file pointing at a
    // definitely-404 endpoint; assert ✗ + HTTP 404 + flow failed; exit non-zero
    // via *exec.ExitError (NOT context cancellation).
    func TestE2E_SkytimeRun_Unhappy(t *testing.T) {
        requireNetwork(t)
        ensureDevServer(t)
        bin := ensureBinary(t)

        starContent := `gh = http.endpoint(base_url = "https://api.github.com")
flow(name = "bad", inputs = {}, steps = [
    step(action = gh.get(path = "/repos/totally/does-not-exist-xyzzy12345")),
])
`
        starFile := filepath.Join(t.TempDir(), "bad.star")
        require.NoError(t, os.WriteFile(starFile, []byte(starContent), 0o644))

        ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
        defer cancel()

        var stdout, stderr bytes.Buffer
        cmd := exec.CommandContext(ctx, bin,
            "run", starFile,
            "--flow", "bad",
            "--input", "{}",
            "--address", "127.0.0.1:7233",
        )
        cmd.Stdout = &stdout
        cmd.Stderr = &stderr
        err := cmd.Run()

        t.Logf("stdout: %s", stdout.String())
        t.Logf("stderr: %s", stderr.String())

        // M-4 (checker iteration 1): plain require.Error doesn't
        // distinguish a true non-zero exit from a context-cancellation
        // timeout (which would surface as a context.DeadlineExceeded-
        // wrapped error type). Pin to *exec.ExitError to confirm the
        // process actually ran to completion AND exited non-zero.
        require.Error(t, err, "skytime run on a 404-pointing flow must exit non-zero")
        require.IsType(t, &exec.ExitError{}, err,
            "expected *exec.ExitError (real non-zero exit), got %T: %v — likely a context-cancellation timeout, dev-server hang, or binary crash",
            err, err)

        // Defensive: confirm the exit code is a real number, not -1
        // (which exec sets when the process was killed by signal).
        require.NotNil(t, cmd.ProcessState, "ProcessState must be populated after Run()")
        require.NotEqual(t, -1, cmd.ProcessState.ExitCode(),
            "exit code -1 means killed-by-signal; want a true non-zero exit from skytime run")

        out := stderr.String()
        require.Contains(t, out, "✗",
            "unhappy path must render ✗ failure marker (Fix C)")
        require.Contains(t, out, "HTTP 404",
            "unhappy path must surface HTTP 404 in error message (Fix A)")
        require.Contains(t, out, "[skytime] flow failed",
            "unhappy path must render flow failed line (Fix C)")
        // Defense in depth: must not also print success line.
        require.NotContains(t, out, "[skytime] flow complete")

        _ = strings.TrimSpace // import-keeper if not used
    }
    ```

       NOTES on the e2e test:
       - The `--address` flag: check pkg/cli/run.go and connect.go to
         confirm the flag name. If it's `--temporal-address`, use that
         instead; pull from cli's flag list. The Phase 4 SUMMARY notes
         "SKYTIME_TEMPORAL_*" env-var fallbacks for six flags — the
         flag is canonical; env var is the fallback. Read flags.go
         briefly to confirm exact spelling before writing the test.
       - If skytime run has a `--task-queue` flag and the default
         ("skytime") matches what the embedded transient worker
         registers under, no override needed. Otherwise pass it
         explicitly. Plan 04-05 SUMMARY: "Worker default task queue
         applies when the flow does not declare one (D3-19 hierarchy)"
         and run.go line 128 sets `TaskQueue: "skytime"` on the
         StartWorkflowOptions.
       - The embedded transient worker (Phase 4 plan 04-05's "skytime
         run --rootdir convenience" mode) auto-boots from
         filepath.Dir(file). For the happy test, that's
         examples/skeleton/, which is exactly what we want — the worker
         picks up both .star files but executes only the named flow.
         For the unhappy test, the temp .star lives alone in t.TempDir(),
         so the worker boots a registry containing only that flow.

    5. Run the full test set including the new e2e:
       `go test ./tests/... ./... -race -count=1 -timeout 5m`
       The e2e tests skip if temporal CLI / network are unavailable;
       on a developer machine with both they run end-to-end. On Windows
       the file is excluded by the build tag and the package contains
       no tests (acceptable — Windows is not a supported dev platform
       for this project).

    6. UPDATE PROJECT.md "Active" section: append a bullet to "Validated"
       documenting Fix A/B/C/D landed:
       - "✓ HTTP non-2xx auto-fail — `pkg/extension/builtin/http` returns
         classified errors for non-2xx responses (4xx → NonRetryable via
         `extension.ErrNonRetryable` sentinel; 5xx → retryable plain
         error); interpreter walkStep extracts `status=N` summary on
         success and surfaces NonRetryableErrResult as workflow failure;
         CLI renderer prints `[skytime] flow failed step I/M (reason)`
         with red marker on err_count > 0; example corpus uses
         /repos/octocat/Hello-World — Phase 4 (quick 260502-onc)"

       Edit only the bullet list — DO NOT touch other PROJECT.md sections.
  </action>
  <verify>
    <automated>cd /Users/mikel/dev/ai/temporero &amp;&amp; go test ./tests/... -count=1 -timeout 5m</automated>
  </verify>
  <done>
    - examples/skeleton/simple_check.star uses /repos/octocat/Hello-World; docstring notes v1 illustrative-input limitation.
    - examples/skeleton/parallel_fanout.star uses three /repos/octocat/Hello-World[/...] paths.
    - tests/differential_test.go::TestDifferentialCorpus PASSES against the updated corpus (no behavior change since AlwaysOkDispatch).
    - tests/e2e_skytime_run_test.go exists with `//go:build !windows` build tag, TestE2E_SkytimeRun_Happy + TestE2E_SkytimeRun_Unhappy.
    - On a machine with `temporal` in PATH AND network, both e2e tests PASS, and the dev-server subprocess group is reliably reaped on package exit (verified by `pgrep temporal` returning empty after `go test` completes).
    - On a machine without `temporal` in PATH, the e2e package short-circuits via TestMain; no failures.
    - On a machine without network, individual tests t.Skip() via requireNetwork.
    - On Windows, the file is excluded by the build tag; the package contains no tests; `go test` exits 0.
    - PROJECT.md "Validated" section has a new bullet documenting the fix landed.
    - Final gate: `go test ./... -race -count=1 -timeout 5m` exits 0 (with skips for missing temporal/network being acceptable).
  </done>
</task>

</tasks>

<verification>
Phase-level verification across the four-task plan:

1. UNIT TESTS: every package's tests pass with race detector.
   `go test ./... -race -count=1 -timeout 5m`
   Acceptable skips: tests/e2e_skytime_run_test.go skips if temporal
   CLI not in PATH or network unavailable; TestWorkflowcheck_NoFindings
   skips if workflowcheck not installed.

2. FIREWALL TESTS pass — no new violations:
   - pkg/activity/firewall_test.go::TestNoTemporalImportsOutsideAllowList
   - pkg/interpreter/firewall_test.go::TestPkgInterpreter_ImportsTemporal
   - pkg/cli/firewall (cobra/charm-log) — unchanged, no new imports
   - tests/firewall_cli_test.go (if present) — unchanged

3. STATIC + DRY-RUN AGREE on updated corpus:
   `go test ./tests/... -run TestDifferentialCorpus -count=1 -v`

4. END-TO-END HAPPY-PATH (manual or automated via Task 4 e2e):
   - skytime run on simple_check.star prints status=200, flow complete, exits 0.
   - status=200 appears BEFORE flow complete in the stream (M-5 ordering guard).

5. END-TO-END UNHAPPY-PATH (manual or automated via Task 4 e2e):
   - skytime run on a 404-pointing flow prints ✗, HTTP 404, flow failed; exits non-zero.
   - Exit error is *exec.ExitError (NOT context.DeadlineExceeded) per M-4 guard.

6. SUBPROCESS HYGIENE (M-2): after `go test ./tests/...` completes,
   `pgrep -f "temporal server start-dev"` returns empty. No orphaned
   children from the dev-server group. Cleanup fires on normal exit,
   panic, AND Ctrl-C (verified by manually sending SIGINT during a long
   test run).

7. NO REGRESSION on Phase 4 baked-in patterns:
   - HTTPResponse Status field name unchanged (Fix B reflects on it).
   - Idempotence map (D4-14) unchanged.
   - applyCredential nil-bypass (quick 260502-guu Fix A) unchanged.
   - errSilent + render.go renderError pipeline unchanged.

8. GO BUILD CLEAN:
   `go build ./... && go vet ./...`
</verification>

<success_criteria>
1. **Fix A**: HTTP non-2xx classified correctly.
   - errors.Is(doHTTP-err, extension.ErrNonRetryable) is TRUE for 4xx, FALSE for 5xx.
   - 2xx returns (HTTPResponse, nil) unchanged — TestExtension_GetSucceedsAgainstHTTPTestServer + TestExtension_PostSendsBody still pass.
   - Activity isRetryable correctly wires the sentinel into the existing dispatch.
   - pkg/extension import added to pkg/activity/execute_batch.go (was NOT previously imported there — verified via grep before commit).

2. **Fix B**: walkStep summary informative on success, failure surfaces.
   - extractStatusSummary returns "status=N" for HTTPResponse-shaped Output, "N ok" for multi-action, "" otherwise.
   - extractFirstNonRetryable correctly returns the first NonRetryableErrResult's Err (covers mid-block first-wins property — see Task 2 BEHAVIOR M-3 note).
   - walkStep returns the error so workflow fails, defer fires step_complete with status="err".
   - pkg/interpreter does NOT import pkg/extension/builtin/http (firewall preserved).

3. **Fix C**: renderer correctly differentiates success vs failure on flow_complete.
   - renderFlowComplete branches on err_count.
   - lastErr captures most recent step_complete-with-err and resets on flow_start.
   - WithAttrs / WithGroup propagate lastErr correctly.

4. **Fix D**: corpus + e2e green.
   - Static + dry-run still agree on updated .star files.
   - On a workstation with `temporal` + network: TestE2E_SkytimeRun_Happy + Unhappy both PASS, demonstrating the full stack.
   - Subprocess group teardown leaves no orphaned `temporal server start-dev` children (M-2).
   - Happy path's status=200/flow-complete ordering enforced (M-5).
   - Unhappy path's exit error is *exec.ExitError, not context cancellation (M-4).
   - File excluded on Windows via `//go:build !windows` build tag (M-2 platform notes).

5. **Test gates**:
   - `go test ./... -race -count=1 -timeout 5m` exits 0 (skips OK).
   - `go vet ./...` clean.
   - `go build ./...` clean.
</success_criteria>

<output>
After completion, create `.planning/quick/260502-onc-auto-fail-http-non-2xx-status-in-summary/260502-onc-SUMMARY.md`
documenting:
  - Files changed (with line counts)
  - Test counts (added vs pre-existing)
  - Firewall verification (interpreter does NOT import builtin/http; pkg/extension/builtin/http does NOT import temporal)
  - Behavior shift summary: silent-success-on-404 → fail-loudly-NonRetryable
  - Decision references for future quick tasks (the ErrNonRetryable sentinel pattern as the established way for future extensions to surface non-retryable failures)
  - Subprocess hygiene pattern (Setpgid + group-kill teardown) as a reusable e2e pattern for any future test that spawns long-running CLIs
  - Any deviations from this PLAN.md and why
</output>
