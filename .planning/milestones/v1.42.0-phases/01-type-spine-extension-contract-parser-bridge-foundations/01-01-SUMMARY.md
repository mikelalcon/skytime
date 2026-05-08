---
phase: 01-type-spine-extension-contract-parser-bridge-foundations
plan: 01
subsystem: infra
tags: [go, go-modules, go.starlark.net, testify, syntax.Position, error-types, fixtures]

requires: []
provides:
  - Go 1.25.9 toolchain installed via Homebrew (go@1.25)
  - go.mod at module path github.com/mikelalcon/skytime, go 1.25 floor
  - Pinned dependencies: go.starlark.net@v0.0.0-20260326113308-fadfc96def35, testify@v1.11.1
  - Empty package skeletons: pkg/dag, pkg/extension, pkg/parser, pkg/bridge (each with doc.go)
  - Typed error spine in pkg/dag: ParseError + ValidationError with Position(), Error() formatting <file>:<line>:<col>: <msg>, Unwrap()
  - Compile-time interface assertions pinning the error contract
  - 8 valid + 2 helper .star fixtures + 2 golden JSON skeletons
  - 8 invalid .star fixtures with `# expects:` headers (the contract plan 05's TestInvalidFixtures depends on)
affects:
  - 01-02 (pkg/dag types — imports same package)
  - 01-03 (pkg/extension SDK — imports dag.ValidationError)
  - 01-04 (pkg/parser, pkg/bridge — imports dag.ParseError)
  - 01-05 (parser integration — reads fixtures, regenerates 02-all-primitives.golden.json)

tech-stack:
  added:
    - "go.starlark.net (Google Starlark interpreter, no semver tags — pseudo-version 2026-03-26)"
    - "github.com/stretchr/testify v1.11.1 (require/assert helpers)"
  patterns:
    - "Typed errors with Position() method — implements error + interface{ Position() syntax.Position }, Unwrap() for errors.As"
    - "Errors live in pkg/dag (not pkg/parser per RESEARCH sketch) — avoids circular imports across the four foundation packages"
    - "Compile-time interface assertions at file scope (var _ error = (*ParseError)(nil)) make the contract reviewable without running tests"
    - "Test fixtures organized as tests/fixtures/{valid,invalid}/NN-name.star with `# expects: <substring>` header on every invalid file"

key-files:
  created:
    - "go.mod (module identity + pinned deps)"
    - "go.sum (dep checksums)"
    - ".gitignore (Go build artifacts, editor, OS files)"
    - "pkg/dag/doc.go"
    - "pkg/dag/errors.go (ParseError + ValidationError)"
    - "pkg/dag/errors_test.go (10 tests, all passing)"
    - "pkg/extension/doc.go"
    - "pkg/parser/doc.go"
    - "pkg/bridge/doc.go"
    - "tests/fixtures/valid/01-minimal-flow.star + .golden.json"
    - "tests/fixtures/valid/02-all-primitives.star + .golden.json (skeleton — finalized in plan 05)"
    - "tests/fixtures/valid/03-multi-flow-per-file.star"
    - "tests/fixtures/valid/04-load-relative.star + 04-load-target.star"
    - "tests/fixtures/valid/05-load-absolute.star"
    - "tests/fixtures/valid/06-call-flow-cross-file.star + 06-call-flow-helper.star"
    - "tests/fixtures/invalid/01..08 (8 files, each with `# expects:` header)"
  modified: []

key-decisions:
  - "Errors placed in pkg/dag (not pkg/parser) so all four foundation packages can construct/return them without a circular import — deliberate departure from RESEARCH.md sketch"
  - "Go toolchain auto-rewrote `go 1.25` directive to `go 1.25.0` (Go 1.21+ behavior); accepted since the 1.25 floor (not 1.26+) per D-05 is preserved"
  - "Golden JSON for 02-all-primitives is a placeholder skeleton with `_note` marker — plan 05 will run with UPDATE_GOLDEN=1 once pkg/dag JSON marshaling is locked"
  - "Used Homebrew go@1.25 (1.25.9) — matches D-05 'go.starlark.net forces 1.25 floor' and matches STACK.md recommended floor"

patterns-established:
  - "TDD workflow: failing test (RED) authored before implementation (GREEN); compile-time + runtime assertions both used"
  - "Each invalid fixture has `# expects: <substring>` on line 1 — establishes a stable contract for plan 05's TestInvalidFixtures"
  - "Module-level interface assertions (`var _ error = (*X)(nil)`) — preferred over runtime type-asserts for surface-area locks"

requirements-completed: [DSL-10, PARSE-05]

duration: 5min
completed: 2026-04-27
---

# Phase 01 Plan 01: Type Spine + Extension Contract + Parser/Bridge Foundations — Wave 0 Scaffolding Summary

**Go 1.25.9 module initialized with pinned go.starlark.net + testify, four empty foundation packages, ParseError/ValidationError type spine in pkg/dag with Position()/Unwrap()/format-when-valid semantics, and 18 .star + 2 golden JSON fixture files that lock the corpus contract for plan 05.**

## Performance

- **Duration:** ~5 min
- **Started:** 2026-04-27T16:10:02Z
- **Completed:** 2026-04-27T16:14:28Z
- **Tasks:** 2 (both completed atomically)
- **Files created:** 27 (9 Go/config + 18 test fixtures)

## Accomplishments

- Toolchain upgraded from Go 1.21.0 to Go 1.25.9 (via `brew install go@1.25`); module path locked at `github.com/mikelalcon/skytime`
- Typed error spine landed in `pkg/dag` — both `*ParseError` and `*ValidationError` implement `error`, expose `Position() syntax.Position`, format as `<file>:<line>:<col>: <msg>` when position is valid, and support `errors.As` via `Unwrap()`
- Compile-time interface assertions in `pkg/dag/errors_test.go` pin the contract before runtime
- Four empty packages (`pkg/dag`, `pkg/extension`, `pkg/parser`, `pkg/bridge`) compile clean with `doc.go` headers spelling out their boundaries (e.g., "may not import temporal")
- Test corpus seeded: 8 valid + 2 helper `.star` files exercising every DSL primitive (flow, step, if_cond, script, for_each_parallel, call_flow), relative + absolute `load()`, multi-flow-per-file, cross-file `call_flow`; 8 invalid fixtures each with a stable `# expects: <substring>` header
- `go build ./...`, `go vet ./...`, and `go test ./...` all green

## Task Commits

Each task was committed atomically:

1. **Task 1: Toolchain verification, go.mod init, package skeletons, typed errors** — `eb68094` (feat)
2. **Task 2: Test fixtures — 8 valid + 8 invalid .star files + 2 golden JSON skeletons** — `9623bfb` (test)

_Note: Task 1 combined RED + GREEN into one commit because the failing-test commit alone would leave the repo unbuildable (interface assertions reference undefined types). TDD discipline was honored at the workflow level — tests were written first and confirmed to fail (`undefined: ParseError`) before `errors.go` was created._

## Files Created/Modified

**Module + tooling (9 files):**
- `go.mod` — module identity, `go 1.25.0` directive (auto-rewritten from `1.25` by Go toolchain), pinned `go.starlark.net` + `testify`
- `go.sum` — dep checksums (4 modules)
- `.gitignore` — Go build artifacts, editor, OS files
- `pkg/dag/doc.go` — package docstring; documents the host-of-errors decision
- `pkg/dag/errors.go` — `ParseError` and `ValidationError` types
- `pkg/dag/errors_test.go` — 10 tests + 4 compile-time interface assertions
- `pkg/extension/doc.go` — SDK contract package boundary ("may not import temporal")
- `pkg/parser/doc.go` — parsing package boundary
- `pkg/bridge/doc.go` — state↔starlark bridge boundary

**Test corpus (18 files):**
- `tests/fixtures/valid/01-minimal-flow.{star,golden.json}` — minimal one-flow shape; golden JSON is the locked contract for the simplest case
- `tests/fixtures/valid/02-all-primitives.{star,golden.json}` — exercises every DSL primitive plus `def` helper called from a lambda; golden JSON is a placeholder for plan 05 to finalize
- `tests/fixtures/valid/03-multi-flow-per-file.star` — D-15 multi-flow corpus
- `tests/fixtures/valid/04-load-relative.star` + `04-load-target.star` — relative `load()` pair
- `tests/fixtures/valid/05-load-absolute.star` — absolute `load()` from configured root
- `tests/fixtures/valid/06-call-flow-cross-file.star` + `06-call-flow-helper.star` — cross-file `call_flow` resolution at parse time
- `tests/fixtures/invalid/01..08-*.star` — 8 negative cases (missing kwarg, mutable capture, traversal, duplicate name, call_flow not found, unknown identifier, forbidden lambda builtin, syntax error), each with `# expects: <substring>` line 1

## Decisions Made

- **Errors live in `pkg/dag`** — RESEARCH.md sketched them in `pkg/parser/errors.go`. D-04 explicitly moved them to `pkg/dag` because all four foundation packages (parser, extension, bridge, dag itself) need to construct/return them. Placing them in `pkg/parser` would force `pkg/extension` and `pkg/bridge` to import `pkg/parser`, which would (in plan 04) need to import `pkg/extension` for the registry — a cycle. The package comment in `pkg/dag/doc.go` documents this rationale for reviewers.
- **Go 1.25.9 chosen over a hypothetical 1.26.x** — `STACK.md` is explicit: floor is 1.25 because that's `go.starlark.net`'s `go.mod` directive. Bumping to 1.26 would cut off downstream consumers still on the supported 1.25 line. Used Homebrew's `go@1.25` formula.
- **Two golden JSON files committed at different fidelity levels** — `01-minimal-flow.golden.json` is a full shape contract (this is the simplest possible flow; the JSON layout will not change). `02-all-primitives.golden.json` is a placeholder with `_note` markers because the JSON representation of `if_cond`, `script`, `for_each_parallel`, etc. is finalized in plan 02 (when `pkg/dag` types land). Plan 05 will regenerate it with `UPDATE_GOLDEN=1` once marshaling is locked.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Go toolchain auto-rewrote `go 1.25` directive to `go 1.25.0`**
- **Found during:** Task 1 (immediately after `go get` runs)
- **Issue:** D-05 and the plan's acceptance criteria specify the exact line `go 1.25`. Go 1.21+ toolchain (we're on 1.25.9) automatically promotes bare-minor directives to fully-qualified forms — every `go get` / `go mod tidy` / `go test` rewrites `1.25` → `1.25.0`. Manually reverting to `1.25` triggers `go: updates to go.mod needed; to update it: go mod tidy` on subsequent commands.
- **Fix:** Accepted `go 1.25.0` because (a) D-05's intent ("not 1.26+") is preserved — `1.25.0` is on the 1.25 line, (b) the Go toolchain treats `1.25` and `1.25.0` as equivalent for compatibility purposes, (c) fighting the toolchain on every command is not viable.
- **Files modified:** `go.mod`
- **Verification:** `go version | grep -E 'go1\.(25|26|27)'` matches `go1.25.9`; `go.mod` floor is the 1.25 line, not 1.26+; `go build ./...` passes
- **Committed in:** `eb68094` (Task 1 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking)
**Impact on plan:** No scope creep. The deviation is a notation difference (`1.25` vs `1.25.0`); semantic floor is unchanged. Worth noting for any future tooling that greps for the literal string `go 1.25`.

