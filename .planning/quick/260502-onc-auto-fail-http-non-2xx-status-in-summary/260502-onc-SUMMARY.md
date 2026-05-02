---
phase: quick-260502-onc
plan: 01
subsystem: extensions
tags: [http, temporal, classify, retryable, slog, e2e, github-api, starlark, reflection, json]

requires:
  - phase: quick-260502-guu
    provides: empty-CredentialID bypass (anonymous endpoints) + Bazel-style colored CLI output
  - phase: 04-static-validation-tier-cli-skeleton
    provides: skytime CLI binary, baked-in HTTP extension, differential corpus, embedded transient worker, progressHandler renderer

provides:
  - HTTP non-2xx auto-fail end-to-end (4xx → NonRetryable, 5xx → retryable, 2xx unchanged)
  - extension.ErrNonRetryable sentinel — established pattern for any future extension surfacing non-retryable failures
  - walkStep status=N summary on success (reflection on typed Output + JSON-key fallback for round-tripped RawOperationOutput)
  - walkStep NonRetryableErrResult routing — D2-14 soft failures surface as workflow errors
  - progressHandler flow_failed line with red marker on err_count > 0 + lastErr per-flow lifecycle
  - examples/skeleton corpus pointing at real public GitHub endpoint (/repos/octocat/Hello-World)
  - End-to-end happy + unhappy smokes (tests/e2e_skytime_run_test.go) with Setpgid + group-kill subprocess teardown pattern

affects: [phase-05-starlark-e2e-testing-tier, phase-06-example-project, future-extensions]

tech-stack:
  added: []
  patterns:
    - "extension.ErrNonRetryable sentinel — fmt.Errorf %w wrap pattern, mirrors ErrUnknownCredential exactly"
    - "Activity classifier (isRetryable) reads errors.Is on extension sentinels — preserves the temporal-firewall (extensions cannot construct *temporal.ApplicationError)"
    - "Reflection-based per-step summary in interpreter — preserves firewall (interpreter does NOT import builtin/http) + JSON-key fallback for production wire round-trip"
    - "progressHandler lastErr lifecycle — schema-stable (no new event attrs) + reset-on-flow_start (no leak across runs)"
    - "Subprocess teardown via Setpgid + syscall.Kill(-pgid, ...) — reusable for any future test that spawns a long-running CLI"
    - "ensureDevServer probe-then-spawn — reuses existing 7233 listener if developer left one running, avoids port-collision failures"

key-files:
  created:
    - pkg/extension/error.go (28 LOC) — ErrNonRetryable sentinel
    - pkg/extension/error_test.go (42 LOC) — sentinel + errors.Is contract
    - tests/e2e_skytime_run_test.go (325 LOC) — happy + unhappy e2e smokes
  modified:
    - pkg/extension/builtin/http/http.go — doHTTP non-2xx classification
    - pkg/extension/builtin/http/http_test.go — 4 new tests (404, 500, 2xx fall-through, 422 POST)
    - pkg/activity/execute_batch.go — isRetryable extends with extension.ErrNonRetryable check
    - pkg/activity/execute_batch_test.go — 2 new tests (isRetryable unit + ExecuteBatch integration)
    - pkg/interpreter/walk_step.go — extractStatusSummary + extractFirstNonRetryable helpers + walkStep wiring
    - pkg/interpreter/walk_step_test.go — 14 new tests (helper unit + integration + RawOperationOutput round-trip)
    - pkg/cli/progress.go — failureContext type + lastErr lifecycle + renderFlowComplete branching
    - pkg/cli/progress_test.go — 4 new tests (FlowFailed, FlowComplete_NoFailure, LastErrResetsOnFlowStart, FlowFailed_NoLastErr)
    - examples/skeleton/simple_check.star — corpus → /repos/octocat/Hello-World, docstring documents v1 limitation
    - examples/skeleton/parallel_fanout.star — block batch → 3 distinct /repos/octocat/Hello-World endpoints
    - .planning/PROJECT.md — Validated bullet for the four-fix stack

