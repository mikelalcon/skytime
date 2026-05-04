---
phase: quick-260504-jtr
plan: 01
subsystem: cli
tags: [cobra, error-rendering, ux, skytime-binary]

# Dependency graph
requires:
  - phase: 04-static-validation-tier-cli-skeleton
    provides: D4-18 renderer-owns-output / cobra-owns-exit split (SilenceErrors=true root + errSilent sentinel in validate/run/dev-server)
provides:
  - cli.RenderRootError(out, err) bool helper for top-level cobra errors
  - cli.ErrAlreadyRendered exported alias of package-private errSilent
  - .star-path heuristic suggesting `skytime run <file>` / `skytime validate <file>` on unknown-command errors
  - 5 regression tests pinning bare-invocation, unknown non-.star, unknown .star, wrap-aware sentinel skip, and validate happy-path passthrough
affects: [phase-05-e2e-test-harness, phase-06-real-example-project, future-cli-subcommand-additions]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "(io.Writer, error) → bool helper signature: keeps RenderRootError trivially testable without threading *cobra.Command across the API; mirrors render.go's renderError(out, err, debug) shape"
    - "Sentinel re-export via exported alias (ErrAlreadyRendered = errSilent): preserves lowercase in-package use while giving external callers a stable handle without renaming every internal call site"
    - "Fresh-root-for-UsageString trick: build NewRootCommand() (no opts) inside the helper to obtain a stable usage block — usage is identical across Options, so no live-tree threading required"

key-files:
  created: []
  modified:
    - pkg/cli/root.go - added ErrAlreadyRendered export + RenderRootError helper + extractFirstQuoted parser
    - pkg/cli/root_test.go - added executeRootCapture helper + 5 TestRootCommand_* regression tests
    - cmd/skytime/main.go - wired cli.RenderRootError(os.Stderr, err) before os.Exit(1) on root.ExecuteContext failure

key-decisions:
  - "Quick 260504-jtr: Detection of cobra unknown-command errors uses `strings.HasPrefix(msg, \"unknown command \")` + first-quoted-token extraction — cobra's args.go:36 emits this via plain fmt.Errorf, NOT a typed error, so string-prefix matching is the only contract-stable hook"
  - "Quick 260504-jtr: `.star` suggestion fires on case-insensitive suffix match against the offending arg (case-insensitive because Windows users can type `.STAR`); both `skytime run` AND `skytime validate` are surfaced because either is plausible without context"
  - "Quick 260504-jtr: ErrAlreadyRendered = errSilent exported alias (NOT a rename) — preserves all in-package errSilent call sites in run.go/dev_server.go/validate.go verbatim while giving cmd/skytime a stable handle; future renames stay in pkg/cli only"
  - "Quick 260504-jtr: D4-18 invariant locked verbatim — SilenceErrors and SilenceUsage stay true, no RunE on root, no flip of cobra's default error path. The new helper composes WITH the existing errSilent flow, not against it"

patterns-established:
  - "RenderRootError pattern: top-level CLI errors get a single helper that knows the package's silent-sentinel + emits Error: prefix + (optional) custom suggestion + cobra usage block. Reusable shape for any future CLI surface that adopts the SilenceErrors+sentinel split"
  - "extractFirstQuoted: %q-aware first-token extractor for parsing fmt.Errorf output. Useful any time a string-shaped error needs the first quoted argument recovered without typed-error access"

requirements-completed: [QUICK-260504-jtr]

# Metrics
duration: 3m16s
completed: 2026-05-04
---

# Quick 260504-jtr: Make root `skytime` command print proper errors Summary

**Cobra root errors now render with `Error:` prefix + cobra usage block to stderr; `.star`-suffix args additionally get a `did you mean: skytime run/validate <file>` hint, replacing the previous silent exit 1 from `skytime path/to/flow.star ...`.**

## Performance

- **Duration:** 3m 16s
- **Started:** 2026-05-04T18:21:06Z
- **Completed:** 2026-05-04T18:24:22Z
- **Tasks:** 1
- **Files modified:** 3

## Accomplishments

- Eliminated the silent-exit-1 surprise from `skytime <unknown-arg>`: stderr now surfaces `Error: unknown command "<arg>" for "skytime"` plus the cobra usage block, exit status preserved at 1.
- First-time consultants typing the natural-but-wrong `skytime examples/foo.star --flow x ...` (Pythonic muscle memory) get an actionable `did you mean: skytime run examples/foo.star / skytime validate examples/foo.star` suggestion before the usage block.
- D4-18 architectural invariant preserved verbatim — SilenceErrors=true on root, errSilent sentinel still drives validate/run/dev-server already-rendered paths, no double-render of usage on validate failures.
- Five regression tests pin the contract: bare invocation (help to stdout, exit 0), unknown non-.star (Error+usage, no suggestion), unknown .star (Error+suggestion+usage), wrap-aware ErrAlreadyRendered skip (no double-render through errors.Is), validate happy path unaffected.

