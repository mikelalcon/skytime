// Errors live in pkg/dag (not pkg/parser) so all four foundation packages —
// parser, extension, bridge, dag itself — can construct/return them without a
// circular import. The Phase 1 RESEARCH.md sketch placed these in
// pkg/parser/errors.go; Phase 1 D-04 moved them here. This is a deliberate
// departure: extension and bridge both surface validation/parse errors and
// would otherwise depend on parser, which depends on them.

package dag

import (
	"fmt"
	"strings"

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
// names, unresolvable call_flow targets. Flow, Step, and Action name the
// offending DAG nodes when known.
//
// D4-04 (Phase 4 CONTEXT.md): the Action field carries the operation kind
// (e.g., "github.create_issue") for lints scoped to a specific action ref.
// Lints scoped to the step level leave Action empty; lints scoped to the
// flow level leave both Step and Action empty.
type ValidationError struct {
	Pos     syntax.Position
	Flow    string
	Step    string
	Action  string // D4-04: action kind (e.g., "github.create_issue") for [flow > step > action] error rendering
	Msg     string
	Wrapped error
}

// Error formats as "<file>:<line>:<col> [flow > step > action]: <msg>" when
// Pos is valid and at least one of Flow/Step/Action is non-empty. Empty
// segments inside the bracket are dropped and the remaining ones joined with
// " > ". When all three are empty, the bracket is omitted. When Pos is
// invalid, the position prefix is dropped (existing fallback).
//
// Examples:
//
//	flows/x.star:10:5 [my_flow > step_2 > github.create_issue]: missing kwarg
//	flows/x.star:10:5 [my_flow]: foo
//	flows/x.star:10:5 [my_flow > github.create_issue]: foo  (Step empty)
//	flows/x.star:10:5: bare msg                             (no bracket at all)
//	[my_flow]: bare msg                                     (Pos invalid)
//	bare msg                                                (Pos invalid + no segments)
func (e *ValidationError) Error() string {
	var segments []string
	if e.Flow != "" {
		segments = append(segments, e.Flow)
	}
	if e.Step != "" {
		segments = append(segments, e.Step)
	}
	if e.Action != "" {
		segments = append(segments, e.Action)
	}
	bracket := ""
	if len(segments) > 0 {
		bracket = " [" + strings.Join(segments, " > ") + "]"
	}
	if e.Pos.IsValid() {
		return fmt.Sprintf("%s:%d:%d%s: %s", e.Pos.Filename(), e.Pos.Line, e.Pos.Col, bracket, e.Msg)
	}
	if bracket != "" {
		// Trim the leading space when no position prefix.
		return strings.TrimPrefix(bracket, " ") + ": " + e.Msg
	}
	return e.Msg
}

// Position returns the source position attached to this error.
func (e *ValidationError) Position() syntax.Position { return e.Pos }

// Unwrap exposes the underlying cause for errors.As / errors.Is.
func (e *ValidationError) Unwrap() error { return e.Wrapped }
