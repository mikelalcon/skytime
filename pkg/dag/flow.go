package dag

import "go.starlark.net/syntax"

// Flow is the top-level workflow declaration. A .star file's `flow(...)` call
// produces one of these. Multi-flow-per-file is allowed (D-15) — each flow()
// call yields a distinct *Flow keyed by Name in the parser's session map.
type Flow struct {
	// Pos is the call-site of `flow(...)` in the source .star file.
	Pos syntax.Position

	// Name is the unique flow identifier within the parser session.
	// Duplicate names across loaded files are a parse error (D-15).
	Name string

	// Inputs is the declared input schema as kwarg-name → declared-type-hint
	// (string/int/dict/list/...). Pure-data; the parser uses this to validate
	// CallFlow inputs at parse-time and (in Phase 4) to drive static schema
	// validation.
	Inputs map[string]string

	// Body is the sequence of child nodes — Step / IfCond / Script /
	// ForEachParallel / CallFlow. Heterogeneous by design: a flow's body is
	// a list, not a typed slice.
	Body []Node

	// TaskQueue is the optional Temporal task queue override per D3-19.
	// Empty string means "inherit from worker default" (typically "skytime").
	// Set via the parser's `flow(..., task_queue="...")` kwarg.
	TaskQueue string `json:"task_queue,omitempty"`
}

// Compile-time guarantee: *Flow satisfies the sealed Node interface.
var _ Node = (*Flow)(nil)

// Kind returns the discriminator "Flow".
func (*Flow) Kind() string { return "Flow" }

// Position returns the call-site of the `flow(...)` declaration.
func (f *Flow) Position() syntax.Position { return f.Pos }

// nodeMarker is the seal — unexported so only pkg/dag types can satisfy Node.
func (*Flow) nodeMarker() {}
