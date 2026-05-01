---
phase: 04-static-validation-tier-cli-skeleton
plan: 03
subsystem: validator
tags: [validator, facade, dryrun, dispatch-mock, differential-corpus, testsuite, firewall, tdd]

# Dependency graph
requires:
  - phase: 01-type-spine-extension-contract-parser-bridge-foundations
    provides: pkg/parser.Parser, pkg/dag.{ParseError, ValidationError}, pkg/extension.{Extension, OperationSpec, CredentialHandler, Credential}, pkg/activity.OperationDispatch
  - phase: 03-lambda-serialization-decision-interpreter-worker
    provides: interpreter.NewWorkflow + FlowRegistry, dag.WorkflowInput, Parser.{FileBytes(), Lambdas(), Flows()} accessors
  - phase: 04-static-validation-tier-cli-skeleton (plan 01)
    provides: pkg/validator package skeleton (doc.go), tests/firewall_cli_test.go with findModuleRootCLI helper
  - phase: 04-static-validation-tier-cli-skeleton (plan 02)
    provides: validateLambdaCtxAccesses + validateActionRefKwargs in parser.finalize chain (the static-validation logic the facade exposes)
provides:
  - "validator.Validate(file, opts ...Option) []error — thin façade calling parser.NewParser + ParseFile, returning typed errors as a slice (empty non-nil on success, one-element on failure)"
  - "validator.{WithExtensions, WithCredentialHandler, WithRoot} functional options forwarded to the parser (CredentialHandler stored for API symmetry; never invoked by validate)"
  - "pkg/validator/dryrun.AlwaysOkDispatch — test-only mock OperationDispatch that wraps every registered op's Func with one returning (nil, nil), preserving Name/Idempotent/KwargsType/DefaultTimeout for schema check fall-through"
  - "tests/differential_test.go — VAL-02 differential corpus test walking examples/skeleton/, asserting static + dry-run agree on accept/reject; t.Skip()s cleanly until W4 (plan 04-07) populates the corpus"
  - "Pitfall #7 mitigation: assertNotRuntimePanic helper rejects runtime.Error in the failure chain on either side"
affects: [04-04, 04-05, 04-06, 04-07, 05, 06]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Thin-facade-over-parser: validator.Validate is the single CLI-facing entry point; all validation logic stays in pkg/parser/finalize.go (D4-01)"
    - "Functional-option API symmetry: validator.{WithExtensions, WithCredentialHandler, WithRoot} mirror parser/cli surfaces so pkg/cli can pass the same options to validate + worker constructors uniformly"
    - "Dispatch-replacement mock seam: dryrun.AlwaysOkDispatch shallow-copies OperationSpec and replaces only Func — Phase 5's Starlark mock harness will reuse the same shape with a different mock body"
    - "Skip-on-empty differential test: TestDifferentialCorpus t.Skip()s when examples/skeleton/ doesn't exist OR contains no .star files; lets verification infrastructure land in W2 before W4 ships fixtures"
    - "Cross-tree test placement to bypass firewalls: tests/differential_test.go lives outside pkg/* so the temporal-firewall in pkg/activity/firewall_test.go (which gates only pkg/*) doesn't block its testsuite imports"
    - "Shared external test package: differential_test.go and firewall_cli_test.go both declare `package firewall_test` so findModuleRootCLI is shared without duplication"

key-files:
  created:
    - "pkg/validator/validator.go"
    - "pkg/validator/options.go"
    - "pkg/validator/validator_test.go"
    - "pkg/validator/dryrun/dispatch.go"
    - "pkg/validator/dryrun/dispatch_test.go"
    - "pkg/validator/dryrun/doc.go"
    - "tests/differential_test.go"
  modified:
    - "pkg/validator/doc.go (Wave 0 stub doc updated to reflect Wave 2 surface that landed)"

