---
phase: 04-static-validation-tier-cli-skeleton
plan: 04
subsystem: cli
tags: [cobra, charm-log, slog, errors-as, validate, persistent-flags, env-vars, tdd]

# Dependency graph
requires:
  - phase: 01-type-spine-extension-contract-parser-bridge-foundations
    provides: dag.{ParseError, ValidationError} with errors.As-friendly typed shape, pkg/extension.{Extension, CredentialHandler}
  - phase: 04-static-validation-tier-cli-skeleton (plan 01)
    provides: pkg/cli skeleton (doc.go), tools.go anchors for cobra/charm.land-log/v2/x-term, dag.ValidationError.Action field + bracket rendering, tests/firewall_cli_test.go with TestPkgCli_ImportsCobra meta-test
  - phase: 04-static-validation-tier-cli-skeleton (plan 03)
    provides: validator.Validate(file, opts...) []error façade with WithExtensions/WithCredentialHandler/WithRoot
provides:
  - "pkg/cli.NewRootCommand(opts ...Option) (*cobra.Command, error) — reusable cobra command tree with SilenceErrors+SilenceUsage (D4-18) and PersistentPreRunE (env-var binding + slog handler init)"
  - "pkg/cli.{WithExtensions, WithCredentialHandler} functional options mirroring parser/worker idioms"
  - "Seven D4-08 persistent flags (--debug --address --namespace --api-key --client-cert --client-key --server-ca) with SKYTIME_TEMPORAL_* env-var fallbacks (only when flag.Changed is false)"
  - "Starlark-first error renderer: errors.As over *dag.ParseError + *dag.ValidationError; --debug walks Unwrap chain printing 'cause: <msg>' per D4-19"
  - "charm-log slog handler with TTY detection (forces colorprofile.ASCII when stderr is not a TTY)"
  - "skytime validate <file.star> subcommand: cobra.ExactArgs(1) → validator.Validate → renderErrors → errSilent sentinel for non-zero exit"
  - "D4-16 unknown-extension hint: scans *dag.ParseError messages for 'undefined:' / 'unknown extension' patterns and appends the docs/cli-binary.md hint"
  - "Activated firewall meta-test: TestPkgCli_ImportsCobra now asserts (no longer skips) since pkg/cli/root.go imports cobra"
affects: [04-05, 04-06, 04-07, 06]

# Tech tracking
tech-stack:
  added:
    - "github.com/charmbracelet/colorprofile v0.4.2 (promoted from indirect to direct via go mod tidy when pkg/cli/render.go imported colorprofile.ASCII)"
  patterns:
    - "Functional-options + per-instance config (no globals, no init() side effects) mirroring parser.NewParser / worker.NewWorker"
    - "Cobra SilenceErrors+SilenceUsage on root, errSilent sentinel returned by RunE — renderer owns output, cobra owns exit status"
    - "Table-driven env-var binding ([]envBinding with flag/envVar/target) — adding a new persistent flag means adding one row, not a new branch"
    - "Pointer-bound flag values on cfg (StringVar/BoolVar) so cobra's Lookup().Changed gate cleanly distinguishes 'flag supplied' from 'flag at zero-value'"
    - "errors.As switch over *dag.ParseError + *dag.ValidationError as the Starlark-first rendering path; bare err.Error() fallback for untyped errors"
    - "Map-keyed cycle detection in renderWrappedChain — defensive against bad Unwrap() implementations"

key-files:
  created:
    - "pkg/cli/options.go"
    - "pkg/cli/root.go"
    - "pkg/cli/flags.go"
    - "pkg/cli/render.go"
    - "pkg/cli/validate.go"
    - "pkg/cli/root_test.go"
    - "pkg/cli/render_test.go"
    - "pkg/cli/validate_test.go"
  modified:
    - "go.mod (charmbracelet/colorprofile promoted from indirect to direct after pkg/cli/render.go imported it)"