key-decisions:
  - "extension.ErrNonRetryable sentinel + activity-side classification — mirrors ErrUnknownCredential pattern exactly (extensions live outside temporal-firewall, cannot construct *temporal.ApplicationError directly)"
  - "4xx vs 5xx split — 4xx wraps ErrNonRetryable (configuration / wrong path / wrong auth — no retry), 5xx wraps as plain error (transient — Temporal RetryPolicy)"
  - "Reflection-based status summary in interpreter — preserves firewall (interpreter cannot import builtin/http); FieldByName('Status') is O(struct-fields), negligible at single-step granularity"
  - "RawOperationOutput JSON fallback (Rule 1 deviation) — production results round-trip through Temporal JSON wire as RawOperationOutput, not typed HTTPResponse; helper parses raw bytes for 'status' key. Without this fallback the e2e happy renders empty summary despite the helper firing"
  - "extractFirstNonRetryable helper-level coverage of mid-block first-wins (BEHAVIOR M-3) — keeps test count manageable while preserving the integration property"
  - "progressHandler lastErr in failureContext struct — schema-stable (no new interpreter event attrs), reset on flow_start (no leak across long-lived handlers), shallow-copied through WithAttrs/WithGroup"
  - "Defensive '(no per-step error captured)' placeholder when flow_complete err_count>0 with no preceding step_complete-with-err — guards against malformed event sequences, never crashes the renderer"
  - "Subprocess teardown via Setpgid + syscall.Kill(-pgid, SIGTERM) → 3s grace → SIGKILL — reliably reaps `temporal server start-dev` children (UI, persistence, frontend) including on Ctrl-C mid-test (signal.Notify in TestMain)"
  - "ensureDevServer probe-then-spawn — if 127.0.0.1:7233 already responds, reuse it (devServerCmd stays nil, teardown is a no-op); avoids port-collision failures when a developer left a server running"
  - "TestMain does NOT pre-spawn dev server — only the e2e tests need it (devServerOnce.Do inside ensureDevServer); non-e2e tests in the same package (differential, firewall_cli) run unimpeded"
  - "M-4 require.IsType(*exec.ExitError) on unhappy path — distinguishes a real non-zero exit from a context.DeadlineExceeded timeout (which would surface as a different error type and pass a plain require.Error vacuously)"
  - "M-5 ordering guard — strings.Index(status=200) < strings.Index([skytime] flow complete) defends against partial-success regressions where flow_complete might emit before any step_complete"
  - "//go:build !windows on e2e file — Setpgid + syscall.Kill negative-pid don't exist on Windows; Windows users would need WSL/Docker for `temporal start-dev` anyway"

patterns-established:
  - "Pattern 1: Extension non-retryable sentinel — `var ErrX = errors.New(\"x\"); return nil, fmt.Errorf(\"...: %w\", ErrX)`. Activity classifier picks up via errors.Is."
  - "Pattern 2: Reflection + JSON-fallback summary extraction — when an interpreter helper needs to read a typed Output's field but the firewall blocks importing the leaf package AND production round-trips erase concrete types, the helper handles BOTH the typed-Output unit-test path (reflect) AND the RawOperationOutput production path (json.Unmarshal of `Bytes`)."
  - "Pattern 3: Subprocess group teardown — Setpgid before Start + syscall.Kill(-pid, ...) with SIGTERM-grace-SIGKILL escalation in a defer + signal handler. Mandatory for any test that spawns a long-running CLI."
  - "Pattern 4: Probe-then-spawn for shared infra — for tests that need a long-running localhost service, probe the endpoint first; reuse the existing instance if responsive, only spawn (with full teardown plumbing) when no instance is detected."

requirements-completed:
  - QUICK-260502-onc-FixA
  - QUICK-260502-onc-FixB
  - QUICK-260502-onc-FixC
  - QUICK-260502-onc-FixD

duration: 14min
completed: 2026-05-02
---

# Quick 260502-onc: HTTP Non-2xx Auto-Fail + status=N Summary + flow_failed Renderer + Corpus Update

**Silent-success-on-404 → fail-loudly-NonRetryable: extension classifies non-2xx, interpreter routes the failure to workflow level, renderer prints the failure visibly, corpus + e2e smokes prove the entire stack at the binary boundary.**

## Performance

- **Duration:** ~14 min (838s)
- **Started:** 2026-05-02T22:07:38Z
- **Completed:** 2026-05-02T22:21:36Z
- **Tasks:** 4 (all TDD: RED → GREEN per task)
- **Files modified:** 11 (3 created, 8 modified, 1 PROJECT.md note)

## Accomplishments

