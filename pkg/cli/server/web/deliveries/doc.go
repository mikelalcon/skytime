// Package deliveries hosts the in-memory ring buffer + source-agnostic header
// redaction for webhook deliveries surfaced on the dashboard (UI-02; D-7.3-17..D-7.3-22).
//
// Source-agnostic constraint: NO references to provider-specific header names
// (X-GitHub-Event, Stripe-Signature, etc.); the firewall test
// `tests/firewall_source_agnostic_test.go` enforces this.
package deliveries
