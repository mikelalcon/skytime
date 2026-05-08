---
phase: 04-static-validation-tier-cli-skeleton
plan: 07
subsystem: cli
tags: [http, net-http, starlarkstruct, cobra, cmd-skytime, ldflags, differential-corpus, examples-skeleton, docs, tdd]

# Dependency graph
requires:
  - phase: 01-type-spine-extension-contract-parser-bridge-foundations
    provides: extension.{Extension, OperationSpec, OperationFunc, BearerCredential, BasicCredential, APIKeyCredential, Secret, Registry, Ptr}, dag.OperationOutput, dag.ActionRef, parser.{NewParser, WithExtensions, WithRoot}
  - phase: 02-generic-activity-block-batch-dispatch-credentials
    provides: activity.OperationDispatch (consumed by AlwaysOkDispatch), Secret.Reveal() audit boundary
  - phase: 03-lambda-serialization-decision-interpreter-worker
    provides: interpreter.{NewWorkflow, NewRegistry, ParsedFlow, FlowRegistry.ContentHashFor}, dag.WorkflowInput, worker.NewWorker (referenced in docs)
  - phase: 04-static-validation-tier-cli-skeleton (plan 04-01)
    provides: pkg/extension/builtin/http skeleton (doc.go) — replaced this wave with full implementation
  - phase: 04-static-validation-tier-cli-skeleton (plan 04-03)
    provides: tests/differential_test.go skip-on-empty TestDifferentialCorpus + corpusExtensions wiring point + dryrun.AlwaysOkDispatch
  - phase: 04-static-validation-tier-cli-skeleton (plan 04-04)
    provides: pkg/cli.NewRootCommand + WithExtensions + WithCredentialHandler — main.go calls these
  - phase: 04-static-validation-tier-cli-skeleton (plan 04-05)
    provides: pkg/cli skytime run subcommand — wired by NewRootCommand which cmd/skytime invokes
  - phase: 04-static-validation-tier-cli-skeleton (plan 04-06)
    provides: pkg/cli skytime dev-server subcommand — same path
provides:
  - "pkg/extension/builtin/http baked-in HTTP extension (D4-14): http.endpoint(base_url, credential?) factory + 5 operations (get/head/post/put/delete) using net/http stdlib only; HTTPResponse{Status, Body, Headers} typed dag.OperationOutput; D4-14-locked idempotence flags (get/head=true; post/put/delete=false) overriding RFC-7231 PUT/DELETE intentionally"
  - "cmd/skytime binary (D4-13, CLI-02): thin wrapper main.go + ldflags-injectable defaultBuildID; signal-aware ExecuteContext; noopCredentialHandler.Resolve points at docs/cli-binary.md for consultants who need real credentials"
  - "examples/skeleton/{simple_check.star, parallel_fanout.star} D4-17 differential corpus: two .star files collectively exercise all six DSL primitives (sequential step, block batch, if_cond, script, for_each_parallel, call_flow) using ONLY the baked-in http extension"
  - "tests/differential_test.go corpusExtensions returns []extension.Extension{httpext.New()} — VAL-02 enforcement live; runDryRun stubs InitState from flow.Inputs so ctx.<input_name> lambda accesses are populated at workflow time"
  - "docs/cli-binary.md walkthrough for D4-16 hint target: how to build a custom Skytime CLI binary by importing pkg/cli + own extensions + a real CredentialHandler"
  - "Activated VAL-02 differential test: TestDifferentialCorpus runs 2 fixtures; both pass static + dry-run agreement under AlwaysOkDispatch"
