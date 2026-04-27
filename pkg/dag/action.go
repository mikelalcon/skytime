package dag

import (
	"fmt"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// ActionRef is the Command-Pattern intent returned by an extension factory
// (e.g., `gh.create_issue(repo=..., title=...)`) and consumed by step() /
// step(block=[...]) on the Go side. It is BOTH:
//
//  1. a pure-data DAG node element (lives inside Step.Actions and
//     ForEachParallel.Steps' inner Steps), and
//  2. a custom starlark.Value (Starlark sees it as a single opaque object
//     that can be passed to step()).
//
// Pitfall #6 (freeze audit) is closed here: Freeze() recursively freezes the
// Kwargs *starlark.Dict so consultants cannot mutate kwargs after the parse
// completes. Hashability is explicitly disabled — ActionRef is not a hashable
// value (it has no canonical equality story across reparses).
type ActionRef struct {
	// Pos is the call-site of the extension factory call that produced this
	// ActionRef. Carried along for D-04 error attribution if a later
	// validation pass complains about this specific action.
	Pos syntax.Position

	// Kind_ is the operation fingerprint, e.g. "github.create_issue". Named
	// with a trailing underscore so it does not collide with the Node.Kind()
	// method (ActionRef is NOT a Node — it lives inside Step.Actions). Public
	// access uses ActionKind() to make the distinction obvious at call sites.
	Kind_ string

	// Kwargs is the operation's kwarg payload as a Starlark dict (mutable
	// until Freeze() is called). Phase 2's generic activity reads this dict
	// to drive the operation invocation.
	Kwargs *starlark.Dict

	// CredentialID is the credential ID embedded by the extension factory
	// when the consultant constructed the extension instance
	// (`gh = github.endpoint("admin")`). Workflow state, ActionRef, and
	// Temporal history must never contain a resolved secret (D-08).
	CredentialID string

	// frozen tracks whether Freeze() has run; lets Freeze be idempotent.
	frozen bool
}

// Compile-time guarantee: *ActionRef satisfies starlark.Value.
var _ starlark.Value = (*ActionRef)(nil)

// String returns a debug-friendly representation: "ActionRef(<kind>)".
func (a *ActionRef) String() string { return fmt.Sprintf("ActionRef(%s)", a.Kind_) }

// Type returns the Starlark type tag.
func (a *ActionRef) Type() string { return "ActionRef" }

// Truth marks every ActionRef as truthy — unconditional. Used by `if
// action_ref:` style checks in Starlark, which we discourage but accept.
func (a *ActionRef) Truth() starlark.Bool { return starlark.True }

// Hash declares unhashability. ActionRef has no canonical equality across
// reparses (file-content-hash IDs change with cosmetic edits, dict ordering,
// etc.), so refusing hashing prevents misuse as a map key.
func (a *ActionRef) Hash() (uint32, error) {
	return 0, fmt.Errorf("ActionRef is not hashable")
}

// Freeze enforces immutability. Per the Starlark spec
// (go.starlark.net/doc/impl.md), every value type must implement Freeze and
// cascade it to every value it contains. This is the recursive freeze that
// Pitfall #6 demands: the inner Kwargs *Dict MUST also become frozen so
// consultants cannot mutate kwargs after the parse completes.
//
// Freeze is idempotent — calling it twice is a no-op.
func (a *ActionRef) Freeze() {
	if a.frozen {
		return
	}
	a.frozen = true
	if a.Kwargs != nil {
		a.Kwargs.Freeze() // cascade — *Dict.Freeze() in turn cascades to its values.
	}
}

// ActionKind returns the operation fingerprint (Kind_ field). Named to avoid
// the ambiguity of an exported `Kind` field that would shadow Node.Kind().
func (a *ActionRef) ActionKind() string { return a.Kind_ }
