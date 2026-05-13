package parser

import (
	"fmt"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// captureResultValueLambda synthesizes `result_lam = lambda ctx: <expr>`
// from the given AST expression's source text, parses it via the
// standard parser thread, and returns a *dag.CapturedLambda whose Pos is
// the user-source value-expression position and whose BodyPos is the
// synthesized file `<result:<file>:<line>:<col>:<key>>`. The returned
// lambda is added to p.lambdas; the synthetic source is added to
// p.fileBytes so the D4-02 walker re-parse path finds it.
//
// Mirrors interpolation.go::desugarInterpolation per Phase 04.2
// RESEARCH §Pattern 4. The user-source remap for ctx-validation errors
// is handled by checkLambdaCtx's `<result:` prefix match (Pitfall 3).
func (p *Parser) captureResultValueLambda(
	valueExpr syntax.Expr,
	callPos syntax.Position,
	key string,
	fileBytes []byte,
) (*dag.CapturedLambda, error) {
	// Slice the user-source text for the value expression. syntax.Start
	// and syntax.End report 1-based (Line, Col) positions; we need byte
	// offsets into fileBytes. lineColToOffset walks fileBytes once per
	// call (cheap — value expressions are always on a small number of
	// lines; the file is already in cache).
	startPos := syntax.Start(valueExpr)
	endPos := syntax.End(valueExpr)
	startOff, ok := lineColToByteOffset(fileBytes, startPos.Line, startPos.Col)
	if !ok {
		return nil, fmt.Errorf("internal: cannot map value-expr start %s into file bytes", startPos)
	}
	endOff, ok := lineColToByteOffset(fileBytes, endPos.Line, endPos.Col)
	if !ok {
		return nil, fmt.Errorf("internal: cannot map value-expr end %s into file bytes", endPos)
	}
	if endOff < startOff || endOff > len(fileBytes) {
		return nil, fmt.Errorf("internal: invalid value-expr byte range [%d,%d) for key %q", startOff, endOff, key)
	}
	valueSrc := string(fileBytes[startOff:endOff])

	// Build the full synthesized source. The "result_lam = " prefix is
	// FIXED (24 chars + 1 space = 25 chars before the body) so BodyPos
	// can be computed as a constant offset rather than re-parsing the
	// synthetic file.
	const resultPrefix = "result_lam = "
	fullSrc := resultPrefix + "lambda ctx: " + valueSrc
	syntheticName := fmt.Sprintf("<result:%s:%d:%d:%s>",
		callPos.Filename(), callPos.Line, callPos.Col, key)

	// Cache the synthetic source so checkLambdaCtx's BodyPos re-parse
	// path can find the AST when validating ctx.<name> references.
	if p.fileBytes == nil {
		p.fileBytes = make(map[string][]byte)
	}
	p.fileBytes[syntheticName] = []byte(fullSrc)

	// Compile the synthetic source on a sub-thread; lambda-time globals
	// give the body access to the locked 20-key vocabulary (len/str/...).
	//
	// Compile failure path: when the user's value expression references
	// names not in lambda-time scope (e.g., user-defined `helper(x)`,
	// extension factories like `gh.endpoint`, top-level constants),
	// Starlark's compile-time resolver errors with "undefined: <name>".
	// We fall back to a no-op `lambda ctx: None` placeholder so the
	// parser produces a *dag.Result whose Types map carries the correct
	// (Opaque) signal even when the value expression is opaque.
	//
	// Why this is safe semantically: Phase 3 worker bootstrap re-parses
	// the user's .star files at workflow start and produces fresh
	// *starlark.Function values for every lambda ID. The Fn captured
	// here at parse time is a placeholder; the runtime uses the
	// re-parsed Fn from FlowRegistry. If the user's `helper(1)` is
	// truly unresolvable at runtime, the workflow will fail then —
	// matching the user's intent (the runtime error stays attached to
	// their actual call site via the cached fileBytes).
	subThread := &starlark.Thread{Name: "skytime-result-value:" + syntheticName}
	opts := defaultFileOptions()
	globals, err := starlark.ExecFileOptions(opts, subThread, syntheticName, fullSrc, lambdaTimeGlobalsForDesugar())
	if err != nil {
		// Fall back to a placeholder lambda. The body intentionally
		// returns None — runtime never executes this Fn (re-parse).
		const placeholderSrc = "result_lam = lambda ctx: None"
		placeholderName := "<result-placeholder:" + syntheticName + ">"
		ph, phErr := starlark.ExecFileOptions(opts, subThread, placeholderName, placeholderSrc, lambdaTimeGlobalsForDesugar())
		if phErr != nil {
			// Should not happen — placeholder is hard-coded valid.
			return nil, &dag.ParseError{
				Pos: startPos,
				Msg: fmt.Sprintf("result.value[%q]: internal — failed to compile placeholder: %v (original: %v)", key, phErr, err),
			}
		}
		globals = ph
	}
	fnVal, ok := globals["result_lam"]
	if !ok {
		return nil, &dag.ParseError{Pos: startPos, Msg: "result.value: internal — synthesized source produced no 'result_lam'"}
	}
	fn, ok := fnVal.(*starlark.Function)
	if !ok {
		return nil, &dag.ParseError{
			Pos: startPos,
			Msg: fmt.Sprintf("result.value: internal — synthesized 'result_lam' is %s, expected function", fnVal.Type()),
		}
	}

	// BodyPos points at the `lambda` keyword inside the synthetic file.
	// "result_lam = " is 13 chars (1..13); `lambda` starts at col 14.
	// 1-based: chars 1..13 are "result_lam = ", `lambda` begins at col 14.
	bodyPos := syntax.MakePosition(&syntheticName, 1, int32(len(resultPrefix)+1))

	// Capture with userPos at the value-expr start (so ctx-validation
	// errors land on user source), BodyPos at the synthetic file (so
	// the D4-02 walker re-parses the synthetic AST).
	return p.captureLambdaAtPosition(fn, startPos, bodyPos, "")
}

// lineColToByteOffset walks src and returns the byte offset for the
// given 1-based (line, col) position, or (0, false) if the position is
// out of range. col is 1-based — col=1 means the first byte of the
// line. Newline characters advance line and reset col to 1; the byte at
// the newline itself is NOT considered "col 1 of the next line" — that
// position is the byte AFTER the newline.
//
// Helper for captureResultValueLambda; deliberately byte-oriented (not
// rune-oriented) because syntax.Position.Col is documented as a "rune
// number" but the existing parser code (interpolation.go's posAt) walks
// bytes as a 1:1 approximation. ASCII source is the common case in
// .star files and matches the existing convention.
func lineColToByteOffset(src []byte, line, col int32) (int, bool) {
	if line <= 0 || col <= 0 {
		return 0, false
	}
	curLine := int32(1)
	curCol := int32(1)
	for i := 0; i < len(src); i++ {
		if curLine == line && curCol == col {
			return i, true
		}
		if src[i] == '\n' {
			curLine++
			curCol = 1
		} else {
			curCol++
		}
	}
	if curLine == line && curCol == col {
		return len(src), true
	}
	return 0, false
}