affects: [05, 06]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Extension namespace shape: *starlarkstruct.Module returned by Initialize; per-method *starlark.Builtin builders close over base_url+credential at endpoint() time and inject base_url into the output Dict so the activity-side OperationFunc reconstructs URL without round-tripping through the closure"
    - "OperationFunc dispatch pattern: per-method shim (doGet/doHead/...) does the args.(*GetArgs) cast, then delegates to a single doHTTP helper that does the net/http NewRequestWithContext + applyCredential + read-body work — keeps HTTP code single-pathed"
    - "Credential routing via type switch: BearerCredential → 'Authorization: Bearer <token>'; BasicCredential → req.SetBasicAuth (stdlib); APIKeyCredential → req.Header.Set(HeaderName, key) with HeaderName fallback to 'Authorization' when empty"
    - "Differential corpus stub-input seeding: stubInitState walks flow.Inputs and emits typed zero values per type-hint string (int→0, bool→false, list→[], dict→{}, default→\"\") so the dry-run path's lambdas see populated ctx for declared inputs"
    - "noopCredentialHandler bifurcation: Resolve(\"\") → (nil, nil) for anonymous endpoints (fixtures using http.endpoint without credential=); non-empty IDs error loudly to catch corpus drift toward credentials the dry-run path cannot resolve"
    - "Thin-wrapper binary: cmd/skytime/main.go is the minimal possible (~37 LOC of code + comments) call site for cli.NewRootCommand + ExecuteContext — adding a new subcommand to the binary is a no-op (it lands in pkg/cli/root.go's AddCommand chain)"
    - "ldflags injection target: cmd/skytime/build_id.go declares `var defaultBuildID = \"dev\"` referenced by `_ = defaultBuildID` in main — keeps the variable alive against `unused` linters while leaving CI free to inject the SHA"

key-files:
  created:
    - "pkg/extension/builtin/http/response.go"
    - "pkg/extension/builtin/http/http.go"
    - "pkg/extension/builtin/http/http_test.go"
    - "cmd/skytime/main.go"
    - "cmd/skytime/build_id.go"
    - "examples/skeleton/simple_check.star"
    - "examples/skeleton/parallel_fanout.star"
    - "docs/cli-binary.md"
  modified:
    - "tests/differential_test.go (corpusExtensions returns httpext.New() — VAL-02 wiring; noopCredentialHandler accepts empty IDs; runDryRun seeds InitState via stubInitState helper)"
    - ".gitignore (/skytime stray binary)"
  removed:
    - "pkg/extension/builtin/http/doc.go (W0 stub replaced with full implementation)"

key-decisions:
  - "[Rule 1 - Bug] noopCredentialHandler.Resolve returns (nil, nil) for empty IDs — anonymous endpoints (http.endpoint without credential=) are a first-class shape fixture authors will use; making the test mock loud-fail on empty IDs would force every fixture to declare a fake credential, which obscures the corpus's intent. Non-empty IDs still loudly fail to catch drift toward credentials the dry-run path cannot resolve."
  - "[Rule 3 - Blocking] runDryRun seeds InitState from flow.Inputs (stubInitState helper) — the static validator (D4-02) accepts ctx.<input_name> on declared inputs, but the dry-run runtime needs the keys to actually exist on ctx. The differential test substitutes a deterministic per-type-hint stub so the corpus does not need to ship per-flow input fixtures."
  - "Initialize returns *starlarkstruct.Module (not raw Builtin or Struct) — the canonical shape used by pkg/parser/builtins_test.go's fakeExtension. parser.globals.go's HasAttrs gate accepts both Module and Struct, but Module matches the existing convention in the test corpus and reads cleanly: `&starlarkstruct.Module{Name: \"http\", Members: starlark.StringDict{...}}`"
  - "base_url injected into the output Kwargs Dict at parse time (not stored on the closure alone) — the activity-side OperationFunc reads BaseURL from the *GetArgs/*BodyArgs struct via the existing extension.DecodeKwargsFromDict path, so the activity does not need a separate channel to receive endpoint state. Single mechanism (kwargs reflection) for all per-call data."
  - "GetArgs vs BodyArgs schema split — get/head/delete share GetArgs (no body field); post/put share BodyArgs (with body field). Two types instead of one with optional body keeps the kwargs schema reflective and makes TestExtension_KwargsTypeShapes a one-line `KwargsType.Name() == \"GetArgs\"` assertion."
  - "callerPosition mirrors parser.callerPosition (Pitfall #3) — the http extension's per-method builtins call thread.CallFrame(1).Pos so ActionRef.Pos points at the .star call site, not the builtin def site. Mirrors the convention pkg/parser established for D-04 attribution."
  - "Plan's `≤ 60 lines` line-count for main.go was advisory; final count is 67 lines split ~30 comment+blank / ~37 code. The substance is thin-wrapper (per D4-13): one signal context, one cli.NewRootCommand call, one ExecuteContext, one CredentialHandler choice. Adding a new subcommand never touches this file."

