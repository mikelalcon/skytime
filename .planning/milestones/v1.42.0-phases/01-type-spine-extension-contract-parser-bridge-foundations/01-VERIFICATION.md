---
phase: 01-type-spine-extension-contract-parser-bridge-foundations
verified: 2026-04-26T00:00:00Z
status: passed
score: 22/22 must-haves verified
---

# Phase 1: Type Spine + Extension Contract + Parser/Bridge Foundations — Verification Report

**Phase Goal:** Lock the data types every later phase imports (`pkg/dag`, `pkg/extension`), implement the Starlark parser with all six DSL primitives (`flow`, `step`, `if_cond`, `script`, `for_each_parallel`, `call_flow`), and stand up the state/lambda bridge so a `.star` file can be parsed into an inspectable `dag.Flow` with **zero Temporal involvement**.

**Verified:** 2026-04-26
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

Phase 1 delivered the type contract, extension SDK, parse-time builtins, and lambda bridge that every later phase imports — entirely without Temporal. The 22 v1 requirements assigned to this phase (DSL-01..10, EXT-01..06, PARSE-01..06) each have ≥ 1 passing automated test. The architectural firewall (`grep -rE '^\s*"go\.temporal\.io|^\s*go\.temporal\.io' pkg/`) returns 0 import-line matches.

### Observable Truths (mapped to ROADMAP.md Phase 1 success criteria)

| #   | Success Criterion / Truth                                                                                                                                                                                                                                                                                  | Status     | Evidence |
| --- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------- | -------- |
| 1   | A consultant can write a `.star` file using all six primitives and produce a complete `dag.Flow` whose nodes can be inspected in a Go unit test, with every node carrying a `syntax.Position`                                                                                                              | ✓ VERIFIED | `tests/fixtures/valid/02-all-primitives.star` parses through `TestValidFixtures/02-all-primitives.star`; goldens regenerated to real parser output (lambda IDs `3b92ab29:19:20`, `3b92ab29:25:18`, `3b92ab29:29:21`). All 6 node types implement `Node` interface with `Position() syntax.Position` (compile-time `var _ Node = (*X)(nil)` assertions in `pkg/dag/node_test.go`, `node.go:Kind/Position/nodeMarker`). |
| 2   | A library developer can implement a working `Extension` (`Name()`, `Initialize`, `Operations()`), register statically/dynamically, and have its factory return an `ActionRef` intent — with no path to import `go.temporal.io/sdk/activity` or register a Temporal activity                                | ✓ VERIFIED | `Extension` interface in `pkg/extension/extension.go`; `TestExtension_FakeImplementorExposesName`, `TestExtension_InitializeReturnsStarlarkValue`, `TestExtensionFactory_ReturnsActionRef`, `TestRegistration_StaticAndDynamic`, `TestExtensionEndpointFactory_PropagatesCredentialID` all green. `TestNoTemporalImportsInExtensionPackage` and `TestNoTemporalImportsInParserPackage` walk the AST via `go/parser` and assert no `go.temporal.io/*` imports anywhere. |
| 3   | A malformed `.star` file produces a position-aware error of the form `<file>:<line>:<col>: <message>` and never panics                                                                                                                                                                                     | ✓ VERIFIED | `pkg/dag/errors.go:ParseError.Error()` formats `%s:%d:%d: %s` when Pos is valid; `TestParseError_ErrorWithValidPos` asserts. `TestParse_NeverPanicsOnGarbage` exercises 5 garbage inputs (random ASCII, unclosed paren, raw bytes, empty, comment-only) and `TestInvalidFixtures` runs all 8 invalid fixtures, asserting each `# expects:` substring matches the typed `*dag.ParseError`/`*dag.ValidationError`. |
| 4   | The bridge converts a nested Go state map to `*starlarkstruct.Struct` with deterministic key order, evaluates a captured lambda using `ctx.req.repo_name`-style dot access on a fresh `*starlark.Thread` per call (with `MaxExecutionSteps` set), returns a frozen result — verified by an iter-determinism unit test asserting byte-equal Starlark dicts | ✓ VERIFIED | `TestToStarlarkStruct_Deterministic` (same-input byte-equal output), `TestToStarlarkStruct_LargeMap` (100 keys), `TestToStarlarkStruct_DotAccess_DSL09`, `TestCallLambda_FreshThread`, `TestCallLambda_DotAccess_DSL09`, `TestCallLambda_DefaultMaxExecutionStepsConstant` (10_000_000), `TestCallLambda_ConcurrentSafety` (50 parallel calls under -race) — all green. `pkg/bridge/struct.go` uses `sort.Strings(keys)` before iterating Go maps. |
| 5   | The parser uses two distinct predeclared environments — a richer parse-time environment and a strict lambda-time subset (no `time`, no `random`, no I/O) — and `resolve.AllowLambda = true` is set explicitly before any parse                                                                              | ✓ VERIFIED | `pkg/parser/resolve_setup.go:18` sets `resolve.AllowLambda = true` in package init; `TestResolveAllowLambdaIsSet` confirms. `TestParseAndLambdaGlobalsAreDistinct` asserts disjoint membership: 6 DSL primitives in `parseTimeGlobals` (not in `lambdaTimeGlobals`), and the 20 D-20 keys in `lambdaTimeGlobals` (not in `parseTimeGlobals`). `TestLambdaTimeGlobalsLocked` is the API-stability gate (exact 20-key set); `TestLambdaTimeGlobals_ForbiddenAbsent` (13 sub-tests for `print`, `set`, `time`, `random`, `getattr`, `load`, `os`, `open`, `read`, `write`, `now`, `uuid`, `http`). |

