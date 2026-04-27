---
phase: 01-type-spine-extension-contract-parser-bridge-foundations
plan: 03
subsystem: extension-sdk
tags: [go, go.starlark.net, starlarkstruct, reflection, sealed-interface, sentinel-error, sync.RWMutex, credential-redaction]

requires:
  - 01-01 (pkg/dag/errors.go — ValidationError used by schema.UnpackOperationKwargs)
  - 01-02 (pkg/dag/action.go — *dag.ActionRef used by TestExtensionFactory_ReturnsActionRef; both packages worked in parallel under Wave 1, ActionRef landed at commit 7e7c0a9 just before this plan's task 3)
provides:
  - pkg/extension/extension.go — Extension SDK contract: Name() / Initialize(thread, kwargs) / Operations()
  - pkg/extension/operation.go — OperationFunc (context.Context, NEVER workflow.Context), OperationSpec (Idempotent *bool — D-12), Ptr[T any] helper
  - pkg/extension/credential.go — sealed Credential interface + 3 concrete kinds (BearerCredential, BasicCredential, APIKeyCredential), each with redacted String() format `<credential:<kind>:<id>>`
  - pkg/extension/handler.go — CredentialHandler interface (Resolve(ctx context.Context, id string) (Credential, error)) — Phase 3 wires
  - pkg/extension/registry.go — per-parser *Registry (sync.RWMutex), ErrIdempotentRequired sentinel, Register validates Idempotent != nil
  - pkg/extension/schema.go — FieldSpec, ParseSchema (reflection on `star:"name,required"` tags), UnpackOperationKwargs (returns *dag.ValidationError), assignStarlarkToGo (string/int family/bool/float64/[]string/map[string]string)
  - TestNoTemporalImportsInExtensionPackage firewall — walks all imports via go/parser stdlib, fails if any path contains go.temporal.io
affects:
  - 01-04 (pkg/parser will use Registry.Register, Initialize, UnpackOperationKwargs at the *starlark.Builtin call site, return *dag.ValidationError on kwarg failures)
  - 02 (Phase 2 generic activity calls OperationFunc with stdlib context.Context and a CredentialHandler-resolved Credential)
  - 03 (Phase 3 worker.Run wires WithCredentialHandler option using the interface declared here)
  - 06 (Phase 6 example extensions implement Extension, declare Idempotent: extension.Ptr(...) on every op, register with Registry)

tech-stack:
  added:
    - "go.starlark.net/starlarkstruct (used by TestExtensionFactory_ReturnsActionRef to model the D-08 module-attribute factory pattern; production extensions in Phase 6 will use it for real)"
  patterns:
    - "Sealed interface via unexported method (isCredential()) — prevents downstream packages from inventing new credential kinds without a deliberate API change"
    - "Sentinel error with errors.Is detection (ErrIdempotentRequired wrapped by fmt.Errorf %w) — D-12 enforcement at registration time, surfaceable to callers"
    - "Idempotent as *bool not bool — distinguishes 'author forgot to declare' (nil) from 'author chose false' (*bool=false); registry rejects nil"
    - "Reflection-based kwarg validator with `star:\"name,required\"` tags — single source of truth for kwarg schema, exportable for Phase 4 static validation, position-aware errors via *dag.ValidationError"
    - "Compile-time interface assertions at file scope (var _ CredentialHandler = (*fakeHandler)(nil)) make the contract reviewable without running tests"
    - "TestNoTemporalImportsInExtensionPackage walks the Go AST via go/parser stdlib — proves the firewall at test time, not just at code review"

key-files:
  created:
    - "pkg/extension/extension.go (71 LOC) — Extension interface + package doc"
    - "pkg/extension/operation.go (53 LOC) — OperationFunc, OperationSpec, Ptr helper"
    - "pkg/extension/credential.go (110 LOC) — sealed Credential + 3 redacted kinds"
    - "pkg/extension/handler.go (27 LOC) — CredentialHandler interface"
    - "pkg/extension/registry.go (97 LOC) — Registry + ErrIdempotentRequired"
    - "pkg/extension/schema.go (255 LOC) — FieldSpec, ParseSchema, UnpackOperationKwargs, assignStarlarkToGo"
    - "pkg/extension/extension_test.go (96 LOC)"
    - "pkg/extension/operation_test.go (53 LOC)"
    - "pkg/extension/credential_test.go (121 LOC)"
    - "pkg/extension/handler_test.go (73 LOC)"
    - "pkg/extension/registry_test.go (247 LOC)"
    - "pkg/extension/schema_test.go (484 LOC)"
  modified: []
  deleted:
    - "pkg/extension/doc.go (superseded by extension.go's comprehensive package doc)"

key-decisions:
  - "Idempotent declared as *bool struct field on OperationSpec (NOT a method on an operation interface). Rationale: (a) the field forces extension authors to write Ptr(true) or Ptr(false) at the literal registration site — accidental omission is a nil and the registry rejects it; (b) a method-based design would require a struct OR interface OR a registration helper, while *bool fits cleanly into the existing OperationSpec struct; (c) the *bool approach makes 'forgot to declare' (nil) distinguishable from 'declared false' (non-nil pointing at false). Sentinel ErrIdempotentRequired wraps the validation error so callers detect via errors.Is."
  - "Credential redaction format: `<credential:<kind>:<id>>` — bracket-delimited so it's visually distinct from the secret payloads it replaces, kind-tagged for telemetry routing, and the same shape across all three kinds. Tests assert the secret is absent from %v / %s / %q output; %+v intentionally bypasses String() per Go's convention and is documented as forbidden in any logging path."
  - "Sealed Credential interface via unexported isCredential() method — a deliberate API-evolution gate. Adding a fourth kind (e.g., OAuth2Credential) is a one-file edit in pkg/extension/credential.go; downstream packages cannot do it accidentally. The seal does not prevent reflection-based misuse, but it does prevent a Phase 6 author from declaring `type GitHubCredential struct { ... }` outside this package."
  - "Reflection-based kwarg validator owns ~250 LOC across schema.go (target was ~150 in the plan; landed higher because of (a) the per-Kind switch in assignStarlarkToGo with explicit error messages for each unsupported case, (b) defensive tag parsing for `star:\"-\"` and untagged fields, (c) the position-aware *dag.ValidationError construction at every error path). All errors carry the call-site syntax.Position so D-04 formatting works at the CLI."
  - "Stub credential.go in task 1 commit — task 2's plan section declared credential.go as task 2's file, but operation.go's OperationFunc signature references the Credential interface; without a stub interface in task 1, the Extension contract test would not compile. Resolution: task 1 introduces the sealed interface declaration only (no concrete kinds); task 2 expands the same file with the three concrete kinds. The diff is additive and the seal stays consistent."

requirements-completed: [EXT-01, EXT-02, EXT-03, EXT-04, EXT-05, EXT-06]

duration: 10min
completed: 2026-04-27
---

# Phase 01 Plan 03: pkg/extension SDK Contract — Summary

**Locked the Extension SDK contract Phase 6 implements: sealed Credential interface with three redacted kinds, per-parser Registry that fails loudly on missing Idempotent (D-12), and a hand-rolled reflection-based kwarg validator returning position-aware `*dag.ValidationError`s. 51 tests pass under `-race`; no Temporal imports anywhere in pkg/extension (firewall enforced via Go-AST walk at test time).**

## Performance

- **Duration:** ~10 min
- **Started:** 2026-04-27T16:19:03Z
- **Completed:** 2026-04-27T16:29:04Z
- **Tasks:** 3 (all completed atomically)
- **Files created:** 12 (6 source + 6 test)
- **Files deleted:** 1 (`pkg/extension/doc.go`, superseded)
- **LOC:** 1,687 total (713 production, 974 test)

## Accomplishments

- **EXT-01** Extension interface compiles cleanly with `Name() string`, `Initialize(thread *starlark.Thread, kwargs []starlark.Tuple) (starlark.Value, error)`, `Operations() map[string]*OperationSpec`
- **EXT-02** D-08 module-attribute factory pattern verified end-to-end: `TestExtensionFactory_ReturnsActionRef` walks `Initialize → github.endpoint("admin") → sub-Module → create_issue(repo, title) → *dag.ActionRef` and asserts `CredentialID == "admin"` propagates
- **EXT-03** `OperationFunc` takes `context.Context` (stdlib); `TestNoTemporalImportsInExtensionPackage` walks every Go file's imports via `go/parser` stdlib and asserts none contains `go.temporal.io` — firewall enforced
- **EXT-04** `Idempotent *bool` on `OperationSpec`; `Registry.Register` returns an error wrapping `ErrIdempotentRequired` for nil; tests use `errors.Is(err, ErrIdempotentRequired)`
- **EXT-05** Sealed `Credential` interface with `BearerCredential` / `BasicCredential` / `APIKeyCredential`; each has `ID()` + redacted `String()` (`<credential:bearer:admin>` style); tests assert no secret appears under `%v` / `%s` / `%q`
- **EXT-06** Per-parser `*Registry` (D-07 — no global state) with `sync.RWMutex`; supports static (before-parse) and dynamic (after-parse) registration through the same `Register` API; concurrent `Register`/`Get`/`All` validated under `-race`
- `CredentialHandler` interface (D-10) declared with `Resolve(ctx context.Context, id string) (Credential, error)` — Phase 3 wires the worker registration; Phase 1 owns the contract
- 51 tests pass under `-race -count=1`; `go vet` clean; whole-repo `go build ./...` and `go test ./...` green

## Task Commits

| Task | Name                                                                | Commit  | Files                                                                                  |
| ---- | ------------------------------------------------------------------- | ------- | -------------------------------------------------------------------------------------- |
| 1    | Extension iface + OperationSpec + OperationFunc + Credential stub  | 5b15c4e | extension.go, operation.go, credential.go (stub), extension_test.go, operation_test.go |
| 2    | Credential 3 concrete kinds (redacted) + CredentialHandler          | 0ca30de | credential.go (expand), credential_test.go, handler.go, handler_test.go                |
| 3    | Registry (D-12 enforcement) + reflection kwarg validator (D-11)     | 17dca16 | registry.go, registry_test.go, schema.go, schema_test.go                               |

All three commits use `--no-verify` per the parallel-execution coordination policy (Wave 1 ran 01-02 and 01-03 simultaneously; the orchestrator validates hooks once after the wave completes).

## Files Created/Modified

**Source (6 files, 713 LOC):**
- `pkg/extension/extension.go` — Extension interface; package doc explains the D-08 lifecycle and the no-Temporal firewall
- `pkg/extension/operation.go` — `OperationFunc`, `OperationSpec`, `Ptr[T any]` helper
- `pkg/extension/credential.go` — sealed `Credential` + `BearerCredential` / `BasicCredential` / `APIKeyCredential` with redacted `String()` methods
- `pkg/extension/handler.go` — `CredentialHandler` interface (Phase 3 wires)
- `pkg/extension/registry.go` — `*Registry` + `ErrIdempotentRequired` sentinel + `sync.RWMutex` thread safety
- `pkg/extension/schema.go` — `FieldSpec`, `ParseSchema`, `UnpackOperationKwargs`, `assignStarlarkToGo`

**Test (6 files, 974 LOC):**
- `pkg/extension/extension_test.go` — Extension/OperationFunc compile-time tests + `TestNoTemporalImportsInExtensionPackage`
- `pkg/extension/operation_test.go` — `OperationSpec.Idempotent` is `*bool` (reflection check), `Ptr` round-trip
- `pkg/extension/credential_test.go` — redaction tests for all three kinds (secret absent under `%v`/`%s`/`%q`); seal-method documentation test
- `pkg/extension/handler_test.go` — `fakeHandler` resolve happy/error paths; compile-time signature pin
- `pkg/extension/registry_test.go` — 12 tests including D-12 enforcement, duplicate-name rejection, defensive-nil checks, static+dynamic registration, concurrent Register/Get under `-race`, snapshot-copy semantics
- `pkg/extension/schema_test.go` — 17 tests across `ParseSchema` / `UnpackOperationKwargs` / `assignStarlarkToGo` + `TestExtensionFactory_ReturnsActionRef` exercising the D-08 module-attribute factory pattern end-to-end

**Deleted (1 file):**
- `pkg/extension/doc.go` — superseded by `extension.go`'s comprehensive package doc; the stub doc.go was a 3-line placeholder from Plan 01-01

## Decisions Made

- **`Idempotent *bool` over a method-based declaration** — *bool is a struct field on `OperationSpec`, so the registration site (e.g., `Idempotent: extension.Ptr(false)`) makes the choice visible at code review. A method (`Idempotent() bool`) on an operation interface would have allowed authors to forget the implementation; with a struct field, "forgot" = nil = registry-rejected. The `Ptr[T any]` generic helper makes the literal site ergonomic.
- **`<credential:<kind>:<id>>` redaction format** — uniform shape across `BearerCredential` / `BasicCredential` / `APIKeyCredential`. Bracket-delimited so it's visually distinct from a real secret. Kind-tagged so telemetry can group by auth method without re-reading the underlying type.
- **Sealed `Credential` interface via unexported `isCredential()` method** — adds a deliberate API-evolution gate. New credential kinds require a one-file edit in `pkg/extension/credential.go`; downstream packages cannot accidentally satisfy `Credential` with a custom type.
- **Reflection-based kwarg validator (~250 LOC, target was 150)** — landed higher because (a) `assignStarlarkToGo` has an explicit error message per unsupported case (`unsupported slice element type %s (only []string in phase 1)`), (b) defensive tag parsing rejects empty `star:` names, (c) every error path constructs a `*dag.ValidationError` with the call-site `syntax.Position` for D-04 formatting. The extra LOC bought correctness and error quality, not feature scope.
- **Stub `credential.go` in task 1's commit** — `OperationFunc`'s signature references `Credential`, so the type must exist for task 1's tests to compile. Task 1 introduces the sealed interface (one-method `ID() string` + sealed `isCredential()`); task 2 expands the same file with the three concrete kinds. The diff is additive and the seal stays consistent — no rewrites needed.
- **`TestNoTemporalImportsInExtensionPackage` walks Go AST imports via `go/parser` stdlib** — preferred over a Make/CI grep target. The test runs in the same `go test` invocation as everything else; CI cannot regress without also breaking the test suite. The check covers every `.go` and `_test.go` file in the package directory in O(files * imports) time.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 — Blocking] `thread.CallFrame(1)` panics with "index out of range [-1]" inside the test fake's Builtin**
- **Found during:** Task 3, after writing `TestExtensionFactory_ReturnsActionRef`
- **Issue:** The plan's <action> block sketched `thread.CallFrame(1).Pos` inside `makeCredentialModule`'s `create_issue` Builtin. When the test invokes the Builtin via `starlark.Call(thread, ciAttr, nil, kwargs)`, the call stack inside the Builtin contains only the bottom frame (no caller above index 0), so `CallFrame(1)` triggers the panic at `eval.go:133`.
- **Fix:** Set `Pos: syntax.Position{}` (zero value) in the test's `*dag.ActionRef`. Real Phase-6 extensions will pull the position from `thread.CallStack` after walking down to the right .star-side frame; that detail is the parser's job (Plan 01-04), not Plan 01-03's contract.
- **Files modified:** `pkg/extension/schema_test.go` (one block)
- **Verification:** `TestExtensionFactory_ReturnsActionRef` passes under `-race`; the test's load-bearing assertion is `CredentialID == "admin"`, which is unaffected.
- **Committed in:** 17dca16 (the same task 3 commit)

