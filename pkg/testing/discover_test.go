package testing

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"go.starlark.net/starlark"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/parser"
)

// TestDiscoverTestFiles_RecursiveWalk — D5-A2: recursive walk for
// *_test.star anywhere under root; non-_test.star files are skipped.
func TestDiscoverTestFiles_RecursiveWalk(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "a", "b", "c"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "a", "c"), 0o755))
	write := func(rel string) {
		require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte(""), 0o644))
	}
	write("x_test.star")
	write("a/y_test.star")
	write("a/b/z_test.star")
	write("a/b/c/q_test.star")
	write("a/c/not_a_test_file.star") // does NOT end in _test.star → skipped
	write("a/flow.star")              // not a test file → skipped

	files, err := DiscoverTestFiles(root)
	require.NoError(t, err)

	// Sort happens inside DiscoverTestFiles; order is lexicographic.
	want := []string{
		filepath.Join(root, "a", "b", "c", "q_test.star"),
		filepath.Join(root, "a", "b", "z_test.star"),
		filepath.Join(root, "a", "y_test.star"),
		filepath.Join(root, "x_test.star"),
	}
	assert.Equal(t, want, files)
}

// TestDiscoverTestFiles_SingleFile — passing a *_test.star path
// (NOT a dir) returns a one-element slice.
func TestDiscoverTestFiles_SingleFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "users_test.star")
	require.NoError(t, os.WriteFile(p, []byte(""), 0o644))
	files, err := DiscoverTestFiles(p)
	require.NoError(t, err)
	assert.Equal(t, []string{p}, files)
}

// TestDiscoverTestFiles_NonTestFile_Errors — passing a non-_test.star
// file path errors with a clear message.
func TestDiscoverTestFiles_NonTestFile_Errors(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "flow.star")
	require.NoError(t, os.WriteFile(p, []byte(""), 0o644))
	_, err := DiscoverTestFiles(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a *_test.star file")
}

// TestDiscoverTestFiles_NonexistentPath_Errors — stat error surfaces
// (no surprise empty-slice return).
func TestDiscoverTestFiles_NonexistentPath_Errors(t *testing.T) {
	dir := t.TempDir()
	_, err := DiscoverTestFiles(filepath.Join(dir, "does-not-exist"))
	require.Error(t, err)
}

// TestDiscoverTests_FiltersTopLevelTestPrefixZeroArg — D5-A1 +
// RESEARCH Pattern 4: only top-level def test_*() with NumParams==0
// qualify. Lambdas, helpers with args, and non-function globals are
// silently excluded.
func TestDiscoverTests_FiltersTopLevelTestPrefixZeroArg(t *testing.T) {
	src := `
def test_alpha():
    pass

def test_with_arg(x):
    pass

def helper():
    pass

x = 42
`
	g := mustParseTestGlobals(t, "t.star", src)
	fns := DiscoverTests(g)
	require.Len(t, fns, 1)
	assert.Equal(t, "test_alpha", fns[0].Name)
	require.NotNil(t, fns[0].Fn)
}

// TestDiscoverTests_SortedAlphabetical — multiple def test_*() names
// surface in lexicographic order regardless of source order.
func TestDiscoverTests_SortedAlphabetical(t *testing.T) {
	src := `
def test_zulu():
    pass

def test_alpha():
    pass

def test_mike():
    pass
`
	g := mustParseTestGlobals(t, "t.star", src)
	fns := DiscoverTests(g)
	require.Len(t, fns, 3)
	names := []string{fns[0].Name, fns[1].Name, fns[2].Name}
	assert.Equal(t, []string{"test_alpha", "test_mike", "test_zulu"}, names)
}

// TestCompileRunFilter_EmptyMeansMatchAll — empty pattern returns
// (nil, nil); MatchRunFilter(nil, ...) is true.
func TestCompileRunFilter_EmptyMeansMatchAll(t *testing.T) {
	re, err := CompileRunFilter("")
	require.NoError(t, err)
	assert.Nil(t, re)
	assert.True(t, MatchRunFilter(re, "anything.test_x"))
}

// TestCompileRunFilter_BadPattern_Errors — unparseable regex returns
// an error wrapping ErrBadFilter (so callers can detect via
// errors.Is).
func TestCompileRunFilter_BadPattern_Errors(t *testing.T) {
	_, err := CompileRunFilter("[invalid")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBadFilter), "expected errors.Is(err, ErrBadFilter); got %v", err)
}

// TestMatchRunFilter_EmptyMatchesAll — defensive: nil regex matches
// any name (the common no-filter path).
func TestMatchRunFilter_EmptyMatchesAll(t *testing.T) {
	assert.True(t, MatchRunFilter(nil, "anything.test_x"))
}

// TestMatchRunFilter_RegexMatch — D5-E3: regex against
// `<file_basename>.<test_name>`. Anchored regex ^users_test\.test_existing
// matches users_test.test_existing_user but not users_test.test_other or
// orders_test.test_existing_user.
func TestMatchRunFilter_RegexMatch(t *testing.T) {
	re, err := CompileRunFilter(`^users_test\.test_existing`)
	require.NoError(t, err)
	assert.True(t, MatchRunFilter(re, "users_test.test_existing_user"))
	assert.False(t, MatchRunFilter(re, "users_test.test_other"))
	assert.False(t, MatchRunFilter(re, "orders_test.test_existing_user"))
}

// mustParseTestGlobals constructs a Parser in test mode + tester
// module + mock-builder predeclared globals, parses src, and returns
// the captured top-level StringDict via Parser.TestGlobals(filename).
//
// Test-only helper: wraps the wiring helperParseTestSrc does (which
// returns reg/ws/err) but exposes the parser globals so DiscoverTests
// can be exercised without rebuilding the wiring inline.
func mustParseTestGlobals(t *testing.T, filename, src string) starlark.StringDict {
	t.Helper()
	reg := NewMockRegistry()
	ws := &WorkflowSpec{}
	p, err := parser.NewParser(
		parser.WithTestMode(),
		parser.WithTestModule(func(_ *parser.Parser, _ *starlark.Thread) starlark.Value {
			return NewTesterModule(reg, ws)
		}),
		parser.WithTestPredeclared(MockLambdaParseTimeBuilders()),
	)
	require.NoError(t, err)
	_, err = p.ParseSource(filename, []byte(src))
	require.NoError(t, err)
	g, ok := p.TestGlobals(filename)
	require.True(t, ok, "TestGlobals(%q) should be present after a test-mode parse", filename)
	return g
}
