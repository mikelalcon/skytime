---
phase: 04-static-validation-tier-cli-skeleton
plan: 01
subsystem: infra
tags: [cobra, charm-log, starlark, ast, firewall, tdd, go-modules]

# Dependency graph
requires:
  - phase: 01-type-spine-extension-contract-parser-bridge-foundations
    provides: dag.ValidationError, Parser, fileBytes cache
  - phase: 02-generic-activity-block-batch-dispatch-credentials
    provides: tools.go anchor pattern, AST firewall test pattern (pkg/activity/firewall_test.go)
  - phase: 03-lambda-serialization-decision-interpreter-worker
    provides: Parser.Lambdas() / Parser.Flows() accessor pattern
provides:
  - "go.mod entries for cobra v1.10.2, charm.land/log/v2 v2.0.0, golang.org/x/term — anchored via tools.go"
  - "ValidationError.Action field + D4-04 [flow > step > action] bracket rendering"
  - "Parser.FileBytes() accessor exposing the per-parser fileBytes cache (load-bearing for W1 AST re-parse)"
  - "tests/firewall_cli_test.go cross-tree firewall blocking cobra/pflag/charm-log outside pkg/cli + non-vacuous skip-pattern meta-test"
  - "Empty package skeletons at pkg/cli, pkg/validator, pkg/extension/builtin/http (doc.go only)"
affects: [04-02, 04-03, 04-04, 04-05, 04-06, 04-07, 06]

# Tech tracking
tech-stack:
  added:
    - "github.com/spf13/cobra v1.10.2 (CLI command tree — pkg/cli only)"
    - "charm.land/log/v2 v2.0.0 (slog handler — pkg/cli only; module renamed from github.com/charmbracelet/log/v2)"
    - "golang.org/x/term v0.41.0 (TTY detection)"
  patterns:
    - "tools.go build-tag anchor extended for CLI deps (Phase 2 idiom replicated)"
    - "[flow > step > action] error rendering with empty-segment-drop logic"
    - "AST re-parse via FileBytes() accessor for syntax-tree recovery (workaround for *starlark.Function not retaining AST)"

key-files:
  created:
    - "pkg/cli/doc.go"
    - "pkg/validator/doc.go"
    - "pkg/extension/builtin/http/doc.go"
    - "tests/firewall_cli_test.go"
  modified:
    - "go.mod (3 new direct requires)"
    - "go.sum"
    - "tools.go (3 new anchor imports)"
    - "pkg/dag/errors.go (Action field + Error() update)"
    - "pkg/dag/errors_test.go (legacy regex updated for D4-04 bracket + new TestValidationError_FormatWithAction with 6 subtests)"
    - "pkg/parser/parser.go (FileBytes() accessor)"
    - "pkg/parser/parser_test.go (TestParser_FileBytes_PopulatedAfterParseSource)"
    - "pkg/parser/fixtures_test.go (posFormatRe regex updated for optional [...] segment)"

key-decisions:
  - "charm-log module path: used charm.land/log/v2 instead of plan's github.com/charmbracelet/log/v2 — upstream rename happened after 04-RESEARCH was written; module path is the only difference (same source, same v2.0.0 tag)"
  - "tools.go anchor extension over deferring deps to W3 — preserves the W0 success criterion that all three CLI deps appear in go.mod immediately, without requiring pkg/cli to ship code in W0"
  - "Legacy posFormatRe regex broadened (vs. fully replaced) — matches the documented optional [...] segment so future error-format additions need only the bracket; preserves existing fixture-test intent"
  - "Meta-test TestPkgCli_ImportsCobra forbids only cobra (not all three) — keeps the meta-test tight: cobra is the central CLI concern; charm-log/term arrive transitively via pkg/cli imports"

patterns-established:
  - "Pattern: Module-rename handling — when an upstream module is renamed, document the rename (in tools.go and the firewall forbidden list) rather than fork-pinning the old path; charm.land/log is github.com/charmbracelet/log under a new module identity"
  - "Pattern: Skip-on-empty meta-test — tests/firewall_cli_test.go's TestPkgCli_ImportsCobra mirrors pkg/worker/firewall_test.go's TestPkgWorker_ImportsTemporal: skip when production sources don't exist or don't yet import the gated dep, full assertion once they do"
  - "Pattern: ValidationError bracket rendering — segment slice + strings.Join + drop-empty produces the [flow > step > action] format with O(1) special cases (segment count 0, 1, 2, 3) collapsed into one branch"