key-decisions:
  - "[Rule 3 - Blocking] Moved dryrun out of internal/: plan placed it at pkg/validator/internal/dryrun/ but Go's internal-package rule blocks tests/differential_test.go (outside pkg/validator/) from importing internal-rooted packages. Final path is pkg/validator/dryrun/. The 'test-only' guarantee is now social rather than syntactic — production code MUST NOT import dryrun.AlwaysOkDispatch. doc.go documents the rationale."
  - "Empty (non-nil) []error on Validate success: the empty slice signals 'no errors' unambiguously to callers iterating len(errs) without nil-checking — chosen over `nil` so future multi-error reporting can append without callers needing to switch to make([]error, 0)."
  - "WithCredentialHandler accepted but unused by validate: validator.Validate is parse-only and never invokes the resolver. Storing the handler on config keeps the option signature uniform with pkg/cli (which plumbs the same option to both validate and worker constructors). Future enhancement: warn if WithCredentialHandler is set but no extensions reference credentials."
  - "Single shared parser.WithExtensions call (not one per ext): validator.Validate calls parser.WithExtensions(cfg.exts...) once with the variadic, instead of looping `for _, e := range cfg.exts { append(parserOpts, parser.WithExtensions(e)) }` per the plan's draft. parser.WithExtensions accepts variadic Extension already (verified in pkg/parser/options.go), so the loop is redundant; the single call is more idiomatic."
  - "Differential test placed at tests/ (not pkg/validator/dryrun_test.go): the temporal-firewall in pkg/activity/firewall_test.go gates pkg/* — testsuite imports under pkg/ would trip it. tests/ is outside the gate; firewall_cli_test.go already lives there. Same pattern as the plan's Action step 1 documents."
  - "Skip-on-empty corpus is REQUIRED behavior, not optional: the plan explicitly schedules W2 (this plan) ahead of W4 (corpus + HTTP extension). Without skip-on-empty, this plan's CI would fail until W4 lands. Verified by running the test today and observing --- SKIP."
  - "fakeExt.doit's real Func panics if called: TestAlwaysOkDispatch's fakeExt declares its real Func with `panic('should NOT be called')` — proves the dispatch's wrapped Func IS the new okFunc and not the original. Stronger than asserting (nil, nil) alone."

patterns-established:
  - "Pattern: Façade-package-with-functional-options (validator.Validate). Thin wrappers in front of subsystem packages (parser, worker) that expose a CLI-friendly API while keeping the subsystem package free of CLI/UI concerns. Pkg/cli W3 will call validator.Validate the same way."
  - "Pattern: Dispatch-replacement test seam. AlwaysOkDispatch demonstrates how to mock the activity layer without standing up real backends — preserve schema, replace I/O. Phase 5's Starlark mock harness inherits the shape; a future Phase 5 'PreFlightDispatch' (recorded responses) can use the same shape."
  - "Pattern: Skip-on-empty for verification-infrastructure-before-fixtures. When verification lands ahead of the data it verifies (W2 differential test ships before W4 corpus), t.Skip() with an actionable 'when this populates, the test runs' message lets CI stay green without disabling the test."
  - "Pattern: Cross-tree integration tests at tests/. tests/ is the right home for tests that need imports a per-pkg firewall would block. Both differential_test.go (temporal SDK) and firewall_cli_test.go (cobra/charm-log) belong here; future Phase 4 W3+ may add cli_integration_test.go."

requirements-completed: [VAL-02]

# Metrics
duration: 7min
completed: 2026-05-01
---

# Phase 4 Plan 03: pkg/validator Façade + Dry-Run Dispatch Seam Summary

**Wave 2 ships the `validator.Validate` thin façade, the `dryrun.AlwaysOkDispatch` mock OperationDispatch, and the load-bearing `TestDifferentialCorpus` (VAL-02) — the differential test skips cleanly until W4 (plan 04-07) populates `examples/skeleton/`, then enforces "static and runtime parser agree on accept/reject" on every CI run.**

## Performance

- **Duration:** ~7 min (427s wall-clock)
- **Started:** 2026-05-01T20:10:24Z
- **Completed:** 2026-05-01T20:17:31Z
- **Tasks:** 3 (all TDD: 5 commits — 2 RED + 2 GREEN + 1 refactor for Rule 3 deviation)
- **Files modified:** 8 (7 created, 1 modified — doc.go updated to reflect landed surface)

## Accomplishments

