package firewall_test

import "testing"

func TestSourceAgnosticRenderer(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 02 fills in. Asserts source-specific header NAMES (X-GitHub-Event, X-GitHub-Delivery, Stripe-Signature, X-Hub-Signature-256) do NOT appear as string literals inside pkg/cli/server/web/ subtree. They may appear inside pkg/extension/builtin/triggers/github/, pkg/extension/builtin/http/, and existing receiver source factory packages.")
}
