---
phase: 07-trigger-primitive-server-shell
plan: 03
type: execute
wave: 2
depends_on: [01, 02]
priority: high
estimated_tasks: 7
autonomous: true
requirements:
  - TRIG-01
  - TRIG-04
files_modified:
  - pkg/parser/builtins.go
  - pkg/parser/globals.go
  - pkg/parser/finalize.go
  - pkg/parser/req_walk.go
  - pkg/parser/req_walk_test.go
  - pkg/parser/trigger_test.go
  - pkg/parser/parser.go
  - pkg/parser/lambda_capture.go
  - pkg/parser/testdata/triggers/valid.star
  - pkg/parser/testdata/triggers/typo.star
  - pkg/parser/testdata/triggers/bad_arity.star
  - pkg/parser/testdata/triggers/unknown_flow.star
  - pkg/parser/testdata/triggers/mutable_closure.star
  - pkg/parser/testdata/triggers/not_a_source.star
  - pkg/parser/testdata/triggers/cross_file_flow.star
  - pkg/parser/testdata/triggers/cross_file_trigger.star
  - pkg/parser/testdata/triggers/duplicate_warn.star
  - pkg/bridge/lambda_globals.go
  - pkg/bridge/lambda_globals_test.go
  - pkg/bridge/doc.go
  - pkg/parser/doc.go
must_haves:
  truths:
    - "Top-level trigger(flow=, source=, map=, idempotency_key=, credential=) builtin is registered in p.parseTimeGlobals via newParseTimeGlobals; calling it parses without I/O and stores a *dag.Trigger in p.triggers"
    - "Trigger parser session has Parser.Triggers() accessor returning a deterministically-sorted slice (Source.Kind, FlowName, Pos)"
    - "trigger.source kwarg type-asserts to extension.TriggerSource — anything else (string, int, bare struct) errors with position-aware *dag.ParseError 'trigger.source: expected TriggerSource, got <type>'"
    - "trigger.map and trigger.idempotency_key lambdas are captured via the existing CapturedLambda machinery PLUS a new arity-1 enforcement layer; arity != 1 errors with position-aware *dag.ParseError"
    - "Trigger lambdas resolve free variables against a NEW triggerTimeGlobals StringDict in pkg/bridge — the locked 20-key lambdaTimeGlobals + json.Module + time.Module (D-07-01)"
    - "Cross-file trigger.FlowName resolution at finalize: trigger and flow can live in different files; unknown flow surfaces position-aware error 'trigger references unknown flow X; known flows: [a, b, c]' (D-07-12)"
    - "Two byte-identical triggers (same FlowName, same Source.MarshalJSON output, same lambda IDs) emit a slog.Warn during finalize and are accepted; non-identical duplicates are accepted silently (D-07-13)"
    - "req-walker: a NEW pkg/parser/req_walk.go generalizes ctx_walk.go's findCtxAccesses to a parameterized free-var-name visitor; ctx-walker becomes one caller, req-walker becomes another caller"
    - "req-walker validates each trigger lambda's req.<field> references against trig.Source.ReqSchema(); typos surface position-aware error 'req has no attribute X; available: [a, b]'"
    - "Existing TestCtxWalk_* tests pass unchanged (regression guard for the ctx_walk.go refactor)"
  artifacts:
    - path: pkg/parser/builtins.go
      provides: "builtinTrigger factory function — kwarg unpack, source type-assert, lambda arity check, position-aware error wrap"
      contains: "func (p *Parser) builtinTrigger"
    - path: pkg/parser/globals.go
      provides: "Single-line registration of \"trigger\" builtin in newParseTimeGlobals (parallel to \"flow\")"
      contains: "\"trigger\": starlark.NewBuiltin(\"trigger\""
    - path: pkg/parser/req_walk.go
      provides: "Houses the parameterized free-var-name + valid-attributes visitor PLUS trigger-finalize helpers: validateTriggerFlowNames, warnDuplicateTriggers, validateTriggerReqAccesses, checkTriggerLambdaReq, sortedFlowNames. The visitor's existing ctx-walker becomes one caller; the trigger req-walker becomes another."
      contains: "func findFreeVarAccesses"
    - path: pkg/parser/finalize.go
      provides: "Wires the new finalize passes into the validation chain (validateTriggerFlowNames before lintMixedIdempotency; validateTriggerReqAccesses after validateLambdaCtxAccesses; warnDuplicateTriggers last). The helpers themselves live in pkg/parser/req_walk.go."
      contains: "validateTriggerFlowNames"
    - path: pkg/parser/parser.go
      provides: "p.triggers map field, p.triggerWarnings []string field (deferred warnings surface at boot), Parser.Triggers() accessor"
      contains: "triggers map[string]*dag.Trigger"
    - path: pkg/bridge/lambda_globals.go
      provides: "triggerTimeGlobals StringDict (locked 20-key lambdaTimeGlobals + json + time) and TriggerTimeGlobals() copy accessor"
      contains: "triggerTimeGlobals"
    - path: pkg/parser/lambda_capture.go
      provides: "captureLambdaWithArity(thread, kwargName, val, expectedArity) wrapper around captureLambda, used by builtinTrigger only"
      contains: "captureLambdaWithArity"
    - path: pkg/parser/trigger_test.go
      provides: "TestBuiltinTrigger, TestBuiltinTrigger_Fields, TestTrigger_UnknownFlow, TestTrigger_BadSource, TestTrigger_ReqAttrTypo, TestTrigger_BadArity, TestTrigger_MutableClosure, TestTrigger_CrossFileFlow, TestTrigger_DuplicateWarn"
      contains: "TestBuiltinTrigger"
    - path: pkg/parser/req_walk_test.go
      provides: "Tests for the parameterized findFreeVarAccesses with both ctx and req as free-var name"
      contains: "TestFindFreeVarAccesses"
  key_links:
    - from: pkg/parser/globals.go (newParseTimeGlobals)
      to: pkg/parser/builtins.go (builtinTrigger)
      via: "g[\"trigger\"] = starlark.NewBuiltin(\"trigger\", p.builtinTrigger)"
      pattern: "starlark\\.NewBuiltin\\(\"trigger\""
    - from: pkg/parser/builtins.go (builtinTrigger)
      to: pkg/dag/trigger.go (*dag.Trigger struct)
      via: "trig := &dag.Trigger{...}; p.triggers[posKey] = trig"
      pattern: "&dag\\.Trigger\\{"
    - from: pkg/parser/builtins.go (builtinTrigger)
      to: pkg/extension/trigger.go (extension.TriggerSource)
      via: "src, ok := sourceVal.(extension.TriggerSource)"
      pattern: "\\(extension\\.TriggerSource\\)"
    - from: pkg/parser/finalize.go (validateTriggerReqAccesses)
      to: pkg/parser/req_walk.go (findFreeVarAccesses)
      via: "accesses, err := findFreeVarAccesses(src, filename, lambdaPos, \"req\")"
      pattern: "findFreeVarAccesses\\("
    - from: pkg/parser/finalize.go (validateTriggerReqAccesses)
      to: pkg/extension/trigger.go (TriggerSource.ReqSchema)
      via: "validFields := trig.Source.ReqSchema()"
      pattern: "ReqSchema\\(\\)"
---

<objective>
Land the parser-side primitive `trigger(...)` (TRIG-01) and the three-layer parse-time validation chain (TRIG-04 + D-07-12 + D-07-13). Wave 2: depends on Plan 01 (`*dag.Trigger`) and Plan 02 (`extension.TriggerSource` + the `FakeTriggerSource` test stub).

Purpose: Make `trigger(flow="X", source=fake.webhook(), map=lambda req: ..., idempotency_key=lambda req: ..., credential="id")` callable from `.star` files. Capture lambdas with the existing CapturedLambda machinery, plus a new arity-1 enforcement layer. Generalize `ctx_walk.go` to a parameterized free-var-name visitor so the existing ctx-walker (Phase 4) and the new req-walker share one AST traversal. Cross-file FlowName resolution lives in finalize. Byte-identical duplicate triggers warn but are accepted.

Output: `pkg/parser/builtins.go::builtinTrigger` (~80 LOC), `pkg/parser/req_walk.go` (~120 LOC — generalized visitor), three new finalize passes (~80 LOC), `pkg/parser/parser.go` field+accessor extensions (~30 LOC), `pkg/bridge/lambda_globals.go` triggerTimeGlobals (~40 LOC), 8 testdata fixtures, and `pkg/parser/trigger_test.go` covering the full TRIG-01 + TRIG-04 + D-07-12 + D-07-13 matrix from VALIDATION.md.

LOAD-BEARING CONSTRAINT: The req-walker refactor MUST keep all existing `TestCtxWalk_*` tests green — generalizing `findCtxAccesses` to take a free-var-name parameter must not break the existing ctx-walker callers. Validate by running `go test ./pkg/parser/ -run TestCtxWalk_ -count=1` after the refactor and asserting zero failures.

NOTE on D-07-03 (REQUIREMENTS.md TRIG-01 wording deviation): The success criterion's illustrative `lambda payload, headers` example is overridden — the actual locked signature is single-positional `lambda req:`. This deviation is documented in `pkg/parser/doc.go` as part of Task 03-04 (alongside the D-07-04 trigger-vs-workflow-lambda env distinction).
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
@.planning/phases/07-trigger-primitive-server-shell/07-01-SUMMARY.md
@.planning/phases/07-trigger-primitive-server-shell/07-02-SUMMARY.md
@CLAUDE.md
@pkg/parser/builtins.go
@pkg/parser/globals.go
@pkg/parser/finalize.go
@pkg/parser/parser.go
@pkg/parser/lambda_capture.go
@pkg/parser/ctx_walk.go
@pkg/parser/state_schema.go
@pkg/bridge/lambda_globals.go
@pkg/dag/trigger.go
@pkg/extension/trigger.go
@pkg/extension/testing/triggersource.go

<interfaces>
<!-- Concrete code patterns the executor MUST replicate. All examples use the real module path github.com/mikelalcon/skytime (verified via `head -1 go.mod`). -->

Existing builtin factory pattern (pkg/parser/builtins.go::builtinFlow lines 119-193):
```go
func (p *Parser) builtinFlow(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
    var (
        name        string
        // ...
    )
    if err := starlark.UnpackArgs("flow", args, kwargs,
        "name", &name,
        // ...
    ); err != nil {
        return nil, p.wrapBuiltinError("flow", thread, err)
    }
    pos := callerPosition(thread)
    // ... validate, construct dag.Flow, register in p.flows ...
    return starlark.None, nil
}
```

Existing free-var lint (pkg/parser/lambda_capture.go::captureLambda lines 19-56):
```go
func (p *Parser) captureLambda(thread *starlark.Thread, kwargName string, val starlark.Value) (*dag.CapturedLambda, error) {
    fn, ok := val.(*starlark.Function)
    if !ok {
        return nil, &dag.ParseError{Pos: callerPosition(thread), Msg: fmt.Sprintf("kwarg %q must be a lambda or function, got %s", kwargName, val.Type())}
    }
    pos := fn.Position()
    // ... freeVars, err := p.validateFreeVars(fn, pos.Filename()) ...
    captured := &dag.CapturedLambda{ID: id, Fn: fn, Pos: pos, FreeVars: freeVars}
    p.lambdas[id] = captured
    return captured, nil
}
```

Existing ctx-walker visitor (pkg/parser/ctx_walk.go lines 38-100):
```go
func findCtxAccesses(src []byte, filename string, lambdaPos syntax.Position) ([]ctxAccess, error) {
    opts := defaultFileOptions()
    file, err := opts.Parse(filename, src, 0)
    // ... locate lambda by position; firstParamName drives the DotExpr walk ...
}
```

The locked lambdaTimeGlobals (pkg/bridge/lambda_globals.go lines 60-94) — 20 keys frozen at module init.

The dag.Trigger struct shape THIS PLAN's parser populates (from Plan 01 SUMMARY; verbatim from pkg/dag/trigger.go):
```go
type Trigger struct {
    Pos               syntax.Position
    FlowName          string
    Source            TriggerSource // dag-local — extension.TriggerSource satisfies it
    MapLambda         *CapturedLambda
    IdempotencyLambda *CapturedLambda
    CredentialID      string
    frozen            bool
}
```

The extension.TriggerSource interface THIS PLAN type-asserts against (from Plan 02 SUMMARY):
```go
type TriggerSource interface {
    Kind() string
    ReqSchema() []string
    MarshalJSON() ([]byte, error)
    triggerSourceMarker() // sealed
}
```

