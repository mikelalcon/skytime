package web

import "testing"

func TestTemplate_DashboardGolden(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 04. Renders the dashboard template against a fixed snapshot and compares to a golden file under pkg/cli/server/web/testdata/dashboard.html.golden.")
}

func TestTemplate_AnchorIDsMatch(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 04. For each delivery's mapped_workflow_id, asserts the rendered HTML contains both id=\"wf-<id>\" on the workflow row AND href=\"#wf-<id>\" on the delivery row's anchor link (D-7.3-21).")
}

func TestTemplate_EmptyStateHintRow(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 04. Renders with zero workflows; asserts table contains the literal hint string 'No workflows yet \\u2014 fire a webhook, click Run below, or wait for cron.' (D-7.3-13).")
}
