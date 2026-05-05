package testing

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// ErrBadFilter wraps the error from regexp.Compile when WithRunFilter
// receives an unparseable pattern. CompileRunFilter returns
// fmt.Errorf("%w: %v", ErrBadFilter, compileErr) so callers can detect
// the class via errors.Is(err, ErrBadFilter) without losing the
// underlying compile diagnostic.
var ErrBadFilter = errors.New("WithRunFilter: pattern compile failed")

// TestFunc is one discovered def test_*() function.
type TestFunc struct {
	Name string             // "test_existing_user"
	Fn   *starlark.Function // captured top-level def
	Pos  syntax.Position    // declaration position (file:line:col)
}

// DiscoverTestFiles returns sorted absolute (or original-relative)
// paths of *_test.star files under root. If root is a single file
// ending in _test.star, returns [root]. If root is a non-test file,
// returns an error. If root is a directory, walks recursively
// (filepath.WalkDir) collecting every file whose basename ends with
// _test.star.
//
// D5-A2: skytime test <dir> walks recursively; only files matching
// *_test.star are test files. The single-file path supports
// pkg/testing.Run callers that want to invoke a single test file
// directly (Plan 06's CLI also funnels here).
func DiscoverTestFiles(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", root, err)
	}
	if !info.IsDir() {
		if !strings.HasSuffix(root, "_test.star") {
			return nil, fmt.Errorf("path %s is not a *_test.star file", root)
		}
		return []string{root}, nil
	}

	var out []string
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), "_test.star") {
			out = append(out, path)
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Strings(out)
	return out, nil
}

// DiscoverTests enumerates the top-level Starlark globals returned by
// the test-mode parse, filters on *starlark.Function symbols whose
// name starts with "test_" AND whose NumParams() == 0 (def test_x():).
// Sorts by name for replay-determinism.
//
// RESEARCH Pattern 4 + D5-A1. Only top-level def test_*() functions
// with zero parameters qualify; lambdas, nested defs, helpers like
// `def test_helper(x):`, and non-Function globals are silently
// excluded.
func DiscoverTests(globals starlark.StringDict) []TestFunc {
	keys := make([]string, 0, len(globals))
	for k := range globals {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var out []TestFunc
	for _, name := range keys {
		if !strings.HasPrefix(name, "test_") {
			continue
		}
		fn, ok := globals[name].(*starlark.Function)
		if !ok {
			continue // shadowed by a non-function value; ignore
		}
		if fn.NumParams() != 0 {
			continue // skip helpers like test_helper(x); ignored silently in v1
		}
		out = append(out, TestFunc{Name: name, Fn: fn, Pos: fn.Position()})
	}
	return out
}

// CompileRunFilter compiles a regex pattern for WithRunFilter. Returns
// (nil, nil) for empty pattern (match-all). Compile failures wrap
// ErrBadFilter so callers can detect via errors.Is.
func CompileRunFilter(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadFilter, err)
	}
	return re, nil
}

// MatchRunFilter returns true if re == nil (match-all) or re matches
// fullName via regexp.MatchString semantics. Used by the runner to
// filter discovered tests by `<file_basename_without_ext>.<test_name>`
// (D5-E3).
func MatchRunFilter(re *regexp.Regexp, fullName string) bool {
	if re == nil {
		return true
	}
	return re.MatchString(fullName)
}
