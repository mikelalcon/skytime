---
phase: 07-trigger-primitive-server-shell
plan: 02
subsystem: extension-sdk
tags: [trigger, sealed-interface, json-marshal, kind-keyed-registry, fake-stub, credential-redaction]

# Dependency graph
requires:
  - phase: 07-01
    provides: dag.TriggerSource interface, dag.Trigger struct, dag.RegisterTriggerSourceUnmarshaler seam
provides:
  - extension.TriggerSource sealed marker interface (Kind, ReqSchema, MarshalJSON, triggerSourceMarker)
  - Compile-time assertion that extension.TriggerSource satisfies dag.TriggerSource
  - Kind-keyed factory registry (RegisterTriggerSourceFactory) with strict-collision error
  - extensionTriggerUnmarshaler dispatch wired via init() to dag.RegisterTriggerSourceUnmarshaler
  - extension.FakeTriggerSource reusable test stub with skytime.test.webhook + skytime.test.cron factories
affects:
  - 07-03 (parser builtin needs extension.TriggerSource type assertion)
  - 07-04 (TriggerRegistry iterates triggers by Source.Kind)
  - 07.1 (github.WebhookSource is the first real implementation)

# Tech tracking
tech-stack:
  added: []
  patterns:
    - sealed-marker interface (mirrors dag.Node nodeMarker, extension.Credential isCredential)
    - kind-keyed factory registry with cross-package init() seam
    - test stub lives in package extension (NOT sub-package) so seal is satisfiable

key-files:
  created:
    - pkg/extension/trigger.go
    - pkg/extension/trigger_unmarshal.go
    - pkg/extension/trigger_test.go
    - pkg/extension/triggersource_fake.go
  modified: []

key-decisions:
  - "FakeTriggerSource lives in package extension (NOT pkg/extension/testing) because Go forbids sub-packages from satisfying parent-package unexported-method seals"
  - "Strict-collision error on duplicate factory registration (function-pointer equality is unreliable in Go)"
  - "init() in pkg/extension installs the dispatch function via dag.RegisterTriggerSourceUnmarshaler; pkg/dag never imports pkg/extension (cycle prevention)"

patterns-established:
  - "Sealed-marker with cross-package bridge: dag.TriggerSource is structural (Kind + MarshalJSON); extension.TriggerSource adds ReqSchema + the unexported triggerSourceMarker seal; the compile-time assertion var _ dag.TriggerSource = TriggerSource(nil) is the bridge"
  - "Kind-keyed factory registry with init()-time installation: extensions register their own unmarshalers; the dag-side dispatch goes through a single registered function variable"
  - "FakeTriggerSource MarshalJSON envelope: {kind: \"<kind>\", config: {req_fields: [sorted], credential_id: \"<id>\"}} — never an extension.Secret"

requirements-completed: [TRIG-02]

# Metrics
duration: 7min
completed: 2026-05-08
---

# Phase 07 Plan 02: extension.TriggerSource Sealed Interface and Unmarshal Registry Summary

**Sealed extension.TriggerSource SDK contract with kind-keyed factory registry, dag.RegisterTriggerSourceUnmarshaler init-time wiring, and reusable FakeTriggerSource test stub**

## Performance

- **Duration:** ~7 min
- **Started:** 2026-05-08T19:40:59Z
- **Completed:** 2026-05-08T19:48:05Z (approx)
- **Tasks:** 2 (Task 1 production, Task 2 TDD red+green)
- **Files modified:** 4 created, 0 modified

## Accomplishments

