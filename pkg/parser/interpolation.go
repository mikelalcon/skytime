package parser

import (
	"strings"

	"go.starlark.net/syntax"

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
		exprStart  int             // byte offset of opening ${ in raw
		exprBuf    strings.Builder // accumulates the inner expression text
		exprPos    syntax.Position // user-source position of opening ${
		depth      int             // bracket nesting in expr mode (counts {[(])
		quote      byte            // active quote char in expr-str mode
		tripleQ    bool            // true when in triple-quoted literal
		escapeNext bool            // last char in expr-str was \
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
