package receiver

import (
	"log/slog"
	"net/http"

	"go.temporal.io/sdk/client"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
	"github.com/mikelalcon/skytime/pkg/worker"
)

// Deps is the dependency bundle Mount needs at request time. Populated
// by pkg/cli/server.go (Plan 06) before calling Mount.
//
// All fields are required; Mount panics at boot when any field is unset
// (boot-time programmer error, not a runtime concern — see validate()).
type Deps struct {
	// Client is the Temporal client used for ExecuteWorkflow at request
	// time. Required.
	Client client.Client

	// CredentialHandler resolves credential IDs JIT inside the request
	// handler. Same handler the activity layer uses (Phase 2). Required —
	// for unsigned mounts (no secret_credential), pass a no-op handler;
	// the per-request pipeline (Plan 04b) short-circuits on empty
	// secretCredID before invoking Resolve.
	CredentialHandler extension.CredentialHandler

	// TaskQueue is the Temporal task queue ExecuteWorkflow targets.
	// Must match the Worker's TaskQueue. Required — empty string would
	// silently route ExecuteWorkflow to Temporal's default queue, which
	// is a hard-to-debug operator footgun. Fail loud at boot.
	TaskQueue string

	// Logger is the per-request log sink. Same logger as the server
	// startup banner (charm-log default; --json-log routes to JSON).
	// Required — nil here would NPE inside emit() at request time.
	Logger *slog.Logger
}

// validate panics on missing required Deps fields. Called once from
// Mount at boot. Boot-time programmer error; not a runtime concern.
//
// Locked panic messages — tests pin the strings via require.PanicsWithValue
// (TestDeps_PanicsOn*).
func (d Deps) validate() {
	if d.Client == nil {
		panic("receiver.Deps: Client is required")
	}
	if d.CredentialHandler == nil {
		panic("receiver.Deps: CredentialHandler is required (use a no-op handler for unsigned mounts)")
	}
	if d.TaskQueue == "" {
		panic("receiver.Deps: TaskQueue is required")
	}
	if d.Logger == nil {
		panic("receiver.Deps: Logger is required")
	}
}

// mountKey groups triggers by (source kind, path, method) per D-7.1-06.
// Two triggers with the same key fan out from one handler. Different
// methods on the same path produce different keys — handled at mount
// time via path-level method dispatch (see Mount).
type mountKey struct {
	kind   string
	path   string
	method string
}

// groupTriggers walks a slice of triggers and bins each HTTP-shaped one
// (i.e., one whose Source satisfies HTTPMounter) by (kind, path, method).
// Non-HTTP sources (cron, etc.) are silently skipped.
//
// Within-group order is INHERITED from the input slice. The caller is
// expected to pass worker.Triggers().All() which returns triggers sorted
// by (Source.Kind, FlowName, Pos.String) per
// pkg/interpreter/registry.go::Freeze. groupTriggers does NOT re-sort
// (verified by TestMount_DoesNotReSort + the no-sort source-grep gate).
//
// Exported within the package so tests can observe the grouping
// directly without going through Mount's mux side-effect.
func groupTriggers(trigs []*dag.Trigger) map[mountKey][]*dag.Trigger {
	groups := map[mountKey][]*dag.Trigger{}
	for _, t := range trigs {
		mounter, ok := t.Source.(HTTPMounter)
		if !ok {
			continue // cron sources, etc. — Phase 7.2 owns their dispatch path.
		}
		path, method := mounter.HTTPMount()
		key := mountKey{kind: t.Source.Kind(), path: path, method: method}
		groups[key] = append(groups[key], t)
	}
	return groups
}

// Mount walks worker.Triggers().All(), groups HTTP-shaped triggers by
// mountKey, and registers handlers on the supplied mux.
//
// Cron / non-HTTP sources are silently skipped (they don't implement
// receiver.HTTPMounter; Phase 7.2 owns their dispatch path).
//
// Per D-7.1-06 fan-out: when N triggers subscribe to the same mount
// key, they ALL fire on every matching delivery. Per-trigger event
// filtering (e.g., github.webhook ShouldDispatch) further narrows the
// dispatched set inside the handler.
//
// Per D-7.1-09 method dispatch: stdlib http.ServeMux is path-only — it
// does NOT method-dispatch and panics on duplicate path registration.
// Mount coalesces all groups sharing the same path into ONE
// HandleFunc that method-dispatches internally:
//
//   - matching method → invokes the per-(kind, path, method) handler
//   - non-matching method → 405 Method Not Allowed (via methodGate)
//
// Within-group ordering is INHERITED from worker.Triggers().All(), which
// returns triggers sorted by (Source.Kind, FlowName, Pos.String) per
// pkg/interpreter/registry.go::Freeze. Mount does NOT re-sort within
// groups (verified during planning by reading registry.go; defended by
// TestMount_DoesNotReSort + the no-sort source-grep gate on receiver.go).
func Mount(mux *http.ServeMux, w *worker.Worker, deps Deps) {
	deps.validate()

	groups := groupTriggers(w.Triggers().All())

	// Build per-(kind, path, method) handlers, keyed by path so we can
	// register exactly one HandleFunc per path on the mux (stdlib mux
	// limitation: one handler per pattern; second registration panics).
	//
	// methodHandlers[path] maps METHOD → HandlerFunc. The path-level
	// HandleFunc dispatches by Method, falling through to 405 on
	// unknown method.
	type methodMap = map[string]http.HandlerFunc
	byPath := map[string]methodMap{}
	for key, trigs := range groups {
		// Within-group order inherited from registry.All() (already
		// sorted by Source.Kind, FlowName, Pos.String). No re-sort here.
		mm, ok := byPath[key.path]
		if !ok {
			mm = methodMap{}
			byPath[key.path] = mm
		}
		mm[key.method] = makeHandler(key, trigs, deps)
	}

	for path, methods := range byPath {
		mux.HandleFunc(path, dispatchByMethod(methods))
	}
}

// dispatchByMethod returns an http.HandlerFunc that routes the request
// to the matching per-method handler, or returns 405 Method Not Allowed
// if no handler is registered for the request method.
//
// This is the path-level glue Mount uses to overcome stdlib
// http.ServeMux's path-only routing — see Mount's doc for the full
// reasoning.
func dispatchByMethod(methods map[string]http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h, ok := methods[r.Method]; ok {
			h(w, r)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// methodGate returns 405 Method Not Allowed for non-matching methods,
// otherwise calls next. Used at the per-handler level when a single
// (kind, path, method) handler is registered standalone (e.g., in tests).
//
// Mount itself uses dispatchByMethod for path-level fan-in; methodGate
// is the simpler one-method gate kept around for direct
// `methodGate("POST", inner)` wrapping in tests + as a documented
// helper (TestMethodGate pins the contract).
//
// Stdlib http.ServeMux does NOT method-dispatch; this helper layers the
// 405 behavior on top so /x with a mounted POST handler returns 405 on
// GET (instead of the default — which would be the POST handler firing
// regardless of method).
func methodGate(want string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != want {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next(w, r)
	}
}
