package events

import "testing"

func TestPoller_EmitsWorkflowStarted(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 03. Asserts a workflow seen for the first time produces a Publish(Event{Name: \"workflow_started\"}).")
}

func TestPoller_EmitsStatusChanged(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 03. Asserts status transition (RUNNING -> COMPLETED) produces a Publish(Event{Name: \"workflow_status_changed\"}).")
}

func TestPoller_EmitsReplayed_HistoryLengthJump(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 03. Asserts HistoryLength delta exceeding the configured threshold (default 50) produces a Publish(Event{Name: \"workflow_replayed\"}).")
}

func TestPoller_FetchError_DoesNotCrash(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 03. Asserts a transient ListWorkflow error logs at Warn level and the next tick retries.")
}

func TestPoller_MergeOpenClosed_Dedupes(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 03. Asserts a workflow present in both Open and Closed result sets is deduped by WorkflowID with Closed winning.")
}
