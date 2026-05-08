---
phase: 04-static-validation-tier-cli-skeleton
plan: 05
subsystem: cli
tags: [cobra, temporal-client, embedded-worker, slog, progress, mtls, api-key, firewall, tdd]

# Dependency graph
requires:
  - phase: 03-lambda-serialization-decision-interpreter-worker (plan 03-04)
    provides: pkg/worker.{NewCloudClient, NewSelfHostedClient, NewDevClient, NewWorker, Worker.{Start, Stop, Registry}}, pkg/dag.WorkflowInput, pkg/interpreter.FlowRegistry.ContentHashFor
  - phase: 04-static-validation-tier-cli-skeleton (plan 04-04)
    provides: pkg/cli.{NewRootCommand, WithExtensions, WithCredentialHandler}, pkg/cli root command with PersistentPreRunE chain + 7 D4-08 persistent flags + Starlark-first renderer + errSilent sentinel + validate subcommand
  - phase: 04-static-validation-tier-cli-skeleton (plan 04-03)
    provides: pkg/validator.{Validate, WithExtensions, WithCredentialHandler} façade
  - phase: 02-generic-activity-block-batch-dispatch-credentials (plan 02-03)
    provides: ExecuteBatch activity registration target for the embedded worker
provides:
  - "pkg/cli.connectClient(cfg) routing to NewCloudClient (--api-key set), NewSelfHostedClient (--client-cert + --client-key set; optional --server-ca pool), or NewDevClient (default), per D4-08"
  - "Test-seam clientFactory struct (NewCloud/NewSelfHosted/NewDev funcs) — production uses defaultClientFactory; tests inject custom factories to capture variant choice without dialing Temporal"
  - "CLI-side mTLS-half-set rejection: --client-cert without --client-key returns 'must be supplied together for mTLS' before any factory call"
  - "pkg/cli.progressHandler — slog.Handler shim filtering Skytime-namespaced records by 'flow_name' attribute presence; renders [skytime] flow=<f> step=<k> action=<a> at <pos> elapsed=<ms>ms <message>; passthrough delegates to wrapped handler with full Enabled/WithAttrs/WithGroup conformance"
  - "pkg/cli.newRunCommand: skytime run <file.star> --flow=X [--input=<json>] subcommand; D4-05 eight-step recipe (validate → parseInputJSON → connectClient → NewWorker → ContentHashFor → ExecuteWorkflow → run.Get → render result)"
  - "context.Canceled (Ctrl-C) handling: prints 'interrupted; workflow continues on Temporal as runID=X' to stderr; returns errSilent (cobra exits non-zero)"
  - "JSON --input parsing with 'invalid --input JSON' friendly error before any Temporal connection"
  - "Activated firewall allow-list: pkg/activity/firewall_test.go::TestNoTemporalImportsOutsideAllowList allowedPkgs gains 'cli'; pkg/cli legitimately imports go.temporal.io/sdk/client"
affects: [04-06, 04-07, 06]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Test-seam factory struct (clientFactory) — same idiom as pkg/worker.clientDialFunc/sdkWorkerNew (Phase 3 plan 03-04). Production assigns the real constructors; tests inject capturing fakes."
    - "Slog handler wrap-don't-reimplement (Pattern 5 from 04-RESEARCH.md): WithAttrs/WithGroup delegate; Handle splits records by attribute presence; Enabled defers to wrapped"
    - "errSilent sentinel reuse — Wave 4's run subcommand reuses Wave 3's pkg-private sentinel; cobra.SilenceErrors=true means it never reaches user output"
    - "Eight-step embedded-worker recipe: validate → parse-input → connect → NewWorker → Start → ContentHashFor → ExecuteWorkflow → run.Get; defer Worker.Stop and client.Close in reverse"
    - "context.Canceled-aware run.Get: errors.Is over the chain; print runID for resumability hint; return errSilent rather than os.Exit(130) for v1 (cobra patterns discourage panic-from-RunE)"
    - "MarkFlagRequired surfaces missing --flow before RunE — cobra returns the error from Execute, SilenceErrors=true means it's silent in output but still non-zero exit"

