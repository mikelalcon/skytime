---
phase: 260502-guu-bypass-empty-credentialid-resolve-bazel-
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - pkg/activity/action_executor.go
  - pkg/activity/execute_batch_test.go
  - pkg/extension/builtin/http/http_test.go
  - pkg/cli/progress.go
  - pkg/cli/progress_test.go
  - pkg/cli/options.go
  - pkg/cli/flags.go
  - pkg/cli/root.go
  - pkg/cli/render.go
  - pkg/cli/run.go
  - pkg/cli/run_test.go
  - pkg/interpreter/workflow.go
  - pkg/interpreter/walk_step.go
  - pkg/interpreter/walk_script.go
  - pkg/interpreter/walk_ifcond.go
  - pkg/interpreter/walk_foreach.go
  - pkg/interpreter/walk_callflow.go
  - pkg/worker/options.go
  - pkg/worker/worker.go
  - .planning/PROJECT.md
autonomous: true
requirements:
  - QUICK-260502-guu-FixA  # Empty-CredentialID bypass in pkg/activity per-action loop
  - QUICK-260502-guu-FixB  # Bazel-style colored CLI output + --verbose flag wiring SDK Logger

must_haves:
  truths:
    - "Running ./skytime run with a flow whose actions omit credential= no longer hits the credential resolver — no resolve attempt, no retry storm, total runtime under 10 s for the locked simple_check repro"
    - "Default ./skytime run output shows Bazel-style step lines with [skytime] banner, [N/M] step counter, kind labels, ✓/✗ status markers, and ms-resolved durations — NOT raw stdlib log lines"
    - "./skytime run --verbose shows Temporal SDK INFO/DEBUG messages routed through charm-log (colorized) alongside the Bazel step lines"
    - "pkg/extension/builtin/http operations accept a nil Credential argument without dereferencing — proven by an explicit test"
    - "All existing unit + integration + firewall tests still pass after the changes (no regression in the activity, interpreter, worker, cli, or extension test suites)"
  artifacts:
    - path: "pkg/activity/action_executor.go"
      provides: "Per-action runAction loop short-circuits resolve when ref.CredentialID == \"\" — operation receives nil Credential"
      contains: "if ref.CredentialID == \"\""
    - path: "pkg/activity/execute_batch_test.go"
      provides: "TestExecuteBatch_BypassesResolverWhenCredentialIDEmpty — handler.Resolve call count stays at zero across the batch"
      contains: "TestExecuteBatch_BypassesResolverWhenCredentialIDEmpty"
    - path: "pkg/extension/builtin/http/http_test.go"
      provides: "TestExtension_GetAcceptsNilCredential (and POST companion) — explicit nil-credential dispatch coverage"
      contains: "NilCredential"
    - path: "pkg/cli/progress.go"
      provides: "Bazel-style renderer consuming structured slog events (flow_start | step_dispatch | step_complete | branch | flow_complete) with charm-log color profile + nested step paths"
      contains: "renderBazelLine"
    - path: "pkg/cli/progress_test.go"
      provides: "TestProgress_BazelFormat — table-driven coverage for every event type + nested numbering + ✓/✗ + duration"
      contains: "TestProgress_BazelFormat"
    - path: "pkg/cli/options.go"
      provides: "config.Verbose runtime field"
      contains: "Verbose"
    - path: "pkg/cli/flags.go"
      provides: "--verbose persistent flag registration"
      contains: "verbose"
    - path: "pkg/cli/run.go"
      provides: "SDK client + worker constructed with go.temporal.io/sdk/log.NewStructuredLogger wrapping a slog.Logger; default routes everything to a level-Off handler so SDK noise stays hidden; --verbose routes to the charm-log handler"
      contains: "log.NewStructuredLogger"
    - path: "pkg/cli/run_test.go"
      provides: "TestRun_VerboseFlagWiresSDKLogger — verifies the flag changes the Logger handed to the worker/client"
      contains: "TestRun_VerboseFlagWiresSDKLogger"
    - path: "pkg/interpreter/workflow.go"
      provides: "Top-level walker emits flow_start at entry and flow_complete at exit through workflow.GetLogger(ctx)"
      contains: "flow_start"
    - path: "pkg/interpreter/walk_step.go"
      provides: "Emits step_dispatch + step_complete with kind=\"step\" and the ActionRef summary"
      contains: "step_dispatch"
    - path: "pkg/interpreter/walk_script.go"
      provides: "Emits step_dispatch + step_complete with kind=\"script\""
      contains: "kind"
    - path: "pkg/interpreter/walk_ifcond.go"
      provides: "Emits step_dispatch + branch + step_complete with kind=\"if_cond\""
      contains: "branch"
    - path: "pkg/interpreter/walk_foreach.go"
      provides: "Emits step_dispatch + per-item events + step_complete with kind=\"for_each_parallel\""
      contains: "for_each_parallel"
    - path: "pkg/interpreter/walk_callflow.go"
      provides: "Emits step_dispatch + step_complete with kind=\"call_flow\""
      contains: "call_flow"
    - path: "pkg/worker/options.go"
      provides: "WorkerOptions.Logger *slog.Logger field — optional; nil falls back to SDK default"
      contains: "Logger"
    - path: "pkg/worker/worker.go"
      provides: "WorkerOptions.Logger threaded into sdkworker.Options.Logger via go.temporal.io/sdk/log.NewStructuredLogger when non-nil"
      contains: "log.NewStructuredLogger"
    - path: ".planning/PROJECT.md"
      provides: "Two new validated capability lines under Phase 4 documenting credential-bypass + Bazel-style output"
      contains: "credential-bypass"
  key_links:
    - from: "pkg/activity/action_executor.go runAction"
      to: "pkg/activity/credential_cache.go resolve"
      via: "if ref.CredentialID == \"\" → skip cache.resolve and pass nil to spec.Func"
      pattern: "if ref.CredentialID == \"\""
    - from: "pkg/cli/run.go newRunCommand RunE"
      to: "go.temporal.io/sdk/log.NewStructuredLogger"
      via: "Build a *slog.Logger from cfg.Verbose (charm-log handler) or io.Discard (level=ErrorLevel+1) and wrap it for client.Options.Logger + worker.Options.Logger"
      pattern: "log\\.NewStructuredLogger"
    - from: "pkg/interpreter/walk_step.go walkStep"
      to: "pkg/cli/progress.go renderBazelLine"
      via: "workflow.GetLogger(ctx).Info(\"step_dispatch\", \"kind\", \"step\", \"idx\", n, \"total\", t, \"label\", actionLabel, \"path\", stepPath) → routed via slog.Default through progressHandler"
      pattern: "step_dispatch"
    - from: "pkg/cli/flags.go registerPersistentFlags"
      to: "pkg/cli/options.go config.Verbose"
      via: "BoolVar(&cfg.Verbose, \"verbose\", false, ...) → connected through cobra PersistentFlags before PersistentPreRunE runs"
      pattern: "verbose"
