package web

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/cli/server/web/deliveries"
	"github.com/mikelalcon/skytime/pkg/cli/server/web/events"
)

// goldenData is the fixed input for TestTemplate_DashboardGolden.
//
// Locked timestamps + IDs so the rendered output is byte-stable across
// runs. Update via `GSD_UPDATE_GOLDEN=1 go test -run TestTemplate_DashboardGolden`
// when the template intentionally changes.
func goldenData() DashboardData {
	closeT := time.Date(2026, 5, 13, 0, 0, 12, 0, time.UTC)
	return DashboardData{
		Flows: []string{"alpha", "beta"},
		Snapshot: events.Snapshot{
			Workflows: []events.WorkflowState{
				{
					WorkflowID:    "alpha/abc12345",
					FlowName:      "alpha",
					Status:        "completed",
					RawStatus:     "COMPLETED",
					HistoryLength: 5,
					StartTime:     time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC),
					CloseTime:     &closeT, // pin a CloseTime so formatDuration is deterministic
				},
			},
			Deliveries: []deliveries.Delivery{
				{
					ID:             "del1",
					Source:         "github.webhook",
					Path:           "/webhook/github",
					Method:         "POST",
					Status:         200,
					Headers:        map[string]string{"X-GitHub-Event": "issues", "Authorization": "<redacted>"},
					PayloadSummary: `{"action":"opened"}`,
					WorkflowIDs:    []string{"alpha/abc12345"},
					ReceivedAt:     time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC),
				},
			},
		},
		TemporalWebUI: "http://localhost:8233",
	}
}

func TestTemplate_DashboardGolden(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, RenderDashboard(&buf, goldenData()))
	got := buf.Bytes()

	goldenPath := filepath.Join("testdata", "dashboard.html.golden")
	if os.Getenv("GSD_UPDATE_GOLDEN") == "1" {
		require.NoError(t, os.MkdirAll("testdata", 0o755))
		require.NoError(t, os.WriteFile(goldenPath, got, 0o644))
		t.Logf("updated golden %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		// Bootstrap: write the golden on the first run so a developer
		// can review + commit it. Subsequent runs assert byte equality.
		require.NoError(t, os.MkdirAll("testdata", 0o755))
		require.NoError(t, os.WriteFile(goldenPath, got, 0o644))
		t.Fatalf("golden %s did not exist; wrote initial bytes — re-run to assert equality (or set GSD_UPDATE_GOLDEN=1 to regenerate)", goldenPath)
	}
	require.Equal(t, string(want), string(got),
		"dashboard template output drifted from golden; if intentional, run GSD_UPDATE_GOLDEN=1 go test -run TestTemplate_DashboardGolden")
}

func TestTemplate_AnchorIDsMatch(t *testing.T) {
	data := DashboardData{
		Snapshot: events.Snapshot{
			Workflows: []events.WorkflowState{
				{
					WorkflowID: "wf-xyz",
					FlowName:   "alpha",
					Status:     "running",
					StartTime:  time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC),
				},
			},
			Deliveries: []deliveries.Delivery{
				{
					ID:          "del1",
					Source:      "github.webhook",
					Method:      "POST",
					Path:        "/webhook/github",
					Status:      200,
					ReceivedAt:  time.Date(2026, 5, 13, 0, 0, 0, 0, time.UTC),
					WorkflowIDs: []string{"wf-xyz"},
				},
			},
		},
	}
	var buf bytes.Buffer
	require.NoError(t, RenderDashboard(&buf, data))
	body := buf.String()
	require.Contains(t, body, `id="wf-wf-xyz"`)
	require.Contains(t, body, `href="#wf-wf-xyz"`)
}

func TestTemplate_EmptyStateHintRow(t *testing.T) {
	data := DashboardData{
		Snapshot: events.Snapshot{}, // zero workflows + zero deliveries
	}
	var buf bytes.Buffer
	require.NoError(t, RenderDashboard(&buf, data))
	body := buf.String()
	// U+2014 em-dash literal — must NOT be replaced with ASCII '-'.
	require.Contains(t, body, "No workflows yet — fire a webhook, click Run below, or wait for cron.")
	// Sanity: confirm the dash literal renders verbatim in source.
	require.True(t, strings.Contains(body, "—"), "em dash must render verbatim")
}
