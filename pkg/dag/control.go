package dag

import (
	"fmt"

	"go.starlark.net/syntax"
)

// IfCond is a conditional branch. The cond= kwarg in Starlark is a lambda
// captured at parse time; LambdaID resolves into WorkflowInput.Lambdas.
type IfCond struct {
	Pos      syntax.Position
	LambdaID string // resolves into WorkflowInput.Lambdas (D-18 format)
	// OutputAlias is the parser-side mode switch for D4.2 expression-mode
	// if_cond. Zero value (empty string) preserves today's procedural
	// behavior verbatim — every existing fixture and downstream consumer
	// continues to work unchanged. Non-empty value enables D4.2-09
	// expression-mode rules: each branch must end in *Result (or *Fail at
	// most one side); branch-result keys + types must structurally match;
	// after the if_cond, the resolved value binds into ctx.<OutputAlias>.
	OutputAlias string
	Then        []Node // branch taken when cond evaluates truthy
	Else        []Node // branch taken when cond evaluates falsy; empty slice (or nil — both fine) when else_= omitted
}

var _ Node = (*IfCond)(nil)

// Kind returns the discriminator "IfCond".
func (*IfCond) Kind() string { return "IfCond" }

// Position returns the call-site of `if_cond(...)`.
func (n *IfCond) Position() syntax.Position { return n.Pos }

func (*IfCond) nodeMarker() {}

// Script is a state-mutation node — its lambda computes a new state value and
// stores it under OutputAlias.
type Script struct {
	Pos         syntax.Position
	ID          string // human-readable script id from the `id=` kwarg
	LambdaID    string // resolves into WorkflowInput.Lambdas
	OutputAlias string // the state key under which the lambda's return value is stored

	// IDFn is the resolved-at-runtime variant of ID. Set by the parser's
	// interpolation desugarer when `script(id="..._${ctx.x}_...")`
	// contains ${...} markers (D4.1-02). The interpreter evaluates this
	// lambda before the script body fires and uses the result as the
	// runtime ID string (visible in slog events). Mutually exclusive with
	// ID at the runtime layer; the parser keeps the LITERAL template in
	// ID for cross-script keys (mirrors D4.1-16 flow.Name handling).
	IDFn *CapturedLambda `json:"-"`
}

var _ Node = (*Script)(nil)

// Kind returns the discriminator "Script".
func (*Script) Kind() string { return "Script" }

// Position returns the call-site of `script(...)`.
func (n *Script) Position() syntax.Position { return n.Pos }

func (*Script) nodeMarker() {}

// LogStep emits a structured log record at workflow time, routed through
// workflow.GetLogger(ctx) at the matching slog level (info/warn/error/debug).
// Side-channel only — produces no output_alias state and does not advance
// the human-mode [N/M] step counter (D-7.2.1-13/16, Phase 07.2.1).
//
// Mirrors *Script minus OutputAlias plus a Level discriminator. Like Script,
// the MsgFn lambda is set when the parser desugars ${ctx.expr} interpolation
// in the message template (D4.1-22 carve-out: parse-time syntactic sugar
// that desugars to a native Starlark lambda; no runtime template engine).
type LogStep struct {
	// Pos is the call-site of `log.<level>(...)` in the .star source —
	// used by D-04 error attribution and by parse-time validators.
	Pos syntax.Position

	// Level is one of "info" | "warn" | "error" | "debug" (D-7.2.1-01).
	// The parser is the level gate; this field is a plain string here to
	// keep pkg/dag free of parser-level concerns. The walker switches on
	// this value to dispatch into workflow.GetLogger(ctx).<Level>.
	Level string

	// Msg is the LITERAL template the .star author wrote — including any
	// ${ctx.expr} markers. Cosmetic edits to this string change the
	// file content hash and therefore the (synthesized) MsgFn.ID — same
	// stability contract as Script.ID / Script.IDFn.
	Msg string

	// MsgFn is the resolved-at-runtime variant of Msg. Set by the parser's
	// interpolation desugarer (D4.1-22) when Msg contained ${...} markers.
	// The walker evaluates this lambda to produce the rendered message
	// string. Nil when Msg is a plain literal with no interpolation.
	//
	// json:"-" because *CapturedLambda holds a *starlark.Function pointer
	// that does not round-trip through Temporal's JSON serializer
	// (mirrors Script.IDFn precedent above).
	MsgFn *CapturedLambda `json:"-"`

	// AttrsLambdaID resolves into WorkflowInput.Lambdas at workflow time.
	// Empty string means no attrs= kwarg was provided; non-empty means the
	// walker must evaluate the lambda and convert the returned dict into
	// slog attrs (D-7.2.1-05).
	AttrsLambdaID string
}