requirements-completed: [VAL-03, CLI-05]

# Metrics
duration: 5min
completed: 2026-05-01
---

# Phase 4 Plan 01: Wave 0 Foundations Summary

**Wave 0 ships the cobra/charm-log/x-term go.mod anchors, ValidationError.Action field with [flow > step > action] rendering, Parser.FileBytes() accessor for AST re-parse, and the cross-tree firewall blocking CLI deps outside pkg/cli — every later Phase 4 wave depends on these load-bearing primitives.**

## Performance

- **Duration:** ~5 min
- **Started:** 2026-05-01T19:49:36Z
- **Completed:** 2026-05-01T19:55:00Z (approx)
- **Tasks:** 3 (TDD: 5 commits across RED/GREEN steps)
- **Files modified:** 10 (4 created, 6 modified)

## Accomplishments

- **CLI deps in go.mod with idempotent tidy** — cobra v1.10.2, charm.land/log/v2 v2.0.0, golang.org/x/term v0.41.0 all present as direct requires, anchored via tools.go so `go mod tidy` doesn't prune them while pkg/cli is still empty.
- **D4-04 ValidationError.Action field + new Error() formatting** — bracket appears only when at least one of Flow/Step/Action is non-empty, segments joined with " > ", legacy callers (no Flow/Step/Action) keep the original `<file>:<line>:<col>: <msg>` shape.
- **Parser.FileBytes() exposed** — the load-bearing accessor for plan 04-02's AST re-parse path. Documented why this is needed (RESEARCH §Pattern 3 critical finding: *starlark.Function does not retain its AST after compilation).
- **Cross-tree firewall test live** — tests/firewall_cli_test.go walks all 662 import statements under pkg/ and asserts none imports cobra/pflag/charm-log outside pkg/cli. Companion meta-test skips cleanly while pkg/cli is empty (mirrors pkg/worker/firewall_test.go's pattern from Phase 3).
- **Three empty packages bootstrapped** — pkg/cli, pkg/validator, pkg/extension/builtin/http exist as importable packages with package-level doc comments, so W1+ cross-package imports resolve.
- **All existing tests still pass** — full `go test ./...` green; two legacy assertions updated for the new D4-04 bracket format.

## Task Commits

Each task was committed atomically; TDD tasks landed RED then GREEN.

1. **Task 1: CLI deps + empty package skeletons** — `749f8e1` (chore)
2. **Task 2: ValidationError.Action field**
   - RED: `9dd84ab` (test) — TestValidationError_FormatWithAction with 6 subtests, fails to compile
   - GREEN: `6c47aa6` (feat) — Action field + Error() rewrite + legacy regex update
3. **Task 3: Parser.FileBytes() + cobra firewall**
   - RED: `0feb006` (test) — TestParser_FileBytes_PopulatedAfterParseSource (build fail) + tests/firewall_cli_test.go
   - GREEN: `ac2fd93` (feat) — FileBytes() accessor

**Plan metadata commit:** TBD (final commit captures SUMMARY + STATE + ROADMAP).

## Files Created/Modified

- `pkg/cli/doc.go` — Package skeleton; documents the cobra/pflag/charm-log allow-list rationale (D4-13)
- `pkg/validator/doc.go` — Package skeleton; documents Wave 2 surface (Validate facade + dryrun mock)
- `pkg/extension/builtin/http/doc.go` — Package skeleton; documents D4-14 op surface and idempotence table
- `tests/firewall_cli_test.go` — TestNoCobraImportsOutsideAllowList (firewall) + TestPkgCli_ImportsCobra (skip-on-empty meta-test) + findModuleRootCLI helper
- `go.mod`, `go.sum` — 3 new direct requires + 16 transitive deps from charm-log
- `tools.go` — 3 new anchor imports under build-tag `tools` so deps survive `go mod tidy`
- `pkg/dag/errors.go` — Action string field; Error() rewrite using segment-slice + bracket-when-any-set
- `pkg/dag/errors_test.go` — TestValidationError_FormatWithAction (6 subtests); legacy TestValidationError_ErrorWithValidPos regex broadened to expect bracket
- `pkg/parser/parser.go` — FileBytes() accessor returning the live fileBytes map
- `pkg/parser/parser_test.go` — TestParser_FileBytes_PopulatedAfterParseSource (white-box; uses NewParser directly per package convention)
- `pkg/parser/fixtures_test.go` — posFormatRe regex broadened to accept optional `[...]` segment between col and trailing colon (so existing invalid-fixture tests continue to assert position-prefix presence under the D4-04 format)

## Decisions Made

- **Module path correction**: Plan referenced `github.com/charmbracelet/log/v2` but upstream renamed the module to `charm.land/log/v2` (same v2.0.0 tag, same source repo on GitHub). Used the new path everywhere; documented in tools.go, pkg/cli/doc.go, and tests/firewall_cli_test.go's forbidden list.
- **tools.go anchor over conditional deferral**: rather than skipping cobra/charm-log requires until W3 lands code that imports them, extended the existing tools.go anchor pattern (Phase 2 idiom for `go.temporal.io/sdk/activity`) to all three CLI deps. Preserves the W0 acceptance criteria literally and ensures `go mod tidy` is idempotent from this commit forward.
- **Legacy assertion broadening over replacement**: rather than removing the Phase 1 `TestValidationError_ErrorWithValidPos` assertion (which expected the old no-bracket format), updated its regex to expect the new bracket. The dual-test approach (broad regex in fixtures_test.go for many fixtures + tight regex in unit tests for specific shapes) is preserved.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] charm-log module path renamed upstream**
- **Found during:** Task 1 (`go get github.com/charmbracelet/log/v2@v2.0.0` failed)
- **Issue:** Upstream module renamed from `github.com/charmbracelet/log/v2` to `charm.land/log/v2`; the GitHub repo's `go.mod` declares the new path, so `go get` rejects the old path with `module declares its path as: charm.land/log/v2 / but was required as: github.com/charmbracelet/log/v2`.
- **Fix:** Used `charm.land/log/v2@v2.0.0` everywhere — the firewall forbidden list, the tools.go anchor, and pkg/cli/doc.go's documentation. The version (v2.0.0), source content, and Go API are identical to what the plan/research expected.
- **Files modified:** go.mod, go.sum, tools.go, pkg/cli/doc.go, tests/firewall_cli_test.go
- **Verification:** `go build ./...` passes; `grep -E '^[[:space:]]*charm.land/log/v2 v2\.' go.mod` matches.
- **Committed in:** 749f8e1

