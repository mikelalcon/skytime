package web

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.starlark.net/syntax"
	"go.temporal.io/sdk/client"

	"github.com/mikelalcon/skytime/pkg/cli/server/web/deliveries"
	"github.com/mikelalcon/skytime/pkg/cli/server/web/events"
	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/interpreter"
)

// fakeRun is a minimal client.WorkflowRun. flowlaunch.Execute only
// touches GetID on the success path.
type fakeRun struct{ id string }

func (r fakeRun) GetID() string                                                                 { return r.id }
func (r fakeRun) GetRunID() string                                                              { return "" }
func (r fakeRun) Get(_ context.Context, _ any) error                                            { return nil }
func (r fakeRun) GetWithOptions(_ context.Context, _ any, _ client.WorkflowRunGetOptions) error { return nil }

// fakeClient embeds client.Client so the compiler accepts it. Only
// ExecuteWorkflow is wired; any other method nil-panics intentionally
// (the dashboard handlers must not touch them).
type fakeClient struct {
	client.Client
	run client.WorkflowRun
	err error
}

func (f *fakeClient) ExecuteWorkflow(
	_ context.Context,
	_ client.StartWorkflowOptions,
	_ any,
	_ ...any,
) (client.WorkflowRun, error) {
	return f.run, f.err
}

// newTestRegistry builds a frozen FlowRegistry with the given flow
// names; each gets a stub content hash equal to "h-" + name so
// ContentHashFor returns a non-empty hash for the trigger path.
func newTestRegistry(t *testing.T, names ...string) *interpreter.FlowRegistry {
	t.Helper()
	r := interpreter.NewRegistry()
	for _, n := range names {
		filename := "test.star"
		parsed := &interpreter.ParsedFlow{
			Flow: &dag.Flow{
				Pos:    syntax.MakePosition(&filename, 1, 1),
				Name:   n,
				Inputs: map[string]string{},
			},
			Lambdas: map[string]*dag.CapturedLambda{},
		}
		require.NoError(t, r.Register(n, "h-"+n, parsed))
	}
	r.Freeze()
	return r
}

// newTestHandlers wires Handlers with a no-op broadcaster + a fake
// client. Tests override fields on the returned Options surface via
// the returned setter; the broadcaster is started so SSE tests can
// subscribe.
func newTestHandlers(t *testing.T, opts Options) *Handlers {
	t.Helper()
	if opts.Broadcaster == nil {
		opts.Broadcaster = events.NewBroadcaster(func() events.Snapshot {
			return events.Snapshot{}
		})
		t.Cleanup(opts.Broadcaster.Shutdown)
	}
	if opts.Registry == nil {
		opts.Registry = newTestRegistry(t, "alpha", "beta")
	}
	if opts.Client == nil {
		opts.Client = &fakeClient{run: fakeRun{id: "ignored"}}
	}
	if opts.TaskQueue == "" {
		opts.TaskQueue = "skytime"
	}
	return NewHandlers(opts)
}

// ---------------------------------------------------------------
// Dashboard GET / tests
// ---------------------------------------------------------------

