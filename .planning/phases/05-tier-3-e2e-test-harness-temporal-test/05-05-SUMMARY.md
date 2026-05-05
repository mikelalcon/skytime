---
phase: 05-tier-3-e2e-test-harness-temporal-test
plan: 05
subsystem: testing
tags: [starlark, runner, recursive-walk, regex-filter, json-output, test2json, human-format, cli-03, replay-deterministic-discovery]

# Dependency graph
requires:
  - phase: 05-tier-3-e2e-test-harness-temporal-test
    provides: "pkg/testing.Run(t, dir, opts...) foundation API + Option type + WithExtensions/WithRunFilter (Plan 04); Parser.TestGlobals(filename) accessor (Plan 04); runOneTest + testReporter interface (Plan 04); recordingT shim in reporter_test.go (Plan 04)"
provides:
  - "pkg/testing.DiscoverTestFiles(root) — recursive *_test.star walker (D5-A2) using filepath.WalkDir; single-file passthrough; non-_test.star path → error"
  - "pkg/testing.DiscoverTests(globals) — top-level def test_*() enumeration filtered by prefix + NumParams==0, sorted alphabetically (D5-A1, RESEARCH Pattern 4)"
  - "pkg/testing.CompileRunFilter(pattern) — compile-once-at-option-time regex; ErrBadFilter sentinel for bad patterns; empty → match all"
  - "pkg/testing.MatchRunFilter(re, fullName) — regex match against `<file_basename_without_ext>.<test_name>` (D5-E3)"
  - "pkg/testing.WithFormat(\"human\"|\"json\") — D5-E1 default human / D5-E2 cmd/test2json mirror; unknown values reject at option time"
  - "pkg/testing.WithOutput(io.Writer) — pluggable sink for captured rendering"
  - "pkg/testing.JSONEvent — exact-tag mirror of stdlib cmd/test2json TestEvent (Time/Action/Package/Test/Elapsed/Output, capitalized JSON keys, omitempty for Test/Elapsed/Output)"
  - "pkg/testing.jsonEmitter — line-delimited JSON record emitter; Time = time.Now().UTC() (Open Q6 RFC3339Nano UTC)"
  - "pkg/testing.formatHumanLine(action, test, elapsed) — D5-E1 verbatim line shape `--- PASS|FAIL|SKIP: <test> (<elapsed:.2f>s)`"
  - "renderOneFile factored from runOneFile so format tests drive a recording shim instead of real t.Run (deviation Rule 3)"
  - "capturingTReporter — wraps *testing.T to BOTH propagate failures AND capture detail text for JSON `output` records / human indented FAIL detail"
  - "WithRunFilter upgraded: now compiles regex at option time (was strings.Contains stub)"
  - "Final all-files summary line in human mode: `PASS|FAIL  N files  (Xs)`"
affects: [05-06-firewall-cli-e2e]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "renderOneFile + driveTestFn callback pattern: format-agnostic core takes a per-test driver closure that supplies (passed, skipped, detail). Production runOneFile injects a t.Run-based driver; test code injects a recordingT-based driver. Same emission logic exercised both ways without duplication."
    - "Recording shim at the file-rendering level: previous Plan 04 walkAndDriveTests operated at the runOneTest level; Plan 05's renderOneFileWithRecorder operates at the runOneFile level so format tests observe the FULL rendered output (per-test lines + per-file footer) without poisoning their parent t."
    - "json.Encoder for line-delimited JSON: each Encode call appends \\n automatically. No manual newline handling; one encoder per file (cheap, sequential within-file per D5-E5)."
    - "Capitalized JSON tags as load-bearing: `Time`/`Action`/`Package`/`Test,omitempty`/`Elapsed,omitempty`/`Output,omitempty` — the EXACT tags stdlib cmd/test2json emits. Drift breaks gotestsum/tparse/GitHub-Actions consumers and is detected by TestJSONEvent_FieldTags at the marshalled-bytes level."
    - "Time.UTC() at emit time: pinned by TestJSONEmitter_TimeIsUTC asserting the emitted record's Time.Zone() is 'UTC'. Naive time.Now() carries the local zone and breaks replay-determinism for tests captured across hosts (Open Q6 conclusion)."
    - "Test fixture name discipline: `not_a_test_file.star` (NOT `not_a_test.star`) for the negative case in TestDiscoverTestFiles_RecursiveWalk — `_test.star` suffix matching is naive substring on basename, so `not_a_test.star` would be matched. Plan-side draft used the shorter name and was corrected as a Rule-1 deviation."

