package snippets

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestMarkdownSnippetDrift asserts that each named code fence in
// ../temporal-auth.md matches the body of its corresponding .go file
// byte-for-byte (with a single normalization: leading/trailing
// whitespace trimmed on both the fence content and the file body).
//
// Wave 2 plans append a row to `cases` for each cloud snippet they
// ship. The fence in the markdown is identified by the HTML comment
// marker `<!-- snippet: <name>.go -->` on the line immediately
// before the opening ```go fence. The body compared is everything
// between the opening ```go line and the closing ``` line, exclusive.
//
// Why a table rather than per-snippet test functions: a single
// `go test -run TestMarkdownSnippetDrift` runs the whole drift check
// (SC2 acceptance) — easy to wire into CI as one step.
func TestMarkdownSnippetDrift(t *testing.T) {
	cases := []struct {
		fenceMarker string // HTML comment marker above the ```go fence
		goFile      string // path relative to this directory
	}{
		// Wave 2 plans append rows here. Each row pairs a markdown
		// fence marker with a .go file in this directory.
		{"<!-- snippet: gcp.go -->", "gcp.go"},
		{"<!-- snippet: aws.go -->", "aws.go"},
		{"<!-- snippet: azure.go -->", "azure.go"},
	}

	mdBytes, err := os.ReadFile(filepath.Join("..", "temporal-auth.md"))
	if err != nil {
		t.Fatalf("read temporal-auth.md: %v", err)
	}
	md := string(mdBytes)

	for _, tc := range cases {
		t.Run(tc.goFile, func(t *testing.T) {
			// Find the marker, then the next ```go line, then the
			// closing ``` line. Extract the body between them.
			idx := strings.Index(md, tc.fenceMarker)
			if idx == -1 {
				t.Fatalf("marker %q not found in temporal-auth.md", tc.fenceMarker)
			}
			rest := md[idx+len(tc.fenceMarker):]
			openRE := regexp.MustCompile("(?m)^\\x60\\x60\\x60go$")
			openLoc := openRE.FindStringIndex(rest)
			if openLoc == nil {
				t.Fatalf("no ```go fence after marker %q", tc.fenceMarker)
			}
			afterOpen := rest[openLoc[1]:]
			// afterOpen starts with \n; closing ``` on its own line.
			closeRE := regexp.MustCompile("(?m)^\\x60\\x60\\x60$")
			closeLoc := closeRE.FindStringIndex(afterOpen)
			if closeLoc == nil {
				t.Fatalf("no closing ``` fence for marker %q", tc.fenceMarker)
			}
			fenceBody := strings.TrimSpace(afterOpen[:closeLoc[0]])

			fileBytes, err := os.ReadFile(tc.goFile)
			if err != nil {
				t.Fatalf("read %s: %v", tc.goFile, err)
			}
			fileBody := strings.TrimSpace(string(fileBytes))

			if fenceBody != fileBody {
				t.Errorf("drift between markdown fence and %s\n\n=== markdown fence ===\n%s\n\n=== file ===\n%s",
					tc.goFile, fenceBody, fileBody)
			}
		})
	}

	// Sanity: ensure at least one row is registered once Wave 2 lands.
	// During Plan 01 the suite intentionally passes with zero rows;
	// CI flips this gate on once any Wave 2 plan lands its row.
	if len(cases) == 0 {
		t.Log("no snippet cases registered yet — Wave 2 plans will add rows")
	}
}
