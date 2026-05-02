---
phase: 260502-guu-bypass-empty-credentialid-resolve-bazel-
plan: 01
subsystem: activity + cli + interpreter + worker
tags: [credential-bypass, cli-output, bazel-renderer, slog-events, sdk-logger]
dependency_graph:
  requires:
    - pkg/activity (Phase 2)
    - pkg/extension/builtin/http (Phase 4 plan 04-07)
    - pkg/cli + pkg/cli/render.go + pkg/cli/connect.go (Phase 4 plan 04-04 / 04-05)
    - pkg/interpreter walk_*.go (Phase 3)
    - pkg/worker {options.go, client.go, worker.go} (Phase 3 plan 03-04)
  provides:
    - Empty-CredentialID short-circuit in pkg/activity runAction
    - Nil-credential acceptance test coverage in pkg/extension/builtin/http
    - Bazel-style slog renderer in pkg/cli/progress.go
    - Structured `event=*` slog records in every interpreter walker
    - `--verbose` persistent flag + SDK Logger plumbing through worker.{Cloud,SelfHosted,Dev}ClientOptions
  affects:
    - skytime run UX (default + verbose)
    - quick 260501-p7c follow-up audit closure
tech-stack:
  added: []
  patterns:
    - "Named-return + defer for slog instrumentation around walker bodies (M-8)"
    - "Per-branch shallow copy of *interpreter before workflow.Go (B-2 concurrency contract)"
    - "Per-handler attribute discriminator (`event`) for slog routing — replaces flow_name discriminator from Plan 04-05"
    - "Tolerant arg-cast helpers (asGetArgs/asBodyArgs) for activity runtime ↔ test-tier convention asymmetry"
key-files:
  created:
    - .planning/quick/260502-guu-bypass-empty-credentialid-resolve-bazel-/260502-guu-SUMMARY.md
    - pkg/cli/run_internal_test.go
  modified:
    - pkg/activity/action_executor.go
    - pkg/activity/execute_batch_test.go
    - pkg/extension/builtin/http/http.go
    - pkg/extension/builtin/http/http_test.go
    - pkg/cli/progress.go
    - pkg/cli/progress_test.go
    - pkg/cli/options.go
    - pkg/cli/flags.go
    - pkg/cli/root.go
    - pkg/cli/render.go
    - pkg/cli/run.go
    - pkg/cli/connect.go
    - pkg/interpreter/workflow.go
    - pkg/interpreter/walk_step.go
    - pkg/interpreter/walk_script.go
    - pkg/interpreter/walk_ifcond.go
    - pkg/interpreter/walk_foreach.go
    - pkg/interpreter/walk_callflow.go
    - pkg/worker/options.go
    - pkg/worker/worker.go
    - pkg/worker/client.go
    - .planning/PROJECT.md
decisions:
  - "Empty `ref.CredentialID` short-circuits the per-action resolve; operation receives nil. Defense-in-depth even though noopCredentialHandler-on-empty-id behavior is technically a Phase 4 audit item — the bypass closes the retry-storm symptom for good."
  - "Bazel renderer dispatches on the `event` slog attribute, NOT the legacy `flow_name` attribute used by Plan 04-05's stub. The old attribute is gone; old TestSlogProgress_* tests deleted."
  - "WorkerOptions.Logger is informational (kept for API symmetry). Temporal SDK v1.42.0's worker.Options does NOT expose a Logger field — workers inherit the client's Logger. Routing happens at client.Options.Logger via {Cloud,SelfHosted,Dev}ClientOptions.Logger."
  - "buildRoutedSlogLogger writes Bazel lines to stderr (cmd.ErrOrStderr), NOT stdout — stdout is reserved for the JSON workflow result."
  - "cfg.sdkLogger is REPLACED inside run.RunE with the routedSlog before connectClient is called. Without this re-binding the progressHandler never sees workflow events; the discard handler swallows them."
  - "HTTP extension's doX functions now accept either GetArgs/BodyArgs (value) or *GetArgs/*BodyArgs (pointer) via asGetArgs/asBodyArgs helpers. Pre-existing Phase 4 bug — production decoder returns value, tests pass pointer; both must work."