key-decisions:
  - "[Rule 3 - Blocking] Plan referenced charmlog.AsciiProfile but no such symbol exists in charm.land/log/v2 — SetColorProfile takes a github.com/charmbracelet/colorprofile.Profile. Imported colorprofile and used colorprofile.ASCII (the canonical constant; colorprofile.Ascii is a back-compat alias). go mod tidy promoted colorprofile from indirect to direct (no version change, was already pulled in transitively by charm-log)."
  - "Stub render.go and validate.go in Task 1 so root.go compiles before Task 2/3 land their full implementations — keeps each TDD cycle independent and lets the firewall meta-test activate as soon as Task 1's root.go imports cobra."
  - "Table-driven envBinding slice in flags.go (vs. plan's per-flag if-block ladder) — same 6 flags, but adding a 7th flag is one row vs. one branch. The plan's structure was correct but expansion-hostile; the table form lands the same behavior in less code."
  - "renderError uses fmt.Fprintln (with newline) rather than the plan's plain Fprintln — verified by TestRenderer_DebugUnwrapsChain expecting exactly two newlines (primary + cause). The Fprintln choice was already implicit in the plan; codifying it here for clarity."
  - "appendUnknownExtensionHint matches both 'undefined:' (Starlark resolver wording) AND 'unknown extension' (forward-compatible for a future parser-emitted message). Either substring → hint. Verified by TestValidateCmd_UnknownExtensionHint: github.create_issue() in a binary with no github extension → Starlark resolver emits 'undefined: github' → hint surfaces."
  - "errSilent is a package-private sentinel error — callers can't accidentally errors.Is it; the only path to hit it is the validate RunE's len(errs)>0 branch. Cobra's SilenceErrors=true means this never reaches user output."
  - "Tests use `package cli` (white-box) for render_test.go because renderError is unexported; `package cli_test` (black-box) for root_test.go and validate_test.go because they exercise the public NewRootCommand surface. Same idiom Phase 3 plan 03-02 established for newInterpreter."

patterns-established:
  - "Pattern: Cobra SilenceErrors+SilenceUsage + sentinel-error RunE — the canonical way to combine D4-18's 'renderer owns output' with cobra's exit-status mechanism. Wave 4's run + dev-server subcommands will reuse errSilent (or a sibling sentinel) for the same purpose."
  - "Pattern: PersistentPreRunE chain inheriting via cobra's persistent-flag inheritance — env-var binding + slog handler init runs once per Execute() regardless of which subcommand is selected. Adding a new init step (e.g., Wave 4's Temporal client construction) appends to the chain in root.go, not in every subcommand."
  - "Pattern: Stub-then-fill TDD across multi-task plans — Task 1 ships stubs for render/validate so root.go compiles and its tests run; Task 2 and Task 3 each fill one stub. Each TDD cycle (RED + GREEN) stays atomic and the firewall meta-test activates the moment Task 1 lands cobra."
  - "Pattern: Pointer-bound flag values on a cfg struct — eliminates the need for cobra.Flag().Value.String() retrieval in subcommand RunEs. Makes the env-var binding clean (overwrite *target only when !Changed)."
  - "Pattern: errors.As switch for typed-error rendering — dag.{ParseError,ValidationError} are the Starlark-first surface; everything else is an untyped fallback. Wave 4's run subcommand reuses this for runtime errors, and Phase 5's mock harness will reuse it for assertion failures."

requirements-completed: [CLI-01, CLI-05, VAL-03]

# Metrics
duration: 5min
completed: 2026-05-01
---

# Phase 4 Plan 04: pkg/cli Cobra Root Command + validate Subcommand Summary

**Wave 3 ships the reusable cobra command tree (`pkg/cli.NewRootCommand`), the Starlark-first error renderer with `--debug` Unwrap chain, the charm-log slog handler with TTY detection, and the `skytime validate` subcommand wiring `pkg/validator.Validate` end-to-end — activating the cobra firewall meta-test from skip-on-empty to PASS the moment `pkg/cli/root.go` imports `github.com/spf13/cobra`.**

## Performance

- **Duration:** ~5 min (270s wall-clock)
- **Started:** 2026-05-01T20:22:39Z
- **Completed:** 2026-05-01T20:27:11Z
- **Tasks:** 3 (all TDD: 6 commits — 3 RED + 3 GREEN)
- **Files modified:** 9 (8 created, 1 modified)

## Accomplishments

