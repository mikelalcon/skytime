package dag

import "go.starlark.net/syntax"

// Fail is a node-builtin that raises a NonRetryableApplicationError at
// runtime. Two-field shape mirrors Step.Name/Step.NameFn (D4.1-15) and
// Script.ID/Script.IDFn (D4.1-02): Message holds the literal template
// ("repo ${ctx.repo} not found"); MessageFn is the synthesized lambda
// populated by the parser when ${...} markers are present. The
// interpreter evaluates MessageFn if non-nil; otherwise emits Message
// verbatim. Eval errors fall back to the literal Message (display safety
// — the failure semantics still raise).
//
// Top-level vs lambda-time fail: see pkg/parser/doc.go. Same name, two
// predeclared environments.
type Fail struct {
	Pos       syntax.Position
	Message   string
	MessageFn *CapturedLambda
}

var _ Node = (*Fail)(nil)

// Kind returns the discriminator "Fail".
func (*Fail) Kind() string { return "Fail" }

// Position returns the call-site of `fail(...)`.
func (n *Fail) Position() syntax.Position { return n.Pos }

func (*Fail) nodeMarker() {}