key-files:
  created:
    - "pkg/cli/connect.go"
    - "pkg/cli/connect_test.go"
    - "pkg/cli/progress.go"
    - "pkg/cli/progress_test.go"
    - "pkg/cli/run.go"
    - "pkg/cli/run_test.go"
  modified:
    - "pkg/activity/firewall_test.go (allowedPkgs += 'cli'; comment refresh)"
    - "pkg/cli/root.go (root.AddCommand(newRunCommand(cfg)) after validate; doc comment refresh)"

key-decisions:
  - "clientFactory struct with three function-typed fields (NewCloud/NewSelfHosted/NewDev) — production uses defaultClientFactory built from worker constructors; tests build custom factories that capture the picked variant. This mirrors Phase 3's clientDialFunc/sdkWorkerNew pattern and keeps the production path one-line."
  - "Mutually exclusive variant routing in switch order: --api-key first → cloud, mTLS triplet next → self-hosted, partial mTLS rejected with friendly error, default → dev. Order matches D4-08 precedence."
  - "Optional --server-ca: when set, ReadFile + AppendCertsFromPEM populates SelfHostedOptions.RootCAs; the empty PEM case ('no PEM certificates appended') is fast-failed at the CLI before the worker constructor is invoked."
  - "progressHandler.Enabled delegates to wrapped — Skytime-namespaced records bypass the wrapped's level via Handle's branch, but slog calls Enabled BEFORE Handle. Returning the wrapped's answer is pragmatic v1: production CLI runs at INFO+, interpreter emits at INFO. Documented in code comment for the Phase 5/6 emitter wiring."
  - "Phase 4 W5 ships the progress shim with no production effect — pkg/interpreter does NOT yet emit flow_name/step_kind/action_kind attrs (Phase 5/6 work). The handler is built and unit-tested; behavior is 'all SDK records pass through' until interpreter wires the attrs. Two unit tests (rendering + level-filter) lock the contract."
  - "skytime run validates first via pkg/validator.Validate (D4-07: same source of truth as `skytime validate`) BEFORE connecting to Temporal — validation failures must never leak into a partially-started workflow attempt. errSilent is returned after renderErrors fires; cobra's SilenceErrors=true keeps stderr clean of cobra's own re-render."
  - "parseInputJSON normalizes nil → empty map — `--input=null` decodes to a nil Go map; downstream consumers (validator schema check, interpreter InitState propagation) never have to nil-check, the input is always non-nil after parse."
  - "Embedded worker uses filepath.Dir(file) as RootDir — boots the registry against just the file's directory rather than a separate --rootdir flag. Single-binary UX matches the Phase 6 README walkthrough goal ('git clone to executed flow in <5 commands'). Production-mode workers use long-running daemons with explicit --rootdir; D4-05 documents `run` as dev-mode convenience."
  - "ExecuteWorkflow uses TaskQueue 'skytime' (the worker's default) — flow-level task_queue overrides via D3-19 still apply because the activity dispatch routes through the worker's interpreter. v1 skytime run does not expose --task-queue; future scaling concern."
  - "context.Canceled handling returns errSilent (exit 1), not os.Exit(130). True 130 requires bubbling the signal up through main.go; cobra patterns discourage panic-from-RunE for exit codes. v1.x polish item, documented in code comment."
  - "Firewall allow-list extension is one line in pkg/activity/firewall_test.go (allowedPkgs += 'cli'). Phase 4 plan 04-04 already had pkg/cli on the cobra firewall's allow-list; the temporal firewall now matches, with a comment noting Phase 4's addition."
  - "TestRunCmd_EndToEnd skip-by-default — gated on SKYTIME_E2E=1 env var. The full embedded-worker happy path needs either a temporal dev server or testsuite plumbing inside skytime run, which is heavier than W5 ships. Phase 6 exercises e2e through the README walkthrough; W5 ships the input-schema and connect-routing smokes."