key-files:
  created:
    - "pkg/testing/discover.go (DiscoverTestFiles + DiscoverTests + CompileRunFilter + MatchRunFilter + ErrBadFilter + TestFunc)"
    - "pkg/testing/discover_test.go (10 named tests: 4 DiscoverTestFiles_*, 2 DiscoverTests_*, 4 CompileRunFilter_/MatchRunFilter_*; mustParseTestGlobals helper)"
    - "pkg/testing/output_json.go (JSONEvent + jsonEmitter + formatHumanLine)"
    - "pkg/testing/output_json_test.go (5 named tests: TestJSONEvent_FieldTags, TestJSONEvent_OmitemptyTestAndOutput, TestJSONEmitter_LineDelimited, TestJSONEmitter_TimeIsUTC, TestFormatHumanLine_PassFailSkip)"
    - "pkg/testing/runner_format_test.go (8 named tests + renderOneFileWithRecorder helper)"
  modified:
    - "pkg/testing/runner.go (WithRunFilter compiles regex at option time; runConfig gains runRegex/formatJSON/formatOut; WithFormat + WithOutput options; renderOneFile factored; capturingTReporter; final-summary line in Run; runOneFile delegates to renderOneFile via t.Run-based driver)"
    - "pkg/testing/runner_test.go (TestTestCommand_RunFilter — VALIDATION cite for CLI-03; stemOf helper; updated walkAndDriveTests neighbors)"
    - "pkg/testing/reporter_test.go (recordingT extended with detail strings.Builder so format tests can observe captured Starlark callsite text)"

key-decisions:
  - "[Plan 05 deviation, Rule 3] renderOneFile factored from runOneFile to break the Go t.Run propagation cycle for format tests. Plan suggested testing JSON-format output by calling Run(subT, dir, WithFormat(\"json\"))(subT) under t.Run('inner', ...), but a failing inner test (the whole point of TestSkytimeTestE2E_JSONFormat_Failing and TestTestCommand_DefaultOutput_NoGoStackTraces) propagates to the parent T → outer test fails. The factor-out enables a recordingT-based driver while production runOneFile retains real t.Run."
  - "[Plan 05 simplification] Test fixture name discipline: `not_a_test_file.star` rather than the plan's `not_a_test.star` — the second STILL ends in `_test.star` and would be matched by DiscoverTestFiles. Pure plan typo; corrected before commit."
  - "[Plan 05 - capturingTReporter pattern] Production runOneFile wraps t.Run's *testing.T inside a capturingTReporter that BOTH calls subT.Errorf (real failure) AND accumulates the rendered text for emission. JSON mode emits the captured text as an `output` record. Human mode renders it indented under the `--- FAIL:` line (D5-E1 detail block)."
  - "[Plan 05 design] Package field uses the file BASENAME (not the bare stem) — `users_test.star` not `users_test`. cmd/test2json's Package field carries the test binary path; Skytime's analog is the file path. Tests pin this verbatim (`assert.Equal(t, \"users_test.star\", ev.Package)`)."
  - "[Plan 05 design] DiscoverTestFiles single-file path requires the *_test.star suffix; non-test paths error with a clear message. Allows pkg/testing.Run callers to drive a single test file directly (Plan 06's CLI funnels here)."
  - "[Plan 05 design] WithFormat unknown values fail loudly at option-apply time. Mistyped formats (\"jsno\") error rather than silently falling back to human — pinned by TestWithFormat_UnknownReturnsErrorAtOptionTime."

patterns-established:
  - "renderOneFile + driveTestFn callback for format-test isolation: production injects a t.Run-based driver, tests inject a recordingT-based driver. Same emission code path, no duplication."
  - "capturingTReporter wrapping *testing.T: BOTH propagate failure AND accumulate detail. Same pattern reusable for any future runner that needs to surface assertion text outside the *testing.T error stream (e.g., live-progress renderer or JUnit XML emitter)."
  - "json.Encoder + per-event Encode call for line-delimited JSON: one encoder per writer; each call writes one record + \\n. No manual newline; no buffering required. Standard pattern for go test -json compatibility."
  - "Capitalized JSON tags pinned by marshalled-bytes inspection: TestJSONEvent_FieldTags asserts on raw `\"Time\":` substring rather than relying on a custom MarshalJSON. Drift detection without serialization-format dependency."