**Score:** 5/5 success criteria verified

### Required Artifacts

| Artifact                          | Expected                                                                                          | Status      | Details                                                                                                       |
| --------------------------------- | ------------------------------------------------------------------------------------------------- | ----------- | ------------------------------------------------------------------------------------------------------------- |
| `go.mod`                          | module `github.com/mikelalcon/skytime`, `go 1.25.x`, pinned `go.starlark.net` + `testify`         | ✓ VERIFIED  | go.mod line 1 `module github.com/mikelalcon/skytime`; line 3 `go 1.25.0`; testify v1.11.1; starlark v0.0.0-20260326113308-fadfc96def35 |
| `pkg/dag/errors.go`               | `*ParseError` + `*ValidationError` with `Position()`/`Error()`/`Unwrap()`                         | ✓ VERIFIED  | Both types present; 10 tests green; format `<file>:<line>:<col>: <msg>` confirmed                              |
| `pkg/dag/node.go`                 | sealed `Node` interface (`Kind`, `Position`, `nodeMarker`)                                        | ✓ VERIFIED  | `var _ Node = (*Flow)(nil)` etc. compile-time assertions; 6 concrete types satisfy it                         |
| `pkg/dag/{flow,step,control}.go`  | 6 node types: Flow, Step, IfCond, Script, ForEachParallel, CallFlow                                | ✓ VERIFIED  | All six exist with proper Pos field; `TestNode_KindAndPosition` table-driven across all 6                     |
| `pkg/dag/action.go`               | ActionRef as `starlark.Value` with recursive Freeze cascade                                       | ✓ VERIFIED  | `var _ starlark.Value = (*ActionRef)(nil)`; `TestActionRef_FreezeCascade` asserts dict + nested list both reject mutation post-Freeze; `TestActionRef_FreezeIdempotent`, `TestActionRef_FreezeNilKwargsDoesNotPanic` |
| `pkg/dag/lambda.go`               | `CapturedLambda` + `ComputeLambdaID(fileBytes, pos)` per D-18 (`sha256[:8]:line:col`)             | ✓ VERIFIED  | Format regex `^[a-f0-9]{8}:\d+:\d+$` verified; deterministic, content-sensitive, position-sensitive tests pass |
| `pkg/dag/retry.go`                | RetryPolicy + Timeout implementing `starlark.Unpacker` (DSL-08)                                   | ✓ VERIFIED  | `var _ starlark.Unpacker = (*RetryPolicy)(nil)` + same for Timeout; 14 tests across the two types             |
| `pkg/dag/marshal.go`              | Stable JSON marshal with `kind` discriminator                                                     | ✓ VERIFIED  | `TestMarshal_Stable_FlowWithBody`, `TestMarshal_Stable_ActionRefWithKwargs`; per-node `kind` discriminator tests for all 6 |
| `pkg/extension/extension.go`      | `Extension` interface (Name, Initialize, Operations)                                              | ✓ VERIFIED  | Interface compiles; `TestExtension_*` covers all three methods                                                |
| `pkg/extension/operation.go`      | `OperationFunc` (`context.Context`, NEVER `workflow.Context`); `Idempotent *bool` (D-12)          | ✓ VERIFIED  | `OperationFunc` takes stdlib `context.Context`; `Idempotent *bool` enforced; `Ptr[T]` helper                  |
| `pkg/extension/credential.go`     | Sealed `Credential` + 3 kinds with redacted `String()`                                            | ✓ VERIFIED  | `<credential:bearer:admin>` format verified; `isCredential()` unexported method seals it                      |
| `pkg/extension/handler.go`        | `CredentialHandler` interface                                                                     | ✓ VERIFIED  | `Resolve(ctx, id) (Credential, error)` declared; `TestCredentialHandler_*` cover happy/error paths            |
| `pkg/extension/registry.go`       | Per-parser Registry; `ErrIdempotentRequired` sentinel                                             | ✓ VERIFIED  | `TestRegistration_RequiresIdempotent` asserts via `errors.Is`; concurrent Register/Get under `-race`           |
| `pkg/extension/schema.go`         | `ParseSchema` + `UnpackOperationKwargs` (reflection on `star:"name,required"` tags)               | ✓ VERIFIED  | 17 tests covering schema parsing, kwarg validation, type assignment, *dag.ValidationError construction        |
| `pkg/bridge/struct.go`            | `ToStarlarkStruct` recursive, sorted keys                                                         | ✓ VERIFIED  | `sort.Strings(keys)` present; 10 tests including `TestToStarlarkStruct_Deterministic`                          |
| `pkg/bridge/lambda_globals.go`    | D-20 locked 20-key subset (no print/set/time/random/I/O)                                          | ✓ VERIFIED  | `TestLambdaTimeGlobalsLocked` API-stability gate; locally-implemented `sumBuiltin` (Universe lacks `sum`)     |
| `pkg/bridge/lambda_call.go`       | Fresh `*starlark.Thread` per call; MaxExecutionSteps=10_000_000 default; Print hook                | ✓ VERIFIED  | `&starlark.Thread{` allocated inside CallLambda body; `SetMaxExecutionSteps` sets opts/default; `thread.Print` routes via `opts.PrintSink`. `TestCallLambda_ConcurrentSafety` (50 parallel under -race) |
| `pkg/parser/parser.go`            | `Parser`, `NewParser(opts ...Option)`, `ParseFile`, `ParseSource`; panic-guarded                  | ✓ VERIFIED  | `TestParse_NeverPanicsOnGarbage` exercises panic guard with 5 inputs; `recover` wraps in `*dag.ParseError`     |
| `pkg/parser/options.go`           | `WithRoot`, `WithExtensions`, `WithMaxExecutionSteps` functional options                          | ✓ VERIFIED  | Option functions present; 4 tests including FailFast on D-12 violation                                         |
| `pkg/parser/resolve_setup.go`     | `init()` sets `resolve.AllowLambda = true`                                                        | ✓ VERIFIED  | line 18; `TestResolveAllowLambdaIsSet` asserts                                                                 |
| `pkg/parser/builtins.go`          | 6 naked builtins (`flow`, `step`, `if_cond`, `script`, `for_each_parallel`, `call_flow`)          | ✓ VERIFIED  | `globals.go` registers all 6 via `starlark.NewBuiltin(...)`; `TestParseTimeGlobals_NakedPrimitives` confirms naked (no namespace prefix) |
| `pkg/parser/load.go`              | Sandboxed load — relative + absolute + `.git` ancestor; traversal rejection (D-13/14/17)          | ✓ VERIFIED  | `filepath.Rel(rootAbs, abs)` + `strings.HasPrefix(rel, "..")` rejection (line 168-169); `TestLoad_*` (8 tests) |
| `pkg/parser/lambda_capture.go`    | Captures `*starlark.Function` with stable D-18 ID + Position                                      | ✓ VERIFIED  | line 40 calls `dag.ComputeLambdaID(fileBytes, pos)`; line 47 constructs `*dag.CapturedLambda`                 |
| `pkg/parser/finalize.go`          | Cross-flow `call_flow` resolution after exec (D-15/D-16)                                          | ✓ VERIFIED  | `resolveCallFlows` walks `Flow.Body` / `IfCond.Then-Else` / `ForEachParallel.Steps`; 3 nested resolution tests |
| `tests/fixtures/valid/`           | 7 valid fixtures + 2 helper files + 2 golden JSON                                                  | ✓ VERIFIED  | 9 .star files + 2 golden JSON (real parser output, no `_note` placeholders)                                    |
| `tests/fixtures/invalid/`         | 8 invalid fixtures, each with `# expects:` header                                                  | ✓ VERIFIED  | All 8 present; each has `# expects:` line 1; `TestInvalidFixtures` runs all 8, all green                       |

