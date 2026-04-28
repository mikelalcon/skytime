package dag

// OperationOutput is the marker interface every extension operation return
// type must implement. Operations cannot return a bare `any` — the type
// system forces authors to declare a typed Output struct that explicitly
// declares it is an OperationOutput.
//
// Why an empty marker rather than something richer? Phase 2 only needs to
// know "is this a legal output?" — Phase 3's interpreter does the type
// switch on concrete types it knows about (extension authors export their
// Output structs and consume them by concrete type at the call site).
// Future v2 schema export iterates over registered output types via
// reflection on the OperationSpec.
//
// Decision reference: D2-03 (locked) — marker interface, not a richer one.
//
// SEAL PROPERTY (deviation from D2-03's `isOperationOutput()` sketch):
// the marker method is EXPORTED (`IsOperationOutput()`) rather than
// unexported. Go's package-private rule for unexported methods would
// prevent any type defined OUTSIDE pkg/dag from satisfying the marker —
// which directly contradicts D2-03's "extension authors write typed Output
// structs" goal (extensions live in pkg/examples/*, not pkg/dag). The
// exported-method form is the standard Go idiom for cross-package
// markers (cf. proto.Message); the seal is social rather than syntactic
// (an external type must explicitly declare the method, which is a
// deliberate opt-in code change reviewers can spot).
//
// Extension-author usage:
//
//	type CreateIssueOutput struct {
//	    Number int
//	    URL    string
//	}
//	func (CreateIssueOutput) IsOperationOutput() {}
//
// Returning a map[string]any from an OperationFunc no longer compiles —
// authors must declare a typed Output struct and implement the marker.
// This is the D2-04 narrowing of OperationFunc.
//
// Audit boundary: every type implementing IsOperationOutput is a Phase 3
// interpreter consumption point. `git grep 'IsOperationOutput()'` lists
// every Output type in the codebase.
type OperationOutput interface {
	// IsOperationOutput is the marker method. Implementations are
	// always empty: `func (T) IsOperationOutput() {}`. The exported
	// name is intentional (see SEAL PROPERTY above).
	IsOperationOutput()
}