**2. [Rule 3 - Blocking] tools.go extension required to keep deps in go.mod**
- **Found during:** Task 1 (after `go get` + `go mod tidy`, the three CLI deps were pruned because no production code imports them)
- **Issue:** `go mod tidy` removes direct requires that aren't transitively imported by any non-test, non-build-tagged source. The plan's W0 acceptance criterion `grep -E '^[[:space:]]*github.com/spf13/cobra v1\.(10|1[1-9])' go.mod` would fail.
- **Fix:** Extended the existing `tools.go` build-tag-anchored file (a Phase 2 idiom for the same problem with `go.temporal.io/sdk/activity`) to anchor all three CLI deps. Documented the rationale in the file's comment and noted "remove once pkg/cli ships in W3".
- **Files modified:** tools.go
- **Verification:** Snapshot-based idempotency check: `cp go.mod /tmp/go.mod.before && cp go.sum /tmp/go.sum.before && go mod tidy && diff` shows zero diff. All three deps remain in go.mod.
- **Committed in:** 749f8e1

**3. [Rule 1 - Bug] Legacy regex assertions broke under D4-04 bracket**
- **Found during:** Task 2 (full `go test ./...` after GREEN)
- **Issue:** Two pre-existing tests asserted regexes anchored to the old `<file>:<line>:<col>: <msg>` format — but they populated `Flow` (and sometimes `Step`), so the new D4-04 format adds `[flow > step]` between col and the trailing colon. Specifically:
  - `pkg/dag/errors_test.go::TestValidationError_ErrorWithValidPos`: hard-coded `^[^:]+:\d+:\d+: missing required 'title'$` — did NOT allow a bracket.
  - `pkg/parser/fixtures_test.go::TestInvalidFixtures` via `posFormatRe = regexp.MustCompile(`^[^:]+:\d+:\d+: `)` — did NOT allow a bracket. Two fixtures (`09-mixed-idempotency.star`, `10-block-oversized.star`) populate Flow.
