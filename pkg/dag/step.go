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
}

// Compile-time guarantee: *Step satisfies the sealed Node interface.
var _ Node = (*Step)(nil)

// Kind returns the discriminator "Step".
func (*Step) Kind() string { return "Step" }

// Position returns the call-site of the `step(...)` declaration.
func (s *Step) Position() syntax.Position { return s.Pos }

// nodeMarker is the seal — unexported so only pkg/dag types can satisfy Node.
func (*Step) nodeMarker() {}
