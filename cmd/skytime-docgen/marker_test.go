package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// parseFuncDecl parses a tiny Go source string and returns the FuncDecl
// matching funcName. Used by all marker tests so each can declare its
// fixture inline as a Go source string (keeps the marker test surface
// minimal and self-contained — no testdata files for marker variants).
func parseFuncDecl(t *testing.T, src, funcName string) *ast.FuncDecl {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, decl := range file.Decls {
		if fd, ok := decl.(*ast.FuncDecl); ok && fd.Name != nil && fd.Name.Name == funcName {
			return fd
		}
	}
	t.Fatalf("func %s not found in source", funcName)
	return nil
}

// captureStderr redirects os.Stderr for the duration of fn() and returns
// what was written. Used by malformed-marker tests to assert that
// ParseMarkers writes a warning. swap-via-os.Stderr is the simplest
// approach that doesn't require refactoring ParseMarkers to take an
// io.Writer (left as a possible future improvement when more tests need
// to assert warning content beyond presence/absence).
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = orig }()
	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	return string(b)
}

func TestParseMarkers_SingleKey(t *testing.T) {
	src := `package x
// skytime:doc summary="hi"
func F() {}
`
	decl := parseFuncDecl(t, src, "F")
	markers := ParseMarkers(decl)
	if got, want := markers["summary"], []string{"hi"}; !reflect.DeepEqual(got, want) {
		t.Errorf("summary = %v; want %v", got, want)
	}
}

func TestParseMarkers_RepeatedKeyMultiline(t *testing.T) {
	src := `package x
// skytime:doc summary="line one"
// skytime:doc summary="line two"
func F() {}
`
	decl := parseFuncDecl(t, src, "F")
	markers := ParseMarkers(decl)
	got := markers["summary"]
	want := []string{"line one", "line two"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("summary = %v; want %v (multi-line via repeated key, order preserved)", got, want)
	}
}

func TestParseMarkers_MultipleKeys(t *testing.T) {
	src := `package x
// skytime:doc summary="declares a flow"
// skytime:doc returns="none"
// skytime:doc since="phase-01"
// skytime:doc example="flow(name=...)"
// skytime:doc see="step, script"
func F() {}
`
	decl := parseFuncDecl(t, src, "F")
	markers := ParseMarkers(decl)
	for _, key := range []string{"summary", "returns", "since", "example", "see"} {
		if _, ok := markers[key]; !ok {
			t.Errorf("missing key %q in markers; got %#v", key, markers)
		}
	}
	if len(markers) != 5 {
		t.Errorf("len(markers) = %d; want 5; %#v", len(markers), markers)
	}
}

func TestParseMarkers_QuotedValueWithEscapes(t *testing.T) {
	src := `package x
// skytime:doc example="flow(name=\"x\", inputs={})"
func F() {}
`
	decl := parseFuncDecl(t, src, "F")
	markers := ParseMarkers(decl)
	got := markers["example"]
	want := []string{`flow(name="x", inputs={})`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("example = %v; want %v (strconv.Unquote handles backslash-escaped quotes)", got, want)
	}
}

func TestParseMarkers_MalformedNoEquals(t *testing.T) {
	src := `package x
// skytime:doc malformedline
// skytime:doc summary="valid"
func F() {}
`
	decl := parseFuncDecl(t, src, "F")
	var markers map[string][]string
	stderr := captureStderr(t, func() {
		markers = ParseMarkers(decl)
	})
	if !strings.Contains(stderr, "missing '='") {
		t.Errorf("stderr does not warn about missing '='; got %q", stderr)
	}
	if got, want := markers["summary"], []string{"valid"}; !reflect.DeepEqual(got, want) {
		t.Errorf("summary = %v; want %v (valid entries should still parse)", got, want)
	}
	if _, bad := markers["malformedline"]; bad {
		t.Errorf("malformedline should not appear in markers; got %#v", markers)
	}
}