- **Fix:**
  - errors_test: regex updated to `^[^:]+:\d+:\d+ \[approve_pr > create_issue\]: missing required 'title'$` (matches what D4-04 actually produces for the test's ValidationError shape).
  - fixtures_test: `posFormatRe` broadened to `^[^:]+:\d+:\d+(?: \[[^\]]+\])?: ` — accepts an optional [...] segment between col and the trailing colon. This is forward-compatible: future D4-04 callers populating Flow/Step/Action all match without further regex churn.
  - The "no-bracket" pure case is now covered by `TestValidationError_FormatWithAction/none_set_with_valid_pos_drops_bracket_entirely`.
- **Files modified:** pkg/dag/errors_test.go, pkg/parser/fixtures_test.go
- **Verification:** `go test ./... -count=1` green across all packages.
- **Committed in:** 6c47aa6 (bundled with the GREEN feat commit, since the legacy assertions are part of the spec change)

---

**Total deviations:** 3 auto-fixed (2 blocking — module rename + tidy pruning; 1 bug — legacy regex assertions)
**Impact on plan:** No scope creep. All three deviations are mechanically required by the new spec or by upstream changes that happened after RESEARCH was written. The plan's intent (deps anchored, D4-04 bracket live, no regressions) is fully preserved.

## Issues Encountered

- **`go mod tidy` upgraded toolchain from `go 1.25.0` to `go 1.25.8`** when charm-log was added. This is the auto-toolchain-bump behavior introduced in Go 1.21. The 1.25 floor is preserved (still 1.25.x); the bump is a minor patch tracking the latest stable 1.25 release. Acceptable per CLAUDE.md (toolchain `go1.26.2` mentioned, declare `go 1.25` in go.mod) — 1.25.x stays compatible with 1.25 consumers.
- **`golang.org/x/term` reverted to v0.41.0** during `go mod tidy` (down from v0.42.0 fetched by `go get @latest`). Go's MVS picks the minimum sufficient version once tidy runs; v0.41.0 is what the Temporal SDK already requires transitively, so MVS resolves there. No functional difference.

## Next Phase Readiness

- **W1 unblocked:** Parser.FileBytes() in place; plan 04-02 (`ctx.<name>` AST walker) can begin re-parsing cached source bytes via `syntax.Parse` to recover lambda AST nodes.
- **W2 unblocked:** pkg/validator package exists; plan 04-03 (Validate facade) can land Go code there without "package not found" errors at import time.
- **W3 unblocked:** pkg/cli package exists; plan 04-04 (cobra root command) can import cobra without firewall violations. The firewall test will start enforcing the rule the moment pkg/cli's first source file lands.
- **W4 unblocked:** pkg/extension/builtin/http exists for the baked-in HTTP extension.
- **D4-04 surface ready:** all downstream lints can populate Action when the offending error scopes to a single action ref; the renderer (CLI W3) reads Error() directly.
- **No blockers for Phase 4 progression.**

## Self-Check: PASSED

**Files verified (8/8 exist):**
- pkg/cli/doc.go
- pkg/validator/doc.go
- pkg/extension/builtin/http/doc.go
- tests/firewall_cli_test.go
- pkg/dag/errors.go (modified)
- pkg/parser/parser.go (modified)
- go.mod (modified)
- tools.go (modified)

**Commits verified (5/5 exist in git log):**
- 749f8e1 (Task 1)
- 9dd84ab (Task 2 RED)
- 6c47aa6 (Task 2 GREEN)
- 0feb006 (Task 3 RED)
- ac2fd93 (Task 3 GREEN)

All success criteria met; no missing artifacts.

---
*Phase: 04-static-validation-tier-cli-skeleton*
*Completed: 2026-05-01*
