package http

import "github.com/mikelalcon/skytime/pkg/dag"

// HTTPResponse is the typed dag.OperationOutput returned by every
// operation of the baked-in HTTP extension (D4-14).
//
// Fields:
//
//	Status  — HTTP status code (200, 404, 500, etc.)
//	Body    — full response body as bytes; Phase 4 does no streaming
//	Headers — flattened headers (multi-valued headers join with ", ")
//
// Consumed in .star via dot access on the script's output_alias:
//
//	ctx.<output_alias>.status
//	ctx.<output_alias>.body
//	ctx.<output_alias>.headers
type HTTPResponse struct {
	Status  int               `json:"status"`
	Body    []byte            `json:"body"`
	Headers map[string]string `json:"headers"`
}

// IsOperationOutput is the exported marker (see pkg/dag/output.go SEAL
// PROPERTY for why the marker method is exported rather than unexported).
func (HTTPResponse) IsOperationOutput() {}

// Compile-time check.
var _ dag.OperationOutput = HTTPResponse{}
