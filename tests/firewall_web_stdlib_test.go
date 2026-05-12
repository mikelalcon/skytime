// Plan 07.3-04 (UI-01..04 firewall): the dashboard subtree
// (pkg/cli/server/web/) is locked to stdlib + go.temporal.io + own
// module imports. No external HTTP/template/SSE/router libraries.
//
// M2 (Phase 7.3 checker): the test additionally tracks at least one
// go.temporal.io import as a non-vacuity canary — if a future commit
// strips every Temporal import from the subtree, the allow-list-vs-
// stdlib gate would still pass on a clean tree (no third-party
// import to flag), making the firewall silently vacuous. The
// sawTemporalImport canary catches that regression.
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

// allowedWebImportPrefixes is the closed allow-list for imports
// reachable from pkg/cli/server/web/ — stdlib (no dot in the first
// path component) + Temporal SDK paths + protobuf transitive +
// own-module paths.
//
// CRITICAL constraint per ROADMAP success criterion #4:
// "no JS framework, no external CSS, no bundler" — extended to Go
// imports: no third-party HTTP/template/SSE/router libraries
// reachable from this subtree.
var allowedWebImportPrefixes = []string{
	// Stdlib has NO domain prefix (e.g., "net/http", "encoding/json").
	// Detected via "no dot in the first path component" rule below.

	// Temporal SDK families (already in go.mod via Phase 1/3/7.1).
	"go.temporal.io/sdk",
	"go.temporal.io/api",
	"google.golang.org/protobuf",
	"google.golang.org/grpc",

	// Own module — sibling subpackages.
	"github.com/mikelalcon/skytime",
}

// bannedWebImportExemplars is a documentation breadcrumb — the
// allow-list above is the actual gate. These are imports we would
// reject if anyone ever tried to reach for them.
var bannedWebImportExemplars = []string{
	"github.com/gorilla/mux",
	"github.com/gorilla/websocket",
	"github.com/r3labs/sse",
	"github.com/tmaxmax/go-sse",
	"github.com/google/safehtml",
	"github.com/google/uuid",
	"github.com/spf13/cobra", // already firewalled by TestNoCobraImportsOutsideAllowList; defense in depth
	"charm.land/log/v2",
}

func TestNoExternalHTTPTemplateInWeb(t *testing.T) {
	_ = bannedWebImportExemplars // referenced for documentation; quiets unused-var lints
	moduleRoot := findModuleRootCLI(t)
	webRoot := filepath.Join(moduleRoot, "pkg", "cli", "server", "web")

	fset := token.NewFileSet()
	filesScanned := 0
	sawTemporalImport := false // M2: positive non-vacuity defense
	var violations []string

	walkErr := filepath.Walk(webRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		// Production .go files only — test files are allowed to
		// import testing helpers (httptest etc., still stdlib).
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(moduleRoot, path)
		f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		filesScanned++
		for _, imp := range f.Imports {
			ip := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(ip, "go.temporal.io/") {
				sawTemporalImport = true
			}
			if isStdlibImport(ip) {
				continue
			}
			allowed := false
			for _, prefix := range allowedWebImportPrefixes {
				if ip == prefix || strings.HasPrefix(ip, prefix+"/") {
					allowed = true
					break
				}
			}
			if !allowed {
				violations = append(violations, rel+": "+ip)
			}
		}
		return nil
	})
	require.NoError(t, walkErr)
	require.Empty(t, violations,
		"stdlib firewall (D-7.3): pkg/cli/server/web/ must only import stdlib + Temporal SDK + own module; found:\n%s",
		strings.Join(violations, "\n"))
	require.Greater(t, filesScanned, 0, "firewall is vacuous: zero .go files scanned under %s", webRoot)
	// M2: positive non-vacuity — at least one go.temporal.io import
	// must have been seen during the scan. If a future commit
	// accidentally removes all Temporal imports, this assertion
	// catches the now-vacuous firewall before the next regression
	// can sneak in.
	require.True(t, sawTemporalImport,
		"firewall non-vacuity: expected at least one go.temporal.io/* import under %s — without one, the allow-list-vs-stdlib gate is vacuous (any non-stdlib third-party import would be the FIRST match and we'd lose our canary)",
		webRoot)
}

// isStdlibImport returns true when the import path has no domain
// dot in its first path segment (Go's stdlib convention — "net/http"
// not "example.com/net/http").
func isStdlibImport(ip string) bool {
	slash := strings.Index(ip, "/")
	first := ip
	if slash >= 0 {
		first = ip[:slash]
	}
	return !strings.Contains(first, ".")
}