- **Fix A (HTTP extension):** non-2xx now wraps either `extension.ErrNonRetryable` (4xx) or a plain error (5xx); 2xx fall-through unchanged. Activity classifier (`isRetryable`) honors the new sentinel via `errors.Is`. The extension stays inside its firewall (no Temporal SDK import).
- **Fix B (interpreter):** `walkStep` now extracts `status=N` on success via reflection on `OkResult.Output.Status` (with a JSON-key fallback for the production round-trip path that decodes `Output` as `RawOperationOutput`). It also surfaces the activity layer's D2-14 `(results, nil)` "soft failure" (`NonRetryableErrResult` in the slice) as a workflow-level failure — without this, Fix A's classification would never reach the renderer.
- **Fix C (renderer):** `progressHandler` gains a per-handler `lastErr` field that captures the most recent `step_complete`-with-err record. On `flow_complete` with `err_count > 0`, it prints `[skytime] flow failed  step I/M (reason)  total Nms` with the word "failed" in red. Reset on every `flow_start` so a long-lived handler doesn't leak failure context across runs. Schema-stable: no new interpreter event attrs.
- **Fix D (corpus + e2e):** Differential corpus now points at `/repos/octocat/Hello-World` (a real public GitHub endpoint that has returned 200 for 15+ years). End-to-end happy (`status=200`, `[skytime] flow complete`, exit 0, ordering pinned) and unhappy (`✗`, `HTTP 404`, `[skytime] flow failed`, `*exec.ExitError`) smokes run the actual `skytime` binary against a real `temporal server start-dev` instance.

## Task Commits

Each task committed atomically (all `feat` — TDD RED + GREEN bundled per task per plan instruction):

1. **Task 1: Fix A — HTTP non-2xx auto-fail via ErrNonRetryable sentinel** — `f6a0019`
2. **Task 2: Fix B — walkStep status=N summary + NonRetryableErrResult routing** — `b80bd90`
3. **Task 3: Fix C — progressHandler renders flow_failed line on err_count > 0** — `055feea`
4. **Task 4: Fix D — corpus → octocat/Hello-World + e2e smokes + Rule 1 RawOperationOutput JSON fallback** — `bec8862`

## Files Created/Modified

### Created
- `pkg/extension/error.go` (28 LOC) — `ErrNonRetryable` sentinel + doc
- `pkg/extension/error_test.go` (42 LOC) — sentinel + `errors.Is` round-trip contract
- `tests/e2e_skytime_run_test.go` (325 LOC) — happy + unhappy e2e smokes; TestMain signal handler; ensureDevServer probe-then-spawn

### Modified
- `pkg/extension/builtin/http/http.go` — `doHTTP` 4xx/5xx classification (5 added LOC + 200-byte body snippet)
- `pkg/extension/builtin/http/http_test.go` — 4 new tests: `_Get_404_NonRetryable`, `_Get_500_Retryable`, `_Get_2xx_StillSuccess` (3 sub-tests: 200/204/299), `_Post_422_NonRetryable`
- `pkg/activity/execute_batch.go` — `isRetryable` extended with `errors.Is(err, extension.ErrNonRetryable)` branch; new import of `pkg/extension`
- `pkg/activity/execute_batch_test.go` — 2 new tests: `TestIsRetryable_HonorsExtensionErrNonRetryable` (6 sub-cases) + `TestExecuteBatch_HTTPNonRetryable_Integration`
- `pkg/interpreter/walk_step.go` — `extractStatusSummary` (with `RawOperationOutput` JSON fallback), `extractFirstNonRetryable`, walkStep success-path summary + failure-routing wiring; new imports: `encoding/json`, `reflect`
- `pkg/interpreter/walk_step_test.go` — 14 new tests: 8 helper unit tests covering all reflection paths + 5 sub-tests for `RawOperationOutput` JSON fallback + `TestWalkStep_NonRetryableResult_FailsWorkflow` integration gate
- `pkg/cli/progress.go` — `failureContext` struct, `progressHandler.lastErr`, lifecycle in `renderFlowStart`/`renderStepComplete`/`renderFlowComplete`, `WithAttrs`/`WithGroup` propagation
- `pkg/cli/progress_test.go` — 4 new tests + `emitProgress` helper: `_FlowFailed`, `_FlowComplete_NoFailure`, `_LastErrResetsOnFlowStart`, `_FlowFailed_NoLastErr`
- `examples/skeleton/simple_check.star` — paths swapped to `/repos/octocat/Hello-World[/branches]`; docstring documents v1 illustrative-input limitation
- `examples/skeleton/parallel_fanout.star` — block batch swapped to 3 real endpoints (`Hello-World`, `Hello-World/branches`, `Hello-World/contributors`)
- `.planning/PROJECT.md` — Validated bullet appended documenting the four-fix stack

