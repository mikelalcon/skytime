// Phase 07.3-05 Task 1: headings-firewall gate for the new long-form
// dashboard walkthrough at docs/walkthroughs/dashboard.md.
//
// Why: future doc refactors must not silently drop one of the
// canonical H2 sections (Prerequisites, Step 1..6, Browser UAT,
// Security Note, Troubleshooting, CLI Flag Reference, What This Phase
// Validated). The gate is bytes.Contains + start-of-line presence —
// no Markdown AST, no parser; exits in milliseconds.
//
// Mirrors tests/walkthrough_github_webhook_headings_test.go and lives
// in the same firewall_test package so it can reuse findModuleRootCLI
// from firewall_cli_test.go.
package firewall_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWalkthroughDashboardHeadings pins docs/walkthroughs/dashboard.md
// to its required H2 section structure. Future doc edits that drop a
// required UAT step will fail this test.
//
// Mirrors TestWalkthrough_GitHubWebhookHeadings in
// walkthrough_github_webhook_headings_test.go.
func TestWalkthroughDashboardHeadings(t *testing.T) {
	required := []string{
		"## Prerequisites",
		"## Step 1 — Start the dev Temporal cluster",
		"## Step 2 — Start `skytime server`",
		"## Step 3 — Open the dashboard",
		"## Step 4 — Trigger a flow manually",
		"## Step 5 — Fire a webhook (optional, requires `gh`)",
		"## Step 6 — Crash-recovery demo",
		"## Browser UAT",
		"## Security Note",
		"## Troubleshooting",
		"## CLI Flag Reference",
		"## What This Phase Validated",
	}
	moduleRoot := findModuleRootCLI(t)
	path := filepath.Join(moduleRoot, "docs", "walkthroughs", "dashboard.md")
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(b)

	var missing []string
	for _, h := range required {
		if !strings.Contains(content, h) {
			missing = append(missing, h)
		}
	}
	require.Empty(t, missing,
		"docs/walkthroughs/dashboard.md is missing required H2 sections:\n  %s",
		strings.Join(missing, "\n  "))

	// Defense-in-depth: ensure each required section is on its own
	// line as an H2 (not a substring inside a larger string).
	for _, h := range required {
		require.True(t,
			strings.Contains(content, "\n"+h+"\n") || strings.HasPrefix(content, h+"\n"),
			"required H2 %q is not at start-of-line", h)
	}

	// Required content references — load-bearing strings the body of
	// the walkthrough promises in its troubleshooting, CLI-flag, and
	// security sections. Drop any of these and a reader following the
	// doc hits a confusing failure mode.
	requiredReferences := []string{
		"--replay-history-threshold=50",        // CLI flag default
		"SKYTIME_TEMPORAL_WEB_UI",              // env var for --temporal-web-ui
		"Authorization: <redacted>",            // redaction proof in delivery rows
		"localhost:8233",                       // Temporal Web UI deep-link target
		"event: shutdown",                      // D-7.3-07 shutdown frame contract
		"origin not allowed",                   // M3 same-origin error body
		"authorization`/`secret`/`token`/`key`/`signature", // redaction substring list
	}
	for _, ref := range requiredReferences {
		require.True(t, strings.Contains(content, ref),
			"required reference missing from docs/walkthroughs/dashboard.md: %q", ref)
	}
}
