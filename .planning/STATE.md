---
gsd_state_version: 1.0
milestone: v1.42.0
milestone_name: milestone
status: executing
stopped_at: Completed 04.1-02-PLAN.md
last_updated: "2026-05-03T04:05:38.810Z"
last_activity: 2026-05-03
progress:
  total_phases: 7
  completed_phases: 4
  total_plans: 27
  completed_plans: 22
  percent: 0
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-04-26)

**Core value:** A consultant team can take an extension catalog and a customer brief, write a `.star` file, and have a production-grade durable workflow running on Temporal — without touching Go and without giving up Temporal's retry/timeout/child-workflow guarantees.
**Current focus:** Phase 04.1 — dynamic-step-kwargs-lambda-accepting-step-action-fn-variant-for-runtime-built-action-kwargs

## Current Position

Phase: 04.1 (dynamic-step-kwargs-lambda-accepting-step-action-fn-variant-for-runtime-built-action-kwargs) — EXECUTING
Plan: 4 of 8
Status: Ready to execute
Last activity: 2026-05-03

Progress: [░░░░░░░░░░] 0%

## Performance Metrics

**Velocity:**

- Total plans completed: 0
- Average duration: -
- Total execution time: -

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| - | - | - | - |

**Recent Trend:**

- Last 5 plans: -
- Trend: -

*Updated after each plan completion*
| Phase 01 P01 | 5min | 2 tasks | 27 files |
| Phase 01 P02 | 7min | 4 tasks | 18 files |
| Phase 01 P03 | 10min | 3 tasks | 12 files |
| Phase 01 P04 | 9min | 3 tasks | 10 files |
| Phase 01 P05 | 19min | 4 tasks | 16 files |
| Phase 02 P01 | 30min | 4 tasks | 33 files |
| Phase 02 P03 | 50min | 3 tasks | 10 files |
| Phase 03 P01 | 8min | 4 tasks | 16 files |
| Phase 03 P02 | 10min | 4 tasks | 10 files |
| Phase 03-lambda-serialization-decision-interpreter-worker P04 | 18min | 3 tasks | 13 files |
| Phase 04 P01 | 5min | 3 tasks | 10 files |
| Phase 04 P02 | 6min | 3 tasks | 8 files |
| Phase 04 P03 | 7min | 3 tasks | 8 files |
| Phase 04 P04 | 5min | 3 tasks | 9 files |
| Phase 04 P05 | 6min | 3 tasks | 8 files |
| Phase 04 P06 | 3min | 1 tasks | 3 files |
| Phase 04-static-validation-tier-cli-skeleton P07 | 7min | 3 tasks | 11 files |
| Phase 260501-p7c P01 | 6min | 3 tasks | 4 files |
| Phase 04.1 P01 | 5min | 3 tasks | 17 files |
| Phase 04.1 P06 | 12min | 3 tasks | 9 files |
| Phase 04.1 P02 | 11min | 3 tasks | 4 files |

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table.
Recent decisions affecting current work:

- Init: Starlark over CEL/custom DSL — lambdas + struct injection give expressive data access without string-parsing risk
- Init: Single generic Temporal activity — avoids per-extension activity registration; enables block-batched I/O
- Init: Extensions return `ActionRef` intents (Command Pattern) — keeps parse phase pure, lets interpreter route/batch/mock from one place
- Roadmap: Phase 7 production-hardening from research deferred to v1.x — no v1 requirements landed there
- Roadmap: Phase 1 intentionally large (22 reqs across DSL/EXT/PARSE) — splitting would create co-evolution hazards between primitives, extension contract, and parser/bridge
- [Phase 01]: Errors live in pkg/dag (not pkg/parser per RESEARCH sketch) — avoids circular imports across the four foundation packages
- [Phase 01]: Go toolchain auto-rewrote `go 1.25` directive to `go 1.25.0` (Go 1.21+ behavior); accepted since the 1.25 floor (not 1.26+) per D-05 is preserved
- [Phase 01]: [Phase 01-02] Sealed Node interface with distinct types (not sum-type Kind field) — produces cleanest Phase 3 type switch and prevents malformed-node bugs at compile time
- [Phase 01]: [Phase 01-02] Pos field deliberately excluded from JSON marshaling — syntax.Position.Filename is machine-absolute, breaks cross-machine golden stability
- [Phase 01]: Plan 01-03: Idempotent declared as *bool struct field on OperationSpec (not method) — forces explicit Ptr(true)/Ptr(false) at registration site; nil = author forgot, registry rejects via errors.Is(err, ErrIdempotentRequired)
- [Phase 01]: Plan 01-03: Sealed Credential interface via unexported isCredential() method; redaction format <credential:<kind>:<id>> tested against %v/%s/%q output for all 3 kinds (Bearer/Basic/APIKey)
- [Phase 01]: Plan 01-03: TestNoTemporalImportsInExtensionPackage walks Go AST imports via go/parser stdlib — proves the firewall at test time, not just at code review
- [Phase 01]: Plan 01-04: sum implemented locally as *starlark.Builtin (sumBuiltin) — go.starlark.net Universe doesn't export sum despite D-20 listing it; preserves locked 20-key list verbatim
- [Phase 01]: Plan 01-04: PrintSink default wraps Logger as InfoContext('starlark print', 'msg', payload, 'lambda_id', captured.ID) — Phase 3 swaps for workflow.GetLogger.Info; lambda_id makes log lines self-locating
- [Phase 01]: Plan 01-04: Cancel watchdog uses native goroutine in Phase 1 — CallLambda runs inside the activity (not workflow goroutine), so native goroutines are safe; Phase 3 swaps to workflow.Go inside the workflow watchdog only
- [Phase 01]: Plan 01-04: starlark.Int values compared via Int64() not testify deep-equal — starlark.Int's unexported impl pointer breaks struct equality even when integer values match
- [Phase 01]: Plan 01-05: NewParser returns (*Parser, error) — registration failures (D-12 ErrIdempotentRequired, name collisions) surface explicitly to caller; promotion from plan's bare *Parser sketch is a strict win
- [Phase 01]: Plan 01-05: Private nodeValue wrapper in pkg/parser — keeps pkg/dag pure data; isolates Starlark coupling exactly where it belongs (parser); ~30 LOC of trivial 4-method shim
- [Phase 01]: Plan 01-05: wrapStarlarkError unwrap-first — returns the typed *dag.ParseError directly when starlark wrapped it (avoids 'cannot load X: ...' wrappedError prefix obscuring the typed message)
- [Phase 01]: Plan 01-05: callerPositionOrZero (depth-safe) for Thread.Load — Starlark's load callback runs with single-frame stack; thread.CallFrame(1) panics; fallback to filename parsing from thread.Name ('parse:<file>' / 'load:<file>' convention)
- [Phase 02]: OperationOutput marker uses EXPORTED IsOperationOutput method (deviation from D2-03 sketch); cross-package marker idiom
- [Phase 02]: Temporal SDK pinned via tools.go build-tag anchor; standard Go-tooling idiom
- [Phase 02]: ActionResult keeps unexported isActionResult() seal; pkg/dag is sole producer
- [Phase 02]: fakeExtension extended with non-idempotent post() op for D2-05 fixtures (instead of registering second fake)
- [Phase 02]: WithMaxBlockSize(n) rejects n<1 at construction; fast-fail over silent permissive default
- [Phase 02]: Plan 02-03: JSON wire format added for dag.ActionResult sealed sum (Rule 2 deviation) — Temporal default DataConverter is encoding/json; without MarshalJSON+UnmarshalJSON, []ActionResult cannot round-trip through the activity contract
- [Phase 02]: Plan 02-03: ActionRef gets UnmarshalJSON + goValueToStarlark (Rule 2 deviation) — testsuite encodes activity input []*ActionRef and decodes on activity side; without UnmarshalJSON, JSON keys (kind/kwargs/credential_id) don't match Go fields (Kind_/Kwargs/CredentialID), all fields zero-valued
- [Phase 02]: Plan 02-03: TestExecuteBatch_HappyPath_Heartbeats uses unexported withHeartbeatEmitter seam (deviation from RESEARCH §Example 2) — Temporal SDK v1.42.0 documents SetOnActivityHeartbeatListener is throttled; the fake emitter (built in 02-02 for exactly this purpose) deterministically captures every emit() call
- [Phase 02]: Plan 02-03: ExecuteBatch returns (results, nil) on cancellation, NOT (results, ctx.Err()) — locked test design wins over plan example pseudocode (cancellation is graceful, SkippedResult placeholders are the signal)
- [Phase 03]: Plan 03-01: empty-string task_queue rejected at the BUILTIN (not the linter); kwarg-presence detection (hasKwarg) distinguishes 'omitted' from 'supplied empty'. Linter pass kept as documented stub for D2-05/D2-07 symmetry.
- [Phase 03]: Plan 03-01: WorkflowInput's custom MarshalJSON deleted; the Phase 3 three-field shape {FlowName, ContentHash, InitState} is JSON-natural — Phase 1's reason for the custom marshaler (omit *starlark.Function values) no longer applies.
- [Phase 03]: Plan 03-01: Custom MarshalJSON shapes (flowJSON / stepJSON / forEachParallelJSON) require manual mirroring of new omitempty fields — golden output is driven by the shape struct, not the dag struct. Documented as a recurring retrofit pattern.
- [Phase 03]: Plan 03-01: Firewall allowlist driven by slice literal {activity, interpreter, worker} so plans 03-02 / 03-04 ship SDK imports without touching the firewall test. Until those packages exist, the latter two entries are no-op skips.
- [Phase 03]: Plan 03-02: White-box tests (package interpreter, not interpreter_test) used for unexported makeCancelChannel + newInterpreter — standard Go idiom, avoids leaking the bridge as public API
- [Phase 03]: Plan 03-02: sync.Once around close(ch) in makeCancelChannel — belt-and-suspenders idempotency per blocker fix W9; defense against any future SDK behavior shift
- [Phase 03]: Plan 03-02: Per-call workflow.Go reader lifecycle in makeCancelChannel — accepted for v1 (no unbounded lambda evals); fallback paragraph inlined for plan 03-03 to evaluate during integration testing
- [Phase 03]: Plan 03-02: Multi-version FlowRegistry shape (map[string]map[string]*ParsedFlow) over single-version — costs nothing, supports test fixtures registering same flow with different bytes; ContentHashFor zero/many → ('', false) forces clean call_flow errors
- [Phase 03]: Plan 03-02: FINAL signature lock for newInterpreter via TestNewInterpreter_FinalSignature compile-time gate — plan 03-03 fills walker bodies only, no signature retrofit
- [Phase 03-lambda-serialization-decision-interpreter-worker]: Plan 03-04: clientDialFunc + sdkWorkerNew package-level seams for tests; embedded-interface fakes guarded by var _ Iface = (*fake)(nil) compile-time assertions catch SDK additions at build time
- [Phase 03-lambda-serialization-decision-interpreter-worker]: Plan 03-04: Worker.Stop sync.Once-wrapped against SDK's documented panic-on-double-Stop; bootRegistry sorts .star paths via sort.Strings before hash+parse for cross-platform determinism
- [Phase 03-lambda-serialization-decision-interpreter-worker]: Plan 03-04: Parser.Lambdas() + Parser.Flows() accessors added as Rule 2 minimal Phase 1 backport — bootRegistry needs to enumerate accumulated state across multiple ParseFile invocations; lambda IDs globally unique (D-18) so single shared map across all ParsedFlow entries is correct
- [Phase 04]: Plan 04-01: charm-log module path corrected — used charm.land/log/v2 (upstream rename) instead of plan's github.com/charmbracelet/log/v2; same v2.0.0 source
- [Phase 04]: Plan 04-01: tools.go anchor pattern extended to cobra/charm.land-log/v2/x-term — preserves W0 'deps in go.mod from day one' criterion across go mod tidy
- [Phase 04]: Plan 04-01: D4-04 ValidationError.Action field + segment-slice bracket rendering — bracket appears only when at least one of Flow/Step/Action is non-empty, legacy callers (Pos+Msg) keep original format
- [Phase 04]: Plan 04-01: posFormatRe regex broadened (vs. replaced) to accept optional [...] segment — preserves Phase 1 fixture-test intent under D4-04 format and is forward-compatible to future bracket additions
- [Phase 04]: Plan 04-02: D4-02 ctx.<name> walker re-parses cached file bytes via Parser.FileBytes() — *starlark.Function discards AST after compilation per RESEARCH critical finding; findCtxAccesses + (*syntax.FileOptions).Parse + position match by (Filename, Line, Col) is the load-bearing primitive
- [Phase 04]: Plan 04-02: stateSet clone-on-fork accumulator pins D4-02 stacking — flow inputs at entry, += script.OutputAlias after each, += for_each.ItemVar inside Steps, if_cond branches see same pre-branch state; ItemsLambdaID validated against pre-loop state (cannot see own item-var)
- [Phase 04]: Plan 04-02: validateActionRefKwargs cross-validate uses Step.Pos (not ActionRef.Pos) — ActionRef.Pos may be zero on hand-built refs; enclosing Step is the closest guaranteed-present syntax-tree node. Pass ordering D4-02 BEFORE D-11 cross-validate so structural state errors surface before kwarg-shape errors
- [Phase 04]: Plan 04-02: Rule 1 fixture-alignment fix — TestFreeVars_ModuleConstAllowed/ModuleLevelDefAllowed updated from inputs={} to inputs={"v":"int"} so D4-02 (now stricter) accepts ctx.v references; D-19 invariant under test (module-level free vars allowed) preserved unchanged
- [Phase 04]: Plan 04-03: Moved dryrun out of internal/ — Go's internal-package rule blocked tests/differential_test.go from importing pkg/validator/internal/dryrun (cross-tree path). Final at pkg/validator/dryrun/; test-only guarantee is now social rather than syntactic, documented in dryrun/doc.go
- [Phase 04]: Plan 04-03: validator.Validate returns empty (non-nil) []error on success — empty slice signals 'no errors' unambiguously and lets future multi-error reporting append without forcing callers to switch from nil-check to make([]error, 0)
- [Phase 04]: Plan 04-03: AlwaysOkDispatch shallow-copies OperationSpec and replaces ONLY Func — preserves Name/Idempotent/KwargsType/DefaultTimeout so the activity layer's DecodeKwargsFromDict path still fires on bad inputs. Phase 5's Starlark mock harness will reuse this dispatch-replacement seam shape.
- [Phase 04]: Plan 04-03: TestDifferentialCorpus skip-on-empty is REQUIRED behavior — W2 differential infrastructure ships before W4 corpus by design. Test t.Skip()s when examples/skeleton/ doesn't exist OR has no .star files; activates automatically when W4 plan 04-07 lands fixtures.
- [Phase 04]: Plan 04-04: Charm-log color-profile symbol corrected — charmlog.AsciiProfile does not exist; SetColorProfile takes a github.com/charmbracelet/colorprofile.Profile. Imported colorprofile and used colorprofile.ASCII (Rule 3 deviation). go mod tidy promoted charmbracelet/colorprofile from indirect to direct require.
- [Phase 04]: Plan 04-04: Cobra SilenceErrors+SilenceUsage on root + errSilent sentinel in validate RunE — D4-18 split: renderer owns output (typed dag.{ParseError,ValidationError} via errors.As; --debug walks Unwrap chain), cobra owns exit status (returns errSilent → non-zero exit without re-print).
- [Phase 04]: Plan 04-04: Table-driven envBinding (flag/envVar/target *string) over per-flag if-block ladder — six SKYTIME_TEMPORAL_* fallbacks share identical 'if !Lookup(flag).Changed && env != "" → cfg.target = env' rule; one row per future flag.
- [Phase 04]: Plan 04-04: Stub-then-fill TDD across multi-task plans — Task 1 ships render/validate stubs so root.go compiles and TestPkgCli_ImportsCobra activates immediately; Task 2 fills renderer; Task 3 fills validate. Each TDD cycle (RED+GREEN) stays atomic across the plan.
- [Phase 04]: Plan 04-04: D4-16 hint heuristic accepts both 'undefined:' (Starlark resolver wording, current surface) AND 'unknown extension' (forward-compatible for a future parser refactor) — either substring match in lowered ParseError.Msg → docs/cli-binary.md hint.
- [Phase 04]: Plan 04-05: clientFactory test seam (NewCloud/NewSelfHosted/NewDev funcs) — production wires defaultClientFactory; tests inject capturing factories. Mirrors Phase 3's clientDialFunc/sdkWorkerNew pattern.
- [Phase 04]: Plan 04-05: D4-08 variant routing in switch order — api-key → cloud, mTLS triplet → self-hosted, partial-mTLS rejected with friendly error before any factory call, otherwise → dev. Mutually exclusive, one trace path.
- [Phase 04]: Plan 04-05: progressHandler ships with no production effect — pkg/interpreter does NOT yet emit flow_name/step_kind/action_kind attrs (Phase 5/6 work). Handler is built and unit-tested; behavior is 'all SDK records pass through' until interpreter wires the attrs.
- [Phase 04]: Plan 04-05: skytime run validates first via pkg/validator.Validate (D4-07) BEFORE connecting Temporal — validation failures must never leak into a partially-started workflow. Same renderer + errSilent path as skytime validate; single source of truth for error formatting.
- [Phase 04]: Plan 04-05: Embedded transient worker uses filepath.Dir(file) as RootDir — single-binary UX, no separate --rootdir flag. Production daemons use --rootdir; D4-05 documents skytime run as dev-mode convenience.
- [Phase 04]: Plan 04-05: Temporal firewall allow-list extended (allowedPkgs += 'cli') — pkg/cli's run subcommand is the legitimate consumer of client.ExecuteWorkflow. Single-line edit; matches the cobra firewall's existing pkg/cli allow-list (Phase 4 plan 04-04).
- [Phase 04]: Plan 04-06: Subprocess wrapper subcommand pattern — DisableFlagParsing: true + ctx-aware exec.CommandContext + foreground stdio + signal.Notify forwarding goroutine + errors.As over *exec.ExitError. Reusable for any future skytime subcommand wrapping an external CLI.
- [Phase 04]: Plan 04-06: Two-seam test design — lookPath (input substitution: point at fake binary) + testRunningCmd (output observation: read running *exec.Cmd to dispatch signals). Lets behavioral tests exercise signal-forwarding without dispatching at the test process itself.
- [Phase 04]: Plan 04-06: TestDevServerCmd_SignalForward uses temp shell wrapper script (#!/bin/sh; exec sleep 10) — Rule 1 deviation. dev_server.go ALWAYS prepends 'server start-dev'; /bin/sleep would reject those as non-numeric and exit before the seam-observer poll. Wrapper ignores $@ and keeps subprocess alive long enough for testRunningCmd observation.
- [Phase 04-static-validation-tier-cli-skeleton]: Plan 04-07: Baked-in HTTP extension at pkg/extension/builtin/http — Initialize returns *starlarkstruct.Module (matches fakeExtension convention); per-method builtins close over base_url+credential at endpoint() time and inject base_url into output Kwargs Dict so activity-side OperationFunc reads endpoint state via the existing DecodeKwargsFromDict reflection path
- [Phase 04-static-validation-tier-cli-skeleton]: Plan 04-07: D4-14 idempotence locked verbatim (get/head=true; post/put/delete=false) — TestExtension_OperationsIdempotenceMatchesD4_14 pins the RFC-7231 PUT/DELETE override; any future change requires a CONTEXT.md amendment
- [Phase 04-static-validation-tier-cli-skeleton]: Plan 04-07: GetArgs vs BodyArgs schema split — get/head/delete share GetArgs (no body); post/put share BodyArgs (with body field); two types instead of one with optional body keeps the kwargs reflection clean and enables a one-line KwargsType.Name() schema-shape assertion
- [Phase 04-static-validation-tier-cli-skeleton]: Plan 04-07: noopCredentialHandler bifurcation in tests/differential_test.go (Rule 1 bug) — empty IDs return (nil, nil) so anonymous endpoints (http.endpoint without credential=) traverse runAction; non-empty IDs error loudly to catch corpus drift. Pre-existing pkg/activity/credential_cache.go does not short-circuit on empty IDs (logged as v1.x audit item; out of scope per scope-boundary rule)
- [Phase 04-static-validation-tier-cli-skeleton]: Plan 04-07: stubInitState helper in tests/differential_test.go (Rule 3 blocking) — runDryRun seeds InitState from flow.Inputs via type-hint→typed-zero mapping (int→0, bool→false, list→[], dict→{}, default→""); required because static D4-02 accepts ctx.<input> declarations but dry-run runtime needs the keys populated on ctx struct. Mirrors what skytime run --input=<json> would supply
- [Phase 260501-p7c]: Quick 260501-p7c: WorkerOptions.UseBuildIDVersioning auto-enable removed from applyDefaults — versioning is now opt-in (default false). Production opt-in path preserved verbatim through to sdkworker.Options.UseBuildIDForVersioning. Default skytime dev-server + skytime run no longer hangs on first dispatch against a fresh task queue with no Build ID compatibility set.
- [Phase 260501-p7c]: Quick 260501-p7c: End-to-end no-hang verification re-anchored from RUN_EXIT to log-evidence (Started Worker / workflow start / ExecuteActivity present in stdout) — the noopCredentialHandler-on-empty-id retry loop is pre-existing and out-of-scope, but it dominates RUN_EXIT and would have masked the bug-fix evidence. Constraint-aligned per the prompt's stated 'verification target is the worker scheduled the workflow' rule.
- [Phase quick-260502-onc]: Quick 260502-onc: extension.ErrNonRetryable sentinel + activity-side classification — established pattern for any extension surfacing non-retryable failures (mirrors ErrUnknownCredential)
- [Phase quick-260502-onc]: Quick 260502-onc: 4xx vs 5xx split — 4xx wraps ErrNonRetryable (no retry); 5xx plain wrap (Temporal RetryPolicy fires)
- [Phase quick-260502-onc]: Quick 260502-onc: Reflection + JSON-fallback summary extraction (Rule 1 deviation) — extractStatusSummary handles both typed Output (unit tests) and round-tripped RawOperationOutput (production wire) since Temporal's JSON DataConverter erases concrete output types
- [Phase quick-260502-onc]: Quick 260502-onc: walkStep extractFirstNonRetryable wires D2-14 (results, nil) soft failures into workflow-level errors so the renderer prints flow_failed; helper-level test coverage for first-wins property keeps test count manageable
- [Phase quick-260502-onc]: Quick 260502-onc: progressHandler lastErr lifecycle — schema-stable (no new event attrs), reset on flow_start, shallow-copied through WithAttrs/WithGroup; defensive '(no per-step error captured)' placeholder for malformed event sequences
- [Phase quick-260502-onc]: Quick 260502-onc: e2e subprocess teardown — Setpgid + syscall.Kill(-pgid, SIGTERM→3s grace→SIGKILL) wired from defer + signal.Notify; ensureDevServer probe-then-spawn reuses existing 7233 listener; //go:build !windows (Setpgid is Unix-only)
- [Phase 04.1]: Plan 04.1-01: dag.Step gains Name/NameFn/ActionFn/BlockFn (D4.1-15, D4.1-06); dag.Flow gains NameFn (D4.1-16); dag.CapturedLambda gains BodyPos with zero-value sentinel (RESEARCH §Pattern 2)
- [Phase 04.1]: Plan 04.1-01: StarlarkLambda starlark.Value wrapper lives in pkg/dag (not pkg/parser) — pkg/interpreter must UnwrapStarlarkLambda inside resolveKwargs WITHOUT importing pkg/parser; placing wrapper in pkg/dag breaks the would-be cycle. Mirrors nodeValue idiom but moved upstream for cross-tier use.
- [Phase 04.1]: Plan 04.1-01: Wave 0 fixtures land directly under tests/fixtures/ (not the existing valid/ or invalid/ subdirs). Plan's verify command and downstream-wave fixture-loaders use the verbatim path tests/fixtures/<name>.star. Existing 01-*.star..11-*.star Phase 1/2 corpora in subdirs are undisturbed.
- [Phase 04.1]: Plan 04.1-06: ANSI strategy = cursor-up + line-clear (NOT alternate-screen-buffer) — preserves scrollback for demos and CI logs per cargo/npm/bazel precedent (RESEARCH §4)
- [Phase 04.1]: Plan 04.1-06: Inline 10-frame braille spinner (locked verbatim ⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏ at 100ms cadence) over charm/huh dependency — 5 LOC + a string slice; charm/huh would conflict with the slog-handler-driven event model
- [Phase 04.1]: Plan 04.1-06: Single render goroutine + buffered chan (size 64) — Handle() is mutex-free; goroutine owns the writer. Avoids the mu.Lock anti-pattern from RESEARCH §Pattern 5; batches concurrent for_each_parallel events through one redraw cycle
- [Phase 04.1]: Plan 04.1-06: progressEvent struct lives in progress.go (no build tag) — Unix and Windows liveRenderer variants share the EXACT same struct shape; otherwise buildProgressEvent's field accesses would compile on Unix but fail on Windows (W12 in plan)
- [Phase 04.1]: Plan 04.1-06: submit() defer-recover() handles close-race between fast-path closed-check and channel send — documented Go idiom that lets the renderer remain goroutine-isolated without an extra mutex on every submit
- [Phase 04.1]: Plan 04.1-06: CLI-06/CLI-07 requirements not yet registered in REQUIREMENTS.md (Plan 04.1-07 adds them via the registrar plan); requirements mark-complete deferred to 04.1-07 execution
- [Phase 04.1]: Plan 04.1-02: synthesized-source string + re-parse desugarer (RESEARCH §Pattern 1) + dual-position attribution (Pos at user source, BodyPos at synthetic file) + filename-prefix remap of <interp:...> back to user Pos for ValidationError clarity

### Pending Todos

None yet.

### Blockers/Concerns

- **Phase 3 entry-gate decision:** Lambda serialization across Temporal history (custom `DataConverter` vs. re-parse-on-start) must be resolved before any interpreter code is written. Default fallback if spike is inconclusive: option (b) re-parse on workflow start with `workflow.SideEffect`, `LambdaID` keys in history, file-content-hash cache.
- **Requirement count discrepancy:** REQUIREMENTS.md header states "51 total" but enumerated requirements sum to 55 (DSL:10 + EXT:6 + PARSE:6 + INTRP:7 + ACT:6 + WORK:3 + VAL:3 + TEST:5 + CLI:5 + EX:4). All 55 are mapped to phases; the header line should be reconciled at the next /gsd:transition.

### Roadmap Evolution

- Phase 04.1 inserted after Phase 4: Dynamic step kwargs — lambda-accepting `step(action_fn=...)` variant for runtime-built action kwargs (URGENT) — surfaced 2026-05-02 when Phase 4's `simple_check.star` corpus demonstrably ignored `--input` because step kwargs are static at parse time. Required before Phase 5 (E2E test harness) and Phase 6 (real example project) can land any flow that takes input.

## Session Continuity

Last session: 2026-05-03T04:05:38.806Z
Stopped at: Completed 04.1-02-PLAN.md
Resume file: None
