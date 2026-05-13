---
gsd_state_version: 1.0
milestone: v1.43.0
milestone_name: Durability + Triggers
status: executing
stopped_at: Completed 07.4-04-PLAN.md (extbin collapse to 30 lines + teaching block migration to pkg/cli/doc.go)
last_updated: "2026-05-13T14:20:15.872Z"
last_activity: 2026-05-13
progress:
  total_phases: 7
  completed_phases: 6
  total_plans: 35
  completed_plans: 35
  percent: 100
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-05-08)

**Core value:** A flow-author team can take an extension catalog and a customer brief, write a `.star` file, and have a production-grade durable workflow running on Temporal — without touching Go and without giving up Temporal's retry/timeout/child-workflow guarantees.
**Current focus:** Phase 07.4 — extbin-consolidation-tech-debt-cleanup

## Current Position

Phase: 07.4 (extbin-consolidation-tech-debt-cleanup) — EXECUTING
Plan: 5 of 5
Status: Ready to execute
Last activity: 2026-05-13

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
| Phase 04.2 P07 | 4m | 4 tasks | 3 files |
| Phase 04.3 P06 | 2min | 1 tasks | 1 files |
| Phase 04.3 P07 | 2min | 1 tasks | 1 files |
| Phase 04.3 P05 | 2min | 1 tasks | 1 files |
| Phase 04.3 P04 | 13min | 1 tasks | 1 files |
| Phase 04.3 P03 | 4min | 1 tasks | 1 files |
| Phase 04.3 P01 | 6 min | 3 tasks | 10 files |
| Phase 04.3 P02 | 7min | 4 tasks | 8 files |
| Phase 04.3 P09 | 3min | 2 tasks | 2 files |
| Phase 04.3 P08 | 3min | 2 tasks | 1 files |
| Phase 05 P01 | 25min | 3 tasks | 10 files |
| Phase 05 P02 | 35min | 3 tasks | 13 files |
| Phase 05 P03 | 9min | 3 tasks | 9 files |
| Phase 05 P04 | 30min | 3 tasks | 13 files |
| Phase 05 P05 | 11min | 2 tasks | 8 files |
| Phase 05 P06 | 14min | 3 tasks | 8 files |
| Phase 06 P01 | 3min | 3 tasks | 5 files |
| Phase 06 P02 | 3min | 3 tasks | 5 files |
| Phase 06-example-project-http-github-webhook P03 | 4min | 3 tasks | 5 files |
| Phase 06 P04 | 5min | 4 tasks | 4 files |
| Phase 06 P05 | 7min | 2 tasks | 2 files |
| Phase 06 P06 | 1min | 3 tasks | 6 files |
| Phase 06 P09 | 2m | 2 tasks | 2 files |
| Phase 06 P08 | 4min | 3 tasks | 5 files |
| Phase 07 P01 | 7min | 2 tasks | 3 files |
| Phase 07 P02 | 7min | 2 tasks | 4 files |
| Phase 07 P03 | 13min | 7 tasks | 13 files |
| Phase 07 P04 | 8min | 6 tasks | 7 files |
| Phase 07 P05 | 8min | 7 tasks | 10 files |
| Phase 07 P06 | 14min | 9 tasks | 22 files |
| Phase 07.1 P01 | 5min | 3 tasks | 10 files |
| Phase 07.1 P02 | 8min | 2 tasks | 4 files |
| Phase 07.1 P05 | 9min | 2 tasks | 5 files |
| Phase 07.1 P03 | 9min | 2 tasks | 4 files |
| Phase 07.1 P07 | 4min | 2 tasks | 3 files |
| Phase 07.1 P04 | 7min | 2 tasks | 6 files |
| Phase 07.1 P04b | 25min | 1 tasks | 9 files |
| Phase 07.1 P06 | 7min | 1 tasks | 2 files |
| Phase 07.1 P08 | 5min | 3 tasks | 6 files |
| Phase 07.2 P01 | 6min | 3 tasks | 9 files |
| Phase 07.2 P02 | 9min | 3 tasks | 10 files |
| Phase 07.2 P03 | 10min | 3 tasks | 14 files |
| Phase 07.2 P04 | 6m | 3 tasks | 7 files |
| Phase 07.2 P04 | finalize 3min (+UAT cycle 2026-05-11) | 4 tasks | 7 files |
| Phase 07.2.1 P01 | 3min | 2 tasks | 5 files |
| Phase 07.2.1 P02 | 6min | 2 tasks | 6 files |
| Phase 07.2.1 P04 | 8min | 3 tasks | 6 files |
| Phase 07.2.1 P03 | 6min | 2 tasks | 4 files |
| Phase 07.2.1 P05 | 8m | 2 tasks | 9 files |
| Phase 07.3 P00 | 4min | 3 tasks | 16 files |
| Phase 07.3 P03 | 12m | 2 tasks | 5 files |
| Phase 07.3 P02 | 5min | 3 tasks | 8 files |
| Phase 07.3-dashboard-manual-trigger-page P01 | 8m | 3 tasks | 6 files |
| Phase 07.3 P04 | 12min (partial — Task 3 checkpoint pending) | 2 of 3 tasks tasks | 8 files files |
| Phase 07.3 P04 | 20m | 3 tasks | 10 files |
| Phase 07.3 P05 | 18min | 3 tasks | 5 files |
| Phase 07.3 P05 | finalize 2min (+browser UAT cycle 2026-05-13) | 4 tasks | 6 files |
| Phase quick-260512-w7c P01 | 7min | 4 tasks | 8 files |
| Phase 07.4 P05 | 5min | 3 tasks | 4 files |
| Phase 07.4-extbin-consolidation-tech-debt-cleanup P03 | 5min | 3 tasks | 5 files |
| Phase 07.4 P01 | 7min | 3 tasks | 6 files |
| Phase 07.4 P02 | 3m | 2 tasks | 4 files |
| Phase 07.4 P04 | 5min | 3 tasks | 2 files |

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
- [Phase 04.2]: Plan 04.2-07 (registrar): DSL-14, DSL-15, VAL-05 registered Complete in REQUIREMENTS.md (v1 total 62→65); ROADMAP.md Phase 04.2 entry rewritten from TBD placeholder to populated post-completion shape with Goal text + 7/7 plan list; PROJECT.md amended with D4.2-05 fail() carve-out blockquote alongside D4.1-22 paragraph (parse-time top-level fail is a parse-time syntactic primitive, NOT a runtime evaluation surface); phase-completion gates GREEN (race detector + vet + decision-coverage + 23 named tests); workflowcheck unavailable (CI installs); Task 5 manual TTY demo skipped per Phase 04.1-07 precedent + gate=optional. Phase 04.2 ready for /gsd:verify-work.
- [Phase 04.3]: Plan 06: Aligned examples/README.md run-command flow names with shipped fixtures (simple_check, parallel_fanout) — plan's <interfaces> block referenced older proposed names (check_repo, batch_repos) that did not match the .star sources
- [Phase 04.3]: Plan 06: Documented expectedErrFlows registration step in 'Adding a New Example' section — Phase 04.2 introduced the package-level map in tests/differential_test.go; contributors adding fail()-emitting fixtures need this knowledge or hit confusing dryrun failures
- [Phase 04.3]: Plan 04.3-07: tutorial is the source of truth for plan 08's README verbatim embed (D-06) — single-edit-point convention, README regenerated on tutorial change. Inline carve-out citations (D4.1-22, D4.2-05) so reader gets the 'no string compilation + parser-time-only' stance from the tutorial alone
- [Phase 04.3]: Plan 04.3-05: docs/for-flow-authors/extensions/http.md hand-written per D-17 — 440 lines, 7 H2 + 14 H3 sections, ops in SOURCE ORDER (get/head/post/put/delete) NOT alphabetical; D4-14 idempotence table with verbatim RFC-7231 PUT/DELETE divergence; 4xx ErrNonRetryable / 5xx retryable classification (quick-260502-onc); JIT credential resolution + Bearer/Basic/APIKey applyCredential routing table; future docgen extension-walker target
- [Phase 04.3]: Plan 04.3-07: docs/getting-started.md uses examples/skeleton/expression_if.star check_user flow as canonical demo (D-05) — asymmetric expression-mode if_cond + dollar-ctx-expr fail interpolation shows headline DSL features in fewest lines; output snapshots labeled "approximately like this" to insulate against renderer drift
- [Phase 04.3]: Plan 04.3-04: Documented 8 persistent flags (not 10): plan's interfaces table claimed --rootdir and --temporal-task-queue exist; pkg/cli/flags.go has neither. Source-of-truth fidelity (must_haves.truths in plan frontmatter) trumps interfaces table; doc reflects actual flags + SKYTIME_TEMPORAL_CLIENT_CERT/CLIENT_KEY/SERVER_CA env names
- [Phase 04.3]: Plan 04.3-04: mTLS documented as cert+key pair (with optional --server-ca), not triplet — pkg/cli/connect.go::connectClientWithFactory rejects only when ONE of --client-cert / --client-key is set without the other; --server-ca is optional and supplies a custom RootCAs pool
- [Phase 04.3]: Plan 04.3-04: docs/reference/cli.md cross-links to builtins.md, architecture.md, getting-started.md, for-flow-authors/, for-extension-developers/ (sibling plans 01-09) — links resolve as siblings ship; cli.md reads coherently in isolation, dead links serve as TODO anchors for parallel agents
- [Phase 04.3]: Plan 04.3-03: docs/architecture.md is the required-reading architecture page (D-15) — parse/execute split prosaically + ASCII diagram verbatim from CONTEXT.md; D4.1-22 + D4.2-05 carve-outs quoted verbatim from PROJECT.md; cites pkg/parser/globals.go, pkg/dag/lambda.go (CapturedLambda + ComputeLambdaID), pkg/interpreter/workflow.go (SkytimeWorkflow factory), pkg/activity/execute_batch.go (ExecuteBatch), pkg/bridge/lambda_call.go (CallLambda), pkg/interpreter/registry.go (FlowRegistry), pkg/interpreter/cancel_watchdog.go
- [Phase 04.3]: Plan 01: cmd/skytime-docgen uses stdlib flag (not cobra) — single-command go-generate tool, ~3MB cobra deps unjustified; library-root firewall stays unchanged because cmd/ is outside pkg/ AND deliberately stdlib-only
- [Phase 04.3]: Plan 01: testdata/*.go.txt fixtures (extension dodges Go compile path; tests rewrite to tempdir/*.go before parsing) — decouples unit tests from live-source evolution; live-source smokes assert walker works on actual pkg/parser/{globals,builtins}.go
- [Phase 04.3]: Plan 01: Order-preserving registry via paired (map[name]funcName, []order) return — Go map iteration is randomized; preserving newParseTimeGlobals' source-insertion order keeps the renderer's natural top-to-bottom shape (alphabetical sort would scramble flow/step/if_cond before result/fail)
- [Phase 04.3]: Plan 01: ParseMarkers warn-and-skip via fmt.Fprintln(os.Stderr) — keeps API minimal; captureStderr test helper handles assertion paths via os.Pipe + os.Stderr swap; refactor to io.Writer reserved for plan 02 if more tests need to assert warning content
- [Phase 04.3]: Plan 02: text/template stdlib over third-party templating — preserves cmd/skytime-docgen's stdlib-only stance from plan 01; firewall test stays unchanged
- [Phase 04.3]: Plan 02: positional-only-with-markers fallback for fail() — UnpackPositionalArgs returns []Param=[] from plan 01 walker; rather than retrofit walker schema, renderer synthesizes table rows from sort.Strings-ordered param_* markers. Plan 01 walker contract preserved verbatim
- [Phase 04.3]: Plan 02: tests/docgen_drift_test.go in package firewall_test (single external-test-package convention for tests/) — helpers suffixed Docgen to avoid collision with findModuleRootCLI / findModuleRootE2E siblings
- [Phase 04.3]: Plan 09: docs/for-flow-authors/README.md (110 lines) + docs/for-extension-developers/README.md (126 lines) hand-written landings — D-03 + D-04 satisfied; mutual cross-links (each cites the other twice) make the two-tier authoring model visible from either entry point; architecture.md cross-link present in both per D-15 required-reading rule
- [Phase 04.3]: Plan 04.3-08: README.md as repo front door (D-02 mid-size, D-06 verbatim embed) — 430 lines, 8 own H2 + 7 verbatim-embedded tutorial H2s; LICENSE 'TBD' (no LICENSE file); tutorial body reproduced byte-equal from docs/getting-started.md with relative-path adjustments for the README's repo-root location
- [Phase 05]: Plan 05-01: MockEntry.RegisterPos uses syntax.MakePosition (not Position{Filename:...}) — go.starlark.net/syntax.Position has unexported file *string field; tests label entries via Filename() accessor on read-back. Rule 1 deviation, test-side compile-fix only; production registry.go matches plan sketch verbatim
- [Phase 05]: Plan 05-01: MockOperationOutput.MarshalJSON returns []byte("{}") for nil Value — wire shape is always a JSON object, mirrors RawOperationOutput shape and avoids null-pointer surprises in Plan 02 router (Open Q4 acceptance with defensive nil handling)
- [Phase 05]: Plan 05-01: WithTestMode/WithTestModule split (flag + builder pattern) breaks parser→pkg/testing import cycle — pkg/cli/test.go (Plan 06) imports pkg/testing, constructs the tester builder, supplies via WithTestModule(builder) so parser stays test-package-agnostic
- [Phase 05]: Plan 05-01: Firewall allow-list expansion paired with non-vacuous meta-test convention — adding 'testing' to pkg/activity/firewall_test.go allowedPkgs is paired with tests/firewall_testsuite_test.go::TestPkgTesting_ImportsTestsuite that skips on empty package with forward-pointing 'Plan 02 lands router.go' message; activates automatically once import lands
- [Phase 05]: Plan 05-02: WithTestPredeclared parser option (Plan-02 addition) — Starlark resolver binds free variables AT PARSE-OF-FILE TIME, so ok/err/nonretryable closures must be in the parse-time predeclared dict for mock_fn lambda bodies to resolve. Production parses (testMode=false) NEVER see these names; D1-20 lambdaTimeGlobals invariant preserved. Cycle-broken: pkg/parser does NOT import pkg/testing (cli/test.go wires MockLambdaParseTimeBuilders via the new option in Plan 06).
- [Phase 05]: Plan 05-02: fail() inside mock_fn body inherits PARSE-time builtinFail (D4.2-05 dual-semantics) — returns *dag.Fail at execute time and surfaces through D5-C4 'mock must return ok/err/nonretryable'. Documented as intentional behavior; rewrote TestRouter_MockEvalErr_NonRetryable to use integer division by zero (1 // 0) which actually raises a Starlark *EvalError.
- [Phase 05]: Plan 05-02: AttemptCounter keys on structural (FlowName, StepIdx, ActionIdx) triple, NOT ref.Pos — content-hash IDs change with cosmetic edits but slot identity is structural; replay-equality (D5-D1) requires the structural form. Mock CapturedLambda IDs use position-based format (mock:<file>:<line>:<col>) instead of D-18 content-hash since builtin closures don't have parser-session FileBytes access — router only reads captured.Fn.
- [Phase 05]: Plan 05-02: Pitfall-8 panic recovery template = defer-recover at top of buildExecuteBatchCallback wraps any Go panic with extension.ErrNonRetryable so workflow-side classification stays correct. D5-B2 verbatim no-mock-found message routes via per-action NonRetryableErrResult (mirrors quick-260502-onc 4xx classification): activity itself succeeds; walk_step path observes NonRetryable in slice and fails the step.
- [Phase 05]: Plan 05-03: interpreter.RunOnceCapturing lifted into a NEW non-test file (replay_helper.go) — existing private runOnceCapturing + eventCapturingLogger in replay_determinism_test.go untouched; the two coexist (legacy internal Phase 04.2 tests + new public Phase 5 surface). Avoids cascading test-file churn for zero functional gain.
- [Phase 05]: Plan 05-03: Per-record diff serialization sorts KV pairs alphabetically on the CONSUMER side (FirstDivergentEvent helper). Producer (logger.Info kvs) preserves source order; consumer canonical-orders for stable string equality. Eliminates Go-map-iteration-randomness false-positive divergences.
- [Phase 05]: Plan 05-03: Activity-boundary listener pair (SetOnActivityStartedListener + SetOnActivityCompletedListener) is the canonical TestWorkflowEnvironment substitute for the missing GetWorkflowHistory accessor (Investigation 2). Both listeners attach automatically inside RunOnceCapturing; events append to the same EventCapture buffer alongside slog records.
- [Phase 05]: Plan 05-03: step_dispatch.pos + step_dispatch.name is the universal flow-callsite attribution channel for D5-D3 — Plan 03 Task 0 extended walk_step.go's logger.Info emission with two new KV pairs; FirstDivergentEvent's lookupOriginatingStep walks backward to the nearest step_dispatch and reads them. Pattern available for any future divergence reporter / error annotator / live-renderer enrichment.
- [Phase 05]: Plan 05-04: runOneTest takes a testReporter interface (Helper + Errorf + starlarktest.Reporter) instead of *testing.T — Go's t.Run propagates inner subtest failures to the parent, blocking unit-test isolation; *testing.T satisfies via duck-typing, recordingT shim observes per-test failures cleanly
- [Phase 05]: Plan 05-04: starlarktest.LoadAssertModule() globals injected into parse-time predeclared dict in test mode (D5-F1, TEST-05); production parses untouched (D1-20 invariant). Library default accumulates failures within a single def test_*() — assert.eq does not raise EvalError, calls Reporter.Error directly (D5-F2 verified)
- [Phase 05]: Plan 05-04: D5 Pitfall 4 file-scope rejection via thread.CallStack walk — isInsideDefTestStar scans for any user-Function frame whose name starts with test_; absent at file scope so tester.run surfaces the verbatim 'must be called inside a def test_*()' message at parse time
- [Phase 05]: Plan 05-04: Parser.TestGlobals(filename) accessor exposes the captured top-level StringDict from a test-mode parse — single starlark.ExecFileOptions overload returns globals when testMode=true (production path discards). Plan 05 enumerates def test_*() symbols via this accessor
- [Phase 05]: Plan 05-04: D5-D1 always-on replay shares AttemptCounter across run1/run2 in tester.run — fundamentally conflicts with retry-attempt mock semantics (err on attempts 1+2 + ok on attempt 3 produces 3 dispatches in run1, 1 in run2, guaranteed FirstDivergentEvent). Plan 06 introduces a non-replay assertion path; TestAttempts_IncrementOnRetry skipped with forward-pointing message; substance pinned by TestRouter_AttemptIncrementsPerCall (Plan 02)
- [Phase 05]: Plan 05-04: pkgtesting.Run(t, dir, opts...) — Open Q7 Go-level foundation API for Phase 6 + Plan 06's cobra wrapper. Single-directory walk for v1 (Plan 05 generalizes); WithExtensions + WithRunFilter Options surface today so Plan 06's --run flag is additive. fakeGhExtension implementation inline in router_test.go removes the last panic stub from pkg/testing
- [Phase 05]: Plan 05-04: Generalized callerPosFromThread skip-list — was tester.mock_action only; now any tester.* prefix. Both builtin_mock_action.go (RegisterPos) and builtin_run.go (test-callsite divergence + file-scope rejection position) consume the same helper; strings.HasPrefix on Frame.Name skip-list
- [Phase 05]: [05-05] renderOneFile factored from runOneFile so format tests use a recordingT shim instead of real t.Run; production runOneFile retains t.Run-based driver
- [Phase 05]: [05-05] JSONEvent uses capitalized json tags (Time/Action/Package/Test,omitempty/Elapsed,omitempty/Output,omitempty) exactly mirroring stdlib cmd/test2json
- [Phase 05]: [05-05] WithRunFilter compiles regex once at option time; ErrBadFilter sentinel detected via errors.Is
- [Phase 05]: [05-05] jsonEmitter writes time.Now().UTC() (Open Q6 RFC3339Nano UTC); replay-deterministic across hosts
- [Phase 05]: Plan 05-06: D5-runner-cli-adapter — RunCLI is a parallel implementation of Plan 04 Run rather than a refactor; planning lock forbade modifying Plans 01..05; ~80 LOC duplication in pkg/testing/cli_run.go (runOneFileCLI + runOneTestCLI bound to bareReporter instead of *testing.T)
- [Phase 05]: Plan 05-06: D5-docs-builtins-marker-location — manual docs/for-flow-authors/testing.md is the source of truth for tester.* until multi-file docgen ships post-v1; cmd/skytime-docgen is single-file (pkg/parser/globals.go only) and tester.* lives in pkg/testing/module.go — outside its reach. Existing TestDocgenDrift CI gate stays green by NOT touching docgen-managed files.
- [Phase 05]: Plan 05-06: bareReporter satisfies starlarktest.Reporter (interface{ Error(args ...any) }) without depending on *testing.T — reusable pattern for any future caller (REST endpoint, REPL, scheduled job) that wants to drive the Tier-3 harness from a non-test context
- [Phase 05]: Plan 05-06: Three-layer CLI-03 contract pinning — 'no Go stack traces in default output' asserted at (a) pkg/testing.RunCLI via TestRunCLI_NoGoStackTracesInFailureOutput, (b) pkg/cli/test.go via TestTestCommand_DefaultOutput_NoGoStackTraces, (c) subprocess via TestSkytimeTestE2E_FailureExitNonzero — independent failure modes for one explicit success-criterion phrase
- [Phase 06]: Wave-0 dep scaffolding skips final go mod tidy to preserve direct-require entries until Wave 1 imports them
- [Phase 06]: Plan 06-02: pkg/extension/credfile/ ships as library tier per D-CREDS-LIB. credfile.Resolver implements extension.CredentialHandler over TOML credfile (default $HOME/.skytime-credentials, override via WithPath). Three sealed credential kinds (Bearer/Basic/APIKey) constructed via extension.NewSecret; unknown IDs wrap extension.ErrUnknownCredential. POSIX mode-bit check warns by default, refuses with WithStrictMode (skipped on Windows per Pitfall 4). 19 test results green under -race.
- [Phase 06-example-project-http-github-webhook]: GitHub extension uses 'gogh' alias for go-github/v78 (avoids local package collision); ActionRef construction via struct literal (no NewActionRef constructor in pkg/dag); args coercion helpers added defensively to prevent value-vs-pointer panic seen in HTTP extension (quick 260502-guu pattern)
- [Phase 06]: webhook.post non-idempotent (extension.Ptr(false)) is the load-bearing source of truth for CONTEXT.md success criterion 3
- [Phase 06]: Activity-boundary mechanical pin via ExecuteBatch + counter-mock proves N ActionRefs => N op invocations without parser dependency
- [Phase 06]: 06-05 cmd/extbin: lazy credfile via sync.Once defers credfile.New() to first Resolve so headline demo (public_repo_check.star, no creds) works without ~/.skytime-credentials
- [Phase 06]: 06-05 cmd/extbin: SKYTIME_CREDFILE_PATH env-var override convention; empty string falls through to credfile's default $HOME/.skytime-credentials
- [Phase 06]: Plan 06-06: Step outputs do NOT auto-bind to ctx in v1 (verified pkg/parser/state_schema.go) — only flow.Inputs / script.OutputAlias / if_cond.OutputAlias / for_each.ItemVar populate state. Flows seed iteration lists from script outputs not step outputs; documented in each .star header.
- [Phase 06]: Plan 06-06: call_flow.inputs are literal-only (pkg/parser/builtins.go:1271 starlarkLiteralToGo) — no ${ctx.x} desugaring or ctx-expression support. issue_triage passes literal placeholders to triage_issue subflow; matches existing skeleton/parallel_fanout.star pattern.
- [Phase 06]: Plan 06-06: Coverage-matrix mechanical pinning shape — flows_test.go's expected map per flow is the single source of truth for the README coverage table; primitiveSet walker collapses Step into step_seq/step_block/step_block_fn discriminators; aggregate assertion guarantees every primitive is exercised in the corpus.
- [Phase 06]: Plan 06-06: Plan kwarg names corrected to actual parser API — retry_policy={...} → retry={'max_attempts':N,'initial_interval':'1s'}; timeouts={...} → timeout={'start_to_close':'30s'}; max_in_flight=N → max_concurrency=N. Verified at pkg/dag/retry.go + pkg/parser/builtins.go.
- [Phase 06]: Plan 06-09: setup-go pinned to @v6 (RESEARCH § 5 upgrade from CONTEXT.md @v5) — v6 has built-in module + build cache eliminating actions/cache@v4 step
- [Phase 06]: Plan 06-09: walkthrough smoke extracted to scripts/walkthrough_smoke.sh (not inline ci.yml) — same script invoked by CI and humans for repro; m13 sanity runs flow twice (absolute + relative cwd) to drift-proof README walkthrough
- [Phase 06]: Plan 06-09: assert rendered terminator 'flow complete' (space) per progress_static.go:245 — slog event KIND is flow_complete (underscore) but renderer translates underscore→space; smoke greps the rendered form
- [Phase 06]: Plan 06-09: permissions: contents: read added beyond CONTEXT.md spec — least-privilege hardening, GitHub-Actions-recommended baseline for read-only CI
- [Phase 06]: Plan 06-08: Seven-section example README copied verbatim from D-DOCS-README; coverage matrix copied verbatim from D-FLOWS-COVERAGE-MATRIX (mechanically pinned by 06-06 TestFlows_CoverageMatrix); .skytime-credentials.example refined comment-only with TOML schema lock preserved (D-CREDS-FORMAT).
- [Phase 07]: Trigger does NOT implement dag.Node (Pitfall 11): Triggers are top-level decls, never inside flow.Body; doc comment in pkg/dag/trigger.go explains
- [Phase 07]: TriggerSource is dag-local + structural (Kind + MarshalJSON only); full seal lives in pkg/extension to avoid pkg/dag importing pkg/extension
- [Phase 07]: Default unmarshalTriggerSource returns explanatory error instead of nil panic (helps tests + diagnostics before pkg/extension wires the registry)
- [Phase 07]: FakeTriggerSource lives in package extension (not pkg/extension/testing) — Go forbids sub-packages from satisfying parent-package unexported-method seals
- [Phase 07]: Strict-collision on RegisterTriggerSourceFactory: any second registration of the same kind returns 'extension: trigger source kind %q already registered' (function-pointer equality is unreliable in Go)
- [Phase 07]: Plan 03: findCtxAccesses generalized to findFreeVarAccesses(src, filename, lambdaPos, freeVarName) — ctx-walker delegates via firstParamNameAt; req-walker is a sibling caller. Two-pass cost bounded; TestCtxWalk_* regression-pinned
- [Phase 07]: Plan 03: triggerTimeGlobals 22-key locked surface (lambdaTimeGlobals + json + time); SAFE in trigger lambdas because they run once at HTTP ingress (Phase 7.1+), no replay
- [Phase 07]: Plan 03: warnDuplicateTriggers sig EXCLUDES lambda IDs — byte-identical lambdas at different positions get different D-18 IDs; including would mask the very duplicates the warning targets
- [Phase 07]: Plan 03: TriggerWarnings accumulate on parser state, not slog.Default at finalize — keeps tests deterministic; Plan 04's worker boot drains via slog.Warn at server startup
- [Phase 07]: Plan 03: validateTriggerReqAccesses type-asserts trig.Source against interface{ ReqSchema() []string } — dag.TriggerSource is the minimal seal (Kind+MarshalJSON only); production goes through extension.TriggerSource which provides ReqSchema
- [Phase 07]: Plan 03: Trigger lambdas at parse time use parseTimeGlobals (rich); triggerTimeGlobals is the runtime env that Phase 7.1+ wires at HTTP ingress — matches Phase 1+ predeclared-env-per-call architecture
- [Phase 07]: TriggerRegistry uses sorted-slice + byContentHash map shape (Pitfall 1) — diverges from FlowRegistry's per-(name,hash) lookup because triggers are iterated wholesale at HTTP-mount time, never per-request
- [Phase 07]: Parser warning drain via slog.Default().Warn at boot — keeps parser pure (warnings accumulate on Parser.triggerWarnings); worker boot owns the slog surface
- [Phase 07]: WorkerStopTimeout default 30s (D-07-17) matches Kubernetes terminationGracePeriodSeconds; Plan 05 server subcommand sets explicitly via --drain-timeout
- [Phase 07]: skytime server subcommand uses signal.Notify (NOT NotifyContext) for two-signal escalation; six locked testDrainHook stages enable Phase 7.1 in-process drain testing once worker.WithSDKFactory ships
- [Phase 07]: NewWorkerForTest exported test constructor in pkg/worker enables pkg/cli black-box tests to bypass package-private sdkWorkerNew seam; Worker.FlowNames + FlowRegistry.FlowNames sorted-slice accessors added for the startup banner
- [Phase 07]: Hard rename dev-server -> dev-temporal (D-07-21); no deprecation alias. Pre-1.0, the cost of dragging the legacy name forward is higher than a CHANGELOG migration note.
- [Phase 07]: tests/dev_server_grep_test.go (D-07-22) is the CI gate against re-introducing the legacy literal; allow-list = .planning/ + CHANGELOG.md + the gate file itself.
- [Phase 07]: tests/firewall_credential_redaction_test.go (D-07-10) AST-walks pkg/dag + pkg/extension + pkg/extension/builtin and rejects %+v/%#v in production code; test files exempt; positive regression test guards against matcher over-broadening.
- [Phase 07.1]: Plan 07.1-01: HTTPMounter sub-interface (in pkg/extension/receiver, not on extension.TriggerSource) keeps the Phase 7 sealed core minimal — cron sources opt out by simply not implementing; type assertion is the discriminator with no ok-bool
- [Phase 07.1]: Plan 07.1-01: errorClass taxonomy locked to exactly 7 strings (ok, signature_mismatch, bad_request, lambda_panic, dispatch_failed, event_filtered, duplicate_skipped) per D-7.1-15 — TestStatusMapping_LockedConstants pins values; additions require CONTEXT.md amendment
- [Phase 07.1]: Plan 07.1-01: 415 unsupported_media_type writer ships in Plan 01 (not Plan 04) — D-7.1 'Body decoder negotiation' discretion locked at planning time to 415 over partial fall-through, so all 8 writers (200 success/duplicate/event-filtered, 400, 401, 415, 500, 502) ship together as one foundation primitive
- [Phase 07.1]: Plan 07.1-01: TDD RED+GREEN squashed into one commit for unexported primitives — white-box test files referencing unexported validateHMAC cannot compile-and-pass independently of the implementation file, so test+impl ship as a single GREEN commit (TDD intent preserved in execution order; artifact is one commit per task)
- [Phase 07.1]: Plan 7.1-02: TriggerSourceSeal embeddable carrier (Rule 2 deviation) — pre-existing seal/sub-package contradiction blocked first concrete TriggerSource. Embedding pattern preserves seal intent (must import pkg/extension), unblocks Plan 03 + downstream extensions
- [Phase 07.1]: Plan 7.1-02: factory unmarshaler receives inner config bytes only, not full envelope — extensionTriggerUnmarshaler already peels {kind, config} before dispatching. Fixed plan pseudocode
- [Phase 07.1]: 07.1-05: worker.WithSDKFactory functional Option (D-7.1-13) ships; pkg/cli black-box tests inject fake SDK worker via testWorkerOptions seam; 3 Phase 7 t.Skip stubs (DrainOnSIGTERM, DrainTimeoutExpiry, SecondSignalForceExit) unskipped — SERVER-02 drain semantics now have unit-test coverage
- [Phase 07.1]: 07.1-05: signal.Notify hoisted before hookStage("worker_started") in pkg/cli/server.go — establishes worker_started as deterministic sync point for black-box drain tests; production behavior unchanged
- [Phase 07.1]: Plan 03: Embedded extension.TriggerSourceSeal for seal satisfaction (Plan 02 added the carrier; Plan 03 consumed it). External packages cannot satisfy unexported method names of foreign packages — embedding is the only seam.
- [Phase 07.1]: Plan 03: Pattern — locked-by-construction signature scheme. github.webhook does NOT expose signature_algo/signature_header kwargs (TRIG-09 + D-7.1-04). Source-grep firewall test (TestGithubWebhook_HardcodedAlgoAndHeader) reads webhook.go bytes to prove the kwargs are absent and the algo/header literals are present.
- [Phase 07.1]: Plan 07.1-07 webhook_demo.star — locked NO-explicit-sleep shape: durability proven via cross-activity continuation (kill between activities, restart, label still appears via Temporal event history); v1 has no first-class durable sleep primitive in .star DSL.
- [Phase 07.1]: Plan 07.1-07 — added TestWebhookDemo_BootsCleanly as a dedicated function (not a parametric expansion of TestFlows_ParseAll) so regressions surface at the precise trigger-registration assertion line.
- [Phase 07.1]: Path-level method dispatch via dispatchByMethod helper coalesces (kind, path, method) groups by path because stdlib http.ServeMux is path-only and panics on duplicate registration; method-dispatch happens inside one HandleFunc per path with 405 fallback (D-7.1-09)
- [Phase 07.1]: Extended pkg/activity/firewall_test.go allowedPkgs slice with 'extension/receiver' (Rule 3 deviation) — receiver legitimately needs go.temporal.io/sdk/client for Deps.Client; mirrors prior phase precedent (cli in Phase 4, testing in Phase 5); directory-prefix match keeps pkg/extension and pkg/extension/builtin/* firewalled
- [Phase 07.1]: Plan 04b: Custom buildReqStruct + callLambda over bridge.CallLambda — bridge recursive ToStarlarkStruct broke index access on hyphenated header keys (req.headers['X-...']). buildReqStruct produces Struct with payload (recursive struct, dot access) + headers (frozen *starlark.Dict, index access).
- [Phase 07.1]: Plan 04b: 413 body-too-large written inline rather than amending Plan 01's locked status writer surface; same JSON envelope shape as writeBadRequestResponse.
- [Phase 07.1]: Plan 04b: credentialBytes covers Bearer + Basic + APIKey at the type-switch boundary (only Bearer needed for HMAC today) — centralizes the .Reveal() leak inventory in ONE function for grep audit.
- [Phase 07.1]: Plan 04b: Single squashed commit (479205f) instead of multi-step atomic landing — predecessor agent's stream timed out mid-edit; resuming from disk and squashing was simpler than slicing partial work into RED/GREEN sub-commits retroactively.
- [Phase 07.1]: Plan 06: net.Listen pre-binds synchronously on the RunE foreground goroutine BEFORE the srv.Serve goroutine spawns (Pitfall 9) — 'address already in use' surfaces inline as a RunE error instead of a delayed log line; tests racing on the same port get a deterministic failure path
- [Phase 07.1]: Plan 06: drainCtx is shared between srv.Shutdown AND the worker.Stop goroutine — single context.WithTimeout(--drain-timeout) caps total drain budget across both stages; if listener.Shutdown consumes 80% of the budget, worker.Stop has 20% before drain_timeout fires (D-7.1-11 edge case)
- [Phase 07.1]: Plan 06: 9-stage testDrainHook seam (Plan 05's 6 + 3 new — listener_started, listener_shutdown_started, listener_shutdown_complete); Plan 05's 3 unskipped drain tests updated to expect the full 7-stage sequence per drain path; --addr=127.0.0.1:0 used in all 4 listener-binding tests to avoid port collisions on shared CI runners
- [Phase 07.1]: Plan 06: printStartupBanner type-asserts receiver.HTTPMounter per-trigger and emits 'mount' key only for HTTP-shaped sources; cron (Phase 7.2) and queue (v1.44+) sources omit the key — no nil/empty pollution. Mount string format is '<METHOD> <path>' (e.g. 'POST /webhook/github').
- [Phase 07.1]: Plan 08 — anti-claim assertion in heading-presence gate (bytes.Contains inverted to fail-on-presence) catches doc drift where the dropped '30 seconds later' claim might creep back
- [Phase 07.1]: Plan 08 — cross-package AST firewall (TestReceiverFirewall_HMACOnly) extends Plan 01's in-package source-grep gate to defense in depth across all of pkg/extension/receiver/*.go
- [Phase 07.2]: Plan 01: ReqSchema returns semantic priority [scheduled_time, actual_time] (D-7.2-14 erratum); alphabetical sort is the walker's render-time job, verified by TestTrigger_CronReqAttrTypo's render-order assertion
- [Phase 07.2]: Plan 01: time/tzdata blank import lives in core.go (NOT cron.go) so every binary registering skycore.New() pulls the embedded IANA zoneinfo for scratch/distroless container compatibility
- [Phase 07.2]: Plan 01: fiveFieldParser INTENTIONALLY OMITS cron.Descriptor flag so @-macros are rejected per D-7.2-22 + success criterion #4 (Pitfall 6)
- [Phase 07.2]: Plan 01: CronSource exported as a type alias of unexported cronSource — gives Plan 02 reconciler a type-assertion seam (*skycore.CronSource) without exposing struct/fields; only accessor methods cross the package boundary
- [Phase 07.2]: Plan 01 [Rule 3 deviation]: go mod tidy deferred between Task 1 (// indirect) and Task 2 (import promotes to direct) — plan's tidy instruction contradicted Task 1's grep-based acceptance criteria; tools.go anchor explicitly rejected since robfig/cron/v3 is a runtime dep
- [Phase 07.2]: Memo decode uses converter.GetDefaultDataConverter().FromPayload (symmetric to encode); avoids fragile raw json.Unmarshal(pld.Data) coupling
- [Phase 07.2]: MemoUpdate gap pinned as documented limitation via TestReconcile_UpdateMemoStaysStale_DocumentedLimitation (idempotent thrash bounded; workarounds forfeit operator-State preservation)
- [Phase 07.2]: FakeScheduleClient lives in production code so Plan 03's pkg/cli tests can consume it across package boundaries
- [Phase 07.2]: ContentHash inclusion in canonical config (Pitfall 7): flow .star edits trigger Schedule Updates even when cron config byte-identical, ensuring fired workflows resolve the correct FlowRegistry entry
- [Phase 07.2]: Phase 7.2 Plan 03: loadRegistries lives in pkg/cli (not pkg/worker) — worker.LoadRegistries is one-line public wrapper; pkg/cli adds CLI-side empty-rootdir check + error wrapping. cli.WithScheduleClientFactory parallels worker.WithSDKFactory for cluster-side test seam. Cron banner format: cron @ {schedule} ({timezone}) — distinct from HTTP triggers' {METHOD} {path}.
- [Phase 07.2]: Phase 7.2 Plan 04: Customer-facing surface shipped (weekly_digest.star cron flow + cron-schedules.md walkthrough + cron-schedules-smoke.sh end-to-end smoke + CI gating); Task 4 human UAT pending checkpoint approval.
- [Phase 07.2]: [Phase 07.2 UAT 2026-05-11] Cron-fired workflows auto-inject scheduled_time + actual_time from WorkflowID suffix — Per the deferred D-7.2-20 decision, cron-fired workflows received InitState=nil, so every fire crashed on missing ctx.scheduled_time. UAT surfaced this immediately on criterion #5. Fix landed in pkg/interpreter/cron_input.go::extractScheduledTime (regex over WorkflowID's `-<RFC3339>` suffix per RESEARCH Pitfall 2), auto-injecting both keys inside NewWorkflow when the workflow was Schedule-fired and InitState lacks them. Locked at commit 5270f86. Recovery comment on pkg/extension/schedules/schedules.go:291 updated.
- [Phase 07.2]: [Phase 07.2 UAT 2026-05-11] Example demo cadence stays at "* * * * *" with explicit Production note — The original weekly_digest.star shipped "0 9 * * 1" (Mondays 09:00) — operators reading the walkthrough never observed a fire within the natural reading window. Section 7 "(Optional) Watch a schedule fire" required manual edit + restart. UAT made the criterion #5 lifecycle the load-bearing demo, so cadence flipped to "* * * * *" with an inline code comment + walkthrough Production note subsection directing real deployments to weekly/daily cadences. log_digest script step added at end of flow body using `print() or {"logged": True}` so each fire emits a visible stdout line. Locked at commit a359259. The print() hack is captured as Phase 999.1 backlog (commit f199f8a).
- [Phase 07.2.1]: Plan 01: LogStep Level field is plain string with no Go enum type — parser is the level gate, pkg/dag stays parser-agnostic
- [Phase 07.2.1]: Plan 01: logStepJSON shape mirrors failJSON verbatim — kind/level/msg/msg_lambda_id?/attrs_lambda_id? with omitempty on optional lambda IDs
- [Phase 07.2.1]: Four separate level-tagged log builtin factories (not parameterized) for cleaner doc rendering and crisper error messages
- [Phase 07.2.1]: Source-level string-literal enforcement via AST re-parse + positionsEqual (block_fn_lint pattern) — UnpackArgs alone cannot distinguish literals from identifier-bound strings
- [Phase 07.2.1]: validateLogStepPlacement slotted at finalize position 5.6 (after validateResultPlacement, before validateIfCondExpressionShape) — orphan-log errors surface before branch-shape errors
- [Phase 07.2.1]: Plan 04: logKindFilterHandler decorator uses r.Attrs short-circuit (bounded when both kind+event seen) over collectAttrs/attrMap allocation — avoids per-record map alloc for common non-matching records
- [Phase 07.2.1]: Plan 04: setupServerLogging non-JSON branch wraps inner.Handler() (Go 1.21+) — preserves charm-log debug/TTY/caller settings without duplicating setupLogging body; re-sets slog.Default to wrapped variant so parser warnings via slog.Default().Warn flow through filter
- [Phase 07.2.1]: Plan 04: renderStepComplete kind=log early-return positioned BEFORE failureContext capture so future log-step validation errors do NOT shadow the actual workflow-failing step in renderFlowComplete attribution (D-7.2.1-13)
- [Phase 07.2.1]: Plan 03: walkLog uses workflow.GetLogger(ctx) per-dispatch (NEVER captured at construction) — replay safety delegated to SDK's ReplayLogger per Pitfall 1
- [Phase 07.2.1]: Plan 03: walkBody pre-counts non-log siblings for stepTot and advances stepIdx only on non-log dispatch — log steps inherit surrounding counter context (Pitfall 7 / D-7.2.1-13)
- [Phase 07.2.1]: Plan 03: helpers (starlarkValueToSlogAttr/reservedSlogKeys/identifierShapeRe) inlined in walk_log.go (~215 LOC) — separate walk_log_helpers.go would scatter validation flow across two files
- [Phase 07.2.1]: Plan 05: extended cmd/skytime-docgen with nested *starlarkstruct.Module namespace recognition + trampoline-aware extractUnpackParams + multi-file builtins discovery (Rule 2 deviation) — required for the four log.<level> surfaces to appear in docs/reference/builtins.md without hand-edits that would have broken the drift gate
- [Phase 07.2.1]: Plan 05: backfilled .planning/REQUIREMENTS.md with LOG-01 + LOG-02 (per CONTEXT.md line 178 known gap) using [x] (validated) status matching existing TRIG/SERVER/SCHED convention for shipped phases; totals bumped 31→33 / 7→8 / 6→7
- [Phase 07.3]: All 38 Wave 0 SKIP messages embed plan number + assertion description so Wave 1+ executors get a precise contract without consulting PLAN.md
- [Phase 07.3]: Pre-existing TestNoDevServerLiteralRemains failure (cron-schedules-smoke.sh:192 stale 'dev-server' comment from 07.2.1 afead57) quarantined to deferred-items.md per SCOPE BOUNDARY rule; not auto-fixed in Plan 00
- [Phase 07.3]: Replay heuristic = HistoryLength delta vs configurable threshold (default 50, exposed as DefaultReplayHistoryThreshold for Plan 04 CLI flag wiring) — locks Research Open Question 2
- [Phase 07.3]: Broadcaster Subscribe() captures Snapshot UNDER the broadcaster mutex and returns it inline (Pitfall 1 race fix); the SSE handler MUST use the returned Snapshot and MUST NOT call a separate Snapshot() method
- [Phase 07.3]: Drop-oldest fan-out with 16-deep subscriber buffers + 64-deep publish buffer; test-only 'dispatched' channel hook enables deterministic DropOldest test without time.Sleep (m1 from Phase 7.3 checker)
- [Phase 07.3]: RedactHeaders sanitizes header NAMES via RFC 7230 token regex (M4 XSS defense-in-depth) AND values via case-insensitive substring match — drops malformed names entirely
- [Phase 07.3]: Open Q 4 Option A locked: receiver.Deps exposes optional DeliveryBuffer + OnDelivery (nil-safe; Phase 7.1 backward-compat preserved) instead of importing the events broadcaster directly
- [Phase 07.3-dashboard-manual-trigger-page]: Open Q1 resolved: BuildWorkflowInput is the shared seam (Option 1) — 4 paths converge on flowlaunch, env.ExecuteWorkflow filtered out of firewall by qualifier name
- [Phase 07.3]: m4 strict — TestServerCmd_DrainOnSIGTERM asserts literal event: shutdown frame before EOF (no permissive __closed__ fallback); catches B3 ordering regression mode
- [Phase 07.3]: testListenerAddrFn seam — fires inside RunE after net.Listen succeeds and before listener_started hookStage; surfaces OS-resolved port to tests using --addr=127.0.0.1:0
- [Phase quick-260512-w7c]: Disambiguator appended to D-18 lambda ID (vs. encoded into hash input) preserves byte-identical IDs for empty-disambiguator callers (flow.name, step.name, script.id, fail.msg, log.msg, result.value); kwargs-desugarer uses kwarg key as the disambiguator
- [Phase 07.4]: Plan 05 (D-7.4-06): trace-table column semantics — Source Plan stays 'Phase 3', Status flips Pending→Complete (file convention wins over CONTEXT phrasing).
- [Phase 07.4]: Plan 05 (D-7.4-07): generic_http_check.star is parse-only, exercises http.endpoint(...) + .head + .get; no live runtime test (matches corpus convention; D-7.4-08 explicit defer).
- [Phase 07.4-extbin-consolidation-tech-debt-cleanup]: Plan 07.4-03: NewFakeCredentialHandler defensive-copies its input map and wraps extension.ErrUnknownCredential via %w: %s — production-vs-test errors.Is parity with credfile.Resolver
- [Phase 07.4-extbin-consolidation-tech-debt-cleanup]: Plan 07.4-03: pkg/cli/test.go threads cfg.credHandler nil-guarded — D-7.4-12 two-field thread (exts + credHandler), zero behavior change for cred-less binaries
- [Phase 07.4]: Plan 01: WithCredfile declaration in options.go (D-7.4-04 LOCKED) + ~120-line implementation in pkg/cli/credfile.go; declaration body is one-line delegate to newCredfileHandler
- [Phase 07.4]: Plan 01: applyCredfileFlag helper consolidates per-subcommand --credfile boilerplate (D-7.4-03 spirit); server.go and cron_plan.go each shrink from ~9 lines of inline type-assertion + friendly errors to a 3-line call site
- [Phase 07.4]: Plan 01: 4-step resolution chain owned by credfileHandler — --credfile flag > SKYTIME_CREDFILE_PATH > WithCredfile(arg) > $HOME/.skytime-credentials; lazy init preserved (credfile.New called only on first Resolve under mutex)
- [Phase 07.4]: WithBuildID closure does zero validation (empty string accepted as no-op) — matches WithExtensions/WithCredentialHandler precedent so callers safely pass os.Getenv("BUILD_ID") without guarding
- [Phase 07.4]: WithBuildID inserted between WithExtensions and WithCredentialHandler in options.go (groups plain-config Options at the top, separating from test-seam WithScheduleClientFactory and convenience-wrapper WithCredfile)
- [Phase 07.4]: UseBuildIDVersioning explicitly NOT touched in run.go/server.go — preserves the OPT-IN contract (production deployers register Build ID compatibility set on task queue first, THEN flip versioning manually)
- [Phase 07.4]: Plan 04: WithBuildID OMITTED from extbin per D-7.4-13 line budget (allowed by plan); teaching block at pkg/cli/doc.go documents the full Option surface so library users discover it via godoc, the example just doesn't exercise the third Option
- [Phase 07.4]: Plan 04: Library teaching block belongs on the library package, not the example (D-7.4-14): pkg/cli/doc.go is the canonical 'build your own binary' godoc home; example binaries become thin reference implementations that point AT the godoc
- [Phase 07.4]: Plan 04: Verbatim ≤N-line file-budget collapse strategies in priority order: 1) compress doc-comment to one line, 2) collapse function call into single line if gofmt accepts, 3) drop blank-line separators within a single import group. AVOID helper closures (they grow files, not shrink)

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
| 260504-jtr | Make root `skytime` command print proper errors — render `Error:` + cobra usage block on unknown commands; `.star`-suffix args additionally get `did you mean: skytime run/validate <file>` hint. Replaces previous silent exit 1 from `skytime path/to/flow.star ...`. New `cli.RenderRootError` helper + `cli.ErrAlreadyRendered` exported alias of errSilent; D4-18 invariant preserved verbatim | 2026-05-04 | 25f99d8 | [260504-jtr-make-root-skytime-command-print-proper-e](./quick/260504-jtr-make-root-skytime-command-print-proper-e/) |
| 260504-k9c | Add `skytime info <file.star>` subcommand — prints 3-column Flow/Description/Inputs table in source-declaration order via `text/tabwriter`; em-dash (U+2014) for empty cells; alphabetized inputs keys for determinism; new optional `description=` kwarg on `flow()` builtin → `*dag.Flow.Description string` (json `omitempty`); `Parser.FlowsInOrder()` accessor backed by `flowOrder []string`; `examples/skeleton/expression_if.star` annotated with descriptions on all three flows | 2026-05-04 | c0a4ce2 + 9b7989f + 7ed8edf | [260504-k9c-add-skytime-info-file-star-subcommand-de](./quick/260504-k9c-add-skytime-info-file-star-subcommand-de/) |
| 260504-m65 | Upgrade `skytime info` table renderer from `text/tabwriter` to `charm.land/lipgloss/v2/table` — bordered Unicode-box-drawing table (rounded corners) with bold header via StyleFunc on `table.HeaderRow`. Behavioral contract preserved verbatim (em-dash sentinels, alphabetized input keys, declaration-order rows, exit-code semantics, parse-error stderr path). Library-root firewall extended: `charm.land/lipgloss/v2` added to forbidden list — pkg/cli is the SOLE library-side importer. `go.mod`: lipgloss/v2 v2.0.1 promoted indirect → direct | 2026-05-04 | b12338d + 406ee95 | [260504-m65-upgrade-skytime-info-table-to-charmbrace](./quick/260504-m65-upgrade-skytime-info-table-to-charmbrace/) |
| 260512-w7c | Fix Phase 04.1 latent interpolation bug: multiple `${ctx.expr}` kwargs on the same action factory call collided on a single lambda ID (e.g., `gh.list_open_issues(owner="${ctx.rp.owner}", repo="${ctx.rp.repo}")` evaluated both kwargs to the same value). Thread a `disambiguator` parameter through `desugarInterpolation` → `captureLambdaAtPosition` so `desugarActionRefKwargs` passes the kwarg key, producing `<contenthash>:<line>:<col>:<key>` IDs. Five non-kwarg call sites pass `""` — back-compat preserved. Two regression tests added: parser-tier `TestInterpolation_MultiKwarg_DistinctLambdaIDs` + example-tier `TestPublicRepoCheck_KwargLambdasResolveDistinctly`. CI walkthrough_smoke (red on main since 2026-05-08) now passes | 2026-05-13 | 5b0af66 + 34ac83b + b34cb91 + 9e0e3e3 | [260512-w7c-fix-phase-04-1-interpolation-lambda-id-c](./quick/260512-w7c-fix-phase-04-1-interpolation-lambda-id-c/) |

### Roadmap Evolution

- Phase 04.1 inserted after Phase 4: Dynamic step kwargs — lambda-accepting `step(action_fn=...)` variant for runtime-built action kwargs (URGENT) — surfaced 2026-05-02 when Phase 4's `simple_check.star` corpus demonstrably ignored `--input` because step kwargs are static at parse time. Required before Phase 5 (E2E test harness) and Phase 6 (real example project) can land any flow that takes input.
- Phase 04.2 inserted after Phase 4 (decimal slot taken because 04.1 already exists): if_cond as expression with strict-equality `result` binding (URGENT) — surfaced 2026-05-03 during Phase 04.1 demo verification. Replaces today's procedural if_cond with an expression-shaped variant where each branch may end with `result(value=lambda ctx: {...})`; strict structural equality required across branches; no subtype/LUB rules in v1 (explicit `float(x)` casts handle widening). Implementation is parser-side: new `result` builtin, state-schema tracking through if_cond branches in `pkg/parser/state_schema.go`, dict-literal type inference of `result`'s lambda body. Runtime semantics unchanged. Required before Phase 5 so the E2E test harness designs against the new state-propagation shape from the start.
- Phase 04.3 inserted after Phase 4 (third decimal slot): documentation and source-driven reference generator (URGENT) — surfaced 2026-05-04 after Phase 04.2 shipped. Builds README.md as front door; `docs/` tree split into `for-flow-authors/` + `for-extension-developers/` audience guides; getting-started run-an-example tutorial against `examples/skeleton/expression_if.star`; narrative architecture and two-tier authoring docs; `cmd/skytime-docgen` Copybara-style tool that walks `pkg/parser/builtins.go` AST + parses `UnpackArgs` calls and `// skytime:doc` comment markers to render `docs/reference/` from source; `examples/README.md` indexing all skeleton fixtures. Plain markdown only (no static-site infra). Locked decisions from `/gsd:quick --discuss` exploration: format=plain markdown; audience split=two top-level guides; tutorial=run-an-example smoke; reference=source-driven via skytime-docgen. Required before Phase 5 so the E2E test harness ships with discoverable docs from day one.

## Session Continuity

Last session: 2026-05-13T14:20:15.868Z
Stopped at: Completed 07.4-04-PLAN.md (extbin collapse to 30 lines + teaching block migration to pkg/cli/doc.go)
Resume file: None
