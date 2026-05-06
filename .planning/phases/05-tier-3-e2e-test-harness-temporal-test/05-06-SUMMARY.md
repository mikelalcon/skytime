---
phase: 05-tier-3-e2e-test-harness-temporal-test
plan: 06
subsystem: testing
tags: [cli, cobra, runcli, subprocess-e2e, docs, testing-md, cli-md, cli-03, no-go-stack-traces, plan-05-final]

# Dependency graph
requires:
  - phase: 05-tier-3-e2e-test-harness-temporal-test
    provides: "pkg/testing.Run(t *testing.T, dir, opts...) foundation API + Option type (WithExtensions / WithRunFilter / WithFormat / WithOutput) (Plan 04+05); pkg/testing.DiscoverTestFiles + DiscoverTests + MatchRunFilter + ErrBadFilter (Plan 05); pkg/testing.JSONEvent + jsonEmitter + formatHumanLine (Plan 05); pkg/testing.NewMockRegistry + WorkflowSpec + newTesterModuleWithCtx + runContext (Plan 02-04); pkg/testing.parseTestFile path that wires WithTestMode + WithTestModule (Plan 04)"
  - phase: 04-static-validation-tier-cli-skeleton
    provides: "pkg/cli root + sibling-subcommand pattern (newRunCommand/newValidateCommand/newDevServerCommand/newInfoCommand) + errSilent sentinel + persistent --debug + cmd/skytime main wrapper (Plan 04-04..04-06); tests/e2e_skytime_run_test.go ensureBinary + findModuleRootE2E pattern (Plan 04-05); tests/firewall_cli_test.go allowlist for pkg/cli (Plan 04-04)"
provides:
  - "pkg/testing.RunCLI(dir, opts...) (passed, failed int, err error) — non-*testing.T entry-point that drives DiscoverTestFiles + DiscoverTests + the parser-test-mode path against a synthetic *bareReporter, returning per-run counts so the CLI can map to exit codes without inventing a *testing.T mock"
  - "pkg/testing.bareReporter — satisfies starlarktest.Reporter (interface{ Error(args ...any) }) without depending on *testing.T; records every assertion failure for indented FAIL-line detail (D5-E1) and JSON `output` records (D5-E2)"
  - "pkg/testing.runOneFileCLI + runOneTestCLI — mirror runner.go::runOneFile and reporter.go::runOneTest but bind bareReporter instead of *testing.T (deviation D5-runner-cli-adapter — ~80 LOC duplication; tested on both surfaces)"
  - "pkg/cli.newTestCommand(cfg) — cobra subcommand wired by pkg/cli/root.go::NewRootCommand alongside validate/run/dev-server/info; --run + --format string flags; cobra.ExactArgs(1) for `<dir>`; D5-E4 exit-code mapping (passed→nil, failed→errSilent, err→errSilent + stderr line)"
  - "tests/skytime_test_e2e_test.go — subprocess E2E harness building cmd/skytime via go build and exercising happy-path PASS, FAIL exit-1, JSON format end-to-end; mirrors tests/e2e_skytime_run_test.go::ensureBinary; build tag !windows for parity with sibling e2e tests"
  - "docs/for-flow-authors/testing.md — manual reference for tester.* (workflow / mock_action / run) + assert.* + *_test.star convention + ${ctx.expr} caveats; cites pkg/testing/module.go and pkg/parser/parser.go::WithTestMode as source-of-truth"
  - "docs/reference/cli.md `## skytime test` section — 6-H3 template (Synopsis / Motivation / Flags / Exit Codes / Example / See Also) inserted between `## skytime info` and `## skytime dev-server`"
  - "CLI-03 contract pinned at THREE layers: (a) pkg/testing.RunCLI via TestRunCLI_NoGoStackTracesInFailureOutput, (b) pkg/cli/test.go via TestTestCommand_DefaultOutput_NoGoStackTraces, (c) subprocess via TestSkytimeTestE2E_FailureExitNonzero"
