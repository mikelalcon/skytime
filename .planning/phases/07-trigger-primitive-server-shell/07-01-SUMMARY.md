---
phase: 07-trigger-primitive-server-shell
plan: 01
subsystem: dag
tags: [trigger, dag, json, marshal, starlark, credential-redaction]

# Dependency graph
requires:
  - phase: 01-foundation
    provides: dag.Node sealed-marker idiom; CapturedLambda{ID, Fn} shape; pkg/dag/marshal.go Pos-exclusion convention; ActionRef CredentialID-only contract
provides:
  - "dag.Trigger struct (Pos, FlowName, Source, MapLambda, IdempotencyLambda, CredentialID, frozen)"
  - "dag-local TriggerSource interface (Kind() string + MarshalJSON() ([]byte, error)) — minimal compilation seam, full seal lives in pkg/extension"
  - "Trigger MarshalJSON / UnmarshalJSON via triggerJSON wire shape (Pos excluded; Source delegated to TriggerSource.MarshalJSON; lambdas serialized as IDs only)"
  - "RegisterTriggerSourceUnmarshaler / unmarshalTriggerSource cross-package seam for kind-keyed Source dispatch (Plan 02 wires)"
affects: [07-02-extension-triggersource, 07-03-parser-trigger-builtin, 07-04-trigger-registry-and-boot, 07-05-server-subcommand, 07-06-rename-and-firewalls]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Cross-package unmarshal-registry seam (var fn + Register* setter) — pkg/dag exposes the seam, pkg/extension fills it from package init"
    - "Pos-exclusion JSON convention extended to Trigger (mirrors actionRefJSON, file-header comment in pkg/dag/marshal.go)"
    - "Sealed-marker idiom WITHOUT Node interface — Trigger reuses dag's marker pattern but does NOT satisfy Node so flow.Body walkers stay unchanged (Pitfall 11 resolution)"

key-files:
  created:
    - pkg/dag/trigger.go
    - pkg/dag/trigger_test.go
    - .planning/phases/07-trigger-primitive-server-shell/07-01-SUMMARY.md
  modified:
    - pkg/dag/marshal.go

key-decisions:
  - "Trigger does NOT implement nodeMarker() — it is a top-level declaration, never inside flow.Body; CONTEXT.md's 'satisfies dag.Node seal' is read as 'reuses sealed-marker idiom', not 'implements Node literally' (Pitfall 11)"
  - "TriggerSource is a dag-local structural interface (Kind + MarshalJSON only); the actual seal lives in pkg/extension to keep pkg/dag dependency-free of pkg/extension"
  - "unmarshalTriggerSource defaults to a clear error message instead of nil panic — anyone unmarshaling a Trigger before pkg/extension package init runs gets diagnostic value"

patterns-established:
  - "Trigger{Pos, FlowName, Source, MapLambda, IdempotencyLambda, CredentialID, frozen} field order — fixed by Plan 01, reused by parser/registry"
  - "triggerJSON{Kind, FlowName, Source(json.RawMessage), MapLambdaID, IdempotencyLambdaID, CredentialID} wire shape — Pos excluded, Source delegated"
  - "Cross-package var-and-setter seam for unmarshal dispatch (mirrors how Phase 4's validator wires into the parser)"

requirements-completed: [TRIG-03]

# Metrics
duration: 7min
completed: 2026-05-08
---

# Phase 07 Plan 01: DAG Trigger Node Summary

**dag.Trigger pure-data node with stable JSON marshaling, dag-local TriggerSource seam, and Pos-exclusion + credential-never-serialized contracts that mirror dag.ActionRef.**

## Performance

- **Duration:** ~7 min
- **Started:** 2026-05-08T19:37:00Z
- **Completed:** 2026-05-08T19:44:00Z
- **Tasks:** 2
- **Files modified:** 3 (2 new, 1 extended)

## Accomplishments

- `dag.Trigger` struct shipped with the field order fixed by the spec — `Pos, FlowName, Source TriggerSource, MapLambda *CapturedLambda, IdempotencyLambda *CapturedLambda, CredentialID string, frozen bool`.
- Dag-local `TriggerSource` interface published — minimal `Kind() string + MarshalJSON() ([]byte, error)` so pkg/dag does not have to import pkg/extension. The full seal (with `triggerSourceMarker()` and `ReqSchema()`) lives in pkg/extension and is wired in Plan 02.
- `Trigger.Kind() / Position() / Freeze()` implemented; `Freeze()` recursively freezes `MapLambda.Fn`, `IdempotencyLambda.Fn`, and the Source if it implements `interface{ Freeze() }`. Idempotent — second `Freeze` is a no-op via the `frozen` flag.
- `Trigger.MarshalJSON / UnmarshalJSON` wired via the `triggerJSON` shape in `pkg/dag/marshal.go` (placed before `actionRefJSON` so "things that are NOT body Nodes" group together). Pos is **excluded** from JSON; lambdas serialize as content-hash IDs only; Source is delegated to `TriggerSource.MarshalJSON()` so each concrete source controls its own envelope.
- `RegisterTriggerSourceUnmarshaler(fn)` / `unmarshalTriggerSource` cross-package seam declared. Plan 02 (pkg/extension) installs the kind-keyed registry from package init. The default value returns a clear error so anyone unmarshaling a Trigger before pkg/extension is loaded gets diagnostic value instead of a nil panic.
- `Trigger` is **NOT** a `dag.Node` — Pitfall 11 resolved. flow.Body walkers stay unchanged.
- Credential contract enforced: `CredentialID` is the **only** credential-related field on the wire. No `extension.Secret`, no resolved credential, no `reveal`-shaped field reaches any marshaled byte. Verified by `TestTrigger_MarshalRoundTrip_NoCredentialLeak`. Plan 06 grep-gates the absence of `%+v` formatting verbs project-wide.

