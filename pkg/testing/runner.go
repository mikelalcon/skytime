package testing

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"go.starlark.net/starlark"

	"github.com/mikelalcon/skytime/pkg/extension"
	"github.com/mikelalcon/skytime/pkg/interpreter"
	"github.com/mikelalcon/skytime/pkg/parser"
)

// Option configures a Run invocation.
type Option func(*runConfig) error

// WithExtensions registers extensions on the parser session that the
// runner builds for each *_test.star file. Mirrors parser.WithExtensions
// but lives at pkg/testing's surface so consumers don't need a
// pkg/parser import to wire extensions.
func WithExtensions(exts ...extension.Extension) Option {
	return func(c *runConfig) error {
		c.exts = append(c.exts, exts...)
		return nil
	}
}

// WithRunFilter sets a Go-style substring filter that gates which
// discovered def test_*() names execute. The full match key is
// "<file_basename_without_ext>.<test_name>" (D5-E3). Empty pattern =
// run all (the v1 default).
//
// Plan 05 will replace the substring match with regexp.MatchString;
// Plan 04 ships the option signature so Plan 06's CLI can wire
// `--run` without further surface changes.
func WithRunFilter(pattern string) Option {
	return func(c *runConfig) error { c.runPattern = pattern; return nil }
}

type runConfig struct {
	exts       []extension.Extension
	runPattern string
}

// Run is the Go-level foundation API for Phase 5's test harness
// (Open Q7). pkg/cli/test.go (Plan 06) wraps this; Phase 6's example
// project can call it directly from a *_test.go in the consumer
// package.
//
// Behavior:
//   - Walks dir for *_test.star files. Plan 04 ships single-directory
//     (non-recursive) discovery; Plan 05 generalizes with recursion +
//     ignores.
//   - For each test file: NewParser(WithTestMode + WithTestModule +
//     WithExtensions) + ParseFile.
//   - Discovers def test_*() symbols via Parser.TestGlobals(filename).
//   - For each test, t.Run with name "<file_basename>/<test_name>"
//     and invoke runOneTest(subT, fn, reg, ws). Discovery results are
//     sorted alphabetically for replay determinism (Go map iteration
//     is randomized).
//
// (Plan 05 replaces the single-directory restriction + adds --run
// regex filtering + adds --format=json output records.)
func Run(t *testing.T, dir string, opts ...Option) {
	t.Helper()
	cfg := &runConfig{}
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			t.Fatalf("Run: option failed: %v", err)
		}
	}
	files, err := discoverTestFiles(dir)
	if err != nil {
		t.Fatalf("Run: discover test files in %s: %v", dir, err)
	}
	if len(files) == 0 {
		t.Skipf("Run: no *_test.star files under %s", dir)
		return
	}

	for _, file := range files {
		runOneFile(t, file, cfg)
	}
}

// discoverTestFiles is Plan 04's stub. Plan 05 replaces with a
// recursive walk + .gitignore-style ignores.
func discoverTestFiles(dir string) ([]string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		// Single-file path: caller passed a *_test.star file directly.
		if !strings.HasSuffix(dir, "_test.star") {
			return nil, fmt.Errorf("path %s is not a *_test.star file", dir)
		}
		return []string{dir}, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), "_test.star") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out, nil
}

// runOneFile parses one *_test.star file and runs every discovered
// def test_*() under t.Run. The MockRegistry, WorkflowSpec, and
// runContext are shared across all tests in the file (file-frame
// mocks visible to every test; per-test frames push/pop inside
// runOneTest).
func runOneFile(t *testing.T, file string, cfg *runConfig) {
	t.Helper()

	tests, err := parseTestFile(file, cfg)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}

	fileBase := filepath.Base(file)
	fileStem := strings.TrimSuffix(fileBase, ".star") // e.g., "simple_test"

	for _, tc := range tests.Tests {
		fullName := fileStem + "." + tc.Name
		if !matchesRunFilter(cfg.runPattern, fullName) {
			continue
		}
		fn := tc.Fn
		t.Run(fileStem+"/"+tc.Name, func(subT *testing.T) {
			runOneTest(subT, fn, tests.Reg, tests.WS)
		})
	}
}