## Task Commits

Each task was committed atomically:

1. **Task 1: Render unknown-command errors with .star path suggestion + wire into main** - `25f99d8` (fix)

_Note: TDD task — test file additions and implementation+wiring landed in a single fix commit because the test file modification, helper implementation, and main.go wiring are mutually load-bearing (tests fail to compile without RenderRootError; main.go gives no observable behavior without RenderRootError). Splitting would have produced an intermediate broken state with no value._

## Files Created/Modified

- `pkg/cli/root.go` - Added `ErrAlreadyRendered` exported alias + `RenderRootError(out io.Writer, err error) bool` helper + `extractFirstQuoted(s string) string` private parser
- `pkg/cli/root_test.go` - Added `executeRootCapture(t, args)` helper + 5 regression tests (bare invocation, unknown non-.star, unknown .star, wrap-aware sentinel skip, validate happy-path passthrough)
- `cmd/skytime/main.go` - Routed root.ExecuteContext error through `cli.RenderRootError(os.Stderr, err)` before `os.Exit(1)`; comment updated to reflect the new wiring

## Decisions Made

- **String-prefix detection over typed error:** Cobra's `unknown command %q for %q` error is emitted via plain `fmt.Errorf` in `cobra@v1.10.2/args.go:36`, not as a typed error. The detection contract is `strings.HasPrefix(err.Error(), "unknown command ")`. Acceptable because cobra is pinned by go.mod and the format has been stable across multiple cobra majors.
- **Both run AND validate surfaced in suggestion:** A user invoking `skytime <file>.star` could plausibly mean either dispatch (run on Temporal) or static validation. Listing both lets the user disambiguate without us guessing wrong; the alphabetical order (run, validate) matches the cobra command-list ordering for visual consistency.
- **Fresh-root-for-UsageString approach over threading the live root:** RenderRootError builds a fresh `NewRootCommand()` (no opts) to obtain `UsageString()` rather than accepting a `*cobra.Command` parameter. Cheap, keeps the API trivially testable, and the usage block is identical regardless of which Options the original was constructed with — Options affect handler/extension wiring, not the cobra command tree shape.
- **Exported alias instead of renaming errSilent:** `ErrAlreadyRendered = errSilent` lets external callers compose without a mass rename of the 13+ in-package `return errSilent` sites in run.go/dev_server.go/validate.go. Forward-compatible: a future major can rename if the in-package style ever shifts.

## Deviations from Plan

None - plan executed exactly as written.

(The plan was a single TDD task with a precise contract — RED tests landed first, GREEN came from the spec'd implementation verbatim, no auto-fix triggers fired. The bare-invocation test verified that cobra's default-when-no-RunE behavior was preserved without code changes, exactly as the plan's `<bug_repro>` block predicted.)

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required. Behavior change is internal to the CLI binary.

## Next Phase Readiness

- The CLI now matches user expectations end-to-end: bare invocation, --help, valid subcommands, unknown subcommands, and unknown .star paths all produce output appropriate to their case.
- The `RenderRootError` pattern is reusable: any future CLI surface adopting the SilenceErrors+sentinel split can plug into the same helper. Phase 5 (E2E test harness) and Phase 6 (real example project) — both of which exercise the CLI as users do — inherit the fix automatically.
- No blockers, no concerns.

## Self-Check: PASSED

**Files verified to exist:**
- pkg/cli/root.go — FOUND
- pkg/cli/root_test.go — FOUND
- cmd/skytime/main.go — FOUND
- .planning/quick/260504-jtr-make-root-skytime-command-print-proper-e/260504-jtr-PLAN.md — FOUND

**Commits verified to exist:**
- 25f99d8 — FOUND (`fix(quick-260504-jtr): render root cobra errors with .star path suggestion`)

**Verification results:**
- `go test ./pkg/cli/... -count=1 -race` — PASSED (8.84s)
- `go test ./tests/... -count=1 -race` — PASSED (3.72s)
- `go test ./... -count=1` — ALL PACKAGES OK
- `go vet ./...` — clean
- `go build ./cmd/skytime` — exit 0
- Manual repro `go run ./cmd/skytime examples/skeleton/simple_check.star` — exit 1, stderr contains `Error: unknown command`, `did you mean:`, `skytime run examples/skeleton/simple_check.star`, `skytime validate examples/skeleton/simple_check.star`, and the cobra `Usage:` block. VERIFY_OK
- Bare `go run ./cmd/skytime` — exit 0, help block on stdout, stderr empty (regression-pinned)
- `go run ./cmd/skytime --help` — exit 0
- `go run ./cmd/skytime help validate` — exit 0
- `go run ./cmd/skytime completion bash` — exit 0
- `go run ./cmd/skytime validate /nonexistent.star` — exit 1, validator's `read "/nonexistent.star"...` message rendered once (no double-render of usage block — `ErrAlreadyRendered` skip working)

---
*Phase: quick-260504-jtr*
*Completed: 2026-05-04*
