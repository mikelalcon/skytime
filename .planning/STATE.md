---
gsd_state_version: 1.0
milestone: v1.42.0
milestone_name: milestone
status: executing
stopped_at: "Completed 03-04-PLAN.md (Wave 4: pkg/worker bootstrap + library-embed integration test). Phase 3 FEATURE-COMPLETE: INTRP-01..07 + WORK-01..03 all green."
last_updated: "2026-05-01T02:12:05.722Z"
last_activity: 2026-05-01
progress:
  total_phases: 6
  completed_phases: 3
  total_plans: 12
  completed_plans: 12
  percent: 0
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-04-26)

**Core value:** A consultant team can take an extension catalog and a customer brief, write a `.star` file, and have a production-grade durable workflow running on Temporal — without touching Go and without giving up Temporal's retry/timeout/child-workflow guarantees.
**Current focus:** Phase 03 — lambda-serialization-decision-interpreter-worker

## Current Position

Phase: 4
Plan: Not started
Status: Ready to execute
Last activity: 2026-05-01

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

### Pending Todos

None yet.

### Blockers/Concerns

- **Phase 3 entry-gate decision:** Lambda serialization across Temporal history (custom `DataConverter` vs. re-parse-on-start) must be resolved before any interpreter code is written. Default fallback if spike is inconclusive: option (b) re-parse on workflow start with `workflow.SideEffect`, `LambdaID` keys in history, file-content-hash cache.
- **Requirement count discrepancy:** REQUIREMENTS.md header states "51 total" but enumerated requirements sum to 55 (DSL:10 + EXT:6 + PARSE:6 + INTRP:7 + ACT:6 + WORK:3 + VAL:3 + TEST:5 + CLI:5 + EX:4). All 55 are mapped to phases; the header line should be reconciled at the next /gsd:transition.

## Session Continuity

Last session: 2026-05-01T02:07:29.838Z
Stopped at: Completed 03-04-PLAN.md (Wave 4: pkg/worker bootstrap + library-embed integration test). Phase 3 FEATURE-COMPLETE: INTRP-01..07 + WORK-01..03 all green.
Resume file: None