- **`pkg/cli.NewRootCommand(opts ...Option) (*cobra.Command, error)`.** Functional-options constructor mirroring `parser.NewParser` and `worker.NewWorker`; returns `(nil, err)` on option failures so misconfiguration surfaces explicitly. Sets `SilenceErrors: true` + `SilenceUsage: true` per D4-18 — the renderer owns error output, cobra owns exit status. PersistentPreRunE chain runs `bindEnvVars` then `setupLogging`.
- **Seven D4-08 persistent flags wired with env-var fallbacks.** `--debug` (bool) + six string flags (`--address`, `--namespace`, `--api-key`, `--client-cert`, `--client-key`, `--server-ca`) bound to corresponding `cfg` fields via `pflag.{Bool,String}Var`. `bindEnvVars` table-driven over `(flag, envVar, target)` triples — only overwrites when `Lookup(flag).Changed` is false.
- **Starlark-first error renderer (`pkg/cli/render.go`).** `renderError(out, err, debug)` runs `errors.As` over `*dag.ParseError` and `*dag.ValidationError`, printing the typed `Error()` shape; falls back to `err.Error()` for untyped errors so the user always sees something. With `debug=true`, `renderWrappedChain` walks `errors.Unwrap` printing each level under a `  cause:` marker (D4-19); cycle-defensive via a `map[error]struct{}` seen set.
- **charm-log slog handler with TTY detection.** `setupLogging` builds a `charmlog.NewWithOptions(os.Stderr, ...)` `*Logger` (which implements `slog.Handler` directly per charm-log v2), sets it as `slog.Default()`, and forces `colorprofile.ASCII` when stderr is not a TTY (defense-in-depth on top of charm-log's own auto-detection).
- **`skytime validate <file.star>` end-to-end (CLI-01).** `cobra.ExactArgs(1)` → `validator.Validate(file, WithExtensions(cfg.exts...), WithCredentialHandler(cfg.credHandler))` → `renderErrors(stderr, errs, cfg.debug)` → `appendUnknownExtensionHint(stderr, errs)` → return `errSilent` (sentinel for non-zero exit without re-print). Happy path: `nil` return, exit 0, empty stderr.
- **D4-16 unknown-extension hint.** `appendUnknownExtensionHint` walks the `[]error` slice, runs `errors.As` over `*dag.ParseError`, and on lower-cased `Msg` matching `"undefined:"` (Starlark resolver wording) or `"unknown extension"` (forward-compatible), appends the four-line hint pointing to `docs/cli-binary.md`.
- **Cobra firewall meta-test activated.** `TestPkgCli_ImportsCobra` (W0 plan 04-01) skipped when pkg/cli was empty; now that `pkg/cli/root.go` imports `github.com/spf13/cobra`, the meta-test PASSES with no skip. The companion firewall test `TestNoCobraImportsOutsideAllowList` continues to scan 704 import paths under `pkg/` and confirms `pkg/cli` is the only allow-listed importer.
- **Full repo green.** `go test ./... -count=1` exits 0 across all 12 packages (pkg/{activity, bridge, cli, dag, extension, extension/testing, interpreter, parser, validator, validator/dryrun, worker} + tests/). `go vet ./...` clean.

## Task Commits

Each task TDD-paired (test → feat); 6 atomic commits.

1. **Task 1 RED:** `091f174` — failing tests for `cli.NewRootCommand` (3 subtests pinning persistent flags, validate subcommand presence, SilenceErrors/SilenceUsage)
2. **Task 1 GREEN:** `ff46e24` — options.go + root.go + flags.go + render.go (stub) + validate.go (stub); activates `TestPkgCli_ImportsCobra`
3. **Task 2 RED:** `e3b6542` — failing renderError tests (5 subtests pinning Starlark-first format, Wrapped chain drop/expose semantics, untyped fallback)
4. **Task 2 GREEN:** `3aebd54` — full render.go with charm-log slog handler, TTY detection, errors.As switch, renderWrappedChain. Rule 3 deviation noted: charmlog.AsciiProfile → colorprofile.ASCII.
5. **Task 3 RED:** `8549f67` — failing validate subcommand integration tests (3 subtests: happy path, error path with file in stderr, D4-16 hint surface)
6. **Task 3 GREEN:** `950663b` — full validate.go with cobra.ExactArgs(1), validator.Validate wiring, errSilent sentinel, appendUnknownExtensionHint

**Plan metadata:** Final commit (separate) captures SUMMARY.md + STATE.md + ROADMAP.md + REQUIREMENTS.md.

## Files Created/Modified

**Created (8):**
- `pkg/cli/options.go` — `Option func(*config) error` + `config` struct + `WithExtensions` + `WithCredentialHandler`
- `pkg/cli/root.go` — `NewRootCommand(opts ...Option) (*cobra.Command, error)` with PersistentPreRunE chain
- `pkg/cli/flags.go` — `registerPersistentFlags` + table-driven `bindEnvVars` (`envBinding` triple)
- `pkg/cli/render.go` — `setupLogging` (charm-log + TTY) + `renderError` (errors.As + Wrapped chain) + `renderErrors` + `renderWrappedChain`
- `pkg/cli/validate.go` — `newValidateCommand(cfg) *cobra.Command` + `errSilent` sentinel + `appendUnknownExtensionHint`
- `pkg/cli/root_test.go` — 3 black-box tests (FlagsRegistered, HasValidateSubcommand, SilencesErrorsAndUsage)
- `pkg/cli/render_test.go` — 5 white-box tests (`package cli`) covering D4-18/D4-19 contract
- `pkg/cli/validate_test.go` — 3 black-box tests (HappyPath, ExitNonZeroOnError, UnknownExtensionHint)

**Modified (1):**
- `go.mod` — `github.com/charmbracelet/colorprofile v0.4.2` promoted from `// indirect` to a direct require by `go mod tidy` after pkg/cli/render.go imported `colorprofile.ASCII`

## Decisions Made

- **Charm-log color-profile symbol correction.** Plan's `charmlog.AsciiProfile` does not exist in `charm.land/log/v2` — `SetColorProfile` accepts a `github.com/charmbracelet/colorprofile.Profile`. Imported colorprofile and used `colorprofile.ASCII` (the canonical constant; `colorprofile.Ascii` is a back-compat alias and works equally). The package was already a transitive dep of charm-log; promoting it to direct is the correct outcome of `go mod tidy`.
- **Stub-then-fill across the three TDD tasks.** Task 1's `setupLogging` and `newValidateCommand` are placeholders so `root.go` compiles; Task 2 fills the renderer, Task 3 fills the validate subcommand. Each task's RED phase isolates its own test set without depending on later-task implementations.
- **Table-driven envBinding over the plan's per-flag if-block ladder.** Six flags share identical "if !Changed && env != \"\" → cfg.target = env" structure; codifying as a `[]envBinding` slice over `(flag, envVar, target *string)` triples keeps the rule single-sourced. Adding a 7th persistent flag in W4 (run subcommand, e.g., `--task-queue`) means adding one row.
- **`errors.As` switch in renderError, not a type-switch.** A type-switch over typed errors only matches when the typed error is the *outermost* in the chain; `errors.As` walks the chain. Phase 1's wrapStarlarkError sometimes wraps a `*dag.ParseError` inside a Starlark `EvalError` then re-extracts; the wrapping context can change. `errors.As` is the future-proof choice and matches D4-18's exact wording.
- **D4-16 hint heuristic accepts both 'undefined:' and 'unknown extension'.** The current parser surface is the Starlark resolver's `undefined: <name>` (verified by ad-hoc probe on `github.create_issue()` in a no-extension binary). A future parser change might emit `unknown extension <name>` directly; matching either keeps the hint stable across that refactor.
- **`renderError` uses Fprintln (with trailing newline), not Fprintf without newline.** `TestRenderer_DebugUnwrapsChain` asserts exactly two newlines in debug output (primary + cause), and `TestRenderer_StarlarkFirst_ValidationError` asserts the trailing `\n`. Locking the newline behavior at the test level prevents accidental drift.
- **`errSilent` is package-private.** Callers cannot `errors.Is(err, errSilent)` from outside pkg/cli — the only path to surface it is the validate RunE returning it after renderErrors fired. Cobra's SilenceErrors=true means this sentinel is never printed.
- **White-box vs. black-box test packages.** `render_test.go` declares `package cli` (white-box) because `renderError` is unexported. `root_test.go` and `validate_test.go` declare `package cli_test` (black-box) because they exercise the public `NewRootCommand` surface. Same idiom Phase 3 plan 03-02 established for `newInterpreter`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] charm-log v2 has no `AsciiProfile` constant**
- **Found during:** Task 2 GREEN (compile error: `charmlog.AsciiProfile undefined (type Logger has no field or method AsciiProfile)`)
- **Issue:** The plan and 04-RESEARCH.md `Pattern 2` referenced `charmlog.AsciiProfile`, but `charm.land/log/v2` does NOT export an `AsciiProfile` constant. `*Logger.SetColorProfile` accepts a `github.com/charmbracelet/colorprofile.Profile` (verified via `go doc charm.land/log/v2.SetColorProfile`). The colorprofile package exports `colorprofile.ASCII` (canonical) and `colorprofile.Ascii` (back-compat alias).
- **Fix:** Imported `github.com/charmbracelet/colorprofile` and used `colorprofile.ASCII`. The package was already a transitive dep of charm-log (already in go.sum); `go mod tidy` then promoted it from `// indirect` to a direct require.
- **Files modified:** `pkg/cli/render.go` (one new import, one constant rename), `go.mod` (charmbracelet/colorprofile moved out of the indirect block)
- **Verification:** `go test ./pkg/cli -run TestRenderer -count=1` exits 0; `go test ./tests -run TestNoCobraImportsOutsideAllowList -count=1` still PASS (colorprofile is not on the forbidden list — the firewall gates cobra/pflag/charm.land/log/v2 specifically).
- **Committed in:** `3aebd54` (Task 2 GREEN — bundled with the feat commit since the symbol fix is part of the spec change)
- **Plan-impact:** No scope change. The plan's intent (force ASCII profile on non-TTY) is preserved; only the import path differs.