---

<objective>
Two surgical fixes to make `./skytime run` usable end-to-end against the simple_check fixture:

**Fix A — Empty-CredentialID bypass.** When `dag.ActionRef.CredentialID == ""`, `pkg/activity` MUST NOT invoke `credentialCache.resolve` and MUST pass `nil` to `OperationSpec.Func`. Today the per-action loop unconditionally calls `cache.resolve(ctx, ref.CredentialID, bypass)` — when the ID is empty the noop handler returns `ErrUnknownCredential`, the activity classifies that as retryable (Phase 2 D2-12 only marks `errors.Is(err, ErrUnknownCredential)` as non-retryable on populated IDs — defense in depth means we still want to skip the call entirely for empty IDs because Phase 2 chose to make `noopCredentialHandler` non-permissive), Temporal retries the WHOLE batch every ~5 s, and the user sees a 30 s spam of error lines before timeout. Audit item logged in STATE.md ("noopCredentialHandler-on-empty-id retry loop is pre-existing and out-of-scope" from quick 260501-p7c) — closing it now.

**Fix B — Bazel-style colored CLI output (locked design).** Default `skytime run` output today is raw stdlib log lines from the Temporal SDK because `client.Options.Logger` and `worker.Options.Logger` aren't set. The locked design (see prompt's bug_context for the exact format and color guide) is a Bazel-style step list with `[skytime]` banner, `[N/M]` counters, kind labels, `✓`/`✗` markers, and ms durations. New `--verbose` persistent flag toggles between SILENT (default — SDK INFO/DEBUG hidden) and FULL (SDK lines visible via charm-log handler).

Purpose: One TDD-strict plan with three tasks (A, B-core, B-wiring + e2e + PROJECT.md). Quality > speed per project constraint.
Output: User can run `./skytime run examples/skeleton/simple_check.star --flow=simple_check --input='{"repo_path":"x"}'` and see clean Bazel output in under 10 s, with the option to raise `--verbose` for SDK detail.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/PROJECT.md
@.planning/STATE.md
@./CLAUDE.md

# Direct sources the executor will modify or read
@pkg/activity/execute_batch.go
@pkg/activity/action_executor.go
@pkg/activity/execute_batch_test.go
@pkg/activity/credential_cache.go
@pkg/extension/credential.go
@pkg/extension/handler.go
@pkg/extension/builtin/http/http.go
@pkg/extension/builtin/http/http_test.go
@pkg/dag/action.go
@cmd/skytime/main.go
@pkg/cli/run.go
@pkg/cli/run_test.go
@pkg/cli/progress.go
@pkg/cli/progress_test.go
@pkg/cli/options.go
@pkg/cli/flags.go
@pkg/cli/root.go
@pkg/cli/render.go
@pkg/cli/connect.go
@pkg/cli/dev_server.go
@pkg/interpreter/workflow.go
@pkg/interpreter/walk_step.go
@pkg/interpreter/walk_script.go
@pkg/interpreter/walk_ifcond.go
@pkg/interpreter/walk_foreach.go
@pkg/interpreter/walk_callflow.go
@pkg/interpreter/lambda_eval_test.go
@pkg/worker/options.go
@pkg/worker/worker.go
@pkg/worker/client.go
@tests/firewall_cli_test.go

<interfaces>
<!-- Key contracts the executor needs. Use these directly — no codebase exploration -->

From pkg/dag/action.go:
```go
type ActionRef struct {
    Pos          syntax.Position
    Kind_        string
    Kwargs       *starlark.Dict
    CredentialID string  // empty string means "no credential" — the Fix A signal
    // ...
}
```

From pkg/extension/handler.go:
```go
type CredentialHandler interface {
    Resolve(ctx context.Context, id string) (Credential, error)
}
var ErrUnknownCredential = errors.New("unknown credential")
```

From pkg/activity/action_executor.go (CURRENT, before Fix A):
```go
func (a *Activity) runAction(ctx context.Context, idx int, ref *dag.ActionRef) (dag.OperationOutput, error) {
    spec, ok := a.dispatch[ref.Kind_]
    if !ok { /* unknown op error */ }
    bypass := a.attemptFn(ctx) > 1
    cred, err := a.cache.resolve(ctx, ref.CredentialID, bypass)  // ALWAYS called — bug
    if err != nil { return nil, classifyResolveError(err) }
    // ... timeout + decode + spec.Func(callCtx, args, cred)
}
```

Operation function contract (extension/operation.go via http.go):
```go
type OperationFunc func(ctx context.Context, args any, cred extension.Credential) (dag.OperationOutput, error)
// nil cred is legal — applyCredential in http.go already handles `if cred == nil { return }`
```

Worker option seam (pkg/worker/options.go) — Fix B adds:
```go
type WorkerOptions struct {
    // ... existing fields ...
    Logger *slog.Logger   // NEW: nil = SDK default; non-nil = wrapped via go.temporal.io/sdk/log.NewStructuredLogger
}
```

Worker SDK Logger wiring (pkg/worker/worker.go) — Fix B adds inside NewWorker:
```go
import sdklog "go.temporal.io/sdk/log"
sdkOpts := sdkworker.Options{ /* existing fields */ }
if opts.Logger != nil {
    sdkOpts.Logger = sdklog.NewStructuredLogger(opts.Logger)
}
```

Cli config Verbose (pkg/cli/options.go) — Fix B adds:
```go
type config struct {
    // ... existing ...
    Verbose bool   // NEW: persistent flag value
}
```

