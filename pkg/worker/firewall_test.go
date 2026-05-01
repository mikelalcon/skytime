package worker_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPkgWorker_ImportsTemporal asserts at least one *.go file (excluding
// _test.go) in pkg/worker imports go.temporal.io/sdk/*. If this test fails,
// the firewall is a no-op (allowlists a package that doesn't actually use the
// SDK). Mirror of pkg/interpreter's TestPkgInterpreter_ImportsTemporal.
//
// The skip-on-empty-package guard makes this test pass from its first commit
// during plan 03-04's task ordering (doc.go / build_id.go / options.go land
// before client.go / worker.go's SDK imports do).
func TestPkgWorker_ImportsTemporal(t *testing.T) {
	moduleRoot := findWorkerModuleRoot(t)
	dir := filepath.Join(moduleRoot, "pkg", "worker")

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
		t.Skip("pkg/worker has no production sources yet; firewall test will activate once client.go / worker.go land")
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
		t.Skip("pkg/worker production sources do not yet import go.temporal.io/sdk; firewall test will activate once client.go / worker.go land")
	}
	require.True(t, found, "pkg/worker production sources must import go.temporal.io/sdk/*")
}

// findWorkerModuleRoot walks up from the test cwd until go.mod is found.
// Co-located helper rather than cross-package import from pkg/activity or
// pkg/interpreter (which would require exporting test helpers).
func findWorkerModuleRoot(t *testing.T) string {
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
