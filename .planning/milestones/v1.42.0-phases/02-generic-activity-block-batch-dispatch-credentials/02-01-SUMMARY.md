---
phase: 02-generic-activity-block-batch-dispatch-credentials
plan: 01
subsystem: type-spine
tags: [temporal-sdk, sealed-sum, secret-wrapper, parser-lint, kwargs-decode, action-result, operation-output]

# Dependency graph
requires:
  - phase: 01-type-spine-extension-contract-parser-bridge-foundations
    provides: Credential sealed interface, OperationFunc/OperationSpec, parser finalize hook, schema.UnpackOperationKwargs, fakeExtension test harness, fixture corpus loop
provides:
  - "go.temporal.io/sdk@v1.42.0 pinned via tools.go anchor (no production import yet)"
  - "extension.Secret wrapper with String / GoString / Format / MarshalJSON / MarshalText redaction; .Reveal() greppable audit boundary"
  - "extension.ErrUnknownCredential sentinel for D2-12 retry classification"
  - "pkg/extension/testing.FakeCredentialHandler reusable test double"
  - "dag.ActionResult sealed sum (OkResult / RetryableErrResult / NonRetryableErrResult / SkippedResult)"
  - "dag.OperationOutput marker interface (D2-04 narrowing of OperationFunc return type)"
  - "extension.Credential kinds use Secret-typed fields (Token, Password, Key)"
  - "extension.OperationSpec.DefaultTimeout time.Duration field (D2-15)"
  - "extension.DecodeKwargsFromDict — runtime-path companion to UnpackOperationKwargs"
  - "parser.WithMaxBlockSize option + lintMixedIdempotency + lintBlockSize finalize passes"
  - "tests/fixtures/invalid/09-mixed-idempotency.star + 10-block-oversized.star"
affects: [02-02-cache-heartbeat-classify, 02-03-execute-batch-wiring, phase-03-interpreter, phase-06-example-extensions]

# Tech tracking
tech-stack:
  added: ["go.temporal.io/sdk v1.42.0 (anchored, not imported by production code)"]
  patterns:
    - "Cross-package marker interface via exported method (IsOperationOutput) — deviation from D2-03's unexported sketch because Go's package-private rule blocks cross-package implementations"
    - "tools.go build-tag anchor to prevent go mod tidy from pruning future-use SDK dep"
    - "Sealed sum interface mirroring Phase 1 Credential pattern (unexported isActionResult method) — pkg/dag is sole producer so the seal is internally consistent"
    - "Secret wrapper with comprehensive fmt.Formatter coverage — covers %v/%+v/%#v/%s/%q via single Format method"
    - "Recursive walk pattern for parser lints, mirroring finalize.go's walkResolveCallFlows shape"

key-files:
  created:
    - "pkg/extension/secret.go (73 LOC)"
    - "pkg/extension/secret_test.go (174 LOC)"
    - "pkg/extension/testing/fake_handler.go (58 LOC) — new sub-package"
    - "pkg/extension/testing/fake_handler_test.go (85 LOC)"
    - "pkg/dag/output.go (48 LOC)"
    - "pkg/dag/output_test.go (59 LOC)"
    - "pkg/dag/result.go (104 LOC)"
    - "pkg/dag/result_test.go (100 LOC)"
    - "tools.go (24 LOC) — go.temporal.io/sdk anchor"
    - "tests/fixtures/invalid/09-mixed-idempotency.star"
    - "tests/fixtures/invalid/10-block-oversized.star"
  modified:
    - "go.mod / go.sum — go.temporal.io/sdk v1.42.0 pinned"
    - "pkg/extension/credential.go — Token/Password/Key are now Secret"
    - "pkg/extension/credential_test.go — Secret-typed assertions + redaction-matrix table test"
    - "pkg/extension/handler.go — added ErrUnknownCredential sentinel"
    - "pkg/extension/handler_test.go — TestErrUnknownCredential_IsErrorsIsCompatible + Secret-typed BearerCredential construction"
    - "pkg/extension/operation.go — OperationFunc returns dag.OperationOutput; OperationSpec.DefaultTimeout"
    - "pkg/extension/operation_test.go — TestOperationFunc_ReturnsOperationOutput + DefaultTimeout reflection check"
    - "pkg/extension/extension_test.go — signature update + dag import"
    - "pkg/extension/registry_test.go — signature update + dag import"
    - "pkg/extension/schema.go — DecodeKwargsFromDict appended"
    - "pkg/extension/schema_test.go — 9 new TestDecodeKwargsFromDict_* cases"
    - "pkg/parser/parser.go — maxBlockSize field + defaultMaxBlockSize constant"
    - "pkg/parser/options.go — WithMaxBlockSize option"
    - "pkg/parser/options_test.go — default + override + invalid-cap tests"
    - "pkg/parser/linter.go — lintMixedIdempotency / lintBlockSize / splitKind"
    - "pkg/parser/linter_test.go — 8 new lint tests"
    - "pkg/parser/finalize.go — invokes both new lints between resolveCallFlows and validateActionRefKwargs"
    - "pkg/parser/finalize_test.go — TestFinalize_LintOrder_CallFlowResolutionShortCircuits"
    - "pkg/parser/builtins_test.go — fakeExtension exposes echo (idempotent) + post (non-idempotent) for D2-05 fixtures"
    - "pkg/parser/fixtures_test.go — signature update on fakeGithubExtension"
    - "pkg/parser/parser_test.go — noopOpFunc signature update"

