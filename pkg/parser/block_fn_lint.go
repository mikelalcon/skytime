package parser

import (
	"fmt"

	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
)

// extOp records one statically-classified extension call within a
// block_fn lambda body. Used by classifyBlockFn to compute homogeneity.
type extOp struct {
	Ext        string
	Op         string
	Idempotent bool
	Pos        syntax.Position
}

// blockFnClassification is the result of walking a block_fn lambda's
// body. The classifier records every recognized <ext>.<op> call and
// flags any opaque shapes (helper functions, indirect dispatch, etc).
type blockFnClassification struct {
	TypedCalls  []extOp
	OpaqueCalls []syntax.Position
	HasOpaque   bool
}

// classifyBlockFn walks the body of a block_fn lambda and classifies
// each *syntax.CallExpr it finds at the OUTERMOST CallExpr layer that
// produces ActionRef elements. The walk re-parses the cached file bytes
// at captured.BodyPos.Filename() (synthesized lambdas) OR
// captured.Pos.Filename() (hand-written lambdas) and matches the lambda
// by position via positionsEqual.
//
// Heuristic (RESEARCH §Pattern 3 + D4.1-11 locked amendment): the
// classifier walks ONLY the OUTERMOST CallExpr nodes that produce
// ActionRef elements — i.e., direct call expressions inside the lambda's
// return-position expression (typically the body of a
// `[gh.op(...) for x in ctx.list]` comprehension or the elements of a
// returned list literal). Sub-expressions INSIDE kwargs of a recognized
// `<ext>.<op>(...)` call are NOT walked — kwarg shape validation is the
// parser's separate kwarg-pass concern (D-11). Examples:
//
//	gh.get(path = str(p))            // typed; str(p) NOT walked
//	gh.get(path = "/" + ctx.x)       // typed; "+ ctx.x" NOT walked
//	helper(x)                        // OPAQUE (helper is user-def)
//	len(ctx.repos)                   // OPAQUE (len is not <ext>.<op>)
//	gh.create_issue(...) if c else gh.delete_issue(...)  // BOTH walked
//
// The rule prevents every block_fn that uses str(...) or `+` for path
// concatenation from being pushed to runtime, defeating the parse-time
// best-effort lint.
//
// Extension recognition: starlark binds the extension instance to a
// user-chosen variable (e.g., `gh = http.endpoint(...)`). The classifier
// can't statically resolve the assignment chain, so it falls back to a
// best-effort op-name lookup: try p.registry.Get(extIdent.Name) first
// (matches when user wrote the registered extension name directly,
// `fake_ext.echo(...)`); otherwise scan all registered extensions for
// the op name and accept the unique match. Ambiguous matches (two
// extensions sharing an op name) are conservatively marked opaque —
// best-effort lint never produces false rejections.
//
// Returns (classification, nil) on success. Returns (zero, error) only
// on internal failures (re-parse error, missing file bytes).
func (p *Parser) classifyBlockFn(captured *dag.CapturedLambda) (blockFnClassification, error) {
	if captured == nil {
		return blockFnClassification{}, nil
	}
	walkPos := captured.Pos
	if captured.BodyPos.IsValid() {
		walkPos = captured.BodyPos
	}
	src, ok := p.fileBytes[walkPos.Filename()]
	if !ok {
		return blockFnClassification{}, fmt.Errorf("classifyBlockFn: file bytes for %q not cached", walkPos.Filename())
	}
	file, err := defaultFileOptions().Parse(walkPos.Filename(), src, 0)
	if err != nil {
		return blockFnClassification{}, err
	}

	// Locate the lambda body or def-stmt body matching walkPos.
	var lambdaBody syntax.Expr
	var lambdaStmts []syntax.Stmt
	syntax.Walk(file, func(n syntax.Node) bool {
		switch fn := n.(type) {
		case *syntax.LambdaExpr:
			if positionsEqual(fn.Lambda, walkPos) {
				lambdaBody = fn.Body
			}
		case *syntax.DefStmt:
			if positionsEqual(fn.Def, walkPos) {
				lambdaStmts = fn.Body
			}
		}
		return true
	})

	var c blockFnClassification

	// classify is called for every node syntax.Walk visits. For each
	// outermost *syntax.CallExpr we either record a typed extOp or mark
	// HasOpaque + return false to STOP recursion into the call's
	// argument list (kwargs are not part of the classifier's surface
	// per the D4.1-11 amendment).
	classify := func(n syntax.Node) bool {
		call, ok := n.(*syntax.CallExpr)
		if !ok {
			return true
		}
		dot, isDot := call.Fn.(*syntax.DotExpr)
		if !isDot {
			// Bare ident call (helper function, len, str, etc.) → opaque.
			c.HasOpaque = true
			c.OpaqueCalls = append(c.OpaqueCalls, call.Lparen)
			return false
		}
		extIdent, ok := dot.X.(*syntax.Ident)
		if !ok {
			c.HasOpaque = true
			c.OpaqueCalls = append(c.OpaqueCalls, call.Lparen)
			return false
		}
		opName := dot.Name.Name
		spec, extName, ok := p.lookupOpSpec(extIdent.Name, opName)
		if !ok || spec == nil || spec.Idempotent == nil {
			c.HasOpaque = true
			c.OpaqueCalls = append(c.OpaqueCalls, call.Lparen)
			return false
		}
		c.TypedCalls = append(c.TypedCalls, extOp{
			Ext:        extName,
			Op:         opName,
			Idempotent: *spec.Idempotent,
			Pos:        call.Lparen,
		})
		// Recognized typed call: STOP descending. The kwarg expressions
		// (e.g., `path = str(p)`) are NOT part of the classifier's
		// surface per the D4.1-11 amendment.
		return false
	}

	if lambdaBody != nil {
		syntax.Walk(lambdaBody, classify)
	}
	for _, stmt := range lambdaStmts {
		syntax.Walk(stmt, classify)
	}
	return c, nil
}

