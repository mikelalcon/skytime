package main

// Builtin describes one Starlark builtin extracted from pkg/parser/builtins.go.
//
// Name is the user-facing Starlark identifier (e.g. "flow"). Function is
// the Go method name (e.g. "builtinFlow") so the renderer (plan 02) can
// emit cross-references back to the source. Params is populated from the
// builtin's first starlark.UnpackArgs call (or empty for
// UnpackPositionalArgs callers like `fail`). Markers carries the
// // skytime:doc key="value" block above the FuncDecl, with repeated keys
// preserved as ordered slice values (multi-line summaries supported per
// Phase 04.3 D-11). Pos is the source location ("file:line:col") of the
// FuncDecl, suitable for diagnostic output.
type Builtin struct {
	Name     string              // "flow"
	Function string              // "builtinFlow"
	Params   []Param             // from UnpackArgs (empty for positional-only)
	Markers  map[string][]string // skytime:doc key → values (multi-line via repeat)
	Pos      string              // "pkg/parser/builtins.go:103"
}

// Param describes a single keyword argument recovered from a builtin's
// starlark.UnpackArgs call.
//
// Name is the bare keyword without any trailing "?" (the optional marker is
// preserved in Required). Required is true when the UnpackArgs key did NOT
// end with "?" — the trailing-? convention is a starlark.UnpackArgs feature.
// Target is the Go identifier name passed by reference (e.g. "taskQueue" for
// `&taskQueue`). Type recovery (Go type name on the target var) is deferred
// to plan 02 — Phase 04.3 plan 01 only captures the identifier.
type Param struct {
	Name     string // "name", "inputs", "steps" (NO "?" suffix)
	Required bool   // true if UnpackArgs key did NOT end with "?"
	Target   string // identifier name, e.g. "taskQueue" — type recovery deferred to plan 02
}
