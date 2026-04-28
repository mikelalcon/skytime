package activity_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNoTemporalImportsOutsidePkgActivity walks every Go file under the
// module's pkg/ tree EXCEPT pkg/activity and asserts none imports any
// go.temporal.io/sdk/* path. This enforces PROJECT.md's "no context bleed" —
// only pkg/activity is allowed to bridge to Temporal.
//
// Mirrors the per-package firewall tests in pkg/parser, pkg/extension (Phase 1)
// but inverts the check — those tests verify their OWN package is clean; this
// test verifies every OTHER pkg/* directory is clean while pkg/activity is
// allowed to import the SDK.
func TestNoTemporalImportsOutsidePkgActivity(t *testing.T) {
	moduleRoot := findModuleRoot(t)
	pkgRoot := filepath.Join(moduleRoot, "pkg")

	fset := token.NewFileSet()
	checked := 0
	walkErr := filepath.Walk(pkgRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Skip pkg/activity — the firewall ALLOWS it to import temporal.
		rel, relErr := filepath.Rel(pkgRoot, path)
		if relErr != nil {
			return relErr
		}
		// rel looks like "activity/foo.go" or "extension/handler.go".
		if rel == "activity" || strings.HasPrefix(rel, "activity"+string(filepath.Separator)) {
			return nil
		}

		f, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(importPath, "go.temporal.io/sdk") {
				t.Errorf("FIREWALL VIOLATION: %s imports %q — only pkg/activity may import go.temporal.io/sdk/*", path, importPath)
			}
			checked++
		}
		return nil
	})
	require.NoError(t, walkErr)
	t.Logf("checked %d import paths across pkg/ (excluding pkg/activity); none imported go.temporal.io/sdk/*", checked)
}

// TestPkgActivity_AllowedToImportTemporal is a meta-test that catches an
// inversion bug: if the firewall test above accidentally allowlists
// pkg/activity but pkg/activity files don't actually import temporal, the
// firewall is a no-op (it claims to allow something that nobody is using).
// Assert at least one pkg/activity *.go file (excluding _test.go) imports a
// go.temporal.io/sdk path.
func TestPkgActivity_AllowedToImportTemporal(t *testing.T) {
	moduleRoot := findModuleRoot(t)
	activityDir := filepath.Join(moduleRoot, "pkg", "activity")

	fset := token.NewFileSet()
	sawTemporalImport := false
	walkErr := filepath.Walk(activityDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		// Production sources only — _test.go files don't count toward the
		// "package actually uses the SDK" assertion.
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(importPath, "go.temporal.io/sdk") {
				sawTemporalImport = true
				return filepath.SkipDir
			}
		}
		return nil
	})
	require.NoError(t, walkErr)
	require.True(t, sawTemporalImport,
		"no pkg/activity *.go (non-test) file imports go.temporal.io/sdk/* — the firewall is a no-op")
}

// findModuleRoot walks up from the test working directory until it finds a
// go.mod file. Tests run with cwd == package dir, so for pkg/activity tests
// the walk goes up two levels (pkg/activity → pkg → repo root).
func findModuleRoot(t *testing.T) string {
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