CLI run wiring (pkg/cli/run.go) — Fix B Task 3 adds:
```go
import sdklog "go.temporal.io/sdk/log"
// Build the *slog.Logger for SDK consumption:
//   - Verbose=false → io.Discard text handler at slog.LevelError+1 (drops everything)
//   - Verbose=true  → re-use cfg.logger (charm-log already on stderr)
sdkSlog := buildSDKSlogLogger(cfg)  // helper in run.go
sdkLogger := sdklog.NewStructuredLogger(sdkSlog)

// Pass to the worker:
w, err := worker.NewWorker(c, worker.WorkerOptions{
    // existing fields ...
    Logger: sdkSlog,  // worker wraps it via NewStructuredLogger inside NewWorker
})

// Pass to the client: pkg/cli is firewall-allowed to import SDK directly,
// but client construction lives in pkg/worker (NewCloudClient/NewSelfHostedClient/NewDevClient).
// Two-option choice (executor picks):
//   (a) Add Logger field to pkg/worker {Cloud,SelfHosted,DevClient}Options + thread into client.Options.Logger.
//   (b) Set the workflow logger via worker.Options only and accept that client-side gRPC connect logs still surface.
// PREFERENCE: option (a) for symmetry — extend the three option structs.
```

Bazel renderer event schema (slog records produced by interpreter, consumed by progressHandler):
```
event: "flow_start"      attrs: flow_name (string), step_count (int)
event: "step_dispatch"   attrs: idx (int), total (int), kind (string), label (string), path (string)
event: "step_complete"   attrs: idx (int), total (int), kind (string), path (string), status ("ok"|"err"), duration_ms (int64), summary (string, optional)
event: "branch"          attrs: idx (int), path (string), branch ("then"|"else")
event: "flow_complete"   attrs: ok_count (int), err_count (int), total_ms (int64)
```

