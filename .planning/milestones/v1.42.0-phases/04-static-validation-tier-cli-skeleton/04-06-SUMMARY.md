---
phase: 04-static-validation-tier-cli-skeleton
plan: 06
subsystem: cli
tags: [cobra, os-exec, signal-forward, subprocess, test-seam, tdd]

# Dependency graph
requires:
  - phase: 04-static-validation-tier-cli-skeleton (plan 04-04)
    provides: pkg/cli.{NewRootCommand, errSilent}, pkg/cli/root.go with PersistentPreRunE chain + 7 D4-08 persistent flags + Starlark-first renderer + validate subcommand
  - phase: 04-static-validation-tier-cli-skeleton (plan 04-05)
    provides: pkg/cli root with run subcommand (the AddCommand site dev-server appends after)
provides:
  - "pkg/cli.newDevServerCommand(cfg) *cobra.Command — subprocess wrapper for `temporal server start-dev` (CLI-04)"
  - "DisableFlagParsing: true so all args after `dev-server` forward verbatim to the subprocess (D4-11)"
  - "lookPath package-level test seam (var lookPath = exec.LookPath) — production uses stdlib; tests substitute"
  - "testRunningCmd package-level test seam (W-8) — set after sub.Start(), cleared by defer; behavioral test dispatches signals at the SUBPROCESS instead of the test process"
  - "printMissingTemporalBinary: 5-line install instructions to stderr (D4-12) — brew/curl/go install paths"
  - "Foreground subprocess: stdin/stdout/stderr wired through cmd.{Out,Err}OrStdout (D4-10)"
  - "Signal forwarding goroutine: signal.Notify(SIGINT, SIGTERM) → sub.Process.Signal — defense-in-depth on top of the same-process-group default"
  - "Subprocess non-zero exit propagation: errors.As over *exec.ExitError → return errSilent → cobra exits non-zero"
  - "Root command wiring: root.AddCommand(newDevServerCommand(cfg)) after run; doc comment refreshed"
affects: [04-07, 06]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Package-level test seam vars (lookPath + testRunningCmd) — same idiom as Phase 3's clientDialFunc/sdkWorkerNew and Phase 4 plan 04-05's clientFactory. Tests substitute; production code uses the stdlib defaults."
    - "DisableFlagParsing + raw-args RunE — the canonical cobra pattern for transparent flag forwarding to a subprocess. Plan 04-06 establishes this for any future skytime subcommand that wraps an external CLI."
    - "Signal-forwarding goroutine with for-range over a buffered channel — slimmer than a select-loop, range exits when signal.Stop drains the chan; defer signal.Stop pairs with the channel close path."
    - "errSilent reuse for subprocess non-zero exit propagation — keeps the renderer-owns-output / cobra-owns-exit-status split (D4-18) consistent across validate, run, and dev-server."
    - "Behavioral test wrapper script: temp #!/bin/sh exec sleep 10 — dev_server.go ALWAYS prepends `server start-dev` so /bin/sleep would reject those as non-numeric. The wrapper ignores $@ and keeps the subprocess alive long enough to observe testRunningCmd."

key-files:
  created:
    - "pkg/cli/dev_server.go"
    - "pkg/cli/dev_server_test.go"
  modified:
    - "pkg/cli/root.go (root.AddCommand(newDevServerCommand(cfg)) after run; doc comment refreshed)"