var _ Node = (*LogStep)(nil)

// Kind returns the discriminator "LogStep".
func (*LogStep) Kind() string { return "LogStep" }

// Position returns the call-site of `log.<level>(...)`.
func (n *LogStep) Position() syntax.Position { return n.Pos }

func (*LogStep) nodeMarker() {}

// ForEachParallel fans out the same step body across a collection. Items can
// come from a static literal (carried as ItemsLiteral) OR from a lambda that
// produces them at execute time (carried as ItemsLambdaID). Exactly one of
// the two is set in a well-formed node — see Validate().
type ForEachParallel struct {
	Pos syntax.Position

	// ItemsLambdaID is set when the items= kwarg is a lambda; resolves into
	// WorkflowInput.Lambdas. Empty string means items= was a static literal
	// (see ItemsLiteral).
	ItemsLambdaID string

	// ItemsLiteral is set when items= was a static list/tuple. Pure data
	// (strings/numbers/dicts/lists) — the parser flattens Starlark literals
	// into Go values here. nil means items= was a lambda (see ItemsLambdaID).
	ItemsLiteral []any

	// ItemVar is the iteration variable name passed via the `item` kwarg.
	ItemVar string

	// Steps is the body executed per item.
	Steps []Node

	// MaxConcurrency is the optional fan-out cap per D3-13. Zero or negative
	// is reserved — the parser rejects negatives at parse time, and zero
	// means "use interpreter default (10)". Set via the parser's
	// `for_each_parallel(..., max_concurrency=N)` kwarg.
	MaxConcurrency int `json:"max_concurrency,omitempty"`

	// Retry / Timeout are DSL-08 kwargs forwarded from for_each_parallel(...).
	Retry   *RetryPolicy
	Timeout *Timeout
}

var _ Node = (*ForEachParallel)(nil)

// Kind returns the discriminator "ForEachParallel".
func (*ForEachParallel) Kind() string { return "ForEachParallel" }

// Position returns the call-site of `for_each_parallel(...)`.
func (n *ForEachParallel) Position() syntax.Position { return n.Pos }

func (*ForEachParallel) nodeMarker() {}

// Validate enforces the exactly-one-of invariant on items: precisely one of
// ItemsLambdaID and ItemsLiteral must be set. Returns a non-nil error
// otherwise. Plan 01-05 will surface this through ValidationError; Phase 1
// keeps the helper local to pkg/dag.
func (n *ForEachParallel) Validate() error {
	hasLambda := n.ItemsLambdaID != ""
	hasLiteral := n.ItemsLiteral != nil
	if hasLambda && hasLiteral {
		return fmt.Errorf("for_each_parallel: must set exactly one of ItemsLambdaID or ItemsLiteral, got both")
	}
	if !hasLambda && !hasLiteral {
		return fmt.Errorf("for_each_parallel: must set exactly one of ItemsLambdaID or ItemsLiteral, got neither")
	}
	return nil
}

// CallFlow is a child-workflow invocation by flow name. The parser performs a
// final cross-flow resolution pass after every loaded file's flow() calls have
// landed (D-16) — at that point Resolved is set to the target Flow. Plan 02
// just declares the field; plan 05 fills it in.
type CallFlow struct {
	Pos syntax.Position

	// Name is the flow identifier passed to call_flow(...). Resolved against
	// the parser session's flow map after all loaded files are processed.
	Name string

	// Inputs is the kwarg map passed to the called flow. Pure data only —
	// lambdas are not allowed inside CallFlow inputs (D-19 enforces this in
	// the parser).
	Inputs map[string]any

	// ChildOptions is forwarded as-is to Phase 3, which will translate it to
	// Temporal child workflow options. Pure data.
	ChildOptions map[string]any

	// Resolved points to the target flow after the parser's cross-flow
	// resolution pass. Marked json:"-" so it does not serialize — golden
	// tests would otherwise include the entire transitive flow graph.
	Resolved *Flow `json:"-"`
}

var _ Node = (*CallFlow)(nil)

// Kind returns the discriminator "CallFlow".
func (*CallFlow) Kind() string { return "CallFlow" }

// Position returns the call-site of `call_flow(...)`.
func (n *CallFlow) Position() syntax.Position { return n.Pos }

func (*CallFlow) nodeMarker() {}
