package web

import "testing"

func TestDashboard_RendersWorkflowList(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 04. GET / returns HTML with table containing one row per workflow from FlowRegistry/poller snapshot, status badge classes from D-7.3-11, id=\"wf-<workflowID>\" on each row.")
}

func TestSSE_InitialSnapshot(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 04. GET /api/events returns Content-Type: text/event-stream and emits a single 'event: snapshot' frame with JSON payload {workflows:[], deliveries:[]} within ~50ms.")
}

func TestSSE_WriteTimeoutDisabled(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 04. Asserts handler calls http.ResponseController(w).SetWriteDeadline(time.Time{}) so the http.Server.WriteTimeout: 30s defeat (Research Pitfall 8) holds.")
}

func TestTrigger_Success(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 04. POST /api/trigger with {\"flow\": \"<registered>\", \"input\": {}} returns 200 + {\"workflow_id\": \"manual/<flow>/<32hex>\"}.")
}

func TestTrigger_UnknownFlow(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 04. POST /api/trigger with unregistered flow returns 400 + {\"error\": \"flow not registered: <name>\"}.")
}

func TestTrigger_BadJSON_DoesNotEchoInput(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 04. POST /api/trigger with malformed body returns 400 + {\"error\": \"input is not valid JSON\"} and the response body does NOT contain the original input bytes (per Research Pitfall 10).")
}

func TestTrigger_SameOriginCheck(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 04. POST /api/trigger with Origin header pointing at a different host returns 403 (defense-in-depth per Research Open Q 3).")
}

func TestDashboard_FlowDropdown(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 04. GET / response contains <option value=\"<flow>\"> for every name in worker.Registry().Names(), in alphabetical order.")
}