requirements-completed: [TEST-01, CLI-03]

# Metrics
duration: 11min
completed: 2026-05-05
---

# Phase 5 Plan 05: Discovery + JSON Output + Human Format Summary

**Plan 04's single-directory `Run` generalized to `filepath.WalkDir` recursion with strict `*_test.star` suffix matching (D5-A2); Starlark module enumeration of `def test_*()` symbols filtered by prefix + zero-arg + alphabetic sort (D5-A1, RESEARCH Pattern 4); `WithRunFilter` upgraded from `strings.Contains` stub to compile-once-at-option-time regex with `ErrBadFilter` sentinel (D5-E3); `WithFormat("json")` emits cmd/test2json-mirror records with Time = `time.Now().UTC()` (Open Q6 RFC3339Nano UTC); default human format = static `--- PASS:` / `--- FAIL:` / `--- SKIP:` lines + per-file footer + final all-files summary (D5-E1); failing tests carry no Go stack frames in default output (CLI-03 verbatim, pinned by TestTestCommand_DefaultOutput_NoGoStackTraces).**

## Performance

- **Duration:** ~11 min (active work; planning context loading included)
- **Started:** 2026-05-05T20:15:00Z
- **Completed:** 2026-05-05T20:25:40Z
- **Tasks:** 2 (both atomic; TDD red-green for both)
- **Files created:** 5
- **Files modified:** 3

## Accomplishments

