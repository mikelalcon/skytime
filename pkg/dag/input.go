// Package comment retained: WorkflowInput is the payload Phase 3 passes
// across the Temporal serialization boundary. Per D3-01..D3-05 (locked
// 2026-04-29):
//   - Lambda *starlark.Function values do NOT cross the wire; only
//     {FlowName, ContentHash, InitState} does.
//   - The worker looks up the parsed flow + lambda map from its in-memory
//     registry by (FlowName, ContentHash) on every workflow tick (including
//     replays). See pkg/interpreter (Phase 3) for the registry implementation.
//   - ContentHash is sha256 of the .star file bytes that defined FlowName,
//     computed by the workflow trigger and recorded in history naturally
//     via WorkflowInput (NOT via workflow.SideEffect).

package dag

// WorkflowInput is the JSON-serializable payload SkytimeWorkflow receives.
// All three fields are required for production execution; tests may pass
// partial inputs and rely on the registry-side error path (D3-06).
//
// Phase 3 rewrite (D3-04, D3-05): Phase 1's `{Flow, Lambdas, InitState}`
// shape is replaced with {FlowName, ContentHash, InitState}. The full
// dag.Flow and the lambda map are NOT embedded — the worker resolves them
// from its in-memory registry at workflow tick time. This keeps the wire
// format minimal and matches the Build-ID + filesystem-snapshot deployment
// model.
//
// No custom MarshalJSON is needed: the Phase 1 implementation existed only
// to omit *starlark.Function values from Lambdas; Phase 3's WorkflowInput
// has no such fields, so the default encoding/json marshaling produces
// stable, sorted-key output.
type WorkflowInput struct {
	// FlowName identifies a flow in the worker's registry.
	FlowName string `json:"flow_name"`

	// ContentHash is the sha256 (hex) of the .star file bytes that defined
	// FlowName at worker boot. Hex-encoded sha256 — full 64-char string.
	// The worker uses (FlowName, ContentHash) as the registry key (D3-04).
	ContentHash string `json:"content_hash"`

	// InitState is the initial workflow state — pure data from the trigger
	// (HTTP request, signal, scheduler). nil is allowed and marshals to
	// JSON null.
	InitState map[string]any `json:"init_state"`
}
