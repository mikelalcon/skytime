// Package webhook exposes a generic outbound POST extension as a
// Skytime extension. The destination URL itself is the secret — see
// CONTEXT.md D-WEBHOOK-HOST. Output types live here so .star authors
// can see what's available on ctx.<step_output> (e.g. ctx.post.status).
package webhook

import "github.com/mikelalcon/skytime/pkg/dag"

// WebhookPostOutput is returned by webhook.post. Status is the HTTP
// response code; Body is the (truncated) response payload from the
// receiver. webhook.site returns a small JSON acknowledgement on
// success — useful for diagnostic prints in `script` lambdas.
//
// Body is capped at 16 KB by doPost (LimitReader) to prevent log
// explosion from a misconfigured receiver returning a multi-MB body.
type WebhookPostOutput struct {
	Status int    `json:"status"`
	Body   string `json:"body"`
}

// IsOperationOutput is the dag.OperationOutput marker (see
// pkg/dag/output.go SEAL PROPERTY for why the marker method is exported
// rather than unexported).
func (WebhookPostOutput) IsOperationOutput() {}

// Compile-time check.
var _ dag.OperationOutput = WebhookPostOutput{}