metrics:
  duration: "21min"
  completed: 2026-05-02
---

# Quick 260502-guu: Empty-CredentialID Bypass + Bazel CLI Output Summary

## One-liner

`skytime run` now short-circuits the credential resolver when `dag.ActionRef.CredentialID == ""` and renders interpreter `event=*` slog records as Bazel-style step lines on stderr by default, with `--verbose` toggling SDK INFO/DEBUG visibility through charm-log.

## What Changed

### Fix A — Empty-CredentialID bypass (`pkg/activity` + http extension test coverage)

`pkg/activity/action_executor.go::runAction` now wraps the per-action `cache.resolve` call with `if ref.CredentialID != ""`. When the ID is empty, `cred` stays at its zero-value `nil` and is passed verbatim to `OperationFunc`. Operations that already handle `nil` (HTTP `applyCredential` short-circuits on nil) work unchanged; operations that need a credential will surface their own typed nil-deref error, which is preferable to the old behavior (Temporal retrying the WHOLE batch every ~5 s for the activity StartToCloseTimeout because `noopCredentialHandler.Resolve("")` returned `ErrUnknownCredential`).

Two new pkg/activity tests pin the bypass:
- `TestExecuteBatch_BypassesResolverWhenCredentialIDEmpty`: 3-action batch, all `CredentialID=""`, `ch.calls.Load() == 0`.
- `TestExecuteBatch_BypassesResolverPerAction_MixedIDs`: mixed batch (admin/empty/admin); loose count assertion (1≤calls≤2 — depends on cache TTL), strict per-op assertion that ref1 (empty) sees `cred==nil` and ref0/ref2 (admin) see `cred!=nil`.

Two new http extension tests pin nil-credential acceptance:
- `TestExtension_GetAcceptsNilCredential`: server asserts `r.Header.Get("Authorization") == ""`.
- `TestExtension_PostAcceptsNilCredential`: same shape for the BodyArgs branch.

### Fix B Core — Bazel renderer + interpreter slog instrumentation