key-decisions:
  - "OperationOutput marker uses EXPORTED IsOperationOutput method (deviation from D2-03's `isOperationOutput()` lowercase sketch). Rationale: D2-03 also says extension authors in pkg/examples/* declare typed Output structs; an unexported method on an interface is package-private and unsatisfiable across packages. Exported-method form is the standard Go cross-package marker idiom (cf. proto.Message); the seal is social rather than syntactic."
  - "Temporal SDK pinned via tools.go build-tag anchor. go mod tidy would otherwise prune the dep until pkg/activity (Wave 1) imports it. Anchor file is removed once the SDK is genuinely consumed."
  - "ActionResult marker stays unexported (isActionResult). pkg/dag is the sole producer (Phase 2 activity constructs them, Phase 3 interpreter consumes them); no external package needs to declare a new variant, so the package-private seal works as intended."
  - "Test fixture 10-block-oversized.star uses Starlark list comprehension (`[fake_ext.echo(msg='m') for _ in range(51)]`) — TopLevelControl=true in defaultFileOptions allows this and the fixture corpus harness tolerates it cleanly."
  - "fakeExtension extended with a non-idempotent `post` operation (alongside the existing idempotent `echo`). Doing this on the existing test type avoids registering a second fake extension just for D2-05 — keeps the corpus harness simple."

patterns-established:
  - "Pattern: Marker interface via exported method when extensions span packages. Sacrifices syntactic seal for cross-package usability; documented in pkg/dag/output.go SEAL PROPERTY note."
  - "Pattern: tools.go anchor — keep an unused dep pinned via build-tagged blank import. Standard Go-tooling idiom, used here for go.temporal.io/sdk."
  - "Pattern: dual-entry-point reflective decoder (UnpackOperationKwargs at parse time, DecodeKwargsFromDict at runtime) sharing the FieldSpec/ParseSchema reflection. Schema evolution requires editing assignStarlarkToGo once; both entry points pick it up."

requirements-completed: [ACT-02, ACT-03, ACT-05]

# Metrics
duration: 30min
completed: 2026-04-28
---

# Phase 2 Plan 01: Wave 0 Type Spine — Secret Wrapper, Sealed Sums, Parser Lints Summary

**Wave 0 lands the type-spine that 02-02 and 02-03 will both consume: Secret-typed Credentials closing the %+v/json/slog leak surface, sealed ActionResult sum + OperationOutput marker in pkg/dag, exported ErrUnknownCredential sentinel, reusable FakeCredentialHandler sub-package, narrowed OperationFunc signature, OperationSpec.DefaultTimeout, runtime-path DecodeKwargsFromDict, and parse-time defenses (mixed-idempotency reject + block-size cap) so the activity layer can rely on the parser as a primary gate.**

## Performance

- **Duration:** ~30 min
- **Started:** 2026-04-28T01:48:00Z (approx)
- **Completed:** 2026-04-28T02:18:00Z (approx)
- **Tasks:** 4/4 complete (autonomous, no checkpoints)
- **Files created:** 11
- **Files modified:** 22

## Accomplishments