affects: []

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Non-*testing.T reporter adapter pattern: starlarktest.Reporter is interface{ Error(args ...any) } — anything implementing Error satisfies it. *bareReporter records every failure into a []string for indented FAIL-line detail or JSON `output` records, eliminating the *testing.T dependency from the CLI execution path."
    - "Plan-additive duplication over plan-disruptive refactor: runOneFileCLI + runOneTestCLI duplicate ~80 LOC of Plan 04+05's runOneFile / runOneTest because the latter are *testing.T-bound (subT.Run / subT.Failed). Per the 'Plans 01..05 already committed; do NOT modify them' planning lock, Plan 06 SHIPS the duplication and accepts the cost; future v1.x can refactor to a shared internal helper."
    - "Plan 06 deviation D5-docs-builtins-marker-location: cmd/skytime-docgen is single-file (walks ONLY pkg/parser/globals.go) and tester.* lives in pkg/testing/module.go — outside the walker's reach. Manual H2 sections in docs/for-flow-authors/testing.md serve as the source of truth for tester.* until multi-file docgen walking ships post-v1. NO modifications to pkg/parser/builtins.go (originally proposed, removed); NO regeneration of docs/reference/builtins.md (originally proposed, removed); the existing TestDocgenDrift CI gate stays green by virtue of NOT touching the docgen-managed files."
    - "Three-layer CLI-03 contract pinning: an explicit no-Go-stack-traces test fires at (a) pkg/testing layer via TestRunCLI_NoGoStackTracesInFailureOutput (asserts on RunCLI's WithOutput buffer), (b) pkg/cli layer via TestTestCommand_DefaultOutput_NoGoStackTraces (asserts on cobra's stdout+stderr buffers), (c) subprocess layer via TestSkytimeTestE2E_FailureExitNonzero (asserts on the built binary's combined stdout+stderr). Three independent failure modes for one explicit success-criterion phrase: 'no Go stack traces in default output'."
    - "Test-binary-build-once-per-process pattern (mirrored from tests/e2e_skytime_run_test.go): testCmdBinOnce sync.Once + testCmdBinErr stored at package scope so each E2E test calls ensureTestCmdBinary(t) and either builds via go build (first call) or reuses the cached path (subsequent calls). Distinct from the run-e2e binary so parallel test invocations don't fight over the same /tmp dir."

key-files:
  created:
    - "pkg/testing/cli_run.go (RunCLI + runOneFileCLI + runOneTestCLI + bareReporter; 210 LOC)"
    - "pkg/testing/cli_run_test.go (8 named tests: TestRunCLI_HappyPath_ReturnsPassedOne, TestRunCLI_FailPath_ReturnsFailedOne, TestRunCLI_Mixed_ReturnsBothCounts, TestRunCLI_JSONFormat_LineDelimitedRecords, TestRunCLI_NoGoStackTracesInFailureOutput, TestRunCLI_BadRunFilter_ReturnsErrAtOptionTime, TestRunCLI_RunFilter_ExcludesNonMatching, TestRunCLI_NoTestFiles_ZeroCountsNoError; 129 LOC)"
    - "pkg/cli/test.go (newTestCommand cobra builder; --run + --format flags; D5-E4 exit-code mapping; 81 LOC)"
    - "pkg/cli/test_test.go (6 named tests including TestTestCommand_RunFilter and TestTestCommand_DefaultOutput_NoGoStackTraces; 100 LOC)"
    - "tests/skytime_test_e2e_test.go (3 subprocess E2E tests + ensureTestCmdBinary helper; build tag !windows; 149 LOC)"
    - "docs/for-flow-authors/testing.md (manual H2 reference for tester.workflow / tester.mock_action / Mock function I/O contract / tester.run / assert.* / ${ctx.expr} interpolation / Running tests / See Also; 365 LOC)"
  modified:
    - "pkg/cli/root.go (one-line addition: root.AddCommand(newTestCommand(cfg)) alongside the existing validate/run/dev-server/info siblings)"
    - "docs/reference/cli.md (new ## skytime test section between ## skytime info and ## skytime dev-server; 6-H3 template; 77 lines added)"