- **`validator.Validate(file, opts...) []error` thin façade.** Calls parser.NewParser + ParseFile and returns the parser's typed errors as a slice. Empty (non-nil) slice on success; one-element slice on failure (parser construction or parse-time error). NO new validation logic — every check stays in pkg/parser/finalize.go per D4-01. Three functional options forward to the parser: `WithExtensions`, `WithCredentialHandler` (stored but unused — API symmetry), `WithRoot`.
- **`dryrun.AlwaysOkDispatch(exts) activity.OperationDispatch`.** Walks the extension list, shallow-copies each registered OperationSpec, and replaces only `Func` with one returning `(nil, nil)`. `Name`, `Idempotent`, `KwargsType`, `DefaultTimeout` survive verbatim so the activity layer's own kwarg-shape checks (`extension.DecodeKwargsFromDict`) still fire on bad inputs. `okFunc` is a package-level closure-free function — no allocation per registered op.
- **`tests/differential_test.go::TestDifferentialCorpus`.** Walks `examples/skeleton/` for `.star` files; for each file runs static `validator.Validate` + dry-run `interpreter.NewWorkflow` against `testsuite.TestWorkflowEnvironment` with `AlwaysOkDispatch`, and asserts they agree on accept/reject. Skip-on-empty when the directory doesn't exist OR has no `.star` files (W4 will populate via plan 04-07). Pitfall #7 mitigation: when both paths fail, asserts neither error chain contains a `runtime.Error`.
- **Rule 3 deviation: `internal/dryrun` → `dryrun`.** Go's internal-package rule blocked `tests/differential_test.go` from importing `pkg/validator/internal/dryrun`. Moved the package one level up. Documented the social-vs-syntactic seam in dryrun/doc.go.
- **All firewalls hold.** `TestNoCobraImportsOutsideAllowList` (cli) and `TestNoTemporalImportsOutsideAllowList` (activity-side) both clean. The new `tests/differential_test.go` imports `go.temporal.io/sdk/{testsuite,workflow,activity}` from outside `pkg/`, which the activity firewall correctly does not gate.
- **Full `go test ./...` green; `go vet ./...` clean.** No regressions across pkg/{activity, bridge, dag, extension, interpreter, parser, validator, validator/dryrun, worker} or tests.

## Task Commits

Each task TDD-paired (test → feat); a refactor commit captures the Rule 3 deviation:

