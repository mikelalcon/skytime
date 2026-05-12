package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// osReadDir is a thin indirection over os.ReadDir to keep the
// findBuiltinFiles signature stable if a test ever needs to swap in a
// fake filesystem. Today it just delegates.
func osReadDir(dir string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	// Sort lexicographically for stable docgen output. os.ReadDir already
	// returns sorted entries on most platforms but the contract is "in
	// directory order" — sort defensively to keep the regenerated
	// builtins.md byte-stable across machines.
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

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
//
// Phase 07.2.1: also recurses into nested *starlarkstruct.Module composite
// literals (e.g. the `log` namespace) and surfaces their Members entries
// as fully-qualified registry keys (`log.info`, `log.warn`, etc.). The
// values inside Members must also be starlark.NewBuiltin(...) calls; the
// extracted Go function name (e.g. `builtinLogInfo`) is used unchanged so
// WalkBuiltins can locate the FuncDecl across pkg/parser/*.go files.
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
				// Direct starlark.NewBuiltin(...) value — the common case.
				if funcName, ok := newBuiltinFuncName(kv.Value); ok {
					if _, dup := registry[name]; dup {
						continue
					}
					registry[name] = funcName
					order = append(order, name)
					continue
				}
				// Nested namespace: &starlarkstruct.Module{Name: "x", Members: starlark.StringDict{...}}.
				// Recover the Members StringDict and emit fully-qualified
				// `<name>.<member>` entries in member-source order.
				if members, ok := starlarkstructModuleMembers(kv.Value); ok {
					for _, mkv := range members {
						mName, ok := unquoteString(mkv.Key)
						if !ok {
							continue
						}
						mFunc, ok := newBuiltinFuncName(mkv.Value)
						if !ok {
							continue
						}
						qualified := name + "." + mName
						if _, dup := registry[qualified]; dup {
							continue
						}
						registry[qualified] = mFunc
						order = append(order, qualified)
					}
				}
			}
			done = true
			return false
		})
		break
	}

	return registry, order, nil
}

// starlarkstructModuleMembers returns the elements of the Members
// StringDict embedded in a `&starlarkstruct.Module{...}` composite-literal
// expression. The expected shape is:
//
//	&starlarkstruct.Module{
//	    Name: "log",
//	    Members: starlark.StringDict{
//	        "info": starlark.NewBuiltin("log.info", p.builtinLogInfo),
//	        ...
//	    },
//	}
//
// Returns the Members CompositeLit's Elts (as *ast.KeyValueExpr slice)
// and true on match, or (nil, false) for any other shape (regular value,
// non-Module struct, missing Members field, etc.). Recognizes only the
// pointer-form `&starlarkstruct.Module{...}` — the package's static-
// registration convention. A non-pointer Module literal would still be
// valid Starlark wiring but would not be picked up here; introduce that
// path when the codebase actually uses it.
func starlarkstructModuleMembers(expr ast.Expr) ([]*ast.KeyValueExpr, bool) {
	un, ok := expr.(*ast.UnaryExpr)
	if !ok || un.Op != token.AND {
		return nil, false
	}
	cl, ok := un.X.(*ast.CompositeLit)
	if !ok {
		return nil, false
	}
	sel, ok := cl.Type.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil {
		return nil, false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "starlarkstruct" || sel.Sel.Name != "Module" {
		return nil, false
	}
	for _, elt := range cl.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		fieldName, ok := kv.Key.(*ast.Ident)
		if !ok || fieldName.Name != "Members" {
			continue
		}
		members, ok := kv.Value.(*ast.CompositeLit)
		if !ok || !isStarlarkStringDict(members.Type) {
			continue
		}
		out := make([]*ast.KeyValueExpr, 0, len(members.Elts))
		for _, e := range members.Elts {
			mkv, ok := e.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			out = append(out, mkv)
		}
		return out, true
	}
	return nil, false
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

// WalkBuiltinsMulti merges WalkBuiltins results across multiple source
// files in one pkg directory. Used when factories are split across
// `builtins.go` + `builtins_log.go` (Phase 07.2.1) and similar future
// splits. Ordering follows `order` exactly; a Starlark name found in
// multiple files keeps the first hit (registry has only one Go-func
// binding per name, so collisions are spec violations and surface via
// the per-file walker rather than here).
//
// The two-pass shape (collect FuncDecls across all files, then resolve
// each builtin with full cross-file visibility) lets trampoline factories
// like `builtinLogInfo` → `buildLogStep` recover the real UnpackArgs
// metadata regardless of which file the helper lives in. A single-pass
// walk would miss helpers defined in a sibling file.
func WalkBuiltinsMulti(paths []string, registry map[string]string, order []string) ([]Builtin, error) {
	// Pass 1: parse every file and collect (path, *ast.File, fset) plus
	// a method-name → FuncDecl registry for trampoline recovery.
	type parsedFile struct {
		path string
		file *ast.File
		fset *token.FileSet
	}
	var parsed []parsedFile
	decls := map[string]*ast.FuncDecl{}
	for _, p := range paths {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, nil, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", p, err)
		}
		parsed = append(parsed, parsedFile{p, f, fset})
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Name == nil {
				continue
			}
			if _, dup := decls[fd.Name.Name]; dup {
				continue
			}
			decls[fd.Name.Name] = fd
		}
	}

	// Pass 2: build Builtin records for each registered Go function,
	// using the cross-file decls registry so trampolines resolve their
	// helper's UnpackArgs.
	byFunc := make(map[string]string, len(registry))
	for k, v := range registry {
		byFunc[v] = k
	}
	merged := map[string]Builtin{}
	for _, pf := range parsed {
		for _, decl := range pf.file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Name == nil {
				continue
			}
			starlarkName, ok := byFunc[fd.Name.Name]
			if !ok {
				continue
			}
			if _, dup := merged[starlarkName]; dup {
				continue
			}
			merged[starlarkName] = Builtin{
				Name:     starlarkName,
				Function: fd.Name.Name,
				Pos:      pf.fset.Position(fd.Pos()).String(),
				Params:   extractUnpackParamsWithRegistry(fd, decls),
				Markers:  ParseMarkers(fd),
			}
		}
	}

	out := make([]Builtin, 0, len(order))
	for _, name := range order {
		if b, ok := merged[name]; ok {
			out = append(out, b)
		}
	}
	return out, nil
}

