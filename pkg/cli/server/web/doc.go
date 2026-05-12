// Package web hosts the stdlib-only dashboard for `skytime server` (Phase 7.3, UI-01..UI-04).
//
// Constraint: NO third-party HTTP, template, or SSE libraries reachable from any
// subpackage. The firewall test `tests/firewall_web_stdlib_test.go` enforces this.
//
// Subpackages:
//   - events/      — broadcaster + workflow-list poller (SSE fan-out)
//   - deliveries/  — ring buffer + redaction for recent webhook deliveries
//   - flowlaunch/  — single ExecuteWorkflow seam per UI-04 (consolidates webhook
//     ingress, manual trigger POST, and cron schedule callbacks).
package web
