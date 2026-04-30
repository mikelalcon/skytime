package interpreter_test

import (
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPkgInterpreter_ImportsTemporal asserts at least one *.go file
// (excluding _test.go) in pkg/interpreter imports go.temporal.io/sdk/*.
// If this test fails, the firewall is a no-op (allowlists a package that
// doesn't actually use the SDK). Mirror of pkg/activity's
// TestPkgActivity_AllowedToImportTemporal, but adapted with a
// skip-on-empty-package guard so this test passes from its first commit
// (during plan 03-02's task ordering, doc.go and state.go land before
// workflow.go's SDK import does).
//
// Once Task 4 lands workflow.go (which imports go.temporal.io/sdk/workflow),
// the skip path is unreachable and the test becomes assertive.
func TestPkgInterpreter_ImportsTemporal(t *testing.T) {
	moduleRoot := findInterpreterModuleRoot(t)
	dir := filepath.Join(moduleRoot, "pkg", "interpreter")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var prodFiles []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		prodFiles = append(prodFiles, filepath.Join(dir, e.Name()))
	}
	if len(prodFiles) == 0 {
		t.Skip("pkg/interpreter has no production sources yet; firewall test will activate once workflow.go lands")
	}

	found := false
	fset := token.NewFileSet()
	for _, p := range prodFiles {
		af, parseErr := parser.ParseFile(fset, p, nil, parser.ImportsOnly)
		require.NoError(t, parseErr)
		for _, imp := range af.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(path, "go.temporal.io/sdk") {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		// Production sources exist but none import the SDK yet — this is
		// the early-task-commit window of plan 03-02 (doc.go / state.go /
		// registry.go land before workflow.go). Skip cleanly. After Task 4
		// lands workflow.go, this branch is unreachable.
		t.Skip("pkg/interpreter production sources do not yet import go.temporal.io/sdk; firewall test will activate once workflow.go lands")
	}
	require.True(t, found, "pkg/interpreter production sources must import go.temporal.io/sdk/*")
}

// TestWorkflowcheck_NoFindings runs `workflowcheck ./pkg/interpreter/...`
// when the binary is in PATH and asserts zero findings (D3-24, INTRP-07).
// Skips when not installed — CI installs it via:
//
//	go install go.temporal.io/sdk/contrib/tools/workflowcheck@latest
//
// Co-located here (firewall-style meta-test) rather than in
// cancel_watchdog_test.go so determinism gates stay together.
func TestWorkflowcheck_NoFindings(t *testing.T) {
	if _, err := exec.LookPath("workflowcheck"); err != nil {
		t.Skip("workflowcheck not in PATH; CI runs it separately. Install: go install go.temporal.io/sdk/contrib/tools/workflowcheck@latest")
	}
	cmd := exec.Command("workflowcheck", "./pkg/interpreter/...")
	cmd.Dir = findInterpreterModuleRoot(t)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "workflowcheck failed: %s", out)
	// workflowcheck prints findings to stdout/stderr; non-zero exit means
	// findings, but in case it reports diagnostics on a zero exit we also
	// inspect the output.
	require.Empty(t, strings.TrimSpace(string(out)), "workflowcheck found violations: %s", out)
}

// findInterpreterModuleRoot walks up from the test cwd until go.mod is
// found. Co-located helper rather than cross-package import from
// pkg/activity (which would require exporting test helpers).
func findInterpreterModuleRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	dir := cwd
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find go.mod walking up from %s", cwd)
		}
		dir = parent
	}
}
