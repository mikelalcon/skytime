package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fixtureToTempGo copies a testdata/*.go.txt fixture into a tempdir-scoped
// .go file and returns the path. The .go.txt extension dodges Go's compile
// path (the fixture references go.starlark.net only via opaque package
// selectors which go/parser handles without type-checking) but go/parser
// requires a file path it can read.
func fixtureToTempGo(t *testing.T, fixtureName string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("testdata", fixtureName))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	tmp := filepath.Join(t.TempDir(), strings.TrimSuffix(fixtureName, ".txt"))
	if err := os.WriteFile(tmp, src, 0o644); err != nil {
		t.Fatalf("write tempfile: %v", err)
	}
	return tmp
}

// findModuleRoot walks up from cwd to the first directory containing
// go.mod. Used by live-source smoke tests that walk pkg/parser/*.go to
// verify the walker against the production tree (not just fixtures).
func findModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found walking up from cwd")
		}
		dir = parent
	}
}

func TestWalkRegistry_Fixture(t *testing.T) {
	tmp := fixtureToTempGo(t, "sample_globals.go.txt")

	registry, order, err := WalkRegistry(tmp)
	if err != nil {
		t.Fatalf("WalkRegistry: %v", err)
	}

	wantOrder := []string{"flow", "step", "if_cond", "result", "fail"}
	if !reflect.DeepEqual(order, wantOrder) {
		t.Errorf("order = %v; want %v", order, wantOrder)
	}
	if got := registry["flow"]; got != "builtinFlow" {
		t.Errorf("registry[flow] = %q; want builtinFlow", got)
	}
	if got := registry["fail"]; got != "builtinFail" {
		t.Errorf("registry[fail] = %q; want builtinFail", got)
	}
	if len(registry) != 5 {
		t.Errorf("len(registry) = %d; want 5; full = %#v", len(registry), registry)
	}
}

func TestWalkRegistry_LiveSource(t *testing.T) {
	root := findModuleRoot(t)
	path := filepath.Join(root, "pkg", "parser", "globals.go")
	registry, order, err := WalkRegistry(path)
	if err != nil {
		t.Fatalf("WalkRegistry: %v", err)
	}

	wantNames := []string{"flow", "step", "if_cond", "script", "for_each_parallel", "call_flow", "result", "fail"}
	seen := map[string]bool{}
	for _, n := range order {
		seen[n] = true
	}
	for _, want := range wantNames {
		if !seen[want] {
			t.Errorf("missing builtin %q in registry; got order=%v", want, order)
		}
	}
	if len(registry) < 8 {
		t.Errorf("len(registry) = %d; want >= 8", len(registry))
	}
}

func TestWalkRegistry_NonExistent(t *testing.T) {
	_, _, err := WalkRegistry("does/not/exist.go")
	if err == nil {
		t.Fatal("expected error for missing path; got nil")
	}
	if !strings.Contains(err.Error(), "does/not/exist.go") {
		t.Errorf("error %q does not mention the path", err)
	}
}

func TestWalkBuiltins_FixtureUnpackArgs(t *testing.T) {
	globals := fixtureToTempGo(t, "sample_globals.go.txt")
	builtins := fixtureToTempGo(t, "sample_builtins.go.txt")

	registry, order, err := WalkRegistry(globals)
	if err != nil {
		t.Fatalf("WalkRegistry: %v", err)
	}
	out, err := WalkBuiltins(builtins, registry, order)
	if err != nil {
		t.Fatalf("WalkBuiltins: %v", err)
	}
	if len(out) != 5 {
		t.Fatalf("len(out) = %d; want 5; full = %#v", len(out), out)
	}

	byName := map[string]Builtin{}
	for _, b := range out {
		byName[b.Name] = b
	}

	flow, ok := byName["flow"]
	if !ok {
		t.Fatalf("missing flow in out; got %#v", out)
	}
	wantNames := []string{"name", "inputs", "steps", "task_queue", "description"}
	wantRequired := []bool{true, false, true, false, false}
	if len(flow.Params) != len(wantNames) {
		t.Fatalf("flow.Params len = %d; want %d; %#v", len(flow.Params), len(wantNames), flow.Params)
	}
	for i, p := range flow.Params {
		if p.Name != wantNames[i] {
			t.Errorf("flow.Params[%d].Name = %q; want %q", i, p.Name, wantNames[i])
		}
		if p.Required != wantRequired[i] {
			t.Errorf("flow.Params[%d].Required = %v; want %v (name=%q)", i, p.Required, wantRequired[i], p.Name)
		}
	}
	if flow.Params[0].Target != "name" {
		t.Errorf("flow.Params[0].Target = %q; want %q", flow.Params[0].Target, "name")
	}
	if len(flow.Markers) != 0 {
		t.Errorf("flow.Markers len = %d; want 0 (fixture has no markers); %#v", len(flow.Markers), flow.Markers)
	}
}

func TestWalkBuiltins_FixtureUnpackPositionalArgs(t *testing.T) {
	globals := fixtureToTempGo(t, "sample_globals.go.txt")
	builtins := fixtureToTempGo(t, "sample_builtins.go.txt")

	registry, order, err := WalkRegistry(globals)
	if err != nil {
		t.Fatalf("WalkRegistry: %v", err)
	}
	out, err := WalkBuiltins(builtins, registry, order)
	if err != nil {
		t.Fatalf("WalkBuiltins: %v", err)
	}
	var fail Builtin
	var found bool
	for _, b := range out {
		if b.Name == "fail" {
			fail = b
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing fail in out; got %#v", out)
	}
	if len(fail.Params) != 0 {
		t.Errorf("fail.Params len = %d; want 0 (positional-only); %#v", len(fail.Params), fail.Params)
	}
	if fail.Function != "builtinFail" {
		t.Errorf("fail.Function = %q; want builtinFail", fail.Function)
	}
}

func TestWalkBuiltins_OrderMatchesRegistry(t *testing.T) {
	globals := fixtureToTempGo(t, "sample_globals.go.txt")
	builtins := fixtureToTempGo(t, "sample_builtins.go.txt")

	registry, order, err := WalkRegistry(globals)
	if err != nil {
		t.Fatalf("WalkRegistry: %v", err)
	}
	out, err := WalkBuiltins(builtins, registry, order)
	if err != nil {
		t.Fatalf("WalkBuiltins: %v", err)
	}
	gotOrder := make([]string, 0, len(out))
	for _, b := range out {
		gotOrder = append(gotOrder, b.Name)
	}
	if !reflect.DeepEqual(gotOrder, order) {
		t.Errorf("WalkBuiltins order = %v; want %v (registry insertion order)", gotOrder, order)
	}
}

func TestWalkBuiltins_PreservesPos(t *testing.T) {
	globals := fixtureToTempGo(t, "sample_globals.go.txt")
	builtins := fixtureToTempGo(t, "sample_builtins.go.txt")

	registry, order, err := WalkRegistry(globals)
	if err != nil {
		t.Fatalf("WalkRegistry: %v", err)
	}
	out, err := WalkBuiltins(builtins, registry, order)
	if err != nil {
		t.Fatalf("WalkBuiltins: %v", err)
	}
	for _, b := range out {
		// Pos shape is "<filepath>:<line>:<col>" via token.Position.String().
		if !strings.Contains(b.Pos, "sample_builtins.go") {
			t.Errorf("Builtin %q Pos = %q; expected to contain sample_builtins.go", b.Name, b.Pos)
		}
		// Must contain at least two ':' separators (file:line:col).
		if strings.Count(b.Pos, ":") < 2 {
			t.Errorf("Builtin %q Pos = %q; expected file:line:col", b.Name, b.Pos)
		}
	}
}

func TestWalkBuiltins_LiveSource(t *testing.T) {
	root := findModuleRoot(t)
	globalsPath := filepath.Join(root, "pkg", "parser", "globals.go")
	builtinsPath := filepath.Join(root, "pkg", "parser", "builtins.go")

	registry, order, err := WalkRegistry(globalsPath)
	if err != nil {
		t.Fatalf("WalkRegistry: %v", err)
	}
	out, err := WalkBuiltins(builtinsPath, registry, order)
	if err != nil {
		t.Fatalf("WalkBuiltins: %v", err)
	}
	if len(out) < 8 {
		t.Fatalf("len(out) = %d; want >= 8 builtins on live source; got %#v", len(out), namesOf(out))
	}

	byName := map[string]Builtin{}
	for _, b := range out {
		byName[b.Name] = b
	}

	flow, ok := byName["flow"]
	if !ok {
		t.Fatalf("missing flow on live source")
	}
	if !paramRequired(flow.Params, "name", true) {
		t.Errorf("flow live: name expected required=true; got %#v", flow.Params)
	}

	step, ok := byName["step"]
	if !ok {
		t.Fatalf("missing step on live source")
	}
	if !paramRequired(step.Params, "action", false) {
		t.Errorf("step live: action expected required=false; got %#v", step.Params)
	}

	callFlow, ok := byName["call_flow"]
	if !ok {
		t.Fatalf("missing call_flow on live source")
	}
	if !paramRequired(callFlow.Params, "name", true) {
		t.Errorf("call_flow live: name expected required=true; got %#v", callFlow.Params)
	}
	if !paramRequired(callFlow.Params, "inputs", false) {
		t.Errorf("call_flow live: inputs expected required=false; got %#v", callFlow.Params)
	}
}

// TestWalkRegistry_NestedModule confirms that &starlarkstruct.Module{
// Name: "log", Members: starlark.StringDict{...} } registrations surface
// as fully-qualified `<ns>.<member>` registry entries — required so the
// log.info / log.warn / log.error / log.debug surfaces land in the
// rendered docs/reference/builtins.md (Phase 07.2.1).
func TestWalkRegistry_NestedModule(t *testing.T) {
	tmp := fixtureToTempGo(t, "sample_log_globals.go.txt")

	registry, order, err := WalkRegistry(tmp)
	if err != nil {
		t.Fatalf("WalkRegistry: %v", err)
	}

	wantOrder := []string{"flow", "log.info", "log.warn", "log.error", "log.debug"}
	if !reflect.DeepEqual(order, wantOrder) {
		t.Errorf("order = %v; want %v", order, wantOrder)
	}
	wantMap := map[string]string{
		"flow":      "builtinFlow",
		"log.info":  "builtinLogInfo",
		"log.warn":  "builtinLogWarn",
		"log.error": "builtinLogError",
		"log.debug": "builtinLogDebug",
	}
	if !reflect.DeepEqual(registry, wantMap) {
		t.Errorf("registry = %#v; want %#v", registry, wantMap)
	}
}

// TestWalkBuiltinsMulti_TrampolineRecovery confirms a thin
// `return p.<helper>(...)` trampoline builtin (the log.<level> shape in
// Phase 07.2.1) has its UnpackArgs metadata recovered from the helper's
// body — not from the trampoline's own (empty) body. Without this the
// rendered signature would fall back to alphabetical positional-only
// (`log.info(attrs, msg)`) which is both misleading and rejects the
// optional `attrs` kwarg's true shape.
func TestWalkBuiltinsMulti_TrampolineRecovery(t *testing.T) {
	globals := fixtureToTempGo(t, "sample_log_globals.go.txt")
	builtins := fixtureToTempGo(t, "sample_log_builtins.go.txt")

	registry, order, err := WalkRegistry(globals)
	if err != nil {
		t.Fatalf("WalkRegistry: %v", err)
	}
	out, err := WalkBuiltinsMulti([]string{builtins}, registry, order)
	if err != nil {
		t.Fatalf("WalkBuiltinsMulti: %v", err)
	}
	byName := map[string]Builtin{}
	for _, b := range out {
		byName[b.Name] = b
	}
	info, ok := byName["log.info"]
	if !ok {
		t.Fatalf("log.info missing from output; got %v", namesOf(out))
	}
	if info.Function != "builtinLogInfo" {
		t.Errorf("log.info.Function = %q; want builtinLogInfo", info.Function)
	}
	if len(info.Params) != 2 {
		t.Fatalf("log.info.Params len = %d; want 2; got %#v", len(info.Params), info.Params)
	}
	if info.Params[0].Name != "msg" || !info.Params[0].Required {
		t.Errorf("log.info.Params[0] = %#v; want {msg required=true}", info.Params[0])
	}
	if info.Params[1].Name != "attrs" || info.Params[1].Required {
		t.Errorf("log.info.Params[1] = %#v; want {attrs required=false}", info.Params[1])
	}

	// Sanity: warn/error/debug should land with identical signatures
	// (they all trampoline into buildLogStep).
	for _, level := range []string{"log.warn", "log.error", "log.debug"} {
		b, ok := byName[level]
		if !ok {
			t.Errorf("%s missing from output", level)
			continue
		}
		if len(b.Params) != 2 || b.Params[0].Name != "msg" || b.Params[1].Name != "attrs" {
			t.Errorf("%s.Params = %#v; want msg + attrs?", level, b.Params)
		}
	}
}

// TestFindBuiltinFiles_PicksUpLogSplit confirms findBuiltinFiles returns
// both builtins.go and builtins_log.go (and excludes _test.go siblings)
// on the live pkg/parser tree. This guards the docgen pipeline against a
// regression where a future split file gets dropped from the rendered
// reference.
func TestFindBuiltinFiles_PicksUpLogSplit(t *testing.T) {
	root := findModuleRoot(t)
	pkgDir := filepath.Join(root, "pkg", "parser")
	files, err := findBuiltinFiles(pkgDir)
	if err != nil {
		t.Fatalf("findBuiltinFiles: %v", err)
	}
	got := map[string]bool{}
	for _, f := range files {
		got[filepath.Base(f)] = true
	}
	if !got["builtins.go"] {
		t.Errorf("expected builtins.go in %v", files)
	}
	if !got["builtins_log.go"] {
		t.Errorf("expected builtins_log.go in %v", files)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			t.Errorf("test file leaked into walk set: %s", f)
		}
	}
}

// paramRequired is a small helper for live-source assertions: returns
// true when params contains a Param with the given name AND the expected
// Required value.
func paramRequired(params []Param, name string, required bool) bool {
	for _, p := range params {
		if p.Name == name {
			return p.Required == required
		}
	}
	return false
}

// namesOf is a debug helper for diagnostic output on live-source failures.
func namesOf(builtins []Builtin) []string {
	names := make([]string, len(builtins))
	for i, b := range builtins {
		names[i] = b.Name
	}
	return names
}
