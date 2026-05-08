---
phase: 07-trigger-primitive-server-shell
plan: 01
type: execute
wave: 1
depends_on: []
priority: high
estimated_tasks: 2
autonomous: true
requirements:
  - TRIG-03
files_modified:
  - pkg/dag/trigger.go
  - pkg/dag/trigger_test.go
  - pkg/dag/marshal.go
must_haves:
  truths:
    - "dag.Trigger struct exists with fields {Pos, FlowName, Source TriggerSource, MapLambda *CapturedLambda, IdempotencyLambda *CapturedLambda, CredentialID string} (sealed via interface in pkg/extension - depends_on Plan 02 NOT, but Plan 01 uses an interface placeholder satisfied via a forward-declared dag-local TriggerSource interface)"
    - "Trigger.Kind() returns the literal string \"Trigger\""
    - "Trigger.Position() returns the Pos field"
    - "Trigger.Freeze() recursively freezes Source (if non-nil and Freezable), MapLambda.Fn, IdempotencyLambda.Fn"
    - "Trigger.MarshalJSON emits {kind:\"Trigger\", flow_name, source, map_lambda_id, idempotency_lambda_id, credential_id (omitempty)} and NEVER includes Pos"
    - "Trigger.MarshalJSON delegates Source serialization to Source.MarshalJSON() so the {kind, config} envelope is produced by the concrete TriggerSource type"
    - "Round-trip MarshalJSON -> UnmarshalJSON preserves FlowName, MapLambda.ID, IdempotencyLambda.ID, CredentialID (Pos remains zero, which matches dag.ActionRef precedent)"
    - "CredentialID is the only credential-related field that crosses JSON; no extension.Secret, no resolved credential value reaches any Trigger marshaled byte"
  artifacts:
    - path: pkg/dag/trigger.go
      provides: "dag.Trigger node type, sealed TriggerSource interface (dag-side; concrete types live in pkg/extension), Position/Kind/Freeze methods"
      contains: "type Trigger struct"
    - path: pkg/dag/trigger_test.go
      provides: "Round-trip JSON test, Pos-exclusion test, Freeze recursion test, no-Secret-leak test"
      contains: "TestTrigger_MarshalRoundTrip"
    - path: pkg/dag/marshal.go
      provides: "triggerJSON shape + UnmarshalJSON via kind-keyed unmarshal registry"
      contains: "triggerJSON"
  key_links:
    - from: pkg/dag/trigger.go (Trigger.MarshalJSON)
      to: pkg/dag/marshal.go (triggerJSON shape)
      via: "json.Marshal(triggerJSON{...})"
      pattern: "triggerJSON\\{"
    - from: pkg/dag/trigger.go (Trigger.MarshalJSON)
      to: TriggerSource.MarshalJSON (defined in Plan 02; placeholder interface in Plan 01)
      via: "delegates to Source.MarshalJSON when Source != nil"
      pattern: "Source\\.MarshalJSON"
---

<objective>
Land the pure-data DAG node `dag.Trigger` with stable JSON marshaling (TRIG-03). This is Wave-1 foundation: Plan 02 (extension.TriggerSource) runs in parallel; Plans 03+ depend on this. Mirror `dag.ActionRef`'s shape (struct + Pos-stripping JSON + CredentialID-only credential reference) verbatim — Triggers carry the same credential-never-serialized contract.

Purpose: Make `*dag.Trigger` available as a sealed top-level declaration type that the parser (Plan 03) emits and the registry (Plan 04) stores. Wire stable JSON via a kind-keyed unmarshal registry so Plan 02's `TriggerSource` round-trips through the wire.

Output: `pkg/dag/trigger.go` (~120 LOC including doc comments), `pkg/dag/trigger_test.go` (~150 LOC), and an extension to `pkg/dag/marshal.go` (~30 LOC for triggerJSON + UnmarshalJSON registry hook).

LOAD-BEARING CONSTRAINT: `Trigger.MarshalJSON` MUST emit ONLY the credential ID string in `credential_id` — never an `extension.Secret`, never a resolved credential. This is verified by an explicit test plus a firewall test in Plan 06. Plan 01 establishes the contract; Plan 06 grep-gates it.

