// Plan 07.1-08 cross-package HMAC firewall — AST-walks
// pkg/extension/receiver/*.go and fails if any production file uses
// bytes.Equal. HMAC comparison MUST use crypto/hmac.Equal
// (constant-time); bytes.Equal is timing-attack-vulnerable.
//
// Plan 01's TestSignature_HMACEqualNotBytesEqual only checks
// signature.go (in-package, source-grep). This test extends the
// gate cross-package via a real Go AST walk so a future Reveal-call
// in handler.go or any other receiver file cannot quietly use
// bytes.Equal as a comparison primitive.
//
// The firewall theme matches Phase 7's
// tests/firewall_credential_redaction_test.go — same structural
// pattern, different invariant.
//
// Lives in package firewall_test alongside firewall_cli_test.go etc.
package firewall_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReceiverFirewall_HMACOnly walks pkg/extension/receiver/*.go and
// fails if any production file uses bytes.Equal. HMAC comparison MUST
// use crypto/hmac.Equal (constant-time); bytes.Equal is
// timing-attack-vulnerable. Plan 01's TestSignature_HMACEqualNotBytesEqual
// only checks signature.go; this test extends the gate cross-package.
func TestReceiverFirewall_HMACOnly(t *testing.T) {
	moduleRoot := findModuleRootHMAC(t)
	targetDir := filepath.Join(moduleRoot, "pkg", "extension", "receiver")

	var violations []string
	fset := token.NewFileSet()
	err := filepath.Walk(targetDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			return perr
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
			if sel.Sel.Name != "Equal" {
				return true
			}
			id, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			if id.Name == "bytes" {
				pos := fset.Position(call.Lparen)
				rel, _ := filepath.Rel(moduleRoot, path)
				violations = append(violations, rel+":"+pos.String())
			}
			return true
		})
		return nil
	})
	require.NoError(t, err, "walk %s", targetDir)

	for _, v := range violations {
		t.Errorf("bytes.Equal call at %s — use crypto/hmac.Equal for HMAC comparison (constant-time; the stdlib byte-slice equality helper is timing-attack-vulnerable). See pkg/extension/receiver/signature.go for the reference implementation.", v)
	}
	if len(violations) == 0 {
		t.Logf("Plan 07.1-08 HMAC firewall: scanned pkg/extension/receiver/*.go; no bytes.Equal call sites (HMAC firewall green)")
	}
}

// findModuleRootHMAC walks up from cwd looking for go.mod.
// Co-located helper for the tests/ package convention; mirrors
// findModuleRootRedaction in firewall_credential_redaction_test.go.
func findModuleRootHMAC(t *testing.T) string {
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
