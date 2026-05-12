// Package events provides the broadcaster + workflow-list poller that fan out
// SSE events to dashboard browsers (Phase 7.3 D-7.3-02..D-7.3-08).
//
// Stdlib-only: net/http, sync, time, encoding/json, log/slog, container/list (if
// needed). NO charm-log, NO cobra.
package events