NOTE on Pitfall #11 (CONTEXT.md `<key_constraints>` says "Trigger satisfies the existing dag.Node seal"; RESEARCH.md § Pitfall 11 recommends Trigger does NOT satisfy `dag.Node` because Triggers never appear inside flow.Body and Node walkers would need defensive `case *dag.Trigger`). RESOLUTION FOR THIS PLAN: Trigger does NOT implement `nodeMarker()`. Trigger is a separate top-level declaration type. The CONTEXT.md sentence is read as "uses the same sealed-marker IDIOM" rather than "implements the Node interface literally". This is a Claude's-discretion call documented in the doc comment of `pkg/dag/trigger.go`.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/PROJECT.md
@.planning/ROADMAP.md
@.planning/STATE.md
@.planning/phases/07-trigger-primitive-server-shell/07-CONTEXT.md
@.planning/phases/07-trigger-primitive-server-shell/07-RESEARCH.md
@.planning/phases/07-trigger-primitive-server-shell/07-VALIDATION.md
@CLAUDE.md
@pkg/dag/action.go
@pkg/dag/node.go
@pkg/dag/marshal.go
@pkg/dag/lambda.go

<interfaces>
<!-- Concrete code patterns the executor MUST replicate. -->

Existing precedent (pkg/dag/action.go:23-44):
```go
type ActionRef struct {
    Pos          syntax.Position // EXCLUDED from JSON (cross-machine stability)
    Kind_        string          // discriminator
    Kwargs       *starlark.Dict
    CredentialID string          // ID-only, never a Secret
    frozen       bool
}
```

Existing JSON shape (pkg/dag/marshal.go:235-265):
```go
type actionRefJSON struct {
    Kind         string         `json:"kind"`
    Kwargs       map[string]any `json:"kwargs"`
    CredentialID string         `json:"credential_id,omitempty"`
}
// MarshalJSON converts Kwargs *starlark.Dict to map[string]any and emits the envelope.
// Pos is NOT in the JSON shape.
```

Existing CapturedLambda (verify in pkg/dag/lambda.go before writing tests):
```go
// CapturedLambda has fields including ID (content-hash string) and Fn (*starlark.Function).
// MapLambda.ID is what serializes; the *starlark.Function does NOT serialize.
```

Trigger struct shape THIS PLAN must produce (paste verbatim into pkg/dag/trigger.go):
```go
// Trigger is a top-level declaration produced by the Starlark builtin
// trigger(...). It is NOT a dag.Node — Triggers never appear inside
// flow.Body. They are stored in interpreter.TriggerRegistry alongside
// FlowRegistry. Sealing semantics mirror dag.Node's pattern (see
// CONTEXT.md key_constraints; Pitfall 11 in RESEARCH.md): Trigger
// satisfies a parallel sealed-marker idiom but NOT the Node interface
// literally, so flow.Body walkers don't need defensive cases.
//
// Sealed via the unexported triggerSourceMarker() method in pkg/extension
// (TriggerSource interface). Plan 01 uses a dag-local interface that
// pkg/extension.TriggerSource will satisfy in Plan 02.
//
// Pos is the call-site of the trigger(...) builtin. EXCLUDED from JSON
// for cross-machine stability (mirrors the convention from pkg/dag/marshal.go).
//
// Credential contract (D-07-09 / D-07-10): Trigger carries CredentialID
// (a string ID into the credential handler's keyspace). Trigger NEVER
// serializes an extension.Secret, never serializes a resolved credential.
// JIT resolution happens inside the receiver in Phase 7.1; even there,
// the resolved Secret is wrapped in extension.Secret which redacts on
// every marshaling path.
type Trigger struct {
    Pos               syntax.Position
    FlowName          string
    Source            TriggerSource
    MapLambda         *CapturedLambda
    IdempotencyLambda *CapturedLambda
    CredentialID      string
    frozen            bool
}

// TriggerSource is a dag-local seal placeholder. The concrete sealed
// interface lives in pkg/extension/trigger.go (Plan 02). Plan 01 declares
// only the methods Trigger needs to call from within pkg/dag.
//
// Plan 02 will declare extension.TriggerSource as:
//   type TriggerSource interface { Kind() string; ReqSchema() []string; MarshalJSON() ([]byte, error); triggerSourceMarker() }
// and any value satisfying both the dag-local interface here AND the
// extension.triggerSourceMarker() seal can flow through dag.Trigger.
type TriggerSource interface {
    Kind() string
    MarshalJSON() ([]byte, error)
}

// Kind returns the discriminator used in DAG marshaling. Always "Trigger".
func (t *Trigger) Kind() string { return "Trigger" }

// Position returns the Pos field for parse-error attribution.
func (t *Trigger) Position() syntax.Position { return t.Pos }

// Freeze recursively freezes Source (if non-nil), MapLambda.Fn, and
// IdempotencyLambda.Fn. Idempotent: second Freeze call is a no-op.
func (t *Trigger) Freeze() {
    if t.frozen { return }
    t.frozen = true
    if t.MapLambda != nil && t.MapLambda.Fn != nil { t.MapLambda.Fn.Freeze() }
    if t.IdempotencyLambda != nil && t.IdempotencyLambda.Fn != nil { t.IdempotencyLambda.Fn.Freeze() }
    // Source freeze: TriggerSource may optionally implement Freezable.
    // If it does, call Freeze(). Otherwise no-op (sources without mutable
    // state need no freezing).
    if f, ok := t.Source.(interface{ Freeze() }); ok { f.Freeze() }
}
```