key-decisions:
  - "[Plan 06 - D5-runner-cli-adapter] RunCLI is a parallel implementation of Plan 04's *testing.T-bound Run rather than a refactor. Reason: 'Plans 01..05 already committed; do NOT modify them' (planning lock). The runOneFile body in Plan 05 uses subT.Run / subT.Failed / subT.Skipped, which makes it *testing.T-bound at the iteration level. RunCLI duplicates the discovery + iteration loop and invokes tests via *bareReporter (which satisfies starlarktest.Reporter without needing *testing.T's t.Run/t.Failed plumbing). Code cost: ~80-100 LOC in pkg/testing/cli_run.go. Future v1.x can refactor to a shared internal helper without breaking either entry point."
  - "[Plan 06 - D5-docs-builtins-marker-location] Plan 06 ships docs/for-flow-authors/testing.md as the manual source of truth for tester.* and explicitly does NOT touch pkg/parser/builtins.go or docs/reference/builtins.md. Original Plan 06 draft proposed adding doc-anchor declarations to pkg/parser/builtins.go so cmd/skytime-docgen could walk them; this approach was rejected because (a) cmd/skytime-docgen's WalkRegistry is single-file (parses ONLY pkg/parser/globals.go); (b) WalkBuiltins iterates *ast.FuncDecl only and silently skips *ast.GenDecl var declarations; (c) the proposed `_ = identifier` anchor lines are not legal Go at file scope. Multi-file docgen walking is a Phase 6+ concern, deferred. The existing TestDocgenDrift CI gate continues passing because no marker changes occur."
  - "[Plan 06 - D5-firewall-no-change] Plan 06 adds NO firewall edits. pkg/cli is already allowlisted for go.temporal.io/sdk/* and cobra/charm-log (Phase 4 plans 04-04 and 04-05). pkg/testing's allowlist for go.temporal.io/sdk/testsuite was added by Plan 01. pkg/cli/test.go imports pkg/testing — already permitted. tests/firewall_cli_test.go remains untouched. Verified by grep: zero new entries to allowedPkgs / allowedRel slices in this plan's diff."
  - "[Plan 06 design] D5-E4 exit-code mapping in RunE: failed==0 && err==nil → return nil (cobra exit 0); failed>0 → return errSilent (cobra exit 1); err != nil → fmt.Fprintf cmd.ErrOrStderr + return errSilent (cobra exit 1). Bad arg count is intercepted by cobra.ExactArgs(1) BEFORE RunE fires (cobra exit 2). Single mutually-exclusive switch path; mirrors validate/run RunE conventions. cfg.debug gates a brief failure-counts diagnostic on stderr (does NOT alter exit-code semantics)."
  - "[Plan 06 design] Subprocess E2E ensureTestCmdBinary uses a SEPARATE /tmp dir from the run-e2e binary (testCmdBinOnce + testCmdBin package-scope vars). Reason: parallel test invocations (`go test ./tests/...` runs subdirectories concurrently in default mode) would otherwise race on the same /tmp/skytime path. Two distinct sync.Once + tmp paths eliminate the race without any t.Cleanup coordination."
  - "[Plan 06 design] testing.md cites THREE source-of-truth files explicitly: pkg/testing/module.go (the tester Starlark module + builtin registrations), pkg/parser/parser.go::WithTestMode (the parser flag that gates tester.* + assert.* injection), pkg/testing/runner.go (Run + RunCLI entry points). Pattern: every audience-facing reference doc ends with a 'Source-of-truth:' bullet list pointing to specific files (not just package names) so a maintainer reading the doc can grep-locate the canonical implementation."
  - "[Plan 06 design] cli.md `## skytime test` placement: AFTER `## skytime info`, BEFORE `## skytime dev-server`. Logical-grouping rationale: validate→run→info are read-then-write operations against existing flows; test runs flow tests (read-then-mock-then-execute); dev-server is operational tooling for local-only iteration. Test sits closer to the code-author tools (validate/run/info) than to the infra tooling (dev-server)."