key-decisions:
  - "[Test fix vs plan sketch] TestDevServerCmd_SignalForward uses a temp shell wrapper script (#!/bin/sh; exec sleep 10) rather than /bin/sleep directly. dev_server.go ALWAYS prepends `server start-dev` to args; /bin/sleep rejects non-numeric args and exits in <1ms, racing the seam observer (testRunningCmd is cleared by defer the moment sub.Wait returns). The wrapper ignores $@ and exec's a long sleep so testRunningCmd is observable for ≥500ms. Same intent as the plan, more robust execution."
  - "Signal forwarding uses for-range over a buffered (cap=1) channel rather than a select loop — slimmer code, identical behavior. Range exits when signal.Stop is called and the channel is closed; the deferred signal.Stop keeps the goroutine bounded by RunE's lifetime."
  - "testRunningCmd is a package-level *exec.Cmd var (not stored on the cobra.Command). Cobra commands are constructed per-call (newDevServerCommand returns a new struct each time); a per-command field would not be reachable from tests without exporting RunE state. The package-level seam is the same idiom W-8 specifies in the plan."
  - "Subprocess Wait() error path: errors.As(err, &exitErr) discriminates exit-code from spawn/wait errors. ExitError → return errSilent (cobra exits non-zero with no extra noise — subprocess already printed its diagnostics to stderr). Non-ExitError → write 'wait: <msg>' to stderr then errSilent — captures truly unexpected errors (file descriptor closed, kernel signal-on-error) without inventing new error semantics."
  - "DisableFlagParsing: true means `--debug` set BEFORE `dev-server` (root persistent flag) still works; flags AFTER `dev-server` go to the subprocess. cobra's persistent-flag parsing happens before subcommand dispatch, so `skytime --debug dev-server --port=7233` parses --debug first then forwards --port=7233."
  - "_ = cfg in newDevServerCommand body — dev-server ignores connection flags (D4-08); accepting cfg keeps the API symmetric with newRunCommand and newValidateCommand and lets a future dev-server feature consume cfg without retroactive signature change."

patterns-established:
  - "Pattern: Subprocess wrapper subcommand — DisableFlagParsing: true, ctx-aware exec.CommandContext, foreground stdio wiring, signal.Notify forwarding goroutine, errors.As over *exec.ExitError for exit-code propagation. Reusable for any future `skytime <cmd>` that shells out to an external tool."
  - "Pattern: Two-seam test design — `lookPath` for input substitution (point at fake binary) + `testRunningCmd` for output observation (read the running *exec.Cmd to dispatch signals). The combination lets tests exercise signal-forwarding behavior without dispatching at the test process itself."
  - "Pattern: Temp shell wrapper for tests of external-tool subprocesses — when the production code prepends mandatory args the test fake doesn't accept, a 1-line `exec <real-program>` shell script in t.TempDir() is the smallest fix. Same pattern reusable for any os/exec test where args matter."

requirements-completed: [CLI-04]

# Metrics
duration: 3min
completed: 2026-05-01
---

# Phase 4 Plan 06: pkg/cli skytime dev-server Subprocess Wrapper Summary

**Wave 4 closes with `skytime dev-server` — a foreground subprocess wrapper around `temporal server start-dev` per D4-09/10/11/12. DisableFlagParsing forwards user flags verbatim, missing-binary surfaces D4-12's 5-line install instructions, and SIGINT/SIGTERM are forwarded to the subprocess via a signal.Notify goroutine. Two test seams (`lookPath` for input substitution + `testRunningCmd` for output observation) make the signal-forwarding behavior testable without dispatching at the test process itself.**

## Performance

- **Duration:** ~3 min (~177s wall-clock)
- **Started:** 2026-05-01T20:40:41Z
- **Completed:** 2026-05-01T20:43:38Z
- **Tasks:** 1 (TDD: 2 commits — RED + GREEN)
- **Files modified:** 3 (2 created, 1 modified)

## Accomplishments

