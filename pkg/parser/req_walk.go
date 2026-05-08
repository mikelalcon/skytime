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
	// Sort triggers by posKey so error attribution is deterministic when
	// multiple typos exist (mirrors validateTriggerFlowNames).
	keys := make([]string, 0, len(p.triggers))
	for k := range p.triggers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		trig := p.triggers[k]
		// trig.Source is dag.TriggerSource (Kind + MarshalJSON only). The
		// req-walker needs ReqSchema, which lives on extension.TriggerSource
		// (the dag-side interface stays minimal to avoid pkg/dag → pkg/extension
		// import cycle). The parser builtin (Plan 03 Task 3) only accepts
		// extension.TriggerSource values, so this assertion holds in practice.
		// Defensive: if a future construction path bypasses the builtin, the
		// failed assertion surfaces a clear error rather than nil-deref.
		schemaProvider, ok := trig.Source.(interface{ ReqSchema() []string })
		if !ok {
			return &dag.ParseError{
				Pos: trig.Pos,
				Msg: fmt.Sprintf("internal: trigger source kind %q does not expose ReqSchema (req-walker)", trig.Source.Kind()),
			}
		}
		validFields := setFromSliceTrigger(schemaProvider.ReqSchema())
		if err := p.checkTriggerLambdaReq(trig, trig.MapLambda, validFields, "map"); err != nil {
			return err
		}
		if err := p.checkTriggerLambdaReq(trig, trig.IdempotencyLambda, validFields, "idempotency_key"); err != nil {
			return err
		}
	}
	return nil
}

// checkTriggerLambdaReq validates one trigger lambda's req.<attr>
// references against the provided valid-fields set. Returns *dag.ValidationError
// at the first typo encountered; nil when the lambda is absent or all
// references are valid.
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
				Msg:  fmt.Sprintf("trigger %s lambda: req has no attribute %q; available: %v (declared by source kind %q)", kwargName, acc.AttrName, sortedKeysTrigger(validFields), trig.Source.Kind()),
			}
		}
	}
	return nil
}

// setFromSliceTrigger builds a presence-set from a string slice. Suffixed
// with "Trigger" to avoid collision with any existing helper of similar
// shape elsewhere in pkg/parser.
func setFromSliceTrigger(s []string) map[string]struct{} {
	m := make(map[string]struct{}, len(s))
	for _, v := range s {
		m[v] = struct{}{}
	}
	return m
}

// sortedKeysTrigger returns the keys of a presence-set as a sorted slice.
// Suffixed with "Trigger" to avoid collision with any existing helper.
func sortedKeysTrigger(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