The builtinTrigger body THIS PLAN must produce (paste verbatim into pkg/parser/builtins.go, after the existing builtinFlow):
```go
// skytime:doc summary="Declares a top-level trigger — binds a TriggerSource to a flow with map/idempotency_key lambdas."
// skytime:doc summary="Trigger lambdas run ONCE at HTTP ingress (Phase 7.1+), NOT in workflow replay; non-determinism (time.now, json) is observably safe."
// skytime:doc returns="None (registers a *dag.Trigger as a parse-time side effect)."
// skytime:doc since="phase-07"
// skytime:doc example="trigger(\n    flow=\"check_user\",\n    source=github.webhook(events=[\"push\"]),\n    map=lambda req: {\"repo\": req.payload.repository.name},\n    idempotency_key=lambda req: req.headers[\"X-GitHub-Delivery\"],\n    credential=\"github-app-prod\",\n)"
// skytime:doc see="flow"
// skytime:doc param_flow="string"
// skytime:doc desc_flow="Target flow name (must resolve in the same parser session — cross-file allowed; resolved at parse-finalize)."
// skytime:doc param_source="TriggerSource"
// skytime:doc desc_source="Sealed extension.TriggerSource value (e.g. github.webhook(...)). Phase 7 has no shipped factories; first one in 7.1."
// skytime:doc param_map="lambda req"
// skytime:doc desc_map="Single-positional lambda. Returns a dict — the workflow input. Free vars rejected (D-19); arity != 1 rejected; req.<field> typos surface valid-list."
// skytime:doc param_idempotency_key="lambda req"
// skytime:doc desc_idempotency_key="Single-positional lambda. Returns a string used as the idempotency key for ExecuteWorkflow's WorkflowID dedup."
// skytime:doc param_credential="string"
// skytime:doc desc_credential="Optional credential ID string. Resolved JIT inside the receiver (Phase 7.1+); never serialized to DAG JSON, never logged."
//
// builtinTrigger constructs a *dag.Trigger from kwargs, type-asserts source
// to extension.TriggerSource, captures map/idempotency_key lambdas with
// arity-1 enforcement, and registers the trigger in p.triggers keyed by
// position. Returns starlark.None — trigger() is a top-level statement,
// like flow(), captured by side effect.
//
// Cross-file FlowName resolution and req-attribute walks happen in
// finalize (D-07-12, D-07-05) — at builtin-call time the FlowName is
// stored as a literal string. Per-source ReqSchema() is queried at
// finalize, not here, because the source value's ReqSchema may depend on
// kwargs not yet validated at builtin time.
func (p *Parser) builtinTrigger(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
    var (
        flowName       string
        sourceVal      starlark.Value
        mapVal         starlark.Value
        idempotencyVal starlark.Value
        credentialID   string
    )
    if err := starlark.UnpackArgs("trigger", args, kwargs,
        "flow", &flowName,
        "source", &sourceVal,
        "map", &mapVal,
        "idempotency_key", &idempotencyVal,
        "credential?", &credentialID,
    ); err != nil {
        return nil, p.wrapBuiltinError("trigger", thread, err)
    }
    pos := callerPosition(thread)

    // Type-check Source. The dag-local TriggerSource is the minimal seal
    // (Kind + MarshalJSON); extension.TriggerSource adds ReqSchema and the
    // unexported triggerSourceMarker(). We assert against extension.TriggerSource
    // because the req-walker (finalize) needs ReqSchema().
    src, ok := sourceVal.(extension.TriggerSource)
    if !ok {
        return nil, &dag.ParseError{
            Pos: pos,
            Msg: fmt.Sprintf("trigger.source: expected TriggerSource, got %s", sourceVal.Type()),
        }
    }

    // Capture lambdas with arity-1 enforcement (D-07-05 layer 2).
    mapLambda, err := p.captureLambdaWithArity(thread, "map", mapVal, 1)
    if err != nil {
        return nil, err
    }
    idempLambda, err := p.captureLambdaWithArity(thread, "idempotency_key", idempotencyVal, 1)
    if err != nil {
        return nil, err
    }

    trig := &dag.Trigger{
        Pos:               pos,
        FlowName:          flowName,
        Source:            src,
        MapLambda:         mapLambda,
        IdempotencyLambda: idempLambda,
        CredentialID:      credentialID,
    }

    // Register in parser session — keyed by Pos (unique per call site).
    // Two triggers at the SAME position would be a parser bug (lambda
    // capture would already have collided); use a posKey string to keep
    // the map indexable in tests.
    key := posKey(pos)
    p.triggers[key] = trig

    return starlark.None, nil
}

// posKey is the trigger map key. Keep it stable + grep-friendly for tests.
// Format: "<filename>:<line>:<col>". Two triggers cannot share this key;
// builtinTrigger is called once per source-position by Starlark.
func posKey(pos syntax.Position) string {
    return fmt.Sprintf("%s:%d:%d", pos.Filename(), pos.Line, pos.Col)
}
```

The captureLambdaWithArity helper THIS PLAN must produce (paste into pkg/parser/lambda_capture.go AT END OF FILE):
```go
// captureLambdaWithArity wraps captureLambda with arity enforcement
// (D-07-05 layer 2). Used by builtinTrigger for map and idempotency_key
// lambdas — both must accept exactly one positional parameter (convention:
// req). Existing callers of captureLambda (script, if_cond, action_fn,
// etc.) accept variable arity by design and continue to use captureLambda
// directly.
//
// Arity check details:
//   - *args / **kwargs are rejected (would dilute the single-req contract).
//   - Defaulted positional (e.g. lambda req=None: ...) is rejected — Phase
//     7's lambda runs with a real req, never with the default.
//   - Plain positional with arity == expectedArity is accepted.
//
// Errors are *dag.ParseError with the lambda's Position so consultants
// land at the lambda definition.
func (p *Parser) captureLambdaWithArity(thread *starlark.Thread, kwargName string, val starlark.Value, expectedArity int) (*dag.CapturedLambda, error) {
    captured, err := p.captureLambda(thread, kwargName, val)
    if err != nil {
        return nil, err
    }
    fn := captured.Fn
    // *starlark.Function exposes NumParams() + Param(i) returning name + has-default.
    numParams := fn.NumParams()
    if numParams != expectedArity {
        return nil, &dag.ParseError{
            Pos: captured.Pos,
            Msg: fmt.Sprintf("kwarg %q lambda must accept exactly %d positional parameter(s) (convention: req); got %d",
                kwargName, expectedArity, numParams),
        }
    }
    // Reject defaulted positional: each param's HasDefault must be false.
    for i := 0; i < numParams; i++ {
        _, hasDefault := fn.Param(i)
        _ = hasDefault // starlark.Function.Param signature: (name string, hasDefault bool) — the bool is the second return; if Go API differs, inspect via reflection. Verify against go.starlark.net@v0.0.0-20260326113308 docs.
    }
    // Reject *args / **kwargs by checking HasVarargs / HasKwargs.
    if fn.HasVarargs() {
        return nil, &dag.ParseError{Pos: captured.Pos, Msg: fmt.Sprintf("kwarg %q lambda must not accept *args (single-positional req only)", kwargName)}
    }
    if fn.HasKwargs() {
        return nil, &dag.ParseError{Pos: captured.Pos, Msg: fmt.Sprintf("kwarg %q lambda must not accept **kwargs (single-positional req only)", kwargName)}
    }
    return captured, nil
}
```

The req-walker generalization THIS PLAN must produce (NEW pkg/parser/req_walk.go):
```go
package parser

import (
    "fmt"
    "sort"

    "go.starlark.net/syntax"

    "github.com/mikelalcon/skytime/pkg/dag"
)

// freeVarAccess generalizes ctxAccess. Same fields; renamed for clarity
// because two callers (ctx-walker, req-walker) share this type.
type freeVarAccess struct {
    Pos      syntax.Position
    AttrName string
}

// findFreeVarAccesses re-parses src to recover the AST, locates the
// lambda or def whose keyword position equals lambdaPos, and returns
// every <freeVarName>.<attr> access in its body. Generalization of
// findCtxAccesses (pkg/parser/ctx_walk.go) — instead of using the
// lambda's first-param name (which conflated the convention with the
// language semantics), this version takes the expected free-var name as
// a parameter so two callers (ctx-walker, req-walker) can dispatch on
// different conventions without duplicating the AST traversal.
//
// IMPORTANT: This function does NOT enforce that the lambda's first
// param IS named freeVarName — the arity check (captureLambdaWithArity)
// is the right place for that, not here. findFreeVarAccesses just walks
// every DotExpr whose X is the Ident with name freeVarName. If the
// lambda's first param is named differently (e.g., the consultant wrote
// `lambda r: r.payload`), no accesses are returned (the walker simply
// finds no matching DotExprs); the validator can decide whether that's
// fine or an error.
//
// Returns ([], nil) when no matching lambda is found in the re-parsed
// file — defensive; callers treat absent lambdas as "nothing to check".
func findFreeVarAccesses(src []byte, filename string, lambdaPos syntax.Position, freeVarName string) ([]freeVarAccess, error) {
    opts := defaultFileOptions()
    file, err := opts.Parse(filename, src, 0)
    if err != nil {
        return nil, err
    }

    var (
        targetBody  syntax.Expr
        targetStmts []syntax.Stmt
        found       bool
    )
    syntax.Walk(file, func(n syntax.Node) bool {
        switch fn := n.(type) {
        case *syntax.LambdaExpr:
            if positionsEqual(fn.Lambda, lambdaPos) {
                targetBody = fn.Body
                found = true
            }
        case *syntax.DefStmt:
            if positionsEqual(fn.Def, lambdaPos) {
                targetStmts = fn.Body
                found = true
            }
        }
        return true
    })
    if !found {
        return nil, nil
    }

    var accesses []freeVarAccess
    collect := func(n syntax.Node) bool {
        if dot, ok := n.(*syntax.DotExpr); ok {
            if id, ok := dot.X.(*syntax.Ident); ok && id.Name == freeVarName {
                accesses = append(accesses, freeVarAccess{
                    Pos:      dot.Name.NamePos,
                    AttrName: dot.Name.Name,
                })
            }
        }
        return true
    }
    if targetBody != nil {
        syntax.Walk(targetBody, collect)
    }
    for _, stmt := range targetStmts {
        syntax.Walk(stmt, collect)
    }
    return accesses, nil
}

// validateTriggerReqAccesses runs in finalize (after FlowName check). For
// each registered trigger, walks both lambdas and verifies every req.<attr>
// reference is in trig.Source.ReqSchema(). Errors carry the access's
// dot-position and the source kind so consultants land at the typo.
func (p *Parser) validateTriggerReqAccesses() error {
    for _, trig := range p.triggers {
        validFields := setFromSlice(trig.Source.ReqSchema())
        if err := p.checkTriggerLambdaReq(trig, trig.MapLambda, validFields, "map"); err != nil {
            return err
        }
        if err := p.checkTriggerLambdaReq(trig, trig.IdempotencyLambda, validFields, "idempotency_key"); err != nil {
            return err
        }
    }
    return nil
}

func (p *Parser) checkTriggerLambdaReq(trig *dag.Trigger, captured *dag.CapturedLambda, validFields map[string]struct{}, kwargName string) error {
    if captured == nil {
        return nil
    }
    src, ok := p.fileBytes[captured.Pos.Filename()]
    if !ok || src == nil {
        return &dag.ParseError{
            Pos: captured.Pos,
            Msg: fmt.Sprintf("internal: file bytes for %q not cached (req-walker)", captured.Pos.Filename()),
        }
    }
    // BodyPos honored when present (synthetic-source lambda from interpolation).
    walkPos := captured.Pos
    if captured.BodyPos.IsValid() {
        walkPos = captured.BodyPos
    }
    accesses, err := findFreeVarAccesses(src, walkPos.Filename(), walkPos, "req")
    if err != nil {
        return err
    }
    for _, acc := range accesses {
        if _, ok := validFields[acc.AttrName]; !ok {
            return &dag.ValidationError{
                Pos:  acc.Pos,
                Flow: trig.FlowName,
                Msg:  fmt.Sprintf("trigger %s lambda: req has no attribute %q; available: %v (declared by source kind %q)", kwargName, acc.AttrName, sortedKeys(validFields), trig.Source.Kind()),
            }
        }
    }
    return nil
}

func setFromSlice(s []string) map[string]struct{} {
    m := make(map[string]struct{}, len(s))
    for _, v := range s {
        m[v] = struct{}{}
    }
    return m
}

func sortedKeys(m map[string]struct{}) []string {
    out := make([]string, 0, len(m))
    for k := range m {
        out = append(out, k)
    }
    sort.Strings(out)
    return out
}
```