**Artifact pass rate:** 26/26 (100%)

### Key Link Verification

| From                                    | To                                          | Via                                       | Status     | Details                                                                                                                  |
| --------------------------------------- | ------------------------------------------- | ----------------------------------------- | ---------- | ------------------------------------------------------------------------------------------------------------------------ |
| `pkg/dag/action.go`                     | `starlark.Value` interface                  | `var _ starlark.Value = (*ActionRef)(nil)` | ✓ WIRED    | Compile-time assertion present at file scope                                                                              |
| `pkg/dag/action.go ActionRef.Freeze()`  | `Kwargs.Freeze()` (cascade)                 | recursive freeze cascade                   | ✓ WIRED    | `Kwargs.Freeze()` call confirmed; `TestActionRef_FreezeCascade` asserts both dict and nested list reject mutation         |
| `pkg/dag/retry.go RetryPolicy.Unpack`   | `starlark.Unpacker` interface               | interface implementation                   | ✓ WIRED    | `var _ starlark.Unpacker = (*RetryPolicy)(nil)` + `(*Timeout)(nil)`                                                       |
| `pkg/extension/registry.go Register()`  | `Idempotent != nil` check                   | sentinel error wrap                        | ✓ WIRED    | `TestRegistration_RequiresIdempotent` uses `errors.Is(err, ErrIdempotentRequired)`                                        |
| `pkg/extension/credential.go`           | redacted output `<credential:bearer:...>`   | format string                              | ✓ WIRED    | `TestBearerCredential_RedactedString` asserts exact format `<credential:bearer:admin>`                                    |
| `pkg/bridge/struct.go ToStarlarkStruct` | `sort.Strings(keys)` before iterating       | deterministic iteration                    | ✓ WIRED    | `sort.Strings` confirmed in code; `TestToStarlarkStruct_Deterministic` + `TestToStarlarkStruct_LargeMap` (100 keys)       |
| `pkg/bridge/lambda_call.go CallLambda`  | fresh `&starlark.Thread{}` per invocation   | Pitfall #1                                 | ✓ WIRED    | Thread allocated inside function body; `TestCallLambda_FreshThread`, `TestCallLambda_ConcurrentSafety` (50 parallel)      |
| `pkg/bridge/lambda_call.go CallLambda`  | `thread.SetMaxExecutionSteps(10_000_000)`   | D-22 default                                | ✓ WIRED    | `DefaultMaxExecutionSteps = 10_000_000` constant; default applied when opts.MaxExecutionSteps == 0                        |
| `pkg/parser/builtins.go`                | `&dag.{Flow,Step,IfCond,Script,ForEachParallel,CallFlow}{}` | DSL builtins return dag nodes              | ✓ WIRED    | All 6 builtins construct dag types; `TestParseFlow_DSL01` etc. assert correct dag.Flow shape                              |
| `pkg/parser/lambda_capture.go`          | `dag.ComputeLambdaID(fileBytes, pos)`        | D-18 ID format                             | ✓ WIRED    | Direct call confirmed; `TestLambdaCapture_StableID` covers determinism + content sensitivity + position match             |
| `pkg/parser/load.go`                    | `filepath.Rel` sandbox check                 | `..`-prefix rejection                      | ✓ WIRED    | `filepath.Rel(rootAbs, abs)` + `strings.HasPrefix(rel, "..")` confirmed; `TestLoad_TraversalRejected` green                |
| `pkg/parser/resolve_setup.go`           | `resolve.AllowLambda = true`                 | `init()`                                   | ✓ WIRED    | line 18 in init(); `TestResolveAllowLambdaIsSet` reads the variable post-init                                              |
| `pkg/parser/parser.go`                  | `starlark.ExecFileOptions`                   | explicit options-based exec                | ✓ WIRED    | `defaultFileOptions` invoked; `TestParse_UsesExecFileOptions` exercises the FileOptions semantics (e.g. `While: false`)    |

