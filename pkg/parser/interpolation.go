package parser

import (
	"fmt"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/bridge"
	"github.com/mikelalcon/skytime/pkg/dag"
)

// scanPart is one segment of a scanned template — either literal text
// or an inner expression to be desugared into a str(...) concatenation.
//
// For Kind == "text" the Text field holds the (already-unescaped) literal
// segment as it should appear in the synthesized lambda; Pos is zero.
// For Kind == "expr" the Expr field holds the inner Starlark expression
// text (without surrounding ${ and }); Pos points at the user's source
// at the offset of the OPENING ${.
type scanPart struct {
	Kind string // "text" or "expr"
	Text string // for Kind == "text"
	Expr string // for Kind == "expr"
	Pos  syntax.Position
}

// scannedTemplate is the result of scanInterpolation. HasInterpolation is
// true if any expr part was found; false otherwise (caller short-circuits
// and stores the literal string unchanged when false).
type scannedTemplate struct {
	Parts            []scanPart
	HasInterpolation bool
}

// scanInterpolation walks an already-unquoted Starlark string literal's
// content looking for ${...} markers (D4.1-04). The 4-state machine —
// "text" / "expr" / "expr-str" — handles:
//
//   - $$ → literal $ in the text accumulator (escape).
//   - ${ → enter expr mode; bracket-counted; the matching } returns to
//     text mode.
//   - " or ' inside expr → enter expr-str mode; \-escapes honored;
//     embedded } is literal until the closing quote.
//   - \n inside an expression → "multi-line interpolation" parse error.
//   - ${} → "empty interpolation" parse error.
//   - ${ with no closing } before EOF → "unterminated interpolation".
//   - bare $ not followed by { → literal $ in text.
//
// openPos is the position of the OPENING quote of the user's string
// literal. Each ${ part's Pos is computed as openPos + offset within
// `raw` (newline-aware: a \n in the raw bumps Line and resets Col).
// Hand-written lambdas use openPos straight; this scanner adjusts so
// errors and lambda IDs land at the actual ${ in the user's source.
func scanInterpolation(raw string, openPos syntax.Position) (scannedTemplate, error) {
	var parts []scanPart
	var hasInterp bool

	var text strings.Builder
	flushText := func() {
		if text.Len() > 0 {
			parts = append(parts, scanPart{Kind: "text", Text: text.String()})
			text.Reset()
		}
	}

	const (
		stText    = 0
		stExpr    = 1
		stExprStr = 2
	)
	state := stText

	var (
		exprStart  int               // byte offset of opening ${ in raw
		exprBuf    strings.Builder   // accumulates the inner expression text
		exprPos    syntax.Position   // user-source position of opening ${
		depth      int               // bracket nesting in expr mode (counts {[(])
		quote      byte              // active quote char in expr-str mode
		tripleQ    bool              // true when in triple-quoted literal
		escapeNext bool              // last char in expr-str was \
	)

	// posAt computes the user-source position of byte offset i in raw,
	// adjusting openPos by the count of newlines in raw[:i]. The first
	// byte of raw is the first character of the string literal's content;
	// position arithmetic is line-incrementing on \n with col reset to 1.
	posAt := func(i int) syntax.Position {
		line := openPos.Line
		col := openPos.Col
		for j := 0; j < i && j < len(raw); j++ {
			if raw[j] == '\n' {
				line++
				col = 1
			} else {
				col++
			}
		}
		fn := openPos.Filename()
		return syntax.MakePosition(&fn, line, col)
	}

	i := 0
	for i < len(raw) {
		c := raw[i]
		switch state {
		case stText:
			if c == '$' && i+1 < len(raw) && raw[i+1] == '$' {
				// $$ → literal $
				text.WriteByte('$')
				i += 2
				continue
			}
			if c == '$' && i+1 < len(raw) && raw[i+1] == '{' {
				// Enter expr mode.
				flushText()
				state = stExpr
				exprStart = i
				exprPos = posAt(i)
				exprBuf.Reset()
				depth = 0
				i += 2
				continue
			}
			text.WriteByte(c)
			i++

		case stExpr:
			if c == '\n' {
				return scannedTemplate{}, &dag.ParseError{
					Pos: exprPos,
					Msg: "multi-line interpolation: ${...} must be a single-line expression",
				}
			}
			if c == '"' || c == '\'' {
				// Enter expr-str. Detect triple-quoted to support
				// triple-string string literals inside expressions.
				quote = c
				tripleQ = i+2 < len(raw) && raw[i+1] == c && raw[i+2] == c
				escapeNext = false
				exprBuf.WriteByte(c)
				if tripleQ {
					exprBuf.WriteByte(c)
					exprBuf.WriteByte(c)
					i += 3
				} else {
					i++
				}
				state = stExprStr
				continue
			}
			if c == '{' || c == '[' || c == '(' {
				depth++
				exprBuf.WriteByte(c)
				i++
				continue
			}
			if c == '}' && depth > 0 {
				depth--
				exprBuf.WriteByte(c)
				i++
				continue
			}
			if c == ']' || c == ')' {
				if depth > 0 {
					depth--
				}
				exprBuf.WriteByte(c)
				i++
				continue
			}
			if c == '}' {
				// Closing }. Empty expression → ParseError.
				if exprBuf.Len() == 0 {
					return scannedTemplate{}, &dag.ParseError{
						Pos: exprPos,
						Msg: "empty interpolation: ${} is not allowed",
					}
				}
				parts = append(parts, scanPart{
					Kind: "expr",
					Expr: exprBuf.String(),
					Pos:  exprPos,
				})
				hasInterp = true
				state = stText
				i++
				continue
			}
			exprBuf.WriteByte(c)
			i++

		case stExprStr:
			// Inside a string literal in expr mode. Track \-escapes; }
			// inside is literal. Triple-quote needs three matching quotes
			// in a row to terminate. Newlines in non-triple-quoted strings
			// would itself be a Starlark syntax error caught by ParseExpr
			// later; we don't try to enforce it at scan time.
			exprBuf.WriteByte(c)
			if escapeNext {
				escapeNext = false
				i++
				continue
			}
			if c == '\\' {
				escapeNext = true
				i++
				continue
			}
			if c == quote {
				if tripleQ {
					if i+2 < len(raw) && raw[i+1] == quote && raw[i+2] == quote {
						exprBuf.WriteByte(quote)
						exprBuf.WriteByte(quote)
						i += 3
						state = stExpr
						continue
					}
					i++
					continue
				}
				// Single quote terminator.
				i++
				state = stExpr
				continue
			}
			i++
		}
	}

	switch state {
	case stExpr, stExprStr:
		_ = exprStart // silence unused if a future branch needs it
		return scannedTemplate{}, &dag.ParseError{
			Pos: exprPos,
			Msg: "unterminated interpolation: ${ has no matching }",
		}
	}

	flushText()
	return scannedTemplate{Parts: parts, HasInterpolation: hasInterp}, nil
}

