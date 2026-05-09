package receiver

import (
	"net/http"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// makeHandler returns the per-mount http.HandlerFunc that processes
// one delivery and dispatches across the fan-out trigs slice. The full
// pipeline is implemented in Plan 04b — this skeleton returns 501 Not
// Implemented so Plan 04's mount-level tests can assert path
// registration without needing the per-request behavior in place.
//
// LOCKED order (Plan 04b will fill the body):
//
//  1. defer log emit (requestRecord.emit at handler exit)
//  2. method gate (already wrapped at Mount-time via dispatchByMethod)
//  3. body cap (http.MaxBytesReader, 25MB per D-7.1-12)
//  4. raw body read (io.ReadAll AFTER MaxBytesReader, BEFORE json —
//     critical for HMAC validation against the WIRE bytes per Pitfall 2)
//  5. JIT credential resolve (only when any trig has a non-empty
//     secretCredID; CredentialHandler.Resolve is invoked once per
//     delivery, not once per fan-out trigger)
//  6. signature validation (validateHMAC against algo + header from
//     accessor methods on the source value; skipped for unsigned mounts)
//  7. content-type check + json.Unmarshal (415 unsupported_media_type
//     for non-application/json bodies)
//  8. per-trigger fan-out: event filter (github.webhook ShouldDispatch)
//     → eval map+idempotency lambdas → ExecuteWorkflow with
//     REJECT_DUPLICATE + WorkflowExecutionErrorWhenAlreadyStarted=true
//     (Pitfall 1)
//  9. response write (200/200-duplicate/200-event-filtered/4xx/5xx per
//     D-7.1-14) + log emit
//
// The function signature is locked here so Plan 04b can replace the
// stub body without re-touching Mount, methodGate, or Deps.
func makeHandler(key mountKey, trigs []*dag.Trigger, deps Deps) http.HandlerFunc {
	// Capture the closure variables; Plan 04b will use them.
	_ = key
	_ = trigs
	_ = deps
	return func(w http.ResponseWriter, r *http.Request) {
		// Plan 04b fills the body. Plan 04 mount tests assert PATH
		// registration only; getting any response other than 404
		// (NotFound from the default mux) suffices to prove the path
		// was mounted. 501 is a "not implemented" stub that flags
		// stale wiring if production code somehow reaches this with
		// Plan 04b also shipped.
		http.Error(w, "not implemented (plan 04b)", http.StatusNotImplemented)
	}
}