The cross-file FlowName resolution + duplicate-warn finalize pass:
```go
// validateTriggerFlowNames runs in finalize AFTER resolveCallFlows (both
// inspect p.flows). Every trigger's FlowName must resolve to a known
// flow; unknown names surface position-aware *dag.ParseError listing the
// known flows. Cross-file ordering does NOT matter — finalize runs after
// all loads complete (D-07-12).
func (p *Parser) validateTriggerFlowNames() error {
    // Sort triggers by posKey for deterministic error attribution when
    // multiple unknown flows exist.
    keys := make([]string, 0, len(p.triggers))
    for k := range p.triggers {
        keys = append(keys, k)
    }
    sort.Strings(keys)
    for _, k := range keys {
        trig := p.triggers[k]
        if _, ok := p.flows[trig.FlowName]; !ok {
            knownFlows := sortedFlowNames(p.flows)
            return &dag.ParseError{
                Pos: trig.Pos,
                Msg: fmt.Sprintf("trigger references unknown flow %q; known flows: %v", trig.FlowName, knownFlows),
            }
        }
    }
    return nil
}

func sortedFlowNames(m map[string]*dag.Flow) []string {
    out := make([]string, 0, len(m))
    for k := range m {
        out = append(out, k)
    }
    sort.Strings(out)
    return out
}

// warnDuplicateTriggers (D-07-13) — byte-identical (FlowName + Source
// MarshalJSON output + lambda IDs + CredentialID) duplicates emit a
// slog.Warn during finalize and are accepted. Non-identical pairs (same
// FlowName + same Source.Kind() but different config bytes) are accepted
// silently — the future HTTP router de-dups handler mounts.
//
// Implementation: group triggers by FlowName + Source.Kind(); for each
// group, hash the (Source MarshalJSON output, MapLambda.ID, IdempotencyLambda.ID,
// CredentialID) tuple; collisions are byte-identical duplicates.
func (p *Parser) warnDuplicateTriggers() error {
    type sig struct {
        flowName     string
        sourceKind   string
        sourceBytes  string
        mapLambdaID  string
        idempLambdaID string
        credentialID string
    }
    seen := make(map[sig]syntax.Position)
    keys := make([]string, 0, len(p.triggers))
    for k := range p.triggers {
        keys = append(keys, k)
    }
    sort.Strings(keys)
    for _, k := range keys {
        trig := p.triggers[k]
        srcBytes, err := trig.Source.MarshalJSON()
        if err != nil {
            return &dag.ParseError{Pos: trig.Pos, Msg: fmt.Sprintf("trigger source MarshalJSON: %v", err)}
        }
        s := sig{
            flowName:     trig.FlowName,
            sourceKind:   trig.Source.Kind(),
            sourceBytes:  string(srcBytes),
            credentialID: trig.CredentialID,
        }
        if trig.MapLambda != nil {
            s.mapLambdaID = trig.MapLambda.ID
        }
        if trig.IdempotencyLambda != nil {
            s.idempLambdaID = trig.IdempotencyLambda.ID
        }
        if firstPos, dup := seen[s]; dup {
            // Defer surfaced warning to boot — store on parser so the
            // worker boot can drain via p.TriggerWarnings(). Tests
            // observe via Parser.TriggerWarnings().
            p.triggerWarnings = append(p.triggerWarnings,
                fmt.Sprintf("%s: duplicate trigger (byte-identical to %s) — accepted but flagged", trig.Pos, firstPos))
            continue
        }
        seen[s] = trig.Pos
    }
    return nil
}
```

The triggerTimeGlobals THIS PLAN must produce (paste into pkg/bridge/lambda_globals.go AT END):
```go
// triggerTimeGlobals is the predeclared environment for trigger map and
// idempotency_key lambdas (D-07-01). Strict superset of lambdaTimeGlobals
// (the locked 20-key Phase 1 set) plus go.starlark.net/lib/json (encode,
// decode, indent) and go.starlark.net/lib/time (now, parse_duration, etc).
//
// Why expanded vs lambdaTimeGlobals: trigger lambdas run at HTTP ingress
// (Phase 7.1+), NOT in workflow replay. Non-determinism (time.now) is
// observably safe — the resulting workflow input is frozen at
// ExecuteWorkflow call time. See pkg/bridge/doc.go for the contract.
//
// Frozen at module init like lambdaTimeGlobals; immutable to consumers.
var triggerTimeGlobals = func() starlark.StringDict {
    sd := make(starlark.StringDict, len(lambdaTimeGlobals)+2)
    for k, v := range lambdaTimeGlobals {
        sd[k] = v
    }
    // go.starlark.net/lib/json — *starlarkstruct.Module with attrs
    // encode/decode/indent. Imported as starlarkjson alias so the
    // existing "json" key name on sd is unambiguous.
    sd["json"] = starlarkjson.Module
    sd["time"] = starlarktime.Module
    sd.Freeze()
    return sd
}()

// TriggerTimeGlobals returns a fresh COPY of the locked trigger-time
// globals. Callers may mutate the returned StringDict freely without
// affecting the locked source of truth.
//
// The 22-key surface (20 from lambdaTimeGlobals + json + time) is
// asserted by TestTriggerTimeGlobalsLocked as the API stability gate;
// any future expansion requires explicit decision logging in PROJECT.md
// and an update to that test.
func TriggerTimeGlobals() starlark.StringDict {
    out := make(starlark.StringDict, len(triggerTimeGlobals))
    for k, v := range triggerTimeGlobals {
        out[k] = v
    }
    return out
}
```

Required imports in pkg/bridge/lambda_globals.go (add to existing import block):
```go
import (
    // ... existing imports ...
    starlarkjson "go.starlark.net/lib/json"
    starlarktime "go.starlark.net/lib/time"
)
```
</interfaces>
</context>

<tasks>