// =============================================================================
// Task 2 — desugarInterpolation + supporting helpers
// =============================================================================

// buildLambdaSource turns a sequence of scanPart into the body of a
// `lambda ctx: ...` expression. The synthesized source ALWAYS wraps each
// expr in str(...) per D4.1-04 — the wrap is unconditional (handles int,
// bool, None, float gracefully).
//
// For an empty parts list this returns `lambda ctx: ""`. The caller
// short-circuits for the no-interpolation case before calling buildLambdaSource;
// this path is defensive.
func buildLambdaSource(parts []scanPart) string {
	var b strings.Builder
	b.WriteString("lambda ctx: ")
	if len(parts) == 0 {
		b.WriteString(`""`)
		return b.String()
	}
	for i, p := range parts {
		if i > 0 {
			b.WriteString(" + ")
		}
		switch p.Kind {
		case "text":
			b.WriteString(starlarkQuote(p.Text))
		case "expr":
			b.WriteString("str(")
			b.WriteString(p.Expr)
			b.WriteString(")")
		}
	}
	return b.String()
}

// starlarkQuote returns a double-quoted Starlark literal of s with
// minimal escaping. Mirrors strconv.Quote for ASCII-friendly output.
//
// Escapes: \ → \\, " → \", \n → \n (literal escape sequence),
// \r → \r, \t → \t. Any other byte is emitted verbatim — Starlark
// allows arbitrary UTF-8 inside double-quoted strings.
func starlarkQuote(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// desugarInterpolation converts a Starlark string literal's content into
// a synthesized *CapturedLambda (D4.1-01). Returns (nil, nil) when no
// ${...} markers are present — the caller stores the literal string
// unchanged. Returns (nil, *dag.ParseError) on scanner errors or invalid
// inner expressions, with the error position attributed to the user's
// source (NOT the synthetic file).
//
// openPos is the position of the OPENING quote of the user's string
// literal; the scanner-computed expr Pos values are relative to it.
//
// Implementation steps (RESEARCH §Pattern 1):
//  1. scan via scanInterpolation; short-circuit if !HasInterpolation.
//  2. Validate each expr part via syntax.ParseExpr — surfaces syntax
//     errors with the user's source position rather than the synthetic
//     file.
//  3. Build "result = lambda ctx: ..." source.
//  4. Cache the synthetic source under "<interp:<file>:<line>:<col>>"
//     in p.fileBytes so D4-02's findCtxAccesses can re-parse it.
//  5. Compile via starlark.ExecFileOptions on a sub-thread that uses
//     the locked D-20 lambda-time globals.
//  6. Pluck globals["result"] as *starlark.Function.
//  7. Build *CapturedLambda via captureLambdaAtPosition with Pos =
//     openPos (user attribution) and BodyPos = the synthetic position.
//  8. Register in p.lambdas and return.
//
// disambiguator (quick 260512-w7c kwargs path): when non-empty, threaded
// to captureLambdaAtPosition where it is appended as ":"+disambiguator
// to the synthesized lambda's ID. Used exclusively by
// desugarActionRefKwargs to distinguish multiple ${...} kwargs sharing a
// single ActionRef call position (otherwise p.lambdas[id] = captured
// would last-wins collide — the first kwarg's lambda would be silently
// overwritten by the second's, so both kwargs would resolve to the
// second value at workflow-resolve time). Every other caller (flow
// name, step name, script id, fail msg, log msg) passes "" — their IDs
// are byte-identical to the pre-fix output.
func (p *Parser) desugarInterpolation(raw string, openPos syntax.Position, disambiguator string) (*dag.CapturedLambda, error) {
	scanned, err := scanInterpolation(raw, openPos)
	if err != nil {
		return nil, err
	}
	if !scanned.HasInterpolation {
		return nil, nil
	}

	// Validate each expr part via syntax.ParseExpr. We use the user file
	// name so any syntax-error attribution lands on user-readable source
	// even if the resulting message line/col are relative to the snippet.
	for _, part := range scanned.Parts {
		if part.Kind != "expr" {
			continue
		}
		if _, perr := syntax.ParseExpr(openPos.Filename(), part.Expr, 0); perr != nil {
			return nil, &dag.ParseError{
				Pos: part.Pos,
				Msg: fmt.Sprintf("interpolation expression %q: %v", part.Expr, perr),
			}
		}
	}

	src := buildLambdaSource(scanned.Parts)
	syntheticName := fmt.Sprintf("<interp:%s:%d:%d>", openPos.Filename(), openPos.Line, openPos.Col)
	fullSrc := "result = " + src
	if p.fileBytes == nil {
		p.fileBytes = make(map[string][]byte)
	}
	p.fileBytes[syntheticName] = []byte(fullSrc)

	// Compile via ExecFileOptions on a sub-thread that imports nothing.
	// Use the locked D-20 lambda-time globals (bridge.LambdaTimeGlobals)
	// so the synthesized lambda runs in the SAME predeclared environment
	// any user-written lambda would. The synthesized lambda itself only
	// uses + concatenation and str(), but parity here means a future
	// hand-written lambda that's later re-routed through this code path
	// behaves identically.
	subThread := &starlark.Thread{Name: "skytime-desugar:" + syntheticName}
	opts := defaultFileOptions()
	globals, err := starlark.ExecFileOptions(opts, subThread, syntheticName, fullSrc, lambdaTimeGlobalsForDesugar())
	if err != nil {
		return nil, &dag.ParseError{
			Pos: openPos,
			Msg: fmt.Sprintf("interpolation: failed to compile synthesized lambda: %v", err),
		}
	}
	fnVal, ok := globals["result"]
	if !ok {
		return nil, &dag.ParseError{Pos: openPos, Msg: "interpolation: internal — synthesized source produced no 'result'"}
	}
	fn, ok := fnVal.(*starlark.Function)
	if !ok {
		return nil, &dag.ParseError{
			Pos: openPos,
			Msg: fmt.Sprintf("interpolation: internal — synthesized 'result' is %s, expected function", fnVal.Type()),
		}
	}

	// BodyPos points at line 1, col 10 of the synthetic file — the
	// position of `lambda` in `result = lambda ctx: ...`. (1-based: chars
	// 1..9 are "result = ", so `lambda` begins at col 10.) Computing this
	// dynamically would require parsing the synthetic file; the prefix is
	// fixed so a constant offset is sufficient and matches what the
	// re-parser will report for the LambdaExpr keyword position.
	const resultPrefix = "result = "
	bodyPos := syntax.MakePosition(&syntheticName, 1, int32(len(resultPrefix)+1))

	return p.captureLambdaAtPosition(fn, openPos, bodyPos, disambiguator)
}

// lambdaTimeGlobalsForDesugar returns the locked D-20 subset used as
// predeclared globals when compiling the synthesized lambda. Delegates
// to bridge.LambdaTimeGlobals (Phase 1 D-20) for a single source of
// truth; a fresh copy is returned per call so callers may mutate it
// without affecting the locked source.
func lambdaTimeGlobalsForDesugar() starlark.StringDict {
	return bridge.LambdaTimeGlobals()
}