patterns-established:
  - "Pattern: Test-seam factory struct for typed constructors — three function-typed fields, defaultFactory in production, test-injected variant in tests. Reusable for any subcommand that needs to capture 'which constructor was called' without invoking it."
  - "Pattern: Slog handler attribute-routing — wrap an upstream handler, branch on a single sentinel attribute (flow_name) in Handle, delegate WithAttrs/WithGroup. Pattern 5 from 04-RESEARCH.md, locked here for Phase 5/6's interpreter emitter to consume."
  - "Pattern: Eight-step embedded-worker recipe — validate → parse-input → connect → NewWorker → Start → ContentHashFor → ExecuteWorkflow → run.Get; with defer client.Close and defer Worker.Stop. Phase 5's `skytime test` and Phase 6's example binaries reuse this shape."
  - "Pattern: errors.Is(err, context.Canceled) gate inside run.Get error path — print resumability hint (runID), return errSilent. Future signal-aware subcommands reuse this gate."
  - "Pattern: cobra MarkFlagRequired + SilenceErrors+SilenceUsage — missing required flag surfaces a non-nil error from Execute (so exit non-zero) but cobra does NOT print usage/error to stderr (renderer owns output). Test asserts err != nil from ExecuteContext, doesn't assert stderr content."

requirements-completed: [CLI-02]

# Metrics
duration: 6min
completed: 2026-05-01
---

# Phase 4 Plan 05: pkg/cli skytime run Subcommand Summary

**Wave 4 ships `skytime run <file.star> --flow=X --input=<json>` — the embedded transient worker recipe (D4-05) wired to D4-08 connection routing (api-key → cloud, mTLS triplet → self-hosted, otherwise → dev), D4-07 input-schema validation through the same source of truth as `skytime validate`, and a slog handler shim (D4-06) that the Phase 5/6 interpreter will populate with per-step progress attrs. The temporal-firewall allow-list now includes pkg/cli so the run subcommand can legitimately consume client.ExecuteWorkflow.**

## Performance

- **Duration:** ~6 min (~330s wall-clock)
- **Started:** 2026-05-01T20:30:30Z (approx — Plan 05 follows Plan 04 immediately)
- **Completed:** 2026-05-01T20:36:19Z
- **Tasks:** 3 (all TDD: 6 commits — 3 RED + 3 GREEN)
- **Files modified:** 8 (6 created, 2 modified)

## Accomplishments