// lookupOpSpec resolves a <receiver>.<op> dotted call to an
// OperationSpec. Two-step heuristic:
//
//  1. p.registry.Get(receiverName) — matches when the user wrote the
//     registered extension name directly (e.g., `fake_ext.echo`).
//  2. Scan all registered extensions for an op with the given name. If
//     EXACTLY one extension declares it, accept that match. Zero or
//     multiple matches → return ok=false (caller marks opaque).
//
// Returns (spec, extName, ok). extName is the registered Extension.Name()
// for use in classification records.
func (p *Parser) lookupOpSpec(receiverName, opName string) (*extension.OperationSpec, string, bool) {
	if ext, ok := p.registry.Get(receiverName); ok {
		if spec, ok2 := ext.Operations()[opName]; ok2 {
			return spec, ext.Name(), true
		}
	}
	// Fallback: scan all registered extensions; accept unique match.
	var (
		hit      *extension.OperationSpec
		hitName  string
		hitCount int
	)
	for name, ext := range p.registry.All() {
		if spec, ok := ext.Operations()[opName]; ok {
			hit = spec
			hitName = name
			hitCount++
		}
	}
	if hitCount == 1 {
		return hit, hitName, true
	}
	return nil, "", false
}

// lintBlockFnIdempotency walks every flow's body and asserts that each
// step(block_fn=...) lambda's body produces a homogeneous batch when
// statically analyzable (D4.1-11). Mixed-typed batches are rejected at
// parse time with the same fix-suggestion shape as D2-05's
// lintMixedIdempotency. Opaque shapes (helper functions, indirect
// dispatch) defer to the runtime fallback (D4.1-12 in pkg/activity).
//
// Recursion mirrors walkLintMixedIdempotency so the two passes have
// identical control-flow shape — easier to grep and reason about.
func (p *Parser) lintBlockFnIdempotency() error {
	for _, flow := range p.flows {
		if err := p.walkLintBlockFnIdempotency(flow.Name, flow.Body); err != nil {
			return err
		}
	}
	return nil
}

// walkLintBlockFnIdempotency is the recursive helper for
// lintBlockFnIdempotency. Mirrors walkLintMixedIdempotency's shape so
// every Phase 2 / Phase 4.1 lint pass shares the same recursion idiom.
func (p *Parser) walkLintBlockFnIdempotency(flowName string, body []dag.Node) error {
	for _, node := range body {
		switch n := node.(type) {
		case *dag.Step:
			if n.BlockFn == nil {
				continue
			}
			c, err := p.classifyBlockFn(n.BlockFn)
			if err != nil {
				return err
			}
			if c.HasOpaque {
				continue // defer to runtime (D4.1-12)
			}
			// All typed — assert homogeneity.
			var (
				anyIdem, anyNonIdem  extOp
				hasIdem, hasNonIdem  bool
			)
			for _, t := range c.TypedCalls {
				if t.Idempotent {
					if !hasIdem {
						anyIdem = t
					}
					hasIdem = true
				} else {
					if !hasNonIdem {
						anyNonIdem = t
					}
					hasNonIdem = true
				}
			}
			if hasIdem && hasNonIdem {
				return &dag.ValidationError{
					Pos:  n.Pos,
					Flow: flowName,
					Msg: fmt.Sprintf(
						"block_fn cannot mix idempotent and non-idempotent operations:\n  - %s.%s (idempotent)\n  - %s.%s (NOT idempotent)\nSuggestion: split into separate steps with action_fn each.",
						anyIdem.Ext, anyIdem.Op, anyNonIdem.Ext, anyNonIdem.Op),
				}
			}
		case *dag.IfCond:
			if err := p.walkLintBlockFnIdempotency(flowName, n.Then); err != nil {
				return err
			}
			if err := p.walkLintBlockFnIdempotency(flowName, n.Else); err != nil {
				return err
			}
		case *dag.ForEachParallel:
			if err := p.walkLintBlockFnIdempotency(flowName, n.Steps); err != nil {
				return err
			}
		}
	}
	return nil
}