---

**Total deviations:** 1 auto-fixed (1 blocking — wrong symbol referenced in the plan).
**Impact on plan:** Single Rule-3 import-path correction. The plan's intent (renderer → typed dag errors → Starlark-first format; charm-log slog handler with TTY detection; --debug walks Wrapped chain; D4-16 hint on unknown extensions) is fully preserved. No scope creep, no architectural change.

## Issues Encountered

None — TDD cycles ran smoothly. Each RED phase failed for the expected reason (compile errors from undefined symbols, then assertion failures on the stub returns). Each GREEN phase passed all subtests on first run after the colorprofile correction.

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- **W4 (skytime run subcommand) unblocked.** `pkg/cli.NewRootCommand` accepts the same `WithExtensions` + `WithCredentialHandler` options run will need; PersistentPreRunE already binds env vars and inits the slog handler; the rendering surface is in place. Plan 04-05 can call `root.AddCommand(newRunCommand(cfg))` from inside `NewRootCommand` (one line) and ship its own RunE.
- **W4 (skytime dev-server subcommand) unblocked.** Same pattern — `root.AddCommand(newDevServerCommand(cfg))` from `NewRootCommand`, the dev-server RunE shells out to `temporal server start-dev` via `os/exec`.
- **W4 (HTTP extension + corpus, plan 04-07) unblocked.** `pkg/extension/builtin/http` has a `doc.go` skeleton (W0); plan 04-07 lands the implementation, then `cmd/skytime/main.go` wires `cli.WithExtensions(httpext.New())` into `NewRootCommand`. The `examples/skeleton/` corpus then activates `TestDifferentialCorpus` (W2 skip-on-empty).
- **VAL-03 contract live.** Every CLI error path now goes through `renderError`, producing the Starlark-first format. W4's run subcommand will route runtime errors through the same renderer.
- **CLI-05 firewall live and active.** `TestPkgCli_ImportsCobra` no longer skips — the meta-test now actively asserts pkg/cli imports cobra. Combined with `TestNoCobraImportsOutsideAllowList` (704 import paths, zero violations), the cobra/pflag/charm-log scope is locked to `pkg/cli` (and, implicitly, the future `cmd/skytime`).
- **No blockers.** Full repo `go test ./... -count=1` exits 0; `go vet ./...` clean; all firewall tests untouched; Phase 1-3 fixture corpora still pass.

