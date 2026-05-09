// Plan 07.1-08 lightweight headings presence gate for the new
// long-form walkthrough at docs/walkthroughs/github-webhook.md.
//
// Why: future doc refactors must not silently drop one of the
// canonical H2 sections (Prerequisites, Step 1..7, Troubleshooting,
// Decoding the structured log line, What's next) and must not
// re-introduce the "30 seconds later" claim that Plan 07's locked
// no-sleep demo shape dropped. The gate is bytes.Contains-only —
// no Markdown AST, no parser; exits in milliseconds.
//
// Lives in package firewall_test alongside firewall_cli_test.go etc.
package firewall_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWalkthrough_GitHubWebhookHeadings asserts the long-form
// walkthrough doc exists with all required H1 + H2 sections, contains
// the load-bearing references the troubleshooting and demo sections
// promise (gh extension install cli/gh-webhook, X-Hub-Signature-256,
// error_class, etc.), and does NOT contain the "30 seconds later"
// claim that Plan 07's no-sleep demo dropped.
func TestWalkthrough_GitHubWebhookHeadings(t *testing.T) {
	moduleRoot := findModuleRootHeadings(t)
	docPath := filepath.Join(moduleRoot, "docs", "walkthroughs", "github-webhook.md")

	content, err := os.ReadFile(docPath)
	require.NoError(t, err, "walkthrough doc must exist at docs/walkthroughs/github-webhook.md")

	requiredHeadings := []string{
		"# GitHub webhook walkthrough",
		"## Prerequisites",
		"## Step 1: Generate a webhook signing secret",
		"## Step 2: Add the secret to your credfile",
		"## Step 3: Start the local Temporal dev server",
		"## Step 4: Start the Skytime server with the webhook_demo flow",
		"## Step 5: Forward GitHub webhooks to your local server",
		"## Step 6: Trigger an event",
		"## Step 7: Crash-recovery demonstration",
		"## Troubleshooting",
		"## Decoding the structured log line",
		"## What's next",
	}
	for _, h := range requiredHeadings {
		if !bytes.Contains(content, []byte(h)) {
			t.Errorf("required heading missing from docs/walkthroughs/github-webhook.md: %q", h)
		}
	}

	// Required content references — load-bearing strings the body of
	// the walkthrough promises in its troubleshooting and demo
	// sections. Drop any of these and a reader following the doc hits
	// a confusing failure mode.
	requiredReferences := []string{
		"gh extension install cli/gh-webhook", // Pitfall 8 — extension is not built into gh
		"gh webhook forward",                  // the canonical command
		"webhook_demo.star",                   // the demo flow file
		"X-Hub-Signature-256",                 // TRIG-09 / D-7.1-04 signature header
		"error_class",                         // D-7.1-15 log-line decoder
		"signature_mismatch",                  // locked errorClass taxonomy entry
		"duplicate_skipped",                   // locked errorClass taxonomy entry
		"WorkflowExecutionErrorWhenAlreadyStarted", // Pitfall 1 troubleshooting
		"401",                                 // troubleshooting status code
		"502",                                 // troubleshooting status code
	}
	for _, ref := range requiredReferences {
		if !bytes.Contains(content, []byte(ref)) {
			t.Errorf("required reference missing from docs/walkthroughs/github-webhook.md: %q", ref)
		}
	}

	// Anti-claim: Plan 07 dropped the "30 seconds later" claim that
	// D-7.1-16 originally suggested, because v1 has no first-class
	// durable sleep primitive. The walkthrough must not re-introduce
	// that claim — the kill-restart demo is the durability proof.
	if bytes.Contains(content, []byte("30 seconds later")) {
		t.Errorf("docs/walkthroughs/github-webhook.md contains forbidden phrase \"30 seconds later\" — Plan 07 locked the demo to cross-activity continuation (no explicit sleep step). Update the walkthrough copy to describe \"kill between activities, restart, label still appears via event history continuation\".")
	}
}

// findModuleRootHeadings walks up from cwd looking for go.mod.
// Co-located helper for the tests/ package convention; mirrors
// findModuleRootCLI in firewall_cli_test.go and findModuleRootGrep
// in dev_server_grep_test.go.
func findModuleRootHeadings(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find go.mod walking up from %s", cwd)
		}
		dir = parent
	}
}