// findBuiltinFiles returns every non-test `builtins*.go` file under
// pkgDir, sorted lexicographically for stable docgen output. Returns an
// empty slice (no error) when none exist; the caller decides whether
// that's fatal. _test.go files are excluded — fixtures + test helpers
// should never contribute to the rendered reference.
func findBuiltinFiles(pkgDir string) ([]string, error) {
	entries, err := osReadDir(pkgDir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "builtins") {
			continue
		}
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		out = append(out, filepath.Join(pkgDir, name))
	}
	return out, nil
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
//
// Trampoline support (Phase 07.2.1): when the FuncDecl body is a single
// `return p.<method>(...)` call (e.g., the four log.<level> wrappers that
// delegate to `buildLogStep`), `allBuiltinDecls` is searched for the
// callee's FuncDecl and the recursive walk continues there. This keeps
// // skytime:doc markers on the trampoline (per-level prose) while the
// signature is recovered from the shared factory's UnpackArgs call.
// `allBuiltinDecls` is keyed by Go method name (no receiver); when nil,
// trampoline recovery is skipped and the legacy single-file behavior
// kicks in (positional-only fallback uses param_* markers).
func extractUnpackParams(fd *ast.FuncDecl) []Param {
	return extractUnpackParamsWithRegistry(fd, nil)
}

// extractUnpackParamsWithRegistry is the trampoline-aware variant of
// extractUnpackParams. registry maps Go method name → *ast.FuncDecl across
// every builtins*.go file in the package; when the visited body is a thin
// `return p.<other>(...)` trampoline (typical of namespaced surfaces such
// as log.<level>), the walker recurses into the callee to recover the
// real UnpackArgs metadata.
func extractUnpackParamsWithRegistry(fd *ast.FuncDecl, registry map[string]*ast.FuncDecl) []Param {
	if fd == nil || fd.Body == nil {
		return nil
	}
	visited := map[string]bool{}
	return extractUnpackParamsRec(fd, registry, visited)
}

func extractUnpackParamsRec(fd *ast.FuncDecl, registry map[string]*ast.FuncDecl, visited map[string]bool) []Param {
	if fd == nil || fd.Body == nil {
		return nil
	}
	if fd.Name != nil {
		if visited[fd.Name.Name] {
			return nil
		}
		visited[fd.Name.Name] = true
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
	if len(params) > 0 || done || registry == nil {
		return params
	}
	// No direct UnpackArgs/UnpackPositionalArgs call — try trampoline
	// recovery. The trampoline shape we recognize:
	//   func (p *Parser) builtinX(...) (..., error) {
	//       return p.<helper>(<args>)
	//   }
	// Find the first `return p.<helper>(...)` and recurse into <helper>.
	for _, stmt := range fd.Body.List {
		retStmt, ok := stmt.(*ast.ReturnStmt)
		if !ok || len(retStmt.Results) != 1 {
			continue
		}
		callExpr, ok := retStmt.Results[0].(*ast.CallExpr)
		if !ok {
			continue
		}
		methodName, ok := receiverMethodName(callExpr.Fun)
		if !ok {
			continue
		}
		helperDecl, ok := registry[methodName]
		if !ok {
			continue
		}
		return extractUnpackParamsRec(helperDecl, registry, visited)
	}
	return nil
}

// receiverMethodName matches `p.<name>` / `<recv>.<name>` selectors used
// in trampoline `return p.<helper>(...)` shapes. Returns (name, true) when
// the call target is a method invocation on an identifier receiver, or
// ("", false) otherwise. Package-qualified calls (e.g.
// `starlark.NewBuiltin(...)`) are rejected by the X-is-ident check
// (any Ident as receiver passes, including `starlark`; the caller's
// registry lookup is the actual gate — only Go methods on *Parser are
// keyed in registry, so the search is naturally scoped).
func receiverMethodName(expr ast.Expr) (string, bool) {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil {
		return "", false
	}
	if _, ok := sel.X.(*ast.Ident); !ok {
		return "", false
	}
	return sel.Sel.Name, true
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
