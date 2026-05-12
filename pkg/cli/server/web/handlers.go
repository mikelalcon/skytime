// Plan 07.3-04: the three dashboard HTTP handlers (GET /,
// GET /api/events, POST /api/trigger) + writeEvent SSE helper +
// JSON-strict same-origin check (M3) + WriteTimeout defeat for SSE
// (Research Pitfall 8).
package web

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"

	"github.com/mikelalcon/skytime/pkg/cli/server/web/deliveries"
	"github.com/mikelalcon/skytime/pkg/cli/server/web/events"
	"github.com/mikelalcon/skytime/pkg/cli/server/web/flowlaunch"
	"github.com/mikelalcon/skytime/pkg/interpreter"
)

// Options holds per-handler dependencies. Mount populates this.
type Options struct {
	Client        client.Client
	TaskQueue     string
	Registry      *interpreter.FlowRegistry
	Broadcaster   *events.Broadcaster
	Buffer        *deliveries.RingBuffer
	Logger        *slog.Logger
	AllowedOrigin string // e.g., "http://localhost:8080" — empty disables the same-origin check
	TemporalWebUI string // e.g., "http://localhost:8233" — empty renders plain text IDs
	// WorkflowsFn returns the current workflow list snapshot (poller-owned).
	// Used by the dashboard handler to pre-render the workflow table.
	WorkflowsFn func() []events.WorkflowState
}

// Handlers groups the three dashboard endpoints behind a single struct
// so Mount can register them on the shared mux.
type Handlers struct {
	opts Options
}

// NewHandlers returns Handlers with sane defaults applied (a default
// logger when none was provided).
func NewHandlers(opts Options) *Handlers {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Handlers{opts: opts}
}

// dashboardHandler serves GET / only.
//
// 405 on non-GET; 404 on any path other than "/" so subpaths fall
// through to the receiver / SSE / trigger handlers (the mux's exact-
// match semantics already direct those routes; "/" is the catch-all
// pattern).
func (h *Handlers) dashboardHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	snap := h.snapshot()
	data := DashboardData{
		// M1: FlowRegistry's enumerator is FlowNames() — NOT Names().
		// Verified in pkg/interpreter/registry.go:114.
		Flows:         h.opts.Registry.FlowNames(),
		Snapshot:      snap,
		TemporalWebUI: h.opts.TemporalWebUI,
	}
	var buf bytes.Buffer
	if err := RenderDashboard(&buf, data); err != nil {
		h.opts.Logger.Error("render dashboard", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(buf.Bytes())
}

// sseHandler serves GET /api/events as text/event-stream.
//
// Pitfall 8: defeat http.Server.WriteTimeout: 30s on this request via
// http.ResponseController.SetWriteDeadline(time.Time{}). Otherwise
// every SSE connection dies at 30s.
func (h *Handlers) sseHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Pitfall 8: must call SetWriteDeadline(time.Time{}) to remove the
	// server-level WriteTimeout for this long-lived connection.
	if err := http.NewResponseController(w).SetWriteDeadline(time.Time{}); err != nil {
		// Test ResponseWriters (httptest.ResponseRecorder) don't
		// support SetWriteDeadline; log + continue so the rest of the
		// handler is still exercisable. Real net/http connections do
		// support it — Pitfall 8 guard fires in production.
		h.opts.Logger.Debug("SetWriteDeadline unsupported on this writer", "err", err)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported by this writer", http.StatusInternalServerError)
		return
	}

	snap, ch, unsubscribe := h.opts.Broadcaster.Subscribe()
	defer unsubscribe()

	// First frame: the captured-under-lock snapshot (Research
	// Pitfall 1). MUST use the returned snapshot — do not call a
	// separate Snapshot() method (would reopen the race).
	if err := writeEvent(w, "snapshot", snap); err != nil {
		return
	}
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				// Channel closed → broadcaster shut down. Emit one
				// last "shutdown" event so subscribers know the
				// disconnect was orderly (B3 — see server.go).
				_ = writeEvent(w, "shutdown", nil)
				flusher.Flush()
				return
			}
			if err := writeEvent(w, ev.Name, ev.Payload); err != nil {
				return // client likely disconnected
			}
			flusher.Flush()
		}
	}
}

// writeEvent serializes one SSE-framed event:
//
//	event: <name>
//	data: <json>
//	<blank line>
//
// When name is empty the event line is omitted (browser defaults to
// the unnamed "message" channel). Payload is JSON-marshaled with the
// stdlib encoder; multi-line bodies are not split into multiple
// "data:" lines because we never emit raw newlines in the marshaled
// JSON.
func writeEvent(w io.Writer, name string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if name != "" {
		fmt.Fprintf(&buf, "event: %s\n", name)
	}
	fmt.Fprintf(&buf, "data: %s\n\n", body)
	_, err = w.Write(buf.Bytes())
	return err
}

type triggerReq struct {
	Flow  string         `json:"flow"`
	Input map[string]any `json:"input"`
}

type triggerResp struct {
	WorkflowID string `json:"workflow_id,omitempty"`
	Error      string `json:"error,omitempty"`
}

