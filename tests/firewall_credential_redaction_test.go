// D-07-10 credential-redaction firewall — AST-walks production Go
// files in pkg/dag, pkg/extension, pkg/extension/builtin,
// pkg/extension/receiver, and examples/http-github-webhook/extensions/github
// and rejects fmt-family calls whose first arg is a string literal
// containing %+v or %#v.
//
// Why: %+v / %#v print struct fields exhaustively, which would defeat
// the Secret wrapper's redaction (Secret's String/GoString/Format
// honor %v / %s / %q but %+v on a containing struct exposes the
// wrapped Reveal()). Rejecting these verbs in the load-bearing
// trigger/credential code paths is a final-line-of-defense backstop
// behind the type-level redaction in pkg/extension/secret.go.
//
// Scope: Phase 7 covered pkg/dag, pkg/extension, and
// pkg/extension/builtin. Phase 7.1 extends to pkg/extension/receiver
// (handler.go's .Reveal() call sites for HMAC computation) and
// examples/http-github-webhook/extensions/github (Plan 03's
// github.webhook source factory production files).
//
// Test files (_test.go) are exempt — tests legitimately use %+v for
// diff output where the subject is not a Secret.
//
// Lives in package firewall_test alongside firewall_cli_test.go +
// docgen_drift_test.go (the canonical external-test-package
// convention for tests/).
package firewall_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCredentialRedactionFirewall walks Go source files in the
// load-bearing trigger/extension packages and rejects %+v / %#v in
// production fmt calls.
func TestCredentialRedactionFirewall(t *testing.T) {
	moduleRoot := findModuleRootRedaction(t)
	targetDirs := []string{
		filepath.Join(moduleRoot, "pkg", "dag"),
		filepath.Join(moduleRoot, "pkg", "extension"),
		filepath.Join(moduleRoot, "pkg", "extension", "builtin"),
		filepath.Join(moduleRoot, "pkg", "extension", "receiver"),
		filepath.Join(moduleRoot, "examples", "http-github-webhook", "extensions", "github"),
		// Phase 7.2: core.cron + cron Schedule reconciler — defense in depth.
		// core has no credentials (cron has no per-trigger secret); schedules
		// operates on credential-free Schedule resources. Adding to the firewall
		// sweep ensures any future %+v/%#v that incidentally surfaces a Trigger
		// value gets rejected immediately. The schedules dir is forward-looking
		// (Plan 02 creates it); until then the missing-dir branch handles it.
		filepath.Join(moduleRoot, "pkg", "extension", "builtin", "core"),
		filepath.Join(moduleRoot, "pkg", "extension", "schedules"),
	}

	fset := token.NewFileSet()
	var allViolations []string

	for _, dir := range targetDirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			t.Logf("skip non-existent target dir: %s", dir)
			continue
		}

		walkErr := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			if strings.HasSuffix(path, "_test.go") {
				return nil // test files are exempt
			}
			file, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if perr != nil {
				return perr
			}
			rel, _ := filepath.Rel(moduleRoot, path)
			for _, v := range matchViolations(fset, file) {
				allViolations = append(allViolations, fmt.Sprintf("%s: %s", rel, v))
			}
			return nil
		})
		require.NoError(t, walkErr, "walk %s", dir)
	}

	if len(allViolations) > 0 {
		t.Errorf("D-07-10 FIREWALL VIOLATION: %%+v / %%#v formatting verbs found in production code under pkg/dag, pkg/extension, pkg/extension/builtin. These verbs print struct fields exhaustively and would defeat extension.Secret redaction. Use %%v / %%s / %%q instead, or refactor the call site to NOT format the secret-containing struct.\n\nViolations:\n  %s",
			strings.Join(allViolations, "\n  "))
	}

	t.Logf("D-07-10 firewall: scanned %d target dir(s) (Phase 7 dag/extension/builtin + Phase 7.1 receiver/github extension); no %%+v / %%#v in production code", len(targetDirs))
}

// TestCredentialRedactionFirewall_AcceptsCleanCode is the positive
// regression test — confirms the matcher does not over-fire on
// legitimate %v / %s / %d uses. Protects against a future "make the
// matcher broader" change that accidentally rejects all fmt calls.
func TestCredentialRedactionFirewall_AcceptsCleanCode(t *testing.T) {
	cleanSrc := `package x

import "fmt"

func ok() string {
	return fmt.Sprintf("%v %s %d %q", 1, "two", 3, "four")
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "x.go", cleanSrc, 0)
	require.NoError(t, err)

	violations := matchViolations(fset, file)
	require.Empty(t, violations,
		"clean code (no %%+v / %%#v) must produce zero violations; got: %v", violations)
}

// matchViolations walks file and returns "<file>:<line>:<col> <fn>"
// strings for every fmt.<Print*> call whose first arg is a string
// literal containing the literal %+v or %#v sequence.
//
// Match rule: identifier "fmt" + selector starting with "Print" or
// equal to one of {Sprintf, Errorf, Fprintf} + first arg is a
// string-literal BasicLit + literal value contains the verbs.
func matchViolations(fset *token.FileSet, file *ast.File) []string {
	var out []string

	// fmt.<funcName> with a format-string first-arg. Cover the common
	// Printf-family entry points — both the simple string-returning
	// (Sprintf, Errorf) and the Writer-taking (Fprintf, Fprintln if
	// it ever takes a format) variants. Print/Println do NOT take a
	// format arg, so we skip them; Printf does.
	formatPos := map[string]int{
		"Sprintf":  0, // first arg is the format string
		"Sprint":   -1,
		"Sprintln": -1,
		"Printf":   0,
		"Print":    -1,
		"Println":  -1,
		"Errorf":   0,
		"Fprintf":  1, // first arg is io.Writer; second is format
		"Fprint":   -1,
		"Fprintln": -1,
	}

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if ident.Name != "fmt" {
			return true
		}
		argIdx, known := formatPos[sel.Sel.Name]
		if !known || argIdx < 0 {
			return true // not a format-string family, or skipped variant
		}
		if argIdx >= len(call.Args) {
			return true
		}
		lit, ok := call.Args[argIdx].(*ast.BasicLit)
		if !ok {
			return true
		}
		if lit.Kind != token.STRING {
			return true
		}
		// Check for the forbidden verbs verbatim. lit.Value contains
		// the surrounding quotes; that's fine — we're substring-matching.
		if strings.Contains(lit.Value, "%+v") || strings.Contains(lit.Value, "%#v") {
			pos := fset.Position(call.Pos())
			out = append(out, fmt.Sprintf("%d:%d fmt.%s contains forbidden verb in literal %s",
				pos.Line, pos.Column, sel.Sel.Name, lit.Value))
		}
		return true
	})

	return out
}

// findModuleRootRedaction walks up from cwd looking for go.mod.
// Co-located helper because tests/ has no other source — duplicating
// is cleaner than exporting from firewall_cli_test.go (its findModuleRootCLI
// is unexported).
func findModuleRootRedaction(t *testing.T) string {
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
