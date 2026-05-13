// Package snippets contains the working Go code samples shown in
// ../temporal-auth.md. Each snippet (gcp.go / aws.go / azure.go /
// mtls.go) demonstrates a production credential-rotation pattern for
// connecting to Temporal Cloud or a self-hosted Temporal cluster.
//
// This module is standalone — it depends on go.temporal.io/sdk for the
// client.Credentials interface but does NOT depend on the main skytime
// library. The cloud-SDK dependencies (Google Secret Manager, AWS
// Secrets Manager, Azure Key Vault) live here so the main module's
// go.mod stays clean — customers importing skytime do not pick up
// cloud-SDK transitives.
//
// Drift between the markdown code fences and the .go file bodies is
// detected by drift_test.go (run via `go test ./...` from this
// directory). CI runs the same command in .github/workflows/ci.yml.
package snippets