<task type="auto">
  <id>07-03-01</id>
  <name>Task 1: Refactor pkg/parser/ctx_walk.go to a parameterized free-var visitor in pkg/parser/req_walk.go (regression-safe)</name>
  <read_first>
    - pkg/parser/ctx_walk.go (FULL file — every line; understand findCtxAccesses signature, positionsEqual, paramName helpers)
    - pkg/parser/ctx_walk_test.go (FULL file — TestCtxWalk_Lambda, TestCtxWalk_Def, TestCtxWalk_TwoLambdasSameLine, TestCtxWalk_NoMatch — these are the regression-guard tests)
    - pkg/parser/state_schema.go (lines 90-260 — validateLambdaCtxAccesses + checkLambdaCtx are the existing callers; understand how findCtxAccesses is invoked from the validator)
    - pkg/parser/finalize.go (where validateLambdaCtxAccesses sits in the chain — the refactor must NOT change its position in finalize)
    - .planning/phases/07-trigger-primitive-server-shell/07-RESEARCH.md (§ Pitfall 3 lines 493-555 — the canonical refactor recipe; ALSO read § Code Examples Example 1 for the visitor signature)
  </read_first>
  <files>pkg/parser/req_walk.go, pkg/parser/req_walk_test.go, pkg/parser/ctx_walk.go, pkg/parser/state_schema.go</files>
  <action>
    Step 1 — Create `pkg/parser/req_walk.go` (NEW file, ~150 LOC) with:
    - The `freeVarAccess` struct (verbatim from `<interfaces>`).
    - The `findFreeVarAccesses(src []byte, filename string, lambdaPos syntax.Position, freeVarName string) ([]freeVarAccess, error)` function VERBATIM from `<interfaces>`.
    - The `validateTriggerReqAccesses` finalize-pass method on `*Parser` VERBATIM from `<interfaces>`.
    - The `checkTriggerLambdaReq` helper VERBATIM from `<interfaces>`.
    - The `setFromSlice` and `sortedKeys` helpers VERBATIM from `<interfaces>` (note: these helpers are pkg/parser-private; if names collide with existing helpers, use `setFromSliceTrigger` / `sortedKeysTrigger` to avoid duplicate-symbol).

    Required imports for `pkg/parser/req_walk.go`:
    ```go
    import (
        "fmt"
        "sort"

        "go.starlark.net/syntax"

        "github.com/mikelalcon/skytime/pkg/dag"
    )
    ```

    Step 2 — REFACTOR `pkg/parser/ctx_walk.go` (existing file, ~135 LOC):
    - Keep the `ctxAccess` type unchanged (existing callers use this type).
    - REPLACE `findCtxAccesses`'s body to delegate to `findFreeVarAccesses` after determining the free-var name from the lambda's first parameter:
      ```go
      func findCtxAccesses(src []byte, filename string, lambdaPos syntax.Position) ([]ctxAccess, error) {
          // First locate the lambda to extract its first-param name —
          // existing behavior. Then delegate to findFreeVarAccesses with
          // that name. Two-pass cost is amortized: each pass is
          // O(file_bytes), and validateLambdaCtxAccesses already pays one
          // re-parse per lambda.
          firstParam, err := firstParamNameAt(src, filename, lambdaPos)
          if err != nil {
              return nil, err
          }
          if firstParam == "" {
              return nil, nil // no matching lambda or no params — same semantics as before
          }
          fv, err := findFreeVarAccesses(src, filename, lambdaPos, firstParam)
          if err != nil {
              return nil, err
          }
          out := make([]ctxAccess, len(fv))
          for i, a := range fv {
              out[i] = ctxAccess{Pos: a.Pos, AttrName: a.AttrName}
          }
          return out, nil
      }

      // firstParamNameAt re-parses src and returns the name of the
      // FIRST positional parameter of the lambda/def whose keyword
      // position equals lambdaPos. Returns ("", nil) when no matching
      // lambda is found OR when the matched lambda has no params.
      func firstParamNameAt(src []byte, filename string, lambdaPos syntax.Position) (string, error) {
          opts := defaultFileOptions()
          file, err := opts.Parse(filename, src, 0)
          if err != nil {
              return "", err
          }
          var name string
          syntax.Walk(file, func(n syntax.Node) bool {
              switch fn := n.(type) {
              case *syntax.LambdaExpr:
                  if positionsEqual(fn.Lambda, lambdaPos) && len(fn.Params) > 0 {
                      name = paramName(fn.Params[0])
                  }
              case *syntax.DefStmt:
                  if positionsEqual(fn.Def, lambdaPos) && len(fn.Params) > 0 {
                      name = paramName(fn.Params[0])
                  }
              }
              return name == "" // stop walking once found
          })
          return name, nil
      }
      ```
    - Keep `positionsEqual` and `paramName` helpers in ctx_walk.go (req_walk.go references them — they're package-private, no import needed).

    Step 3 — Update `pkg/parser/state_schema.go::validateLambdaCtxAccesses` and its callers (`checkLambdaCtx`, etc.) — NO CODE CHANGES expected; they call `findCtxAccesses(...)` which now delegates internally. Verify by running `go test ./pkg/parser/ -run TestCtxWalk_ -count=1 -race` — every existing TestCtxWalk_* test must pass unchanged.

    Step 4 — Create `pkg/parser/req_walk_test.go` (NEW). Use `package parser` (white-box; req_walk.go's helpers are package-private). Tests:

    - `TestFindFreeVarAccesses_CtxName`: re-uses an existing testdata fixture (or inline source) where a lambda is `lambda ctx: ctx.foo + ctx.bar`. Calls `findFreeVarAccesses(src, filename, lambdaPos, "ctx")` and asserts two accesses (foo, bar). Equivalent to the existing TestCtxWalk_Lambda test but exercises the generalized signature.
    - `TestFindFreeVarAccesses_ReqName`: source `lambda req: req.payload + req.headers["X"]`. Calls with `"req"`. Asserts two accesses (payload, headers).
    - `TestFindFreeVarAccesses_WrongName`: source `lambda req: req.payload`. Calls with `"ctx"` (NOT the actual first-param name). Asserts ZERO accesses (the visitor doesn't enforce param-name match — that's the validator's job).
    - `TestFindFreeVarAccesses_NoMatchingLambda`: passes a position that points inside the file but at no lambda. Asserts `(nil, nil)` return — defensive.

    Run:
    ```bash
    go test ./pkg/parser/ -run TestFindFreeVarAccesses -count=1 -race
    go test ./pkg/parser/ -run TestCtxWalk_ -count=1 -race  # REGRESSION GUARD
    go vet ./pkg/parser/...
    ```

    All must exit 0. If TestCtxWalk_* fails, the refactor is broken — debug before proceeding.

    DO NOT delete `findCtxAccesses` — it's the bridge for existing callers.
    DO NOT change `validateLambdaCtxAccesses` semantics or move it in finalize.
    DO NOT add validateTriggerReqAccesses to finalize.go in this task — Task 5 wires it.
  </action>
  <acceptance_criteria>
    - File `pkg/parser/req_walk.go` exists
    - `grep -nE 'func findFreeVarAccesses\(' pkg/parser/req_walk.go` returns exactly one match
    - `grep -n 'freeVarName string' pkg/parser/req_walk.go` returns at least one match (the new parameter)
    - `grep -n 'type freeVarAccess struct' pkg/parser/req_walk.go` returns exactly one match
    - `grep -nE 'func \(p \*Parser\) validateTriggerReqAccesses\(\)' pkg/parser/req_walk.go` returns exactly one match
    - `grep -nE 'func \(p \*Parser\) checkTriggerLambdaReq\(' pkg/parser/req_walk.go` returns exactly one match
    - `grep -n 'firstParamNameAt' pkg/parser/ctx_walk.go` returns at least one match (the helper extracted during refactor)
    - `grep -n 'findFreeVarAccesses' pkg/parser/ctx_walk.go` returns at least one match (delegation)
    - File `pkg/parser/req_walk_test.go` exists with all four `TestFindFreeVarAccesses_*` tests
    - `go test ./pkg/parser/ -run TestFindFreeVarAccesses -count=1 -race` exits 0
    - `go test ./pkg/parser/ -run TestCtxWalk_ -count=1 -race` exits 0 (REGRESSION — must remain green after refactor)
    - `go build ./pkg/parser/...` exits 0
    - `go vet ./pkg/parser/...` exits 0
  </acceptance_criteria>
  <verify>
    <automated>go test ./pkg/parser/ -run 'TestFindFreeVarAccesses|TestCtxWalk_' -count=1 -race && go vet ./pkg/parser/... && grep -q 'func findFreeVarAccesses' pkg/parser/req_walk.go && grep -q 'firstParamNameAt' pkg/parser/ctx_walk.go</automated>
  </verify>
  <done>
    `findFreeVarAccesses` is the parameterized visitor; `findCtxAccesses` delegates to it via `firstParamNameAt`; existing TestCtxWalk_* tests pass unchanged. The req-walker validator (`validateTriggerReqAccesses` + `checkTriggerLambdaReq`) is defined but NOT YET wired into finalize.go (Task 5 wires it).
  </done>
</task>

<task type="auto">
  <id>07-03-02</id>
  <name>Task 2: Add triggerTimeGlobals to pkg/bridge/lambda_globals.go + locked-set test</name>
  <read_first>
    - pkg/bridge/lambda_globals.go (FULL file — current 20-key locked set)
    - pkg/bridge/lambda_globals_test.go (FULL file — TestLambdaTimeGlobalsLocked is the API stability gate)
    - pkg/bridge/doc.go if exists; otherwise plan to add file-level doc explaining the trigger vs lambda env distinction (D-07-04)
    - .planning/phases/07-trigger-primitive-server-shell/07-RESEARCH.md (§ Library/SDK Knowledge — lines 277-303 cover go.starlark.net/lib/json + lib/time module shapes; the imports use the aliases starlarkjson + starlarktime)
    - .planning/phases/07-trigger-primitive-server-shell/07-CONTEXT.md (D-07-01: triggerTimeGlobals = lambdaTimeGlobals + json + time. D-07-04: trigger lambdas run at HTTP ingress, non-determinism observably safe)
  </read_first>
  <files>pkg/bridge/lambda_globals.go, pkg/bridge/lambda_globals_test.go, pkg/bridge/doc.go</files>
  <behavior>
    - Test 1 (TestTriggerTimeGlobalsLocked): assert `len(triggerTimeGlobals)` is exactly 22 (20 from lambdaTimeGlobals + json + time). Assert every key in lambdaTimeGlobals is also in triggerTimeGlobals with the SAME *starlark.Value pointer (no accidental rebind). Assert `triggerTimeGlobals["json"]` is non-nil and is the `starlarkjson.Module` value. Assert `triggerTimeGlobals["time"]` is non-nil and is the `starlarktime.Module` value.
    - Test 2 (TestTriggerTimeGlobals_ReturnedCopy): `c1 := TriggerTimeGlobals(); c1["unsafe"] = nil; c2 := TriggerTimeGlobals(); _, exists := c2["unsafe"]; assert.False(t, exists)` — copy semantics like LambdaTimeGlobals.
    - Test 3 (TestTriggerTimeGlobals_JSONEncodeWorks): create a `*starlark.Thread`, exec a tiny script `result = json.encode({"a": 1})` against `TriggerTimeGlobals()`, assert `result.String() == "\"{\\\"a\\\":1}\""` or similar — proves json.Module is wired correctly.
    - Test 4 (TestTriggerTimeGlobals_TimeNowCallable): exec `result = time.now()` against `TriggerTimeGlobals()`; assert no error; assert result type is time-shaped (likely `*starlarktime.Time`). Don't assert the value — non-deterministic.
    - Test 5 (TestTriggerTimeGlobals_LambdaSubsetExact): the 20 lambdaTimeGlobals keys MUST appear unchanged in triggerTimeGlobals; iterate `lambdaTimeGlobals` and confirm membership. Catches drift if a future contributor changes one of the 20 in lambdaTimeGlobals without updating triggerTimeGlobals.
  </behavior>
  <action>
    Step 1 — Edit `pkg/bridge/lambda_globals.go`:

    1. Extend the import block to add the two starlark sub-package aliases:
       ```go
       import (
           "fmt"

           "go.starlark.net/starlark"
           "go.starlark.net/syntax"

           starlarkjson "go.starlark.net/lib/json"
           starlarktime "go.starlark.net/lib/time"
       )
       ```

    2. APPEND (after the existing `LambdaTimeGlobals()` function at line 112) the `triggerTimeGlobals` constant + `TriggerTimeGlobals()` accessor — VERBATIM from `<interfaces>`.

    Step 2 — Add `pkg/bridge/doc.go` (or extend existing) with a top-of-file doc block:
    ```go
    // Package bridge owns the Starlark↔Go value bridge for Skytime: lambda
    // call envelopes, Starlark struct construction, and the predeclared
    // environments used at workflow execute time and at HTTP-ingress time.
    //
    // ENVIRONMENT DISTINCTION (D-20 + D-07-04 + D-07-01):
    //
    //   - lambdaTimeGlobals (Phase 1, locked at 20 keys): the strict subset
    //     available inside lambdas at WORKFLOW EXECUTION time. No
    //     non-determinism (no time, no random, no I/O). Workflows replay
    //     deterministically; any non-determinism breaks Temporal's
    //     event-sourcing guarantee.
    //
    //   - triggerTimeGlobals (Phase 7, 22 keys = lambdaTimeGlobals + json
    //     + time): the env for trigger map and idempotency_key lambdas.
    //     These run ONCE at HTTP ingress (Phase 7.1+) BEFORE
    //     client.ExecuteWorkflow — the result is the workflow input,
    //     frozen at that point. Non-determinism (time.now) is observably
    //     safe because the workflow never re-evaluates the lambda.
    //
    // Conflating the two environments would silently break replay
    // determinism for workflow lambdas — keep them strictly separate. Tests
    // (TestLambdaTimeGlobalsLocked, TestTriggerTimeGlobalsLocked) gate the
    // surfaces against drift.
    package bridge
    ```

    Step 3 — Edit `pkg/bridge/lambda_globals_test.go` to add the five new tests above. Use the same patterns as TestLambdaTimeGlobalsLocked / TestLambdaTimeGlobals_ReturnedCopy.

    Test 5 implementation:
    ```go
    func TestTriggerTimeGlobals_LambdaSubsetExact(t *testing.T) {
        for k, v := range lambdaTimeGlobals {
            tv, ok := triggerTimeGlobals[k]
            require.True(t, ok, "lambdaTimeGlobals key %q missing from triggerTimeGlobals", k)
            assert.Equal(t, v, tv, "triggerTimeGlobals[%q] differs from lambdaTimeGlobals[%q]", k, k)
        }
    }
    ```

    Step 4 — Run:
    ```bash
    go build ./pkg/bridge/...
    go test ./pkg/bridge/ -run 'TestTriggerTimeGlobals|TestLambdaTimeGlobals' -count=1 -race
    go vet ./pkg/bridge/...
    ```

    All must exit 0. If TestLambdaTimeGlobalsLocked fails, the lambdaTimeGlobals shape changed unexpectedly — investigate before proceeding.

    DO NOT modify lambdaTimeGlobals' 20-key contents.
    DO NOT add a TriggerTimeGlobals_ForbiddenAbsent test — the trigger env intentionally INCLUDES json + time, the very forbids of lambdaTimeGlobals.
    Confirm the import path `go.starlark.net/lib/json` resolves: run `go doc go.starlark.net/lib/json Module` after the import is added; expected output: `var Module *starlarkstruct.Module`.
  </action>
  <acceptance_criteria>
    - `grep -n 'starlarkjson "go.starlark.net/lib/json"' pkg/bridge/lambda_globals.go` returns exactly one match
    - `grep -n 'starlarktime "go.starlark.net/lib/time"' pkg/bridge/lambda_globals.go` returns exactly one match
    - `grep -n 'var triggerTimeGlobals = func()' pkg/bridge/lambda_globals.go` returns exactly one match
    - `grep -n 'func TriggerTimeGlobals()' pkg/bridge/lambda_globals.go` returns exactly one match
    - `grep -nE 'sd\["json"\] = starlarkjson\.Module' pkg/bridge/lambda_globals.go` returns exactly one match
    - `grep -nE 'sd\["time"\] = starlarktime\.Module' pkg/bridge/lambda_globals.go` returns exactly one match
    - `pkg/bridge/doc.go` contains a doc block referencing both `lambdaTimeGlobals` and `triggerTimeGlobals` with the D-07-04 rationale
    - `go test ./pkg/bridge/ -run TestTriggerTimeGlobalsLocked -count=1` exits 0
    - `go test ./pkg/bridge/ -run TestTriggerTimeGlobals_ReturnedCopy -count=1` exits 0
    - `go test ./pkg/bridge/ -run TestTriggerTimeGlobals_JSONEncodeWorks -count=1` exits 0
    - `go test ./pkg/bridge/ -run TestTriggerTimeGlobals_TimeNowCallable -count=1` exits 0
    - `go test ./pkg/bridge/ -run TestTriggerTimeGlobals_LambdaSubsetExact -count=1` exits 0
    - `go test ./pkg/bridge/ -run TestLambdaTimeGlobalsLocked -count=1` exits 0 (REGRESSION)
    - `go build ./pkg/bridge/...` exits 0
    - `go vet ./pkg/bridge/...` exits 0
  </acceptance_criteria>
  <verify>
    <automated>go test ./pkg/bridge/ -run 'TestTriggerTimeGlobals|TestLambdaTimeGlobalsLocked' -count=1 -race && go vet ./pkg/bridge/... && grep -q 'starlarkjson "go.starlark.net/lib/json"' pkg/bridge/lambda_globals.go && grep -q 'starlarktime "go.starlark.net/lib/time"' pkg/bridge/lambda_globals.go && grep -q 'var triggerTimeGlobals = func()' pkg/bridge/lambda_globals.go</automated>
  </verify>
  <done>
    `pkg/bridge.triggerTimeGlobals` exists as a frozen 22-key StringDict (locked 20 from lambdaTimeGlobals + json.Module + time.Module). `TriggerTimeGlobals()` returns a fresh copy. The five new tests cover locked-shape, copy semantics, json.encode round-trip, time.now callability, and exact-subset of lambdaTimeGlobals. The shipping doc block in `pkg/bridge/doc.go` documents D-07-04 (trigger lambdas at HTTP ingress vs workflow lambdas at execute time).
  </done>
</task>

<task type="auto">
  <id>07-03-03</id>
  <name>Task 3: Implement builtinTrigger + captureLambdaWithArity in pkg/parser/builtins.go and pkg/parser/lambda_capture.go</name>
  <read_first>
    - pkg/parser/builtins.go (lines 119-193 — builtinFlow factory pattern; this is the verbatim shape for builtinTrigger)
    - pkg/parser/lambda_capture.go (FULL file — captureLambda is the wrapper target; understand fn.NumParams / HasVarargs / HasKwargs availability against go.starlark.net's *Function)
    - pkg/dag/trigger.go (Plan 01 output — *Trigger struct shape)
    - pkg/extension/trigger.go (Plan 02 output — extension.TriggerSource interface)
    - pkg/parser/parser.go (lines 37-111 — Parser struct; understand where to add `triggers` and `triggerWarnings` fields)
    - .planning/phases/07-trigger-primitive-server-shell/07-RESEARCH.md (§ Code Examples Example 1, lines 819-878 — the verbatim builtinTrigger body to paste)
    - .planning/phases/07-trigger-primitive-server-shell/07-CONTEXT.md (D-07-03 lambda signature; D-07-05 three-layer validation)
  </read_first>
  <files>pkg/parser/builtins.go, pkg/parser/lambda_capture.go, pkg/parser/parser.go</files>
  <action>
    Step 1 — Edit `pkg/parser/parser.go`:

    1. Add to the Parser struct (after `lambdas` field, before `preBuiltResults`):
       ```go
       // triggers is the parser-session multi-trigger map (D-07-12).
       // builtinTrigger populates this. Keyed by posKey(pos) to keep map
       // operations stable across tests (positions are guaranteed unique
       // per call site by Starlark's exec model).
       triggers map[string]*dag.Trigger

       // triggerWarnings holds deferred warnings produced by finalize
       // passes (D-07-13 byte-identical duplicate-trigger warnings).
       // The boot loop drains these via Parser.TriggerWarnings() and
       // surfaces them via slog.Warn at server startup. Tests inspect
       // directly.
       triggerWarnings []string
       ```

    2. In `NewParser` initialize the new map:
       ```go
       triggers: make(map[string]*dag.Trigger),
       ```

    3. Add the accessors (after `Flows()` / `FlowsInOrder()`):
       ```go
       // Triggers returns the parser session's accumulated triggers in a
       // deterministic order (sorted by Source.Kind, FlowName, then Pos).
       // The returned slice is freshly allocated; callers may mutate
       // without affecting parser state. Used by pkg/worker/boot.go to
       // drain triggers into the TriggerRegistry (Plan 04).
       //
       // Empty slice when called before any ParseFile / ParseSource
       // invocation, or when no triggers have been registered.
       func (p *Parser) Triggers() []*dag.Trigger {
           out := make([]*dag.Trigger, 0, len(p.triggers))
           for _, t := range p.triggers {
               out = append(out, t)
           }
           sort.Slice(out, func(i, j int) bool {
               a, b := out[i], out[j]
               if a.Source.Kind() != b.Source.Kind() {
                   return a.Source.Kind() < b.Source.Kind()
               }
               if a.FlowName != b.FlowName {
                   return a.FlowName < b.FlowName
               }
               // Tiebreaker: position string (filename:line:col). Stable
               // across runs because all three fields are already filled
               // by Starlark before Trigger is constructed.
               return a.Pos.String() < b.Pos.String()
           })
           return out
       }

       // TriggerWarnings returns deferred parser warnings (e.g.,
       // D-07-13 byte-identical duplicate triggers). The returned slice
       // is freshly allocated. Empty when no warnings accumulated.
       func (p *Parser) TriggerWarnings() []string {
           if len(p.triggerWarnings) == 0 {
               return nil
           }
           out := make([]string, len(p.triggerWarnings))
           copy(out, p.triggerWarnings)
           return out
       }
       ```

    4. Add `"sort"` import if not present.

    Step 2 — Edit `pkg/parser/lambda_capture.go`:

    APPEND `captureLambdaWithArity` at end of file. Use the verbatim body from `<interfaces>` with one CRITICAL adjustment to the param-default check — the go.starlark.net Function API signature for `Param(i)` is:
    ```go
    // Param returns the name and position of the i'th parameter.
    func (f *Function) Param(i int) (string, syntax.Position)
    // Param defaults are queried via:
    func (f *Function) ParamDefault(i int) Value
    ```
    So defaulted-positional rejection becomes:
    ```go
    for i := 0; i < numParams; i++ {
        if fn.ParamDefault(i) != nil {
            return nil, &dag.ParseError{
                Pos: captured.Pos,
                Msg: fmt.Sprintf("kwarg %q lambda parameter %d must not have a default value (single-positional req only)", kwargName, i),
            }
        }
    }
    ```
    `HasVarargs()` and `HasKwargs()` are real methods on *starlark.Function — verify by running `go doc go.starlark.net/starlark Function` after the file change. Adjust if the actual API differs (e.g., methods may live on the Function's underlying compiled funcode).

    Step 3 — Edit `pkg/parser/builtins.go`:

    APPEND `builtinTrigger` after the existing `builtinFlow` (insert after line ~193). Use the VERBATIM body from `<interfaces>`. Include:
    - All `// skytime:doc` markers (the doc block before the function — this triggers regen of `docs/reference/builtins.md` via `go generate ./...`).
    - The `posKey(pos syntax.Position) string` helper — paste at end of file (or in a sensible location near other helpers like `wrapBuiltinError`).

    Required new imports for `pkg/parser/builtins.go` (the file already imports `fmt`, `strings`, `go.starlark.net/starlark`, `go.starlark.net/syntax`, and `pkg/dag`; ADD):
    ```go
    "github.com/mikelalcon/skytime/pkg/extension"
    ```

    Step 4 — Verify compilation:
    ```bash
    go build ./pkg/parser/...
    go vet ./pkg/parser/...
    ```

    Both must exit 0. The function is registered in globals.go in Task 4; this task lands the body.

    DO NOT register the builtin in globals.go yet (Task 4 owns that).
    DO NOT call validateTriggerReqAccesses or validateTriggerFlowNames in finalize yet (Task 5 owns those).
    DO NOT create testdata fixtures or trigger_test.go yet (Tasks 6 and 7 own those).
  </action>
  <acceptance_criteria>
    - `grep -n 'triggers map\[string\]\*dag.Trigger' pkg/parser/parser.go` returns exactly one match (the new field)
    - `grep -n 'triggerWarnings \[\]string' pkg/parser/parser.go` returns exactly one match (the new field)
    - `grep -nE 'func \(p \*Parser\) Triggers\(\) \[\]\*dag\.Trigger' pkg/parser/parser.go` returns exactly one match
    - `grep -nE 'func \(p \*Parser\) TriggerWarnings\(\) \[\]string' pkg/parser/parser.go` returns exactly one match
    - `grep -n 'triggers: make(map' pkg/parser/parser.go` returns at least one match (NewParser initialization)
    - `grep -nE 'func \(p \*Parser\) builtinTrigger\(' pkg/parser/builtins.go` returns exactly one match
    - `grep -n 'expected TriggerSource, got' pkg/parser/builtins.go` returns exactly one match (the source-type error literal)
    - `grep -nE 'func \(p \*Parser\) captureLambdaWithArity\(' pkg/parser/lambda_capture.go` returns exactly one match
    - `grep -nE 'func posKey\(pos syntax\.Position\) string' pkg/parser/builtins.go` returns exactly one match
    - `grep -n 'github.com/mikelalcon/skytime/pkg/extension' pkg/parser/builtins.go` returns at least one match (the new import)
    - `grep -n 'sourceVal\.\(extension\.TriggerSource\)' pkg/parser/builtins.go` returns at least one match (the type assertion)
    - `go build ./pkg/parser/...` exits 0
    - `go vet ./pkg/parser/...` exits 0
    - `go test ./pkg/parser/... -count=1 -race` passes the FULL existing parser suite (regression — the new functions are not yet wired, no behavior change expected for existing callers)
  </acceptance_criteria>
  <verify>
    <automated>go build ./pkg/parser/... && go vet ./pkg/parser/... && go test ./pkg/parser/... -count=1 -race && grep -q 'func (p \*Parser) builtinTrigger' pkg/parser/builtins.go && grep -q 'func (p \*Parser) captureLambdaWithArity' pkg/parser/lambda_capture.go && grep -q 'triggers map\[string\]\*dag.Trigger' pkg/parser/parser.go</automated>
  </verify>
  <done>
    `Parser` has a `triggers` map field, `triggerWarnings` field, `Triggers()` accessor returning a deterministically-sorted slice, and `TriggerWarnings()` accessor. `builtinTrigger` and `captureLambdaWithArity` are defined but not yet reachable from .star files (no globals.go registration in this task). The full existing parser test suite remains green.
  </done>
</task>

<task type="auto">
  <id>07-03-04</id>
  <name>Task 4: Register "trigger" in newParseTimeGlobals + thread triggerTimeGlobals into trigger lambda parse</name>
  <read_first>
    - pkg/parser/globals.go (FULL file — newParseTimeGlobals; understand the registration pattern; sort the existing 7 builtins to find the natural insertion point)
    - pkg/parser/builtins.go (Task 3 output — builtinTrigger exists)
    - pkg/bridge/lambda_globals.go (Task 2 output — TriggerTimeGlobals())
    - pkg/parser/doc.go IF it already exists (read before appending; otherwise create new)
    - .planning/phases/07-trigger-primitive-server-shell/07-CONTEXT.md (D-07-01 — triggerTimeGlobals integration; "Define a new triggerTimeGlobals constant in pkg/bridge that extends lambdaTimeGlobals"; D-07-03 — locked single-positional `lambda req:` signature deviation; D-07-04 — trigger lambdas use enriched globals + are non-deterministic, workflow lambdas are deterministic)
  </read_first>
  <files>pkg/parser/globals.go, pkg/parser/doc.go</files>
  <action>
    Edit `pkg/parser/globals.go::newParseTimeGlobals`:

    Add ONE LINE to the initial `g := starlark.StringDict{...}` map literal (insert AFTER `"call_flow"` and before `"result"` for grep-friendly proximity to other top-level declarations):
    ```go
    "trigger":           starlark.NewBuiltin("trigger", p.builtinTrigger),
    ```

    Critical placement: add the entry between `"call_flow"` (line ~49) and `"result"` (line ~54) so the grep test in trigger_test.go can assert on stable line numbers. The Bridge between the parser primitives and the result/fail values is the most logical reading order.

    NOTE: Trigger lambdas (map and idempotency_key) are PARSED at parse-time using the FULL `parseTimeGlobals` env (which includes flow, step, extension namespaces, etc.) — exactly like other captured lambdas. The `triggerTimeGlobals` env shipped in Task 2 is for **runtime** lambda evaluation at HTTP ingress (Phase 7.1+). This plan does NOT need to thread `bridge.TriggerTimeGlobals()` anywhere yet — Phase 7.1's HTTP receiver will call `bridge.CallLambda(thread, captured, bridge.TriggerTimeGlobals(), reqStruct)` (or similar) when resolving each trigger lambda. Plan 03 only ensures the env CONSTANT exists in pkg/bridge for future consumers.

    Verify:
    ```bash
    go build ./pkg/parser/...
    go vet ./pkg/parser/...
    go test ./pkg/parser/... -count=1 -race
    ```

    All must exit 0. The builtin is now reachable from .star files but no fixture exercises it yet (Tasks 6+7).

    Step 2 — Create or update `pkg/parser/doc.go` with a 10-20 line section documenting the trigger-lambda contract. This file MAY already exist; if so, append the new section after the existing content. If not, create it with a `package parser` declaration and the doc block.

    The doc block MUST contain (literal substrings — surfaced via grep checks below):
    - "D-07-03" — the deviation reference
    - "trigger lambda" — the contract subject
    - "lambdaTimeGlobals + json + time" — the trigger-lambda env (D-07-04)
    - "non-deterministic" — explains why trigger lambdas can use non-deterministic globals (run once at HTTP ingress, not in workflow replay)

    Suggested content (paste verbatim into `pkg/parser/doc.go` after the package clause; adjust prose if doc.go already has content):
    ```go
    // Trigger lambda contract (D-07-03 / D-07-04 deviation note).
    //
    // The trigger(...) builtin captures two lambdas — `map` and `idempotency_key` —
    // each with the locked single-positional signature `lambda req: ...`. This
    // OVERRIDES the illustrative `lambda payload, headers` shown in REQUIREMENTS.md
    // TRIG-01's success criterion (D-07-03): the locked one-arg form is the actual
    // contract; the multi-arg illustration is decorative.
    //
    // Trigger lambdas resolve free variables against `bridge.triggerTimeGlobals`
    // (lambdaTimeGlobals + json + time), NOT against the workflow `lambdaTimeGlobals`
    // (D-07-04). The extra `json.Module` + `time.Module` are SAFE in trigger lambdas
    // because trigger lambdas are non-deterministic by design — they execute exactly
    // once at HTTP ingress (the receive-side activity), and their output is persisted
    // into the workflow's StartWorkflowOptions before the workflow itself starts.
    // No workflow replay ever re-evaluates a trigger lambda, so non-deterministic
    // globals (e.g., time.now(), wall-clock-dependent json formatting) are safe here.
    //
    // Workflow lambdas, by contrast, evaluate inside workflow.Go contexts during
    // replay — they MUST stay deterministic and thus MUST NOT see json/time.
    ```

    Step 3 — Verify the doc.go content via grep:
    ```bash
    grep -q "D-07-03" pkg/parser/doc.go
    grep -q "trigger lambda" pkg/parser/doc.go
    grep -q "lambdaTimeGlobals + json + time" pkg/parser/doc.go
    grep -q "non-deterministic" pkg/parser/doc.go
    go build ./pkg/parser/...
    go vet ./pkg/parser/...
    ```

    All must exit 0.

    DO NOT add `triggerTimeGlobals` to the parse-time env — they're disjoint by design.
    DO NOT add the new builtin to the `tester` namespace or test-mode globals — triggers are production-mode concepts.
  </action>
  <acceptance_criteria>
    - `grep -nE '"trigger":\s+starlark\.NewBuiltin\("trigger", p\.builtinTrigger\)' pkg/parser/globals.go` returns exactly one match
    - `grep -nE '"flow":\s+starlark\.NewBuiltin\("flow", p\.builtinFlow\)' pkg/parser/globals.go` returns exactly one match (regression — flow registration unchanged)
    - The "trigger" entry appears AFTER the "call_flow" entry and BEFORE the "result" entry in newParseTimeGlobals (verify by `awk '/"call_flow":/,/"result":/' pkg/parser/globals.go | grep -c '"trigger":'` returns at least 1)
    - File `pkg/parser/doc.go` exists
    - `grep -q "D-07-03" pkg/parser/doc.go` exits 0 (deviation reference greppable)
    - `grep -q "trigger lambda" pkg/parser/doc.go` exits 0 (D-07-03 trigger-lambda contract surfaced)
    - `grep -q "lambdaTimeGlobals + json + time" pkg/parser/doc.go` exits 0 (D-07-04 enriched-globals contract)
    - `grep -q "non-deterministic" pkg/parser/doc.go` exits 0 (D-07-04 rationale: trigger lambdas run once at HTTP ingress, no replay)
    - `go build ./pkg/parser/...` exits 0
    - `go vet ./pkg/parser/...` exits 0
    - `go test ./pkg/parser/... -count=1 -race` passes the existing parser suite (no regression)
  </acceptance_criteria>
  <verify>
    <automated>go build ./pkg/parser/... && go vet ./pkg/parser/... && go test ./pkg/parser/... -count=1 -race && grep -qE '"trigger":\s+starlark\.NewBuiltin\("trigger", p\.builtinTrigger\)' pkg/parser/globals.go</automated>
  </verify>
  <done>
    `trigger(...)` is callable from `.star` files at parse time. `pkg/parser/doc.go` contains a 10-20 line block documenting the D-07-03 trigger-lambda signature deviation AND the D-07-04 trigger-vs-workflow-lambda env distinction (trigger lambdas use lambdaTimeGlobals + json + time, run once at HTTP ingress, are non-deterministic by design). The existing parser test suite remains green (the new builtin is not yet exercised by any test — Tasks 6+7 add coverage).
  </done>
</task>

<task type="auto">
  <id>07-03-05</id>
  <name>Task 5: Wire validateTriggerFlowNames + warnDuplicateTriggers + validateTriggerReqAccesses into finalize.go</name>
  <read_first>
    - pkg/parser/finalize.go (FULL file — finalize chain ordering matters; understand each existing pass position and the rationale comments)
    - pkg/parser/req_walk.go (Task 1 output — validateTriggerReqAccesses + checkTriggerLambdaReq are defined but not wired)
    - pkg/parser/parser.go (Task 3 output — p.triggers + p.triggerWarnings exist)
    - pkg/dag/trigger.go (Plan 01 output)
    - .planning/phases/07-trigger-primitive-server-shell/07-RESEARCH.md (§ Pitfall 2 — finalize ordering for cross-file FlowName resolution)
    - .planning/phases/07-trigger-primitive-server-shell/07-CONTEXT.md (D-07-12 cross-file resolution; D-07-13 byte-identical duplicate warning, not error)
  </read_first>
  <files>pkg/parser/finalize.go, pkg/parser/req_walk.go</files>
  <action>
    Step 1 — Edit `pkg/parser/req_walk.go`. APPEND the two new finalize-pass methods VERBATIM from the `<interfaces>` block:

    1. `func (p *Parser) validateTriggerFlowNames() error` — paste verbatim. Its sortedFlowNames helper goes in the same file.
    2. `func (p *Parser) warnDuplicateTriggers() error` — paste verbatim. Uses `slog.Warn` ONLY by accumulating into `p.triggerWarnings` (per Task 3's parser-state addition); does NOT call `slog.Default().Warn` directly because finalize tests should be deterministic and not rely on a configured logger.

    For warnDuplicateTriggers, observe: the function NEVER returns an error — it accumulates warnings and returns nil. Comment this clearly:
    ```go
    // warnDuplicateTriggers ALWAYS returns nil — duplicate triggers are
    // ACCEPTED per D-07-13. Warnings accumulate on p.triggerWarnings; the
    // worker boot loop (Plan 04) drains them via Parser.TriggerWarnings()
    // and surfaces them via slog.Warn at server startup.
    ```

    Required imports in `pkg/parser/req_walk.go` after this step:
    ```go
    import (
        "fmt"
        "sort"

        "go.starlark.net/syntax"

        "github.com/mikelalcon/skytime/pkg/dag"
    )
    ```

    Step 2 — Edit `pkg/parser/finalize.go::finalize`. Insert the new passes in the chain. ORDERING (per § Pitfall 2 + finalize.go's existing comment block):

    1. `resolveCallFlows` (existing) — must run first; both finalize passes here AND the new validateTriggerFlowNames inspect p.flows.
    2. `validateTriggerFlowNames` (NEW) — runs RIGHT AFTER resolveCallFlows, BEFORE lintMixedIdempotency. Rationale: an unknown FlowName error is more useful than any downstream lint that masks it.
    3. `lintMixedIdempotency` (existing).
    4. ... existing chain through `validateLambdaCtxAccesses` ...
    5. `validateTriggerReqAccesses` (NEW) — runs AFTER `validateLambdaCtxAccesses` (ctx-typo errors should surface FIRST per the existing finalize ordering doctrine).
    6. ... `validateResultPlacement`, `validateIfCondExpressionShape`, `validateActionRefKwargs` (existing).
    7. `warnDuplicateTriggers` (NEW) — runs LAST in the chain. Rationale: it never returns an error; running it last ensures no real error is masked by spurious warning state. (Order doesn't actually matter functionally since it can't error; running last makes the intent obvious.)

    Resulting chain (replace existing finalize body):
    ```go
    func (p *Parser) finalize() error {
        if err := p.resolveCallFlows(); err != nil {
            return err
        }
        if err := p.validateTriggerFlowNames(); err != nil { // D-07-12
            return err
        }
        if err := p.lintMixedIdempotency(); err != nil {
            return err
        }
        if err := p.lintBlockFnIdempotency(); err != nil {
            return err
        }
        if err := p.lintBlockSize(); err != nil {
            return err
        }
        if err := p.lintEmptyTaskQueue(); err != nil {
            return err
        }
        if err := p.validateLambdaCtxAccesses(); err != nil {
            return err
        }
        if err := p.validateTriggerReqAccesses(); err != nil { // D-07-05
            return err
        }
        if err := p.validateResultPlacement(); err != nil {
            return err
        }
        if err := p.validateIfCondExpressionShape(); err != nil {
            return err
        }
        if err := p.validateActionRefKwargs(); err != nil {
            return err
        }
        return p.warnDuplicateTriggers() // D-07-13 (never errors)
    }
    ```

    Update the doc comment at the top of `finalize.go` to document the two new passes (insert items 1.5 and 5.5 in the existing numbered list in the comment block).

    Step 3 — Verify:
    ```bash
    go build ./pkg/parser/...
    go vet ./pkg/parser/...
    go test ./pkg/parser/... -count=1 -race
    ```

    The full parser suite MUST pass. Existing tests do not declare triggers, so the new finalize passes are no-ops for them. If any existing test fails, the chain ordering broke something — investigate before proceeding.

    DO NOT remove or reorder existing finalize passes (other than slotting in the two new ones at the documented positions).
    DO NOT log directly via slog.Default in warnDuplicateTriggers; warnings flow through p.triggerWarnings.
  </action>
  <acceptance_criteria>
    - `grep -nE 'func \(p \*Parser\) validateTriggerFlowNames\(\) error' pkg/parser/req_walk.go` returns exactly one match
    - `grep -nE 'func \(p \*Parser\) warnDuplicateTriggers\(\) error' pkg/parser/req_walk.go` returns exactly one match
    - `grep -n 'p.validateTriggerFlowNames()' pkg/parser/finalize.go` returns exactly one match
    - `grep -n 'p.validateTriggerReqAccesses()' pkg/parser/finalize.go` returns exactly one match
    - `grep -n 'p.warnDuplicateTriggers()' pkg/parser/finalize.go` returns exactly one match
    - `awk '/p\.resolveCallFlows\(\)/,/p\.validateTriggerFlowNames\(\)/' pkg/parser/finalize.go | wc -l` returns at least 4 (validates ordering: validateTriggerFlowNames after resolveCallFlows, before any other lint)
    - `awk '/p\.validateLambdaCtxAccesses\(\)/,/p\.validateTriggerReqAccesses\(\)/' pkg/parser/finalize.go | wc -l` returns at least 4 (validates ordering: req-walker after ctx-walker)
    - `grep -n 'trigger references unknown flow' pkg/parser/req_walk.go` returns at least one match (the canonical D-07-12 error literal)
    - `grep -n 'duplicate trigger' pkg/parser/req_walk.go` returns at least one match (the D-07-13 warning literal)
    - `go build ./pkg/parser/...` exits 0
    - `go vet ./pkg/parser/...` exits 0
    - `go test ./pkg/parser/... -count=1 -race` passes (REGRESSION — existing tests must remain green; finalize chain order matters)
  </acceptance_criteria>
  <verify>
    <automated>go build ./pkg/parser/... && go vet ./pkg/parser/... && go test ./pkg/parser/... -count=1 -race && grep -q 'p.validateTriggerFlowNames()' pkg/parser/finalize.go && grep -q 'p.validateTriggerReqAccesses()' pkg/parser/finalize.go && grep -q 'p.warnDuplicateTriggers()' pkg/parser/finalize.go</automated>
  </verify>
  <done>
    Finalize chain runs `validateTriggerFlowNames` after `resolveCallFlows`, `validateTriggerReqAccesses` after `validateLambdaCtxAccesses`, and `warnDuplicateTriggers` last. Existing parser tests pass unchanged. The chain is wired but no testdata fixture exercises it yet (Tasks 6+7).
  </done>
</task>

<task type="auto">
  <id>07-03-06</id>
  <name>Task 6: Create the testdata/triggers/ corpus (8 fixtures)</name>
  <read_first>
    - pkg/parser/testdata/ (list any existing subdirectories — understand the corpus convention)
    - pkg/extension/testing/triggersource.go (Plan 02 output — FakeTriggerSource and the registered kinds skytime.test.webhook + skytime.test.cron)
    - .planning/phases/07-trigger-primitive-server-shell/07-VALIDATION.md (§ Wave 0 Requirements — the canonical 8-fixture list)
    - .planning/phases/07-trigger-primitive-server-shell/07-CONTEXT.md (§ Specifics — the test corpus inventory)
  </read_first>
  <files>pkg/parser/testdata/triggers/valid.star, pkg/parser/testdata/triggers/typo.star, pkg/parser/testdata/triggers/bad_arity.star, pkg/parser/testdata/triggers/unknown_flow.star, pkg/parser/testdata/triggers/mutable_closure.star, pkg/parser/testdata/triggers/not_a_source.star, pkg/parser/testdata/triggers/cross_file_flow.star, pkg/parser/testdata/triggers/cross_file_trigger.star, pkg/parser/testdata/triggers/duplicate_warn.star</files>
  <action>
    NOTE: These .star fixtures REFERENCE a test stub TriggerSource bound under a Starlark name. The fixture files themselves cannot import Go directly. The test harness (Task 7) will:
    1. Build a Parser with a custom test extension that exposes `fake.webhook(...)` returning a `*FakeTriggerSource`.
    2. ParseFile against each fixture path.
    3. Assert specific error contents OR clean parse, per the fixture's intent.

    Each fixture should be runnable through the parser in test mode with the test extension wired. Use Starlark naming `fake.webhook(...)` (callable that returns a FakeTriggerSource).

    Step 1 — Create `pkg/parser/testdata/triggers/valid.star`:
    ```python
    # valid.star — clean trigger; req.payload + req.headers reference a
    # source that declares both attributes; should parse without error.

    flow(
        name = "check_user",
        inputs = {"repo": "string"},
        steps = [],
    )

    trigger(
        flow = "check_user",
        source = fake.webhook(req_fields = ["payload", "headers"]),
        map = lambda req: {"repo": str(req.payload)},
        idempotency_key = lambda req: str(req.headers),
        credential = "github-app-prod",
    )
    ```

    Step 2 — Create `pkg/parser/testdata/triggers/typo.star`:
    ```python
    # typo.star — req.payloud (typo for payload). Source declares
    # ["payload", "headers"]. Should error at finalize:
    #   "trigger map lambda: req has no attribute 'payloud'; available: [headers, payload] (declared by source kind 'skytime.test.webhook')"

    flow(name = "check_user", steps = [])

    trigger(
        flow = "check_user",
        source = fake.webhook(req_fields = ["payload", "headers"]),
        map = lambda req: req.payloud,
        idempotency_key = lambda req: "static-key",
    )
    ```

    Step 3 — Create `pkg/parser/testdata/triggers/bad_arity.star`:
    ```python
    # bad_arity.star — map lambda has arity 2 instead of 1. Should error
    # at builtin call (captureLambdaWithArity layer 2):
    #   "kwarg \"map\" lambda must accept exactly 1 positional parameter(s) (convention: req); got 2"

    flow(name = "check_user", steps = [])

    trigger(
        flow = "check_user",
        source = fake.webhook(req_fields = ["payload"]),
        map = lambda req, headers: req.payload,
        idempotency_key = lambda req: "k",
    )
    ```

    Step 4 — Create `pkg/parser/testdata/triggers/unknown_flow.star`:
    ```python
    # unknown_flow.star — trigger references a flow that doesn't exist.
    # Should error at finalize (validateTriggerFlowNames):
    #   "trigger references unknown flow \"missing\"; known flows: [check_user]"

    flow(name = "check_user", steps = [])

    trigger(
        flow = "missing",
        source = fake.webhook(req_fields = ["payload"]),
        map = lambda req: req.payload,
        idempotency_key = lambda req: "k",
    )
    ```

    Step 5 — Create `pkg/parser/testdata/triggers/mutable_closure.star`:
    ```python
    # mutable_closure.star — map lambda closes over a mutable module-level
    # variable. Should error at lambda capture (validateFreeVars — Phase 1
    # contract reused). The exact error text is determined by Phase 1's
    # validateFreeVars; tests assert the error mentions the closure variable.

    counter = [0]  # mutable list — Phase 1 free-var lint catches reads of mutable globals

    flow(name = "check_user", steps = [])

    trigger(
        flow = "check_user",
        source = fake.webhook(req_fields = ["payload"]),
        map = lambda req: counter[0],  # forbidden — counter is mutable
        idempotency_key = lambda req: "k",
    )
    ```

    Step 6 — Create `pkg/parser/testdata/triggers/not_a_source.star`:
    ```python
    # not_a_source.star — source kwarg is a string literal, not a
    # TriggerSource. Should error at builtin call:
    #   "trigger.source: expected TriggerSource, got string"

    flow(name = "check_user", steps = [])

    trigger(
        flow = "check_user",
        source = "just a string",
        map = lambda req: req,
        idempotency_key = lambda req: "k",
    )
    ```

    Step 7 — Create `pkg/parser/testdata/triggers/cross_file_flow.star`:
    ```python
    # cross_file_flow.star — declares flow check_user. Loaded by
    # cross_file_trigger.star which declares the trigger.

    flow(
        name = "check_user",
        inputs = {"repo": "string"},
        steps = [],
    )
    ```

    Step 8 — Create `pkg/parser/testdata/triggers/cross_file_trigger.star`:
    ```python
    # cross_file_trigger.star — loads cross_file_flow.star, then declares
    # a trigger against the flow defined there. Tests parse this file
    # alone (with cross_file_flow.star already loaded into the same
    # parser session via load()). Validates D-07-12 cross-file flow-name
    # resolution at finalize.

    load("cross_file_flow.star", "_")  # forces load; symbol bound but unused

    trigger(
        flow = "check_user",  # defined in the loaded file
        source = fake.webhook(req_fields = ["payload"]),
        map = lambda req: req.payload,
        idempotency_key = lambda req: "k",
    )
    ```

    NOTE: Starlark's `load()` requires at least one symbol; the convention `"_"` works as a discard binding. If go.starlark.net rejects `_`, use a real symbol — tests will provide one (e.g., the test harness pre-declares a `_FLOW_LOADED = True` constant in cross_file_flow.star and loads that). Adjust the fixture as needed during Task 7's authoring if the constant approach is cleaner.

    Step 9 — Create `pkg/parser/testdata/triggers/duplicate_warn.star`:
    ```python
    # duplicate_warn.star — two byte-identical triggers (same flow, same
    # source kind + config, same lambda IDs, same credential). Should
    # parse cleanly with a deferred warning accumulated on
    # parser.triggerWarnings (D-07-13).

    flow(name = "check_user", steps = [])

    trigger(
        flow = "check_user",
        source = fake.webhook(req_fields = ["payload", "headers"]),
        map = lambda req: req.payload,
        idempotency_key = lambda req: "k",
        credential = "github-app",
    )

    trigger(
        flow = "check_user",
        source = fake.webhook(req_fields = ["payload", "headers"]),
        map = lambda req: req.payload,
        idempotency_key = lambda req: "k",
        credential = "github-app",
    )
    ```

    NOTE for Task 7: byte-identical lambdas at DIFFERENT positions get DIFFERENT D-18 lambda IDs (D-18 IDs include line:col). So the duplicate-warn detection must compare by (FlowName, source kind, source MarshalJSON output, credential ID) — NOT lambda IDs. Adjust the warnDuplicateTriggers `sig` struct in Task 5 to OMIT mapLambdaID and idempLambdaID fields, OR compare against the lambda's resolved Pos.Line / Pos.Col offset. Re-read Task 5's warnDuplicateTriggers function: it currently INCLUDES mapLambdaID + idempLambdaID — that means duplicate_warn.star's two triggers will NOT be flagged (different lambda IDs). FIX: in warnDuplicateTriggers, hash the lambda by its ResolvedFn body source bytes if possible, OR just remove mapLambdaID + idempLambdaID from the `sig` struct (simpler — relying on FlowName + Source.Kind() + sourceBytes + CredentialID is sufficient for "byte-identical" intent). Update Task 5's body accordingly during this task's verify step OR document a Task 7 follow-up.

    DECISION: Update Task 5's `sig` struct to OMIT lambda IDs. The "byte-identical" criterion is (FlowName + Source.Kind + Source.MarshalJSON output + CredentialID). Two triggers with identical lambdas at different positions but same source and credential ARE the duplicate the warning targets. Edit pkg/parser/req_walk.go warnDuplicateTriggers to drop the mapLambdaID + idempLambdaID fields.

    Step 10 — Verify:
    ```bash
    ls pkg/parser/testdata/triggers/
    # Expected: 9 files — valid.star, typo.star, bad_arity.star, unknown_flow.star,
    #   mutable_closure.star, not_a_source.star, cross_file_flow.star,
    #   cross_file_trigger.star, duplicate_warn.star
    ```

    DO NOT add `_test.star` suffix to any fixture (the boot would skip them; we want production-mode parsing).
    DO NOT add Go code — these are pure Starlark fixtures.
  </action>
  <acceptance_criteria>
    - Directory `pkg/parser/testdata/triggers/` exists with 9 files (`*.star`)
    - File `pkg/parser/testdata/triggers/valid.star` exists; contains both `flow(` and `trigger(` declarations
    - File `pkg/parser/testdata/triggers/typo.star` exists; contains `req.payloud` literal
    - File `pkg/parser/testdata/triggers/bad_arity.star` exists; contains `lambda req, headers:`
    - File `pkg/parser/testdata/triggers/unknown_flow.star` exists; contains `flow = "missing"`
    - File `pkg/parser/testdata/triggers/mutable_closure.star` exists; contains `counter = [0]` and `counter[0]`
    - File `pkg/parser/testdata/triggers/not_a_source.star` exists; contains `source = "just a string"`
    - File `pkg/parser/testdata/triggers/cross_file_flow.star` exists; declares `flow(name = "check_user"`
    - File `pkg/parser/testdata/triggers/cross_file_trigger.star` exists; contains both `load("cross_file_flow.star"` and `flow = "check_user"`
    - File `pkg/parser/testdata/triggers/duplicate_warn.star` exists; contains TWO `trigger(` declarations with byte-identical content (verify via `grep -c 'trigger(' pkg/parser/testdata/triggers/duplicate_warn.star` returns 2)
    - `pkg/parser/req_walk.go` `sig` struct in `warnDuplicateTriggers` does NOT contain fields named `mapLambdaID` or `idempLambdaID` (verify via `grep -A 10 'type sig struct' pkg/parser/req_walk.go | grep -c 'LambdaID'` returns 0)
  </acceptance_criteria>
  <verify>
    <automated>ls pkg/parser/testdata/triggers/valid.star pkg/parser/testdata/triggers/typo.star pkg/parser/testdata/triggers/bad_arity.star pkg/parser/testdata/triggers/unknown_flow.star pkg/parser/testdata/triggers/mutable_closure.star pkg/parser/testdata/triggers/not_a_source.star pkg/parser/testdata/triggers/cross_file_flow.star pkg/parser/testdata/triggers/cross_file_trigger.star pkg/parser/testdata/triggers/duplicate_warn.star && [ "$(grep -c 'trigger(' pkg/parser/testdata/triggers/duplicate_warn.star)" -ge 2 ] && ! grep -A 10 'type sig struct' pkg/parser/req_walk.go | grep -q 'LambdaID'</automated>
  </verify>
  <done>
    All 9 testdata fixtures exist; duplicate_warn.star contains two byte-identical triggers; warnDuplicateTriggers' `sig` struct uses (FlowName + Source.Kind + Source.MarshalJSON output + CredentialID) as the duplicate criterion. Task 7 wires the parser-test harness against these fixtures.
  </done>
</task>

<task type="auto" tdd="true">
  <id>07-03-07</id>
  <name>Task 7: Author pkg/parser/trigger_test.go covering TRIG-01 + TRIG-04 + D-07-12 + D-07-13</name>
  <read_first>
    - pkg/parser/builtins.go (Task 3 output)
    - pkg/parser/parser.go (Task 3 output)
    - pkg/parser/finalize.go (Task 5 output)
    - pkg/parser/req_walk.go (Tasks 1+5 output)
    - pkg/extension/testing/triggersource.go (Plan 02 output — FakeTriggerSource + RegisterFakeFactories)
    - pkg/parser/testdata/triggers/*.star (Task 6 output)
    - pkg/parser/finalize_test.go for the existing harness pattern (NewParser + ParseFile + assert *dag.ParseError type and Pos)
    - .planning/phases/07-trigger-primitive-server-shell/07-VALIDATION.md (Per-Task Verification Map — TRIG-01, TRIG-04, D-07-12, D-07-13 lines)
  </read_first>
  <files>pkg/parser/trigger_test.go</files>
  <behavior>
    - Test 1 (TestBuiltinTrigger): parse valid.star with the test extension; assert no error; assert `p.Triggers()` returns exactly 1 trigger; assert trigger.FlowName == "check_user"; assert trigger.Source.Kind() == "skytime.test.webhook"; assert trigger.MapLambda != nil and trigger.IdempotencyLambda != nil; assert trigger.CredentialID == "github-app-prod".
    - Test 2 (TestBuiltinTrigger_Fields): parse valid.star; assert trigger.Pos.Filename() ends with "valid.star"; assert trigger.Pos.Line and trigger.Pos.Col are nonzero; assert MapLambda.ID and IdempotencyLambda.ID are non-empty (D-18 IDs).
    - Test 3 (TestTrigger_UnknownFlow): parse unknown_flow.star; assert error is *dag.ParseError; assert error message matches regex `trigger references unknown flow "missing"; known flows: \[check_user\]`; assert error.Pos points at the trigger() call line.
    - Test 4 (TestTrigger_BadSource): parse not_a_source.star; assert error is *dag.ParseError; assert error message matches regex `trigger\.source: expected TriggerSource, got string`; assert error.Pos points at the trigger() call line.
    - Test 5 (TestTrigger_ReqAttrTypo): parse typo.star; assert error is *dag.ValidationError; assert error message matches regex `trigger map lambda: req has no attribute "payloud"; available: \[headers payload\] \(declared by source kind "skytime\.test\.webhook"\)`; assert error.Pos points at the typo (req.payloud) attribute name's position, NOT the trigger() call line.
    - Test 6 (TestTrigger_BadArity): parse bad_arity.star; assert error is *dag.ParseError; assert error message matches regex `kwarg "map" lambda must accept exactly 1 positional parameter\(s\) \(convention: req\); got 2`; assert error.Pos points at the lambda's `lambda` keyword position.
    - Test 7 (TestTrigger_MutableClosure): parse mutable_closure.star; assert error returned (likely *dag.ParseError or *dag.ValidationError); assert error message contains "counter" or "free variable" or "mutable" (the existing Phase 1 free-var lint owns the exact wording — accept any of those substrings).
    - Test 8 (TestTrigger_CrossFileFlow): build a parser with the testdata/triggers root directory; ParseFile cross_file_trigger.star (which load()s cross_file_flow.star); assert no error; assert p.Flows() contains "check_user"; assert p.Triggers() contains exactly 1 trigger with FlowName "check_user".
    - Test 9 (TestTrigger_DuplicateWarn): parse duplicate_warn.star; assert NO error (duplicates accepted); assert p.Triggers() returns exactly 2 triggers; assert p.TriggerWarnings() returns exactly 1 warning string; assert the warning string matches regex `duplicate trigger \(byte-identical to .*\) — accepted but flagged`.
  </behavior>
  <action>
    Step 1 — Set up the test harness. Use `package parser_test` (black-box) to mirror existing parser test files and to allow importing pkg/extension/testing without cycles.

    Imports:
    ```go
    package parser_test

    import (
        "regexp"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "go.starlark.net/starlark"
        "go.starlark.net/starlarkstruct"

        "github.com/mikelalcon/skytime/pkg/dag"
        "github.com/mikelalcon/skytime/pkg/extension"
        exttest "github.com/mikelalcon/skytime/pkg/extension/testing"
        "github.com/mikelalcon/skytime/pkg/parser"
    )
    ```

    Step 2 — Define a test extension `fakeWebhookExt` that exposes `fake.webhook(req_fields=[...])` returning a `*FakeTriggerSource`. The shape:

    ```go
    // fakeWebhookExt is a test extension exposing one Starlark namespace
    // ("fake") with one attribute ("webhook") that constructs a
    // FakeTriggerSource. Used across all trigger fixtures.
    type fakeWebhookExt struct{}

    func (fakeWebhookExt) Name() string { return "fake" }

    func (fakeWebhookExt) Operations() map[string]*extension.OperationSpec { return nil }

    func (fakeWebhookExt) Initialize(thread *starlark.Thread, _ extension.InitOptions) (starlark.Value, error) {
        webhook := starlark.NewBuiltin("webhook", func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
            var reqFieldsList *starlark.List
            if err := starlark.UnpackArgs("webhook", args, kwargs, "req_fields?", &reqFieldsList); err != nil {
                return nil, err
            }
            var reqFields []string
            if reqFieldsList != nil {
                iter := reqFieldsList.Iterate()
                defer iter.Done()
                var v starlark.Value
                for iter.Next(&v) {
                    s, ok := starlark.AsString(v)
                    if !ok {
                        return nil, fmt.Errorf("req_fields must be list[string]")
                    }
                    reqFields = append(reqFields, s)
                }
            }
            return &fakeTriggerStarlarkValue{
                src: &exttest.FakeTriggerSource{
                    KindName:  "skytime.test.webhook",
                    ReqFields: reqFields,
                },
            }, nil
        })
        return starlarkstruct.FromStringDict(starlark.String("fake"), starlark.StringDict{
            "webhook": webhook,
        }), nil
    }

    var _ extension.Extension = fakeWebhookExt{}
    ```

    NOTE: `FakeTriggerSource` is a Go-side struct, not a starlark.Value — it does not implement Truth/Type/Hash/Freeze/String. To make it flow through `trigger(source=...)`, wrap it in a thin Starlark adapter:

    ```go
    // fakeTriggerStarlarkValue wraps a FakeTriggerSource so it can be
    // passed through the Starlark value system. The trigger builtin's
    // type assertion (sourceVal.(extension.TriggerSource)) inspects
    // the WRAPPED VALUE's interface, not the wrapper. Two ways to do
    // this:
    //   (a) Make FakeTriggerSource itself satisfy starlark.Value (Type,
    //       Truth, etc.) — couples test stub to starlark.
    //   (b) Define a wrapper struct here that satisfies starlark.Value
    //       AND embeds *FakeTriggerSource so type-assertion sees through.
    // Choice (b) keeps FakeTriggerSource clean.
    type fakeTriggerStarlarkValue struct {
        src *exttest.FakeTriggerSource
    }

    func (f *fakeTriggerStarlarkValue) Kind() string                     { return f.src.Kind() }
    func (f *fakeTriggerStarlarkValue) ReqSchema() []string              { return f.src.ReqSchema() }
    func (f *fakeTriggerStarlarkValue) MarshalJSON() ([]byte, error)     { return f.src.MarshalJSON() }
    func (f *fakeTriggerStarlarkValue) String() string                   { return "fake.webhook" }
    func (f *fakeTriggerStarlarkValue) Type() string                     { return "fake.webhook" }
    func (f *fakeTriggerStarlarkValue) Freeze()                          {}
    func (f *fakeTriggerStarlarkValue) Truth() starlark.Bool             { return starlark.True }
    func (f *fakeTriggerStarlarkValue) Hash() (uint32, error)            { return 0, fmt.Errorf("not hashable") }

    // The seal — fakeTriggerStarlarkValue must satisfy
    // extension.TriggerSource. Since triggerSourceMarker() is unexported
    // in pkg/extension, we delegate to the embedded FakeTriggerSource
    // (which satisfies the seal because pkg/extension/testing is a
    // sub-package of pkg/extension).
    func (f *fakeTriggerStarlarkValue) triggerSourceMarker() {}
    // Wait — triggerSourceMarker() is unexported. fakeTriggerStarlarkValue
    // lives in package parser_test, which is NOT a sub-package of
    // pkg/extension. So fakeTriggerStarlarkValue CANNOT define
    // triggerSourceMarker() directly (unexported = package-private).
    //
    // RESOLUTION: Use composition. fakeTriggerStarlarkValue EMBEDS
    // *FakeTriggerSource (which already has the seal method via the
    // testing sub-package). The embedded promote-method approach works:
    type fakeTriggerStarlarkValue struct {
        *exttest.FakeTriggerSource // embedded — promotes triggerSourceMarker()
    }
    // Plus the Starlark value methods (Type, Truth, etc.) defined on
    // *fakeTriggerStarlarkValue. Kind() / ReqSchema() / MarshalJSON()
    // are also promoted from the embedded FakeTriggerSource — no
    // delegation methods needed.
    ```

    DECISION (use this version): the wrapper EMBEDS the test stub:
    ```go
    type fakeTriggerStarlarkValue struct {
        *exttest.FakeTriggerSource
    }
    // Starlark value methods only:
    func (f *fakeTriggerStarlarkValue) String() string         { return "fake.webhook" }
    func (f *fakeTriggerStarlarkValue) Type() string           { return "fake.webhook" }
    func (f *fakeTriggerStarlarkValue) Freeze()                {}
    func (f *fakeTriggerStarlarkValue) Truth() starlark.Bool   { return starlark.True }
    func (f *fakeTriggerStarlarkValue) Hash() (uint32, error)  { return 0, fmt.Errorf("not hashable") }
    var _ starlark.Value = (*fakeTriggerStarlarkValue)(nil)
    var _ extension.TriggerSource = (*fakeTriggerStarlarkValue)(nil)
    ```

    Step 3 — Author a helper `parseFixture(t, fixturePath)`:
    ```go
    func parseFixture(t *testing.T, relPath string) (*parser.Parser, error) {
        t.Helper()
        p, err := parser.NewParser(
            parser.WithRoot("testdata/triggers"),
            parser.WithExtensions(fakeWebhookExt{}),
        )
        require.NoError(t, err)
        _, err = p.ParseFile("testdata/triggers/" + relPath)
        return p, err
    }
    ```

    Step 4 — Implement Tests 1-9 from `<behavior>`. Use `regexp.MustCompile(...).MatchString(err.Error())` for regex assertions; `require.Error` for fail-fast; `assert.Equal` for accumulating checks.

    Concrete shape for the unknown-flow test:
    ```go
    func TestTrigger_UnknownFlow(t *testing.T) {
        _, err := parseFixture(t, "unknown_flow.star")
        require.Error(t, err)
        var pe *dag.ParseError
        require.ErrorAs(t, err, &pe, "expected *dag.ParseError, got %T", err)
        assert.Regexp(t,
            `trigger references unknown flow "missing"; known flows: \[check_user\]`,
            err.Error())
    }
    ```

    For the cross-file test (Test 8):
    ```go
    func TestTrigger_CrossFileFlow(t *testing.T) {
        p, err := parser.NewParser(
            parser.WithRoot("testdata/triggers"),
            parser.WithExtensions(fakeWebhookExt{}),
        )
        require.NoError(t, err)
        // Must parse cross_file_trigger.star, which load()s cross_file_flow.star.
        _, err = p.ParseFile("testdata/triggers/cross_file_trigger.star")
        require.NoError(t, err, "cross-file trigger should parse cleanly via load()")

        // Both flow and trigger must be present.
        flows := p.Flows()
        _, ok := flows["check_user"]
        assert.True(t, ok, "flow check_user should be loaded from cross_file_flow.star")

        trigs := p.Triggers()
        require.Len(t, trigs, 1)
        assert.Equal(t, "check_user", trigs[0].FlowName)
    }
    ```

    Step 5 — Run:
    ```bash
    go test ./pkg/parser/ -run TestBuiltinTrigger -count=1 -race
    go test ./pkg/parser/ -run TestTrigger_ -count=1 -race
    go vet ./pkg/parser/...
    go test ./pkg/parser/... -count=1 -race  # FULL REGRESSION
    ```

    All must exit 0. If any fails, fix the underlying behavior — not the test.

    Step 6 — Commit hygiene: Tests 1-9 cover ALL 11 VALIDATION.md per-task verification rows for TRIG-01, TRIG-04, D-07-12, D-07-13. Confirm cross-reference:

    | VALIDATION.md row | Test in this file |
    |-------------------|-------------------|
    | TRIG-01 trigger(...) parses without I/O | TestBuiltinTrigger |
    | TRIG-01 Captured Trigger has correct fields | TestBuiltinTrigger_Fields |
    | TRIG-04 Unknown flow → position-aware error | TestTrigger_UnknownFlow |
    | TRIG-04 Source not a TriggerSource → error | TestTrigger_BadSource |
    | TRIG-04 req.<field> typo → valid-list error | TestTrigger_ReqAttrTypo |
    | TRIG-04 Lambda arity wrong → error | TestTrigger_BadArity |
    | TRIG-04 Mutable closure → free-var lint surfaces | TestTrigger_MutableClosure |
    | D-07-12 Cross-file trigger.FlowName resolution | TestTrigger_CrossFileFlow |
    | D-07-13 Byte-identical duplicates → warning, not error | TestTrigger_DuplicateWarn |

    DO NOT skip any test even if the Phase 1 free-var lint produces a slightly different error message than expected — adjust the regex to match what the actual lint produces (read the linter.go source if uncertain).
    DO NOT export any helper from pkg/parser tests — they live in package parser_test.
    DO NOT add tests for behaviors covered by other plans (e.g., TRIG-03 round-trip lives in Plan 01).
  </action>
  <acceptance_criteria>
    - File `pkg/parser/trigger_test.go` exists
    - `grep -c '^func Test' pkg/parser/trigger_test.go` returns at least 9 (one per behavior listed)
    - `grep -nE 'func TestBuiltinTrigger\b' pkg/parser/trigger_test.go` returns exactly one match
    - `grep -nE 'func TestBuiltinTrigger_Fields' pkg/parser/trigger_test.go` returns exactly one match
    - `grep -nE 'func TestTrigger_UnknownFlow' pkg/parser/trigger_test.go` returns exactly one match
    - `grep -nE 'func TestTrigger_BadSource' pkg/parser/trigger_test.go` returns exactly one match
    - `grep -nE 'func TestTrigger_ReqAttrTypo' pkg/parser/trigger_test.go` returns exactly one match
    - `grep -nE 'func TestTrigger_BadArity' pkg/parser/trigger_test.go` returns exactly one match
    - `grep -nE 'func TestTrigger_MutableClosure' pkg/parser/trigger_test.go` returns exactly one match
    - `grep -nE 'func TestTrigger_CrossFileFlow' pkg/parser/trigger_test.go` returns exactly one match
    - `grep -nE 'func TestTrigger_DuplicateWarn' pkg/parser/trigger_test.go` returns exactly one match
    - `grep -n 'fakeTriggerStarlarkValue' pkg/parser/trigger_test.go` returns at least 2 matches (definition + use)
    - `grep -n 'fakeWebhookExt' pkg/parser/trigger_test.go` returns at least 2 matches
    - `go test ./pkg/parser/ -run TestBuiltinTrigger -count=1 -race` exits 0
    - `go test ./pkg/parser/ -run TestTrigger_ -count=1 -race` exits 0 (all 7 TestTrigger_* cases pass)
    - `go test ./pkg/parser/... -count=1 -race` exits 0 (full regression — no existing test broken)
    - `go vet ./pkg/parser/...` exits 0
  </acceptance_criteria>
  <verify>
    <automated>go test ./pkg/parser/ -run 'TestBuiltinTrigger|TestTrigger_' -count=1 -race && go test ./pkg/parser/... -count=1 -race && go vet ./pkg/parser/... && [ "$(grep -c '^func Test' pkg/parser/trigger_test.go)" -ge 9 ]</automated>
  </verify>
  <done>
    All 9 tests for TRIG-01, TRIG-04, D-07-12, D-07-13 pass. The fakeWebhookExt + fakeTriggerStarlarkValue test harness flows FakeTriggerSource through the Starlark value system and the trigger builtin. Full parser test suite remains green.
  </done>
</task>

</tasks>

<verification>
After all 7 tasks complete, run:

```bash
go build ./pkg/parser/... ./pkg/bridge/... ./pkg/dag/... ./pkg/extension/... ./pkg/extension/testing/...
go vet ./pkg/parser/... ./pkg/bridge/... ./pkg/dag/... ./pkg/extension/... ./pkg/extension/testing/...
go test ./pkg/parser/... ./pkg/bridge/... ./pkg/dag/... ./pkg/extension/... ./pkg/extension/testing/... -count=1 -race
```

All must exit 0. The full quick-loop suite (per VALIDATION.md sampling rate) is green.

Cross-package check (no leakage to other packages yet):
```bash
git grep -F 'parser.NewParser' -- 'pkg/' | grep -v 'pkg/parser/' | head -5
# Expected: pkg/worker/boot.go, pkg/validator/validator.go (existing) — no NEW callers
```

Test corpus determinism check:
```bash
ls pkg/parser/testdata/triggers/ | sort
# Expected output: bad_arity.star, cross_file_flow.star, cross_file_trigger.star,
#   duplicate_warn.star, mutable_closure.star, not_a_source.star, typo.star,
#   unknown_flow.star, valid.star
```
</verification>

<success_criteria>
- TRIG-01 satisfied: `trigger(...)` is a top-level Starlark builtin parsed without I/O; `Parser.Triggers()` returns the captured slice.
- TRIG-04 satisfied: All five parse-time validation layers surface position-aware errors (unknown flow, source not TriggerSource, req.<field> typo, lambda arity, mutable closure).
- D-07-01 satisfied: `pkg/bridge.triggerTimeGlobals` is a 22-key locked StringDict (lambdaTimeGlobals + json + time).
- D-07-04 documented: `pkg/bridge/doc.go` explains why trigger lambdas can use non-deterministic globals while workflow lambdas cannot.
- D-07-05 satisfied: three-layer parse-time validation (free-var lint + arity + req-walker).
- D-07-12 satisfied: cross-file `trigger.FlowName` resolution at finalize.
- D-07-13 satisfied: byte-identical duplicate triggers warn, do not error; non-identical duplicates accepted silently.
- The `findCtxAccesses` -> `findFreeVarAccesses` refactor maintains regression parity (TestCtxWalk_* tests pass unchanged).
- Wave-2 unblocks Wave-3 (Plan 04 — TriggerRegistry consumes `Parser.Triggers()`).
</success_criteria>

<output>
After completion, create `.planning/phases/07-trigger-primitive-server-shell/07-03-SUMMARY.md` documenting:
- The `trigger(...)` builtin signature exactly as shipped (kwargs, types, return value)
- The `triggerTimeGlobals` 22-key inventory (locked 20 from lambdaTimeGlobals + json + time)
- The `findCtxAccesses` -> `findFreeVarAccesses` refactor rationale and the regression-guard strategy (TestCtxWalk_*)
- The finalize chain order — exact position of the three new passes (validateTriggerFlowNames, validateTriggerReqAccesses, warnDuplicateTriggers)
- The `Parser.Triggers()` accessor return contract (sorted by Source.Kind, FlowName, Pos) — Plan 04 will consume
- The `Parser.TriggerWarnings()` accessor — Plan 04 boot loop drains via slog.Warn
- The captureLambdaWithArity helper signature + arity rejection rules
- The duplicate-trigger `sig` struct's exact composition: (FlowName, Source.Kind, Source.MarshalJSON output, CredentialID) — NO lambda IDs
- The 8 testdata fixtures + cross-file harness pattern (load() with at least one symbol)
- The fakeWebhookExt + fakeTriggerStarlarkValue test-harness shape — Plan 04 will reuse the same wrapper for boot tests
- Test coverage for TRIG-01, TRIG-04, D-07-12, D-07-13 — explicit list of test functions
</output>
</content>
</invoke>