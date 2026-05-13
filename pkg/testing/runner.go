package testing

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

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

// WithFormat sets the output format. Accepted values:
//
//   - "" or "human" → static line-per-test Go-test style (D5-E1).
//   - "json" → cmd/test2json mirror (D5-E2 + Open Q6 RFC3339Nano UTC).
//
// Unknown values return an error at option-apply time; mistyped
// formats fail loudly rather than silently falling back to human.
func WithFormat(format string) Option {
	return func(c *runConfig) error {
		switch format {
		case "", "human":
			c.formatJSON = false
		case "json":
			c.formatJSON = true
		default:
			return fmt.Errorf("WithFormat: unknown format %q (accepted: human, json)", format)
		}
		return nil
	}
}

// WithOutput sets the writer for human-format / JSON-format output.
// Default is os.Stdout. Tests typically pass a *bytes.Buffer to
// capture and inspect the rendered records.
func WithOutput(w io.Writer) Option {
	return func(c *runConfig) error {
		c.formatOut = w
		return nil
	}
}

type runConfig struct {
	exts        []extension.Extension
	credHandler extension.CredentialHandler // Phase 7.4 CLI-10 — installed via WithCredentialHandler; nil = no handler (existing behavior)
	runPattern  string
	runRegex    *regexp.Regexp // compiled once at WithRunFilter() time
	formatJSON  bool           // true → emit cmd/test2json mirror
	formatOut   io.Writer      // default os.Stdout (resolved at runOneFile)
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

	// Sequential per-file iteration (D5-E5 + Open Q2 sequential v1).
	grandStart := time.Now()
	for _, file := range files {
		runOneFile(t, file, cfg)
	}

	// Final summary line. JSON consumers parse per-event records and
	// don't need a trailing summary; human consumers want one.
	if cfg.formatJSON {
		return
	}
	out := cfg.formatOut
	if out == nil {
		out = os.Stdout
	}
	elapsed := time.Since(grandStart).Seconds()
	if t.Failed() {
		fmt.Fprintf(out, "FAIL  %d files  (%.2fs)\n", len(files), elapsed)
	} else {
		fmt.Fprintf(out, "PASS  %d files  (%.2fs)\n", len(files), elapsed)
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
//
// In JSON mode, emits one cmd/test2json-compatible record per event:
//
//	start → run → (output × N) → pass | fail | skip
//
// In human mode (default), emits `--- PASS:` / `--- FAIL:` /
// `--- SKIP:` lines per test plus a per-file footer (D5-E1).
func runOneFile(t *testing.T, file string, cfg *runConfig) {
	t.Helper()
	renderOneFile(t, file, cfg, func(name string, fn func(testReporter) (failed, skipped bool, detail string)) (bool, bool, string) {
		var passed, skipped bool
		var detail string
		t.Run(name, func(subT *testing.T) {
			rec := &capturingTReporter{T: subT}
			fn(rec)
			detail = rec.detail.String()
			passed = !subT.Failed() && !subT.Skipped()
			skipped = subT.Skipped()
		})
		return passed, skipped, detail
	})
}

// driveTestFn is the per-test driver injected into renderOneFile so
// callers can plug in either a real *testing.T (production) or a
// recording shim (tests that observe rendered output without poisoning
// their parent test).
//
//   - name: the t.Run name ("<file_stem>/<test_name>")
//   - inner(rep): runs the def test_*() body under rep; rep can be a
//     *testing.T-backed capturingTReporter or a pure shim.
//
// Returns (passed, skipped, detail) where detail is the assertion
// failure text captured by the reporter (or empty if no failures).
type driveTestFn func(name string, inner func(testReporter) (failed, skipped bool, detail string)) (passed, skipped bool, detail string)

// renderOneFile is the format-agnostic core of runOneFile, factored
// for testability. drive callbacks per discovered def test_*();
// renderOneFile handles JSON-vs-human emission, per-file footer, and
// the {start, run, pass/fail/skip, output} sequence.
//
// Test code drives this with a recording shim so format-test
// observations don't propagate inner failures to the parent
// *testing.T (Go's t.Run propagates inner failures to its parent).
func renderOneFile(t *testing.T, file string, cfg *runConfig, drive driveTestFn) {
	t.Helper()

	tests, err := parseTestFile(file, cfg)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}

	fileBase := filepath.Base(file)
	fileStem := strings.TrimSuffix(fileBase, ".star") // e.g., "simple_test"

	out := cfg.formatOut
	if out == nil {
		out = os.Stdout
	}
	var em *jsonEmitter
	if cfg.formatJSON {
		em = newJSONEmitter(out)
	}

	pkg := fileBase // cmd/test2json's Package field — basename per D5-E2
	var fileTotal, fileFailed int
	fileStart := time.Now()

	for _, tc := range tests.Tests {
		fullName := fileStem + "." + tc.Name
		if !MatchRunFilter(cfg.runRegex, fullName) {
			continue
		}
		fileTotal++
		fn := tc.Fn

		if em != nil {
			em.emit("start", pkg, tc.Name, "", 0)
			em.emit("run", pkg, tc.Name, "", 0)
		}

		testStart := time.Now()
		passed, skipped, detail := drive(fileStem+"/"+tc.Name, func(rep testReporter) (bool, bool, string) {
			runOneTest(rep, fn, tests.Reg, tests.WS)
			return false, false, "" // drive callers compute these from rep
		})

		elapsed := time.Since(testStart).Seconds()
		switch {
		case skipped:
			if em != nil {
				if detail != "" {
					em.emit("output", pkg, tc.Name, detail, 0)
				}
				em.emit("skip", pkg, tc.Name, "", elapsed)
			} else {
				fmt.Fprint(out, formatHumanLine("SKIP", tc.Name, elapsed))
			}
		case !passed:
			fileFailed++
			if em != nil {
				if detail != "" {
					em.emit("output", pkg, tc.Name, detail, 0)
				}
				em.emit("fail", pkg, tc.Name, "", elapsed)
			} else {
				fmt.Fprint(out, formatHumanLine("FAIL", tc.Name, elapsed))
				if detail != "" {
					// D5-E1 indented detail block under FAIL line.
					fmt.Fprint(out, indentDetail(detail))
				}
			}
		default:
			if em != nil {
				em.emit("pass", pkg, tc.Name, "", elapsed)
			} else {
				fmt.Fprint(out, formatHumanLine("PASS", tc.Name, elapsed))
			}
		}
	}

	// Per-file footer (human only); D5-E1 final example.
	if em == nil && fileTotal > 0 {
		fileElapsed := time.Since(fileStart).Seconds()
		if fileFailed == 0 {
			fmt.Fprintf(out, "PASS  %s  %d tests  (%.2fs)\n", pkg, fileTotal, fileElapsed)
		} else {
			fmt.Fprintf(out, "FAIL  %s  %d tests  %d failed  (%.2fs)\n", pkg, fileTotal, fileFailed, fileElapsed)
		}
	}
}

// indentDetail prefixes every line of detail with "    " so failures
// render as a Go-test-style indented block under their --- FAIL line.
// Empty input → empty output. Trailing newline preserved.
func indentDetail(detail string) string {
	if detail == "" {
		return ""
	}
	lines := strings.Split(strings.TrimRight(detail, "\n"), "\n")
	var b strings.Builder
	for _, line := range lines {
		b.WriteString("    ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
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

// capturingTReporter wraps *testing.T to BOTH propagate failures
// (subT.Errorf / subT.Error → real test failure) AND capture the
// rendered text so the runner can emit it as an `output` record in
// JSON mode or an indented detail block under `--- FAIL` in human
// mode. CLI-03 explicit requirement: surface Starlark callsite
// without Go stack frames.
//
// Implements testReporter via composition: Helper / Errorf / Error.
type capturingTReporter struct {
	T      *testing.T
	detail strings.Builder
}

func (c *capturingTReporter) Helper() { c.T.Helper() }

func (c *capturingTReporter) Errorf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	c.detail.WriteString(msg)
	if !strings.HasSuffix(msg, "\n") {
		c.detail.WriteByte('\n')
	}
	// Surface to *testing.T as the actual failure, but use Errorf
	// directly to avoid double-formatting.
	c.T.Errorf("%s", msg)
}

func (c *capturingTReporter) Error(args ...any) {
	msg := fmt.Sprint(args...)
	c.detail.WriteString(msg)
	if !strings.HasSuffix(msg, "\n") {
		c.detail.WriteByte('\n')
	}
	c.T.Error(args...)
}
