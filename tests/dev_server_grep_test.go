// D-07-22 grep gate — iterate every tracked file (via git ls-files)
// and reject the literal "dev-server" outside an allow-list.
//
// Phase 7 hard-renamed `skytime dev-server` to `skytime dev-temporal`
// per D-07-21 (no deprecation alias). This test is the CI-side
// regression-prevention against accidental re-introduction of the
// legacy literal — code review can miss a single doc paragraph or a
// help-text constant; the grep gate cannot.
//
// Allow-list (documented inline below):
//   - .planning/ — historical phase plans, summaries, research, and
//     context docs that legitimately reference the legacy name when
//     describing what was renamed.
//   - CHANGELOG.md (if present) — release-notes file inherently lists
//     prior names.
//   - tests/dev_server_grep_test.go (this file) — the gate itself
//     contains the literal as the thing being checked.
//
// Lives in package firewall_test alongside firewall_cli_test.go etc.
package firewall_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNoDevServerLiteralRemains scans every git-tracked file for the
// literal "dev-server" string. Any hit outside the allow-list fails
// the test with the full violation list so a single CI run pinpoints
// every overlooked occurrence.
func TestNoDevServerLiteralRemains(t *testing.T) {
	moduleRoot := findModuleRootGrep(t)

	cmd := exec.Command("git", "ls-files")
	cmd.Dir = moduleRoot
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Run(), "git ls-files failed: %s", stderr.String())

	files := strings.Split(strings.TrimSpace(stdout.String()), "\n")

	// Allow-list: paths or path-prefixes whose contents are exempt
	// from the gate.
	allowedPrefixes := []string{
		".planning/", // historical phase docs reference the legacy name
	}
	allowedFiles := map[string]bool{
		"CHANGELOG.md":                    true,
		"tests/dev_server_grep_test.go":   true, // the gate itself
	}

	var violations []string
	scanned := 0
	for _, rel := range files {
		if rel == "" {
			continue
		}
		if allowedFiles[rel] {
			continue
		}
		skip := false
		for _, pref := range allowedPrefixes {
			if strings.HasPrefix(rel, pref) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		abs := filepath.Join(moduleRoot, rel)
		info, err := os.Stat(abs)
		if err != nil || info.IsDir() {
			// Submodule entries from ls-files can stat to a directory
			// or fail; skip without erroring.
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			// Binary files or unreadable entries — skip without
			// erroring; a binary cannot meaningfully contain prose
			// the grep cares about.
			continue
		}
		scanned++
		if bytes.Contains(data, []byte("dev-server")) {
			violations = append(violations, rel)
		}
	}

	if len(violations) > 0 {
		t.Errorf("D-07-22 GREP GATE VIOLATION: the literal \"dev-server\" appears in %d tracked file(s) outside the allow-list (.planning/, CHANGELOG.md, tests/dev_server_grep_test.go). The Phase 7 hard rename per D-07-21 dropped this name with no deprecation alias — every occurrence must become \"dev-temporal\" (Skytime's renamed subcommand) or \"temporal dev server\" (the underlying Temporal subprocess). Files:\n  %s",
			len(violations), strings.Join(violations, "\n  "))
	}

	t.Logf("D-07-22 grep gate: scanned %d tracked file(s); zero \"dev-server\" literal outside allow-list", scanned)
}

// TestNoDevServerLiteralRemains_AllowListIsEffective is a sanity test
// that confirms the gate's allow-list excludes itself from the scan
// — without this, the gate would always fail because this file
// contains the literal as part of its docstring and matcher.
func TestNoDevServerLiteralRemains_AllowListIsEffective(t *testing.T) {
	moduleRoot := findModuleRootGrep(t)
	gateFile := filepath.Join(moduleRoot, "tests", "dev_server_grep_test.go")
	data, err := os.ReadFile(gateFile)
	require.NoError(t, err)
	require.Contains(t, string(data), "dev-server",
		"this file MUST contain the literal as the thing being grepped for")
	require.Contains(t, string(data), "tests/dev_server_grep_test.go",
		"the allow-list MUST self-reference so the gate doesn't fail on its own contents")
}

// findModuleRootGrep walks up from cwd looking for go.mod.
// Co-located helper for the tests/ package convention; mirrors
// findModuleRootCLI in firewall_cli_test.go.
func findModuleRootGrep(t *testing.T) string {
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

// keep fmt import alive for any future format-style violation reporter.
var _ = fmt.Sprintf
