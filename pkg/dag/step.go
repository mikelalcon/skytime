package dag

import "go.starlark.net/syntax"

// Step is a sequential I/O step. The Starlark surface offers two flavors:
//
//	step(action=gh.create_issue(...))                  # one ActionRef
//	step(block=[gh.create_issue(...), gh.add_label(...)]) # batched ActionRefs
//
// Both flavors land here as Actions: a slice of length 1 or >1. Phase 2's
// generic activity reads Actions and dispatches them sequentially in a single
// invocation to keep Temporal history bounded.
//
// The Starlark surface accepts four mutually-exclusive shapes per D4.1-06:
// action= / block= / action_fn= / block_fn=. Exactly one MUST be set; the
// parser enforces.
type Step struct {
	// Pos is the call-site of `step(...)` in the source .star file.
	Pos syntax.Position

	// Actions holds the ActionRef batch. Length 1 for the action= form,
	// length >1 for the block= form. Never empty in a parsed flow (the
	// parser rejects empty-block / no-action steps).
	Actions []*ActionRef

	// Retry is the optional Temporal RetryPolicy from DSL-08. nil when not
	// specified; semantics ("nil = SDK default") are Phase 2's call.
	Retry *RetryPolicy

	// Timeout is the optional StartToClose / ScheduleToStart timeout pair
	// from DSL-08. nil when not specified.
	Timeout *Timeout

	// TaskQueue is the optional per-step Temporal task queue override per D3-19.
	// Hierarchy: step.TaskQueue > flow.TaskQueue > worker default.
	// Set via the parser's `step(..., task_queue="...")` kwarg.
	TaskQueue string `json:"task_queue,omitempty"`

	// Name is the optional display name for the step. When non-empty the
	// renderer prefers this over the auto-derived "<kind>(<label>)". Set
	// from the parser's `step(name="...")` kwarg (D4.1-15). Empty string
	// means "fall back to the auto-derived label".
	Name string `json:"name,omitempty"`

	// NameFn is the resolved-at-runtime variant of Name. Set by the
	// parser's interpolation desugarer when `step(name="...${ctx.x}...")`
	// contains ${...} markers (D4.1-15 + D4.1-01..05). The interpreter
	// evaluates this lambda before activity dispatch and uses the result
	// as the display name. Mutually exclusive with Name (parser enforces).
	NameFn *CapturedLambda `json:"-"`

	// ActionFn is the lambda variant of Action. Set by the parser's
	// `step(action_fn=lambda ctx: ext.op(...))` kwarg (D4.1-06). The
	// interpreter evaluates this lambda inside the workflow at dispatch
	// time; the lambda MUST return a single *ActionRef (D4.1-07). Mutually
	// exclusive with Actions/BlockFn (parser enforces).
	ActionFn *CapturedLambda `json:"-"`

	// BlockFn is the lambda variant of Block. Set by the parser's
	// `step(block_fn=lambda ctx: [ext.op(...) for ...])` kwarg (D4.1-06).
	// The interpreter evaluates this lambda inside the workflow at
	// dispatch time; the lambda MUST return a Starlark list of *ActionRef
	// (D4.1-07). Mutually exclusive with Actions/ActionFn (parser
	// enforces). Empty-list result short-circuits per D4.1-12 / RESEARCH
	// Open Question 3.
	BlockFn *CapturedLambda `json:"-"`
}

// Compile-time guarantee: *Step satisfies the sealed Node interface.
var _ Node = (*Step)(nil)

// Kind returns the discriminator "Step".
func (*Step) Kind() string { return "Step" }

// Position returns the call-site of the `step(...)` declaration.
func (s *Step) Position() syntax.Position { return s.Pos }

// nodeMarker is the seal — unexported so only pkg/dag types can satisfy Node.
func (*Step) nodeMarker() {}