- **`pkg/cli/dev_server.go::newDevServerCommand(cfg) *cobra.Command`.** Subprocess wrapper around `temporal server start-dev` per CLI-04. `DisableFlagParsing: true` means everything after `dev-server` reaches RunE as raw `args` and forwards verbatim to the subprocess (D4-11). `exec.CommandContext` is ctx-aware so context cancellation kills the subprocess; stdin/stdout/stderr are wired to the CLI's own (D4-10).
- **`lookPath` package-level test seam.** `var lookPath = exec.LookPath` lets tests substitute a stub returning `("", &exec.Error{...})` to simulate missing-binary, or pointing at a fake executable for behavioral tests. Same idiom established in Phase 3 (`clientDialFunc`, `sdkWorkerNew`) and Phase 4 plan 04-05 (`clientFactory`).
- **`testRunningCmd` second test seam (W-8).** Package-level `var testRunningCmd *exec.Cmd` — set inside RunE just after `sub.Start()` and cleared on RunE return via `defer`. The behavioral signal-forward test reads this to dispatch a signal at the SUBPROCESS rather than at the test process; production sets/reads through the same seam (no behavior difference, only test observability).
- **Missing-binary error (D4-12).** `printMissingTemporalBinary(out io.Writer)` writes the canonical 5-line install block: `error: \`temporal\` CLI not found on PATH.` + `Install:` header + `macOS: brew install temporal` + `script: curl -sSf https://temporal.download/cli.sh | sh` + `Go: go install go.temporal.io/server/cmd/temporal@latest`. Returns `errSilent` so cobra exits non-zero without re-printing.
- **Signal forwarding goroutine (D4-10).** `signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)` + a buffered (cap=1) channel + a `for sig := range sigCh` goroutine that calls `sub.Process.Signal(sig)`. Defense-in-depth on top of the parent's process group (which would naturally propagate Ctrl-C) — works correctly in non-tty contexts (CI, scripted invocations) where process-group signals may not propagate.
- **Subprocess exit propagation.** `sub.Wait()` errors split via `errors.As` over `*exec.ExitError`: ExitError → return `errSilent` so cobra exits non-zero (subprocess already printed diagnostics to stderr); non-ExitError → write `wait: <msg>` to stderr then `errSilent`. Mirrors the validate / run subcommands' `errSilent` discipline (D4-18: renderer owns output, cobra owns exit status).
- **Root command wiring.** `pkg/cli/root.go::NewRootCommand` adds `root.AddCommand(newDevServerCommand(cfg))` after run; doc comment updated from "validate and run subcommands wired (dev-server lands in W4 plan 04-06)" to "validate, run, and dev-server subcommands wired".
- **Full repo green.** `go test ./... -count=1` exits 0 across all 13 packages. `go vet ./...` clean. `go build ./...` clean. The `tests/firewall_cli_test.go::TestNoCobraImportsOutsideAllowList` continues to pass (dev_server.go imports stay within stdlib + cobra, and pkg/cli is on the allow-list).

## Task Commits

Single-task TDD plan; 2 atomic commits.

