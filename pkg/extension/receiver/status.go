package receiver

import (
	"encoding/json"
	"net/http"
)

// LOCKED — D-7.1-15 error_class taxonomy. Every constant here is a
// stable identifier consumed by log line consumers. Additions require
// a CONTEXT.md amendment (D-7.1-15 explicit lock).
const (
	errorClassOK                = "ok"
	errorClassSignatureMismatch = "signature_mismatch"
	errorClassBadRequest        = "bad_request"
	errorClassLambdaPanic       = "lambda_panic"
	errorClassDispatchFailed    = "dispatch_failed"
	errorClassEventFiltered     = "event_filtered"
	errorClassDuplicateSkipped  = "duplicate_skipped"
)

// writeSuccessResponse writes 200 with {"workflow_id":...} when len==1
// or {"workflow_ids":[...]} when len>=2. Empty input is invalid (caller
// must use writeEventFilteredResponse for "no triggers matched").
func writeSuccessResponse(w http.ResponseWriter, ids []string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if len(ids) == 1 {
		_ = json.NewEncoder(w).Encode(map[string]string{"workflow_id": ids[0]})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string][]string{"workflow_ids": ids})
}

// writeDuplicateResponse writes 200 with the duplicate-skipped envelope
// (D-7.1-14: 200 NOT 409 so source providers like GitHub do not retry).
func writeDuplicateResponse(w http.ResponseWriter, existingID string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":      "duplicate; skipped",
		"workflow_id": existingID,
	})
}

// writeEventFilteredResponse writes 200 with the event-filter envelope
// (D-7.1-07: empty match set still 200 so providers don't retry).
func writeEventFilteredResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "event filtered"})
}

// writeUnauthorizedResponse writes 401 with the locked unauthorized
// envelope. CRITICAL: NO `detail` field — leaking signature-mismatch
// vs missing-secret distinction reveals attacker information about
// the validation pipeline (D-7.1-14).
func writeUnauthorizedResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
}

// writeBadRequestResponse writes 400 with safe-message detail. Caller
// MUST never pass user-payload bytes — only error-class strings ("json
// parse failed", "missing content-type", etc.).
func writeBadRequestResponse(w http.ResponseWriter, safeDetail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":  "bad_request",
		"detail": safeDetail,
	})
}

// writeUnsupportedMediaTypeResponse writes 415. Used by Plan 04's
// handler when Content-Type is not application/json — D-7.1's Body
// decoder negotiation discretion was LOCKED at planning time to 415
// over partial fall-through. NO `detail` field — the error class
// communicates everything the source provider needs.
func writeUnsupportedMediaTypeResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnsupportedMediaType)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "unsupported_media_type"})
}

// writeInternalResponse writes 500. detail is the error CLASS only
// (e.g., "starlark eval error", "lambda_panic") — never the user
// payload, never the lambda source, never the resolved Secret.
func writeInternalResponse(w http.ResponseWriter, errorClassDetail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":  "internal",
		"detail": errorClassDetail,
	})
}

// writeUpstreamResponse writes 502. Source providers (GitHub) WILL
// retry on 502 — that is correct (D-7.1-14: temporal_unavailable
// means receiver could not enqueue; retry will succeed once Temporal
// is reachable, with REJECT_DUPLICATE handling any partial-success
// re-fires).
func writeUpstreamResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":  "upstream",
		"detail": "temporal_unavailable",
	})
}