**Key links:** 13/13 wired

### Data-Flow Trace (Level 4)

| Artifact                      | Data Variable     | Source                                    | Produces Real Data                  | Status     |
| ----------------------------- | ----------------- | ----------------------------------------- | ----------------------------------- | ---------- |
| `pkg/parser/parser.go`        | `flows`           | `starlark.ExecFileOptions` over `.star`   | Real `*dag.Flow` map per fixture    | ✓ FLOWING  |
| `pkg/parser/builtins.go`      | `*dag.Flow.Body`  | DSL builtins constructed at call sites    | Real `[]dag.Node` (Step/IfCond/...) | ✓ FLOWING  |
| `pkg/parser/lambda_capture.go`| `p.lambdas`       | builtins capture lambdas via thread       | Real `*starlark.Function` + ID/Pos  | ✓ FLOWING  |
| `pkg/parser/finalize.go`      | `CallFlow.Resolved` | `resolveCallFlows` recursive walk       | Real cross-flow pointer or `*dag.ParseError` | ✓ FLOWING |
| `pkg/bridge/lambda_call.go`   | thread + state    | `ToStarlarkStruct(state)` + fresh Thread  | Real `*starlarkstruct.Struct`       | ✓ FLOWING  |
| Goldens for fixture 02        | dag.Flow JSON     | `parser.ParseFile()` → `MarshalJSON`      | Real lambda IDs (`3b92ab29:19:20`)  | ✓ FLOWING  |

