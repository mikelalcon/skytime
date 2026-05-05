package main

import (
	"fmt"
	"go/ast"
	"os"
	"strconv"
	"strings"
)

// markerPrefix is the canonical Go-comment prefix that identifies a
// skytime:doc marker line. Every line of decl.Doc.List that starts with
// this prefix is parsed as `key="quoted-value"`. Lines without the prefix
// (godoc, blank lines, etc.) are ignored. The trailing space is part of
// the prefix to keep `// skytime:docfoo=...` from accidentally matching.
const markerPrefix = "// skytime:doc "

// ParseMarkers extracts `// skytime:doc key="value"` markers from decl's
// leading CommentGroup. Multi-line values are supported by repeating the
// same key across lines — the renderer (plan 02) joins with newlines.
// Malformed lines (missing `=`, unquoted value, unterminated quote, empty
// key) write a warning to stderr and are skipped — never abort. This keeps
// the walker robust against typos in source-side markers without silently
// dropping valid entries on the same builtin.
//
// Returns an empty (non-nil) map when decl has no leading CommentGroup or
// when no marker lines are present. Empty map signals "checked, found
// nothing"; nil-vs-empty distinction is irrelevant to consumers (range
// over either is a no-op) but the empty-map shape keeps reflection-based
// JSON encoding consistent (plan 02's text/template renderer can rely on
// `.Markers` always being a usable map).
func ParseMarkers(decl *ast.FuncDecl) map[string][]string {
	out := map[string][]string{}
	if decl == nil || decl.Doc == nil {
		return out
	}
	for _, c := range decl.Doc.List {
		line := c.Text
		if !strings.HasPrefix(line, markerPrefix) {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, markerPrefix))
		if payload == "" {
			fmt.Fprintf(os.Stderr, "skytime-docgen: warning: empty skytime:doc marker at %s\n", positionString(decl, c))
			continue
		}
		// Split on first '=' — keys are bare identifiers, values are
		// double-quoted Go-style strings (strconv.Unquote handles escapes).
		eq := strings.Index(payload, "=")
		if eq < 0 {
			fmt.Fprintf(os.Stderr, "skytime-docgen: warning: missing '=' in marker %q at %s\n", payload, positionString(decl, c))
			continue
		}
		key := strings.TrimSpace(payload[:eq])
		rawValue := strings.TrimSpace(payload[eq+1:])
		if key == "" {
			fmt.Fprintf(os.Stderr, "skytime-docgen: warning: empty key in marker %q at %s\n", payload, positionString(decl, c))
			continue
		}
		if !strings.HasPrefix(rawValue, "\"") {
			fmt.Fprintf(os.Stderr, "skytime-docgen: warning: value must be double-quoted: %q at %s\n", payload, positionString(decl, c))
			continue
		}
		value, err := strconv.Unquote(rawValue)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skytime-docgen: warning: cannot unquote %q at %s: %v\n", rawValue, positionString(decl, c), err)
			continue
		}
		out[key] = append(out[key], value)
	}
	return out
}

// positionString returns a best-effort source position for diagnostic
// output. The caller passes the FuncDecl whose Doc the comment belongs
// to plus the comment itself; we use the Comment.Slash offset relative
// to the declaration name. A token.FileSet-backed position would be more
// human-friendly, but the FuncDecl traversal in WalkBuiltins doesn't
// thread the FileSet down to ParseMarkers; offset+name is enough to
// locate the line during a malformed-marker investigation.
func positionString(decl *ast.FuncDecl, c *ast.Comment) string {
	if decl == nil || decl.Name == nil {
		return fmt.Sprintf("offset %d", c.Slash)
	}
	return fmt.Sprintf("%s (offset %d)", decl.Name.Name, c.Slash)
}