func TestParseMarkers_MalformedUnterminatedQuote(t *testing.T) {
	src := `package x
// skytime:doc summary="oops
// skytime:doc since="phase-01"
func F() {}
`
	decl := parseFuncDecl(t, src, "F")
	var markers map[string][]string
	stderr := captureStderr(t, func() {
		markers = ParseMarkers(decl)
	})
	if !strings.Contains(stderr, "cannot unquote") {
		t.Errorf("stderr does not warn about unquote failure; got %q", stderr)
	}
	if got, want := markers["since"], []string{"phase-01"}; !reflect.DeepEqual(got, want) {
		t.Errorf("since = %v; want %v (entries after malformed line should still parse)", got, want)
	}
	if _, bad := markers["summary"]; bad {
		t.Errorf("summary should not appear (unterminated quote); got %#v", markers)
	}
}

func TestParseMarkers_NoMarkerBlock(t *testing.T) {
	src := `package x
// F is a regular godoc-style comment with no skytime:doc markers.
// It should produce an empty Markers map (not nil).
func F() {}
`
	decl := parseFuncDecl(t, src, "F")
	markers := ParseMarkers(decl)
	if len(markers) != 0 {
		t.Errorf("len(markers) = %d; want 0 for godoc-only comment; %#v", len(markers), markers)
	}
	if markers == nil {
		t.Error("ParseMarkers returned nil for godoc-only comment; expected empty map")
	}
}

func TestParseMarkers_NilDoc(t *testing.T) {
	src := `package x

func F() {}
`
	decl := parseFuncDecl(t, src, "F")
	if decl.Doc != nil {
		t.Fatalf("expected decl.Doc nil for comment-less FuncDecl; got %#v", decl.Doc)
	}
	markers := ParseMarkers(decl)
	if len(markers) != 0 {
		t.Errorf("len(markers) = %d; want 0 for nil Doc; %#v", len(markers), markers)
	}
}

func TestParseMarkers_IgnoresGodocBetween(t *testing.T) {
	src := `package x
// skytime:doc summary="x"
//
// builtinFlow is the flow constructor — this is regular godoc and must
// NOT be mistaken for a marker.
func F() {}
`
	decl := parseFuncDecl(t, src, "F")
	markers := ParseMarkers(decl)
	if len(markers) != 1 {
		t.Errorf("len(markers) = %d; want 1 (only summary); %#v", len(markers), markers)
	}
	if got, want := markers["summary"], []string{"x"}; !reflect.DeepEqual(got, want) {
		t.Errorf("summary = %v; want %v", got, want)
	}
}

// TestWalkBuiltins_WithMarkers exercises the integration of ParseMarkers
// with WalkBuiltins. A synthetic builtins source is parsed end-to-end:
// the marker block above builtinFlow flows through WalkBuiltins and lands
// in the returned Builtin's Markers map. This is the integration seam the
// renderer (plan 02) will rely on.
func TestWalkBuiltins_WithMarkers(t *testing.T) {
	src := `package parser

import "go.starlark.net/starlark"

// skytime:doc summary="declares a workflow"
// skytime:doc since="phase-01"
func (p *Parser) builtinFlow(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var name string
	if err := starlark.UnpackArgs("flow", args, kwargs, "name", &name); err != nil {
		return nil, err
	}
	_ = name
	return nil, nil
}

type Parser struct{}
`
	tmp := filepath.Join(t.TempDir(), "builtins.go")
	if err := os.WriteFile(tmp, []byte(src), 0o644); err != nil {
		t.Fatalf("write tempfile: %v", err)
	}

	registry := map[string]string{"flow": "builtinFlow"}
	order := []string{"flow"}
	out, err := WalkBuiltins(tmp, registry, order)
	if err != nil {
		t.Fatalf("WalkBuiltins: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("len(out) = %d; want 1; %#v", len(out), out)
	}
	got := out[0].Markers["summary"]
	want := []string{"declares a workflow"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Markers[summary] = %v; want %v", got, want)
	}
	if got := out[0].Markers["since"]; len(got) != 1 || got[0] != "phase-01" {
		t.Errorf("Markers[since] = %v; want [phase-01]", got)
	}
	// And the Params should still be extracted alongside.
	if len(out[0].Params) != 1 || out[0].Params[0].Name != "name" {
		t.Errorf("Params = %v; want [{Name:name Required:true Target:name}]", out[0].Params)
	}
}