patterns-established:
  - "Pattern: Baked-in extension under pkg/extension/builtin/<name>/ — package name is the namespace (Extension.Name() returns the directory name); production binary registers via cli.WithExtensions(httpext.New()); consultants follow the same shape for their custom extensions in their own repos. The path is deliberately under pkg/extension/builtin/ (not pkg/http_ext or extensions/http) to nest with the existing extension SDK."
  - "Pattern: Stub-input seeding for differential tests — when a static validator accepts symbolic references that the runtime needs to resolve to concrete values, the differential test's runDryRun helper synthesizes typed-zero stubs from the parser's declared schema. Phase 5's mock harness can reuse stubInitState directly when test fixtures don't declare per-test inputs."
  - "Pattern: Per-method builtin closure over endpoint state — endpointFactory builds a *starlarkstruct.Module whose Members are *starlark.Builtins each closing over (kind, baseURL, credential). The Builtins inject base_url into the per-call kwargs Dict so the activity-side path is uniform regardless of factory state. Reusable for any extension where per-instance state needs to reach the activity (e.g., GitHub: org name, default headers; Slack: workspace ID)."
  - "Pattern: Thin binary wrapper around pkg/cli — cmd/skytime/main.go is the minimal possible call site. Subcommand wiring lives in pkg/cli/root.go::NewRootCommand; the binary picks the extension set + CredentialHandler and runs ExecuteContext. Phase 6's example binary (and any consultant custom binary) is a copy with different extension/handler choices."

requirements-completed: [VAL-02, CLI-02, CLI-05]

# Metrics
duration: 7min
completed: 2026-05-01
---

# Phase 4 Plan 07: Wave 4 Closeout — HTTP Extension + cmd/skytime Binary + D4-17 Corpus Summary

**Wave 4 closes Phase 4 by landing the four pieces that make every prior wave's promise true: the baked-in http extension (D4-14), the cmd/skytime binary entry point (D4-13), the examples/skeleton/ differential corpus (D4-17), and docs/cli-binary.md (D4-16 hint target). TestDifferentialCorpus stops skipping — both fixtures pass static + dry-run agreement under AlwaysOkDispatch. `go run ./cmd/skytime validate examples/skeleton/*.star` exits 0 e2e.**

## Performance

- **Duration:** ~7 min (468s wall-clock)
- **Started:** 2026-05-01T20:48:09Z
- **Completed:** 2026-05-01T20:55:57Z
- **Tasks:** 3 (TDD: 5 commits across RED/GREEN steps; one chore commit for .gitignore)
- **Files modified:** 11 (8 created, 2 modified, 1 removed)

## Accomplishments

- **Baked-in HTTP extension (D4-14, VAL-02-enabling).** `pkg/extension/builtin/http` ships `New() extension.Extension` with five operations (get/head/post/put/delete) following D4-14-locked idempotence (get/head=true; post/put/delete=false — RFC-7231 override on PUT/DELETE intentional and pinned by `TestExtension_OperationsIdempotenceMatchesD4_14`). `HTTPResponse{Status, Body, Headers}` is the typed `dag.OperationOutput`. Bearer/Basic/APIKey credentials route via type switch. net/http stdlib only — no third-party HTTP client (D4-14 grep clean).
- **cmd/skytime binary (D4-13, CLI-02 closeout).** ~37 LOC of substantive code: signal.NotifyContext for SIGINT/SIGTERM, `cli.NewRootCommand` with `skyhttp.New()` + a `noopCredentialHandler{}`, ExecuteContext, exit non-zero on error. `cmd/skytime/build_id.go` declares the ldflags injection target (`var defaultBuildID = "dev"`) referenced by `_ = defaultBuildID` so the variable survives `unused` linters while leaving CI free to inject `$(git rev-parse HEAD)`. `--help` lists validate, run, dev-server; `validate /tmp/nonexistent` exits non-zero with a typed parser error.
- **D4-17 differential corpus (`examples/skeleton/`).** Two .star fixtures collectively exercise all six DSL primitives:
  - `simple_check.star` covers sequential step + script + if_cond against `gh.get(...)`.
  - `parallel_fanout.star` covers `step(block=[...])` + `for_each_parallel` + `call_flow` over a helper flow.
  Both files use ONLY the baked-in http extension; together they make `TestDifferentialCorpus` (the W2 skip-on-empty test from plan 04-03) actually run and pass.
