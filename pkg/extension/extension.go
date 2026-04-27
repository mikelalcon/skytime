// Package extension defines the SDK contract every library-developer-authored
// extension implements. A Phase-6 extension (e.g. pkg/examples/github,
// pkg/examples/http, pkg/examples/slack) implements the Extension interface
// here, declares its OperationSpecs (each with a *bool Idempotent — D-12), and
// registers itself with a per-parser Registry (D-07).
//
// Firewall: this package MUST NOT import any go.temporal.io/* package. The
// firewall is enforced at test-time by TestNoTemporalImportsInExtensionPackage,
// which walks every Go source file's imports via go/parser. Crossing this
// firewall would re-introduce the workflow.Context/activity coupling that
// PROJECT.md "no context bleed" forbids.
//
// To implement an extension, satisfy this Extension interface and register
// with `parser.Register(yourExt)` (Phase 1 plan 04 wires Register into the
// parser). See pkg/examples/* for reference implementations once Phase 6
// lands.
package extension

import "go.starlark.net/starlark"

// Extension is the SDK contract every library-developer-authored extension
// implements.
//
// LIFECYCLE: Initialize is called ONCE per parser at extension Register time —
// NOT once per Starlark invocation. The returned starlark.Value is bound as
// the global keyed by Name() in the parser's predeclared globals. From the
// .star author's view (the D-08 example):
//
//	github = <returned starlark.Value>            # bound once at parser registration
//	gh = github.endpoint("admin")                 # attribute lookup + call → returns sub-value
//	gh.create_issue(repo="x", title="y")          # attribute lookup + call → returns *dag.ActionRef
//
// The returned value MUST be attribute-bearing (typically *starlarkstruct.Module
// or any starlark.Value implementing starlark.HasAttrs) so the .star author can
// write `github.endpoint(...)` and `github.<op>(...)`. The attribute named
// "endpoint" is a *starlark.Builtin returning a credential-aware sub-Module
// that carries the credential ID; subsequent operation attributes on that
// sub-Module build *dag.ActionRef intents embedding the resolved credential
// ID — credentials never enter workflow state (PROJECT.md security constraint).
type Extension interface {
	// Name returns the Starlark-namespace identifier (e.g. "github"). The
	// parser binds this name as a top-level global to the value returned by
	// Initialize.
	Name() string

	// Initialize is called ONCE per parser at Register time (not per
	// Starlark invocation). The returned attribute-bearing Starlark value
	// (typically *starlarkstruct.Module) becomes the global keyed by
	// Name().
	//
	// The returned value's attributes MUST include:
	//   - one attribute per operation declared by Operations() (each a
	//     *starlark.Builtin that, when called, returns a *dag.ActionRef);
	//   - additional factory attributes (e.g., "endpoint") that return a
	//     credential-aware sub-Module — the sub-Module's attributes are
	//     *starlark.Builtins closing over the credential ID and producing
	//     *dag.ActionRef instances with CredentialID populated.
	//
	// The kwargs parameter is reserved for parser-level Initialize options
	// (Phase 1: usually empty). It is NOT the per-call kwargs of an
	// extension operation — those flow through the *starlark.Builtin
	// attribute call site.
	Initialize(thread *starlark.Thread, kwargs []starlark.Tuple) (starlark.Value, error)

	// Operations returns the per-extension operation map keyed by operation
	// name (e.g. "create_issue"). The Registry consults this at registration
	// time to verify each operation has Idempotent != nil (D-12). The parser
	// uses these specs to validate operation kwargs at the *starlark.Builtin
	// call site (see schema.go's UnpackOperationKwargs).
	Operations() map[string]*OperationSpec
}
