package dag

import "go.starlark.net/syntax"

// Node is the sealed interface every DAG node implements.
//
// Phase 3's interpreter walks a parsed flow with a type switch:
//
//	switch n := node.(type) {
//	case *Flow:            // ...
//	case *Step:            // ...
//	case *IfCond:          // ...
//	case *Script:          // ...
//	case *ForEachParallel: // ...
//	case *CallFlow:        // ...
//	}
//
// The interface is sealed via the unexported nodeMarker() method — only types
// declared in pkg/dag can implement Node. This prevents Phase 3 (and any later
// consumer) from accidentally widening the type set without a deliberate change
// to pkg/dag. To verify the seal, attempt `type FakeNode struct{}` outside this
// package with the three Node methods; the compiler rejects with
// "cannot use ... as Node value" because nodeMarker is unexported.
type Node interface {
	// Kind returns the discriminator string ("Flow", "Step", "IfCond",
	// "Script", "ForEachParallel", "CallFlow"). Used both as the JSON
	// `kind` field (see pkg/dag/marshal.go) and as a debug aid.
	Kind() string

	// Position returns the .star call-site syntax.Position used by D-04
	// error messages. Plan 01-01 fixed the Position-bearing error contract
	// (ParseError, ValidationError); this method is how nodes feed those
	// errors with their source location.
	Position() syntax.Position

	// nodeMarker is the seal. Unexported so external packages cannot satisfy
	// Node by accident.
	nodeMarker()
}