### Auto-added Critical Functionality

None. The plan was specified at near-line-by-line fidelity; no missing-correctness gaps emerged.

---

**Total deviations:** 1 auto-fixed (1 blocking)
**Impact on plan:** No scope change. The planner's `thread.CallFrame(1)` reference was a benign sketch — the actual call-site position propagation belongs to Plan 01-04's parser, not the test fake.

## Issues Encountered

- **Parallel-wave coordination with Plan 01-02 (pkg/dag).** When Task 3 began, the parallel agent's pkg/dag types (`ActionRef`, `RetryPolicy`, `CapturedLambda`) were not yet committed — the working directory had WIP files referencing undefined types, breaking `go build ./...`. The parallel-execution prompt anticipated this and authorised "a small interface or stub" if needed. In practice, the pkg/dag agent committed `*dag.ActionRef` (commit `7e7c0a9`) before Task 3 reached `schema_test.go`'s test 18, so the stub was unnecessary. Tasks 1 and 2 used only `pkg/dag/errors.go` (committed in Plan 01-01) and were unaffected.

## Authentication Gates

None — this plan is pure-Go contract definition with no external services.

## User Setup Required

None — every test runs offline against in-memory fakes.

## Known Stubs

None. Every contract has at least one passing automated test; nothing is deferred to a follow-up plan.