- **Task 1 (recursive walker + DiscoverTests + regex filter):** `pkg/testing/discover.go` ships `DiscoverTestFiles(root)` (filepath.WalkDir recursion + single-file passthrough + sorted output), `DiscoverTests(globals)` (top-level def test_*() enumeration filtered by prefix + NumParams==0 + sorted), `CompileRunFilter(pattern)` (compile-once at option time; `ErrBadFilter` sentinel) and `MatchRunFilter(re, fullName)`. `WithRunFilter` upgraded from Plan 04's `strings.Contains` stub: regex compile errors now surface at option-apply time wrapped in `ErrBadFilter` (detected via `errors.Is`). `runner.go::runOneFile` now uses `DiscoverTests` + `MatchRunFilter`. `TestTestCommand_RunFilter` pins CLI-03 filter behavior at the pkg/testing layer; Plan 06 will add a thin pkg/cli wrapper test exposing the same behavior through cobra.
- **Task 2 (--format=json + human format):** `pkg/testing/output_json.go` ships `JSONEvent` with EXACT capitalized tags matching stdlib cmd/test2json (`Time`/`Action`/`Package`/`Test,omitempty`/`Elapsed,omitempty`/`Output,omitempty`), `jsonEmitter` writing one record + `\n` per Encode call with `Time = time.Now().UTC()` (Open Q6), and `formatHumanLine(action, test, elapsed)` rendering D5-E1 verbatim shape. `runner.go` adds `WithFormat("human"|"json")` + `WithOutput(io.Writer)` options. `renderOneFile` factored from `runOneFile` so format tests drive a `recordingT`-based shim instead of real `t.Run` (avoids Go's inner-test failure propagation). `capturingTReporter` wraps `*testing.T` to BOTH propagate failures AND accumulate the rendered text for emission as JSON `output` records or human indented FAIL detail. Final all-files summary line (`PASS|FAIL  N files  (Xs)`) in human mode. `TestSkytimeTestE2E_JSONFormat`, `TestSkytimeTestE2E_JSONFormat_Failing`, `TestTestCommand_DefaultOutput_NoGoStackTraces` pin the CLI-03 contract verbatim.

## Task Commits

Each task committed atomically:

1. **Task 1: recursive *_test.star walker + DiscoverTests + regex --run filter** — `000cefe` (feat)
2. **Task 2: --format=json output records mirroring stdlib cmd/test2json schema** — `1282dfb` (feat)

## Files Created/Modified

### Created

- `pkg/testing/discover.go` — `DiscoverTestFiles(root) ([]string, error)`, `DiscoverTests(globals) []TestFunc`, `CompileRunFilter(pattern) (*regexp.Regexp, error)`, `MatchRunFilter(re, fullName) bool`, `var ErrBadFilter`, `type TestFunc struct {Name string; Fn *starlark.Function; Pos syntax.Position}`
- `pkg/testing/discover_test.go` — 10 named tests + `mustParseTestGlobals` helper that wires `parser.WithTestMode + WithTestModule(NewTesterModule) + WithTestPredeclared(MockLambdaParseTimeBuilders())` and surfaces `Parser.TestGlobals(filename)`
- `pkg/testing/output_json.go` — `type JSONEvent struct {Time time.Time; Action, Package, Test string; Elapsed float64; Output string}` with capitalized JSON tags; `jsonEmitter`; `newJSONEmitter(w)`; `(*jsonEmitter).emit(action, pkg, test, output, elapsed)`; `formatHumanLine(action, test, elapsed) string`
- `pkg/testing/output_json_test.go` — 5 named tests + 1 omitempty test
- `pkg/testing/runner_format_test.go` — 8 named tests + `renderOneFileWithRecorder` helper

### Modified

- `pkg/testing/runner.go` — `WithRunFilter` upgraded to `CompileRunFilter`; `runConfig` extended with `runRegex`/`formatJSON`/`formatOut`; new `WithFormat`/`WithOutput` options; new `renderOneFile` + `driveTestFn` callback type; `runOneFile` rewritten as a thin t.Run-based driver delegating to `renderOneFile`; `capturingTReporter` wrapper for *testing.T; final all-files summary line in `Run`; `parseTestFile` uses `DiscoverTests` instead of inline filtering; imports gain `io`, `regexp`, `time`; doc comment on `Run` updated to reflect Plan 05 generalization
- `pkg/testing/runner_test.go` — `TestTestCommand_RunFilter` (VALIDATION cite for CLI-03); `stemOf` helper
- `pkg/testing/reporter_test.go` — `recordingT` extended with `detail strings.Builder`; `Error`/`Errorf` accumulate the rendered text so format tests can observe captured Starlark callsite

## Decisions Made

See frontmatter `key-decisions` for the full list. Most-load-bearing:

1. **`renderOneFile` factored from `runOneFile` (Rule 3 deviation).** The plan's Task 2 sketch had the format tests calling `Run(subT, dir, WithFormat("json"), WithOutput(&buf))` under a `t.Run("inner", ...)` wrapper. This breaks for failing-test scenarios (TestSkytimeTestE2E_JSONFormat_Failing, TestTestCommand_DefaultOutput_NoGoStackTraces) because Go's `t.Run` propagates inner test failures to the parent T — the outer format test fails itself, even though it's intentionally exercising the failure-rendering path. Solution: factor `runOneFile` into a `renderOneFile` that takes a `driveTestFn` callback. Production `runOneFile` injects a `t.Run`-based driver; tests inject a `recordingT`-based driver via `renderOneFileWithRecorder`. Same emission code path, no duplication, and format tests can assert on the rendered output without poisoning their parent.

2. **`capturingTReporter` wrapping `*testing.T` for dual purpose.** When tests inside `runOneFile` fail, two things must happen: (a) the *testing.T must be told to fail (`subT.Errorf`), and (b) the failure detail must be captured for later emission as a JSON `output` record or as indented human-format text. `capturingTReporter` does both. The pattern is reusable for any future runner that needs to surface assertion text outside the *testing.T error stream.

3. **Test fixture name discipline.** The plan's `TestDiscoverTestFiles_RecursiveWalk` draft fixture was `not_a_test.star` to test that non-_test.star files are skipped. But `not_a_test.star` literally ends with `_test.star` — naive suffix matching matches it. The fixture name was corrected to `not_a_test_file.star`, which actually has the property the test is asserting. Caught at first run.

4. **Capitalized JSON tags pinned at the marshalled-bytes level.** `TestJSONEvent_FieldTags` asserts on raw substrings `"Time":`, `"Action":"pass"`, etc. inside the marshalled output. This catches drift even if someone "improves" the JSON serialization with a custom MarshalJSON or removes the tags accidentally — the test depends on the wire format, not on the Go struct's internals.

5. **`time.Now().UTC()` at emit time, pinned by `TestJSONEmitter_TimeIsUTC`.** Naive `time.Now()` carries the local timezone; replay-determinism (D5-D1) requires that two runs of the same test on different hosts produce byte-equal JSON output (modulo the timestamp itself). Forcing UTC neutralizes the host-locale variable.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Test fixture `not_a_test.star` actually matches the `_test.star` suffix.**

- **Found during:** Task 1 (first run of `TestDiscoverTestFiles_RecursiveWalk`).
- **Issue:** The plan's draft fixture name `not_a_test.star` was meant to test that non-test files are skipped, but the name itself ends in `_test.star`, so `DiscoverTestFiles`'s naive `strings.HasSuffix(name, "_test.star")` matches it correctly. The test asserted 4 files; my walker returned 5.
- **Fix:** Renamed to `not_a_test_file.star`, which truly does NOT end in `_test.star`.
- **Files modified:** `pkg/testing/discover_test.go`.
- **Verification:** `TestDiscoverTestFiles_RecursiveWalk` passes.
- **Committed in:** `000cefe` (Task 1 commit).

**2. [Rule 3 - Blocking] `runOneFile` `t.Run` propagates inner test failures to the format test's parent T.**

- **Found during:** Task 2 (running `TestSkytimeTestE2E_JSONFormat_Failing` and `TestTestCommand_DefaultOutput_NoGoStackTraces`).
- **Issue:** Both tests intentionally execute a failing test fixture and assert on the rendered output (JSON `fail` record / human `--- FAIL:` line + no Go stack frames). The plan's draft wrapped `Run(subT, dir, WithFormat("json"), WithOutput(&buf))` inside `t.Run("inner", subT)`. Go's `t.Run` propagates a failed subtest to its parent T, so even though we expected the inner subtest to fail, the OUTER format test failed too.
- **Fix:** Extracted `renderOneFile` from `runOneFile`. `runOneFile` is now a thin `t.Run`-based driver that calls `renderOneFile` with a closure over `t.Run`. Tests use `renderOneFileWithRecorder` which calls `renderOneFile` with a `recordingT`-based driver — failures land on the recordingT instead of escalating to the parent T. Production code path unchanged in behavior; same emission logic exercised by both.
- **Files modified:** `pkg/testing/runner.go`, `pkg/testing/runner_format_test.go`, `pkg/testing/reporter_test.go` (extended `recordingT.detail`).
- **Verification:** All 8 format tests pass; production `runOneFile` still uses `t.Run`; the existing `TestRunner_DiscoversAndRunsSingleFile` and `TestRunner_AssertFailureMakesSubtestFail` (Plan 04) still pass.
- **Committed in:** `1282dfb` (Task 2 commit).

---

**Total deviations:** 2 auto-fixed (1 Rule 1 bug — test fixture name typo; 1 Rule 3 blocking — Go t.Run failure propagation broke format-test isolation).
**Impact on plan:** No scope creep. Both fixes were strictly local to this plan's tests/structure. The `renderOneFile` factor preserves the plan's emission shape (start/run/output/pass/fail/skip + per-file footer + final summary) and is the cleanest path to enable format-test isolation without rewriting the t.Run-based production driver.

## Issues Encountered

None blocking. Both deviations were auto-fixed within the same task without requiring a checkpoint.

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- **Plan 06 (CLI subcommand + e2e firewall):** ready. The `Run(t, dir, opts...)` API now supports `WithFormat("json"|"human")` + `WithOutput(io.Writer)` + `WithRunFilter(<regex>)` + `WithExtensions(...)`. Plan 06's `pkg/cli/test.go` will:
  - Wire cobra `--run <regex>` flag → `WithRunFilter(...)` (compile errors surface at the cobra level).
  - Wire `--format <human|json>` → `WithFormat(...)` (typos rejected at option time).
  - Construct a `*testing.T`-shim that converts subtest failures into stdout records and exit-code-1 (the CLI exit-code-on-fail path is the same shape Plan 04 anticipated for the runner-driven execution).
  - Add `tests/skytime_test_e2e_test.go` (or similar) that drives the same paths via the subprocess. Plan 05's pkg/testing tests are in-process; Plan 06 layers the CLI envelope.
- **Plan 06 D5-D1 ↔ retry resolution:** still owned by Plan 06 (Plan 04 architectural finding). Plan 05 did not touch the replay-determinism path. The forward-pointing skip on `TestAttempts_IncrementOnRetry` remains in place.
- **No blockers.** All Wave-4 contracts are pinned by named tests; Plan 06 can compose without re-litigating field names or shapes.

## Self-Check: PASSED

Verified file-existence, content markers, and commit-presence for every claim in this Summary.

**Files created (verified via `[ -f path ]`):**

- `pkg/testing/discover.go` — FOUND
- `pkg/testing/discover_test.go` — FOUND
- `pkg/testing/output_json.go` — FOUND
- `pkg/testing/output_json_test.go` — FOUND
- `pkg/testing/runner_format_test.go` — FOUND

**Files modified:**

- `pkg/testing/runner.go` — contains `WithFormat`, `WithOutput`, `renderOneFile`, `capturingTReporter`, `CompileRunFilter` call inside `WithRunFilter`, `MatchRunFilter` call inside `renderOneFile`, `DiscoverTests` call inside `parseTestFile`, final all-files summary in `Run`
- `pkg/testing/runner_test.go` — contains `TestTestCommand_RunFilter` and `stemOf`
- `pkg/testing/reporter_test.go` — `recordingT` has `detail strings.Builder`

**Commits (verified via `git log --oneline | grep`):**

- `000cefe` feat(05-05): recursive *_test.star walker + DiscoverTests + regex --run filter — FOUND
- `1282dfb` feat(05-05): --format=json output records mirroring stdlib cmd/test2json schema — FOUND

**Test gates:**

- `go test -race -count=1 ./pkg/testing/... ./pkg/parser/... ./pkg/interpreter/... ./tests/...` → all packages OK
- `go vet ./pkg/testing` → clean
- `gofmt -d pkg/testing/discover.go pkg/testing/output_json.go pkg/testing/runner.go` → clean (no diff)
- `TestDiscoverTestFiles_RecursiveWalk` → PASS (D5-A2)
- `TestDiscoverTestFiles_SingleFile` → PASS
- `TestDiscoverTestFiles_NonTestFile_Errors` → PASS
- `TestDiscoverTestFiles_NonexistentPath_Errors` → PASS
- `TestDiscoverTests_FiltersTopLevelTestPrefixZeroArg` → PASS (D5-A1)
- `TestDiscoverTests_SortedAlphabetical` → PASS
- `TestCompileRunFilter_EmptyMeansMatchAll` → PASS
- `TestCompileRunFilter_BadPattern_Errors` → PASS (errors.Is(err, ErrBadFilter))
- `TestMatchRunFilter_EmptyMatchesAll` → PASS
- `TestMatchRunFilter_RegexMatch` → PASS (D5-E3)
- `TestTestCommand_RunFilter` → PASS (CLI-03 filter behavior)
- `TestJSONEvent_FieldTags` → PASS (capitalized JSON tags)
- `TestJSONEvent_OmitemptyTestAndOutput` → PASS
- `TestJSONEmitter_LineDelimited` → PASS
- `TestJSONEmitter_TimeIsUTC` → PASS (Open Q6)
- `TestFormatHumanLine_PassFailSkip` → PASS (D5-E1)
- `TestRun_HumanFormat_DefaultOutputContainsPassLine` → PASS
- `TestRun_HumanFormat_FinalSummary` → PASS
- `TestSkytimeTestE2E_JSONFormat` → PASS (CLI-03 + D5-E2)
- `TestSkytimeTestE2E_JSONFormat_Failing` → PASS (no Go stack frames in JSON Output)
- `TestTestCommand_DefaultOutput_NoGoStackTraces` → PASS (CLI-03 verbatim)
- `TestWithFormat_UnknownReturnsErrorAtOptionTime` → PASS
- `TestWithFormat_AcceptsHumanAndJSON` → PASS

---
*Phase: 05-tier-3-e2e-test-harness-temporal-test*
*Completed: 2026-05-05*