- **Secret wrapper closes the %+v/json/slog leak surface (D2-08/D2-09)** — five formatter methods (`String`, `GoString`, `Format`, `MarshalJSON`, `MarshalText`) all emit `<redacted>`; raw value reachable only via `.Reveal()` (greppable audit boundary). Verified across all six fmt verbs + struct embed + json.Marshal round-trip.
- **Type spine for Phase 2/3 wire format** — `dag.ActionResult` sealed sum (4 kinds), `dag.OperationOutput` marker, `pkg/extension.OperationFunc` narrowed to return `OperationOutput` (was `any`), `OperationSpec.DefaultTimeout` added.
- **Parser becomes the primary D2-05/D2-07 gate** — `lintMixedIdempotency` + `lintBlockSize` reject mixed-idempotency batches and over-cap blocks at parse time with position-aware `*dag.ValidationError`. `WithMaxBlockSize(N)` overrides default cap of 50.
- **All 245+ Phase 1 tests still pass** — Phase-1-incompatible field-type changes (`Token: string` → `Token: Secret`) and signature changes (`OperationFunc` return type narrow) handled inline in test fakes; full module + race-detector suite green.

## Task Commits

Each task was committed atomically. The full Phase 2 Wave 0 lands across four feat commits:

1. **Task 1: Add Temporal SDK + Secret wrapper + ErrUnknownCredential + FakeCredentialHandler** — `3fb45c0` (feat)
2. **Task 2: Phase 1 type narrowing — Secret-typed Credentials, OperationOutput marker, ActionResult sealed sum** — `dfbd726` (feat)
3. **Task 3: Parser linter passes for mixed-idempotency + block-size cap** — `29a7895` (feat)
4. **Task 4: DecodeKwargsFromDict — runtime-path kwargs decoder** — `e6e506a` (feat)

## Files Created/Modified

### Created (11 files)

- `pkg/extension/secret.go` — `Secret` wrapper, `NewSecret`, `Reveal`; five formatter methods (D2-08/D2-09)
- `pkg/extension/secret_test.go` — full redaction matrix + Marshal* + zero-value tests
- `pkg/extension/testing/fake_handler.go` — new sub-package with `FakeCredentialHandler` (D2-12)
- `pkg/extension/testing/fake_handler_test.go` — interface satisfaction + hit/miss + nil-map tests
- `pkg/dag/output.go` — `OperationOutput` marker (D2-03/D2-04, exported method)
- `pkg/dag/output_test.go` — type-implements-marker + documented compile-time assertion
- `pkg/dag/result.go` — `ActionResult` sealed sum + 4 kinds (D2-01/D2-02)
- `pkg/dag/result_test.go` — sealed-sum + type-switch exhaustiveness tests
- `tools.go` — build-tag anchor for `go.temporal.io/sdk` (prevents `go mod tidy` pruning)
- `tests/fixtures/invalid/09-mixed-idempotency.star` — D2-05 fixture
- `tests/fixtures/invalid/10-block-oversized.star` — D2-07 fixture (51 actions via list-comprehension)

### Modified (22 files)

- `go.mod` / `go.sum` — `go.temporal.io/sdk v1.42.0` + transitive deps
- `pkg/extension/credential.go` — `BearerCredential.Token`, `BasicCredential.Password`, `APIKeyCredential.Key` retyped from `string` to `Secret`
- `pkg/extension/credential_test.go` — Secret-typed construction; new `TestCredentials_RedactedInAllFormats` table test
- `pkg/extension/handler.go` — `ErrUnknownCredential` sentinel
- `pkg/extension/handler_test.go` — `TestErrUnknownCredential_IsErrorsIsCompatible`; `BearerCredential.Token` updated to `NewSecret(...)`
- `pkg/extension/operation.go` — `OperationFunc` returns `dag.OperationOutput`; `OperationSpec.DefaultTimeout time.Duration`
- `pkg/extension/operation_test.go` — `TestOperationFunc_ReturnsOperationOutput` + `TestOperationSpec_DefaultTimeoutZeroIsNoTimeout`
- `pkg/extension/extension_test.go` / `pkg/extension/registry_test.go` / `pkg/extension/schema_test.go` — `OperationFunc` signature updates + `dag` import
- `pkg/extension/schema.go` — appended `DecodeKwargsFromDict`
- `pkg/extension/schema_test.go` — appended 9 `TestDecodeKwargsFromDict_*` tests
- `pkg/parser/parser.go` — `maxBlockSize` Parser field + `defaultMaxBlockSize` constant
- `pkg/parser/options.go` — `WithMaxBlockSize(n)` option
- `pkg/parser/options_test.go` — default + override + invalid-cap tests
- `pkg/parser/linter.go` — `lintMixedIdempotency`, `lintBlockSize`, `splitKind`
- `pkg/parser/linter_test.go` — 8 new lint tests covering single/mixed/nested/cap-boundary cases
- `pkg/parser/finalize.go` — invokes both new lints between `resolveCallFlows` and `validateActionRefKwargs`
- `pkg/parser/finalize_test.go` — `TestFinalize_LintOrder_CallFlowResolutionShortCircuits`
- `pkg/parser/builtins_test.go` — `fakeExtension` extended with non-idempotent `post` op
- `pkg/parser/fixtures_test.go` / `pkg/parser/parser_test.go` — `OperationFunc` signature updates

