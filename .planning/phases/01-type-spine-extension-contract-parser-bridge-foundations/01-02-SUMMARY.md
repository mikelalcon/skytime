---
phase: 01-type-spine-extension-contract-parser-bridge-foundations
plan: 02
subsystem: pkg/dag
tags: [go, go.starlark.net, syntax.Position, sealed-interface, freeze-cascade, json-marshal, dsl-08, parse-04]

requires:
  - 01-01 (typed error spine, fixture corpus, package skeletons)

provides:
  - Sealed Node interface (Kind/Position/nodeMarker) — pkg/dag/node.go
  - Six concrete node types: Flow, Step, IfCond, Script, ForEachParallel, CallFlow — pkg/dag/{flow,step,control}.go
  - WorkflowInput JSON-marshalable carrier (Lambdas excluded; Phase 3 owns *starlark.Function serialization) — pkg/dag/input.go
  - ActionRef as a custom starlark.Value with recursive Freeze cascade (Pitfall #6 closed) — pkg/dag/action.go
  - CapturedLambda holding *starlark.Function + D-18 ID + Position + frozen FreeVars — pkg/dag/lambda.go
  - ComputeLambdaID(fileBytes, pos) — D-18 format `sha256(fileBytes)[:4-hex]:line:col` (8-char prefix)
  - RetryPolicy and Timeout (DSL-08) implementing starlark.Unpacker — pkg/dag/retry.go
  - ForEachParallel.Validate() — exactly-one-of (ItemsLambdaID xor ItemsLiteral)
  - Stable JSON marshal with `kind` discriminator for all 6 nodes + ActionRef — pkg/dag/marshal.go

affects:
  - 01-03 (pkg/extension): consumes ActionRef return type from extension factories; uses CredentialID embedding semantics
  - 01-04 (pkg/parser, pkg/bridge): constructs all 6 node types from Starlark; uses ComputeLambdaID; reads CapturedLambda
  - 01-05 (parser integration): regenerates 02-all-primitives.golden.json against the locked JSON shape

tech-stack:
  added:
    - "go.starlark.net/starlark (custom Value implementation, Unpacker pattern)"
    - "go.starlark.net/syntax (syntax.Position carried on every node)"
    - "crypto/sha256 + encoding/hex (D-18 lambda ID hashing)"
  patterns:
    - "Sealed interface via unexported nodeMarker() — Node cannot be implemented outside pkg/dag"
    - "Compile-time interface assertions at file scope (var _ Node = (*Flow)(nil), var _ starlark.Value = (*ActionRef)(nil), var _ starlark.Unpacker = ...)"
    - "Recursive Freeze cascade: Freeze() sets local frozen flag (idempotent), then calls Freeze() on contained Starlark values"
    - "Marshal-time alias structs (flowJSON, stepJSON, etc.) — flat shape with `kind` first; deliberately excludes Pos to keep golden output cross-machine stable"
    - "ActionRef.Kwargs *starlark.Dict converted to map[string]any at marshal time — encoder sorts keys deterministically (Pitfall #3)"
    - "starlark.Unpacker (not UnpackArgs positional pairs) for DSL-08 RetryPolicy/Timeout — they decode from a single dict kwarg"

key-files:
  created:
    - "pkg/dag/node.go"
    - "pkg/dag/node_test.go"
    - "pkg/dag/flow.go"
    - "pkg/dag/flow_test.go"
    - "pkg/dag/step.go"
    - "pkg/dag/step_test.go"
    - "pkg/dag/control.go"
    - "pkg/dag/control_test.go"
    - "pkg/dag/input.go"
    - "pkg/dag/input_test.go"
    - "pkg/dag/retry.go"
    - "pkg/dag/retry_test.go"
    - "pkg/dag/action.go"
    - "pkg/dag/action_test.go"
    - "pkg/dag/lambda.go"
    - "pkg/dag/lambda_test.go"
    - "pkg/dag/marshal.go"
    - "pkg/dag/marshal_test.go"
  modified:
    - "go.mod (golang.org/x/sys + google.golang.org/protobuf added as transitive indirects after `go test` resolved Starlark deps)"
    - "go.sum (matching transitive checksums)"

key-decisions:
  - "Distinct types satisfying a sealed Node interface (NOT a sum-type Kind field on a base struct) — produces cleanest Phase 3 type switch and eliminates a category of bugs (Step constructed with IfCond fields populated). RESEARCH.md confirmed this design."
  - "Node interface seal via unexported nodeMarker() method — external packages literally cannot implement Node, preventing accidental widening of the type set."
  - "ActionRef.Kind_ field uses trailing underscore to avoid collision with the Node.Kind() method (ActionRef is intentionally NOT a Node — it lives inside Step.Actions). Public access is through ActionKind()."
  - "Pos field excluded from JSON marshaling for ALL types — syntax.Position.Filename is machine-absolute and breaks cross-machine golden stability. Position is a parse-time concern (D-04 errors), not a wire-format concern."
  - "WorkflowInput.MarshalJSON omits *starlark.Function values inside Lambdas (in-memory only in Phase 1). LambdaIDs surface as a sorted slice — Phase 3 picks the real serialization strategy (custom DataConverter vs. re-parse-on-start) and replaces this stub."
  - "Empty slices render as `[]` not `null` in marshal output (`Body`, `Actions`, `Then`, `Else`, `Steps`) — keeps plan 05's golden shapes stable regardless of whether the parser populates an empty slice or leaves the field nil."
  - "RetryPolicy backoff_coefficient accepts both Starlark Int and Float — common authoring convenience (`backoff_coefficient=2` is a valid literal)."

patterns-established:
  - "Stub-then-fill approach for cross-task type dependencies inside a single package: Task 1 created placeholder ActionRef/RetryPolicy/Timeout/CapturedLambda types so Step/ForEachParallel/WorkflowInput could compile and have their tests run independently; Tasks 2-3 replaced the stubs with full implementations."
  - "Each task committed atomically with a single feat/test commit; tests pass before each commit (TDD discipline preserved at the workflow level)."

requirements-completed: [DSL-08, PARSE-04]

duration: 7m
completed: 2026-04-27
---

# Phase 01 Plan 02: pkg/dag Type Spine Summary

**Sealed Node interface with six concrete types, ActionRef as a custom starlark.Value with recursive Freeze cascade, RetryPolicy/Timeout decoded via starlark.Unpacker, content-addressable CapturedLambda IDs, and stable kind-discriminated JSON marshaling — locking the wire format every later phase imports.**

## Performance

- **Duration:** ~7 minutes
- **Started:** 2026-04-27T16:18:40Z
- **Completed:** 2026-04-27T16:25:52Z
- **Tasks:** 4 (all completed atomically)
- **Files created:** 18 Go files (9 implementation + 9 test)
- **Tests:** 53 new tests in pkg/dag (63 total in package incl. plan 01-01 errors)

## Final Type Signatures

| Type              | File          | Provides                                                                            |
| ----------------- | ------------- | ----------------------------------------------------------------------------------- |
| `Node`            | `node.go`     | Sealed interface: `Kind() string`, `Position() syntax.Position`, `nodeMarker()`     |
| `Flow`            | `flow.go`     | `{Pos, Name, Inputs map[string]string, Body []Node}` — top-level workflow          |
| `Step`            | `step.go`     | `{Pos, Actions []*ActionRef, Retry *RetryPolicy, Timeout *Timeout}`                |
| `IfCond`          | `control.go`  | `{Pos, LambdaID, Then []Node, Else []Node}` — conditional branch                   |
| `Script`          | `control.go`  | `{Pos, ID, LambdaID, OutputAlias}` — state-mutation node                           |
| `ForEachParallel` | `control.go`  | `{Pos, ItemsLambdaID, ItemsLiteral, ItemVar, Steps, Retry, Timeout}` + `Validate()` |
| `CallFlow`        | `control.go`  | `{Pos, Name, Inputs, ChildOptions, Resolved (json:-)}` — child-workflow invoke    |
| `ActionRef`       | `action.go`   | `starlark.Value` with `String/Type/Truth/Hash/Freeze`; recursive Freeze cascade    |
| `CapturedLambda`  | `lambda.go`   | `{ID, Fn *starlark.Function, Pos, FreeVars starlark.StringDict}`                   |
| `RetryPolicy`     | `retry.go`    | `starlark.Unpacker` decoding `{initial_interval, backoff_coefficient, max_attempts, non_retryable_errors}` |
| `Timeout`         | `retry.go`    | `starlark.Unpacker` decoding `{start_to_close, schedule_to_start}`                |
| `WorkflowInput`   | `input.go`    | `{Flow, Lambdas (json:-), InitState}` + custom MarshalJSON emitting sorted lambda IDs |

## Discretion Decision Rationale

**Distinct types implementing a sealed Node interface (NOT a sum-type Kind field on a base struct).**

CONTEXT.md left this choice open. RESEARCH.md and plan 02 settled on the interface-with-distinct-types approach because:

1. **Cleaner Phase 3 interpreter switch** — Phase 3's interpreter walks the body with `switch n := node.(type)`, getting compile-time exhaustiveness against the six distinct types. A sum-type `Kind` field forces a string-based switch with a default panic branch.
2. **Eliminates malformed-node bugs** — A `Step` value cannot accidentally have `IfCond.Then` populated; the type system enforces that each node's fields are its own.
3. **Sealed via unexported `nodeMarker()`** — External packages literally cannot implement Node, preventing accidental widening of the type set without a deliberate change to pkg/dag.
4. **Same approach as `temporalio/samples-go/dsl`** — Verified prior art for Starlark-as-DSL on Temporal.

The cost is six near-identical `Kind()`/`Position()`/`nodeMarker()` triples — accepted because each is two lines of boilerplate and the type-system safety pays back over six phases of consumers.

## ActionRef Freeze Cascade Verification

Pitfall #6 (freeze audit) is closed for ActionRef. The contract is:

1. `Freeze()` sets the local `frozen` flag and is idempotent (calling twice is a no-op — second call returns early).
2. If `Kwargs != nil`, `Freeze()` calls `a.Kwargs.Freeze()`.
3. `*starlark.Dict.Freeze()` cascades to its contained values per the Starlark spec — so a `*starlark.List` stored as a dict value also becomes frozen.

Tests in `pkg/dag/action_test.go`:

- **`TestActionRef_FreezeCascade`** — constructs an ActionRef with a `*Dict` containing a `*List`. After `Freeze()`, both the dict and the inner list reject mutation (`SetKey` and `Append` return `"frozen"` errors).
- **`TestActionRef_FreezeIdempotent`** — calls `Freeze()` twice, no panic, dict remains frozen.
- **`TestActionRef_FreezeNilKwargsDoesNotPanic`** — defensive check for the construction path where Kwargs is nil.

This closes the recursive-freeze surface for ActionRef. The same discipline must be applied if any future custom `starlark.Value` is added to pkg/dag — there is currently no automated audit beyond per-type tests.

## ComputeLambdaID Format (D-18 Reference)

```
ID = hex(sha256(fileBytes)[:4]) + ":" + line + ":" + col
   = ^[a-f0-9]{8}:\d+:\d+$
```

Examples:

```
deadbeef:5:10
1234abcd:42:7
```

Properties (verified by `pkg/dag/lambda_test.go`):

- **Deterministic:** same `fileBytes + pos` → same ID across calls.
- **Content-sensitive:** any byte change in `fileBytes` flips the prefix (cosmetic edits to whitespace/comments DO change the ID — intentional per D-18, lets Phase 3's re-parse-on-start verify file content matches without canonicalizing the AST).
- **Position-sensitive:** same content + different position (line/col) produces different IDs.

Phase 3 will key its lambda serialization on this format. The exact serialization mechanism (custom DataConverter vs. re-parse-on-start) is deferred to the Phase 3 entry-gate decision, but both options compose with the content-hash prefix.

## WorkflowInput JSON Marshal Stub

`pkg/dag/input.go` defines `WorkflowInput.MarshalJSON` that:

- Emits `flow` and `init_state` via the standard encoder.
- Replaces the `Lambdas map[string]*CapturedLambda` field with a sorted `lambda_ids []string` slice (Pitfall #3 — deterministic key order).
- **Excludes `*starlark.Function` values entirely** — they are not JSON-serializable.

```go
// TODO(phase3): replace with real lambda serialization (custom DataConverter
// or re-parse-on-start). See Phase 3 entry-gate decision.
```

This stub lets plan 04 (bridge) and plan 05 (parser) test the wire shape today without committing to a serialization strategy. Phase 3 owns the replacement.

## Task Commits

Each task was committed atomically (4 commits, all using `--no-verify` per parallel-execution mode):

1. **Task 1: Node interface + 6 pure-data node types** — `d99367c` (feat)
2. **Task 2: RetryPolicy + Timeout (DSL-08) with starlark.Unpacker** — `626ab58` (feat)
3. **Task 3: ActionRef (starlark.Value w/ recursive Freeze) + CapturedLambda** — `7e7c0a9` (feat)
4. **Task 4: stable JSON marshaling with kind discriminator** — `32de22c` (feat)

The Task 1 commit also created stub `ActionRef`/`RetryPolicy`/`Timeout`/`CapturedLambda` placeholders so the package compiled before tasks 2-3 filled them in. Tasks 2 and 3 replaced those stubs in place. TDD discipline was preserved by writing tests alongside their implementations and confirming `go test ./pkg/dag/... -race -count=1` exits 0 before each commit.

## Files Created/Modified

**Implementation (9 files):**
- `pkg/dag/node.go` — sealed Node interface
- `pkg/dag/flow.go` — Flow type
- `pkg/dag/step.go` — Step type
- `pkg/dag/control.go` — IfCond, Script, ForEachParallel, CallFlow
- `pkg/dag/input.go` — WorkflowInput + custom MarshalJSON
- `pkg/dag/action.go` — ActionRef custom starlark.Value
- `pkg/dag/lambda.go` — CapturedLambda + ComputeLambdaID
- `pkg/dag/retry.go` — RetryPolicy + Timeout starlark.Unpacker
- `pkg/dag/marshal.go` — MarshalJSON for all 6 nodes + ActionRef

**Tests (9 files):**
- `pkg/dag/node_test.go` — 1 test (table-driven across 6 nodes), file-scope Node assertions
- `pkg/dag/flow_test.go` — 2 tests
- `pkg/dag/step_test.go` — 2 tests
- `pkg/dag/control_test.go` — 8 tests (IfCond, Script, ForEachParallel.Validate ×4, CallFlow ×2)
- `pkg/dag/input_test.go` — 2 tests (WorkflowInput.MarshalJSON)
- `pkg/dag/retry_test.go` — 14 tests (RetryPolicy ×8, Timeout ×6)
- `pkg/dag/action_test.go` — 8 tests (ActionRef Type/String/Truth/Hash/Freeze cascade/idempotent/nil-Kwargs/ActionKind)
- `pkg/dag/lambda_test.go` — 5 tests (regex, determinism, content-sensitive, position-sensitive, real-Starlark-snippet)
- `pkg/dag/marshal_test.go` — 11 tests (per-node + heterogeneous body + 2 stability)

**Module modifications (incidental):**
- `go.mod` — `golang.org/x/sys v0.42.0` added as indirect (transitive of go.starlark.net runtime path)
- `go.sum` — matching checksums for x/sys + google.golang.org/protobuf (transitive of testify or starlark)

These transitive deps appeared during the first `go test` run in the new package — they are pure indirects, not direct imports.

## Decisions Made

(Mirrored in `key-decisions:` frontmatter; expanded here.)

- **Sealed interface, distinct types** — see "Discretion Decision Rationale" above.
- **`Kind_` trailing-underscore field on ActionRef** — `ActionRef.Kind()` would conflict with `Node.Kind()` if ActionRef ever satisfied Node by accident. Trailing underscore makes the field unambiguous; `ActionKind()` is the public read accessor.
- **Pos excluded from all marshaled output** — syntax.Position.Filename is absolute (e.g., `/Users/mikel/.../tests/fixtures/valid/01-minimal-flow.star`); two machines produce different bytes for the same logical flow. Plan 05's golden tests need byte-stability across CI/laptop. If positional info is later required in golden output, add it as a relative-path-only field.
- **Empty slices render as `[]`** — Body, Actions, Then, Else, Steps. Avoids `null` vs `[]` ambiguity in golden tests.
- **RetryPolicy backoff_coefficient accepts both Int and Float** — `backoff_coefficient=2` should work as a Starlark literal (Starlark `2` is `Int`, not `Float`). The Unpacker promotes Int→Float64 for this single field.
- **WorkflowInput.MarshalJSON keeps Lambdas in-memory only** — Phase 3 owns serialization; Phase 1 emits sorted `lambda_ids` so the wire shape is determined and golden-stable, but the actual function values do not survive JSON.
- **`ForEachParallel.Validate()` lives in pkg/dag, not in the parser** — the exactly-one-of invariant is a property of the type. The parser will surface the error through `ValidationError`, but the check belongs with the type definition.

## Deviations from Plan

**None.** Plan 01-02 executed exactly as written.

The plan acknowledged ActionRef/RetryPolicy/Timeout/CapturedLambda as forward references inside Step/ForEachParallel/WorkflowInput; the stub-then-fill approach (Task 1 stubs, Tasks 2-3 fill) was the natural execution order and matches the plan's `<action>` notes ("the package-internal forward reference is fine").

## Issues Encountered

- **Parallel agent on plan 01-03** — both this executor and plan 01-03's executor were committing during the same wave. We touched disjoint package directories (`pkg/dag/` vs `pkg/extension/`) so no file conflicts. Both agents used `--no-verify` per the orchestrator instruction; pre-commit hooks will run once the wave completes.
- **`go.mod`/`go.sum` transitive growth** — `golang.org/x/sys` and `google.golang.org/protobuf` appeared as new indirect dependencies after the first `go test` run. These are transitives (likely from `go.starlark.net` or `testify` paths exercised by the new code). Committed alongside Task 1.

## User Setup Required

None — no external services, secrets, or manual steps.

## Known Stubs

**1. `WorkflowInput.MarshalJSON` lambda-function omission (intentional, Phase 3 owns)**
- **Location:** `pkg/dag/input.go` line ~36 (`MarshalJSON` body)
- **Reason:** `*starlark.Function` is not JSON-serializable. Phase 3 picks the serialization strategy (custom DataConverter vs. re-parse-on-start with content-hash cache).
- **TODO marker present:** `TODO(phase3): replace with real lambda serialization`. The function emits sorted lambda IDs in place of full function bodies.
- **Resolution path:** Phase 3 entry-gate decision — see PROJECT.md "Out of Scope" / Phase 3 plan.

**2. `starlarkValueToGo` in `pkg/dag/marshal.go` covers only primitive Starlark types**
- **Location:** `pkg/dag/marshal.go` line ~187
- **Reason:** Phase 1 fixtures use only String/Bool/Int/Float kwargs. Nested Dict/List/struct values fall through to `v.String()` which is not round-trippable.
- **Resolution path:** Phase 3 will extend the converter when richer kwarg types appear in real consultant code. Plan 05 may stretch this if a fixture needs nested values; the test corpus will surface that requirement.

Neither stub blocks plan 02's goal (lock the type spine). Plan 03/04/05 can construct, freeze, and marshal every node without the resolutions above.

## Next Phase Readiness

- **Plan 01-03 (pkg/extension SDK)** — unblocked. Can import `dag.ActionRef` as the return type from extension factory builtins; can store `dag.ValidationError` for kwarg-schema rejection.
- **Plan 01-04 (pkg/parser + pkg/bridge)** — unblocked. Can construct any of the 6 node types via struct literal, call `*ActionRef.Freeze()`, compute lambda IDs via `dag.ComputeLambdaID(fileBytes, pos)`, pass `*RetryPolicy`/`*Timeout` to `starlark.UnpackArgs("step", ..., "retry?", &retry)` for the step()/for_each_parallel() builtins.
- **Plan 01-05 (parser integration / fixture-based tests)** — unblocked. The marshal contract is locked; plan 05's `02-all-primitives.golden.json` regenerator can run against this stable shape.
- No blockers. No concerns.

## Verification Summary

```
go build ./pkg/dag/...                        → exit 0
go vet ./pkg/dag/...                          → exit 0
go test ./pkg/dag/... -race -count=1          → exit 0
grep -r 'go.temporal.io' pkg/dag/             → 0 matches  (architectural firewall holds)
grep -E 'pkg/(parser|extension|bridge)' pkg/dag/  → 0 matches  (downward import flow per ARCHITECTURE.md)
```

Test counts:
- `pkg/dag/...` total tests: 63 (10 carried from plan 01-01, 53 added in plan 01-02)
- All `var _ Node = (*X)(nil)` and `var _ starlark.Value/Unpacker = ...` compile-time assertions resolve.

## Self-Check: PASSED

Verified all claimed files exist and all claimed commits are present:

- FOUND: pkg/dag/node.go
- FOUND: pkg/dag/node_test.go
- FOUND: pkg/dag/flow.go
- FOUND: pkg/dag/flow_test.go
- FOUND: pkg/dag/step.go
- FOUND: pkg/dag/step_test.go
- FOUND: pkg/dag/control.go
- FOUND: pkg/dag/control_test.go
- FOUND: pkg/dag/input.go
- FOUND: pkg/dag/input_test.go
- FOUND: pkg/dag/retry.go
- FOUND: pkg/dag/retry_test.go
- FOUND: pkg/dag/action.go
- FOUND: pkg/dag/action_test.go
- FOUND: pkg/dag/lambda.go
- FOUND: pkg/dag/lambda_test.go
- FOUND: pkg/dag/marshal.go
- FOUND: pkg/dag/marshal_test.go
- FOUND: commit d99367c (Task 1)
- FOUND: commit 626ab58 (Task 2)
- FOUND: commit 7e7c0a9 (Task 3)
- FOUND: commit 32de22c (Task 4)
- VERIFIED: `go build ./pkg/dag/...` exits 0
- VERIFIED: `go vet ./pkg/dag/...` exits 0
- VERIFIED: `go test ./pkg/dag/... -race -count=1` exits 0 with 63 tests passing

---
*Phase: 01-type-spine-extension-contract-parser-bridge-foundations*
*Completed: 2026-04-27*
