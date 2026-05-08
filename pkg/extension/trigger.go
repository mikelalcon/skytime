package extension

import (
	"github.com/mikelalcon/skytime/pkg/dag"
)

// TriggerSource is the sealed extension-side type representing a trigger
// source factory's value. Phase 7 ships zero concrete TriggerSource types —
// Phase 7.1's first source (github.WebhookSource) is the first real
// implementation. Sources live under their owning extension's namespace
// (D-07-08): GitHub triggers live under pkg/extension/builtin/github,
// generic webhook triggers under examples/http-github-webhook/extensions/webhook,
// cron under TBD-7.2.
//
// Sealed via the unexported triggerSourceMarker() method. Only types
// declared inside pkg/extension or sub-packages it owns can implement
// TriggerSource. Mirrors the seal pattern from extension.Credential
// (isCredential()) and dag.Node (nodeMarker()).
//
// Every TriggerSource implementation must:
//   - Return a stable string Kind() (e.g. "github.webhook"). Used as the
//     JSON discriminator AND the startup banner label
//     ("source-kind → flow-name").
//   - Return a fixed []string ReqSchema() listing valid req.<field>
//     attributes for the parser-time req-walker (Plan 03).
//   - Implement MarshalJSON() producing the {kind, config} envelope.
//     CRITICAL: config carries credential ID strings only — never an
//     extension.Secret, never a resolved credential value. Verified by
//     tests/firewall_credential_redaction_test.go (Plan 06).
//   - Satisfy dag.TriggerSource (Kind + MarshalJSON) so the value flows
//     through *dag.Trigger.Source — guaranteed compile-time by the
//     `var _ dag.TriggerSource = TriggerSource(nil)` assertion below.
type TriggerSource interface {
	// Kind returns the discriminator (e.g. "github.webhook"). Used in
	// JSON marshaling and the startup banner.
	Kind() string

	// ReqSchema returns the valid req.<field> attribute names available
	// to the trigger's map and idempotency_key lambdas. Used by the
	// parser-time req-walker (pkg/parser/req_walk.go) to surface
	// "req.payloud" → "did you mean: req.payload?" errors.
	ReqSchema() []string

	// MarshalJSON produces the {kind, config} envelope. config MUST
	// contain credential ID strings only — never resolved Secret values
	// (D-07-09). Implementations are responsible for ensuring no
	// Secret-typed field reaches the marshaled bytes.
	MarshalJSON() ([]byte, error)

	// triggerSourceMarker is the seal — unexported, callable only from
	// pkg/extension or sub-packages.
	triggerSourceMarker()
}

// Compile-time assertion: every extension.TriggerSource MUST satisfy
// dag.TriggerSource. If a concrete TriggerSource type is added that
// somehow doesn't satisfy dag.TriggerSource (e.g., signature drift),
// the build breaks here. Static guarantee that *dag.Trigger.Source can
// always hold an extension.TriggerSource value.
var _ dag.TriggerSource = TriggerSource(nil)