Identification convention: progressHandler routes records carrying the `event` attribute (NEW; replaces today's `flow_name`-attribute filter from W5). Records without `event` flow through to the wrapped charm-log handler (so SDK lines and `--verbose` lines still go through charm-log).

Step path scheme (pick simple convention):
- Top-level steps: "1", "2", "3", ...
- Inside if_cond.then: "1a", "1a", ... (parent index + branch letter, then sub-index)
- Inside for_each_parallel iteration N, sub-step M: "3.N.M"
Implementation: thread a `stepPath string` field on the interpreter struct (OR pass through walkBody as a parameter — executor picks). Reset to top-level on the outer walkBody, append on entry to nested walkers.

Bazel renderer color guide (charm-log color profile + ANSI escapes via lipgloss-style helpers OR manual escape sequences if lipgloss adds firewall noise — both are acceptable, lipgloss is already an indirect transitive dep):
- `[skytime]` banner: dim cyan
- `[N/M]` counter: bright cyan
- kind label (right-padded for column alignment): bright white
- ✓ + duration: green
- ✗ + error: red
- → arrow: yellow
- nested rows indented 2 spaces

If TTY-detection (golang.org/x/term.IsTerminal on stderr fd, already used in render.go) reports non-tty, drop colors — emit plain ASCII.

For the existing TestSlogProgress_RendersStepEvents and TestSlogProgress_PassthroughRespectsLevel tests in progress_test.go: those test the OLD `flow_name`-based filter. They MUST be deleted or rewritten to use the NEW `event`-attribute filter — same behavior contract (Skytime records render, others passthrough), but the filter key changes.
</interfaces>

</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Fix A — bypass resolver when CredentialID is empty (RED → GREEN)</name>
  <files>
    - pkg/activity/execute_batch_test.go
    - pkg/activity/action_executor.go
    - pkg/extension/builtin/http/http_test.go
  </files>
  <behavior>
    - TestExecuteBatch_BypassesResolverWhenCredentialIDEmpty (NEW, in execute_batch_test.go):
      Build an Activity with `counterHandler` wrapping a FakeCredentialHandler. Build a 3-action batch where every ActionRef has `CredentialID: ""`. The op function asserts `cred == nil` (`require.Nil(t, cred)` inside the op closure).
      Call env.ExecuteActivity(impl.ExecuteBatch, batch). After completion: results length == 3, every result is OkResult, and `ch.calls.Load() == 0` — the resolver MUST NOT have been called at all.
    - TestExecuteBatch_BypassesResolverPerAction_MixedIDs (NEW, in execute_batch_test.go):
      Mixed batch: ref0 has CredentialID="admin", ref1 has CredentialID="", ref2 has CredentialID="admin".
      Expectation: ch.calls.Load() reports exactly 1 (only the admin credential resolved once via cache hit on ref2 — not 0, not 2; this proves the bypass is per-action, not all-or-nothing).
      The op closure for ref1 asserts `cred == nil`; the closure for ref0 + ref2 asserts `cred != nil` (`require.NotNil`).
    - TestExtension_GetAcceptsNilCredential (NEW, in http_test.go):
      Construct *GetArgs against an httptest.NewServer; call spec.Func(ctx, args, nil) — must not panic; must succeed; the served request must have NO Authorization header (`require.Empty(t, r.Header.Get("Authorization"))`).
    - TestExtension_PostAcceptsNilCredential (NEW, in http_test.go):
      Same shape as above for the post Func with BodyArgs — confirms the BodyArgs branch does not introduce a nil-deref of cred.
  </behavior>
  <action>
    1. Open pkg/activity/execute_batch_test.go. Append the two new tests at the bottom of the file. Reuse the existing newIntegrationActivity / mkRef helpers; the `cred == nil` assertion goes inside the op closure passed via OperationSpec.Func.

       Important: the existing `mkRef(kind, credID)` helper already accepts an empty credID string — `mkRef("fake.echo", "")` is the construction pattern.

    2. Run the new tests — they MUST FAIL initially. Expected failures:
       - TestExecuteBatch_BypassesResolverWhenCredentialIDEmpty: either (a) op closure receives non-nil cred (because resolver is invoked → cache returns whatever the FakeHandler stores), or (b) more likely the FakeCredentialHandler returns ErrUnknownCredential for the unknown empty-string ID and the activity surfaces that as an error → ExecuteActivity returns err (non-nil). Either failure mode confirms the resolver is being touched.
       - TestExecuteBatch_BypassesResolverPerAction_MixedIDs: ch.calls reports != 1.

       Commit RED (intentional failing tests):
       ```
       git add pkg/activity/execute_batch_test.go pkg/extension/builtin/http/http_test.go
       node "$HOME/.claude/get-shit-done/bin/gsd-tools.cjs" commit "test(260502-guu): RED — Fix A empty-CredentialID bypass + http nil-credential coverage"
       ```

    3. Open pkg/activity/action_executor.go. Locate the current resolve call inside runAction:
       ```go
       bypass := a.attemptFn(ctx) > 1
       cred, err := a.cache.resolve(ctx, ref.CredentialID, bypass)
       if err != nil {
           return nil, classifyResolveError(err)
       }
       ```
       Wrap it with a CredentialID check:
       ```go
       var cred extension.Credential // explicit zero-value nil
       if ref.CredentialID != "" {
           bypass := a.attemptFn(ctx) > 1
           c, err := a.cache.resolve(ctx, ref.CredentialID, bypass)
           if err != nil {
               return nil, classifyResolveError(err)
           }
           cred = c
       }
       // ... continue to per-action timeout + kwargs decode + spec.Func(callCtx, args, cred)
       ```

       NOTE: The resolve-bypass for retry attempts (Attempt > 1) in execute_batch.go already filters `if ref.CredentialID != ""` before invalidating cache entries — that loop is correct and stays unchanged. The fix is only at the per-action resolve site in runAction.

       NOTE: The `extension` import is already in scope in action_executor.go (used for OperationSpec, Credential type, DecodeKwargsFromDict). No new imports needed.

       DON'T change the OperationFunc signature.
       DON'T introduce a NoCredential type — nil is canonical.
       DON'T make noopCredentialHandler more permissive.

    4. Open pkg/extension/builtin/http/http_test.go. Append the two new nil-credential tests. The existing TestExtension_GetSucceedsAgainstHTTPTestServer already passes nil — but it does not ASSERT that no Authorization header was sent. The new tests are explicit guards; place them adjacent to the existing tests.

    5. Re-run all of: `go test ./pkg/activity/... ./pkg/extension/... -race -count=1`. All tests (new + existing) MUST pass. The pre-existing 4-action retry-cache tests still rely on populated CredentialIDs, so they remain green.

    6. Commit GREEN:
       ```
       git add pkg/activity/action_executor.go
       node "$HOME/.claude/get-shit-done/bin/gsd-tools.cjs" commit "fix(260502-guu): bypass credential resolve when ActionRef.CredentialID is empty (Fix A)"
       ```
  </action>
  <verify>
    <automated>cd /Users/mikel/dev/ai/temporero && go test ./pkg/activity/... ./pkg/extension/... -race -count=1 -run 'TestExecuteBatch_BypassesResolverWhenCredentialIDEmpty|TestExecuteBatch_BypassesResolverPerAction_MixedIDs|TestExtension_.*NilCredential|TestExecuteBatch_HappyPath_SingleAction|TestExecuteBatch_HandlerInvokedJIT|TestExecuteBatch_RetryAttempt_BypassesCache|TestExecuteBatch_ACT05_SecretNeverLeaks'</automated>
  </verify>
  <done>
    - Two new BypassesResolver tests + two new NilCredential http tests pass.
    - All pre-existing pkg/activity + pkg/extension/builtin/http tests still pass (no regression).
    - pkg/activity/action_executor.go runAction has the `if ref.CredentialID != ""` guard.
    - Two commits exist: RED test commit + GREEN fix commit.
  </done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Fix B Core — Bazel renderer + interpreter slog instrumentation (RED → GREEN)</name>
  <files>
    - pkg/cli/progress.go
    - pkg/cli/progress_test.go
    - pkg/interpreter/workflow.go
    - pkg/interpreter/walk_step.go
    - pkg/interpreter/walk_script.go
    - pkg/interpreter/walk_ifcond.go
    - pkg/interpreter/walk_foreach.go
    - pkg/interpreter/walk_callflow.go
  </files>
  <behavior>
    - TestProgress_BazelFormat (NEW, table-driven, in progress_test.go) covers every event type:
      * flow_start: emits `[skytime] flow simple_check  3 steps  starting`
      * step_dispatch (kind=step, label="gh.get(/repos/example/repo)"): emits `[1/3] step                gh.get(/repos/example/repo)`
      * step_complete (status=ok, duration_ms=234, summary="status=200"): emits `     ✓ 234ms  status=200`
      * step_complete (status=err, duration_ms=120, summary="connection refused"): emits `     ✗ 120ms  connection refused`
      * step_dispatch with kind=if_cond + branch event (branch=then): emits `[3/3] if_cond             ctx.health  → then`
      * step_dispatch with nested path "3a": emits `[3a/3a] step              gh.get(...)` indented 2 spaces
      * flow_complete: emits `[skytime] flow complete  3/3 steps  total 433ms`

      Each row asserts via `require.Contains` on the rendered output (no exact whitespace match — column padding can drift). Color codes are NOT asserted (TTY detection means tests run in non-tty mode → ASCII fallback; the test asserts plain text only).

    - TestProgress_PassthroughOnNonSkytimeRecord (NEW): A logger.Info("plain SDK log message") with NO `event` attribute MUST flow through to the wrapped charm-log handler buffer, NOT to the progress writer.

    - TestProgress_NestedStepPath (NEW, focused): a step_dispatch with path="3a" emits `[3a/3a]` and the row is indented 2 spaces from column 0.

    - The two existing TestSlogProgress_RendersStepEvents and TestSlogProgress_PassthroughRespectsLevel tests assert the OLD `flow_name`-based filter. They MUST be REPLACED by the new tests above (the old filter is gone). DELETE the two old tests when adding the new ones. The behavior contract is the same — Skytime records render, non-Skytime records passthrough — but the discriminator field changes from `flow_name` to `event`.
  </behavior>
  <action>
    1. RED: Open pkg/cli/progress_test.go. DELETE TestSlogProgress_RendersStepEvents and TestSlogProgress_PassthroughRespectsLevel. ADD TestProgress_BazelFormat (table-driven), TestProgress_PassthroughOnNonSkytimeRecord, TestProgress_NestedStepPath. The tests construct a *progressHandler with a bytes.Buffer for both progress output and passthrough output, fire slog.Info calls with the documented event attrs, and assert on the buffer contents.

       Run `go test ./pkg/cli/... -run TestProgress -count=1` — tests MUST FAIL (renderer not yet rewritten).

       Commit RED:
       ```
       git add pkg/cli/progress_test.go
       node "$HOME/.claude/get-shit-done/bin/gsd-tools.cjs" commit "test(260502-guu): RED — Bazel renderer event schema + nested paths"
       ```

    2. GREEN renderer: Rewrite pkg/cli/progress.go.

       Keep the progressHandler shape (slog.Handler with wrapped + out). Change the Handle method:
       ```go
       func (p *progressHandler) Handle(ctx context.Context, r slog.Record) error {
           if !hasAttr(r, "event") {
               return p.wrapped.Handle(ctx, r)  // passthrough to charm-log
           }
           return p.renderBazelLine(r)
       }
       ```

       renderBazelLine inspects the `event` attr value and dispatches to one of:
       - renderFlowStart(r)   → reads flow_name, step_count attrs
       - renderStepDispatch(r) → reads idx, total, kind, label, path attrs
       - renderStepComplete(r) → reads idx, total, kind, path, status, duration_ms, summary attrs
       - renderBranch(r)       → reads idx, path, branch attrs
       - renderFlowComplete(r) → reads ok_count, err_count, total_ms attrs

       Layout (use fmt.Fprintf to p.out — no lipgloss dependency added; ANSI sequences inline only when TTY):
       ```
       [skytime] flow {flow_name}  {step_count} steps  starting
       [{idx}/{total}] {kind:<padded-to-19}  {label}
            ✓ {duration_ms}ms  {summary}
       [{idx}/{total}] if_cond             {label}  → {branch}
       [skytime] flow complete  {ok_count}/{ok_count+err_count} steps  total {total_ms}ms
       ```

       For nested rows (path contains a dot or letter like "3a"): indent the entire row by 2 spaces and use `[{path}/{path}]` instead of `[{idx}/{total}]`. Detection: if path != "" and path != fmt.Sprintf("%d", idx), render in nested form.

       TTY detection: re-use the golang.org/x/term.IsTerminal check pattern from render.go. When NOT a TTY, emit plain ASCII (no escape codes). Tests run in non-tty mode so they assert against plain text.

       Color (only when TTY):
       - banner [skytime]: ANSI \x1b[2;36m (dim cyan) + reset
       - counter [N/M]: \x1b[1;36m (bright cyan)
       - kind label: \x1b[1;37m (bright white)
       - ✓: \x1b[32m (green)
       - ✗: \x1b[31m (red)
       - →: \x1b[33m (yellow)

       Helper:
       ```go
       func (p *progressHandler) tty() bool { /* memoized term.IsTerminal check on the underlying *os.File if p.out is one; otherwise false */ }
       ```

       Run `go test ./pkg/cli/... -run TestProgress -count=1` — MUST pass.

       Commit GREEN renderer:
       ```
       git add pkg/cli/progress.go
       node "$HOME/.claude/get-shit-done/bin/gsd-tools.cjs" commit "feat(260502-guu): Bazel-style slog renderer for skytime progress events"
       ```

    3. GREEN interpreter instrumentation: Add slog calls via workflow.GetLogger(ctx) at the documented schema points. The interpreter ALREADY uses workflow.GetLogger in workflow.go (`logger.Info("skytime workflow start", ...)`); reuse the same logger access — i.logger is already cached on the interpreter struct.

       3a. Add a stepPath field to the interpreter struct (pkg/interpreter/workflow.go):
       ```go
       type interpreter struct {
           // ... existing fields ...
           stepPath string  // current nesting prefix; "" for top-level
           stepIdx  int     // 1-indexed counter for siblings at this nesting level
           stepTot  int     // total siblings at this nesting level
       }
       ```
       walkBody resets/sets stepIdx and stepTot at entry:
       ```go
       func (i *interpreter) walkBody(ctx workflow.Context, body []dag.Node) error {
           savedIdx, savedTot := i.stepIdx, i.stepTot
           i.stepTot = len(body)
           defer func() { i.stepIdx, i.stepTot = savedIdx, savedTot }()
           for k, node := range body {
               i.stepIdx = k + 1
               // ... walkNode(ctx, node)
           }
       }
       ```

       3b. workflow.go (NewWorkflow returned closure): emit flow_start at the very start (right after the existing logger.Info), then defer flow_complete with elapsed measurement:
       ```go
       startTime := workflow.Now(ctx)  // deterministic time per Temporal docs
       logger.Info("skytime",
           "event", "flow_start",
           "flow_name", input.FlowName,
           "step_count", len(parsed.Flow.Body),
       )
       // ... existing parsed lookup + interpreter construction + walkBody ...
       endTime := workflow.Now(ctx)
       logger.Info("skytime",
           "event", "flow_complete",
           "ok_count", okCount,        // count walkers that returned nil
           "err_count", errCount,
           "total_ms", endTime.Sub(startTime).Milliseconds(),
       )
       ```
       Track ok_count / err_count via the err return of walkBody — for v1 simplicity, ok_count = len(parsed.Flow.Body) on success, err_count = 0; on error, ok_count = 0, err_count = 1 (the failing step). Document this v1 simplification in a comment.

       3c. walk_step.go: emit step_dispatch + step_complete around the ExecuteActivity call. Build a label from `step.Actions` — for a single-action step the label is `step.Actions[0].Kind_ + "(" + truncated path/url + ")"`; for multi-action use `len(step.Actions) + " actions"`. Capture start time via `workflow.Now(ctx)`; on return, log step_complete with status="ok"|"err" and duration_ms. summary attr: HTTP status if the result is an OkResult containing an HTTPResponse-shaped output (best-effort; empty string when not parseable).

       3d. walk_script.go: emit step_dispatch (kind="script", label=n.OutputAlias) before evalLambda; step_complete after.

       3e. walk_ifcond.go: emit step_dispatch (kind="if_cond", label="cond"); after evaluating cond, emit a branch event with branch="then" or "else"; emit step_complete after the branch walkBody returns.

       3f. walk_foreach.go: emit step_dispatch (kind="for_each_parallel", label=fmt.Sprintf("items=%d", n)) before the fan-out loop; emit step_complete after the barrier. Per-item events are OPTIONAL for v1 (the goroutine count + barrier already logs at the SDK level); skip per-item logging to keep history-event count down.

       3g. walk_callflow.go: emit step_dispatch (kind="call_flow", label=cf.Name) before ExecuteChildWorkflow; step_complete after.

       For each emit, attach `path` = i.stepPath + (top-level: fmt.Sprintf("%d", i.stepIdx); nested: see 3h), `idx` = i.stepIdx, `total` = i.stepTot.

       3h. Nested path convention: in walkIfCond, when entering the then/else branch, save i.stepPath, set i.stepPath = fmt.Sprintf("%d%s", parentIdx, "a") (then) or "b" (else), recurse, restore. In walkForEach, set i.stepPath = fmt.Sprintf("%d.%d", parentIdx, itemIdx) inside the per-item walkBody. In walkCallFlow, do NOT recurse — child flow is its own workflow with its own path.

    4. Verify interpreter tests still pass: `go test ./pkg/interpreter/... -race -count=1`.
       The TestEvalLambda_PrintRoutesToWorkflowLogger test in lambda_eval_test.go uses log.NewStructuredLogger to capture workflow logs into a buffer; it WILL see the new event records, so adjust assertions if necessary (it currently checks for the `[skytime/print]` message — that should still appear; just additional records are present in the buffer).

       Update walker tests if they construct a stand-alone interpreter without the new stepIdx/stepTot fields — Go zero-values are fine for stepIdx=0 and stepTot=0 in tests that don't drive walkBody.

       Workflowcheck (`go install go.temporal.io/sdk/contrib/tools/workflowcheck@latest && workflowcheck ./pkg/interpreter/...`) MUST report zero findings — using workflow.Now (already deterministic per SDK docs) and workflow.GetLogger keeps things determinism-safe.

    5. Commit GREEN interpreter:
       ```
       git add pkg/interpreter/
       node "$HOME/.claude/get-shit-done/bin/gsd-tools.cjs" commit "feat(260502-guu): emit structured slog events from interpreter walkers"
       ```
  </action>
  <verify>
    <automated>cd /Users/mikel/dev/ai/temporero && go test ./pkg/cli/... ./pkg/interpreter/... -race -count=1</automated>
  </verify>
  <done>
    - All TestProgress_* tests pass; old TestSlogProgress_* tests removed.
    - All pkg/interpreter tests pass (no regression on walk_*_test.go, lambda_eval_test.go, workflow_test.go).
    - Interpreter walkers emit slog records with `event` attribute at every documented schema point.
    - workflowcheck reports zero findings (or skips if not installed in CI — but locally if installed, must be clean).
    - Three commits: RED tests + GREEN renderer + GREEN interpreter.
  </done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: Fix B Wiring + e2e + PROJECT.md note (RED → GREEN → SMOKE)</name>
  <files>
    - pkg/cli/options.go
    - pkg/cli/flags.go
    - pkg/cli/root.go
    - pkg/cli/render.go
    - pkg/cli/run.go
    - pkg/cli/run_test.go
    - pkg/worker/options.go
    - pkg/worker/worker.go
    - .planning/PROJECT.md
  </files>
  <behavior>
    - TestRun_VerboseFlagWiresSDKLogger (NEW, in run_test.go):
      Construct a *cobra.Command via NewRootCommand with a fake clientFactory that captures the client.Options handed to it. Set --verbose=false (default), execute against a minimal flow, assert the captured client.Options.Logger handles slog records at level=ErrorLevel+1 (drops INFO) — observable by writing an INFO record through the captured logger and checking the wrapped buffer is empty.
      Then re-run with --verbose=true; assert the same INFO record DOES surface (charm-log handler).

      Implementation note: the existing connect_test.go already shows the clientFactory test seam pattern — re-use it for this test.

    - TestProgressHandler_WrapsWorkerLogger (NEW, in run_test.go):
      Verify the WorkerOptions.Logger handed to sdkWorkerNew has a *progressHandler underneath. Use the existing sdkWorkerNew test seam (pkg/worker/worker.go) — capture WorkerOptions in a fake; assert that captured.Logger is non-nil; fire a slog.Record with `event=step_dispatch` through it; assert the progressHandler intercepts (writes Bazel-style line to its `out` field) rather than passing through to the underlying charm-log handler.

    - TestSDKLoggerRoundTripPreservesEventAttr (NEW, behavior gate, in run_test.go OR sdk_logger_test.go):
      Call `sdklog.NewStructuredLogger(slogWithCapture).Info("msg", "event", "step_dispatch", "idx", 1)`, then assert the captured slog.Record has `event` as an Attr (not concatenated into msg). Proves the SDK adapter preserves keyvals as slog Attrs through the round-trip — the routing discriminator survives the SDK translation.

    - WorkerOptions.Logger field: existing pkg/worker/worker_test.go must still pass (tests don't pass Logger → nil → SDK default → no behavior change).
  </behavior>
  <action>
    1. RED test scaffolding: Open pkg/cli/run_test.go. Read the existing test patterns (connect_test.go shows the clientFactory pattern — see how withFakeFactory works). Add the two new tests with the assertions described in <behavior>. They WILL FAIL because:
       - cfg.Verbose doesn't exist yet
       - --verbose flag isn't registered
       - run.go doesn't construct any SDK Logger

       Commit RED:
       ```
       git add pkg/cli/run_test.go
       node "$HOME/.claude/get-shit-done/bin/gsd-tools.cjs" commit "test(260502-guu): RED — --verbose flag wires SDK Logger; progressHandler routes default slog"
       ```

    2. GREEN config + flag:
       - pkg/cli/options.go: add `Verbose bool` to config struct (alongside Debug).
       - pkg/cli/flags.go: in registerPersistentFlags add:
         ```go
         root.PersistentFlags().BoolVar(&cfg.Verbose, "verbose", false,
             "show Temporal SDK INFO/DEBUG logs alongside Skytime progress (default: hidden)")
         ```
       - pkg/cli/render.go: extend setupLogging to take Verbose into account. Two distinct loggers are returned (or one logger + one helper):
         ```go
         // setupLogging now also returns the SDK-side slog.Logger for run.go
         func setupLogging(debug, verbose bool) (skytimeLogger *slog.Logger, sdkLogger *slog.Logger) {
             // skytimeLogger: same charm-log handler as today, used for slog.Default().
             // sdkLogger: when verbose=true, same charm-log handler. When verbose=false,
             //   a slog.NewTextHandler wrapping io.Discard at slog.LevelError+1
             //   (effectively: drops everything Temporal might emit).
             ...
         }
         ```
         Or keep setupLogging returning one logger and add a NEW helper newSDKSlogLogger(verbose, debug) for the SDK side. Either works; pick whichever produces the smaller diff.

       - pkg/cli/root.go: PersistentPreRunE updates the call to setupLogging to pass cfg.Verbose. Store both loggers on cfg (add `sdkLogger *slog.Logger` field to options.go config).

    3. GREEN worker Logger plumbing:
       - pkg/worker/options.go: add `Logger *slog.Logger` field to WorkerOptions (importing log/slog from stdlib — this is firewall-allowed in pkg/worker which can import the SDK).
       - pkg/worker/worker.go: in NewWorker, after building sdkOpts, add:
         ```go
         import sdklog "go.temporal.io/sdk/log"
         // ...
         if opts.Logger != nil {
             sdkOpts.Logger = sdklog.NewStructuredLogger(opts.Logger)
         }
         ```

    4. GREEN run.go wiring: In newRunCommand RunE, after PersistentPreRunE has populated cfg.Logger and cfg.sdkLogger:
       - workflow.GetLogger inside the worker process uses the worker's sdkOpts.Logger — the workflow logger is what the interpreter walkers emit through. So the routing path is:
         interpreter walkers → workflow.GetLogger → SDK → sdkOpts.Logger (NewStructuredLogger of cfg.sdkLogger) → slog → progressHandler → renders Bazel lines OR passes through to charm-log.

         Therefore cfg.sdkLogger MUST itself be wrapped with the progressHandler before being handed to worker.NewWorker. Reordering:
         ```go
         // In RunE, after connect:
         progressOut := cmd.OutOrStdout()
         charmLogHandler := cfg.sdkLogger.Handler()  // the existing handler from setupLogging
         routedHandler := newProgressHandler(charmLogHandler, progressOut)
         routedSlog := slog.New(routedHandler)

         w, err := worker.NewWorker(c, worker.WorkerOptions{
             // ... existing fields ...
             Logger: routedSlog,
         })
         ```
       - Pass routedSlog also to client.Options.Logger via the worker package: extend pkg/worker/{Cloud,SelfHosted,DevClient}Options with `Logger *slog.Logger` and thread into client.Options.Logger via `sdklog.NewStructuredLogger(opts.Logger)` inside NewCloudClient/NewSelfHostedClient/NewDevClient. Update connectClientWithFactory in pkg/cli/connect.go to populate Logger on the option struct from cfg.

       - Default verbose=false → cfg.sdkLogger uses io.Discard handler at LevelError+1 → SDK logs vanish. The progressHandler routes `event=*` records to the Bazel renderer regardless (since the renderer is in front of the wrapped handler — the discriminator is on the record, not the level).

       Edge case: verbose=false means the SDK's INFO("Started Worker ...") record reaches the wrapped charm-log handler which has level threshold LevelError+1, so it drops. Skytime's interpreter `event` records bypass that threshold because progressHandler.Handle short-circuits before delegating. Important: the renderer's Enabled() must return TRUE for InfoLevel always (so SDK's pre-Handle Enabled() check doesn't drop our records before they hit Handle):
       ```go
       func (p *progressHandler) Enabled(ctx context.Context, level slog.Level) bool {
           return true  // we own routing inside Handle; never gate at Enabled
       }
       ```
       Update the existing Enabled implementation to return true unconditionally. The existing TestSlogProgress_PassthroughRespectsLevel was deleted in Task 2 because its assertion is no longer correct under this design.

    5. Run all unit tests: `go test ./... -race -count=1`. Everything must pass.

       Commit GREEN wiring:
       ```
       git add pkg/cli/ pkg/worker/
       node "$HOME/.claude/get-shit-done/bin/gsd-tools.cjs" commit "feat(260502-guu): --verbose flag wires SDK Logger through progress renderer (Fix B wiring)"
       ```

    6. SMOKE end-to-end (REQUIRED — proves both A + B):
       ```bash
       cd /Users/mikel/dev/ai/temporero
       go build -o /tmp/skytime-260502-guu ./cmd/skytime
       /tmp/skytime-260502-guu dev-server --log-level=warn &
       DEV_PID=$!
       sleep 2
       /tmp/skytime-260502-guu run examples/skeleton/simple_check.star --flow=simple_check --input='{"repo_path":"octocat/hello"}' > /tmp/skytime-run.out 2> /tmp/skytime-run.err
       RUN_EXIT=$?
       kill $DEV_PID 2>/dev/null
       wait $DEV_PID 2>/dev/null

       echo "RUN_EXIT=$RUN_EXIT"
       echo "--- stdout ---"
       cat /tmp/skytime-run.out
       echo "--- stderr ---"
       cat /tmp/skytime-run.err
       ```
       Assertions (all MUST be true):
       - $RUN_EXIT != 124 (timeout) — total runtime must be < 10 s. The flow may exit non-zero because example.com isn't a real GitHub repo, but it must NOT hang.
       - Combined stdout+stderr CONTAINS `[skytime]` prefix at least once.
       - Combined stdout+stderr does NOT contain `INFO  Started Worker` (raw stdlib log shape — the verbose-off SDK Logger swallows it).
       - Combined stdout+stderr does NOT contain `no credential resolver configured` (Fix A guarantees this — simple_check.star uses http.endpoint without credential=, so CredentialID is empty).
       - Combined stdout+stderr does NOT contain repeated `retry attempt — credential cache invalidated` lines (Fix A: there's nothing for the activity to retry on).

       Then re-run with --verbose:
       ```bash
       /tmp/skytime-260502-guu dev-server --log-level=warn &
       DEV_PID=$!
       sleep 2
       /tmp/skytime-260502-guu run --verbose examples/skeleton/simple_check.star --flow=simple_check --input='{"repo_path":"octocat/hello"}' > /tmp/skytime-run-v.out 2> /tmp/skytime-run-v.err
       kill $DEV_PID 2>/dev/null
       wait $DEV_PID 2>/dev/null
       ```
       Assertion: combined output contains charm-log-formatted SDK lines (e.g., look for "Started Worker" or "ExecuteActivity" — exact phrasing depends on SDK version; the test is "the verbose flag DID change visible output volume by adding SDK lines"). Document the actual observed line in the SUMMARY.

       If any assertion fails, debug — do not commit until smoke passes.

    7. PROJECT.md note: open .planning/PROJECT.md, find the Validated section under Phase 4 lines (the `- ✓ ...` block ending with the "examples/skeleton" line). Append two NEW lines (do NOT remove or edit existing lines):
       ```
       - ✓ Empty-CredentialID bypass — `pkg/activity` per-action loop short-circuits the resolver call when `dag.ActionRef.CredentialID == ""`; operation receives `nil` credential. Closes the noopCredentialHandler retry-storm audit item from quick 260501-p7c — Phase 4
       - ✓ Bazel-style colored CLI output — `skytime run` default output renders interpreter slog events (`flow_start` / `step_dispatch` / `step_complete` / `branch` / `flow_complete`) as a Bazel-style step list with `[skytime]` banner, `[N/M]` counters, kind-aligned labels, ✓/✗ status markers; `--verbose` persistent flag toggles SDK INFO/DEBUG visibility through charm-log — Phase 4
       ```

       Commit:
       ```
       git add .planning/PROJECT.md
       node "$HOME/.claude/get-shit-done/bin/gsd-tools.cjs" commit "docs(260502-guu): PROJECT.md — credential-bypass + Bazel-style output validated"
       ```
  </action>
  <verify>
    <automated>cd /Users/mikel/dev/ai/temporero && go test ./... -race -count=1</automated>
  </verify>
  <done>
    - --verbose flag exists on root and reachable via cmd.PersistentFlags().Lookup("verbose").
    - With verbose=false: TestRun_VerboseFlagWiresSDKLogger passes, SDK INFO records dropped.
    - With verbose=true: same test passes (re-runs with the flag set), SDK INFO records visible.
    - WorkerOptions.Logger + {Cloud,SelfHosted,DevClient}Options.Logger fields exist; nil = SDK default; non-nil = wrapped via sdklog.NewStructuredLogger.
    - Smoke test transcript (stdout+stderr) saved to /tmp/skytime-run.out + /tmp/skytime-run.err meets all 5 assertions in step 6.
    - PROJECT.md has two new validated capability lines under Phase 4.
    - Three commits exist in this task: RED tests + GREEN wiring + docs.
    - Final `go test ./... -race -count=1` exits 0.
  </done>
</task>

</tasks>

<verification>
After all three tasks complete, run the full gate:

```bash
cd /Users/mikel/dev/ai/temporero
go test ./... -race -count=1
go build -o /tmp/skytime-260502-guu ./cmd/skytime
```

Both commands must exit 0.

Then re-run the smoke test from Task 3 step 6 ONE MORE TIME from a clean shell to confirm the build artifact behaves correctly. Capture the output in the SUMMARY.md.

Specifically verify these from PROJECT.md and the prompt's acceptance criteria:
1. RUN_EXIT for the simple_check repro is < 10 s wall clock and not 124.
2. Default output starts with `[skytime] flow simple_check ... starting` Bazel banner.
3. No raw stdlib log lines (look for the `2026/05/NN HH:MM:SS INFO` pattern from Go's stdlib log package — must be absent).
4. No `no credential resolver configured` lines (Fix A).
5. With --verbose, charm-log SDK lines reappear.

If the firewall test fails (`tests/firewall_cli_test.go` or `tests/firewall_temporal_test.go`), inspect the new imports — the renderer additions in pkg/cli should NOT introduce charm-log usage anywhere outside pkg/cli; the SDK Logger wiring in pkg/worker is allowed (worker is in the SDK firewall allowlist).
</verification>

<success_criteria>
- ✓ Fix A green: TestExecuteBatch_BypassesResolverWhenCredentialIDEmpty + TestExecuteBatch_BypassesResolverPerAction_MixedIDs pass; pre-existing pkg/activity tests still pass.
- ✓ Fix A green: TestExtension_GetAcceptsNilCredential + TestExtension_PostAcceptsNilCredential pass.
- ✓ Fix B green: TestProgress_BazelFormat (table-driven, all event types) + TestProgress_NestedStepPath + TestProgress_PassthroughOnNonSkytimeRecord pass.
- ✓ Fix B green: TestRun_VerboseFlagWiresSDKLogger + TestProgressHandler_WrapsLoggerInRunCmd pass.
- ✓ Test gates: `go test ./... -race -count=1` exits 0.
- ✓ End-to-end repro: `./skytime run examples/skeleton/simple_check.star --flow=simple_check --input='{"repo_path":"x"}'` runs in < 10 s, output contains `[skytime]` lines, no raw stdlib log noise, no credential retry storm.
- ✓ End-to-end repro with --verbose: SDK lines reappear via charm-log.
- ✓ PROJECT.md has two new validated-capability lines under Phase 4 (existing lines untouched).
- ✓ Firewall tests still pass (tests/firewall_cli_test.go, tests/firewall_temporal_test.go).
- ✓ Per-task commit cadence: RED → GREEN cycles atomic; each cycle a separate git commit with node "$HOME/.claude/get-shit-done/bin/gsd-tools.cjs" commit.
</success_criteria>

<output>
After completion, create `.planning/quick/260502-guu-bypass-empty-credentialid-resolve-bazel-/260502-guu-SUMMARY.md` containing:

- Summary of what changed (Fix A + Fix B Core + Fix B Wiring).
- The exact bytes from the smoke test transcript (stdout + stderr) — paste both default and --verbose outputs verbatim so reviewers can see the Bazel format and the verbose contrast without rebuilding.
- Commit list (RED + GREEN cadence — should be ~7 commits across the 3 tasks).
- Notes on any deviations from the plan (Rule 1/2/3 deviations called out per Skytime convention).
- The new PROJECT.md lines added.
- Confirmation that go test ./... -race -count=1 exited 0.
</output>
