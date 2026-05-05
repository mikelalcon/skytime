package main

import "go/ast"

// ParseMarkers extracts `// skytime:doc key="value"` markers from a
// FuncDecl's leading comment group. STUB — real body in Task 3.
func ParseMarkers(decl *ast.FuncDecl) map[string][]string {
	_ = decl
	return nil
}