## Trigger Struct Shape (Shipped)

```go
type Trigger struct {
    Pos               syntax.Position
    FlowName          string
    Source            TriggerSource
    MapLambda         *CapturedLambda
    IdempotencyLambda *CapturedLambda
    CredentialID      string
    frozen            bool
}

type TriggerSource interface {
    Kind() string
    MarshalJSON() ([]byte, error)
}
```

## triggerJSON Wire Shape (Shipped)

```go
type triggerJSON struct {
    Kind                string          `json:"kind"`                  // always "Trigger"
    FlowName            string          `json:"flow_name"`
    Source              json.RawMessage `json:"source"`                // delegated to TriggerSource.MarshalJSON; "null" when Source==nil
    MapLambdaID         string          `json:"map_lambda_id,omitempty"`
    IdempotencyLambdaID string          `json:"idempotency_lambda_id,omitempty"`
    CredentialID        string          `json:"credential_id,omitempty"`
}
```

## Cross-Package Seam (Plan 02 Wires This)

```go
// In pkg/dag/marshal.go:
var unmarshalTriggerSource = func(data []byte) (TriggerSource, error) {
    return nil, fmt.Errorf("dag: no TriggerSource unmarshaler registered (Plan 02 wires extension package)")
}

func RegisterTriggerSourceUnmarshaler(fn func([]byte) (TriggerSource, error))
```

Plan 02 (`pkg/extension/trigger_unmarshal.go`) calls `dag.RegisterTriggerSourceUnmarshaler(...)` from package init with a kind-keyed dispatcher.

## Test Coverage for TRIG-03

`pkg/dag/trigger_test.go` ships eight test functions all green under `-race`:

1. `TestTrigger_MarshalRoundTrip` — kind, flow_name, lambda IDs, credential_id, and source envelope all serialize correctly.
2. `TestTrigger_MarshalRoundTrip_NoPos` — `syntax.MakePosition("/tmp/abs/path.star", 42, 7)` is set, but the marshaled bytes contain neither the filename, the line number, nor any "pos"-prefixed key.
3. `TestTrigger_MarshalRoundTrip_NoCredentialLeak` — with credential, `"credential_id":"my-secret-id"` appears and no `secret`/`reveal`-shaped field appears; with blank credential, the entire `credential_id` field is omitted (omitempty).
4. `TestTrigger_UnmarshalRoundTrip` — registers a fake TriggerSource unmarshaler via `t.Cleanup`-guarded `RegisterTriggerSourceUnmarshaler`, marshals a fully-populated Trigger, unmarshals it, and asserts `FlowName / MapLambda.ID / IdempotencyLambda.ID / CredentialID` are restored exactly while `Pos.IsValid() == false`.
5. `TestTrigger_UnmarshalRoundTrip_NilSource` — nil Source serializes as `"source":null` and round-trips back to nil without invoking the registry.
6. `TestTrigger_UnmarshalRoundTrip_KindMismatchRejected` — wrong kind discriminator surfaces a wrapped `kind=...` error.
7. `TestTrigger_Freeze` — uses `starlark.ExecFile` to materialize two real `*starlark.Function` lambdas, asserts `Trigger.Freeze` cascades into both lambdas + a `freezableTSrc` Source, and verifies idempotency (second Freeze does NOT re-freeze the Source).
8. `TestTrigger_Freeze_NilSafe` — Freeze on a Trigger with nil Source / nil lambdas does not panic and still flips the `frozen` flag.

## Task Commits

Each task was committed atomically (TDD path on Task 2):

1. **Task 1: Create pkg/dag/trigger.go** — `7522953` (feat)
2. **Task 2 RED: Add failing trigger tests** — `7f3345c` (test)
3. **Task 2 GREEN: Wire MarshalJSON/UnmarshalJSON in pkg/dag/marshal.go** — `588537e` (feat)

No REFACTOR step needed — the implementation matched the plan's verbatim interface block and stayed clean.

## Files Created/Modified