## Issues Encountered

- **Local Go was 1.21.0** (per CONTEXT.md "Phase Specific Notes" — anticipated). Resolved by `brew install go@1.25` which created `/Users/mikel/homebrew/bin/go` symlink to `1.25.9`. No checkpoint required because Homebrew install is automatable.

## User Setup Required

None — toolchain install was performed by the executor; no external services configured this plan.

## Known Stubs

- `tests/fixtures/valid/02-all-primitives.golden.json` contains `_note: "shape finalized in plan 05; this is a placeholder"`. **Intentional**: the JSON shape for `if_cond`, `script`, `for_each_parallel`, `call_flow` depends on the `pkg/dag` types finalized in plan 02 and the JSON marshaling locked in plan 05. Plan 05 task 4 regenerates this file with `UPDATE_GOLDEN=1`. This stub does not block the plan's goal (Wave 0 scaffolding); plan 05 explicitly owns the resolution.

## Next Phase Readiness

- **Plan 01-02 (pkg/dag types) is unblocked** — can immediately import the foundation, define Flow/Step/IfCond/Script/ForEachParallel/CallFlow/ActionRef/CapturedLambda/RetryPolicy types in the same package, and use the existing `errors.go` for any internal validation.
- **Plan 01-03 (pkg/extension SDK) is unblocked** — can import `dag.ValidationError` for kwarg-schema rejection without circular concerns.
- **Plan 01-04 (pkg/parser + pkg/bridge) is unblocked** — can return `*dag.ParseError` from parser entry points; testify is already pinned for table-driven tests.
- **Plan 01-05 (parser integration / fixture-based tests) is unblocked** — every fixture file the integration tests need exists with the right shape and `# expects:` contract.
- No blockers. No concerns.

## Self-Check: PASSED

Verified all claimed files exist and all claimed commits are present:

- FOUND: go.mod
- FOUND: go.sum
- FOUND: .gitignore
- FOUND: pkg/dag/doc.go
- FOUND: pkg/dag/errors.go
- FOUND: pkg/dag/errors_test.go
- FOUND: pkg/extension/doc.go
- FOUND: pkg/parser/doc.go
- FOUND: pkg/bridge/doc.go
- FOUND: all 18 fixture files (10 valid + 8 invalid)
- FOUND: commit eb68094 (Task 1)
- FOUND: commit 9623bfb (Task 2)
- VERIFIED: `go build ./...` exits 0
- VERIFIED: `go vet ./...` exits 0
- VERIFIED: `go test ./pkg/dag/... -count=1` exits 0 with 10 tests passing

---
*Phase: 01-type-spine-extension-contract-parser-bridge-foundations*
*Completed: 2026-04-27*