// parsedTestFile holds everything runOneFile / its test counterparts
// need to drive a single *_test.star file. The runContext slot has
// already been wired to ctxRef; reg + ws are the shared per-file
// state.
type parsedTestFile struct {
	File  string
	Reg   *MockRegistry
	WS    *WorkflowSpec
	Tests []discoveredTest
}

// discoveredTest pairs a def test_*() name with its compiled
// *starlark.Function. The slice in parsedTestFile is sorted
// alphabetically for replay determinism.
type discoveredTest struct {
	Name string
	Fn   *starlark.Function
}

// parseTestFile loads, parses, and discovers def test_*() symbols
// from a single *_test.star file. Returns a parsedTestFile with reg
// + ws already wired into a runContext keyed by the parser's flows.
func parseTestFile(file string, cfg *runConfig) (*parsedTestFile, error) {
	bytes, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}

	reg := NewMockRegistry()
	ws := &WorkflowSpec{}
	var ctxRef *runContext
	builder := func(_ *parser.Parser, _ *starlark.Thread) starlark.Value {
		return newTesterModuleWithCtx(reg, ws, &ctxRef)
	}

	pOpts := []parser.Option{
		parser.WithTestMode(),
		parser.WithTestModule(builder),
		parser.WithTestPredeclared(MockLambdaParseTimeBuilders()),
	}
	if len(cfg.exts) > 0 {
		pOpts = append(pOpts, parser.WithExtensions(cfg.exts...))
	}
	p, err := parser.NewParser(pOpts...)
	if err != nil {
		return nil, fmt.Errorf("NewParser: %w", err)
	}
	if _, err := p.ParseSource(file, bytes); err != nil {
		return nil, fmt.Errorf("ParseSource: %w", err)
	}

	// Build runContext: parser flows + content hashes. The content
	// hash is sha256-hex of the test file's bytes registered to the
	// flow. Single-file scope only — load() across files is a Phase 6
	// concern and out of scope for v1.
	flowsByName := map[string]*interpreter.ParsedFlow{}
	hashesByName := map[string]string{}
	fileBytesMap := p.FileBytes()
	for name, flow := range p.Flows() {
		var srcBytes []byte
		if b, ok := fileBytesMap[file]; ok {
			srcBytes = b
		}
		sum := sha256.Sum256(srcBytes)
		hash := hex.EncodeToString(sum[:])
		flowsByName[name] = &interpreter.ParsedFlow{
			Flow:    flow,
			Lambdas: p.Lambdas(),
		}
		hashesByName[name] = hash
	}
	ctxRef = &runContext{
		flows:         flowsByName,
		contentHashes: hashesByName,
	}

	// Discover def test_*() functions in the captured globals.
	globals, ok := p.TestGlobals(file)
	if !ok {
		return nil, fmt.Errorf("TestGlobals not found for %s", file)
	}

	var names []string
	for name, val := range globals {
		if !strings.HasPrefix(name, "test_") {
			continue
		}
		fn, isFn := val.(*starlark.Function)
		if !isFn {
			continue
		}
		if fn.NumParams() != 0 {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names) // replay-deterministic order

	tests := make([]discoveredTest, 0, len(names))
	for _, n := range names {
		tests = append(tests, discoveredTest{Name: n, Fn: globals[n].(*starlark.Function)})
	}

	return &parsedTestFile{
		File:  file,
		Reg:   reg,
		WS:    ws,
		Tests: tests,
	}, nil
}

// matchesRunFilter applies the Plan 05 regex semantics. Plan 04
// stub: empty pattern → match all; non-empty → strings.Contains.
// Plan 05 replaces with regexp.MatchString.
func matchesRunFilter(pattern, fullName string) bool {
	if pattern == "" {
		return true
	}
	return strings.Contains(fullName, pattern)
}