- `pkg/dag/trigger.go` (NEW, 102 lines) — Trigger struct, TriggerSource interface, Kind/Position/Freeze methods.
- `pkg/dag/trigger_test.go` (NEW, 235 lines) — Eight test functions exercising marshal, unmarshal, Pos exclusion, credential redaction, and Freeze recursion + idempotency.
- `pkg/dag/marshal.go` (MODIFIED, +115 lines) — `triggerJSON` shape, `Trigger.MarshalJSON`, `Trigger.UnmarshalJSON`, `unmarshalTriggerSource` package-level seam, and `RegisterTriggerSourceUnmarshaler` setter. Inserted before `actionRefJSON` to group "non-Node" types together.

## Decisions Made

- **Trigger does NOT implement `dag.Node` (Pitfall 11 resolved).** CONTEXT.md mentions "satisfies dag.Node seal"; we read this as "reuses the sealed-marker idiom" rather than "implements the Node interface literally". Justification: Triggers are top-level declarations stored in `interpreter.TriggerRegistry`, never inside `flow.Body`. Implementing Node would force every body walker (parser finalize, interpreter walkSteps) to add a defensive `case *dag.Trigger:` arm. The doc comment in `trigger.go` explains this.
- **`TriggerSource` is dag-local + structural, not sealed at the dag level.** Putting the seal in pkg/dag would force pkg/dag to import pkg/extension or vice versa to satisfy the marker. Instead, the full seal (with `triggerSourceMarker()`) lives in pkg/extension; pkg/dag exposes only the structural methods Trigger needs to call. The compile-time guarantee that only properly-sealed sources reach a `*dag.Trigger` comes from the parser builtin (Plan 03), which accepts only `extension.TriggerSource` values.
- **Default `unmarshalTriggerSource` returns an explanatory error.** Plan 02 will set the variable from `pkg/extension`'s `init()`; before that wiring lands, anyone unmarshaling a Trigger with a non-null Source gets a clear "no TriggerSource unmarshaler registered" message instead of a nil panic. This also helps tests that need to assert the seam exists.
- **Inserted triggerJSON BEFORE actionRefJSON in marshal.go.** The plan said "your call, but PUT IT BEFORE actionRefJSON to keep 'things that are NOT body Nodes' together". Done — both Trigger and ActionRef are sealed-but-not-Node types.

## Deviations from Plan

None. Plan executed exactly as written. The verbatim interface blocks in the plan compiled and tested clean on first attempt; the only minor adaptation was using `syntax.MakePosition(&fname, 42, 7)` in `TestTrigger_MarshalRoundTrip_NoPos` because `syntax.Position`'s `Filename` field is unexported in the pinned `go.starlark.net` version (the public constructor is `MakePosition(file *string, line, col int32)`). This matches what `pkg/dag/lambda.go::ComputeLambdaID` already does — it accesses `pos.Line` and `pos.Col` (which ARE exported) but constructs Positions only via `MakePosition`. No deviation rule fired; this is just the standard Go API for `syntax.Position`.

## Issues Encountered

- **Git lock contention from parallel Wave-1 agents.** One commit hit `Unable to create '.git/index.lock': File exists` because plan 02's executor was committing concurrently. Resolved via the documented `until [ ! -f .git/index.lock ]; do sleep 1; done` pattern — no work lost, no retries needed beyond the wait.

## Self-Check: PASSED

- pkg/dag/trigger.go: FOUND
- pkg/dag/trigger_test.go: FOUND
- pkg/dag/marshal.go (modified): FOUND
- Commit 7522953 (Task 1): FOUND
- Commit 7f3345c (Task 2 RED): FOUND
- Commit 588537e (Task 2 GREEN): FOUND
- `go build ./pkg/dag/...`: PASS
- `go vet ./pkg/dag/...`: PASS
- `go test ./pkg/dag/... -count=1 -race`: PASS (full pkg/dag suite green; existing ActionRef/Flow/Step/IfCond/Result/Fail/Script/ForEachParallel/CallFlow tests all still pass)
- Cross-package leakage check: OK — no `dag.Trigger` references outside `pkg/dag/` yet (Plan 02 changes pkg/extension; Plan 03 changes pkg/parser).

## User Setup Required

None — pure DAG node + JSON marshaling; no external services or configuration.

## Next Phase Readiness

- **Plan 02 (pkg/extension TriggerSource):** unblocked. The dag-local `TriggerSource` interface and `RegisterTriggerSourceUnmarshaler` seam are both present. Plan 02's `pkg/extension/trigger.go` will declare `extension.TriggerSource` (with `triggerSourceMarker()` seal + `ReqSchema()`) and call `dag.RegisterTriggerSourceUnmarshaler(...)` from `init()`.
- **Plan 03 (parser builtin):** unblocked. `*dag.Trigger` is constructible; the parser's `trigger(...)` builtin can populate FlowName/Source/MapLambda/IdempotencyLambda/CredentialID and append to a per-parser slice that Plan 04 will read.
- **Plans 04+:** indirectly ready via the chain above.

---
*Phase: 07-trigger-primitive-server-shell*
*Completed: 2026-05-08*