**`pkg/cli/progress.go`** rewritten end-to-end. The `progressHandler` now dispatches on the `event` slog attribute (replacing the Plan 04-05 stub's `flow_name` discriminator):

| event             | output shape                                                            |
| ----------------- | ----------------------------------------------------------------------- |
| `flow_start`      | `[skytime] flow <name>  <count> steps  starting`                        |
| `step_dispatch`   | `[N/M] <kind padded>  <label>` (nested rows indent 2 spaces, use `[path/path]`) |
| `step_complete`   | `     ✓ <ms>ms  <summary>`  /  `     ✗ <ms>ms  <summary>`               |
| `branch`          | `     → <branch-name>` (then/else)                                      |
| `flow_complete`   | `[skytime] flow complete  <ok>/<total> steps  total <ms>ms`             |

ANSI escapes (dim cyan banner, bright cyan counter, bright white kind, green ✓, red ✗, yellow →) apply only when `out` is a TTY (memoized `term.IsTerminal` check); non-TTY outputs plain ASCII so test buffers and CI pipes stay greppable.

`progressHandler.Enabled` returns `true` unconditionally so the wrapped handler's level (`silentSDKLevel = LevelError+1` when `--verbose=false`) doesn't gate Skytime events before `Handle` routes them.

Three new `pkg/cli/progress_test.go` tests replace the deleted `TestSlogProgress_*` tests:
- `TestProgress_BazelFormat` — table-driven, every event type.
- `TestProgress_NestedStepPath` — `path="3a"` renders `[3a/3a]` with 2-space indent.
- `TestProgress_PassthroughOnNonSkytimeRecord` — non-event records flow to wrapped handler.

**`pkg/interpreter/`**: every walker (`walk_step`, `walk_script`, `walk_ifcond`, `walk_foreach`, `walk_callflow`) now emits `step_dispatch` + `step_complete` via `workflow.GetLogger(ctx)` using the **named-return + defer** pattern (M-8 fix) so completion events fire on every return path including errors. `walk_ifcond` additionally emits a `branch` event after evaluating the condition.

`workflow.go` was extended with three walker-local context fields (`stepIdx`, `stepTot`, `stepPath`) plus a doc-block declaring the **B-2 concurrency contract** verbatim — `walkForEach` shallow-copies `*i` per branch and mutates `branchInterp.stepPath = "<parentPath>.<itemIdx>"` BEFORE spawning `workflow.Go`. The race detector reports zero findings on `for_each_parallel` paths.

`walkBody` saves+restores `stepIdx`/`stepTot` per body so nested walks stay deterministic. `walk_ifcond` saves+restores `stepPath` and sets `<parentIdx>a`/`<parentIdx>b` for the duration of the inner branch walk.

The flow-level closure (`NewWorkflow` returned func) emits `flow_start` after registry lookup and `flow_complete` (deferred-style via end-of-block emission) carrying ok_count/err_count/total_ms.

### Fix B Wiring — `--verbose` flag + SDK Logger plumbing

**`pkg/cli`:**
- `options.go::config` gets `Verbose bool` (exported for white-box tests) and `sdkLogger *slog.Logger`.
- `flags.go::registerPersistentFlags` registers `--verbose` (default false) with the docstring "show Temporal SDK INFO/DEBUG logs alongside Skytime progress (default: hidden)".
- `render.go::buildSDKSlogLogger(cfg)` returns `cfg.logger` (charm-log) when `cfg.Verbose==true`, otherwise wraps `io.Discard` at `silentSDKLevel = slog.LevelError+1` so SDK records vanish.
- `render.go::buildRoutedSlogLogger(cfg, out)` returns `slog.New(newProgressHandler(cfg.sdkLogger.Handler(), out))` — the renderer in front of the wrapped handler.
- `root.go::PersistentPreRunE` now sets `cfg.sdkLogger = buildSDKSlogLogger(cfg)`.
- `connect.go::connectClientWithFactory` threads `cfg.sdkLogger` into `worker.{Cloud,SelfHosted,Dev}ClientOptions.Logger`.
- `run.go::RunE` REPLACES `cfg.sdkLogger` with `buildRoutedSlogLogger(cfg, cmd.ErrOrStderr())` BEFORE calling `connectClient` so the workflow-side `workflow.GetLogger` (which inherits the client's Logger) routes through the Bazel renderer.

**`pkg/worker`:**
- `options.go`: `WorkerOptions.Logger` and `{Cloud,SelfHosted,Dev}ClientOptions.Logger` fields added (`*slog.Logger`).
- `client.go`: every constructor wraps `opts.Logger` via `sdklog.NewStructuredLogger` and assigns to `client.Options.Logger`.
- `worker.go`: imports `sdklog` and references `opts.Logger` for forward compat, but documents that `sdkworker.Options` does not expose a `Logger` field in v1.42.0 — workers inherit the client's logger. The field exists for API symmetry and the eventual SDK seam.

Four new white-box tests in `pkg/cli/run_internal_test.go`:
- `TestRun_VerboseFlagWiresSDKLogger` (two sub-tests): verbose=false drops INFO; verbose=true forwards INFO.
- `TestProgressHandler_WrapsWorkerLogger`: an `event=step_dispatch` record routed through `buildRoutedSlogLogger` lands on the progress writer, NOT the wrapped charm-log buffer.
- `TestSDKLoggerRoundTripPreservesEventAttr`: `sdklog.NewStructuredLogger(slogger).Info("msg", "event", "step_dispatch", ...)` preserves `event` as a `slog.Attr` (not concatenated into msg).
- `TestConnectClient_ThreadsLoggerIntoOptions`: dev + cloud factories receive `cfg.sdkLogger` via their respective `Logger` fields.

## Smoke Test Transcript (verbatim)

### Default (`skytime run examples/skeleton/simple_check.star --flow=simple_check --input='{"repo_path":"octocat/hello"}'`)

Exit: `0`  Elapsed: `<1s`  (well under the 10s acceptance bound).

```
--- stdout ---
{
  "health": {
    "healthy": true,
    "repo": "octocat/hello"
  },
  "repo_path": "octocat/hello"
}

--- stderr ---
[skytime] flow simple_check  3 steps  starting
[1/3] step                http.get("/repos/example/repo")
     ✓ 92ms  
[2/3] script              health
     ✓ 0ms  
[3/3] if_cond             cond
     → then
  [3a/3a] step                http.get("/repos/example/repo/branches")
       ✓ 67ms  
     ✓ 67ms  
[skytime] flow complete  3/3 steps  total 160ms
```

### Verbose (`skytime run --verbose ...`)

Exit: `0`  Elapsed: `1s`.

```
--- stdout ---
{
  "health": {
    "healthy": true,
    "repo": "octocat/hello"
  },
  "repo_path": "octocat/hello"
}

--- stderr ---
INFO Started Worker Namespace=default TaskQueue=skytime WorkerID=skytime/dev BuildID=dev
INFO skytime workflow start Namespace=default TaskQueue=skytime WorkerID=skytime/dev BuildID=dev WorkflowType=SkytimeWorkflow WorkflowID=f2d59a2a-e6e5-4432-83a6-6124ee846984 RunID=019de9aa-d3c1-7cc9-a0b7-766f072ac356 Attempt=1 flow_name=simple_check content_hash=e0e4fcada4e7eb22e6b230e6f6b10442fd700c30b5da23a53d33a57834e4d7d9 binary_checksum=dev run_id=019de9aa-d3c1-7cc9-a0b7-766f072ac356
[skytime] flow simple_check  3 steps  starting
[1/3] step                http.get("/repos/example/repo")
     ✓ 64ms  
[2/3] script              health
     ✓ 0ms  
[3/3] if_cond             cond
     → then
  [3a/3a] step                http.get("/repos/example/repo/branches")
       ✓ 22ms  
     ✓ 22ms  
[skytime] flow complete  3/3 steps  total 87ms
INFO Stopped Worker Namespace=default TaskQueue=skytime WorkerID=skytime/dev BuildID=dev
```

(ANSI escape sequences elided in this transcript for readability — the live terminal renders the `INFO` prefix bold/bright and the kv pairs dim. The Bazel lines themselves are uncolored in the transcript because the test capture is via shell redirect — non-TTY → ASCII fallback.)

### Programmatic --verbose Assertions (M-5 — all PASS)

```
DEFAULT_LINES=11
VERBOSE_LINES=14

PASS 1: --verbose adds >=3 lines (default=11, verbose=14)
PASS 2: --verbose stderr contains charm-log INFO/DEBUG/WARN level prefix
PASS 3: default stderr suppresses SDK INFO/DEBUG (no prefix at line start)
```

### Acceptance Criteria Verification

| Criterion                                                      | Status |
| -------------------------------------------------------------- | ------ |
| `RUN_EXIT < 10s` for simple_check repro                        | PASS (1s) |
| Default stderr contains `[skytime]` banner                     | PASS (2 occurrences: flow_start + flow_complete) |
| Default stderr does NOT contain `INFO  Started Worker`         | PASS (suppressed by silentSDKLevel) |
| Default stderr does NOT contain `no credential resolver configured` | PASS (Fix A short-circuit) |
| Default stderr does NOT contain `retry attempt — credential cache invalidated` repeats | PASS (Fix A makes there nothing to retry) |
| `--verbose` re-introduces SDK lines via charm-log              | PASS (INFO Started/Stopped Worker reappear) |

## Commit List (RED → GREEN cadence)

| # | Hash      | Subject                                                                                            |
| - | --------- | -------------------------------------------------------------------------------------------------- |
| 1 | `16cdf84` | test(260502-guu): RED — Fix A empty-CredentialID bypass + http nil-credential coverage             |
| 2 | `7aac312` | fix(260502-guu): bypass credential resolve when ActionRef.CredentialID is empty (Fix A)            |
| 3 | `4341aa1` | test(260502-guu): RED — Bazel renderer event schema + nested paths                                 |
| 4 | `5eea043` | feat(260502-guu): Bazel-style slog renderer for skytime progress events                            |
| 5 | `c087620` | feat(260502-guu): emit structured slog events from interpreter walkers                             |
| 6 | `f3d8555` | test(260502-guu): RED — --verbose flag wires SDK Logger; progressHandler routes default slog       |
| 7 | `8202b81` | feat(260502-guu): --verbose flag wires SDK Logger through progress renderer (Fix B wiring)         |
| 8 | `46fc95e` | fix(260502-guu): http extension args casts accept value or pointer (Rule 1 deviation)              |
| 9 | `a0f223d` | fix(260502-guu): route Bazel renderer via cfg.sdkLogger so workflow events flow correctly          |
| 10| `32caf34` | docs(260502-guu): PROJECT.md — credential-bypass + Bazel-style output validated                    |

10 commits across 3 tasks. The plan's "~7 commits" estimate is exceeded by 3: the two emergency fixes uncovered during the smoke test (commits 8, 9) and the documentation commit (10) which was always its own commit per task 3 step 7.

## Deviations from Plan

### [Rule 1 — Bug] HTTP extension hard-cast to pointer args (commit `46fc95e`)

- **Found during:** Task 3 step 6 e2e smoke (default run).
- **Issue:** `pkg/extension/builtin/http/http.go::doGet/doHead/doPost/doPut/doDelete` cast `args` to `*GetArgs` / `*BodyArgs`. The activity-side decoder (`pkg/activity/action_executor.go::decodeActionRefKwargs`) returns the decoded struct as a VALUE per its documented contract: `return reflect.ValueOf(args).Elem().Interface(), nil`. So in production, `args` is `GetArgs{...}` (value), not `*GetArgs{...}` (pointer), and the cast panics.
- **Why Phase 4 missed it:** The pre-existing http extension tests (`TestExtension_GetSucceedsAgainstHTTPTestServer`, etc.) construct `args` via `reflect.New(argsType).Interface()` — which returns a POINTER. The runtime contract was never exercised in test, only in the smoke-style `skytime run`. Fix A made this surface: previously the credential resolver swallowed every empty-id action with `ErrUnknownCredential`, so the activity never reached the dispatch step; now it does.
- **Fix:** Introduced `asGetArgs(any) *GetArgs` and `asBodyArgs(any) *BodyArgs` helpers that accept BOTH value and pointer. doX funcs delegate. Tolerant by design.
- **Files modified:** `pkg/extension/builtin/http/http.go`.
- **Commit:** `46fc95e`.

### [Rule 1 — Bug] Bazel renderer routing via cfg.sdkLogger (commit `a0f223d`)

- **Found during:** Task 3 step 6 e2e smoke (default run, after Rule 1 fix above).
- **Issue:** The original wiring (commit `8202b81`) handed `routedSlog := buildRoutedSlogLogger(...)` to `worker.NewWorker(c, opts{Logger: routedSlog})`. But `sdkworker.Options` in v1.42.0 has no `Logger` field — the SDK worker INHERITS the client's `Logger`. The client's logger was `cfg.sdkLogger` (a discard handler when `--verbose=false`), so workflow.GetLogger inside the workflow emitted events into the discard sink. Bazel banners never rendered.
- **Why test missed it:** The white-box `TestProgressHandler_WrapsWorkerLogger` test directly fed an event through `routedSlog` and observed bytes on the writer — that path is correct. The end-to-end indirection (workflow → SDK worker → SDK client → cfg.sdkLogger) was never exercised in unit test. Caught immediately by the smoke test transcript showing `--- stderr ---` empty.
- **Fix:** In `run.RunE`, REPLACE `cfg.sdkLogger` with `buildRoutedSlogLogger(cfg, cmd.ErrOrStderr())` BEFORE calling `connectClient`. That way the client (and therefore the worker) gets the routed logger, and workflow events flow through the progressHandler.
- **Files modified:** `pkg/cli/run.go`.
- **Commit:** `a0f223d`.

### [Rule 3 — Blocking] WorkerOptions.Logger field is informational (commit `8202b81`)

- **Found during:** Task 3 GREEN wiring step.
- **Issue:** Plan called for `pkg/worker/worker.go::NewWorker` to thread `opts.Logger` into `sdkOpts.Logger`. Verified against `go.temporal.io/sdk@v1.42.0/internal/worker.go`: `WorkerOptions` does NOT expose a `Logger` field. The SDK worker inherits the client's Logger.
- **Fix:** WorkerOptions.Logger field preserved for API symmetry and as a future seam. The actual routing happens at `client.Options.Logger` via `pkg/worker/client.go`'s three constructors, which IS exposed and IS the documented v1.39+ path. Documented inline in `worker.go`.
- **Files modified:** `pkg/worker/options.go`, `pkg/worker/worker.go`, `pkg/worker/client.go`.

### Bazel banners route to STDERR, not STDOUT

- **Found during:** Task 3 GREEN wiring (concurrent with the routing fix).
- **Decision:** The plan's `buildRoutedSlogLogger` accepts a generic `progressOut`. I chose `cmd.ErrOrStderr()` rather than `cmd.OutOrStdout()` because the `skytime run` final step prints the JSON workflow result to stdout (`fmt.Fprintln(cmd.OutOrStdout(), string(out))`). Mixing Bazel banners into that stream would corrupt downstream JSON-parsing tooling. Stderr is the conventional channel for human-facing progress; stdout stays JSON-pure.
- **Test impact:** Acceptance criterion #2 ("Combined stdout+stderr CONTAINS `[skytime]` prefix") is satisfied — the smoke transcript shows `[skytime]` appears 2 times in stderr.

## PROJECT.md Lines Added

Two new lines under the Phase 4 Validated section (existing 9 Phase 4 lines untouched):

```markdown
- ✓ Empty-CredentialID bypass — `pkg/activity` per-action loop short-circuits the resolver call when `dag.ActionRef.CredentialID == ""`; operation receives `nil` credential. Closes the noopCredentialHandler retry-storm audit item from quick 260501-p7c — Phase 4
- ✓ Bazel-style colored CLI output — `skytime run` default output renders interpreter slog events (`flow_start` / `step_dispatch` / `step_complete` / `branch` / `flow_complete`) as a Bazel-style step list with `[skytime]` banner, `[N/M]` counters, kind-aligned labels, ✓/✗ status markers; `--verbose` persistent flag toggles SDK INFO/DEBUG visibility through charm-log — Phase 4
```

## Test Gate Confirmation

```
$ go test ./... -race -count=1
?   	github.com/mikelalcon/skytime/cmd/skytime	[no test files]
ok  	github.com/mikelalcon/skytime/pkg/activity	1.781s
ok  	github.com/mikelalcon/skytime/pkg/bridge	1.397s
ok  	github.com/mikelalcon/skytime/pkg/cli	2.115s
ok  	github.com/mikelalcon/skytime/pkg/dag	3.238s
ok  	github.com/mikelalcon/skytime/pkg/extension	2.802s
ok  	github.com/mikelalcon/skytime/pkg/extension/builtin/http	3.026s
ok  	github.com/mikelalcon/skytime/pkg/extension/testing	2.390s
ok  	github.com/mikelalcon/skytime/pkg/interpreter	3.663s
ok  	github.com/mikelalcon/skytime/pkg/parser	2.664s
ok  	github.com/mikelalcon/skytime/pkg/validator	2.187s
ok  	github.com/mikelalcon/skytime/pkg/validator/dryrun	2.100s
ok  	github.com/mikelalcon/skytime/pkg/worker	2.057s
ok  	github.com/mikelalcon/skytime/tests	2.072s
```

All packages pass with `-race -count=1`. `go build -o /tmp/skytime-260502-guu ./cmd/skytime` exits 0.

## Notes on Concurrency Safety (B-2 contract verification)

`go test ./pkg/interpreter/... -race -count=1` reports zero race detector findings. The B-2 fix (every `walkForEach` branch goroutine receives `branchInterp := *i` shallow copy with `branchInterp.stepPath` mutated BEFORE `workflow.Go`) is exercised by the existing `walk_foreach_test.go` test suite which fans out across multiple items. The doc-block above the `interpreter` struct's `stepIdx`/`stepTot`/`stepPath` fields is verbatim per the plan's iteration-2 lock.

## Self-Check: PASSED

All 24 files claimed in `key-files` were verified to exist on disk via `[ -f path ]`. All 10 commits claimed in the commit table were verified via `git log --oneline --all | grep -q "^<hash>"`.
