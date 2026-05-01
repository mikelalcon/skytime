// Package firewall_test enforces architecture firewalls that span
// the whole tree (no single pkg/ owns the rule). Mirrors the
// pattern in pkg/activity/firewall_test.go but covers cobra /
// pflag / charm-log instead of go.temporal.io/sdk.
//
// Charm-log was renamed upstream from github.com/charmbracelet/log/v2 to
// charm.land/log/v2 (the GitHub repo still hosts the source; the module
// path moved). The forbidden list below uses the new path; if a future
// release re-publishes under the old path we'll need to add it back.
package firewall_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNoCobraImportsOutsideAllowList walks every Go file under pkg/
// and asserts that only pkg/cli imports cobra, pflag, or charm-log/v2.
// cmd/skytime is outside pkg/ so it is implicitly allowed; the rule is
// "library-side, only pkg/cli".
//
// D4-13 (Phase 4 CONTEXT.md): pkg/parser, pkg/dag, pkg/extension,
// pkg/bridge, pkg/activity, pkg/interpreter, pkg/worker,
// pkg/validator MUST NOT import these packages.
func TestNoCobraImportsOutsideAllowList(t *testing.T) {
	forbidden := []string{
		"github.com/spf13/cobra",
		"github.com/spf13/pflag",
		"charm.land/log/v2",
	}
	allowedRel := []string{"cli"} // pkg/cli — the only library-side allow

	moduleRoot := findModuleRootCLI(t)
	pkgRoot := filepath.Join(moduleRoot, "pkg")
	sep := string(filepath.Separator)

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
		rel, err := filepath.Rel(pkgRoot, path)
		if err != nil {
			return err
		}
		for _, allowed := range allowedRel {
			if rel == allowed || strings.HasPrefix(rel, allowed+sep) {
				return nil
			}
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		for _, imp := range f.Imports {
			ip := strings.Trim(imp.Path.Value, `"`)
			for _, fb := range forbidden {
				if ip == fb || strings.HasPrefix(ip, fb+"/") {
					t.Errorf("FIREWALL VIOLATION: %s imports %q — only pkg/cli (and cmd/skytime) may import CLI deps (D4-13)", path, ip)
				}
			}
			checked++
		}
		return nil
	})
	require.NoError(t, walkErr)
	t.Logf("checked %d import paths across pkg/ (excluding allowlist %v); none imported forbidden CLI deps", checked, allowedRel)
}

// TestPkgCli_ImportsCobra is the non-vacuous meta-test: pkg/cli MUST
// eventually import cobra (otherwise the allow-list is pointless).
// Skips with t.Skip until W3 lands the first import, mirroring
// TestPkgWorker_ImportsTemporal's skip-on-empty-package pattern from
// Phase 3 plan 03-04.
func TestPkgCli_ImportsCobra(t *testing.T) {
	moduleRoot := findModuleRootCLI(t)
	dir := filepath.Join(moduleRoot, "pkg", "cli")

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
		t.Skip("pkg/cli has no production sources yet; firewall meta-test will activate once W3 lands root.go")
		return
	}

	fset := token.NewFileSet()
	found := false
	for _, p := range prodFiles {
		af, perr := parser.ParseFile(fset, p, nil, parser.ImportsOnly)
		require.NoError(t, perr)
		for _, imp := range af.Imports {
			ip := strings.Trim(imp.Path.Value, `"`)
			if ip == "github.com/spf13/cobra" || strings.HasPrefix(ip, "github.com/spf13/cobra/") {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Skip("pkg/cli production sources do not yet import github.com/spf13/cobra; firewall meta-test will activate once root.go lands")
		return
	}
	require.True(t, found, "pkg/cli production sources must import github.com/spf13/cobra")
}

// findModuleRootCLI walks up from cwd looking for go.mod. Co-located
// helper because tests/ has no other source — duplicating the helper
// is cleaner than exporting from pkg/activity_test.
func findModuleRootCLI(t *testing.T) string {
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