- Sealed `extension.TriggerSource` interface with the four-method contract (Kind, ReqSchema, MarshalJSON, triggerSourceMarker seal) — D-07-06.
- Compile-time assertion `var _ dag.TriggerSource = TriggerSource(nil)` guarantees extension values flow through `*dag.Trigger.Source` without runtime checks.
- Kind-keyed factory registry (`RegisterTriggerSourceFactory`) with strict-collision error literal `extension: trigger source kind %q already registered` and clear no-factory error literal.
- Cross-package seam wired at `pkg/extension` package init: `dag.RegisterTriggerSourceUnmarshaler(extensionTriggerUnmarshaler)` — D-07-09 envelope dispatch.
- Reusable `extension.FakeTriggerSource` test stub with `RegisterFakeFactories()` installing `skytime.test.webhook` + `skytime.test.cron` for downstream test packages (parser/dag/worker/interpreter).
- Nine test functions covering TRIG-02 + D-07-09 + D-07-10 (sealed-marker compile + runtime, dag.TriggerSource bridge, registry round-trip via dag.Trigger, strict-collision, empty-kind, nil-fn, no-factory dispatch, missing-kind dispatch, no-Secret-leak in MarshalJSON output).

## Task Commits

Each task was committed atomically:

1. **Task 1: Sealed interface + registry + init() wiring** — `30bb611` (feat)
2. **Task 2 RED: failing tests for FakeTriggerSource and registry round-trip** — `dbe1789` (test)
3. **Task 2 GREEN: FakeTriggerSource stub satisfying TriggerSource seal** — `a1c63e6` (feat)

**Plan metadata commit:** TBD (after STATE.md / ROADMAP.md updates)

## Files Created/Modified

### Created
- `pkg/extension/trigger.go` — sealed TriggerSource interface (4 methods), compile-time `var _ dag.TriggerSource = TriggerSource(nil)` assertion, doc comments on the {kind, config} envelope contract.
- `pkg/extension/trigger_unmarshal.go` — kind-keyed `triggerFactoryRegistry`, `RegisterTriggerSourceFactory`, `extensionTriggerUnmarshaler` dispatch, `init()` calling `dag.RegisterTriggerSourceUnmarshaler`.
- `pkg/extension/triggersource_fake.go` — `FakeTriggerSource` struct + Kind/ReqSchema (sorted)/MarshalJSON ({kind, config} envelope) + `triggerSourceMarker()` seal + `RegisterFakeFactories()` for test reuse.
- `pkg/extension/trigger_test.go` — black-box (`package extension_test`) covering 9 test cases.

### Modified
- None.

## Decisions Made

- **FakeTriggerSource lives in package extension (NOT pkg/extension/testing).** Go's package-visibility rules forbid sub-packages from satisfying a parent's unexported-method seal. Verified at compile-time with the failure: `cannot use ... as extension.TriggerSource value ... unexported method triggerSourceMarker`. The cross-package test-reuse goal is preserved by exporting the type from `pkg/extension` directly. This is documented in the file header doc comment so future readers don't reinstate the broken layout.
- **Strict-collision on RegisterTriggerSourceFactory.** Function-pointer comparison in Go is unreliable for non-nil values, so the registry rejects ALL second registrations of the same kind, returning the error `extension: trigger source kind %q already registered`. `RegisterFakeFactories()` swallows the error with `_ =` so re-invocations across test packages remain safe.
- **`init()` chosen over `Initialize`-time registration for the dispatch wiring.** `extensionTriggerUnmarshaler` is the single dispatch function; it never changes, so `init()` is safe and observable (`grep dag.RegisterTriggerSourceUnmarshaler` finds the seam). Per-source factories still register at `Initialize` time per D-07-08.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] FakeTriggerSource location: pkg/extension/testing/triggersource.go is not satisfiable**
- **Found during:** Task 2 GREEN (compile failure)
- **Issue:** Plan's `<interfaces>` block specified `pkg/extension/testing/triggersource.go` for `FakeTriggerSource`. That layout fails to compile because `triggerSourceMarker()` is unexported in `package extension`, and Go's package-visibility rules forbid sub-packages from implementing parent-package unexported methods. Concrete error: `cannot use &FakeTriggerSource{…} (value of type *FakeTriggerSource) as extension.TriggerSource value in return statement: *FakeTriggerSource does not implement extension.TriggerSource (unexported method triggerSourceMarker)`.
- **Fix:** Moved the stub into `pkg/extension/triggersource_fake.go` (package `extension`). The cross-package test-reuse goal is preserved because `FakeTriggerSource` is exported from `pkg/extension` directly. Test files now `import "github.com/mikelalcon/skytime/pkg/extension"` and reference `extension.FakeTriggerSource` instead of `exttest.FakeTriggerSource`.
- **Files modified:** `pkg/extension/triggersource_fake.go` (new), `pkg/extension/trigger_test.go` (updated import + references via `sed s/exttest.FakeTriggerSource/extension.FakeTriggerSource/g`).
- **Verification:** All 9 tests pass; `go vet ./pkg/extension/... ./pkg/extension/testing/...` clean.
- **Committed in:** `a1c63e6` (Task 2 GREEN commit).