## Next Phase Readiness

- **Plan 01-04 (pkg/parser, pkg/bridge) is unblocked.** Parser can:
  - construct `*Registry` per-parser (D-07);
  - call `ext.Initialize(thread, kwargs)` to bind extension globals;
  - validate operation kwargs at `*starlark.Builtin` call sites with `UnpackOperationKwargs(opName, callPos, specs, kwargs, target)`;
  - return `*dag.ValidationError` directly when kwargs are malformed;
  - reference `*Credential`, `*CredentialHandler`, `*OperationSpec` types.
- **Plan 01-05 (parser integration / fixture-based tests) is unblocked** — every contract the parser tests exercise is now in place.
- **Phase 2 (generic activity) is unblocked at the contract level** — `OperationFunc(ctx context.Context, args any, cred Credential)` is locked; the activity's call site just plumbs `activity.Context()` into the first parameter.
- **Phase 3 (worker)** can wire `worker.Run(client, flowDir, skytime.WithCredentialHandler(...))` against the `CredentialHandler` interface declared here.
- **Phase 6 (example extensions)** can implement `Extension`, declare `Idempotent: extension.Ptr(...)` on every operation, and pass `Registry.Register(...)` validation at process start.

No blockers. No concerns.

## Self-Check: PASSED

Verified all claimed files exist and all claimed commits are present:

- FOUND: pkg/extension/extension.go
- FOUND: pkg/extension/operation.go
- FOUND: pkg/extension/credential.go
- FOUND: pkg/extension/handler.go
- FOUND: pkg/extension/registry.go
- FOUND: pkg/extension/schema.go
- FOUND: pkg/extension/extension_test.go
- FOUND: pkg/extension/operation_test.go
- FOUND: pkg/extension/credential_test.go
- FOUND: pkg/extension/handler_test.go
- FOUND: pkg/extension/registry_test.go
- FOUND: pkg/extension/schema_test.go
- FOUND: commit 5b15c4e (Task 1)
- FOUND: commit 0ca30de (Task 2)
- FOUND: commit 17dca16 (Task 3)
- VERIFIED: `go build ./pkg/extension/...` exits 0
- VERIFIED: `go vet ./pkg/extension/...` exits 0
- VERIFIED: `go test ./pkg/extension/... -race -count=1` passes (51 tests)
- VERIFIED: `grep -rE '^\s*"go\.temporal\.io|^\s*go\.temporal\.io' pkg/extension/` returns nothing — no Temporal import statements
- VERIFIED: `errors.Is(err, ErrIdempotentRequired)` is the assertion in `TestRegistration_RequiresIdempotent`

---
*Phase: 01-type-spine-extension-contract-parser-bridge-foundations*
*Completed: 2026-04-27*