func TestDashboard_RendersWorkflowList(t *testing.T) {
	wf := events.WorkflowState{
		WorkflowID:    "alpha/abc12345",
		FlowName:      "alpha",
		Status:        "running",
		RawStatus:     "RUNNING",
		HistoryLength: 5,
		StartTime:     time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC),
	}
	h := newTestHandlers(t, Options{
		Registry:    newTestRegistry(t, "alpha"),
		WorkflowsFn: func() []events.WorkflowState { return []events.WorkflowState{wf} },
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.dashboardHandler(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, `id="wfTable"`)
	require.Contains(t, body, `id="wf-alpha/abc12345"`)
	require.Contains(t, body, `class="status-running"`)
	require.Contains(t, rec.Header().Get("Content-Type"), "text/html")
}

func TestDashboard_FlowDropdown(t *testing.T) {
	// Register out of alphabetical order to prove the dropdown sorts
	// (FlowNames returns sorted).
	h := newTestHandlers(t, Options{
		Registry: newTestRegistry(t, "beta", "alpha"),
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.dashboardHandler(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, `<option value="alpha">alpha</option>`)
	require.Contains(t, body, `<option value="beta">beta</option>`)
	// Alpha appears before beta in source order.
	require.Less(t, strings.Index(body, `value="alpha"`), strings.Index(body, `value="beta"`))
}

// ---------------------------------------------------------------
// SSE GET /api/events tests
// ---------------------------------------------------------------

func TestSSE_InitialSnapshot(t *testing.T) {
	snap := events.Snapshot{
		Workflows: []events.WorkflowState{{WorkflowID: "wf-init", FlowName: "x"}},
	}
	b := events.NewBroadcaster(func() events.Snapshot { return snap })
	t.Cleanup(b.Shutdown)
	h := newTestHandlers(t, Options{Broadcaster: b})

	srv := httptest.NewServer(http.HandlerFunc(h.sseHandler))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Contains(t, resp.Header.Get("Content-Type"), "text/event-stream")
	require.Equal(t, "no-cache", resp.Header.Get("Cache-Control"))
	require.Equal(t, "no", resp.Header.Get("X-Accel-Buffering"))

	// Read the first 1KB — must contain "event: snapshot" + "data: {".
	buf := make([]byte, 1024)
	n, _ := io.ReadAtLeast(resp.Body, buf, 1)
	got := string(buf[:n])
	require.Contains(t, got, "event: snapshot")
	require.Contains(t, got, "data: {")
	require.Contains(t, got, "wf-init")
}

func TestSSE_WriteTimeoutDisabled(t *testing.T) {
	// The Pitfall 8 contract: the SSE handler MUST call
	// http.ResponseController.SetWriteDeadline(time.Time{}) so a
	// surrounding http.Server.WriteTimeout does NOT kill long-lived
	// SSE connections.
	//
	// We pin the contract by exercising it: build an http.Server with
	// a 100ms WriteTimeout, then verify that an SSE connection through
	// it survives well past 100ms AND delivers an event-after-snapshot
	// that arrived later.
	b := events.NewBroadcaster(func() events.Snapshot { return events.Snapshot{} })
	t.Cleanup(b.Shutdown)
	h := newTestHandlers(t, Options{Broadcaster: b})

	srv := httptest.NewUnstartedServer(http.HandlerFunc(h.sseHandler))
	srv.Config.WriteTimeout = 100 * time.Millisecond
	srv.Start()
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	br := bufio.NewReader(resp.Body)
	// Drain the initial snapshot frame.
	_, err = readSSEFrame(br)
	require.NoError(t, err)

	// Sleep > WriteTimeout, then publish + assert the new event arrives.
	time.Sleep(300 * time.Millisecond)
	b.Publish(events.Event{Name: "workflow_started", Payload: map[string]any{"workflow_id": "post-timeout"}})
	frame, err := readSSEFrame(br)
	require.NoError(t, err, "SSE connection died at the 100ms server WriteTimeout — Pitfall 8 fix missing")
	require.Contains(t, frame, "event: workflow_started")
	require.Contains(t, frame, "post-timeout")
}

// readSSEFrame reads up to a blank-line boundary (SSE frame
// terminator).
func readSSEFrame(r *bufio.Reader) (string, error) {
	var sb strings.Builder
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return sb.String(), err
		}
		sb.WriteString(line)
		if line == "\n" || line == "\r\n" {
			return sb.String(), nil
		}
	}
}

// ---------------------------------------------------------------
// POST /api/trigger tests
// ---------------------------------------------------------------

func TestTrigger_Success(t *testing.T) {
	fc := &fakeClient{run: fakeRun{id: "manual/alpha/0123456789abcdef"}}
	h := newTestHandlers(t, Options{
		Registry: newTestRegistry(t, "alpha"),
		Client:   fc,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/trigger",
		strings.NewReader(`{"flow":"alpha","input":{"k":"v"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.triggerHandler(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	require.Contains(t, rec.Body.String(), `"workflow_id":"manual/alpha/0123456789abcdef"`)
}

func TestTrigger_UnknownFlow(t *testing.T) {
	h := newTestHandlers(t, Options{
		Registry: newTestRegistry(t, "alpha"),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/trigger",
		strings.NewReader(`{"flow":"nope","input":{}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.triggerHandler(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "flow not registered: nope")
}

func TestTrigger_BadJSON_DoesNotEchoInput(t *testing.T) {
	h := newTestHandlers(t, Options{
		Registry: newTestRegistry(t, "alpha"),
	})
	const secret = "secret-value-not-json"
	req := httptest.NewRequest(http.MethodPost, "/api/trigger", strings.NewReader(secret))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.triggerHandler(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "input is not valid JSON")
	require.NotContains(t, body, secret, "Pitfall 10: trigger errors must NOT echo user input")
}

func TestTrigger_SameOriginCheck(t *testing.T) {
	h := newTestHandlers(t, Options{
		Registry:      newTestRegistry(t, "alpha"),
		AllowedOrigin: "http://localhost:8080",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/trigger",
		strings.NewReader(`{"flow":"alpha","input":{}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://evil.com")
	rec := httptest.NewRecorder()
	h.triggerHandler(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "origin not allowed")
}

// TestTrigger_SameOriginCheck_JSONRequiresOrigin pins M3 (Phase 7.3
// checker): when AllowedOrigin is set AND Content-Type is JSON,
// Origin MUST be present + matching. Empty Origin on a JSON POST is
// rejected (browsers always set Origin on non-simple content types).
func TestTrigger_SameOriginCheck_JSONRequiresOrigin(t *testing.T) {
	h := newTestHandlers(t, Options{
		Registry:      newTestRegistry(t, "alpha"),
		Client:        &fakeClient{run: fakeRun{id: "wf-x"}},
		AllowedOrigin: "http://localhost:8080",
	})

	// (a) JSON + empty Origin → 403 (the M3 fix).
	req := httptest.NewRequest(http.MethodPost, "/api/trigger",
		strings.NewReader(`{"flow":"alpha","input":{}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.triggerHandler(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code, "M3: JSON + empty Origin must be rejected")

	// (b) JSON + matching Origin → 200.
	req = httptest.NewRequest(http.MethodPost, "/api/trigger",
		strings.NewReader(`{"flow":"alpha","input":{}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://localhost:8080")
	rec = httptest.NewRecorder()
	h.triggerHandler(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "M3: JSON + matching Origin must succeed; body=%s", rec.Body.String())

	// (c) Non-JSON content type + empty Origin → NOT 403 (lenient
	// path). The body isn't JSON so we'll get 400 from the decoder,
	// but the Origin gate passed.
	req = httptest.NewRequest(http.MethodPost, "/api/trigger", strings.NewReader(`flow=alpha`))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	h.triggerHandler(rec, req)
	require.NotEqual(t, http.StatusForbidden, rec.Code, "non-JSON content types remain on the lenient Origin path")
}

// ---------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------

func TestDeliveriesBufferSnapshotIncluded(t *testing.T) {
	buf := deliveries.NewRingBuffer(deliveries.DefaultCap)
	buf.Append(deliveries.Delivery{ID: "del-1", Source: "github.webhook", Status: 200})
	h := newTestHandlers(t, Options{
		Registry: newTestRegistry(t, "alpha"),
		Buffer:   buf,
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.dashboardHandler(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "github.webhook")
}
