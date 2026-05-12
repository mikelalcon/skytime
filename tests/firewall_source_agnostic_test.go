package firewall_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Banned source-specific header names. The dashboard renderer
// (pkg/cli/server/web/) MUST stay source-agnostic per D-7.3-17;
// these strings may appear in provider-specific source factories
// (examples/http-github-webhook/extensions/github, etc.) but not
// in the generic delivery rendering path.
var bannedSourceSpecificHeaders = []string{
	"X-GitHub-Event",
	"X-GitHub-Delivery",
	"X-Hub-Signature-256",
	"X-Hub-Signature",
	"Stripe-Signature",
	"X-Slack-Signature",
	"x-amz-sns-message-signature",
}

func TestSourceAgnosticRenderer(t *testing.T) {
	moduleRoot := findModuleRootCLI(t)
	webRoot := filepath.Join(moduleRoot, "pkg", "cli", "server", "web")

	var violations []string
	filesScanned := 0
	walkErr := filepath.Walk(webRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		filesScanned++
		b, rerr := os.ReadFile(path)
		require.NoError(t, rerr)
		for _, banned := range bannedSourceSpecificHeaders {
			if !strings.Contains(string(b), banned) {
				continue
			}
			// Allow if it appears only inside a // comment line —
			// walk lines and check.
			rel, _ := filepath.Rel(moduleRoot, path)
			for lineNo, line := range strings.Split(string(b), "\n") {
				if !strings.Contains(line, banned) {
					continue
				}
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "//") {
					continue // comments are allowed to mention the banned name
				}
				// B2: use fmt.Sprintf for line number, never a
				// hand-rolled itoa (would collide with other
				// firewall_test files in this package).
				violations = append(violations, rel+":"+fmt.Sprintf("%d", lineNo+1)+": "+banned)
			}
		}
		return nil
	})
	require.NoError(t, walkErr)
	require.Empty(t, violations,
		"source-agnostic firewall (D-7.3-17): provider-specific header names found inside %s:\n%s",
		"pkg/cli/server/web/",
		strings.Join(violations, "\n"))

	// Non-vacuous defense: confirm the banned strings DO appear in the
	// provider-specific source factory tree, so this test would catch
	// a regression that moves them into pkg/cli/server/web/.
	//
	// The github source factory lives under
	// examples/http-github-webhook/extensions/github (per D-07-08:
	// source factories live under their owning extension's namespace,
	// not in a separate `triggers.*` package — confirmed against
	// pkg/extension/builtin/ directory layout in 2026-05-12).
	ghRoot := filepath.Join(moduleRoot, "examples", "http-github-webhook", "extensions", "github")
	foundInProvider := false
	_ = filepath.Walk(ghRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		for _, banned := range bannedSourceSpecificHeaders {
			if strings.Contains(string(b), banned) {
				foundInProvider = true
				return filepath.SkipDir
			}
		}
		return nil
	})
	require.True(t, foundInProvider,
		"firewall is vacuous: none of the banned provider-specific header names %v appear under examples/http-github-webhook/extensions/github — did the GitHub source factory get moved?",
		bannedSourceSpecificHeaders)

	require.Greater(t, filesScanned, 0,
		"firewall is vacuous: zero .go files scanned under %s", webRoot)
}