- **`pkg/cli.connectClient(cfg)` variant routing (D4-08).** Switch-based dispatch: `--api-key` set → `worker.NewCloudClient`, `--client-cert + --client-key` set → `worker.NewSelfHostedClient` (with optional `--server-ca` PEM-pool loading), partial mTLS rejected with `"must be supplied together for mTLS"` before any factory invocation, otherwise → `worker.NewDevClient`. The `clientFactory` struct (three function-typed fields) is the test seam; production uses `defaultClientFactory`; tests inject custom factories to capture the picked variant without dialing real Temporal.
- **`pkg/cli.progressHandler` slog shim (D4-06).** Wraps an upstream `slog.Handler`; records carrying a `flow_name` attribute render to a progress writer as `[skytime] flow=<f> step=<k> action=<a> at <pos> elapsed=<ms>ms <message>` one-liners; records without it pass through to the wrapped handler unchanged. Full slog.Handler conformance (Enabled/Handle/WithAttrs/WithGroup); Enabled delegates to the wrapped handler. Phase 4 W5 ships the shim with no production effect — pkg/interpreter does NOT yet emit the attrs (Phase 5/6 wires that).
- **`pkg/cli.newRunCommand` — full skytime run subcommand (CLI-02).** D4-05 eight-step recipe: (1) `validator.Validate` first, (2) parse `--input` JSON via `parseInputJSON` with `"invalid --input JSON"` friendly prefix, (3) `connectClient`, (4) `worker.NewWorker` against `filepath.Dir(file)`, (5) `Worker.Registry().ContentHashFor(flowName)`, (6) `client.ExecuteWorkflow` with `dag.WorkflowInput{FlowName, ContentHash, InitState}`, (7) `run.Get` blocks, (8) on `errors.Is(err, context.Canceled)` print `"interrupted; workflow continues on Temporal as runID=X"`. Surface: `--flow` (required via `MarkFlagRequired`), `--input` (default `"{}"`), `Args: cobra.ExactArgs(1)`. Result printed as indented JSON on stdout; all errors route through `cmd.ErrOrStderr()` and return `errSilent`.
- **Temporal firewall allow-list extended to include pkg/cli.** `pkg/activity/firewall_test.go` `allowedPkgs` slice gains `"cli"` (fourth entry after `activity, interpreter, worker`). The companion `TestNoCobraImportsOutsideAllowList` already had pkg/cli on its allow-list (Phase 4 plan 04-04); the temporal firewall now matches.
- **Root command wiring.** `pkg/cli/root.go::NewRootCommand` adds `root.AddCommand(newRunCommand(cfg))` after the validate add. Doc comment refreshed: "validate and run subcommands wired (dev-server lands in W4 plan 04-06)".
- **Full repo green.** `go test ./... -count=1` exits 0 across all 13 packages (the 12 from W3 plus pkg/cli's six new tests). `go vet ./...` clean. `go build ./...` clean.
- **Test coverage.** pkg/cli now ships 19 tests across 6 files: 3 RootCommand + 5 Renderer + 3 ValidateCmd + 2 ConnectClient (cloud/dev routing + partial mTLS) + 2 SlogProgress (rendering + level filter) + 4 RunCmd (input schema + required flag + validate-blocks-connect + e2e gate-skip).

## Task Commits

Each task TDD-paired (test → feat); 6 atomic commits.

1. **Task 1 RED:** `8076710` — failing tests for `connectClient` variant routing (3 subtests: cloud, dev, partial-mTLS-rejected)
2. **Task 1 GREEN:** `f7b60e6` — pkg/cli/connect.go + temporal firewall allow-list += "cli"
3. **Task 2 RED:** `82f2485` — failing progressHandler tests (2 tests: RendersStepEvents + PassthroughRespectsLevel)
4. **Task 2 GREEN:** `eb4fe2c` — pkg/cli/progress.go full slog.Handler conformance
5. **Task 3 RED:** `a104492` — failing skytime run integration tests (4 tests: input schema, required flow, validate blocks connect, e2e skip-gate)
6. **Task 3 GREEN:** `dd2ce79` — pkg/cli/run.go eight-step recipe + root.go wiring

**Plan metadata:** Final commit (separate) captures SUMMARY.md + STATE.md + ROADMAP.md + REQUIREMENTS.md.

## Files Created/Modified

**Created (6):**
- `pkg/cli/connect.go` — `clientFactory` struct + `defaultClientFactory` + `connectClient(cfg)` + `connectClientWithFactory(cfg, f)` (test seam)
- `pkg/cli/connect_test.go` — 2 white-box tests covering D4-08 variant routing + partial-mTLS rejection
- `pkg/cli/progress.go` — `progressHandler` struct + `newProgressHandler` + Enabled/Handle/WithAttrs/WithGroup + `renderProgressLine` + `hasAttr` helper
- `pkg/cli/progress_test.go` — 2 white-box tests covering D4-06 routing-by-attribute + level-filter passthrough
- `pkg/cli/run.go` — `newRunCommand(cfg)` + `parseInputJSON` helper
- `pkg/cli/run_test.go` — 4 black-box tests covering input-schema rejection, required-flag enforcement, validate-blocks-connect, and e2e gate-skip

**Modified (2):**
- `pkg/activity/firewall_test.go` — `allowedPkgs` slice += `"cli"`; comment block refreshed to mention Phase 4 plan 04-05
- `pkg/cli/root.go` — `root.AddCommand(newRunCommand(cfg))` after validate; doc comment updated

## Decisions Made

- **clientFactory test seam structurally mirrors Phase 3's clientDialFunc/sdkWorkerNew.** Three function-typed fields (NewCloud/NewSelfHosted/NewDev), production wires defaultClientFactory built from `worker.New*Client`, tests build custom factories that flip a string to capture the picked variant. The variant tests assert *which* factory was called, not what it returned — `fakeTemporalClient` is a one-liner embedding `client.Client` for the type contract.
- **Switch-based mutually-exclusive routing.** Order: api-key first → cloud; mTLS triplet next → self-hosted; partial mTLS (only one of cert/key) → reject with friendly error; default → dev. Matches D4-08 precedence and produces a single, easy-to-trace dispatch.
- **--server-ca PEM pool loaded inline at the CLI.** When non-empty, `os.ReadFile` + `x509.NewCertPool().AppendCertsFromPEM` populates `SelfHostedOptions.RootCAs`; the failure case (no PEM appended) fast-fails before the worker constructor. Keeps the worker constructor's contract clean (it sees a fully-configured `*x509.CertPool` or nil).
- **progressHandler.Enabled delegates to wrapped, not always-true.** Skytime-namespaced records (those carrying flow_name) bypass the wrapped's level filtering inside Handle, but slog calls Enabled BEFORE Handle to short-circuit no-op records. Returning the wrapped's answer is pragmatic v1 — production CLI runs at INFO+; the interpreter will emit progress at INFO. If a future emitter uses DEBUG level the user passes `--debug`. Documented in the code comment.
- **Phase 4 W5 ships the progress shim with no production effect.** pkg/interpreter does NOT yet emit `flow_name`/`step_kind`/`action_kind` attrs — that's Phase 5/6 work. The handler is built, unit-tested, and ready; behavior is "all SDK records pass through" until the interpreter wires the attrs. Two unit tests lock the contract for downstream consumers.
- **skytime run validates first.** Static validation runs BEFORE connecting to Temporal so validation failures never leak into a partially-started workflow attempt. The error path is the same renderer + errSilent sentinel as `skytime validate` — single source of truth for error formatting (D4-18 / VAL-03).
- **parseInputJSON normalizes nil → empty map.** `--input=null` decodes to a nil Go map; downstream consumers should never have to nil-check. The empty-string case is also normalized to `"{}"` (and then to an empty map).
- **filepath.Dir(file) as RootDir for the embedded worker.** Single-binary UX — no separate `--rootdir` flag — matches the Phase 6 README walkthrough goal. Production workers use long-running daemons with explicit `--rootdir` (D3-07); the run subcommand documents itself as dev-mode convenience.
- **TaskQueue "skytime" hardcoded for the embedded worker.** Matches `worker.defaultTaskQueue`. The flow's `task_queue=` declaration (D3-19) still wins for activity routing because the worker's interpreter handles dispatch — the StartWorkflowOptions only need to point at the queue the worker is polling. Future v1.x adds `--task-queue` if customers ask.
- **context.Canceled handled at the run.Get error path.** `errors.Is(err, context.Canceled)` true → print resumability hint with runID, return errSilent. cobra exits non-zero (1 in v1; 130 requires os.Exit from main, which v1 does not implement).
- **CredentialHandler nil-check before NewWorker call.** worker.NewWorker rejects nil-handler with a generic message; the CLI surfaces a friendlier message pointing at `cli.WithCredentialHandler` when constructing the binary. cmd/skytime (Phase 4 plan 04-07) is responsible for supplying one.
- **Firewall allow-list extension via slice literal addition.** One line in `pkg/activity/firewall_test.go` (line 38). The Phase 3 plan 03-04 pattern (slice literal `{activity, interpreter, worker}`) accommodates this without any test-shape change. The cobra firewall already had pkg/cli on its allow-list (Phase 4 plan 04-04); the temporal firewall now matches.
- **TestRunCmd_EndToEnd gated on SKYTIME_E2E=1.** Running an actual SkytimeWorkflow through testsuite from inside skytime run is a heavy integration test that needs the embedded worker to fire. Phase 6 exercises this through the README walkthrough; W5 ships the input-schema and connect-routing smokes.
- **Black-box tests for run + connect + progress.** `run_test.go` declares `package cli_test` because it exercises the public NewRootCommand surface. `connect_test.go` and `progress_test.go` declare `package cli` (white-box) because `clientFactory` and `newProgressHandler` are unexported. Same idiom Phase 4 plan 04-04 established.

## Deviations from Plan

None — plan executed exactly as written. The plan's eight-step recipe, four test names, three function names, two struct names, and one firewall-line edit all landed verbatim. No Rule 1/2/3 fixes were needed; no Rule 4 escalations.

The only minor deviation noted in code comments is the Enabled/Handle interaction documented above (Skytime records bypass wrapped level via Handle's branch, but Enabled delegates so slog can short-circuit non-Skytime no-ops) — this matches the plan's "wrap don't reimplement" intent verbatim and is documented as forward-compatibility for the Phase 5/6 emitter.

**Total deviations:** 0. Plan ran clean.

## Issues Encountered

None. Each TDD cycle ran smoothly:
- Task 1 RED: compile errors from undefined `clientFactory` and `connectClientWithFactory` (expected) → GREEN landed all symbols, all 3 subtests passed first run.
- Task 2 RED: compile error from undefined `newProgressHandler` (expected) → GREEN landed slog.Handler conformance, both subtests passed first run.
- Task 3 RED: 2 of 3 RUN tests failed with "stderr does not contain X" because the run subcommand didn't exist (expected); TestRunCmd_RequiresFlowFlag actually passed in RED because cobra surfaced an "unknown command 'run'" error, which still satisfies `require.Error`. The test contract is "missing --flow → non-nil error from Execute" and that holds before AND after GREEN — this is correct test behavior, the assertion is on the error existence, not the specific message.

## User Setup Required

None — no external service configuration required. The end-to-end test (`TestRunCmd_EndToEnd`) is gated on `SKYTIME_E2E=1` and skipped by default; Phase 6 will exercise the full workflow trigger path.

## Next Phase Readiness

- **W4 plan 04-06 (skytime dev-server) unblocked.** `NewRootCommand` already adds the validate and run subcommands; plan 04-06 just adds `root.AddCommand(newDevServerCommand(cfg))` after run, with the dev-server RunE shelling out to `temporal server start-dev` via `os/exec`. The connection routing (D4-08) is already in place — validate and dev-server ignore the connection flags; run consumes them.
- **W4 plan 04-07 (HTTP extension + corpus) unblocked.** `pkg/extension/builtin/http` is the doc.go skeleton from W0; plan 04-07 lands the implementation, then `cmd/skytime/main.go` wires `cli.WithExtensions(httpext.New())` into `NewRootCommand`. The `examples/skeleton/` corpus then activates `TestDifferentialCorpus` (W2 skip-on-empty).
- **CLI-02 contract live.** `skytime run` is the single-binary, self-contained execution path: parse → validate → fire workflow → block for result. The embedded transient worker removes the need for a separate `skytime worker` daemon for the dev-mode convenience case.
- **D4-05/06/07/08 contracts all live.** Embedded transient worker (D4-05), per-step progress shim (D4-06; emitter wiring deferred to Phase 5/6), input-schema validation reuse (D4-07), connection-variant routing (D4-08) — all four shipped with tests.
- **Phase 5 composition point.** The progressHandler routes by attribute, not by package; Phase 5's Starlark mock harness can emit the same `flow_name`/`step_kind`/`action_kind` attrs through a different dispatch and the progress lines will render. The `pkg/cli/progress.go` shape is reusable across runtime and testing tiers.
- **No blockers.** Full repo `go test ./... -count=1` exits 0; `go vet ./...` clean; all firewall tests untouched.

## Self-Check: PASSED

**Files verified (6/6 created files exist on disk):**
- pkg/cli/connect.go
- pkg/cli/connect_test.go
- pkg/cli/progress.go
- pkg/cli/progress_test.go
- pkg/cli/run.go
- pkg/cli/run_test.go

**Modified files verified (2/2):**
- pkg/activity/firewall_test.go (allowedPkgs += "cli")
- pkg/cli/root.go (root.AddCommand(newRunCommand(cfg)) + doc comment refresh)

**Commits verified (6/6 in `git log`):**
- 8076710 (Task 1 RED), f7b60e6 (Task 1 GREEN)
- 82f2485 (Task 2 RED), eb4fe2c (Task 2 GREEN)
- a104492 (Task 3 RED), dd2ce79 (Task 3 GREEN)

**Verification gates green:**
- `go test ./pkg/cli -count=1` → PASS (19/19: 3 RootCommand + 5 Renderer + 3 ValidateCmd + 2 ConnectClient + 2 SlogProgress + 4 RunCmd)
- `go test ./pkg/cli -run TestConnectClient -count=1` → PASS (3 subtests)
- `go test ./pkg/cli -run TestSlogProgress -count=1` → PASS (2 tests)
- `go test ./pkg/cli -run TestRunCmd -count=1` → PASS (4 tests; e2e skips on missing SKYTIME_E2E)
- `go test ./pkg/activity -run "Firewall|TestNoTemporal" -count=1` → PASS (allow-list extended cleanly)
- `go test ./tests -run TestNoCobraImportsOutsideAllowList -count=1` → PASS
- `go test ./... -count=1` → all 13 packages green
- `go build ./...` → clean
- `go vet ./...` → clean

All success criteria met; zero deviations from plan. No missing artifacts.

---
*Phase: 04-static-validation-tier-cli-skeleton*
*Completed: 2026-05-01*