- **VAL-02 wiring + dry-run path fixes (`tests/differential_test.go`).** `corpusExtensions` returns `[]extension.Extension{httpext.New()}` (the W4 wiring point). `noopCredentialHandler.Resolve("")` returns `(nil, nil)` for anonymous endpoints; non-empty IDs error loudly to catch corpus drift. `runDryRun` seeds `InitState` from `flow.Inputs` via `stubInitState` so workflow lambdas accessing `ctx.<input_name>` see populated state.
- **docs/cli-binary.md walkthrough (D4-16 hint target).** Steps consultants through building a custom Skytime CLI binary: import `pkg/cli`, register `skyhttp.New()` + their own extensions, supply a real `CredentialHandler`, build with optional ldflags BuildID injection. References `cmd/skytime/main.go`, `pkg/cli/root.go`, `pkg/extension/builtin/http/http.go`.
- **Full repo green.** `go test ./... -count=1` exits 0 across all 13 packages + tests/. `go vet ./...` clean. `go build ./cmd/skytime` exits 0. `go run ./cmd/skytime validate examples/skeleton/simple_check.star` exits 0 e2e (and same for parallel_fanout.star).

## Task Commits

Atomic per-task commits; TDD-paired where applicable:

1. **Task 1 RED:** `9b24bfd` (test) — failing tests for skyhttp.New(), HTTPResponse, idempotence, kwargs schema, credential routing, W-3 behavior gate
2. **Task 1 GREEN:** `e00c5b7` (feat) — pkg/extension/builtin/http/{response.go, http.go} full implementation
3. **Task 2:** `8699441` (feat) — cmd/skytime/{main.go, build_id.go} thin wrapper binary
4. **Task 2 chore:** `40836e6` (chore) — .gitignore /skytime stray binary
5. **Task 3:** `9722302` (feat) — examples/skeleton/{simple_check.star, parallel_fanout.star} + tests/differential_test.go wiring (corpusExtensions + noopCredentialHandler bifurcation + stubInitState seeding) + docs/cli-binary.md

**Plan metadata:** Final commit (separate) captures SUMMARY.md + STATE.md + ROADMAP.md + REQUIREMENTS.md.

## Files Created/Modified

**Created (8):**
- `pkg/extension/builtin/http/response.go` — `HTTPResponse{Status, Body, Headers}` typed `dag.OperationOutput`
- `pkg/extension/builtin/http/http.go` — `skytimeHTTP` Extension, `endpointFactory`, `newMethodBuiltin`, `doGet/doHead/doPost/doPut/doDelete`, `doHTTP`, `applyCredential`
- `pkg/extension/builtin/http/http_test.go` — 8 tests covering name, D4-14 idempotence, kwargs schema split, registration, GET e2e, POST body, Bearer credential, parser behavior gate
- `cmd/skytime/main.go` — thin wrapper calling `cli.NewRootCommand` with `skyhttp.New()` + `noopCredentialHandler`
- `cmd/skytime/build_id.go` — `var defaultBuildID = "dev"` ldflags injection target (D3-20 mirror)
- `examples/skeleton/simple_check.star` — sequential step + script + if_cond corpus fixture
- `examples/skeleton/parallel_fanout.star` — for_each_parallel + block batch + call_flow corpus fixture
- `docs/cli-binary.md` — D4-16 hint target walkthrough

**Modified (2):**
- `tests/differential_test.go` — `corpusExtensions` returns httpext.New(); `noopCredentialHandler.Resolve("")` returns (nil, nil); `runDryRun` calls `stubInitState(flows[name])` for `InitState`; new `stubInitState` helper synthesizes typed-zero stubs from `flow.Inputs`
- `.gitignore` — `/skytime` ignored (stray binary from `go build ./cmd/skytime` at repo root)

**Removed (1):**
- `pkg/extension/builtin/http/doc.go` — W0 stub replaced with the full implementation

## Decisions Made