1. **Task 1 RED:** `f6d5c1f` — failing tests for `newDevServerCommand` (4 tests: MissingBinary, Spawn skip-when-no-temporal, SignalForward behavioral, SignalForwardSourceSmoke). Compile fails for undefined `lookPath`, `newDevServerCommand`, `testRunningCmd`.
2. **Task 1 GREEN:** `a7ae75c` — `pkg/cli/dev_server.go` (full implementation) + `pkg/cli/root.go` (AddCommand) + test fix (temp shell wrapper script in TestDevServerCmd_SignalForward). All 4 tests pass: 3 PASS, 1 SKIP (temporal binary not on this machine's PATH).

**Plan metadata:** Final commit (separate) captures SUMMARY.md + STATE.md + ROADMAP.md.

## Files Created/Modified

**Created (2):**
- `pkg/cli/dev_server.go` — `newDevServerCommand(cfg) *cobra.Command` + `lookPath` seam + `testRunningCmd` seam + `printMissingTemporalBinary(out io.Writer)`
- `pkg/cli/dev_server_test.go` — 4 white-box tests (`package cli`): MissingBinary (lookPath stub), Spawn (skip-if-temporal-absent, well-formed `--ip=127.0.0.1 --port=0 --ui-port=0`), SignalForward (behavioral via testRunningCmd seam + temp wrapper), SignalForwardSourceSmoke (source grep)

**Modified (1):**
- `pkg/cli/root.go` — `root.AddCommand(newDevServerCommand(cfg))` after run; doc comment refreshed

## Decisions Made

- **Temp shell wrapper script for the signal-forward test.** `dev_server.go` ALWAYS prepends `server start-dev` to `args`; `/bin/sleep server start-dev 10` rejects "server" / "start-dev" as non-numeric and exits in <1ms — racing the seam observer (testRunningCmd is cleared by `defer` the moment `sub.Wait` returns). A `#!/bin/sh\nexec sleep 10\n` wrapper in `t.TempDir()` ignores `$@` and keeps the subprocess alive long enough (10s) for the test to observe `testRunningCmd`, dispatch SIGINT, and verify RunE returns within 2s.
- **Signal forwarding via `for sig := range sigCh` over a select-loop.** Slimmer code, identical behavior. The range exits when `signal.Stop(sigCh)` drains the channel (deferred at RunE start). The buffered (cap=1) channel ensures `signal.Notify` never drops a signal between the OS delivery and the goroutine's read.
- **`testRunningCmd` is a package-level var, not a cobra.Command field.** Cobra commands are constructed per-call (`newDevServerCommand` returns a new struct each invocation); per-command state would not be reachable from `package cli` white-box tests without exporting RunE internals. The package-level seam matches the plan's W-8 specification verbatim.
- **`errors.As` discriminates `*exec.ExitError`.** ExitError → return errSilent (subprocess printed its own diagnostics; cobra exits non-zero with no further noise). Non-ExitError → write `wait: <msg>` to stderr then errSilent — captures fd-closed / kernel-signal errors without inventing new error semantics.
- **`DisableFlagParsing: true` interaction with persistent flags.** `--debug` on the root command (a persistent flag) still works because cobra parses persistent flags BEFORE subcommand dispatch. Flags AFTER `dev-server` are forwarded — so `skytime --debug dev-server --port=7233` works as expected: `--debug` sets the slog handler level, `--port=7233` reaches `temporal server start-dev`.
- **`_ = cfg` in newDevServerCommand body.** dev-server ignores connection flags (D4-08); accepting `cfg` keeps the API symmetric with `newRunCommand` and `newValidateCommand`. A future dev-server feature (e.g., `--debug` consulting `cfg.debug` for verbose subprocess flags) lands without a signature change.
- **White-box test package (`package cli`).** `lookPath` and `testRunningCmd` are unexported; the tests need to substitute and read them. Same idiom Phase 4 plans 04-04 and 04-05 established for `renderError` / `clientFactory` / `newProgressHandler`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] TestDevServerCmd_SignalForward race condition with /bin/sleep**
- **Found during:** Task 1 GREEN initial test run (RUN phase failed: "testRunningCmd never set — RunE did not reach the post-Start seam")
- **Issue:** The plan sketch overrode `lookPath` to `/bin/sleep` directly. But `dev_server.go` ALWAYS prepends `server start-dev` so the actual command becomes `/bin/sleep server start-dev 10`. macOS's BSD sleep (and GNU sleep) reject non-numeric arguments and exit in <1ms with status 1. By the time the test poll loop wakes up (10ms cadence), the subprocess has already exited, `sub.Wait()` returned, and `defer testRunningCmd = nil` cleared the seam — the test never observed a non-nil testRunningCmd.
- **Fix:** Replaced the `/bin/sleep` lookPath substitution with a temp shell wrapper script (`#!/bin/sh\nexec sleep 10\n`) written to `t.TempDir()`. The wrapper ignores `$@`, exec's a 10-second sleep, and stays alive long enough for the test to observe `testRunningCmd`, dispatch SIGINT, and assert RunE returns. Same test intent as the plan; more robust execution. Defensive `sub.Process.Kill()` on the timeout path prevents subprocess leaks if the assertion ever does fail.
- **Files modified:** `pkg/cli/dev_server_test.go` (test 3 strategy adjusted; +18 −18 lines)
- **Verification:** `go test ./pkg/cli -run TestDevServerCmd_SignalForward -count=1 -v` exits 0; the wrapper runs in ~10ms wall-clock (sleep gets SIGINT promptly).
- **Committed in:** `a7ae75c` (bundled with the GREEN feat commit since the test fix is part of the spec change)
- **Plan-impact:** No scope change. The plan's intent (behavioral SIGINT-forwarding test via the testRunningCmd seam) is fully preserved; only the fake-binary mechanism is more robust than `/bin/sleep` direct substitution.

