---
phase: 05-tier-3-e2e-test-harness-temporal-test
plan: 01
subsystem: testing
tags: [starlark, temporal, testsuite, mock-registry, parser-options, firewall]

# Dependency graph
requires:
  - phase: 04-static-validation-tier-cli-skeleton
    provides: pkg/cli + functional options pattern (Plan 06 will use to wire WithTestModule via cli.RegisterTestCommand)
  - phase: 03-lambda-serialization-decision-interpreter-worker
    provides: pkg/interpreter SkytimeWorkflow + FlowRegistry (the harness will reuse these unchanged at execute time)
  - phase: 02-generic-activity-block-batch-dispatch-credentials
    provides: dag.OperationOutput marker + RawOperationOutput JSON wire shape (MockOperationOutput must round-trip identically)
provides:
  - pkg/testing skeleton package
  - MockRegistry data structure with D5-B4 3-tier match precedence (no behavior consumers yet)
  - MockOperationOutput JSON-wire-format-stable wrapper (D5-C3 + Open Q4 — value-map directly, no envelope)
  - parser.WithTestMode() + parser.WithTestModule(builderFn) functional options
  - Parser.testMode + Parser.testModuleBuilder + Parser.testGlobals fields
  - Firewall allow-list expansion: pkg/testing may import go.temporal.io/sdk/*
  - Non-vacuous testsuite firewall meta-tests (skip-on-empty until Plan 02 lands router)
affects: [05-02-tester-module, 05-03-replay-helper, 05-04-discovery-runner, 05-05-cli, 05-06-firewall-e2e]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Two-option opt-in (flag + builder) breaks parser→pkg/testing import cycle"
    - "Per-frame stack (file frame + per-test frames) for D5-A4 mock scoping"
    - "Tier-iterating match algorithm — outer for tier=1..3, inner walks frames top-to-bottom, entries end-to-start"
    - "Non-vacuous firewall meta-test: skip-on-empty with forward-pointing message until import lands"

key-files:
  created:
    - pkg/testing/doc.go
    - pkg/testing/registry.go
    - pkg/testing/registry_test.go
    - pkg/testing/output.go
    - pkg/testing/output_test.go
    - pkg/parser/options_testmode_test.go
    - tests/firewall_testsuite_test.go
  modified:
    - pkg/parser/options.go
    - pkg/parser/parser.go
    - pkg/activity/firewall_test.go

key-decisions:
  - "MockEntry.RegisterPos uses syntax.MakePosition (not a struct literal) — go.starlark.net/syntax.Position has an unexported file *string field; tests label entries via RegisterPos.Filename() accessor"
  - "splitExtOp helper in registry.go parses 'gh.get' → ('gh', 'get', true) via byte iteration (no strings.Split allocation); rejects malformed kinds (no dot, leading/trailing dot)"
  - "matchKwargs predicate centralizes the 'every key must regex-match its kwarg' rule so Match cases 1 and 3 share the same logic; an empty match map matches unconditionally for tier-3 with no kwargs filter"
  - "MockOperationOutput.MarshalJSON returns []byte(\"{}\") for nil Value so the wire shape is always a JSON object — RawOperationOutput.Bytes will hold a JSON object even for empty mocks"
  - "Test-mode option pair shipped without injection logic — Plan 02 owns extending newParseTimeGlobals; Plan 01 only adds the field + option so subsequent waves drop in without re-touching parser.go"

patterns-established:
  - "Mock-registry frame stack: Push/Pop per-test frames; Add routes to top-of-stack; Match walks per-test frames top-down, then file frame, end-to-start within each"
  - "Open Question Q4 acceptance: MockOperationOutput marshals as the value-map directly (no wrapper field) so the wire shape is indistinguishable from a real extension Output round-tripped to RawOperationOutput"
  - "Firewall allow-list expansion paired with non-vacuous meta-test: when adding a package to the temporal-SDK allow-list, also add a tests/firewall_*_test.go that PROVES the package eventually uses the SDK (skip-on-empty until the import lands)"

requirements-completed: [TEST-01, TEST-02]

# Metrics
duration: 25min
completed: 2026-05-05
---

# Phase 5 Plan 01: Phase 5 Wave-0 Scaffolding Summary

**pkg/testing skeleton + MockRegistry 3-tier ladder + JSON-wire-stable MockOperationOutput + parser test-mode opt-in + firewall allow-list expansion with non-vacuous testsuite meta-tests**

## Performance

- **Duration:** 25 min
- **Started:** 2026-05-05T17:56:57Z
- **Completed:** 2026-05-05T18:21:51Z
- **Tasks:** 3
- **Files created:** 7
- **Files modified:** 3

## Accomplishments

- `pkg/testing` package compiles with MockRegistry implementing the D5-B4 3-tier match ladder (tier 1 = (ext, op) + match regex; tier 2 = (ext, op) exact; tier 3 = (ext, "*") wildcard) with per-test frame stack shadowing the file frame (D5-A4) and recency-within-tier ordering.
- `MockOperationOutput` satisfies `dag.OperationOutput` and JSON-marshals as the value-map directly (no wrapper field), accepting Open Question 4 verbatim — the wire shape is byte-equal to a real extension Output post-`RawOperationOutput` round-trip.
- `parser.WithTestMode()` + `parser.WithTestModule(builderFn)` functional options + 3 new Parser fields (`testMode`, `testModuleBuilder`, `testGlobals`) — production parse path is unchanged; the option pair breaks the parser→pkg/testing import cycle so Plan 06 can supply the builder from `pkg/cli/test.go`.
- Firewall allow-list expanded: `pkg/testing` is the fifth allowlisted importer of `go.temporal.io/sdk/*` (after activity/interpreter/worker/cli) per D5-firewall-q8.
- Non-vacuous firewall meta-tests: `TestPkgTesting_ImportsTestsuite` skips with a forward-pointing "Plan 02 will land router.go" message until the import lands; `TestPkgTesting_DoesNotImportSDKWorker` is the inverse rule (the harness uses `TestWorkflowEnvironment`, never registers a real worker).

## Task Commits

Each task was committed atomically:

1. **Task 1: pkg/testing skeleton + MockRegistry 3-tier ladder + MockOperationOutput** — `78fcec0` (feat)
2. **Task 2: Parser WithTestMode/WithTestModule + Parser.testGlobals field** — `de84e15` (feat)
3. **Task 3: Firewall allow-list extension + non-vacuous testsuite firewall meta-test** — `f556b46` (feat)

**Plan metadata:** _to be added with the SUMMARY commit_

## Files Created/Modified

### Created
- `pkg/testing/doc.go` — package preamble documenting the parse/execute split for the Tier-3 harness
- `pkg/testing/registry.go` — `MockEntry`, `Frame`, `MockRegistry` with `Push/PopTestFrame`, `Add`, `Match` (D5-B4 3-tier ladder), `splitExtOp`, `matchKwargs`, `CompileMatchRegex` helper, `ErrCrossExtensionWildcard` sentinel
- `pkg/testing/registry_test.go` — `TestRegistry_TierPrecedence_TestVectors` (5 subtests) + 6 sibling named tests (empty, no-cross-ext-wildcard, regex-compile-at-registration, partial-match-by-default, non-string-kwarg-absent, pop-empty-stack panics)
- `pkg/testing/output.go` — `MockOperationOutput` satisfying `dag.OperationOutput`; `MarshalJSON` returns the value map directly (Open Q4)
- `pkg/testing/output_test.go` — Open Q4 wire-shape tests + nil-value-empty-object defensive
- `pkg/parser/options_testmode_test.go` — 4 named tests for the new options
- `tests/firewall_testsuite_test.go` — `TestPkgTesting_ImportsTestsuite` (skip-on-empty) + `TestPkgTesting_DoesNotImportSDKWorker`

### Modified
- `pkg/parser/parser.go` — appended 3 fields to `Parser` struct: `testMode bool`, `testModuleBuilder func(p *Parser, thread *starlark.Thread) starlark.Value`, `testGlobals map[string]starlark.StringDict`
- `pkg/parser/options.go` — added `WithTestMode()` + `WithTestModule(builderFn)` options; added `go.starlark.net/starlark` to imports
- `pkg/activity/firewall_test.go` — added `"testing"` to `allowedPkgs` slice with D5-firewall-q8 deviation comment block; updated FIREWALL VIOLATION error message to mention the expanded allow-list

## Decisions Made

- **`syntax.Position` construction in tests** — `go.starlark.net/syntax.Position` has an unexported `file *string` field, so test fixtures must use `syntax.MakePosition(&label, 0, 0)` rather than a struct literal `Position{Filename: label}`. Plan code-block sketch was a struct literal; corrected to the constructor in tests with a `Filename()` accessor on read-back. Pure test-side issue; production code in `registry.go` is unaffected.
- **Test partition** — added the 4 new test-mode tests in a sibling `pkg/parser/options_testmode_test.go` rather than appending to `parser_test.go` (which is already 290 lines). Keeps grep-by-test-name discoverable.
- **Empty `MarshalJSON` shape** — `MockOperationOutput{}.MarshalJSON()` emits `{}` (not `null`) so the round-tripped `RawOperationOutput.Bytes` always holds a JSON object. Mirrors the existing `RawOperationOutput` JSON wire shape and avoids null-pointer surprises in Plan 02's router.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `syntax.Position` field is unexported; struct-literal sketch in plan does not compile**
- **Found during:** Task 1 (registry tests)
- **Issue:** The plan's `<action>` sketch for `pkg/testing/registry.go` uses `syntax.Position` as the type of `MockEntry.RegisterPos`, which is correct. The plan's test sketch (`registry_test.go` skeleton) was abstract enough not to specify how the test labels entries; my initial implementation used `RegisterPos: syntax.Position{Filename: label}` and `e.RegisterPos.Filename` (field access). Both fail to compile — `Position.file` is unexported, and `Filename` is a method.
- **Fix:** Use `syntax.MakePosition(&label, 0, 0)` in `mkEntry` and `e.RegisterPos.Filename()` (method call) in `labelOf`.
- **Files modified:** `pkg/testing/registry_test.go` only
- **Verification:** `go test ./pkg/testing -v` passes all 6 named tests (5 subtests inside `TestRegistry_TierPrecedence_TestVectors`).
- **Committed in:** `78fcec0` (Task 1 commit)

---

**Total deviations:** 1 auto-fixed (1 bug — test-side compile error)
**Impact on plan:** Out-of-the-box compile fix only; production `registry.go` matches the plan sketch verbatim. No scope creep, no architectural change.

## Issues Encountered

None — all three tasks executed cleanly after the syntax.Position field-access correction.

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- **Plan 02 (`tester` module against this registry):** Ready. `MockRegistry`, `MockEntry`, `MockOperationOutput`, `Parser.WithTestMode`, `Parser.WithTestModule`, and `Parser.testGlobals` are all in place. Plan 02's `tester.workflow` / `tester.mock_action` / `tester.run` builtins can call `MockRegistry.Add` (with `CompileMatchRegex` for D5-B5) and supply the parser builder via `WithTestModule(myBuilder)`. The non-vacuous firewall meta-test (`TestPkgTesting_ImportsTestsuite`) will activate as soon as Plan 02 lands the first `go.temporal.io/sdk/testsuite` import.
- **Plans 03/04 (parser test-mode hook + replay helper + discovery):** Ready. The parser's `testMode` flag will gate the `tester` module injection; `testGlobals[filename]` is reserved for Plan 05's `def test_*` discovery (Plan 02 should populate `parseTimeGlobals` only, leaving `testGlobals` for Plan 05 to fill from the post-`ExecFileOptions` StringDict).
- **Plan 06 (CLI subcommand + e2e firewall test):** Ready. `cli.RegisterTestCommand` (Plan 06) will import `pkg/testing`, construct the `tester` module builder, and pass it to `parser.NewParser(WithTestMode(), WithTestModule(builder))`.
- **No blockers.** All Wave-0 contracts are pinned by named tests, so Wave 1 can proceed without re-litigating field names or shapes.

## Self-Check: PASSED

- `pkg/testing/doc.go` — exists, contains `package testing` and "parse/execute split" verbatim.
- `pkg/testing/registry.go` — exists, contains `func (r *MockRegistry) Match(ref *dag.ActionRef, kwargsAsString map[string]string) (MockEntry, bool) {`, `func NewMockRegistry()`, `func (r *MockRegistry) PushTestFrame()`, `func (r *MockRegistry) PopTestFrame()`, `func (r *MockRegistry) Add(`, `var ErrCrossExtensionWildcard = errors.New(`.
- `pkg/testing/output.go` — exists, contains `func (m MockOperationOutput) MarshalJSON() ([]byte, error) {` and `var _ dag.OperationOutput = MockOperationOutput{}`.
- `pkg/parser/options.go` — contains `func WithTestMode() Option {` and `func WithTestModule(builderFn func(p *Parser, thread *starlark.Thread) starlark.Value) Option {`.
- `pkg/parser/parser.go` — Parser struct contains all three new fields with exact names (`testMode bool`, `testModuleBuilder func(p *Parser, thread *starlark.Thread) starlark.Value`, `testGlobals map[string]starlark.StringDict`).
- `pkg/activity/firewall_test.go` — `allowedPkgs` literal contains `"testing"`.
- `tests/firewall_testsuite_test.go` — exists, contains `func TestPkgTesting_ImportsTestsuite(` and `func TestPkgTesting_DoesNotImportSDKWorker(`.
- Commits: `78fcec0`, `de84e15`, `f556b46` all present in `git log --oneline`.
- `go test -race -count=1 ./pkg/testing/... ./pkg/parser/... ./tests/... ./pkg/activity/...` exits 0 across all four packages.
- `go vet ./pkg/testing/... ./pkg/parser/... ./tests/...` exits 0.
- `gofmt -d` produces no diff on any of the touched files.
- `grep -c '"github.com/mikelalcon/skytime/pkg/testing"' pkg/parser/{parser,options,globals}.go` returns 0 (no parser → pkg/testing import — cycle-prevention preserved).
- `go build ./...` succeeds across the full module.

---
*Phase: 05-tier-3-e2e-test-harness-temporal-test*
*Completed: 2026-05-05*