## Decisions Made

| Decision | Rationale |
| --- | --- |
| `OperationOutput.IsOperationOutput()` is **exported** (deviation from D2-03 sketch) | An unexported marker method on an interface is package-private; cross-package types cannot satisfy it. D2-03 explicitly intends extension authors in `pkg/examples/*` to declare typed Output structs. The exported-method form is the standard Go cross-package marker idiom. The "seal" becomes social: external types must explicitly declare the method, which is a deliberate opt-in code change reviewers can spot. Documented in `pkg/dag/output.go` SEAL PROPERTY note. |
| Temporal SDK pinned via `tools.go` build-tag anchor | Plan requires the SDK to land at v1.42.0 even though no Wave 0 code imports it — `go mod tidy` would otherwise prune the entry. Standard Go-tooling idiom (used by gRPC, Kubernetes, etc.); anchor will be removed once `pkg/activity` (Wave 1) imports the SDK directly. |
| `ActionResult` keeps unexported `isActionResult()` seal | Unlike `OperationOutput`, no external package needs to declare a new `ActionResult` variant. `pkg/dag` is the sole producer; the activity (Phase 2) constructs and the interpreter (Phase 3) consumes via type switch. The package-private seal works as intended without a cross-package implementation requirement. |
| Test fixture 10 uses Starlark list comprehension | The parser's `defaultFileOptions` enables `TopLevelControl: true`, and Starlark's `[expr for x in range(n)]` form is part of the core language. Fixture 10 builds 51 echoes via `[fake_ext.echo(msg="m") for _ in range(51)]` — keeps the fixture compact and the corpus loop tolerates parse-time computation cleanly. |
| `fakeExtension` extended in place rather than creating a second fake | The Phase 1 corpus harness already wires `fakeExtension` for fixtures; D2-05 needs a non-idempotent op alongside the existing idempotent `echo`. Adding `post` to the same fake (rather than registering a separate non-idempotent extension) keeps the corpus loop unchanged and avoids fixture-coordination overhead. |
| `WithMaxBlockSize(n)` rejects `n < 1` at construction time | Two options were considered: error vs. "no cap" semantics. Error chosen so misconfiguration is fast-fail rather than silently permissive. The activity (Phase 2) defensively re-enforces, but parser-side fast-fail surfaces the bug at the call site. |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 — Bug] D2-03's `isOperationOutput()` unexported method is unsatisfiable across packages**

- **Found during:** Task 2 (test compilation)
- **Issue:** D2-03 specifies `OperationOutput interface { isOperationOutput() }` (lowercase, unexported) AND says "extension authors write typed Output structs that implement the marker". In Go, an interface with an unexported method can only be satisfied by types defined in the same package as the interface. Extension authors in `pkg/examples/*` (or even in `pkg/extension`) cannot satisfy an `isOperationOutput` marker defined in `pkg/dag`. The two parts of D2-03 are mutually contradictory.
- **Fix:** Renamed the marker method to exported `IsOperationOutput()`. Documented the deviation prominently in `pkg/dag/output.go` under "SEAL PROPERTY". The seal becomes social rather than syntactic — external types must explicitly declare the method (a deliberate opt-in code change reviewers can spot), which preserves the "must opt in" intent of D2-03 while permitting cross-package implementations.
- **Files modified:** `pkg/dag/output.go`, `pkg/dag/output_test.go`, `pkg/dag/result_test.go`, `pkg/extension/operation_test.go`
- **Verification:** All Phase 2 type-spine tests pass; the documented compile-time assertion (commented out in `output_test.go`) confirms an un-marked type still fails to satisfy the interface.
- **Committed in:** `dfbd726` (Task 2)