patterns-established:
  - "Non-*testing.T reporter adapter (bareReporter) for CLI/library callers that need to drive Starlark-test-style code without the Go testing package — the pattern works for any future caller that wants to invoke the Tier-3 harness from a non-test context (REST endpoint, REPL, scheduled job, etc.)."
  - "Three-layer contract pinning for explicit success-criteria phrases: (a) library layer test, (b) wrapper layer test, (c) subprocess layer test. Same invariant ('no Go stack traces in default output') asserted independently at each layer so a regression in any single layer surfaces locally; CI doesn't have to depend on the slowest layer to detect drift."
  - "ensureBinary-once-per-package pattern (sync.Once + cached bin path + cached err) for subprocess E2E tests — separate cache per testbinary-purpose so parallel test invocations don't race. Reusable for any future cmd/skytime-* binary that needs subprocess testing."
  - "Audience-split documentation routing: docs/for-flow-authors/ is the consultant-facing surface (testing.md, extensions/http.md), docs/reference/ is the auto-generated/manual reference, docs/architecture.md is the cross-audience explainer. New consultant-facing concepts land in docs/for-flow-authors/ FIRST; reference-doc updates either auto-regenerate (docgen) or land manually with the same H1+'Source-of-truth:'+H2 sections shape."

requirements-completed: [CLI-03]
requirements-secondary-coverage: [TEST-01, TEST-02, TEST-03, TEST-04, TEST-05]

# Metrics
duration: ~14min
completed: 2026-05-05
---

# Phase 5 Plan 06: skytime test CLI subcommand + tester.* docs + subprocess E2E Summary

**`pkg/testing.RunCLI` non-*testing.T entry-point + `skytime test <dir>` cobra subcommand + 3 subprocess E2E tests + `docs/for-flow-authors/testing.md` manual reference for `tester.*` + `## skytime test` section in `docs/reference/cli.md` — closes Phase 5 by making CLI-03 reachable from the built binary, with the no-Go-stack-traces invariant pinned at three independent layers (pkg/testing, pkg/cli, subprocess).**

## Performance

- **Duration:** ~14 min (active work; planning context loading included)
- **Completed:** 2026-05-05
- **Tasks:** 3 (all atomic; TDD red-green for the two code tasks; docs-only third task)
- **Files created:** 6
- **Files modified:** 2
- **Insertions:** 1,112 lines (per `git diff --stat 86629ba..HEAD`)

## Accomplishments