## Self-Check: PASSED

**Files verified (8/8 created files exist on disk):**
- pkg/cli/options.go
- pkg/cli/root.go
- pkg/cli/flags.go
- pkg/cli/render.go
- pkg/cli/validate.go
- pkg/cli/root_test.go
- pkg/cli/render_test.go
- pkg/cli/validate_test.go

**Modified file verified (1/1):**
- go.mod (charmbracelet/colorprofile promoted to direct require)

**Commits verified (6/6 in `git log`):**
- 091f174 (Task 1 RED), ff46e24 (Task 1 GREEN)
- e3b6542 (Task 2 RED), 3aebd54 (Task 2 GREEN)
- 8549f67 (Task 3 RED), 950663b (Task 3 GREEN)

**Verification gates green:**
- `go test ./pkg/cli -count=1` → PASS (11/11: 3 RootCommand + 5 Renderer + 3 ValidateCmd)
- `go test ./tests -run TestPkgCli_ImportsCobra -count=1` → PASS (no longer skip)
- `go test ./tests -run TestNoCobraImportsOutsideAllowList -count=1` → PASS (704 import paths checked)
- `go test ./... -count=1` → all 12 packages green
- `go vet ./...` → clean

All success criteria met; one Rule-3 deviation documented (charmlog.AsciiProfile → colorprofile.ASCII). No missing artifacts.

---
*Phase: 04-static-validation-tier-cli-skeleton*
*Completed: 2026-05-01*