Trigger JSON shape THIS PLAN must produce (paste verbatim into pkg/dag/marshal.go AT THE END of the file, after the existing forEachParallelJSON / callFlowJSON blocks):
```go
// triggerJSON is the marshal-time shape of *Trigger. The Pos field is
// deliberately excluded for cross-machine stability (same convention as
// actionRefJSON, line 232). The Source field is delegated to the concrete
// TriggerSource implementation (returns the {kind, config} envelope).
//
// CRITICAL: credential_id is a STRING ID. No extension.Secret value, no
// resolved credential, ever appears in this JSON shape. The Source.config
// envelope produced by each concrete TriggerSource also carries only
// credential ID strings — verified by tests/firewall_credential_redaction_test.go
// in Plan 06.
type triggerJSON struct {
    Kind                string          `json:"kind"`
    FlowName            string          `json:"flow_name"`
    Source              json.RawMessage `json:"source"`
    MapLambdaID         string          `json:"map_lambda_id,omitempty"`
    IdempotencyLambdaID string          `json:"idempotency_lambda_id,omitempty"`
    CredentialID        string          `json:"credential_id,omitempty"`
}

// MarshalJSON emits a Trigger with a "kind":"Trigger" discriminator. The
// Source field is rendered via Source.MarshalJSON() so each concrete
// TriggerSource type controls its own envelope. Lambdas are referenced by
// content-hash ID only — the *starlark.Function bodies are reconstructed
// at runtime via interpreter.RunOnceCapturing (Phase 3 lambda-serialization
// contract).
func (t *Trigger) MarshalJSON() ([]byte, error) {
    var sourceRaw json.RawMessage
    if t.Source != nil {
        b, err := t.Source.MarshalJSON()
        if err != nil { return nil, err }
        sourceRaw = b
    } else {
        sourceRaw = json.RawMessage("null")
    }
    var mapID, idempID string
    if t.MapLambda != nil { mapID = t.MapLambda.ID }
    if t.IdempotencyLambda != nil { idempID = t.IdempotencyLambda.ID }
    return json.Marshal(triggerJSON{
        Kind:                "Trigger",
        FlowName:            t.FlowName,
        Source:              sourceRaw,
        MapLambdaID:         mapID,
        IdempotencyLambdaID: idempID,
        CredentialID:        t.CredentialID,
    })
}

// UnmarshalJSON reads the {kind, flow_name, source, map_lambda_id,
// idempotency_lambda_id, credential_id} envelope. Source unmarshaling
// dispatches via a kind-keyed registry populated by extensions during
// their Initialize() lifecycle (see pkg/extension/trigger_unmarshal.go,
// Plan 02). Pos is NOT recovered (zero value), matching the dag.ActionRef
// precedent — runtime attribution uses lambda IDs, not source positions.
//
// Lambdas are NOT reconstructed here. The deserialized Trigger has
// MapLambda/IdempotencyLambda nil (only the IDs are kept) — Plan 04's
// TriggerRegistry will rehydrate Fn pointers via the same lambda registry
// the FlowRegistry uses.
func (t *Trigger) UnmarshalJSON(data []byte) error {
    var raw triggerJSON
    if err := json.Unmarshal(data, &raw); err != nil { return err }
    if raw.Kind != "Trigger" {
        return fmt.Errorf("dag: unmarshal Trigger: kind=%q expected \"Trigger\"", raw.Kind)
    }
    t.FlowName = raw.FlowName
    t.CredentialID = raw.CredentialID
    // MapLambda / IdempotencyLambda: only the IDs are kept; Fn rehydration
    // is the registry's responsibility.
    if raw.MapLambdaID != "" {
        t.MapLambda = &CapturedLambda{ID: raw.MapLambdaID}
    }
    if raw.IdempotencyLambdaID != "" {
        t.IdempotencyLambda = &CapturedLambda{ID: raw.IdempotencyLambdaID}
    }
    // Source: decode the discriminator and route through the registry.
    if len(raw.Source) > 0 && string(raw.Source) != "null" {
        src, err := unmarshalTriggerSource(raw.Source)
        if err != nil { return fmt.Errorf("dag: unmarshal Trigger.Source: %w", err) }
        t.Source = src
    }
    return nil
}

// unmarshalTriggerSource is wired in Plan 02 (pkg/extension/trigger_unmarshal.go).
// Plan 01 declares the function variable so Trigger.UnmarshalJSON compiles.
// Plan 02 sets the variable from extension package init.
var unmarshalTriggerSource = func(data []byte) (TriggerSource, error) {
    return nil, fmt.Errorf("dag: no TriggerSource unmarshaler registered (Plan 02 wires extension package)")
}

// RegisterTriggerSourceUnmarshaler is the cross-package seam used by
// pkg/extension to install the kind-keyed registry. Called once at
// package init from pkg/extension.
func RegisterTriggerSourceUnmarshaler(fn func([]byte) (TriggerSource, error)) {
    unmarshalTriggerSource = fn
}
```
</interfaces>
</context>