## Decisions Made

See `key-decisions` in frontmatter for the full machine-readable list. Highlights:

- **Sentinel + activity-side classification** mirrors the established `ErrUnknownCredential` pattern verbatim — extensions cannot import `go.temporal.io/sdk/temporal`, so a typed `*ApplicationError` is impossible at the call site; the sentinel + classifier-side recognition is the canonical Phase 2 pattern, extended here to `OperationFunc` errors.
- **4xx vs 5xx split is intentional** — 4xx is configuration / wrong path / wrong auth (no retry helps); 5xx is transient backend (Temporal RetryPolicy should fire). The split is pinned by `TestExtension_Get_500_Retryable.require.False(t, errors.Is(err, ErrNonRetryable))`.
- **Reflection-based summary in interpreter** is the firewall-preserving choice: pkg/interpreter is a foundation package, pkg/extension/builtin/http is a leaf — interpreter MUST NOT import the leaf. Reflection on `Output.Status` (with JSON fallback for the production round-trip path) is the audited workaround.
- **`extractFirstNonRetryable` helper** keeps the v1 behavior simple (first NonRetryable wins, matches the activity's skip-rest semantics) and is unit-tested at the helper level — the workflow-level integration test then proves the wiring.
- **`progressHandler` schema stability** — Fix C derives failure context from records the renderer already sees (`step_complete` with `status="err"`); the interpreter's slog event schema does NOT change. Future extensions adding new fields don't break the renderer.
- **Subprocess teardown** — `Setpgid` + `syscall.Kill(-pgid, ...)` is the only reliable way to reap a `temporal server start-dev` subtree. Doc comment in the test file explains the choice so future maintainers don't "simplify" it to plain `Process.Kill()`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `extractStatusSummary` empty on production e2e happy path**
- **Found during:** Task 4 (initial e2e happy run)
- **Issue:** Plan wrote `extractStatusSummary` to reflect on `OkResult.Output.Status`. Helper unit tests passed because they constructed `dag.OkResult{Output: fakeStatusOutput{Status: 200}}` with the typed concrete value. But on the production wire, results round-trip through Temporal's JSON DataConverter via `pkg/dag/result_marshal.go`, which decodes `OkResult.Output` as `dag.RawOperationOutput{Bytes: <raw json>}` (the placeholder type for unknown-at-decode-time outputs). `RawOperationOutput` has only `Bytes`, no `Status` field — reflection returned "" and the e2e happy path rendered an empty summary.
- **Fix:** Added a fast-path branch in `extractStatusSummary` that detects `RawOperationOutput`, parses its `Bytes` for a `"status"` key (`json.Unmarshal` into `struct { Status *int }`), and returns `status=N` when present. Reflection path remains for unit tests that bypass the wire.
- **Files modified:** `pkg/interpreter/walk_step.go` (added `encoding/json` import + branch), `pkg/interpreter/walk_step_test.go` (5 new sub-tests pinning the JSON-fallback paths: status present, status absent, empty bytes, invalid json, status not int)
- **Verification:** Re-ran e2e happy — output now contains `status=200` and `status=200` (one per HTTP step). Both ordering guards pass. All 5 new sub-tests + the unit tests for reflection still pass.
- **Committed in:** `bec8862` (Task 4 commit, bundled with corpus update + e2e tests since it surfaced during Task 4 execution).

**2. [Rule 1 - Bug] `TestExtractFirstNonRetryable_FirstWins` used `require.Same` for value-typed errors**
- **Found during:** Task 2 (first GREEN run)
- **Issue:** Test was written with `require.Same(t, errA, got, ...)` which requires both arguments to be pointers; `errImpl` is a string-based value type, so `require.Same` panicked with "Both arguments must be pointers".
- **Fix:** Replaced with `require.NotNil(t, got)` + `require.Equal(t, errA.Error(), got.Error())` + `require.NotEqual(t, errB.Error(), got.Error())` — verifies the first-wins property by comparing error messages, which is the load-bearing assertion.
- **Files modified:** `pkg/interpreter/walk_step_test.go`
- **Verification:** Test passes; first-wins property still pinned (errA wins, errB explicitly NOT in returned error).
- **Committed in:** `b80bd90` (Task 2 commit).

**3. [Rule 3 - Blocking] e2e test package conflict with existing `tests/` package name**
- **Found during:** Task 4 (first compile of `tests/e2e_skytime_run_test.go`)
- **Issue:** Plan specified `package skytime_e2e_test` for the new file. But `tests/differential_test.go` and `tests/firewall_cli_test.go` both use `package firewall_test`. Go forbids two test packages in the same directory.
- **Fix:** Aligned the new file to `package firewall_test`.
- **Files modified:** `tests/e2e_skytime_run_test.go`
- **Verification:** `go vet ./tests/...` clean.
- **Committed in:** `bec8862` (Task 4 commit).

**4. [Rule 1 - Bug] `TestMain` aborted the whole package when `temporal` CLI was absent**
- **Found during:** Task 4 (drafting the e2e file per plan)
- **Issue:** Plan's `TestMain` did `os.Exit(0)` if `exec.LookPath("temporal")` failed. But TestMain runs for ALL tests in the package, including the pre-existing differential + firewall_cli tests which don't need `temporal`. That would skip the entire package on machines without temporal — a regression of the differential CI gate.
- **Fix:** Moved the `LookPath` check INTO `ensureDevServer` (per-test `t.Skip()`); TestMain now only wires the signal handler and the after-suite teardown. Plan's "skip the entire package" intent was to avoid pre-spawning the dev server unnecessarily; the same outcome is achieved by `ensureDevServer`'s `devServerOnce.Do(...)` — only e2e tests trigger the spawn.
- **Files modified:** `tests/e2e_skytime_run_test.go`
- **Verification:** `go test ./tests/...` runs differential + firewall_cli even when this binary's e2e gate is disabled.
- **Committed in:** `bec8862` (Task 4 commit).

**5. [Rule 2 - Critical Functionality] Probe-then-spawn for shared dev server**
- **Found during:** Task 4 (first e2e run — `lsof -i :7233` showed an existing developer-spawned `temporal server start-dev`)
- **Issue:** Plan's `ensureDevServer` unconditionally spawns a new server. If port 7233 is already occupied by a developer's dev server, `Start()` succeeds but the new instance can't bind — the test then hangs in the readiness loop until the 30s deadline, then fails. Worse, when the developer kills the test, both dev servers fight for the port.
- **Fix:** Probe `temporal operator namespace describe default --address 127.0.0.1:7233` first. If it returns `nil` error, an existing server is responsive — reuse it (set `devServerCmd = nil` so teardown is a no-op). Only `Start()` if the probe fails.
- **Files modified:** `tests/e2e_skytime_run_test.go`
- **Verification:** Ran e2e against the existing developer-spawned server at PID 46075 — both tests pass. Subprocess hygiene check post-run (`ps aux | grep temporal start-dev | grep -v 46075`) returns empty.
- **Committed in:** `bec8862` (Task 4 commit).

---

**Total deviations:** 5 auto-fixed (3 Rule 1 bugs, 1 Rule 2 critical functionality, 1 Rule 3 blocking).
**Impact on plan:** All deviations are necessary for correctness or to make the e2e tests usable on a developer workstation. Deviation #1 (RawOperationOutput JSON fallback) is the most material — without it, Fix B-1 would have looked like it worked at unit level but produced empty summaries in production, which is the exact failure mode this entire plan exists to eliminate. Deviation #5 (probe-then-spawn) makes the e2e tests friendly to the typical developer workflow where a dev server is already running. No scope creep.

## Issues Encountered

- **Existing dev server on port 7233 collided with the plan's spawn-unconditionally approach.** Resolved via probe-then-spawn (deviation #5 above). The reuse path is now the production-friendly default.
- **e2e package collision.** Plan said `package skytime_e2e_test`; existing `tests/` files use `package firewall_test`. Aligned (deviation #3).
- **`require.Same` mismatch with value-typed errors.** Caught immediately on first GREEN run and fixed inline (deviation #2).

No CHECKPOINT or human-action required at any point.

## User Setup Required

None — all changes verified via existing local toolchain (Go 1.25, `temporal` CLI 1.7.0, Temporal Server 1.31.0). Network reachability to api.github.com required for the e2e tests; absence triggers `t.Skip` via `requireNetwork` pre-flight.

## Test Verification Pinned

| Gate | Result |
|------|--------|
| `go test ./... -race -count=1 -timeout 5m` | PASS (all packages green; e2e ran against existing dev server) |
| `go vet ./...` | clean |
| `go build ./...` | clean |
| `TestExtension_Get_404_NonRetryable` | PASS — Fix A 4xx branch |
| `TestExtension_Get_500_Retryable` | PASS — Fix A 5xx branch |
| `TestExtension_Get_2xx_StillSuccess` (200/204/299) | PASS — regression guard |
| `TestExtension_Post_422_NonRetryable` | PASS — Fix A POST body-bearing branch |
| `TestIsRetryable_HonorsExtensionErrNonRetryable` (6 sub-cases) | PASS — Fix A activity classifier |
| `TestExecuteBatch_HTTPNonRetryable_Integration` | PASS — Fix A end-to-end through ExecuteBatch |
| `TestExtractStatusSummary_*` (8 sub-tests + 5 RawOperationOutput sub-tests) | PASS — Fix B-1 helper coverage |
| `TestExtractFirstNonRetryable_*` (3 sub-tests, incl. FirstWins for M-3 mid-block coverage) | PASS — Fix B-2 helper coverage |
| `TestWalkStep_NonRetryableResult_FailsWorkflow` | PASS — Fix B-2 integration gate |
| `TestProgress_FlowFailed` | PASS — Fix C primary path |
| `TestProgress_FlowComplete_NoFailure` | PASS — Fix C success branch unchanged |
| `TestProgress_LastErrResetsOnFlowStart` | PASS — Fix C lifecycle |
| `TestProgress_FlowFailed_NoLastErr` | PASS — Fix C defensive placeholder |
| `TestDifferentialCorpus` | PASS — corpus update doesn't break static-vs-dryrun agreement |
| `TestE2E_SkytimeRun_Happy` | PASS — `status=200`, `[skytime] flow complete`, ordering pinned |
| `TestE2E_SkytimeRun_Unhappy` | PASS — `✗`, `HTTP 404`, `[skytime] flow failed`, `*exec.ExitError` |
| Subprocess hygiene (`ps aux \| grep "temporal server start-dev"`) | clean — only pre-existing dev server (PID 46075); test left no orphans |
| Firewall preservation (`grep -r "extension/builtin/http" pkg/interpreter/`) | only docstring mentions; NO actual import |

## Next Phase Readiness

- Quick task self-contained — does not unblock or block any phase advance.
- The `extension.ErrNonRetryable` sentinel pattern is now the established way for any future extension (Phase 5 Starlark E2E mocks, Phase 6 example HTTP/GitHub/Slack extensions) to surface non-retryable failures. The contract is documented in `pkg/extension/error.go` doc comment.
- The `Setpgid + group-kill` subprocess teardown pattern in `tests/e2e_skytime_run_test.go` is reusable for any future e2e test that spawns a long-running CLI (e.g., a future `skytime` daemon mode test).
- The `RawOperationOutput` JSON-fallback pattern in `extractStatusSummary` is documented as a recurring design point — any future interpreter helper that needs to read fields off a typed Output must handle BOTH the unit-test path (typed concrete) AND the production wire path (`RawOperationOutput.Bytes`).

---

## Self-Check: PASSED

Verified all key artifacts exist on disk and all task commits exist in git history:

- pkg/extension/error.go: FOUND
- pkg/extension/error_test.go: FOUND
- pkg/extension/builtin/http/http.go: FOUND (modified)
- pkg/extension/builtin/http/http_test.go: FOUND (modified)
- pkg/activity/execute_batch.go: FOUND (modified)
- pkg/activity/execute_batch_test.go: FOUND (modified)
- pkg/interpreter/walk_step.go: FOUND (modified)
- pkg/interpreter/walk_step_test.go: FOUND (modified)
- pkg/cli/progress.go: FOUND (modified)
- pkg/cli/progress_test.go: FOUND (modified)
- examples/skeleton/simple_check.star: FOUND (modified)
- examples/skeleton/parallel_fanout.star: FOUND (modified)
- tests/e2e_skytime_run_test.go: FOUND
- .planning/PROJECT.md: FOUND (modified)
- Commit f6a0019: FOUND (Task 1)
- Commit b80bd90: FOUND (Task 2)
- Commit 055feea: FOUND (Task 3)
- Commit bec8862: FOUND (Task 4)

---
*Quick task: 260502-onc-auto-fail-http-non-2xx-status-in-summary*
*Completed: 2026-05-02*