1. **Task 1 RED: validator facade failing tests** — `3c36385` (test)
2. **Task 1 GREEN: validator.Validate + functional options** — `b19905e` (feat)
3. **Task 2 RED: dryrun.AlwaysOkDispatch failing tests** — `4cd0d77` (test)
4. **Task 2 GREEN: AlwaysOkDispatch implementation** — `9d29f6a` (feat)
5. **Rule 3 deviation: move dryrun out of internal/** — `d91d54e` (refactor)
6. **Task 3: TestDifferentialCorpus** — `9919050` (feat)

**Plan metadata:** Final commit (this commit) captures SUMMARY.md, STATE.md, ROADMAP.md, REQUIREMENTS.md.

## Files Created/Modified

**Created:**
- `pkg/validator/validator.go` — `Validate(file string, opts ...Option) []error` (only func in the file). Empty slice on success, one-element on failure.
- `pkg/validator/options.go` — `Option func(*config) error` + `WithExtensions`, `WithCredentialHandler`, `WithRoot`. Internal `config` struct accumulates option state for parser construction.
- `pkg/validator/validator_test.go` — 3 tests: `TestValidate_ReturnsTypedErrors` (bad .star → typed error), `TestValidate_HappyPathReturnsEmpty` (asserts non-nil empty slice via require.Empty + require.NotNil), `TestValidate_NonexistentFile` (parser surfaces *dag.ParseError on read failure).
- `pkg/validator/dryrun/dispatch.go` — `AlwaysOkDispatch(exts []extension.Extension) activity.OperationDispatch` + private `okFunc`. Shallow-copies OperationSpec; replaces only Func.
- `pkg/validator/dryrun/dispatch_test.go` — 2 tests: `TestAlwaysOkDispatch` (preservation invariants + (nil, nil) return), `TestAlwaysOkDispatch_MultipleExtensions` (key shape "<extName>.<opName>"; no collision). Uses `fakeExt` with a panic-on-call real Func to prove the wrap actually happened.
- `pkg/validator/dryrun/doc.go` — Package doc explaining test-only purpose and the move out of `internal/`.
- `tests/differential_test.go` — `TestDifferentialCorpus` (skip-on-empty differential test) + `runDryRun` helper + `assertNotRuntimePanic` helper + `noopCredentialHandler` (returns explicit "should-not-be-called" error). Lives in `package firewall_test` to share `findModuleRootCLI` with the sibling firewall test.

**Modified:**
- `pkg/validator/doc.go` — Updated to reflect Wave 2 surface that landed (Validate + dryrun mock); previously stubbed as "Wave 0 placeholder, Validate lands in Wave 2".

## Decisions Made

- **Move from `internal/dryrun` to `dryrun` (Rule 3 deviation):** see Deviations section. The plan's `internal/` placement was incompatible with the cross-tree test placement at `tests/`. Resolved by moving up one level; the test-only guarantee is now social.
- **Empty (non-nil) []error on success:** chosen over `nil` so the empty slice signals "no errors" unambiguously and future multi-error reporting can append without forcing callers to switch from `nil`-check to `make([]error, 0)`. Asserted by `TestValidate_HappyPathReturnsEmpty` via `require.NotNil + require.Empty`.
- **`WithCredentialHandler` stored but never invoked:** validate is parse-only — credentials never resolve at parse time. The option exists for API symmetry with `pkg/cli`'s root command, which will plumb the same option down to both `validator.Validate` and `worker.NewWorker`. A future enhancement could warn if WithCredentialHandler is supplied but no extensions reference credentials.
- **`parser.WithExtensions(cfg.exts...)` single call (not loop-per-ext):** the plan's draft had `for _, e := range cfg.exts { append(parserOpts, parser.WithExtensions(e)) }` — but `parser.WithExtensions` is already variadic. Single variadic call is more idiomatic; behavior identical.
- **Differential test in `package firewall_test` (not `tests_test`):** the existing `tests/firewall_cli_test.go` declares `package firewall_test`. Using the same package name lets `differential_test.go` reuse `findModuleRootCLI` without duplicating the helper. The plan called for `tests_test` but the existing sibling forced the choice.
- **`fakeExt.doit` real Func panics on call:** the test fixture's real Func declares `panic("should NOT be called")` to prove `AlwaysOkDispatch.Func` is the wrapped `okFunc` and not the original. Stronger than asserting `(nil, nil)` alone — if the wrap accidentally preserved the original Func, the test fails fast with a clear stack trace.
- **`noopCredentialHandler.Resolve` returns explicit error:** the dry-run path never reaches the credential resolver (`okFunc` ignores cred), but `activity.New` requires a non-nil handler. Returning `errors.New("noopCredentialHandler.Resolve called — dry-run should not require credentials")` makes any unintended call surface loudly rather than masquerading as a quiet `nil, nil`.
- **Skip-on-empty matches W2-before-W4 schedule:** the plan's wave structure puts the differential infrastructure (W2) ahead of the corpus (W4). Skip-on-empty is the load-bearing mechanism that makes this temporal split work. Verified today: `--- SKIP: TestDifferentialCorpus (0.00s)` with the message `corpus dir … does not exist yet — W4 will populate it`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Go's internal-package rule blocks cross-tree dryrun import**
- **Found during:** Task 3 (compile failure when `tests/differential_test.go` tried to import `pkg/validator/internal/dryrun`)
- **Issue:** Go's [internal-package rule](https://go.dev/ref/spec#Import_declarations) restricts importing `*/internal/*` packages to packages whose path starts with the parent of `internal/`. `pkg/validator/internal/dryrun` is importable only by packages under `pkg/validator/`. The plan's choice to place the differential test at `tests/` (to side-step the temporal firewall in `pkg/activity/firewall_test.go`) made the `internal/` placement unreachable.
- **Fix:** Moved `pkg/validator/internal/dryrun` → `pkg/validator/dryrun` via `git mv`. Updated `pkg/validator/dryrun/doc.go` to document the test-only convention is now social rather than syntactic. Updated the import in `tests/differential_test.go`.
- **Files modified:** `pkg/validator/dryrun/{dispatch.go, dispatch_test.go, doc.go}` (renamed); `tests/differential_test.go` (import path).
- **Verification:** `go test ./pkg/validator/dryrun -count=1` passes (2/2); `go test ./tests -run TestDifferentialCorpus -count=1` passes with `--- SKIP`; full `go test ./...` green.
- **Committed in:** `d91d54e` (refactor: move dryrun out of internal/)
- **Plan-impact:** Task 2's acceptance criterion 3 "Path is `pkg/validator/internal/dryrun/` — Go's `internal` rule prevents external packages from importing" is no longer literally satisfied. The functional intent (block production imports of the mock) is preserved by convention + dryrun/doc.go's call-out.

---

**Total deviations:** 1 auto-fixed (1 blocking — Go internal-package rule incompatibility).
**Impact on plan:** Single Rule-3 path move preserves all functional acceptance criteria. The "internal-package gate" is replaced with a "documented convention + future grep meta-test" gate; the plan's intent (Phase 5 reuse, no leak into production) survives. No scope creep, no logic change in dispatch.go itself.

## Issues Encountered

- **Existing `tests/firewall_cli_test.go` declares `package firewall_test`, not `tests_test`.** First compile attempt with `tests_test` failed with "found packages tests (differential_test.go) and firewall (firewall_cli_test.go) in tests/". Fixed by aligning my new file to the existing package name. This is a project convention to be aware of for future tests in this directory.
- **Go's internal-package rule discovered at first compile, not at planning time.** Worth flagging: the plan explicitly schedules the differential test outside `pkg/validator/` to avoid the temporal firewall, but pairs that with `internal/` for the mock dispatch. These two choices are mutually incompatible by Go's rules; the plan would benefit from a one-line note acknowledging the rule. (Documented in this SUMMARY's Decisions Made + Deviations sections instead.)

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- **VAL-02 enforcement infrastructure complete.** TestDifferentialCorpus is wired and skipping cleanly. The moment W4 (plan 04-07) creates `examples/skeleton/` with the first `.star` file using the baked-in HTTP extension (plan 04-04 lands the extension), this test will start running on every CI invocation and drift between static + runtime parser will fail fast.
- **W3 (pkg/cli root command) unblocked.** `validator.Validate(file, opts ...)` is the API the `skytime validate` cobra subcommand will call — plan 04-04 imports it directly.
- **W4 (HTTP extension + corpus) unblocked.** When `corpusExtensions(t)` in `tests/differential_test.go` grows from `nil` to `[]extension.Extension{httpext.New()}`, the differential test activates. The W4 plan should add the wiring as a single-line change.
- **Phase 5 (mock harness) seam established.** `dryrun.AlwaysOkDispatch` shape is reusable: a future `dryrun.RecordedDispatch(map[string]any)` or `dryrun.StarlarkMockDispatch(...)` follows the same shallow-copy-spec-replace-Func pattern. Phase 5 plans can grep for "AlwaysOkDispatch" to find the seam.
- **No blockers.** Full repo `go test ./...` exits 0; `go vet ./...` clean; firewall tests untouched; all Phase 1-3 fixture corpora continue to pass.

## Self-Check: PASSED

**Files verified (8/8 exist on disk):**
- `pkg/validator/validator.go` — `Validate` declared at line 36
- `pkg/validator/options.go` — `Option`, `WithExtensions`, `WithCredentialHandler`, `WithRoot` declared
- `pkg/validator/validator_test.go` — 3 `TestValidate_*` tests
- `pkg/validator/dryrun/dispatch.go` — `AlwaysOkDispatch` declared
- `pkg/validator/dryrun/dispatch_test.go` — 2 `TestAlwaysOkDispatch_*` tests
- `pkg/validator/dryrun/doc.go` — package doc with internal-rule rationale
- `tests/differential_test.go` — `TestDifferentialCorpus` + helpers
- `pkg/validator/doc.go` (modified) — Wave 2 surface documentation

**Commits verified (6/6 in `git log`):**
- `3c36385` (Task 1 RED), `b19905e` (Task 1 GREEN)
- `4cd0d77` (Task 2 RED), `9d29f6a` (Task 2 GREEN)
- `d91d54e` (Rule 3 refactor)
- `9919050` (Task 3 differential test)

**Verification gates green:**
- `go test ./pkg/validator -run TestValidate -count=1` → PASS (3/3)
- `go test ./pkg/validator/dryrun -count=1` → PASS (2/2)
- `go test ./tests -run TestDifferentialCorpus -count=1 -v` → SKIP (corpus not yet populated; expected)
- `go test ./pkg/activity -run TestNoTemporalImportsOutsideAllowList -count=1` → PASS (no firewall regression)
- `go test ./tests -run TestNoCobraImportsOutsideAllowList -count=1` → PASS (no firewall regression)
- `go test ./... -count=1` → all green
- `go vet ./...` → clean

All success criteria met; one Rule-3 deviation documented. No missing artifacts.

---
*Phase: 04-static-validation-tier-cli-skeleton*
*Completed: 2026-05-01*
