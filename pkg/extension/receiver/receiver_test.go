package receiver

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.starlark.net/syntax"
	"go.temporal.io/sdk/client"

	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
	"github.com/mikelalcon/skytime/pkg/interpreter"
	"github.com/mikelalcon/skytime/pkg/worker"

	skygh "github.com/mikelalcon/skytime/examples/http-github-webhook/extensions/github"
	skyhttp "github.com/mikelalcon/skytime/pkg/extension/builtin/http"
)

// fakeClient embeds client.Client. The nil embedded interface is enough to
// satisfy the type for tests that never call any method on it (Mount only
// stores the reference into Deps; the 501-stub handler never dispatches).
type fakeClient struct{ client.Client }

var _ client.Client = (*fakeClient)(nil)

// fakeCredentialHandler is a minimal extension.CredentialHandler. Mount
// only stores the reference into Deps; tests never invoke Resolve via the
// stub handler.
type fakeCredentialHandler struct{}

func (*fakeCredentialHandler) Resolve(_ context.Context, _ string) (extension.Credential, error) {
	return nil, nil
}

// newWorkerWithTriggers builds a *worker.Worker with the given triggers
// already registered + frozen. Each trigger is registered under a unique
// content_hash (the registry deduplicates per (hash, identity), so we use
// a synthetic per-trigger hash).
func newWorkerWithTriggers(t *testing.T, trigs []*dag.Trigger) *worker.Worker {
	t.Helper()
	flowReg := interpreter.NewRegistry()
	flowReg.Freeze()

	trigReg := interpreter.NewTriggerRegistry()
	for i, tr := range trigs {
		// Synthetic content hash per trigger so registration never collides.
		// The exact value is irrelevant; the registry only uses it as a key
		// in byContentHash (a future-feature side-index).
		hash := fakeHash(i)
		require.NoError(t, trigReg.Register(hash, tr))
	}
	trigReg.Freeze()
	return worker.NewWorkerForTest(flowReg, trigReg)
}

func fakeHash(i int) string {
	return "hash-" + string(rune('a'+i))
}

// trig builds a *dag.Trigger with the given FlowName + Source + a synthetic
// position. Position is used by composeWorkflowID and by the registry's
// sort tiebreaker; the tests below specify positions explicitly when
// ordering matters.
func trig(flow string, src dag.TriggerSource, pos syntax.Position) *dag.Trigger {
	return &dag.Trigger{
		Pos:      pos,
		FlowName: flow,
		Source:   src,
	}
}

// pos constructs a syntax.Position with the given filename + line. Col is
// fixed at 1 for brevity. Pos.String() is used in the registry's sort
// tiebreaker.
func pos(filename string, line int32) syntax.Position {
	return syntax.MakePosition(&filename, line, 1)
}

// validDeps returns a Deps with all required fields populated. Used by
// the mount-grouping tests; the field-by-field nil tests build their own
// minimal Deps.
func validDeps() Deps {
	return Deps{
		Client:            &fakeClient{},
		CredentialHandler: &fakeCredentialHandler{},
		TaskQueue:         "test-queue",
		Logger:            discardLogger(),
	}
}

// =============================================================================
// Mount-level tests — fan-out grouping, mount key dedup, method gating,
// cron-source skip, and the no-redundant-sort regression gate.
// =============================================================================