---

**Total deviations:** 1 auto-fixed (1 bug fix)
**Impact on plan:** No scope creep. The deviation is a pure layout fix forced by Go's language rules; the plan's intent (reusable test stub satisfying the seal) is preserved verbatim. Acceptance criteria using grep on `pkg/extension/testing/triggersource.go` will need to be updated in any future plan or verifier — the file path is `pkg/extension/triggersource_fake.go`.

## Issues Encountered

- **Wave-1 parallel race with Plan 01.** Plan 02 imports `dag.RegisterTriggerSourceUnmarshaler` and `dag.TriggerSource`, which are produced by Plan 01. Both plans were spawned in Wave 1 in parallel; my Task 1 build initially failed because Plan 01 had only shipped Task 1 (the trigger.go struct) but not Task 2 (the marshal.go registry seam). Resolved by polling the dag package via `until grep -q "func RegisterTriggerSourceUnmarshaler" pkg/dag/*.go; do sleep 5; done`. After Plan 01's Task 2 landed, Plan 02 built cleanly. Total wait: ~3 minutes.

## User Setup Required

None — no external service configuration required. The `RegisterFakeFactories()` helper is test-only.

## Next Phase Readiness

- **Plan 03 (parser trigger builtin)** unblocked: the parser's `builtinTrigger` can type-assert `sourceVal.(extension.TriggerSource)` and emit `*dag.Trigger` with the captured Source; downstream test packages can use `extension.FakeTriggerSource` as a stand-in for the future `github.WebhookSource`.
- **Plan 04 (TriggerRegistry)** unblocked: registry can iterate triggers grouped by `Source.Kind()` with confidence the value satisfies both `extension.TriggerSource` (sealed) and `dag.TriggerSource` (structural).
- **Phase 7.1 (github.WebhookSource)** unblocked: the SDK contract is locked. The first real source implements the four-method interface verbatim, registers via `extension.RegisterTriggerSourceFactory("github.webhook", ...)` at the github extension's `Initialize` time, and round-trips via the existing dispatch path.
- **No blockers.** Plan 06's firewall test will assert no `%+v` / `%#v` against `*dag.Trigger` or any `TriggerSource` concrete type. Plan 02's `TestFakeTriggerSource_NoSecretInConfig` is the unit-level credential-redaction test referenced from D-07-10.

## Self-Check: PASSED

- Files: pkg/extension/trigger.go, pkg/extension/trigger_unmarshal.go, pkg/extension/triggersource_fake.go, pkg/extension/trigger_test.go all exist on disk.
- Commits: 30bb611 (Task 1), dbe1789 (Task 2 RED), a1c63e6 (Task 2 GREEN) all present in git log.
- All 9 plan tests pass under `-race`; full `pkg/extension/...` + `pkg/dag/...` suites green; `go build ./...` clean.

---
*Phase: 07-trigger-primitive-server-shell*
*Completed: 2026-05-08*
