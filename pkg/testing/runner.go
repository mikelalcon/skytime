package testing

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

// WithRunFilter compiles a regex (Go regexp syntax) at option-apply
// time and gates which discovered def test_*() names execute. The
// full match key is "<file_basename_without_ext>.<test_name>"
// (D5-E3). Empty pattern = run all.
//
// Compile failures surface here (NOT at run time) wrapped in
// ErrBadFilter; callers can detect via errors.Is(err, ErrBadFilter).
func WithRunFilter(pattern string) Option {
	return func(c *runConfig) error {
		re, err := CompileRunFilter(pattern)
		if err != nil {
			return err
		}
		c.runPattern = pattern
		c.runRegex = re
		return nil
	}
}

type runConfig struct {
	exts       []extension.Extension
	runPattern string
	runRegex   *regexp.Regexp // compiled once at WithRunFilter() time
}

// Run is the Go-level foundation API for Phase 5's test harness
// (Open Q7). pkg/cli/test.go (Plan 06) wraps this; Phase 6's example
// project can call it directly from a *_test.go in the consumer
// package.
//
// Behavior:
//   - Walks dir RECURSIVELY for *_test.star files (D5-A2 via
//     DiscoverTestFiles → filepath.WalkDir). Single-file paths
//     (foo_test.star) are also accepted.
//   - For each test file: NewParser(WithTestMode + WithTestModule +
//     WithTestPredeclared + WithExtensions) + ParseFile.
//   - Discovers def test_*() symbols via DiscoverTests over the
//     captured Parser.TestGlobals(filename). Top-level only;
//     NumParams()==0 required (D5-A1, RESEARCH Pattern 4).
//   - For each test, t.Run with name "<file_basename>/<test_name>"
//     and invoke runOneTest(subT, fn, reg, ws). Discovery results
//     are sorted alphabetically for replay determinism.
//   - WithRunFilter(pattern) compiles regex once at option time; only
//     tests whose `<file_basename>.<test_name>` matches the regex
//     execute (D5-E3).
//   - Sequential within and across files (D5-E5; cross-file
//     parallelization deferred to v1.x per RESEARCH Open Q2).
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

// discoverTestFiles delegates to the public DiscoverTestFiles so
// existing internal callers (runner_test.go's walkAndDriveTests) keep
// working without churn. Plan 05: recursive walk + single-file
// support live in DiscoverTestFiles itself.
func discoverTestFiles(dir string) ([]string, error) {
	return DiscoverTestFiles(dir)
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
		if !MatchRunFilter(cfg.runRegex, fullName) {
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

	// Discover def test_*() functions in the captured globals via
	// the shared DiscoverTests helper (D5-A1, RESEARCH Pattern 4).
	// Returns alphabetically sorted entries for replay determinism.
	globals, ok := p.TestGlobals(file)
	if !ok {
		return nil, fmt.Errorf("TestGlobals not found for %s", file)
	}
	fns := DiscoverTests(globals)
	tests := make([]discoveredTest, 0, len(fns))
	for _, fn := range fns {
		tests = append(tests, discoveredTest{Name: fn.Name, Fn: fn.Fn})
	}

	return &parsedTestFile{
		File:  file,
		Reg:   reg,
		WS:    ws,
		Tests: tests,
	}, nil
}