// triggerHandler serves POST /api/trigger.
//
// Returns:
//
//	200 + {"workflow_id": "..."} on successful dispatch
//	400 + {"error": "..."}       on bad JSON, missing flow, unknown flow
//	403 + {"error": "..."}       on same-origin failure (M3 JSON-strict)
//	405                          on non-POST
//	500 + {"error": "..."}       on rand.Read or dispatch failure
//
// Pitfall 10: error responses NEVER echo user input — generic strings
// only ("input is not valid JSON", "dispatch failed", etc.).
func (h *Handlers) triggerHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		h.writeTrigger(w, http.StatusMethodNotAllowed, triggerResp{Error: "method not allowed"})
		return
	}
	if !h.sameOriginOK(r) {
		h.writeTrigger(w, http.StatusForbidden, triggerResp{Error: "origin not allowed"})
		return
	}
	var req triggerReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		// Pitfall 10: do NOT echo the malformed body in the error.
		h.writeTrigger(w, http.StatusBadRequest, triggerResp{Error: "input is not valid JSON"})
		return
	}
	if req.Flow == "" {
		h.writeTrigger(w, http.StatusBadRequest, triggerResp{Error: "flow name required"})
		return
	}
	contentHash, ok := h.opts.Registry.ContentHashFor(req.Flow)
	if !ok {
		h.writeTrigger(w, http.StatusBadRequest, triggerResp{Error: "flow not registered: " + req.Flow})
		return
	}
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		h.opts.Logger.Error("rand.Read failed", "err", err)
		h.writeTrigger(w, http.StatusInternalServerError, triggerResp{Error: "internal error"})
		return
	}
	wfid := "manual/" + req.Flow + "/" + hex.EncodeToString(buf[:])
	id, _, err := flowlaunch.Execute(
		r.Context(),
		h.opts.Client,
		h.opts.TaskQueue,
		req.Flow,
		contentHash,
		req.Input,
		flowlaunch.Options{
			WorkflowID:              wfid,
			ReusePolicy:             enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
			ErrorWhenAlreadyStarted: false,
		},
	)
	if err != nil {
		// Pitfall 10: log the detail server-side, return generic
		// message to the client — never echo flow names, input keys,
		// or wrapped Temporal error chains to the browser.
		h.opts.Logger.Warn("manual trigger dispatch failed", "flow", req.Flow, "err", err)
		h.writeTrigger(w, http.StatusInternalServerError, triggerResp{Error: "dispatch failed"})
		return
	}
	h.writeTrigger(w, http.StatusOK, triggerResp{WorkflowID: id})
}

// writeTrigger writes status + JSON body. Body errors are swallowed —
// at the point this fires the client has likely already disconnected,
// and there's no second response to send.
func (h *Handlers) writeTrigger(w http.ResponseWriter, status int, resp triggerResp) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

// sameOriginOK enforces JSON-strict same-origin (M3 from Phase 7.3
// checker).
//
// Contract:
//   - AllowedOrigin == "" → check disabled, all requests pass
//     (backward compat for `--dashboard-allowed-origin=""` and tests).
//   - Content-Type starts with "application/json" → Origin MUST be
//     present AND equal AllowedOrigin. Empty Origin is REJECTED —
//     browsers always set Origin on JSON POSTs (non-simple
//     content-type triggers CORS preflight), so an empty Origin on
//     a JSON POST is anomalous and treated as untrusted. This closes
//     the Open Q 3 cross-site form-post hole.
//   - Other content types (curl --data text/plain, CLI clients) →
//     empty Origin remains allowed. Plan 05's walkthrough Security
//     Note documents this caveat: non-browser JSON POSTers must
//     include a matching Origin header or unset
//     --dashboard-allowed-origin.
func (h *Handlers) sameOriginOK(r *http.Request) bool {
	if h.opts.AllowedOrigin == "" {
		return true // disabled
	}
	origin := r.Header.Get("Origin")
	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "application/json") {
		// JSON-strict: Origin required + must match.
		return origin != "" && origin == h.opts.AllowedOrigin
	}
	// Other content types: legacy lenient behavior.
	return origin == "" || origin == h.opts.AllowedOrigin
}

// snapshot builds the current events.Snapshot from the poller
// (workflows) + buffer (deliveries) when the dashboardHandler renders
// GET /. Bypasses the broadcaster lock (which is hot during fan-out)
// to keep the initial page-load fast.
func (h *Handlers) snapshot() events.Snapshot {
	s := events.Snapshot{}
	if h.opts.Buffer != nil {
		s.Deliveries = h.opts.Buffer.Snapshot(deliveries.DefaultCap)
	}
	if h.opts.WorkflowsFn != nil {
		s.Workflows = h.opts.WorkflowsFn()
	}
	return s
}

// sanitizeOriginFromAddr converts a listen address like ":8080" or
// "127.0.0.1:8080" or "0.0.0.0:8080" into a canonical Origin
// ("http://localhost:8080" / "http://127.0.0.1:8080"). Empty addr
// returns "" so the caller can disable the same-origin check.
func sanitizeOriginFromAddr(addr string) string {
	if addr == "" {
		return ""
	}
	host := addr
	if strings.HasPrefix(host, ":") {
		host = "localhost" + host
	}
	host = strings.Replace(host, "0.0.0.0", "localhost", 1)
	u := url.URL{Scheme: "http", Host: host}
	return u.String()
}