- **Task 1 (`pkg/testing.RunCLI` non-*testing.T entry-point):** `pkg/testing/cli_run.go` ships `RunCLI(dir string, opts ...Option) (passed, failed int, err error)` driving the same parser-test-mode + DiscoverTestFiles + DiscoverTests + jsonEmitter pipeline as Plan 04+05's `Run`, but using a synthetic `*bareReporter` instead of `*testing.T`. `bareReporter` satisfies starlarktest.Reporter (`interface{ Error(args ...any) }`) without any test-runtime dependency — the CLI can ingest pass/fail counts and map them to exit codes per D5-E4. `runOneFileCLI` + `runOneTestCLI` mirror Plan 04+05's `runOneFile` + `runOneTest` but bind bareReporter (deviation D5-runner-cli-adapter; ~80 LOC duplication is the cost of NOT modifying Plan 04+05's already-committed code paths). 8 named tests pin happy/fail/mixed paths, JSON format line-delimited records, no-Go-stack-traces (CLI-03 verbatim at the RunCLI layer), `ErrBadFilter` at option time, `--run` regex exclusion, and zero-counts for empty dirs.
- **Task 2 (`skytime test` cobra subcommand + subprocess E2E):** `pkg/cli/test.go` ships `newTestCommand(cfg)` with `Use: "test <dir>"` + `cobra.ExactArgs(1)` + `--run <regex>` + `--format human|json` flags; the `RunE` body calls `testingpkg.RunCLI(dir, opts...)` and translates `(passed, failed, err)` to exit codes per D5-E4 (failed==0 && err==nil → nil; failed>0 → errSilent; err != nil → stderr line + errSilent). `pkg/cli/root.go` gains the one-line `root.AddCommand(newTestCommand(cfg))` alongside validate/run/dev-server/info. `pkg/cli/test_test.go` (6 named tests) covers happy-path, errSilent on failure, `--run` filter, no-Go-stack-traces at the cobra layer, bad `--format` value error path. `tests/skytime_test_e2e_test.go` (3 subprocess E2E tests) builds `cmd/skytime` once via `go build`, exercises happy-path PASS, FAIL exit-1, and `--format=json` end-to-end against the built binary — pinning CLI-03 success-criteria #5 verbatim at the outermost layer.
- **Task 3 (Documentation — manual references):** `docs/for-flow-authors/testing.md` ships as the manual reference for `tester.*` (workflow / mock_action / run) + `assert.*` + `*_test.star` convention + `${ctx.expr}` caveats. Source-of-truth bullets cite `pkg/testing/module.go` (tester module registration) and `pkg/parser/parser.go::WithTestMode` (parser flag gating injection). `docs/reference/cli.md` gains a new `## skytime test` section between `## skytime info` and `## skytime dev-server` using the established 6-H3 template (Synopsis / Motivation / Flags / Exit Codes / Example / See Also) with cross-links to the flow-author guide and to `skytime run`. Per deviation D5-docs-builtins-marker-location, NO changes to `pkg/parser/builtins.go` or `docs/reference/builtins.md` — `cmd/skytime-docgen` is single-file (walks only `pkg/parser/globals.go`) and `tester.*` lives in `pkg/testing/module.go`, outside its reach. Multi-file docgen walking is deferred post-v1; testing.md is the manual source of truth until then. The existing TestDocgenDrift CI gate stays green (verified).

## Task Commits

Each task committed atomically:

1. **Task 1: pkg/testing.RunCLI non-*testing.T entry-point for pkg/cli** — `7118a91` (feat)
2. **Task 2: skytime test cobra subcommand + subprocess E2E** — `004b7d0` (feat)
3. **Task 3: manual testing.md + cli.md `## skytime test` section** — `dbeae97` (docs)

## Files Created/Modified

### Created

- `pkg/testing/cli_run.go` — `func RunCLI(dir string, opts ...Option) (passed, failed int, err error)`, `func runOneFileCLI(...)`, `func runOneTestCLI(...)`, `type bareReporter struct { failed bool; messages []string }`, `func (b *bareReporter) Error(args ...any)`, `func (b *bareReporter) allMessages() string`. Imports: stdlib (`crypto/sha256`, `encoding/hex`, `fmt`, `io`, `os`, `path/filepath`, `strings`, `time`) + `go.starlark.net/starlark` + `go.starlark.net/starlarktest` + `pkg/interpreter` + `pkg/parser`. Does NOT import `testing` (verified — that's the whole point of `bareReporter`).
- `pkg/testing/cli_run_test.go` — 8 named tests + `writeFile` helper.
- `pkg/cli/test.go` — `func newTestCommand(cfg *config) *cobra.Command`. Imports: stdlib (`fmt`) + `github.com/spf13/cobra` + `testingpkg "github.com/mikelalcon/skytime/pkg/testing"`. The `testingpkg` alias avoids shadowing of stdlib `testing` (which doesn't appear here, but the convention is consistent with future test integration that DOES import both).
- `pkg/cli/test_test.go` — 6 named tests + `helperRunTestCmd` + `writeFile` helpers.
- `tests/skytime_test_e2e_test.go` — 3 named tests + `ensureTestCmdBinary` (sync.Once-cached binary build) + `helperWriteTest` (file fixture writer). Package `firewall_test` (matches sibling e2e tests in this dir). Build tag `//go:build !windows`.
- `docs/for-flow-authors/testing.md` — H1 + intro + Source-of-truth bullets + 9 H2 sections covering File-naming convention / tester.workflow / tester.mock_action / Mock function I/O contract / tester.run / assert.* / ${ctx.expr} interpolation in tests / Running tests / See Also.

### Modified

- `pkg/cli/root.go` — single-line addition `root.AddCommand(newTestCommand(cfg))` after the existing `newInfoCommand(cfg)` line. Maintains alphabetical-ish-by-frequency-of-use ordering.
- `docs/reference/cli.md` — new `## skytime test` H2 section (77 LOC) inserted between `## skytime info` and `## skytime dev-server`. 6 H3 sub-sections: Synopsis, Motivation, Flags, Exit Codes, Example, See Also.

## Decisions Made

See frontmatter `key-decisions` for the full list. Most-load-bearing:

1. **D5-runner-cli-adapter — duplicate over refactor.** Plan 04+05's `runOneFile` is *testing.T-bound at the iteration level (`subT.Run` / `subT.Failed`). Two paths considered: (a) refactor `runOneFile` to call a shared internal driver and have `Run` + `RunCLI` both wrap it; (b) keep Plan 04+05 untouched and define `RunCLI` as a parallel implementation reusing `DiscoverTestFiles` / `DiscoverTests` / `MatchRunFilter` / `jsonEmitter` from Plan 05. Plan 06 picked (b) per the planning-context lock that Plans 01..05 are already committed and must not be modified. Cost: ~80 LOC duplication in `runOneFileCLI` + `runOneTestCLI`. Future v1.x can refactor to a shared internal helper without breaking either entry point.

2. **D5-docs-builtins-marker-location — manual docs over docgen integration.** Original Plan 06 draft proposed adding doc-anchor declarations to `pkg/parser/builtins.go` so `cmd/skytime-docgen` could walk them. Three reasons this was rejected: (i) `cmd/skytime-docgen.WalkRegistry` is single-file (parses ONLY `pkg/parser/globals.go`), so anchors in `builtins.go` would never be reached; (ii) `WalkBuiltins` iterates `*ast.FuncDecl` only and silently skips `*ast.GenDecl` var declarations, so anchors as vars would also be invisible; (iii) the proposed `_ = identifier` syntax is not legal Go at file scope. Plan 06 ships `docs/for-flow-authors/testing.md` as the manual source of truth instead. Multi-file docgen walking is a Phase 6+ concern, deferred. The existing TestDocgenDrift CI gate stays green by virtue of NOT touching the docgen-managed files.

3. **D5-E4 exit-code mapping — single switch in RunE.** Three exit codes: 0 (passed), 1 (failed or err), 2 (cobra usage / bad arg count). Cobra owns 2 via `cobra.ExactArgs(1)` BEFORE RunE fires; RunE owns 0/1 via the `(passed, failed, err)` triple from RunCLI. `cfg.debug` gates a brief failure-counts diagnostic on stderr but does NOT alter exit-code semantics. Mirrors validate/run RunE conventions; single mutually-exclusive switch path keeps the contract auditable.

4. **Three-layer CLI-03 contract pinning.** The phrase "no Go stack traces in default output" appears verbatim in CLI-03 success-criteria #5. Plan 06 pins it at THREE independent layers: (a) `TestRunCLI_NoGoStackTracesInFailureOutput` asserts on RunCLI's `WithOutput` buffer; (b) `TestTestCommand_DefaultOutput_NoGoStackTraces` asserts on cobra's combined stdout+stderr buffers; (c) `TestSkytimeTestE2E_FailureExitNonzero` asserts on the built binary's combined output. A regression in any single layer surfaces locally without depending on the slowest layer to detect drift.

5. **ensureTestCmdBinary uses a SEPARATE /tmp dir from the run-e2e binary.** `tests/e2e_skytime_run_test.go` already has `ensureBinary` with its own sync.Once + tmp path. Plan 06 mirrors the pattern but with `testCmdBinOnce` + `testCmdBin` so parallel test invocations don't race on the same /tmp/skytime path. Two distinct sync.Once + tmp paths eliminate the race without any t.Cleanup coordination.

## Deviations from Plan

### Auto-fixed Issues / Plan-Lock Adjustments

The deviations are documented in the plan's `deviations:` frontmatter and were anticipated up-front rather than discovered mid-execution. None required a checkpoint.

**1. [D5-runner-cli-adapter — Plan-lock adjustment, expected]** Plan 04+05's `runOneFile` is *testing.T-bound at the iteration level. RunCLI duplicates ~80 LOC of `runOneFile` + `runOneTest` because the planning context locked Plans 01..05 as committed and forbidden from modification. Documented as the load-bearing decision in the plan; not a surprise.
- **Found:** before plan execution (pre-recognized in `<deviations>` frontmatter).
- **Files affected:** `pkg/testing/cli_run.go` (NEW — `runOneFileCLI` + `runOneTestCLI`).
- **Verification:** all 8 RunCLI tests pass; the existing Plan 04 `Run` path also still passes (no regression on `pkg/testing/...`).
- **Committed in:** `7118a91` (Task 1 commit).

**2. [D5-docs-builtins-marker-location — Plan-correction, found during draft]** Original Plan 06 draft proposed adding doc-anchor declarations to `pkg/parser/builtins.go` and regenerating `docs/reference/builtins.md`. Three issues blocked this approach: (i) docgen is single-file (only `pkg/parser/globals.go`); (ii) the walker iterates `*ast.FuncDecl`, not `*ast.GenDecl` vars; (iii) the proposed `_ = identifier` syntax is not legal Go at file scope. Plan 06 dropped the docgen integration entirely and ships `testing.md` as the manual source of truth.
- **Found:** during plan-drafting verification (before execution).
- **Files affected:** original Plan 06 draft removed `pkg/parser/builtins.go` and `docs/reference/builtins.md` from the file list; the final plan omits both.
- **Verification:** `TestDocgenDrift` CI gate stays green (verified — Plan 06 made zero touches to docgen-managed files).
- **Committed in:** `dbeae97` (Task 3 commit; the docs-only commit's message explicitly notes this deviation).

**3. [D5-firewall-no-change — Meta-deviation, no edits]** Plan 06 makes NO firewall changes. Verified up-front: pkg/cli is already allowlisted for `go.temporal.io/sdk/*` (Phase 4 plan 04-05) and for cobra/charm-log (Phase 4 plan 04-04); pkg/testing's allowlist for `go.temporal.io/sdk/testsuite` was added by Plan 01; `pkg/cli/test.go` imports `pkg/testing` — already permitted. `tests/firewall_cli_test.go` and `pkg/activity/firewall_test.go` are untouched in this plan's diff.
- **Verification:** `go test -race -count=1 ./tests -run TestNoCobraImportsOutsideAllowList` exits 0; `go test -race -count=1 ./pkg/activity -run TestNoTemporalImports` exits 0.

---

**Total deviations:** 3 documented (1 plan-lock adjustment expected; 1 plan-correction found during drafting; 1 meta-deviation noting no edits needed).
**Impact on plan:** No scope creep. The duplication in `cli_run.go` is the cost of NOT modifying Plans 04+05; the docgen sidestep was caught before execution and the plan was corrected in-place. All other code/docs landed exactly as the (corrected) plan specified.

## Issues Encountered

None blocking. All three deviations were pre-recognized in the plan's `<deviations>` frontmatter and required no checkpoint.

## User Setup Required

None — no external service configuration required. The `tests/skytime_test_e2e_test.go` E2E builds `cmd/skytime` via `go build` from the test process; no Temporal server required (Tier-3 harness uses `testsuite.TestWorkflowEnvironment` in-process).

## Next Phase Readiness

- **Phase 5 closed.** All 6 plans (05-01..05-06) are complete. CLI-03 is reachable end-to-end from `git clone && go build ./cmd/skytime && skytime test <dir>` and produces Starlark-callsite-aware pass/fail with no Go stack traces.
- **Phase 6 (Example Project — HTTP + GitHub + Slack):** ready. The example project's `examples/http-github-slack/` `.star` flows + `*_test.star` test files can be exercised via `skytime test examples/http-github-slack/` once they ship. The `pkgtesting.Run(t *testing.T, dir)` Go-level entry point also remains available for in-process Go-test integration if the example project wants to wire its tests into `go test ./examples/...`.
- **Multi-file docgen walking deferred (post-v1).** When `cmd/skytime-docgen` gains the ability to walk multiple Go files (`pkg/parser/globals.go` + `pkg/testing/module.go` + future extension modules), tester.* doc anchors will become viable and `docs/for-flow-authors/testing.md` can either be auto-generated or kept manual with the docgen output supplanting the current free-prose H2 sections. Until then, testing.md is the source of truth.
- **No blockers.** Phase 5 is shippable.

## Self-Check: PASSED

Verified file-existence, content markers, and commit-presence for every claim in this Summary.

**Files created (verified via `[ -f path ]`):**

- `pkg/testing/cli_run.go` — FOUND (210 LOC)
- `pkg/testing/cli_run_test.go` — FOUND (129 LOC)
- `pkg/cli/test.go` — FOUND (81 LOC)
- `pkg/cli/test_test.go` — FOUND (100 LOC)
- `tests/skytime_test_e2e_test.go` — FOUND (149 LOC)
- `docs/for-flow-authors/testing.md` — FOUND (365 LOC)

**Files modified:**

- `pkg/cli/root.go` — contains `root.AddCommand(newTestCommand(cfg))`
- `docs/reference/cli.md` — contains new `## skytime test` H2 section between `## skytime info` and `## skytime dev-server`

**Commits (verified via `git log --oneline | grep`):**

- `7118a91` feat(05-06): pkg/testing.RunCLI non-*testing.T entry-point for pkg/cli — FOUND
- `004b7d0` feat(05-06): skytime test cobra subcommand + subprocess E2E — FOUND
- `dbeae97` docs(05-06): manual testing.md + cli.md `## skytime test` section — FOUND

**Test gates:**

- `go test -race ./pkg/testing/... ./pkg/cli/... ./tests/...` → all packages OK (pkg/testing 2.03s, pkg/cli 10.05s, tests 11.17s)
- `go vet ./...` → clean (no findings)
- `TestRunCLI_HappyPath_ReturnsPassedOne` → PASS
- `TestRunCLI_FailPath_ReturnsFailedOne` → PASS
- `TestRunCLI_Mixed_ReturnsBothCounts` → PASS
- `TestRunCLI_JSONFormat_LineDelimitedRecords` → PASS
- `TestRunCLI_NoGoStackTracesInFailureOutput` → PASS (CLI-03 layer (a))
- `TestRunCLI_BadRunFilter_ReturnsErrAtOptionTime` → PASS
- `TestRunCLI_RunFilter_ExcludesNonMatching` → PASS
- `TestRunCLI_NoTestFiles_ZeroCountsNoError` → PASS
- `TestNewTestCommand_UseString` → PASS
- `TestTestCommand_HappyPath_ReturnsNil` → PASS
- `TestTestCommand_FailExitsViaErrSilent` → PASS
- `TestTestCommand_RunFilter` → PASS (VALIDATION cite)
- `TestTestCommand_DefaultOutput_NoGoStackTraces` → PASS (CLI-03 layer (b))
- `TestTestCommand_BadFlagFormat_Error` → PASS
- `TestSkytimeTestE2E_HappyPath` → PASS (subprocess; CLI-03 success-criteria #5)
- `TestSkytimeTestE2E_FailureExitNonzero` → PASS (CLI-03 layer (c))
- `TestSkytimeTestE2E_JSONFormat` → PASS (subprocess; D5-E2)
- `TestDocgenDrift` (existing CI gate) → PASS (no docgen-managed files modified)

---
*Phase: 05-tier-3-e2e-test-harness-temporal-test*
*Completed: 2026-05-05*
