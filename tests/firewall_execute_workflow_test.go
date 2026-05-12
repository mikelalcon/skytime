package firewall_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestExecuteWorkflow_CallSiteCount asserts that c.ExecuteWorkflow (any
// qualified call to a method named ExecuteWorkflow) appears in exactly
// two production files: pkg/cli/server/web/flowlaunch/launch.go (the
// single seam) and pkg/cli/run.go (synchronous run path retains its
// call site because it needs *WorkflowRun for run.Get).
//
// UI-04 (D-7.3-31). Walks pkg/ and cmd/ via go/ast.
//
// Filters out the testsuite idiom: `env.ExecuteWorkflow` on
// *testsuite.TestWorkflowEnvironment is a different API surface
// (Temporal SDK test environment) and is allowed in production
// helper files like pkg/interpreter/replay_helper.go (the
// Phase 5 D5-D1 replay-determinism helper). The firewall pins
// production client-dispatch call sites, not test-environment
// invocations.
func TestExecuteWorkflow_CallSiteCount(t *testing.T) {
	allowed := map[string]bool{
		"pkg/cli/server/web/flowlaunch/launch.go": true,
		"pkg/cli/run.go":                          true,
	}
	callSites := collectCallSites(t, "ExecuteWorkflow")

	var unexpected []string
	for _, cs := range callSites {
		// Skip testsuite.TestWorkflowEnvironment.ExecuteWorkflow —
		// not a client.Client dispatch. By convention this receiver
		// is bound to `env` (matches the Temporal SDK testsuite docs).
		if cs.qualifier == "env" {
			continue
		}
		if !allowed[cs.rel] {
			unexpected = append(unexpected, cs.String())
		}
	}
	require.Empty(t, unexpected,
		"c.ExecuteWorkflow may only be called from %v; found %d extra production call site(s): %v",
		sortedKeys(allowed), len(unexpected), unexpected)

	// Defense-in-depth: also require BOTH allowed sites actually call
	// ExecuteWorkflow (otherwise the firewall is vacuous).
	seen := map[string]bool{}
	for _, cs := range callSites {
		if cs.qualifier == "env" {
			continue
		}
		if allowed[cs.rel] {
			seen[cs.rel] = true
		}
	}
	for path := range allowed {
		require.True(t, seen[path],
			"expected c.ExecuteWorkflow call site %q is missing — did the refactor remove it accidentally?", path)
	}
}

// TestBuildWorkflowInput_CallSiteCount asserts that BuildWorkflowInput
// appears at exactly three production sites:
//   - pkg/cli/server/web/flowlaunch/launch.go — internal same-package
//     call from Execute() (*ast.Ident form: BuildWorkflowInput(...))
//   - pkg/cli/run.go — qualified call (*ast.SelectorExpr:
//     flowlaunch.BuildWorkflowInput(...))
//   - pkg/extension/schedules/schedules.go — same qualified form
//
// UI-04 single source of truth (Research Open Q 1 Option 1).
//
// CRITICAL: the AST walk inside collectCallSites switches on BOTH
// *ast.SelectorExpr AND *ast.Ident — see comment there. Matching only
// SelectorExpr would silently drop the internal in-package call and
// the test would fail with "2 of 3 expected", masking the real intent.
func TestBuildWorkflowInput_CallSiteCount(t *testing.T) {
	allowed := map[string]bool{
		"pkg/cli/server/web/flowlaunch/launch.go": true,
		"pkg/cli/run.go":                          true,
		"pkg/extension/schedules/schedules.go":    true,
	}
	callSites := collectCallSites(t, "BuildWorkflowInput")

	// Filter: accept BOTH the qualified flowlaunch.BuildWorkflowInput
	// form (qualifier == "flowlaunch") AND the unqualified internal
	// same-package form (qualifier == "" AND rel ==
	// "pkg/cli/server/web/flowlaunch/launch.go"). Any other shape is a
	// false positive (e.g., a different package's method with the same
	// name).
	var filtered []callSite
	for _, cs := range callSites {
		switch {
		case cs.qualifier == "flowlaunch":
			filtered = append(filtered, cs)
		case cs.qualifier == "" && cs.rel == "pkg/cli/server/web/flowlaunch/launch.go":
			// *ast.Ident form: in-package internal call inside the
			// flowlaunch package itself. This is the load-bearing
			// third call site.
			filtered = append(filtered, cs)
		}
	}

	var unexpected []string
	for _, cs := range filtered {
		if !allowed[cs.rel] {
			unexpected = append(unexpected, cs.String())
		}
	}
	require.Empty(t, unexpected,
		"BuildWorkflowInput may only be called from %v; found %d extra production call site(s): %v",
		sortedKeys(allowed), len(unexpected), unexpected)

	seen := map[string]bool{}
	for _, cs := range filtered {
		if allowed[cs.rel] {
			seen[cs.rel] = true
		}
	}
	for path := range allowed {
		require.True(t, seen[path],
			"expected BuildWorkflowInput call site %q is missing", path)
	}

	// Defense-in-depth: lock the kind-by-site contract so a future
	// refactor that accidentally moves the internal call OUT of the
	// flowlaunch package (turning *ast.Ident into *ast.SelectorExpr,
	// or vice versa) fails loudly.
	require.Len(t, filtered, 3,
		"expected exactly 3 BuildWorkflowInput call sites (1 in-package Ident inside flowlaunch/launch.go + 2 qualified SelectorExpr); got %d: %v",
		len(filtered), filtered)
}

// callSite captures one CallExpr located by collectCallSites.
type callSite struct {
	rel       string // relative path from module root (forward slashes)
	qualifier string // the X in X.Method for SelectorExpr; "" for *ast.Ident (in-package call)
	fnName    string // sel.Sel.Name OR ident.Name — always == the name argument passed to collectCallSites
	pos       token.Position
}

func (cs callSite) String() string {
	return cs.rel + ":" + cs.pos.String()
}

// collectCallSites walks pkg/ + cmd/, parses every non-test .go file,
// and returns every CallExpr whose Fun matches:
//   - *ast.SelectorExpr with Sel.Name == name (qualifier captures sel.X.Name)
//   - *ast.Ident with Name == name             (qualifier == "" — in-package call)
//
// The switch on both shapes is REQUIRED for in-package internal calls
// like flowlaunch.Execute()'s own call to BuildWorkflowInput, which
// Go AST emits as *ast.Ident (no package qualifier). Matching only
// SelectorExpr silently drops same-package call sites.
func collectCallSites(t *testing.T, name string) []callSite {
	t.Helper()
	moduleRoot := findModuleRootCLI(t)
	fset := token.NewFileSet()
	var out []callSite
	for _, root := range []string{"pkg", "cmd"} {
		walkErr := filepath.Walk(filepath.Join(moduleRoot, root), func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, relErr := filepath.Rel(moduleRoot, path)
			if relErr != nil {
				return relErr
			}
			f, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				return perr
			}
			ast.Inspect(f, func(n ast.Node) bool {
				ce, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch fun := ce.Fun.(type) {
				case *ast.SelectorExpr:
					// qualified call: pkg.Name(...) or recv.Name(...)
					if fun.Sel.Name != name {
						return true
					}
					var qual string
					if id, ok := fun.X.(*ast.Ident); ok {
						qual = id.Name
					}
					out = append(out, callSite{
						rel:       filepath.ToSlash(rel),
						qualifier: qual,
						fnName:    fun.Sel.Name,
						pos:       fset.Position(ce.Pos()),
					})
				case *ast.Ident:
					// in-package internal call: Name(...) (no qualifier)
					// Go AST emits Ident, not SelectorExpr, when the
					// call target is in the same package.
					if fun.Name != name {
						return true
					}
					out = append(out, callSite{
						rel:       filepath.ToSlash(rel),
						qualifier: "", // sentinel: in-package call
						fnName:    fun.Name,
						pos:       fset.Position(ce.Pos()),
					})
				}
				return true
			})
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walk %s: %v", root, walkErr)
		}
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