// TestMount_FanOutGrouping covers the three core grouping invariants in one
// fixture: (1) two github.webhook triggers with different flows but the
// same mount key collapse to one path; (2) one http.webhook on a different
// path mounts a second path; (3) a cron-shaped source (no HTTPMounter) is
// silently skipped.
//
// After Mount returns we exercise the registered handlers with httptest to
// confirm the paths are actually mounted (a missing mount would yield 404
// from the stdlib mux's NotFound handler; mounted handlers respond with
// any non-404 code — Plan 04b's real handler returns 415 here because the
// probe POSTs have no Content-Type set).
func TestMount_FanOutGrouping(t *testing.T) {
	gh1 := skygh.NewGithubWebhookSourceForTest([]string{"issues"}, "")
	gh2 := skygh.NewGithubWebhookSourceForTest([]string{"pull_request"}, "")
	hh := skyhttp.NewHTTPWebhookSourceForTest("/hooks/x", "POST", "", "sha256", "X-Signature")
	cron := &extension.FakeTriggerSource{KindName: "test.cron", ReqFields: []string{"scheduled_time"}}

	w := newWorkerWithTriggers(t, []*dag.Trigger{
		trig("issue_triage", gh1, pos("a.star", 1)),
		trig("pr_triage", gh2, pos("b.star", 1)),
		trig("custom_label", hh, pos("c.star", 1)),
		trig("nightly_cron", cron, pos("d.star", 1)),
	})

	mux := http.NewServeMux()
	Mount(mux, w, validDeps())

	// Probe each path. Mounted handlers respond with any non-404; the
	// only "unmounted" signal is 404 from the default NotFoundHandler.
	cases := []struct {
		method      string
		path        string
		wantMounted bool
		why         string
	}{
		{"POST", "/webhook/github", true, "github.webhook mounted (collapsed gh1+gh2)"},
		{"POST", "/hooks/x", true, "http.webhook mounted on /hooks/x"},
		// No path was registered for the cron source.
		{"POST", "/cron/nightly", false, "cron source must NOT mount any HTTP path"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if tc.wantMounted {
			require.NotEqual(t, http.StatusNotFound, rec.Code,
				"%s — mounted path must NOT return 404 (got %d)", tc.why, rec.Code)
		} else {
			require.Equal(t, http.StatusNotFound, rec.Code,
				"%s — unmounted path must return 404 (got %d)", tc.why, rec.Code)
		}
	}
}

// TestMount_GroupsByMountKey: two http.webhook triggers binding the same
// (path="/x", method="POST") with different flows produce ONE handler
// registration covering both flows. Verified directly via the unexported
// groupTriggers helper: the returned map has exactly one mountKey entry
// whose slice contains both triggers. The mux registration is sanity-
// checked by confirming /x doesn't 404 (Plan 04b's real handler now
// processes the empty body and returns 415 for missing Content-Type).
func TestMount_GroupsByMountKey(t *testing.T) {
	hh1 := skyhttp.NewHTTPWebhookSourceForTest("/x", "POST", "", "sha256", "X-Signature")
	hh2 := skyhttp.NewHTTPWebhookSourceForTest("/x", "POST", "", "sha256", "X-Signature")

	w := newWorkerWithTriggers(t, []*dag.Trigger{
		trig("flow_a", hh1, pos("a.star", 1)),
		trig("flow_b", hh2, pos("b.star", 1)),
	})

	groups := groupTriggers(w.Triggers().All())
	require.Len(t, groups, 1, "two triggers on the same (kind, path, method) collapse to one group")
	for k, trigs := range groups {
		require.Equal(t, "/x", k.path)
		require.Equal(t, "POST", k.method)
		require.Equal(t, "http.webhook", k.kind)
		require.Len(t, trigs, 2, "both triggers share the group")
	}

	// Sanity-check the mux registration: /x is reachable (not 404).
	mux := http.NewServeMux()
	Mount(mux, w, validDeps())
	req := httptest.NewRequest("POST", "/x", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	require.NotEqual(t, http.StatusNotFound, rec.Code,
		"mounted path /x must not 404 (got %d)", rec.Code)
}

// TestMount_DifferentMethodsSamePath: http.webhook(path="/x", method="POST")
// and http.webhook(path="/x", method="GET") produce TWO mount groups; the
// methodGate dispatches by method (D-7.1-09). Stdlib http.ServeMux does NOT
// method-dispatch on its own — a single path registration would shadow the
// other. Verified by exercising both methods against /x and asserting
// neither gets a 405.
//
// NOTE: stdlib http.ServeMux only allows one handler per path pattern;
// when two registrations land on the same path, the second panics. This
// test will RED until the implementation handles the same-path-different-
// method case (e.g., by combining both methods into a single mount-time
// dispatch). The plan requires "TWO mounts on the same path (one per
// method)" — implementation must coalesce by path AND dispatch by method
// inside the handler, since stdlib mux doesn't.
//
// Implementation strategy chosen: groupTriggers groups by (kind, path,
// method); Mount merges all groups sharing the same path into a single
// HandleFunc that internally method-dispatches. This is documented in
// Mount's doc comment and tested here.
func TestMount_DifferentMethodsSamePath(t *testing.T) {
	hhPost := skyhttp.NewHTTPWebhookSourceForTest("/x", "POST", "", "sha256", "X-Signature")
	hhGet := skyhttp.NewHTTPWebhookSourceForTest("/x", "GET", "", "sha256", "X-Signature")

	w := newWorkerWithTriggers(t, []*dag.Trigger{
		trig("post_flow", hhPost, pos("a.star", 1)),
		trig("get_flow", hhGet, pos("b.star", 1)),
	})

	mux := http.NewServeMux()
	Mount(mux, w, validDeps())

	// POST must hit the mounted handler (real handler reaches 415 on
	// empty body without Content-Type — any non-404, non-405 proves
	// the POST handler ran).
	postReq := httptest.NewRequest("POST", "/x", nil)
	postRec := httptest.NewRecorder()
	mux.ServeHTTP(postRec, postReq)
	require.NotEqual(t, http.StatusNotFound, postRec.Code,
		"POST /x must reach the POST mount's handler (got %d)", postRec.Code)
	require.NotEqual(t, http.StatusMethodNotAllowed, postRec.Code,
		"POST /x must NOT 405 (POST is registered)")

	// GET must hit the mounted handler too (sibling method group).
	getReq := httptest.NewRequest("GET", "/x", nil)
	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, getReq)
	require.NotEqual(t, http.StatusNotFound, getRec.Code,
		"GET /x must reach the GET mount's handler (got %d)", getRec.Code)
	require.NotEqual(t, http.StatusMethodNotAllowed, getRec.Code,
		"GET /x must NOT 405 (GET is registered)")

	// PUT must be 405 (no PUT registration for /x).
	putReq := httptest.NewRequest("PUT", "/x", nil)
	putRec := httptest.NewRecorder()
	mux.ServeHTTP(putRec, putReq)
	require.Equal(t, http.StatusMethodNotAllowed, putRec.Code,
		"PUT /x must be 405 — no PUT mount on /x")
}

// TestMount_DoesNotReSort: registry.All() already returns triggers sorted
// by (Source.Kind, FlowName, Pos.String) per Freeze. Mount must NOT
// re-sort within groups. We register two triggers in REVERSE-SORTED order
// (z_flow before a_flow) and confirm groupTriggers preserves the registry's
// sorted order ([a_flow, z_flow]) — i.e., no extra sort happens that
// would re-order them.
//
// This test plus the source-grep gate (no `sort.SliceStable` / `sort.Strings`
// in receiver.go) defends Info #11 fix from regressing.
func TestMount_DoesNotReSort(t *testing.T) {
	hh1 := skyhttp.NewHTTPWebhookSourceForTest("/x", "POST", "", "sha256", "X-Signature")
	hh2 := skyhttp.NewHTTPWebhookSourceForTest("/x", "POST", "", "sha256", "X-Signature")

	// Register in reverse-sorted order (z first, a second). Registry's
	// Freeze sorts to (a_flow, z_flow).
	w := newWorkerWithTriggers(t, []*dag.Trigger{
		trig("z_flow", hh1, pos("z.star", 1)),
		trig("a_flow", hh2, pos("a.star", 1)),
	})

	// Confirm the registry's All() yields the post-sort order first.
	all := w.Triggers().All()
	require.Len(t, all, 2)
	require.Equal(t, "a_flow", all[0].FlowName, "registry.All() must already be sorted")
	require.Equal(t, "z_flow", all[1].FlowName)

	// Now confirm groupTriggers preserves that order within the group
	// (no re-sort).
	groups := groupTriggers(all)
	require.Len(t, groups, 1)
	for _, trigs := range groups {
		require.Len(t, trigs, 2)
		require.Equal(t, "a_flow", trigs[0].FlowName,
			"Mount must not re-sort within groups; registry order preserved")
		require.Equal(t, "z_flow", trigs[1].FlowName)
	}
}

// =============================================================================
// methodGate test — 405 for non-matching methods, pass-through otherwise.
// =============================================================================

// TestMethodGate verifies the helper layered on top of stdlib ServeMux:
// matching method calls inner; non-matching method returns 405.
func TestMethodGate(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	gate := methodGate("POST", inner)

	// POST → inner fires.
	postReq := httptest.NewRequest("POST", "/x", nil)
	postRec := httptest.NewRecorder()
	gate(postRec, postReq)
	require.True(t, called, "POST must invoke inner")
	require.Equal(t, http.StatusOK, postRec.Code)

	// GET → 405 without invoking inner.
	called = false
	getReq := httptest.NewRequest("GET", "/x", nil)
	getRec := httptest.NewRecorder()
	gate(getRec, getReq)
	require.False(t, called, "GET must NOT invoke inner")
	require.Equal(t, http.StatusMethodNotAllowed, getRec.Code)
}

// =============================================================================
// Deps validation — boot-time programmer error; panics with locked messages.
// =============================================================================

// TestDeps_PanicsOnMissingClient asserts Mount panics when Deps.Client is nil.
// The panic message contains "Client is required" so operators see the
// missing field by name.
func TestDeps_PanicsOnMissingClient(t *testing.T) {
	w := newWorkerWithTriggers(t, nil)
	bad := validDeps()
	bad.Client = nil

	require.PanicsWithValue(t,
		"receiver.Deps: Client is required",
		func() { Mount(http.NewServeMux(), w, bad) },
		"Mount must panic with 'Client is required' on nil Client")
}

// TestDeps_PanicsOnMissingCredentialHandler asserts the same shape for a
// nil CredentialHandler. The message also hints at the unsigned-mount
// no-op handler escape hatch.
func TestDeps_PanicsOnMissingCredentialHandler(t *testing.T) {
	w := newWorkerWithTriggers(t, nil)
	bad := validDeps()
	bad.CredentialHandler = nil

	require.Panics(t, func() { Mount(http.NewServeMux(), w, bad) })
	defer func() {
		if r := recover(); r != nil {
			require.Contains(t, r, "CredentialHandler is required")
		}
	}()
	// Also explicitly test the message via PanicsWithValue.
	require.PanicsWithValue(t,
		"receiver.Deps: CredentialHandler is required (use a no-op handler for unsigned mounts)",
		func() { Mount(http.NewServeMux(), w, bad) },
		"Mount must panic on nil CredentialHandler with the locked message")
}

// TestDeps_PanicsOnEmptyTaskQueue asserts the empty-string check. An empty
// task queue would silently route ExecuteWorkflow to the default queue,
// which is a hard-to-debug operator footgun — fail loud at boot.
func TestDeps_PanicsOnEmptyTaskQueue(t *testing.T) {
	w := newWorkerWithTriggers(t, nil)
	bad := validDeps()
	bad.TaskQueue = ""

	require.PanicsWithValue(t,
		"receiver.Deps: TaskQueue is required",
		func() { Mount(http.NewServeMux(), w, bad) },
		"Mount must panic with 'TaskQueue is required' on empty queue")
}

// TestDeps_PanicsOnMissingLogger asserts the nil-Logger check. The
// per-request log line is the receiver's primary observability surface;
// nil here would NPE inside emit(). Fail loud.
func TestDeps_PanicsOnMissingLogger(t *testing.T) {
	w := newWorkerWithTriggers(t, nil)
	bad := validDeps()
	bad.Logger = nil

	require.PanicsWithValue(t,
		"receiver.Deps: Logger is required",
		func() { Mount(http.NewServeMux(), w, bad) },
		"Mount must panic with 'Logger is required' on nil Logger")
}

// =============================================================================
// helpers
// =============================================================================

// discardLogger returns a *slog.Logger that drops everything. Tests don't
// need to inspect the per-request log line (Plan 04b owns the emit
// behavior); discarding keeps the failure messages clean.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
