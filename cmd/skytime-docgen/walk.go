package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

// WalkRegistry parses path (typically pkg/parser/globals.go) and returns
// the Skytime Starlark builtin registry as (name → goFunc) plus the
// source-order slice of builtin names.
//
// Implementation walks newParseTimeGlobals' StringDict CompositeLit and
// pulls each KeyValueExpr where the value is a starlark.NewBuiltin(name,
// p.builtinXxx) call. The order slice preserves source insertion order so
// downstream rendering (plan 02) reads top-to-bottom as the .go file is
// authored — alphabetical sort would scramble the natural grouping
// (flow/step/if_cond before result/fail).
func WalkRegistry(path string) (map[string]string, []string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}

	registry := map[string]string{}
	var order []string

	// Find newParseTimeGlobals; bail out at the first match.
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name == nil || fd.Name.Name != "newParseTimeGlobals" || fd.Body == nil {
			continue
		}
		// Inspect for the StringDict CompositeLit. Stop after the first.
		var done bool
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if done {
				return false
			}
			cl, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			if !isStarlarkStringDict(cl.Type) {
				return true
			}
			for _, elt := range cl.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				name, ok := unquoteString(kv.Key)
				if !ok {
					continue
				}
				funcName, ok := newBuiltinFuncName(kv.Value)
				if !ok {
					continue
				}
				if _, dup := registry[name]; dup {
					continue
				}
				registry[name] = funcName
				order = append(order, name)
			}
			done = true
			return false
		})
		break
	}

	return registry, order, nil
}

// isStarlarkStringDict returns true when expr names starlark.StringDict.
func isStarlarkStringDict(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return pkg.Name == "starlark" && sel.Sel.Name == "StringDict"
}

// unquoteString returns the string literal value of expr if it is a quoted
// BasicLit, or ("", false) otherwise.
func unquoteString(expr ast.Expr) (string, bool) {
	bl, ok := expr.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(bl.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

// newBuiltinFuncName extracts the Go function name from a starlark.NewBuiltin
// call expression of the form `starlark.NewBuiltin("name", p.builtinXxx)`.
// Returns the receiver-less method name (e.g. "builtinFlow") and true on
// match, or ("", false) for any other shape.
func newBuiltinFuncName(expr ast.Expr) (string, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) < 2 {
		return "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil {
		return "", false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "starlark" || sel.Sel.Name != "NewBuiltin" {
		return "", false
	}
	// Args[1] is p.builtinXxx — selector with .Sel as the func name.
	fnSel, ok := call.Args[1].(*ast.SelectorExpr)
	if !ok || fnSel.Sel == nil {
		return "", false
	}
	return fnSel.Sel.Name, true
}

// WalkBuiltins parses path (typically pkg/parser/builtins.go) and returns
// []Builtin in registration order. registry maps starlark name → Go func
// name; order is the source-insertion order from WalkRegistry. The caller
// is expected to pass both verbatim from WalkRegistry — splitting them
// keeps the helper signature explicit (avoids returning an order-tracking
// container type from WalkRegistry).
//
// Each FuncDecl in path whose name appears as a value in registry is
// processed: extractUnpackParams pulls the parameter list from the first
// starlark.UnpackArgs / UnpackPositionalArgs call, ParseMarkers pulls the
// // skytime:doc block above the FuncDecl, and Pos captures the source
// location for diagnostic output. The returned slice is ordered to match
// the registry's insertion order; functions registered but not defined in
// path are silently skipped (a registration-without-implementation bug
// would already fail go build, so this is defense-in-depth).
func WalkBuiltins(path string, registry map[string]string, order []string) ([]Builtin, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	// Reverse map for fast lookup: goFunc → starlark name.
	byFunc := make(map[string]string, len(registry))
	for k, v := range registry {
		byFunc[v] = k
	}

	found := map[string]Builtin{}
	for _, decl := range file.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name == nil {
			continue
		}
		starlarkName, ok := byFunc[fd.Name.Name]
		if !ok {
			continue
		}
		found[starlarkName] = Builtin{
			Name:     starlarkName,
			Function: fd.Name.Name,
			Pos:      fset.Position(fd.Pos()).String(),
			Params:   extractUnpackParams(fd),
			Markers:  ParseMarkers(fd),
		}
	}

	out := make([]Builtin, 0, len(order))
	for _, name := range order {
		if b, ok := found[name]; ok {
			out = append(out, b)
		}
	}
	return out, nil
}

// extractUnpackParams walks fd.Body for the FIRST starlark.UnpackArgs or
// UnpackPositionalArgs call and returns the parameter list. UnpackArgs has
// shape `UnpackArgs(name, args, kwargs, key1, &t1, key2, &t2, ...)` —
// Params populated from index-3 alternating pairs. UnpackPositionalArgs is
// recognized as a positional-only shape; Params returned empty (the renderer
// surfaces fail's parameter via skytime:doc markers — see plan 02).
func extractUnpackParams(fd *ast.FuncDecl) []Param {
	if fd == nil || fd.Body == nil {
		return nil
	}
	var params []Param
	var done bool
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if done {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "starlark" {
			return true
		}
		switch sel.Sel.Name {
		case "UnpackPositionalArgs":
			// Positional-only; no kwarg metadata recoverable. Params stays
			// empty; markers carry the prose. Stop walking.
			done = true
			return false
		case "UnpackArgs":
			// Args layout: [name, args, kwargs, key1, &t1, key2, &t2, ...].
			// Walk pairs from index 3 in steps of 2.
			args := call.Args
			for i := 3; i+1 < len(args); i += 2 {
				key, ok := unquoteString(args[i])
				if !ok {
					continue
				}
				required := !strings.HasSuffix(key, "?")
				name := strings.TrimSuffix(key, "?")
				target := unaryAddrTarget(args[i+1])
				params = append(params, Param{
					Name:     name,
					Required: required,
					Target:   target,
				})
			}
			done = true
			return false
		}
		return true
	})
	return params
}

// unaryAddrTarget extracts the identifier name from an `&ident` UnaryExpr
// passed to UnpackArgs. Returns the bare identifier (e.g. "taskQueue") for
// `&taskQueue`, or "" for anything else (nested fields, struct dereferences,
// etc.). Type recovery on the target is deferred to plan 02.
func unaryAddrTarget(expr ast.Expr) string {
	un, ok := expr.(*ast.UnaryExpr)
	if !ok || un.Op != token.AND {
		return ""
	}
	id, ok := un.X.(*ast.Ident)
	if !ok {
		return ""
	}
	return id.Name
}
