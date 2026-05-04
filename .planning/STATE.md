---
gsd_state_version: 1.0
milestone: v1.42.0
milestone_name: milestone
status: executing
stopped_at: Completed 04.2-06-PLAN.md
last_updated: "2026-05-04T17:43:33.103Z"
last_activity: 2026-05-04
progress:
  total_phases: 8
  completed_phases: 5
  total_plans: 34
  completed_plans: 33
  percent: 100
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-04-26)

**Core value:** A consultant team can take an extension catalog and a customer brief, write a `.star` file, and have a production-grade durable workflow running on Temporal — without touching Go and without giving up Temporal's retry/timeout/child-workflow guarantees.
**Current focus:** Phase 04.2 — if-cond-as-expression-with-strict-equality-result-binding

## Current Position

Phase: 04.2 (if-cond-as-expression-with-strict-equality-result-binding) — EXECUTING
Plan: 7 of 7
Status: Ready to execute
Last activity: 2026-05-04

Progress: [██████████] 100%

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
| Phase 04.1 P03 | 10min | 4 tasks | 8 files |
| Phase 04.1 P04 | 10min | 3 tasks | 4 files |
| Phase 04.1 P05a | 2min | 1 tasks | 2 files |
| Phase 04.1 P05b | 25m | 2 tasks | 6 files |
| Phase 04.1 P07 | 11min | 5 tasks | 5 files |
| Phase 04.2 P01 | 19min | 3 tasks | 27 files |
| Phase 04.2 P02 | 27m | 2 tasks | 13 files |
| Phase 04.2 P03 | 3m | 2 tasks | 6 files |
| Phase 04.2 P05 | 6m | 2 tasks | 5 files |
| Phase 04.2 P04 | 11m | 3 tasks | 6 files |
| Phase 04.2 P06 | 8m | 3 tasks | 8 files |

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
- [Phase 04.1]: Plan 04.1-03: builtinStep extended with action_fn/block_fn/name kwargs + 4-way mutual exclusion (D4.1-06, D4.1-15); builtinFlow + builtinScript accept ${...} interpolation (D4.1-16, D4.1-02); D4-02 walker extended for *dag.Step (validates ActionFn/BlockFn/NameFn lambdas + every *StarlarkLambda inside ActionRef.Kwargs); extension.assignStarlarkToGo carve-out accepts *StarlarkLambda for string-typed schema fields (D4.1-05); desugarActionRefKwargs always-rebuilds dict to survive http extension's freeze-on-construction invariant; un-skipped Plan 02 deferred TestInterpolation_TypoRejected + TestInterpolation_NoTypo e2e tests (now passing)
- [Phase 04.1]: lookupOpSpec two-step extension recognition heuristic (registry.Get + unique op-name scan) — handles both fake_ext.echo and gh.get-where-gh=http.endpoint patterns
- [Phase 04.1]: Outermost-CallExpr-only AST walk via return false — kwargs of recognized <ext>.<op>(...) calls NOT walked (D4.1-11 amendment)
- [Phase 04.1]: Runtime block_fn fallback is tests-only — pkg/activity/validate_batch.go already enforces D2-05/06/07; Plan 04 pins the gate via TestValidateBatch_RuntimeBlockFnReachesGate
- [Phase 04.1]: Plan 04.1-05a: pkg/interpreter/resolve_kwargs.go ships i.resolveKwargs(ctx, ref) — walks ref.Kwargs via *starlark.Dict.Items() (insertion-order, NOT randomized; workflowcheck-safe; comment block flags 'do not add a sort'); evaluates *StarlarkLambda values via existing evalLambda path (D-20/D-22/D3-21); allocation-free fast path returns original frozen dict when no lambdas present (Phase 1/2/3 static actions stay zero-cost); strict starlark.String type assertion on resolved values with kwarg-name + actual-type error; output dict frozen before return (Temporal replay determinism); D4.1-14 plumbing for Plan 05b's walkStep wiring.
- [Phase 04.1]: fail() callsite preservation walks CallStack innermost-to-outermost skipping <builtin> frames (B6, D4.1-08)
- [Phase 04.1]: ActionRef.UnmarshalJSON sorts kwarg keys before SetKey to keep Items() byte-stable across activity-boundary round-trips
- [Phase 04.1]: Empty block_fn batch emits step_complete inline (summary='empty batch') and gates the deferred emit via deferEmit=false (D4.1-09)
- [Phase 04.1]: Lambda-returned ActionRefs are explicitly Freeze()'d in walkStep before resolveKwargs (W8); static actions stay zero-cost via idempotent Freeze
- [Phase 04.1]: resolveFlowName / stepDisplayLabel never fail the workflow on lambda eval errors — display-only attribute, fall back to literal Name (D4.1-15, D4.1-16)
- [Phase 04.1]: Plan 04.1-07: examples/skeleton corpus rewritten end-to-end — simple_check.star uses ${ctx.repo} name+path interpolation (D4.1-23); parallel_fanout.star uses step(block_fn=) + for_each_parallel over runtime ctx.repos (D4.1-24); TestDifferentialCorpus passes on rewritten fixtures with NO dryrun dispatch change required (D4.1-25 — workflow's resolveKwargs flattens lambda kwargs before activity dispatch, so AlwaysOkDispatch sees plain strings)
- [Phase 04.1]: Plan 04.1-07: PROJECT.md amended with D4.1-22 carve-out paragraph (verbatim) appended to BOTH the Strict-Directives "no string compilation" rule AND the Out of Scope "CEL or string-based expressions" bullet — documents that parser-time syntactic sugar (${ctx.expr} → lambda) is not string compilation; runtime template engines (CEL, Jinja) remain forbidden; extending beyond parser-time desugaring requires new ADR
- [Phase 04.1]: Plan 04.1-07: 7 new requirement IDs registered (DSL-11/12/13, VAL-04, CLI-06/07, EX-FIX-01); v1 requirement count 55→62; Traceability table extended with Phase 04.1 mapping; ALL marked Complete in alignment with Phase 04.1 implementing plans (01..06)
- [Phase 04.1]: Plan 04.1-07: Auto-fixed Rule 1 — TestE2E_SkytimeRun_Happy was hardcoded to --input '{"repo_path":...}' (Phase 4 corpus shape); rewrite renamed input to "repo" (D4.1-23). Test was failing 'struct has no .repo attribute' on every run; fixed alongside the corpus change so happy-path e2e remains green.
- [Phase 04.1]: PHASE COMPLETE — go test -race ./... -count=1 GREEN; go vet ./... GREEN; workflowcheck unavailable in environment (documented); decision-coverage grep emits zero MISSING for D4.1-01..25 (all 25 decisions have ≥1 reference in committed code/docs)
- [Phase 04.2]: Plan 04.2-01: *dag.Result.Types stored as map[string]any (not map[string]parser.TypeInfo) to dodge pkg/dag → pkg/parser import cycle — parser stores parser.TypeInfo values; plan 03 validator type-asserts back; interpreter never reads Types
- [Phase 04.2]: Plan 04.2-01: TypeInfo strict no-LUB structural Equal — int ≠ float; Equal returns false on Opaque vs concrete; branch-equality validator (plan 03) detects opaque-on-either-side BEFORE calling Equal and defers; Equal stays compile-time strict
- [Phase 04.2]: Plan 04.2-01: stateSet → stateSchema rename preserves D4-02 walker API verbatim — has/clone/sortedKeys methods stay; add() takes (name, TypeInfo); addUntyped() is the visibility-only shortcut; typeFromHint maps flow.Inputs hint string → TypeInfo seed (int/float/bool/string scalars; list/array→list[opaque]; dict/object/map→dict; otherwise opaque)
- [Phase 04.2]: Plan 04.2-01: Wave 0 RED scaffolding chooses 'fail loudly with deliberate plan-naming message' over t.Skip — keeps RED → GREEN signal visible in go test -run output throughout plans 02-05; messages explicitly name 'RED until plan 02 ships builtinResult' for traceability
- [Phase 04.2]: Plan 04.2-01: inferType signature locked at (e syntax.Expr, schema stateSchema, firstParam string) TypeInfo with Wave-0 stub returning TypeOpaque{} — plan 02 drops in real body without touching parseInferExpr test helper or any call site
- [Phase 04.2]: Plan 04.2-01: Plan referenced nodeJSONWrap discriminator wrapper; codebase uses per-type MarshalJSON dispatch (verified via grep). New *Result.MarshalJSON + *Fail.MarshalJSON integrate via Go default []Node encoder calling each element's MarshalJSON — no further wrapper. Documented as documentation-vs-codebase discrepancy
- [Phase 04.2]: Plan 04.2-02: Pre-exec source rewriting for result(value=dict-literal) — Starlark eagerly evaluates kwarg expressions, would error on undefined ctx at top-level. Pre-exec scan validates AST + builds *dag.Result upfront + rewrites value=dict-literal to length-preserving 0-sentinel; original bytes stay cached for AST re-parse paths.
- [Phase 04.2]: Plan 04.2-02: Compile-fallback for opaque value lambdas — synthesized 'lambda ctx: <expr>' bodies referencing user-defined helpers fail Starlark's compile resolver; fall back to 'lambda ctx: None' placeholder. Phase 3 worker re-parse provides authoritative Fn at workflow start.
- [Phase 04.2]: Plan 04.2-02: validateResultPlacement plan-02 placement gate (Rule 2 deviation) — TestResult_RejectedOutsideExpressionMode demands early reject with output_alias hint; plan 03 keeps focus on D4.2-09 cases (last-node-Result/Fail, key/type equality).
- [Phase 04.2]: Plan 04.2-02: ctx_walk.go dot.NamePos -> dot.Name.NamePos (Rule 1 bug fix) — go.starlark.net DotExpr.NamePos is consistently <invalid>; latent bug masked by tests asserting only ve.Msg. New TestCheckLambdaCtx_RemapsResultPrefix surfaces it.
- [Phase 04.2]: Plan 04.2-02: Dual-name fail registration — fail is the sole sanctioned overlap between parse-time and lambda-time predeclared envs (D4.2-05); TestParseAndLambdaGlobalsAreDistinct allows fail exception; TestFail_LambdaTime_StillRaises in pkg/bridge is the regression guard.
- [Phase 04.2]: Plan 04.2-03: validateIfCondExpressionShape finalize-pass enforces D4.2-09 (5 cases) + D4.2-11 (per-key TypeInfo strict-equality with Opaque-defers); inserted between validateResultPlacement and validateActionRefKwargs per ordering rule
- [Phase 04.2]: Plan 04.2-03: per-branch authoritative type re-inference via reinferResultTypes — re-parses synthesized <result:...> file, finds first LambdaExpr via syntax.Walk, calls inferType against proper per-branch schema; supersedes plan 02's empty-schema placeholder Types map
- [Phase 04.2]: Plan 04.2-03: seedStateSchemaForFlow extracted as shared finalize-pass helper (Rule 2 consolidation) — both validateLambdaCtxAccesses and validateIfCondExpressionShape now use the same flow.Inputs → typeFromHint seed; eliminates inline loop in D4-02 walker
- [Phase 04.2]: Plan 04.2-03: orphan-Result detector folded into walkValidateIfCondExpression's case *dag.Result — placement gate (plan 02) catches top-level + procedural-branch cases; this validator catches any *dag.Result reaching the orphan case after walkBranchSkippingLastResultOrFail's last-position skip
- [Phase 04.2]: Plan 04.2-03: defensive Opaque fallback in reinferResultTypes for every failure mode (missing BodyPos, missing fileBytes, parse error, no lambda found) — surfaces as deferral, not user-facing internal error; matches plan 02's compile-fallback pattern
- [Phase 04.2]: Plan 04.2-03 [Rule 2 deviation]: cases 4-5 implemented in Task 1 instead of stubbed — plan suggested deferring to Task 2 but Wave-0 RED tests TestValidateIfCondExpressionShape_KeysMismatch/TypeMismatch_NoLUB/OneSideOpaqueDefers required full impl to GREEN at Task 1 commit boundary; Task 2 became pure test-body addition (appropriate scope split)
- [Phase 04.2]: Verbose-mode keys=[...] uses Go default %v formatting (preserves source-insertion order, deferred bespoke formatting)
- [Phase 04.2]: strSlice tolerates post-Resolve []any degradation (slog API contract; capturingHandler reads after Resolve, renderer reads before)
- [Phase 04.2]: FailLeaf rendering reuses existing renderStepComplete{status=err} path verbatim — zero new renderer code for kind=fail
- [Phase 04.2]: Plan 04.2-04: walk_result.go::bindResultToState iterates n.Keys (Pitfall 5 — replay-deterministic source insertion order), evaluates per-key value lambdas, freezes the assembled dict, converts via bridge.FromStarlarkValue, writes via i.state.setOutput, emits one result_bound slog event
- [Phase 04.2]: Plan 04.2-04: walk_fail.go::raiseFail resolves Message verbatim or via MessageFn (display-only fallback to literal Message on eval-error or wrong-type — failure semantics still raise); returns temporal.NewNonRetryableApplicationError with type FailNode carrying n.Pos
- [Phase 04.2]: Plan 04.2-04 [Rule 1 deviation]: eventCapturingLogger over slog.Handler — workflow.GetLogger routes through testsuite.SetLogger (Temporal log.Logger), NOT slog.SetDefault; mirrors walk_step_namefn_test.go's captureLogger pattern with snapshot/serializeRecords/findEventRecords seams added for byte-equal replay comparison
- [Phase 04.2]: Plan 04.2-04: parseSrcAsFlow test helper splices parser.Lambdas() into interpreter's ParsedFlow shape (pkg/parser does not export ParsedFlow type — mirrors tests/differential_test.go's worker-bootstrap convention); locked signature func parseSrcAsFlow(t, src, flowName) *ParsedFlow
- [Phase 04.2]: Plan 04.2-04: walk_ifcond.go expression-mode dispatch splits branch into leading + last and switch-dispatches *dag.Result/*dag.Fail; defensive fallthrough for non-Result/Fail terminator (parser validator should reject; runtime fallthrough is defense-in-depth — malformed DAG behaves like procedural mode, never binds alias)
- [Phase 04.2]: Plan 04.2-06: Rule 2 fix — post-branch OutputAlias propagation in BOTH walkBodyForCtxValidation (D4-02) and walkValidateIfCondExpression (D4.2-09). Without it, downstream ctx.<alias> readers fail validation. Symmetric mutation mirrors how script.OutputAlias is added by both walkers
- [Phase 04.2]: Plan 04.2-06: Rule 1 fix — walkNode case *dag.Fail dispatches to raiseFail (D4.2-07 procedural-mode fail() is legal). Reuses plan 04's helper. Defensive case *dag.Result returns OrphanResultNode error (validator should reject earlier; defense-in-depth)
- [Phase 04.2]: Plan 04.2-06: tests/differential_test.go expectedErrFlows package-level map gates dryrun assertion to accept *temporal.ApplicationError for stub-driven top-level fail() flows; pre-existing fixtures keep strict NoError. pkg/validator/dryrun/dispatch.go required ZERO changes (verified empty git diff) — AlwaysOkDispatch is orthogonal to walk_result/walk_fail (which evaluate lambdas inline, never call activity.OperationDispatch)

### Pending Todos

None yet.

### Blockers/Concerns

- **Phase 3 entry-gate decision:** Lambda serialization across Temporal history (custom `DataConverter` vs. re-parse-on-start) must be resolved before any interpreter code is written. Default fallback if spike is inconclusive: option (b) re-parse on workflow start with `workflow.SideEffect`, `LambdaID` keys in history, file-content-hash cache.
- **Requirement count discrepancy:** REQUIREMENTS.md header states "51 total" but enumerated requirements sum to 55 (DSL:10 + EXT:6 + PARSE:6 + INTRP:7 + ACT:6 + WORK:3 + VAL:3 + TEST:5 + CLI:5 + EX:4). All 55 are mapped to phases; the header line should be reconciled at the next /gsd:transition.

### Quick Tasks Completed

| # | Description | Date | Commit | Directory |
|---|-------------|------|--------|-----------|
| 260503-q9p | Restore ANSI colors in the live progress block (regression from Phase 04.1 Plan 06) | 2026-05-03 | 76da75e | [260503-q9p-restore-ansi-colors-in-the-live-progress](./quick/260503-q9p-restore-ansi-colors-in-the-live-progress/) |
| 260503-qkk | Inline if_cond branch label (` → then`/` → else`) onto step_complete line; drop standalone branch row on both static + live renderers | 2026-05-03 | b58bffc + 9d95b49 | [260503-qkk-inline-if-cond-branch-label-then-else-on](./quick/260503-qkk-inline-if-cond-branch-label-then-else-on/) |
| 260503-qx1 | Include kind + label on step_complete line — mirrors step_dispatch column shape so user-defined step names persist past dispatch onto the finalized ✓ row; fixes latent live-renderer bug that hardcoded "step" for every kind | 2026-05-03 | 70de069 + 9f4858b | [260503-qx1-include-step-kind-label-on-step-complete](./quick/260503-qx1-include-step-kind-label-on-step-complete/) |
| 260503-rhy | Render if_cond + for_each_parallel as block scopes (header ▶ branch / ▶ open + indented children + footer ✓/✗ ms); 4-space-per-depth indent via shared pathDepth helper; replaces qkk inline branch suffix on if_cond step_complete with header on branch event | 2026-05-03 | 74797c1 + 6bc3a4d | [260503-rhy-render-if-cond-and-for-each-parallel-as-](./quick/260503-rhy-render-if-cond-and-for-each-parallel-as-/) |

### Roadmap Evolution

- Phase 04.1 inserted after Phase 4: Dynamic step kwargs — lambda-accepting `step(action_fn=...)` variant for runtime-built action kwargs (URGENT) — surfaced 2026-05-02 when Phase 4's `simple_check.star` corpus demonstrably ignored `--input` because step kwargs are static at parse time. Required before Phase 5 (E2E test harness) and Phase 6 (real example project) can land any flow that takes input.
- Phase 04.2 inserted after Phase 4 (decimal slot taken because 04.1 already exists): if_cond as expression with strict-equality `result` binding (URGENT) — surfaced 2026-05-03 during Phase 04.1 demo verification. Replaces today's procedural if_cond with an expression-shaped variant where each branch may end with `result(value=lambda ctx: {...})`; strict structural equality required across branches; no subtype/LUB rules in v1 (explicit `float(x)` casts handle widening). Implementation is parser-side: new `result` builtin, state-schema tracking through if_cond branches in `pkg/parser/state_schema.go`, dict-literal type inference of `result`'s lambda body. Runtime semantics unchanged. Required before Phase 5 so the E2E test harness designs against the new state-propagation shape from the start.

## Session Continuity

Last session: 2026-05-04T17:43:16.828Z
Stopped at: Completed 04.2-06-PLAN.md
Resume file: None
