// Errors live in pkg/dag (not pkg/parser) so all four foundation packages —
// parser, extension, bridge, dag itself — can construct/return them without a
// circular import. The Phase 1 RESEARCH.md sketch placed these in
// pkg/parser/errors.go; Phase 1 D-04 moved them here. This is a deliberate
// departure: extension and bridge both surface validation/parse errors and
// would otherwise depend on parser, which depends on them.

package dag

import (
	"fmt"

	"go.starlark.net/syntax"
)

// ParseError reports a problem turning a `.star` file into a *dag.Flow:
// syntax errors, unknown identifiers, sandbox violations, illegal lambda
// captures. Pos points at the offending source location when available.
type ParseError struct {
	Pos     syntax.Position
	Msg     string
	Wrapped error
}

// Error formats as "<file>:<line>:<col>: <msg>" when Pos is valid; otherwise
// returns just Msg so callers can attribute errors raised before any source
// location is known.
func (e *ParseError) Error() string {
	if e.Pos.IsValid() {
		return fmt.Sprintf("%s:%d:%d: %s", e.Pos.Filename(), e.Pos.Line, e.Pos.Col, e.Msg)
	}
	return e.Msg
}

// Position returns the source position attached to this error.
func (e *ParseError) Position() syntax.Position { return e.Pos }

// Unwrap exposes the underlying cause for errors.As / errors.Is.
func (e *ParseError) Unwrap() error { return e.Wrapped }

// ValidationError reports a parse-time semantic problem against an extension
// schema or a flow-level invariant: missing required kwargs, duplicate flow
// names, unresolvable call_flow targets. Flow and Step name the offending DAG
// nodes when known.
type ValidationError struct {
	Pos     syntax.Position
	Flow    string
	Step    string
	Msg     string
	Wrapped error
}

// Error formats as "<file>:<line>:<col>: <msg>" when Pos is valid; otherwise
// returns just Msg.
func (e *ValidationError) Error() string {
	if e.Pos.IsValid() {
		return fmt.Sprintf("%s:%d:%d: %s", e.Pos.Filename(), e.Pos.Line, e.Pos.Col, e.Msg)
	}
	return e.Msg
}

// Position returns the source position attached to this error.
func (e *ValidationError) Position() syntax.Position { return e.Pos }

// Unwrap exposes the underlying cause for errors.As / errors.Is.
func (e *ValidationError) Unwrap() error { return e.Wrapped }