<tasks>

<task type="auto">
  <id>07-01-01</id>
  <name>Task 1: Create pkg/dag/trigger.go with the Trigger struct, sealed-marker shape, and Freeze recursion</name>
  <read_first>
    - pkg/dag/action.go (lines 1-100 — read the ActionRef precedent verbatim: struct fields, Pos exclusion comment, CredentialID semantics, Freeze recursion model)
    - pkg/dag/node.go (read sealed Node interface; understand WHY Trigger should NOT implement nodeMarker per Pitfall 11)
    - pkg/dag/lambda.go (verify exact CapturedLambda field names — ID, Fn — needed by Trigger.Freeze)
    - pkg/dag/marshal.go (lines 1-30 — read the file header comment about Pos exclusion, then lines 232-265 for actionRefJSON shape; THIS task does not edit marshal.go but the marshal-time shape feeds Task 2)
    - .planning/phases/07-trigger-primitive-server-shell/07-CONTEXT.md (`<key_constraints>`: credentials never serialized; D-07-06 sealed marker; Pitfall 11 resolution noted in this plan's `<objective>`)
    - .planning/phases/07-trigger-primitive-server-shell/07-RESEARCH.md (§ Pitfall 11; § Code Examples Example 2 for TriggerSource shape — but ignore the methods Plan 02 owns; this plan only needs the dag-local Kind() + MarshalJSON())
  </read_first>
  <files>pkg/dag/trigger.go</files>
  <action>
    Create `pkg/dag/trigger.go` (NEW file, ~120 LOC) with:

    1. Package + imports:
       ```go
       package dag

       import (
           "go.starlark.net/syntax"
       )
       ```

    2. The dag-local `TriggerSource` interface AT TOP OF FILE (before `Trigger` struct):
       ```go
       // TriggerSource is the dag-side view of pkg/extension.TriggerSource.
       // Plan 01 ships this minimal interface (Kind + MarshalJSON) because
       // Plan 02 (pkg/extension/trigger.go) imports pkg/dag, so pkg/dag
       // cannot import pkg/extension (cycle). Concrete TriggerSource types
       // (e.g. github.WebhookSource in Phase 7.1) satisfy BOTH this
       // dag-local interface AND extension.TriggerSource (which adds
       // ReqSchema and the unexported triggerSourceMarker seal).
       type TriggerSource interface {
           Kind() string
           MarshalJSON() ([]byte, error)
       }
       ```

    3. The `Trigger` struct VERBATIM as in `<interfaces>`. Field order: Pos, FlowName, Source, MapLambda, IdempotencyLambda, CredentialID, frozen.

    4. Methods `Kind()`, `Position()`, `Freeze()` VERBATIM as in `<interfaces>`. Note: `Freeze()` checks `frozen` flag for idempotency, freezes MapLambda.Fn and IdempotencyLambda.Fn, and uses a type-assertion `if f, ok := t.Source.(interface{ Freeze() }); ok` to freeze the Source if it implements Freeze.

    5. NO `nodeMarker()` method — Trigger does NOT satisfy `dag.Node`. Add a doc comment explaining this:
       ```go
       // Trigger is intentionally NOT a dag.Node. flow.Body walkers (parser
       // finalize passes, interpreter walkSteps) iterate []Node and would
       // need defensive case *dag.Trigger arms if Trigger satisfied the
       // Node seal. Triggers are top-level declarations stored in
       // interpreter.TriggerRegistry, never inside flow.Body. See RESEARCH.md
       // § Pitfall 11 for the full reasoning. CONTEXT.md key_constraints
       // mentions "Trigger satisfies the existing dag.Node seal" — read as
       // "uses the same sealed-marker idiom", NOT "implements Node literally".
       ```

    Use `extension.Ptr(true)` is the existing helper (verified at pkg/extension/operation.go:79); not used in this plan but worth knowing.

    Per CLAUDE.md, the project's `go 1.25` directive is preserved; this plan adds no new imports beyond `go.starlark.net/syntax`.

    DO NOT touch pkg/dag/marshal.go in this task — that's Task 2.
    DO NOT add `Trigger` to the existing `Node` interface in `pkg/dag/node.go`.
    DO NOT define `unmarshalTriggerSource` or `RegisterTriggerSourceUnmarshaler` in this file — those go in marshal.go (Task 2).
  </action>
  <acceptance_criteria>
    - File `pkg/dag/trigger.go` exists
    - `grep -n 'type Trigger struct' pkg/dag/trigger.go` returns exactly one match
    - `grep -n 'type TriggerSource interface' pkg/dag/trigger.go` returns exactly one match
    - `grep -nE 'func \(t \*Trigger\) Kind\(\) string' pkg/dag/trigger.go` returns exactly one match
    - `grep -nE 'return "Trigger"' pkg/dag/trigger.go` returns at least one match (in Kind())
    - `grep -nE 'func \(t \*Trigger\) Position\(\) syntax\.Position' pkg/dag/trigger.go` returns exactly one match
    - `grep -nE 'func \(t \*Trigger\) Freeze\(\)' pkg/dag/trigger.go` returns exactly one match
    - `grep -n 'nodeMarker()' pkg/dag/trigger.go` returns ZERO matches (Trigger is NOT a Node)
    - `grep -n 'NEVER\|never\|credential ID' pkg/dag/trigger.go` returns at least one match (the credential-never-serialized doc comment per D-07-09 / D-07-10)
    - `go build ./pkg/dag/...` exits 0
    - `go vet ./pkg/dag/...` exits 0
  </acceptance_criteria>
  <verify>
    <automated>go build ./pkg/dag/... && go vet ./pkg/dag/... && grep -q 'type Trigger struct' pkg/dag/trigger.go && grep -q 'type TriggerSource interface' pkg/dag/trigger.go && ! grep -q 'nodeMarker' pkg/dag/trigger.go</automated>
  </verify>
  <done>
    `pkg/dag/trigger.go` compiles; `Trigger` struct + dag-local `TriggerSource` interface + `Kind()/Position()/Freeze()` are present; `Trigger` does NOT implement `nodeMarker()`. No JSON code yet (Task 2).
  </done>
</task>

<task type="auto" tdd="true">
  <id>07-01-02</id>
  <name>Task 2: Wire MarshalJSON / UnmarshalJSON in pkg/dag/marshal.go and ship round-trip + Pos-exclusion + Freeze tests</name>
  <read_first>
    - pkg/dag/trigger.go (the file just created in Task 1)
    - pkg/dag/marshal.go (FULL file — understand the existing pattern: each `*JSON` shape struct, `MarshalJSON` method, Pos exclusion comment at file head)
    - pkg/dag/marshal_test.go if it exists (sibling pattern for round-trip testing)
    - pkg/dag/action_test.go (precedent for ActionRef Freeze testing — pattern for asserting `frozen` flag)
    - .planning/phases/07-trigger-primitive-server-shell/07-VALIDATION.md (the per-task verification map: TRIG-03 maps to TestTrigger_MarshalRoundTrip + TestTrigger_Freeze in pkg/dag/trigger_test.go)
  </read_first>
  <files>pkg/dag/marshal.go, pkg/dag/trigger_test.go</files>
  <behavior>
    - Test 1 (TestTrigger_MarshalRoundTrip): Construct a Trigger with all fields set (FlowName="check_user", Source=fakeTriggerSource{kind:"fake.webhook"}, MapLambda={ID:"abc:1:2"}, IdempotencyLambda={ID:"def:3:4"}, CredentialID="gh-secret"). Marshal to JSON. The bytes MUST contain `"kind":"Trigger"`, `"flow_name":"check_user"`, `"map_lambda_id":"abc:1:2"`, `"idempotency_lambda_id":"def:3:4"`, `"credential_id":"gh-secret"`, `"source":{"kind":"fake.webhook"...}`. The bytes MUST NOT contain any reference to "Pos", "Filename", or absolute file paths.
    - Test 2 (TestTrigger_MarshalRoundTrip_NoPos): Marshal a Trigger with `Pos: syntax.Position{Filename:"/tmp/abs/path.star", Line:42, Col:7}`. Assert `!bytes.Contains(out, []byte("/tmp/abs/path.star"))` AND `!bytes.Contains(out, []byte("\"line\":42"))` AND `!bytes.Contains(out, []byte("\"pos\""))`.
    - Test 3 (TestTrigger_MarshalRoundTrip_NoCredentialLeak): Marshal a Trigger with `CredentialID:"my-secret-id"`. Confirm the marshaled bytes contain `"credential_id":"my-secret-id"` (the ID string is fine) but DO NOT contain anything that resembles a Secret value. Also marshal with CredentialID="" — `credential_id` field MUST be omitted (omitempty) — assert `!bytes.Contains(out, []byte("credential_id"))`.
    - Test 4 (TestTrigger_UnmarshalRoundTrip): Marshal a Trigger to bytes; install a fake unmarshaler via `dag.RegisterTriggerSourceUnmarshaler`; unmarshal the bytes; assert FlowName, MapLambda.ID, IdempotencyLambda.ID, CredentialID are restored exactly. Pos.IsValid() returns false on the unmarshaled value (zero Pos is acceptable per ActionRef precedent).
    - Test 5 (TestTrigger_Freeze): Create Trigger with non-nil MapLambda.Fn and IdempotencyLambda.Fn (use `starlark.NewBuiltin` or a parsed *starlark.Function fixture). Call Freeze. Assert lambda Fns are frozen (call .Freeze() again — must be idempotent in starlark; use `Fn.Truth()` after freeze to confirm no panic). Then call trig.Freeze() a SECOND time — must not panic and must not re-call inner Freezes (set `frozen` true on first call).
    - Test 6 (TestTrigger_Freeze_NilSafe): Call Freeze on a Trigger with nil MapLambda, nil IdempotencyLambda, nil Source — must not panic.
  </behavior>
  <action>
    Step 1 — Extend `pkg/dag/marshal.go` (APPEND to end of file, after the existing `callFlowJSON` block at line ~228; before `actionRefJSON` at line 232 if you prefer to group with non-Node types — your call, but PUT IT BEFORE `actionRefJSON` to keep "things that are NOT body Nodes" together).

    Add the imports `"fmt"` (verify already present; if not, add) — `encoding/json` is already imported.

    Paste VERBATIM from the `<interfaces>` block:
    - `triggerJSON` struct
    - `func (t *Trigger) MarshalJSON() ([]byte, error)` method
    - `func (t *Trigger) UnmarshalJSON(data []byte) error` method
    - `var unmarshalTriggerSource = func(data []byte) (TriggerSource, error) { ... }` package-level seam
    - `func RegisterTriggerSourceUnmarshaler(fn func([]byte) (TriggerSource, error))` exported setter

    Step 2 — Create `pkg/dag/trigger_test.go` (NEW file). Use the testify package per CLAUDE.md (`require` + `assert`, NOT gomock). Standard `package dag` (white-box) so internal `frozen` field is reachable.

    For the fake TriggerSource used in tests, define inside `trigger_test.go`:
    ```go
    type fakeTSrc struct {
        kindName string
        configB  []byte // pre-rendered config bytes
    }

    func (f *fakeTSrc) Kind() string { return f.kindName }
    func (f *fakeTSrc) MarshalJSON() ([]byte, error) {
        // Envelope: {"kind":"<kindName>","config":<configB>}
        return []byte(fmt.Sprintf(`{"kind":%q,"config":%s}`, f.kindName, string(f.configB))), nil
    }
    // No triggerSourceMarker() — that lives in pkg/extension. The dag-local
    // TriggerSource interface only requires Kind() + MarshalJSON, which fakeTSrc satisfies.
    ```

    Implement the six tests above. Each must use `require.NoError` for fail-fast preconditions, `assert.Equal` / `assert.Contains` / `assert.NotContains` for accumulating checks. Use `bytes.Contains` for substring checks on marshaled output.

    Test 4 (UnmarshalRoundTrip) — register a per-test unmarshaler and reset it via `t.Cleanup`:
    ```go
    func TestTrigger_UnmarshalRoundTrip(t *testing.T) {
        prev := unmarshalTriggerSource // grab current
        t.Cleanup(func() { unmarshalTriggerSource = prev })
        RegisterTriggerSourceUnmarshaler(func(data []byte) (TriggerSource, error) {
            // Decode the {kind, config} envelope to recover a fakeTSrc
            var env struct{ Kind string; Config json.RawMessage }
            if err := json.Unmarshal(data, &env); err != nil { return nil, err }
            return &fakeTSrc{kindName: env.Kind, configB: env.Config}, nil
        })
        // ... rest of test ...
    }
    ```

    Step 3 — Run the tests:
    ```bash
    go test ./pkg/dag/ -run TestTrigger_ -count=1 -race
    go vet ./pkg/dag/...
    ```

    DO NOT introduce any new dependency in go.mod. encoding/json + fmt + go.starlark.net/syntax (already transitive) cover everything.
    DO NOT modify pkg/dag/action.go or pkg/dag/node.go.
    DO NOT add unmarshalTriggerSource population in this plan — that's Plan 02.

    Per D-07-09 / D-07-10 (CONTEXT.md key_constraints): The credential-never-serialized rule is enforced by Test 3 (NoCredentialLeak) PLUS the firewall test landed in Plan 06. This plan establishes the textual contract; Plan 06 grep-gates it.
  </action>
  <acceptance_criteria>
    - `grep -n 'type triggerJSON struct' pkg/dag/marshal.go` returns exactly one match
    - `grep -nE 'func \(t \*Trigger\) MarshalJSON' pkg/dag/marshal.go` returns exactly one match
    - `grep -nE 'func \(t \*Trigger\) UnmarshalJSON' pkg/dag/marshal.go` returns exactly one match
    - `grep -n 'RegisterTriggerSourceUnmarshaler' pkg/dag/marshal.go` returns exactly one match
    - `grep -n 'unmarshalTriggerSource' pkg/dag/marshal.go` returns at least two matches (var declaration + use in UnmarshalJSON)
    - `pkg/dag/marshal.go` does NOT contain the strings "Pos\":" or "filename" inside the triggerJSON struct (Pos exclusion contract)
    - `go test ./pkg/dag/ -run TestTrigger_MarshalRoundTrip -count=1` exits 0
    - `go test ./pkg/dag/ -run TestTrigger_MarshalRoundTrip_NoPos -count=1` exits 0
    - `go test ./pkg/dag/ -run TestTrigger_MarshalRoundTrip_NoCredentialLeak -count=1` exits 0
    - `go test ./pkg/dag/ -run TestTrigger_UnmarshalRoundTrip -count=1` exits 0
    - `go test ./pkg/dag/ -run TestTrigger_Freeze -count=1` exits 0
    - `go test ./pkg/dag/ -run TestTrigger_Freeze_NilSafe -count=1` exits 0
    - `go test ./pkg/dag/... -count=1 -race` exits 0 (full pkg/dag suite passes)
    - `go vet ./pkg/dag/...` exits 0
  </acceptance_criteria>
  <verify>
    <automated>go test ./pkg/dag/ -run 'TestTrigger_(MarshalRoundTrip|UnmarshalRoundTrip|Freeze)' -count=1 -race && go vet ./pkg/dag/... && grep -q 'type triggerJSON struct' pkg/dag/marshal.go && grep -q 'RegisterTriggerSourceUnmarshaler' pkg/dag/marshal.go</automated>
  </verify>
  <done>
    `dag.Trigger` round-trips through JSON; Pos is excluded; CredentialID is the only credential-related field; Freeze is recursive and idempotent. Plan 02 will install the real unmarshaler via `RegisterTriggerSourceUnmarshaler`; Plan 03 will populate Trigger from the parser; Plan 04 will store them in TriggerRegistry.
  </done>
</task>

</tasks>

<verification>
After both tasks complete, run:

```bash
go build ./pkg/dag/... && go vet ./pkg/dag/... && go test ./pkg/dag/... -count=1 -race
```

All must exit 0. The full DAG package test suite must remain green (existing ActionRef / Flow / Step tests untouched).

Cross-package check (no other package consumes Trigger yet):
```bash
git grep -F 'dag.Trigger' -- 'pkg/' | grep -v 'pkg/dag/' || echo 'OK: no leakage to other packages yet'
```

Expected: `OK: no leakage to other packages yet` (Plan 02 changes pkg/extension; Plan 03 changes pkg/parser).
</verification>

<success_criteria>
- TRIG-03 satisfied: `dag.Trigger` exists with stable JSON marshaling (Kind, FlowName, Source via delegation, MapLambdaID, IdempotencyLambdaID, CredentialID); round-trips cleanly through Marshal -> Unmarshal (Source via kind-keyed registry seam wired in Plan 02).
- D-07-09 enforced: credential ID is the only credential-related field on the JSON wire; tested explicitly by `TestTrigger_MarshalRoundTrip_NoCredentialLeak`.
- D-07-10 contract established: tests assert no Pos / no Secret reaches the wire; Plan 06 grep-gates the absence of `%+v` formatting verbs.
- Wave-1 unblocks Wave-2 (Plan 03 — parser builtinTrigger needs `*dag.Trigger`).
</success_criteria>

<output>
After completion, create `.planning/phases/07-trigger-primitive-server-shell/07-01-SUMMARY.md` documenting:
- The Trigger struct shape exactly as shipped (field order, types)
- The triggerJSON wire shape exactly as shipped
- The dag-local TriggerSource interface (Kind + MarshalJSON only — sealed-marker lives in pkg/extension)
- Confirmation that Trigger does NOT satisfy the Node interface (Pitfall 11 resolved)
- The `RegisterTriggerSourceUnmarshaler` seam name + signature (Plan 02 will populate it)
- Test coverage for TRIG-03 — explicit list of test functions in pkg/dag/trigger_test.go
</output>