No HOLLOW or DISCONNECTED artifacts found.

### Behavioral Spot-Checks

| Behavior                                     | Command                                                                              | Result                                              | Status   |
| -------------------------------------------- | ------------------------------------------------------------------------------------ | --------------------------------------------------- | -------- |
| Compile across all packages                   | `go build ./...`                                                                     | exit 0                                              | ✓ PASS   |
| Vet clean                                     | `go vet ./...`                                                                       | exit 0 (no warnings)                                | ✓ PASS   |
| Full suite passes with race detector          | `go test ./... -race -count=1`                                                       | exit 0; pkg/bridge ok 1.882s, pkg/dag ok 1.420s, pkg/extension ok 1.628s, pkg/parser ok 2.085s | ✓ PASS |
| Total test count                              | `go test ./... -v -count=1 \| grep -E "^--- PASS" \| wc -l`                          | 218 passing tests, 0 failures                       | ✓ PASS   |
| Temporal firewall (no actual imports)         | `grep -rE '^\s*"go\.temporal\.io\|^\s*go\.temporal\.io' pkg/`                         | 0 matches (only firewall-enforcing test code)       | ✓ PASS   |
| Valid fixtures parse cleanly                   | `TestValidFixtures` (7 sub-tests for 01..07)                                          | All 7 PASS                                          | ✓ PASS   |
| Invalid fixtures produce expected errors       | `TestInvalidFixtures` (8 sub-tests for 01..08)                                        | All 8 PASS, each matches its `# expects:` substring | ✓ PASS   |
| Concurrent CallLambda (Pitfall #1 closure)    | `TestCallLambda_ConcurrentSafety` (50 parallel under -race)                          | PASS                                                | ✓ PASS   |
| ToStarlarkStruct iter-determinism (DSL-09 SC4) | `TestToStarlarkStruct_Deterministic` + `TestToStarlarkStruct_LargeMap` (100 keys)    | Both PASS                                           | ✓ PASS   |

### Requirements Coverage (22 of 22 mapped)

| Requirement | Source Plan(s) | Description (from REQUIREMENTS.md)                                                                                            | Status      | Evidence                                                                                                            |
| ----------- | -------------- | ----------------------------------------------------------------------------------------------------------------------------- | ----------- | ------------------------------------------------------------------------------------------------------------------- |
| DSL-01      | 01-05          | `flow(name=..., inputs=..., steps=[...])` produces `*dag.Flow`                                                                | ✓ SATISFIED | `TestParseFlow_DSL01` + `TestValidFixtures/01-minimal-flow.star`                                                    |
| DSL-02      | 01-05          | `step(action=...)` produces `*dag.Step` with one ActionRef                                                                    | ✓ SATISFIED | `TestStep_SingleAction`                                                                                              |
| DSL-03      | 01-05          | `step(block=[a,b,c])` produces `*dag.Step` with multiple ActionRefs                                                           | ✓ SATISFIED | `TestStep_Block`                                                                                                     |
| DSL-04      | 01-05          | `if_cond(cond=lambda, then=[...], else_=[...])` with `CapturedLambda`                                                          | ✓ SATISFIED | `TestIfCond_LambdaCapture`                                                                                           |
| DSL-05      | 01-05          | `script(id=..., fn=lambda, output_alias=...)` with `CapturedLambda`                                                            | ✓ SATISFIED | `TestScript_LambdaCapture`                                                                                           |
| DSL-06      | 01-05          | `for_each_parallel(items=..., item=..., steps=[...])` accepts list and lambda producer                                         | ✓ SATISFIED | `TestForEachParallel_BothItemForms_List` + `TestForEachParallel_BothItemForms_Lambda`                               |
| DSL-07      | 01-05          | `call_flow(name=..., inputs=..., child_options=...)` resolves at parse time                                                    | ✓ SATISFIED | `TestCallFlow_NameResolution` + `TestResolveCallFlows_Found/NotFound`                                                |
| DSL-08      | 01-02          | `step(retry=..., timeout=...)` carries Temporal kwargs as pure data                                                            | ✓ SATISFIED | `TestRetryPolicy_Unpack_*` (8 tests) + `TestTimeout_Unpack_*` (6 tests) + `TestRetryPolicy_Through_Step`              |
| DSL-09      | 01-04          | `bridge.ToStarlarkStruct` deterministic key-order; dot-notation                                                                | ✓ SATISFIED | `TestToStarlarkStruct_Deterministic` + `TestToStarlarkStruct_DotAccess_DSL09` + `TestCallLambda_DotAccess_DSL09`     |
| DSL-10      | 01-01, 01-05   | `resolve.AllowLambda = true` set explicitly before any parse                                                                  | ✓ SATISFIED | `pkg/parser/resolve_setup.go:18` + `TestResolveAllowLambdaIsSet`                                                     |
| EXT-01      | 01-03          | `Extension` interface: `Name()`, `Initialize`, `Operations()`                                                                 | ✓ SATISFIED | `TestExtension_FakeImplementorExposesName` + `TestExtension_InitializeReturnsStarlarkValue`                          |
| EXT-02      | 01-03, 01-05   | Extension factory returns `ActionRef` intent (never registers Temporal activity)                                              | ✓ SATISFIED | `TestExtensionFactory_ReturnsActionRef` + `TestExtensionEndpointFactory_PropagatesCredentialID` (D-08 end-to-end)    |
| EXT-03      | 01-03          | `OperationFunc` uses `context.Context` (stdlib), never `workflow.Context`                                                     | ✓ SATISFIED | `TestOperationFunc_SignatureCompiles` + `TestNoTemporalImportsInExtensionPackage` (AST walk)                         |
| EXT-04      | 01-03          | Each extension declares per-operation `Idempotent bool`                                                                       | ✓ SATISFIED | `TestRegistration_RequiresIdempotent` (`errors.Is(err, ErrIdempotentRequired)`) + `TestNewParser_PropagatesRegistrationError` |
| EXT-05      | 01-03          | `Credential` typed value with redacted `String()` — workflow state only stores credential string ID                            | ✓ SATISFIED | `TestBearerCredential_RedactedString` (`<credential:bearer:admin>`) + `TestBasicCredential_RedactedString` + `TestAPIKeyCredential_RedactedString` + `TestCredential_SealedViaIsCredential` |
| EXT-06      | 01-03, 01-05   | Static or dynamic-local Go extension registration                                                                              | ✓ SATISFIED | `TestRegistration_StaticAndDynamic` + `TestRegister_InvalidatesGlobalsCache`                                         |
| PARSE-01    | 01-05          | All 6 DSL primitives are naked `*starlark.Builtin` globals (not namespaced)                                                    | ✓ SATISFIED | `TestParseTimeGlobals_NakedPrimitives`; `globals.go` binds each as a top-level key                                  |
| PARSE-02    | 01-05          | `load()` sandboxed to root; rejects `../`-traversal                                                                            | ✓ SATISFIED | `TestLoad_Relative` + `TestLoad_Absolute` + `TestLoad_GitAncestor` + `TestLoad_TraversalRejected` + `TestLoad_Cache` |
| PARSE-03    | 01-04, 01-05   | Parse-time and lambda-time globals are distinct                                                                                | ✓ SATISFIED | `TestParseAndLambdaGlobalsAreDistinct` + `TestLambdaTimeGlobalsLocked` + `TestLambdaTimeGlobals_ForbiddenAbsent` (13 keys) |
| PARSE-04    | 01-02, 01-05   | Lambda capture stores `*starlark.Function` with stable ID + `syntax.Position`                                                  | ✓ SATISFIED | `TestLambdaCapture_StableID` + `TestLambdaCapture_ContentSensitive` + `TestLambdaCapture_PositionMatchesDef`         |
| PARSE-05    | 01-01, 01-05   | Malformed file produces `*ParseError` `<file>:<line>:<col>: <msg>`; never panics                                              | ✓ SATISFIED | `TestParse_NeverPanicsOnGarbage` (5 sub-tests) + `TestParse_ErrorFormat` + `TestInvalidFixtures` (8 sub-tests)        |
| PARSE-06    | 01-04          | Bridge's `CallLambda` uses fresh thread, `MaxExecutionSteps`, Print hook                                                       | ✓ SATISFIED | `TestCallLambda_FreshThread` + `TestCallLambda_PrintHookRouted` + `TestCallLambda_DefaultMaxExecutionStepsConstant` + `TestCallLambda_DefaultPrintRoutesToSlog` |

**Coverage:** 22/22 (100%) — every requirement in REQUIREMENTS.md mapped to Phase 1 has at least one passing automated test. No orphaned requirements.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |

No blocker anti-patterns found.

**Documented stubs (intentional, deferred to later phases):**
- `pkg/dag/input.go` — `WorkflowInput.MarshalJSON` omits `*starlark.Function` values; sorted lambda IDs surface only. **TODO(phase3): replace with real lambda serialization.** Phase 3 entry-gate decision (custom `DataConverter` vs. re-parse-on-start). Not a Phase 1 blocker.
- `pkg/dag/marshal.go` — `starlarkValueToGo` covers only String/Bool/Int/Float (Phase 1 fixtures use only these). Nested Dict/List values fall through to `v.String()`. Phase 3/Phase 6 will extend when needed.
- `pkg/parser/finalize.go` — `validateActionRefKwargs` is a Phase-1 no-op (extension factories validate at construction time inside their Builtins). The hook exists for Phase 4's static validator.

These are explicitly noted in 01-04-SUMMARY.md / 01-05-SUMMARY.md as intentional with clear resolution paths in later phases. They do not block Phase 1's goal (lock the type spine, parse `.star` to `dag.Flow`, no Temporal involvement).

### Human Verification Required

None for Phase 1. Every requirement is exercise-able via `go test`. Determinism replay (Pitfall #3) and Temporal dev-server integration are Phase 3 concerns; Phase 1's verifications are entirely in-process Go.

### Gaps Summary

No gaps found. Phase 1 delivered:

- **Type contract (`pkg/dag`):** 6 sealed Node types + ActionRef (custom `starlark.Value` with recursive Freeze cascade) + CapturedLambda + RetryPolicy/Timeout (`starlark.Unpacker`) + ComputeLambdaID (D-18) + stable `kind`-discriminator JSON marshal.
- **Extension SDK (`pkg/extension`):** Sealed Credential interface with 3 redacted kinds, `OperationFunc` taking stdlib `context.Context`, per-parser Registry with `ErrIdempotentRequired` enforcement, reflection-based kwarg validator returning `*dag.ValidationError`.
- **State/lambda bridge (`pkg/bridge`):** Deterministic `ToStarlarkStruct` (sort.Strings before iteration), D-20 locked 20-key `lambdaTimeGlobals` (with locally-implemented `sumBuiltin`), `CallLambda` allocating a fresh `*starlark.Thread` per invocation with `MaxExecutionSteps=10_000_000` default and configurable `PrintSink`.
- **Parser (`pkg/parser`):** All 6 DSL primitives as naked builtins, `resolve.AllowLambda = true` in init, sandboxed `load()` (relative + absolute + `.git`-ancestor + traversal rejection), lambda capture with content-hash IDs and free-var validation per D-19, multi-flow per file with cross-flow `call_flow` resolution, position-aware `*dag.ParseError` on every malformed input.
- **Test corpus:** 7 valid fixtures parse to expected `*dag.Flow`; 8 invalid fixtures produce errors matching their `# expects:` substring; 2 goldens regenerated to real parser output.
- **Quality gates:** 218 tests pass under `-race -count=1`; `go vet` clean; whole-repo `go build` and `go test` green; the architectural firewall (`grep -rE '^\s*"go\.temporal\.io|^\s*go\.temporal\.io' pkg/`) returns 0 matches.

All 22 v1 requirements assigned to this phase have ≥ 1 passing automated test, and the goal-backward truths (the 5 success criteria from ROADMAP.md) all verify. Phase 2 (generic activity), Phase 3 (interpreter), Phase 4 (CLI / static validator), and Phase 6 (example extensions) are unblocked at the contract level.

---

_Verified: 2026-04-26_
_Verifier: Claude (gsd-verifier)_
