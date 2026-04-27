package dag

import (
	"encoding/json"
	"sort"
)

// WorkflowInput is the payload Phase 3 will pass across the Temporal
// serialization boundary into a workflow execution. Phase 1 only locks the
// shape; Phase 3 owns the actual serialization strategy for the embedded
// *starlark.Function values inside Lambdas (custom DataConverter vs.
// re-parse-on-start — see Phase 3 entry-gate decision).
//
// Keeping this struct in its own file (rather than inside flow.go) is a
// readability call: Flow describes the *graph*, while WorkflowInput is the
// runtime carrier that wraps a Flow alongside its captured lambdas and the
// initial state. They're cohesive at the package level but not at the file
// level — splitting keeps each file's concept count low.
type WorkflowInput struct {
	// Flow is the parsed graph. Always required.
	Flow *Flow `json:"flow"`

	// Lambdas maps stable lambda IDs (D-18 format: sha256(fileBytes)[:8] + ":" + line + ":" + col)
	// to their captured function values. ⚠ *starlark.Function is not
	// JSON-serializable; Phase 3 picks the serialization strategy. Phase 1
	// keeps this field in-memory only and excludes the function values from
	// MarshalJSON output (see WorkflowInput.MarshalJSON).
	Lambdas map[string]*CapturedLambda `json:"-"`

	// InitState is the initial workflow state passed to the first lambda.
	// Pure data; serializes via the standard json package.
	InitState map[string]any `json:"init_state"`
}

// MarshalJSON serializes a WorkflowInput while omitting the *starlark.Function
// values inside Lambdas (which cannot survive JSON encoding). Phase 1 emits
// the lambda IDs only — Phase 3 will replace this with the chosen
// serialization strategy.
//
// TODO(phase3): replace with real lambda serialization (custom DataConverter
// or re-parse-on-start). See Phase 3 entry-gate decision.
func (w *WorkflowInput) MarshalJSON() ([]byte, error) {
	type stub struct {
		Flow      *Flow          `json:"flow"`
		LambdaIDs []string       `json:"lambda_ids"`
		InitState map[string]any `json:"init_state"`
	}
	ids := make([]string, 0, len(w.Lambdas))
	for id := range w.Lambdas {
		ids = append(ids, id)
	}
	sort.Strings(ids) // deterministic key order — Pitfall #3 (map iteration leakage)
	return json.Marshal(stub{Flow: w.Flow, LambdaIDs: ids, InitState: w.InitState})
}
