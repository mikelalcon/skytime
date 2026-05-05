package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
)

// WalkRegistry parses path and returns the Starlark builtin registry.
// STUB — real body in Task 2 of this plan.
func WalkRegistry(path string) (map[string]string, []string, error) {
	_ = ast.Inspect
	_ = parser.AllErrors
	_ = token.NewFileSet
	return nil, nil, fmt.Errorf("WalkRegistry: not yet implemented (plan 04.3-01 task 2)")
}

// WalkBuiltins parses path and returns []Builtin in registration order.
// STUB — real body in Task 2 of this plan.
func WalkBuiltins(path string, registry map[string]string, order []string) ([]Builtin, error) {
	_ = path
	_ = registry
	_ = order
	return nil, fmt.Errorf("WalkBuiltins: not yet implemented (plan 04.3-01 task 2)")
}