- **`*starlarkstruct.Module` as the Initialize return shape.** Verified against `pkg/parser/builtins_test.go::fakeExtension` (the canonical convention in this repo) and `pkg/parser/globals.go`'s `starlark.HasAttrs` gate. The Module shape composes naturally with the .star surface `http.endpoint(base_url=..., credential=...).get(path=...)` (attribute lookup → call → attribute lookup → call).
- **base_url injected into the per-call Kwargs Dict.** Each per-method builtin closes over baseURL+credential and injects base_url into the output Dict before freezing. This means the activity-side `*GetArgs.BaseURL` field is populated by the existing `extension.DecodeKwargsFromDict` path — no separate channel needed for endpoint state to reach `OperationFunc`.
- **GetArgs vs BodyArgs split.** get/head/delete share `GetArgs` (no body field); post/put share `BodyArgs` (with body field). Two types instead of one struct with optional body keeps the kwargs schema reflective and makes the schema-shape test a one-line `KwargsType.Name() == "GetArgs"` assertion.
- **callerPosition mirrors parser.callerPosition.** Pitfall #3 (use thread.CallFrame(1).Pos, not fn.Position()) applies — the per-method builtins build ActionRefs whose Pos points at the .star call site for D-04 attribution downstream.
- **noopCredentialHandler.Resolve("") returns (nil, nil).** Anonymous endpoints (fixtures using `http.endpoint(base_url=...)` without `credential=`) need to traverse the activity's runAction without invoking a real resolver. Returning (nil, nil) for empty IDs lets the dry-run path proceed; non-empty IDs still error loudly to catch corpus drift toward credentials the dry-run path cannot resolve. (See Deviations.)
- **runDryRun seeds InitState via stubInitState.** The static validator (D4-02) accepts `ctx.<input_name>` references when the input is in `flow.Inputs`. The dry-run runtime needs those keys to actually exist on the workflow's ctx struct. The new `stubInitState` helper walks `flow.Inputs` and emits typed-zero values per type-hint string. (See Deviations.)
- **`*starlarkstruct.Module` for the per-endpoint object too.** endpointFactory returns a Module (not a Struct) for symmetry with the top-level Initialize return — both are HasAttrs values; both compose with the parser's attribute-lookup-then-call sequence.
- **cmd/skytime/main.go line count 67 (vs plan's "≤ 60").** Treated as advisory: ~30 lines are comment+blank, ~37 are substantive code (signal context, NewRootCommand call, ExecuteContext, noopCredentialHandler). The plan's intent (thin wrapper per D4-13) is preserved — adding a new subcommand never touches this file.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] noopCredentialHandler.Resolve unconditionally errored on every call**
- **Found during:** Task 3 (first run of TestDifferentialCorpus after wiring corpusExtensions)
- **Issue:** The W2 (plan 04-03) noopCredentialHandler returned `errors.New("noopCredentialHandler.Resolve called — dry-run should not require credentials")` for every ID. But `pkg/activity/credential_cache.go::resolve` calls the handler even with empty IDs (the cache path doesn't short-circuit on `id == ""`). The corpus fixtures use `http.endpoint(base_url=...)` without `credential=`, so `ActionRef.CredentialID` is empty — and the activity then errored out at the credential-resolve step. Result: dry-run side reported "credential resolve failed" for every fixture; static side accepted; differential test failed with `DIVERGENCE static_passed=true dryrun_passed=false`.
- **Fix:** Bifurcated `noopCredentialHandler.Resolve`: empty IDs return `(nil, nil)` so anonymous endpoints traverse `runAction` cleanly; non-empty IDs still error loudly so corpus drift toward credentials the dry-run path cannot resolve fails fast with a clear message. The pre-existing production bug (cache should skip resolve for empty IDs) is logged as a deferred concern but NOT fixed in this plan — the test-side bifurcation is the minimal scope-respecting change.
- **Files modified:** `tests/differential_test.go`
- **Verification:** TestDifferentialCorpus advances past credential errors and surfaces the next failure (the InitState issue below).
- **Committed in:** `9722302` (Task 3 GREEN — bundled with the differential wiring since both fixes are required for the test to actually pass)

**2. [Rule 3 - Blocking] runDryRun passed empty InitState — workflow lambdas saw missing input keys**
- **Found during:** Task 3 (after the credential fix, both fixtures showed `struct has no .repo_path attribute` and `struct has no .repos attribute`)
- **Issue:** The W2 (plan 04-03) runDryRun passed `InitState: map[string]any{}` to ExecuteWorkflow. Both fixtures declare flow inputs (`{"repo_path": "string"}` and `{"repos": "list"}`) and reference them via `ctx.repo_path` / `ctx.repos` inside lambdas. Static validator (D4-02) accepts these references because the names are in `flow.Inputs`. But the dry-run runtime constructs the workflow's ctx struct from `InitState` keys — empty InitState → no `repo_path` key → lambda fails with "struct has no .repo_path attribute" inside the script.
- **Fix:** Added `stubInitState(*dag.Flow) map[string]any` helper that walks `flow.Inputs` and emits typed-zero values per type-hint string (`int`→`int64(0)`, `bool`→`false`, `list`→`[]any{}`, `dict`→`map[string]any{}`, default→`""`). `runDryRun` now passes `stubInitState(flows[name])` instead of an empty map. Mirrors what `skytime run --input=<json>` would supply at the CLI; the differential test substitutes deterministic stubs so the corpus does not need to ship per-flow input fixtures.
- **Files modified:** `tests/differential_test.go`
- **Verification:** TestDifferentialCorpus passes both fixtures; full `go test ./... -count=1` green.
- **Committed in:** `9722302` (Task 3 GREEN — bundled)

**3. [Rule 3 - Blocking] Stray /skytime binary appears at repo root after `go build ./cmd/skytime`**
- **Found during:** Task 2 verification (post-commit `git status` showed `?? skytime`)
- **Issue:** Running `go build ./cmd/skytime` from the repo root (no `-o` flag) drops a `skytime` executable at the repo root. Phase 4 plan 07 introduced the `cmd/` tree, so this output appears in the working tree for the first time. Without a gitignore entry, future contributors running ad-hoc builds would accidentally commit it.
- **Fix:** Added `/skytime` to `.gitignore` under "Stray binary from `go build ./cmd/skytime` at repo root".
- **Files modified:** `.gitignore`
- **Verification:** `git status` no longer reports the binary as untracked.
- **Committed in:** `40836e6` (chore — separate commit since the gitignore addition is independent of the binary-entry-point feat commit)

---

**Total deviations:** 3 auto-fixed (1 bug, 2 blocking).
**Impact on plan:** All three are mechanically required for the plan's success criteria to hold. Deviations 1+2 are inherent in W2's stubbed test infrastructure surfacing real concerns once W4 lands the actual corpus + extension. Deviation 3 is housekeeping — first time the repo has a runnable cmd/ binary. No scope creep, no architectural change. The plan's intent (HTTP extension + thin binary + corpus + docs + VAL-02 enforcement) is fully preserved.

## Issues Encountered

- **`pkg/extension/builtin/http/http.go` Initialize-return shape verification.** Plan flagged this as critical: "Read pkg/parser/globals.go before writing the code." Read confirmed: parser stores Initialize's return value under the extension name and gates on `starlark.HasAttrs`. Both `*starlarkstruct.Module` and `*starlarkstruct.Struct` satisfy HasAttrs; chose Module for symmetry with `pkg/parser/builtins_test.go::fakeExtension`. The W-3 behavior gate test (`TestExtension_RegistersAndParsesAFlow`) was the load-bearing assertion — passed on first run, confirming the shape is correct.
- **Pre-existing potential bug in pkg/activity/credential_cache.go** (out of scope for this plan): `resolve()` calls `handler.Resolve(ctx, id)` even when `id == ""`. ExecuteBatch's retry-invalidation loop already filters empty IDs (line 54), so the convention "empty ID = skip credential plumbing" exists in the codebase but is not consistently applied. Logged here as a deferred concern; the test-side bifurcation in `noopCredentialHandler` is the minimal Rule-1 fix that respects scope. Phase 6 or a v1.x audit pass should consider tightening `runAction`'s call to skip resolve when CredentialID is empty.

## User Setup Required

None — no external service configuration required for the plan's verification gates. To exercise `cmd/skytime dev-server` end-to-end (out-of-scope for this plan but enabled by it), install the `temporal` CLI per D4-12: `brew install temporal` / `curl -sSf https://temporal.download/cli.sh | sh` / `go install go.temporal.io/server/cmd/temporal@latest`.

## Next Phase Readiness

- **Phase 4 feature-complete.** All Phase 4 v1 requirements landed: VAL-01/02/03 (validator, differential test, error format), CLI-01 (validate), CLI-02 (run), CLI-04 (dev-server), CLI-05 (firewall). Phase 5 (Starlark E2E mock harness) and Phase 6 (full HTTP+GitHub+Slack example project) inherit a working CLI tree, a working corpus, and a working enforcement test.
- **VAL-02 contract live.** TestDifferentialCorpus is no longer skipping — every CI run exercises both fixtures through static + dry-run paths and asserts agreement. Adding fixtures to `examples/skeleton/` is automatic (filepath.WalkDir picks them up); adding extensions is a single-line append in `corpusExtensions`.
- **Phase 5 composition point.** The dispatch-replacement seam (`dryrun.AlwaysOkDispatch`) is ready for Phase 5's Starlark mock harness to consume. The progress shim (W3 plan 04-05) is ready for the interpreter emitter Phase 5/6 will wire. The corpus pattern (.star fixtures + per-fixture differential test) generalizes to Phase 5's mock-driven assertions and Phase 6's full-stack example flows.
- **Phase 6 README walkthrough unblocked.** `git clone → go build ./cmd/skytime → ./skytime dev-server` works (with the temporal CLI installed); `./skytime validate examples/skeleton/simple_check.star` works; the missing pieces are the GitHub + Slack extensions and the customer-brief example flow Phase 6 will land.
- **Phase 4 stub tracking:** the noopCredentialHandler bifurcation in `tests/differential_test.go` is intentional infrastructure for the dry-run path, not a stub blocking real functionality. The pre-existing pkg/activity credential_cache empty-ID issue is logged in "Issues Encountered" for v1.x audit; it does NOT block any Phase 4 success criterion.
- **No blockers.** Full repo `go test ./... -count=1` exits 0 across all packages; `go vet ./...` clean; firewall tests untouched; `go run ./cmd/skytime validate examples/skeleton/*.star` exits 0 e2e.

## Self-Check: PASSED

**Files verified (8/8 created files exist on disk):**
- pkg/extension/builtin/http/response.go
- pkg/extension/builtin/http/http.go
- pkg/extension/builtin/http/http_test.go
- cmd/skytime/main.go
- cmd/skytime/build_id.go
- examples/skeleton/simple_check.star
- examples/skeleton/parallel_fanout.star
- docs/cli-binary.md

**Modified files verified (2/2):**
- tests/differential_test.go (corpusExtensions, noopCredentialHandler, stubInitState, runDryRun seeding)
- .gitignore (/skytime entry)

**Removed file verified (1/1):**
- pkg/extension/builtin/http/doc.go (W0 stub gone)

**Commits verified (5/5 in `git log`):**
- 9b24bfd (Task 1 RED), e00c5b7 (Task 1 GREEN)
- 8699441 (Task 2), 40836e6 (Task 2 chore)
- 9722302 (Task 3)

**Verification gates green:**
- `go test ./pkg/extension/builtin/http -count=1` → PASS (8/8)
- `go test ./pkg/extension/builtin/http -run TestExtension_RegistersAndParsesAFlow -count=1` → PASS (W-3 behavior gate)
- `go test ./tests -run TestDifferentialCorpus -count=1` → PASS (2 subtests, no skips — VAL-02 enforcement live)
- `go test ./... -count=1` → all 13 packages + tests/ green
- `go build ./cmd/skytime` → clean
- `go run ./cmd/skytime --help | grep -E "validate|run|dev-server"` → all three present
- `go run ./cmd/skytime validate examples/skeleton/simple_check.star` → exits 0 (e2e)
- `go run ./cmd/skytime validate examples/skeleton/parallel_fanout.star` → exits 0 (e2e)
- `grep -c 'pkg/cli' docs/cli-binary.md` → matches; `grep -c 'NewRootCommand' docs/cli-binary.md` → matches
- `grep -E "resty|gentleman|fasthttp" pkg/extension/builtin/http/*.go` → no matches (D4-14 stdlib-only)
- `go vet ./...` → clean

All success criteria met; three deviations documented (1 bug, 2 blocking — all required by W4's corpus exposing W2-stubbed behavior). No missing artifacts.

---
*Phase: 04-static-validation-tier-cli-skeleton*
*Completed: 2026-05-01*