**2. [Rule 3 — Blocking] `go mod tidy` prunes `go.temporal.io/sdk` because no production code imports it yet**

- **Found during:** Task 1
- **Issue:** Plan requires `go list -m go.temporal.io/sdk` to print `v1.42.0` after Task 1, but `go mod tidy` removes any `require` line whose package is not imported. No Wave 0 code imports the SDK (Wave 1's `pkg/activity` will, in 02-02/02-03), so the dep would be pruned immediately.
- **Fix:** Created `tools.go` at the repo root with `//go:build tools` constraint and a blank import of `go.temporal.io/sdk/activity`. The build tag ensures the file never compiles into a real binary, and the blank import keeps `go mod tidy` from pruning the entry. Standard Go-tooling idiom (used by gRPC, Kubernetes, kubectl, etc.).
- **Files modified:** `tools.go` (new), `go.mod`, `go.sum`
- **Verification:** `go list -m go.temporal.io/sdk` prints `go.temporal.io/sdk v1.42.0`; `go mod tidy` is idempotent; `go test ./... -tags=` (no `tools` tag) shows the file is excluded from normal builds.
- **Committed in:** `3fb45c0` (Task 1)

## Authentication Gates

None — Task 1 added a Go module dep (no auth needed).

## Phase 1 Backward Compatibility

Phase 1's 245+ tests still pass. The signature changes (Token/Password/Key field types, OperationFunc return type) are backward-incompatible at the Go API level, but Phase 1 has no real consumers — only test fakes in `pkg/extension/*_test.go` and `pkg/parser/*_test.go`. All test fakes were updated inline; the firewall test (`TestNoTemporalImportsInExtensionPackage` / `...InParserPackage`) confirms `go.temporal.io/sdk` is in `go.mod` but not imported by any non-`tools.go` file.

## Forwards Compatibility (Wave 1 / Wave 2)

The type spine 02-02 and 02-03 will consume:

- `dag.ActionResult` + 4 kinds — wire format from Phase 2 activity to Phase 3 interpreter
- `dag.OperationOutput` — every extension's typed return type implements `IsOperationOutput()`
- `extension.Secret` — used by all `Credential`-bearing structs going forward
- `extension.ErrUnknownCredential` — D2-12 retry classification sentinel
- `extension.OperationSpec.DefaultTimeout` — D2-15 sum-of-timeouts compute base
- `extension.DecodeKwargsFromDict` — runtime kwargs decoder for the activity dispatch loop
- `pkg/extension/testing.FakeCredentialHandler` — test double for `pkg/activity` integration tests
- `parser.WithMaxBlockSize` — defense-in-depth parser-side cap that the activity defensively re-enforces

## Self-Check: PASSED

All success criteria verified:

- [x] All 4 tasks executed per plan acceptance criteria
- [x] Each task committed individually (`3fb45c0`, `dfbd726`, `29a7895`, `e6e506a`)
- [x] `go test ./... -race -count=1` exits 0
- [x] `go vet ./...` clean
- [x] `go build ./...` clean
- [x] `go list -m go.temporal.io/sdk` prints `go.temporal.io/sdk v1.42.0`
- [x] No imports of `go.temporal.io` in `pkg/dag`, `pkg/extension`, `pkg/parser` (only the `tools.go` anchor file at the repo root)
- [x] `Secret` type has all five formatter methods (String, GoString, Format, MarshalJSON, MarshalText)
- [x] `DecodeKwargsFromDict` exists with documented signature; round-trip test passes
- [x] Two new fixtures parse-fail with expected error messages (corpus harness verified)
- [x] `pkg/dag/result.go` defines `ActionResult` sealed sum + 4 kinds
- [x] `pkg/dag/output.go` defines `OperationOutput` marker
- [x] `pkg/extension/credential.go` uses `Secret` for Token/Password/Key
- [x] `pkg/extension/operation.go` returns `OperationOutput`; `OperationSpec.DefaultTimeout` added
- [x] `pkg/extension/handler.go` exports `ErrUnknownCredential`
- [x] `pkg/extension/testing/fake_handler.go` exists as a sub-package
- [x] `pkg/parser/linter.go` adds `lintMixedIdempotency` + `lintBlockSize`
- [x] `pkg/parser/options.go` adds `WithMaxBlockSize`
- [x] `pkg/parser/finalize.go` invokes both new lints
- [x] 2 new invalid fixtures exist and are caught by the corpus test loop