---

**Total deviations:** 1 auto-fixed (1 bug — race condition in plan-sketched test).
**Impact on plan:** Single Rule-1 test-mechanism fix. The plan's intent (DisableFlagParsing + lookPath seam + testRunningCmd seam + 5-line missing-binary instructions + signal forwarding goroutine) is fully preserved. No scope creep, no architectural change.

## Issues Encountered

None other than the documented test fix above. Each TDD phase ran on first try after the wrapper-script swap:
- RED: compile errors from undefined `lookPath`, `newDevServerCommand`, `testRunningCmd` (expected)
- GREEN initial: 3 PASS + 1 FAIL (SignalForward race) → wrapper-script fix → all PASS

## User Setup Required

None for the test suite (TestDevServerCmd_Spawn auto-skips when `temporal` binary is absent, mirroring the workflowcheck skip pattern). For end-to-end manual verification, install the temporal CLI per D4-12:
- `brew install temporal` (macOS)
- `curl -sSf https://temporal.download/cli.sh | sh` (any Unix)
- `go install go.temporal.io/server/cmd/temporal@latest` (Go-toolchain users)

Then `skytime dev-server` will spawn `temporal server start-dev` foreground; Ctrl-C terminates the subprocess and the CLI exits.

## Next Phase Readiness

- **W4 plan 04-07 (HTTP extension + corpus) unblocked.** All three Wave 4 subcommands (validate, run, dev-server) are now wired into `NewRootCommand`. Plan 04-07 lands `pkg/extension/builtin/http` and `examples/skeleton/`; `cmd/skytime/main.go` then wires `cli.WithExtensions(httpext.New())` and the corpus activates `TestDifferentialCorpus` (W2 skip-on-empty).
- **CLI-04 contract live.** `skytime dev-server` is the canonical "spawn a local Temporal" entry point. Foreground subprocess; Ctrl-C native; missing-binary surfaces install instructions; user flags forward verbatim.
- **D4-09/10/11/12 contracts all live.** Subprocess wrapper (D4-09), foreground + SIGINT forward (D4-10), DisableFlagParsing pass-through (D4-11), missing-binary install instructions (D4-12) — all four shipped with tests.
- **Phase 6 README walkthrough unblocked for the dev-server step.** The Phase 6 walkthrough ("git clone to executed flow in <5 commands") needs `skytime dev-server` to bring up Temporal locally; this plan ships the subprocess wrapper.
- **No blockers.** Full repo `go test ./... -count=1` exits 0; `go vet ./...` clean; firewall tests (cobra + temporal allow-lists) untouched.

## Self-Check: PASSED

**Files verified (2/2 created files exist on disk):**
- pkg/cli/dev_server.go
- pkg/cli/dev_server_test.go

**Modified file verified (1/1):**
- pkg/cli/root.go (root.AddCommand(newDevServerCommand(cfg)) + doc comment refresh)

**Commits verified (2/2 in `git log`):**
- f6d5c1f (Task 1 RED — failing tests for dev-server subcommand)
- a7ae75c (Task 1 GREEN — dev_server.go + root.go wiring + test fix)

**Verification gates green:**
- `go test ./pkg/cli -run TestDevServerCmd -count=1` → PASS (3 pass + 1 skip; SKIP is TestDevServerCmd_Spawn when temporal binary absent)
- `go test ./... -count=1` → all 13 packages green
- `grep -E '"io"' pkg/cli/dev_server.go` → exits 0 (acceptance criterion)
- `go build ./...` → clean
- `go test ./tests -run TestNoCobraImportsOutsideAllowList -count=1` → PASS

All success criteria met; one Rule-1 deviation documented (test-fixture race fix). No missing artifacts.

---
*Phase: 04-static-validation-tier-cli-skeleton*
*Completed: 2026-05-01*
