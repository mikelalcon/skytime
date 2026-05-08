package dag

import (
	"go.starlark.net/syntax"
)

// TriggerSource is the dag-side view of pkg/extension.TriggerSource.
//
// Plan 01 ships this minimal interface (Kind + MarshalJSON) because Plan 02
// (pkg/extension/trigger.go) will need to import pkg/dag, so pkg/dag cannot
// import pkg/extension (cycle). Concrete TriggerSource types (e.g.
// github.WebhookSource in Phase 7.1) will satisfy BOTH this dag-local
// interface AND extension.TriggerSource (which adds ReqSchema and the
// unexported triggerSourceMarker seal).
//
// The seal lives in pkg/extension; the dag package does not enforce the
// marker because that would force pkg/extension to be a forward dep of
// pkg/dag. Instead, this is a structural interface: any value satisfying
// Kind() string + MarshalJSON() ([]byte, error) can flow through *Trigger.
// In practice, only types that ALSO satisfy extension.TriggerSource will
// reach a *Trigger because the parser builtin (Plan 03) accepts only
// extension.TriggerSource values; the dag-local interface here is a
// compilation seam, not a security boundary.
type TriggerSource interface {
	Kind() string
	MarshalJSON() ([]byte, error)
}

// Trigger is a top-level declaration produced by the Starlark builtin
// trigger(...). It is NOT a dag.Node — Triggers never appear inside
// flow.Body. They are stored in interpreter.TriggerRegistry alongside
// FlowRegistry. Sealing semantics mirror dag.Node's pattern (see
// CONTEXT.md key_constraints; Pitfall 11 in RESEARCH.md): Trigger
// satisfies a parallel sealed-marker idiom but NOT the Node interface
// literally, so flow.Body walkers don't need defensive cases.
//
// Trigger is intentionally NOT a dag.Node. flow.Body walkers (parser
// finalize passes, interpreter walkSteps) iterate []Node and would
// need defensive case *dag.Trigger arms if Trigger satisfied the
// Node seal. Triggers are top-level declarations stored in
// interpreter.TriggerRegistry, never inside flow.Body. See RESEARCH.md
// § Pitfall 11 for the full reasoning. CONTEXT.md key_constraints
// mentions "Trigger satisfies the existing dag.Node seal" — read as
// "uses the same sealed-marker idiom", NOT "implements Node literally".
//
// Sealed via the unexported triggerSourceMarker() method in pkg/extension
// (TriggerSource interface). Plan 01 uses a dag-local interface that
// pkg/extension.TriggerSource will satisfy in Plan 02.
//
// Pos is the call-site of the trigger(...) builtin. EXCLUDED from JSON
// for cross-machine stability (mirrors the convention from
// pkg/dag/marshal.go header — syntax.Position carries an absolute
// Filename which differs between machines, breaking golden stability
// across CI/laptop boundaries).
//
// Credential contract (D-07-09 / D-07-10): Trigger carries CredentialID
// (a string ID into the credential handler's keyspace). Trigger NEVER
// serializes an extension.Secret, never serializes a resolved credential.
// Only the credential ID string crosses any JSON boundary. JIT resolution
// happens inside the receiver in Phase 7.1; even there, the resolved
// Secret is wrapped in extension.Secret which redacts on every marshaling
// path.
type Trigger struct {
	Pos               syntax.Position
	FlowName          string
	Source            TriggerSource
	MapLambda         *CapturedLambda
	IdempotencyLambda *CapturedLambda
	CredentialID      string
	frozen            bool
}

// Kind returns the discriminator used in DAG marshaling. Always "Trigger".
func (t *Trigger) Kind() string { return "Trigger" }

// Position returns the Pos field for parse-error attribution.
func (t *Trigger) Position() syntax.Position { return t.Pos }

// Freeze recursively freezes Source (if non-nil and the concrete type
// implements Freeze), MapLambda.Fn, and IdempotencyLambda.Fn. Idempotent:
// the second Freeze call is a no-op.
//
// Source.Freeze is best-effort — TriggerSource's dag-local interface does
// not require a Freeze method (the seal lives in pkg/extension, and
// concrete sources without mutable state need no freezing). The type
// assertion `interface{ Freeze() }` covers concrete sources that do
// implement Freeze (e.g. anything wrapping a *starlark.Dict).
func (t *Trigger) Freeze() {
	if t.frozen {
		return
	}
	t.frozen = true
	if t.MapLambda != nil && t.MapLambda.Fn != nil {
		t.MapLambda.Fn.Freeze()
	}
	if t.IdempotencyLambda != nil && t.IdempotencyLambda.Fn != nil {
		t.IdempotencyLambda.Fn.Freeze()
	}
	if f, ok := t.Source.(interface{ Freeze() }); ok {
		f.Freeze()
	}
}
