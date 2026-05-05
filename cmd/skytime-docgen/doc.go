// Package main implements skytime-docgen, a Copybara-style source-driven
// reference generator for the Skytime Starlark DSL builtins.
//
// It walks pkg/parser/globals.go (the StringDict assembled by
// newParseTimeGlobals) to enumerate registered builtins, then walks
// pkg/parser/builtins.go to extract each builtin's parameter list from
// its starlark.UnpackArgs(...) call and any `// skytime:doc key="value"`
// marker block above the function declaration.
//
// Stdlib-only: go/parser, go/ast, go/token, text/template, flag, strconv,
// strings, fmt, os. No cobra, no charm-log, no lipgloss.
//
// Trigger: go generate ./pkg/parser/ (added in Phase 04.3 plan 02).
// Output: docs/reference/builtins.md (plan 02 ships the renderer).
package main
