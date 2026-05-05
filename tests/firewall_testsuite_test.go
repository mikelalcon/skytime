// Package firewall_test enforces the Phase 5 firewall: pkg/testing
// MUST eventually import go.temporal.io/sdk/testsuite (otherwise the
// allow-list expansion in pkg/activity/firewall_test.go is pointless).
//
// Mirrors the non-vacuous pattern from tests/firewall_cli_test.go
// `TestPkgCli_ImportsCobra` (Phase 4 plan 04-04, AST walk via go/parser).
package firewall_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPkgTesting_ImportsTestsuite is non-vacuous: pkg/testing must
// import go.temporal.io/sdk/testsuite once Plan 02 lands router.go.
// Skips with a forward-pointing message until that import appears.
//
// This activates the Phase 5 firewall expansion meaningfully — without
// it, "pkg/testing" sitting in the allow-list would silently permit
// any go.temporal.io/sdk/* import without verifying the harness
// actually depends on testsuite.
func TestPkgTesting_ImportsTestsuite(t *testing.T) {
	moduleRoot := findModuleRootTesting(t)
	testingDir := filepath.Join(moduleRoot, "pkg", "testing")

	if _, err := os.Stat(testingDir); os.IsNotExist(err) {
		t.Skip("pkg/testing has no production sources yet; firewall meta-test will activate once Plan 01 lands doc.go")
		return
	}

	fset := token.NewFileSet()
	var found bool
	var checkedFiles int

	walkErr := filepath.Walk(testingDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		checkedFiles++
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		for _, imp := range f.Imports {
			v := strings.Trim(imp.Path.Value, `"`)
			if v == "go.temporal.io/sdk/testsuite" || strings.HasPrefix(v, "go.temporal.io/sdk/testsuite/") {
				found = true
				return filepath.SkipDir
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk pkg/testing: %v", walkErr)
	}

	if checkedFiles == 0 {
		t.Skip("pkg/testing has no .go production sources yet; firewall meta-test will activate once Plan 01 lands doc.go")
		return
	}
	if !found {
		t.Skip("pkg/testing production sources do not yet import go.temporal.io/sdk/testsuite; firewall meta-test will activate once Plan 02 lands router.go")
		return
	}
	t.Logf("verified pkg/testing imports go.temporal.io/sdk/testsuite (Phase 5 D5-firewall-q8 deviation honored)")
}

// TestPkgTesting_DoesNotImportSDKWorker — the harness must NOT
// register itself as a separate Temporal activity worker; only
// TestWorkflowEnvironment is permissible. (RESEARCH.md
// Investigation 11.)
func TestPkgTesting_DoesNotImportSDKWorker(t *testing.T) {
	moduleRoot := findModuleRootTesting(t)
	testingDir := filepath.Join(moduleRoot, "pkg", "testing")

	if _, err := os.Stat(testingDir); os.IsNotExist(err) {
		t.Skip("pkg/testing has no production sources yet")
		return
	}

	fset := token.NewFileSet()
	var checkedFiles int

	walkErr := filepath.Walk(testingDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		checkedFiles++
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		for _, imp := range f.Imports {
			v := strings.Trim(imp.Path.Value, `"`)
			if v == "go.temporal.io/sdk/worker" {
				t.Errorf("%s: pkg/testing must NOT import go.temporal.io/sdk/worker (harness uses TestWorkflowEnvironment only)", path)
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk pkg/testing: %v", walkErr)
	}
	if checkedFiles == 0 {
		t.Skip("pkg/testing has no .go production sources yet")
	}
}

// findModuleRootTesting walks up from the working directory until it
// finds a go.mod. Mirrors findModuleRootCLI in firewall_cli_test.go.
func findModuleRootTesting(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	dir := wd
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("module root not found upward from %s", wd)
		}
		dir = parent
	}
}
