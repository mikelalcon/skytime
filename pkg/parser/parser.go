// Package parser implements Starlark→dag.Flow parsing with sandboxed
// load(), lambda capture, and reflection-based kwarg validation.
//
// Architecture firewall: this package MUST NOT import any go.temporal.io/*
// package. The firewall is enforced at test time by
// TestNoTemporalImportsInParserPackage which walks every Go source file's
// imports via go/parser. Crossing this firewall would re-introduce the
// workflow.Context/activity coupling PROJECT.md "no context bleed" forbids.
package parser

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/bridge"
	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
)

// Parser is the per-instance parsing surface (D-07: no global state). One
// parser holds:
//   - a sandbox root (load() resolution)
//   - a per-parser extension registry
//   - lazily-built parse-time globals (rebuild after Register())
//   - a load cache (idempotent loads + cycle protection)
//   - a fileBytes cache for D-18 lambda IDs (sha256(fileBytes)[:8]:line:col)
//   - an in-progress flow map (multi-flow per file, D-15)
//   - an in-progress lambda map keyed by stable ID (D-18)
//
// All non-public state is constructed by NewParser — callers do not need to
// initialize maps themselves.
type Parser struct {
	root             string
	registry         *extension.Registry
	parseTimeGlobals starlark.StringDict
	maxExecSteps     uint64

	// maxBlockSize is the per-step block-cap for step(block=[...]) actions
	// (D2-07). Default 50; configurable via WithMaxBlockSize. The
	// lintBlockSize finalize-pass enforces it. The activity layer (Phase 2)
	// defensively re-enforces — the parser cap is for fast-fail UX, the
	// activity is the safety boundary.
	maxBlockSize int

	// loadCache holds the result of `load(...)` calls keyed by absolute file
	// path. A second load of the same file returns the cached globals
	// (idempotent loads). Cycles fall through naturally — once we've started
	// loading a file we haven't yet cached, the second concurrent load
	// returns whatever globals were committed at the cache-set point.
	loadCache map[string]loadCacheEntry

	// fileBytes caches the source bytes of every loaded .star file, keyed
	// by absolute path. Lambda capture (lambda_capture.go) reads this to
	// compute D-18 IDs.
	fileBytes map[string][]byte

	// flows is the parser-session multi-flow map (D-15). builtinFlow
	// populates this and rejects duplicate names.
	flows map[string]*dag.Flow

	// flowOrder records flow Names in source-declaration order — the
	// order in which builtinFlow successfully registered each. Parallel
	// to flows map; used by FlowsInOrder() for the `skytime info` table.
	// Quick task 260504-k9c.
	flowOrder []string

	// lambdas indexes captured lambdas by their stable D-18 ID. Phase 3
	// uses this map at workflow start to resolve LambdaIDs back to the
	// *starlark.Function values.
	lambdas map[string]*dag.CapturedLambda

	// triggers is the parser-session multi-trigger map (D-07-12).
	// builtinTrigger populates this. Keyed by posKey(pos) to keep map
	// operations stable across tests (positions are guaranteed unique
	// per call site by Starlark's exec model).
	triggers map[string]*dag.Trigger

	// triggerWarnings holds deferred warnings produced by finalize
	// passes (D-07-13 byte-identical duplicate-trigger warnings).
	// The boot loop drains these via Parser.TriggerWarnings() and
	// surfaces them via slog.Warn at server startup. Tests inspect
	// directly.
	triggerWarnings []string

	// preBuiltResults caches *dag.Result objects built by the pre-exec
	// scan in preExecBuildResults (Phase 04.2 D4.2-02). Keyed by call
	// position string ("file:line:col"). builtinResult looks up the
	// cached node at exec time instead of evaluating the value= dict
	// (which is rewritten to a sentinel `0` in execSrc so Starlark
	// never tries to resolve `ctx` at top-level).
	preBuiltResults map[string]*dag.Result

	// Phase 5: test-mode opt-in. WithTestMode() flips testMode true.
	// WithTestModule(builderFn) wires a builder that newParseTimeGlobals
	// (extended in Plan 02) calls to inject the `tester` *starlarkstruct.Module
	// + starlarktest.LoadAssertModule() globals when testMode is true.
	//
	// Splitting "flag" from "module builder" breaks the parser→testing
	// import cycle: parser stays test-package-agnostic; pkg/cli/test.go
	// (Plan 06) supplies the builder via the option.
	//
	// testGlobals is reserved for Plan 05 (def test_* discovery). Plan 02
	// populates parseTimeGlobals; Plan 05 populates testGlobals[filename]
	// with the StringDict returned from starlark.ExecFileOptions.
	testMode          bool
	testModuleBuilder func(p *Parser, thread *starlark.Thread) starlark.Value
	testGlobals       map[string]starlark.StringDict

	// testPredeclared is an optional map of additional parse-time
	// globals injected when testMode is active. Phase 5 Plan 02 uses
	// this to bind ok/err/nonretryable so mock_fn lambda bodies (which
	// reference these as free variables) resolve cleanly at parse
	// time. Production parses (testMode=false) never see these names.
	//
	// Wired via WithTestPredeclared. The pkg/cli/test.go entry point
	// (Plan 06) calls bridge-like helpers in pkg/testing
	// (MockLambdaGlobals, etc.) and passes the relevant subset here.
	testPredeclared starlark.StringDict
}

// defaultMaxBlockSize is the D2-07 default cap for step(block=[...]) action
// counts. Balanced against Temporal's ~4MB activity input limit per project
// research; configurable via WithMaxBlockSize.
const defaultMaxBlockSize = 50

// loadCacheEntry caches the globals resulting from a load() call. Errors are
// also cached so a re-load of a broken module returns the same error rather
// than re-reading the file.
type loadCacheEntry struct {
	globals starlark.StringDict
	err     error
}

// NewParser constructs a Parser with the given options. Options run in
// declaration order; the first error short-circuits and is returned with a
// "parser option" prefix. Common errors:
//   - extension registration with nil Idempotent → wraps
//     extension.ErrIdempotentRequired (D-12).
//   - extension name collision → "extension %q already registered".
//
// The parse-time globals dict is built lazily (on first ParseFile/ParseSource)
// because Register() may be called after NewParser (EXT-06 dynamic
// registration). Callers wanting to validate everything up-front should
// either register all extensions via WithExtensions and call ParseSource on
// an empty source, or rely on Register's own returned error.
func NewParser(opts ...Option) (*Parser, error) {
	p := &Parser{
		registry:        extension.NewRegistry(),
		maxExecSteps:    bridge.DefaultMaxExecutionSteps,
		maxBlockSize:    defaultMaxBlockSize, // D2-07
		loadCache:       make(map[string]loadCacheEntry),
		fileBytes:       make(map[string][]byte),
		flows:           make(map[string]*dag.Flow),
		flowOrder:       make([]string, 0, 4), // Quick 260504-k9c
		lambdas:         make(map[string]*dag.CapturedLambda),
		triggers:        make(map[string]*dag.Trigger),
		preBuiltResults: make(map[string]*dag.Result),
	}
	for _, opt := range opts {
		if err := opt(p); err != nil {
			return nil, fmt.Errorf("parser option: %w", err)
		}
	}
	return p, nil
}

// Register adds an extension to the parser's registry (EXT-06: dynamic
// registration). Equivalent to passing WithExtensions(ext) to NewParser, but
// callable after construction. Invalidates the cached parseTimeGlobals so
// the next parse re-runs Initialize for the newly-registered extension.
func (p *Parser) Register(ext extension.Extension) error {
	if err := p.registry.Register(ext); err != nil {
		return err
	}
	// Force rebuild on next parse so the new extension's Initialize fires
	// and its name appears in the predeclared globals.
	p.parseTimeGlobals = nil
	return nil
}

// Lambdas returns the parser session's captured lambda map keyed by D-18
// stable ID (sha256(fileBytes)[:8] + ":" + line + ":" + col). The returned
// map is the live map — callers MUST NOT mutate it. Used by the worker
// bootstrap (pkg/worker, plan 03-04) to populate the FlowRegistry's
// per-ParsedFlow Lambdas field; lambda IDs are globally unique so a single
// shared map is correct.
//
// Empty when called before any ParseFile / ParseSource invocation.
func (p *Parser) Lambdas() map[string]*dag.CapturedLambda {
	return p.lambdas
}

// Flows returns the parser session's accumulated flow map keyed by flow
// name. The returned map is the live map — callers MUST NOT mutate it. Used
// by the worker bootstrap (pkg/worker, plan 03-04) to enumerate all flows
// across multiple ParseFile invocations during boot.
//
// Empty when called before any ParseFile / ParseSource invocation.
func (p *Parser) Flows() map[string]*dag.Flow {
	return p.flows
}

// FlowsInOrder returns the parser session's flows in source-declaration
// order (the order in which `flow(...)` calls were evaluated). DO NOT
// reorder. Used by `skytime info` to render the table in source order.
// Returns the LIVE *Flow pointers — callers MUST NOT mutate.
//
// Returns an empty (non-nil) slice when called before any ParseFile /
// ParseSource invocation, or when no flows have been registered.
//
// Quick task 260504-k9c.
func (p *Parser) FlowsInOrder() []*dag.Flow {
	out := make([]*dag.Flow, 0, len(p.flowOrder))
	for _, name := range p.flowOrder {
		if f, ok := p.flows[name]; ok {
			out = append(out, f)
		}
	}
	return out
}

// TestGlobals returns the top-level Starlark globals captured during a
// test-mode parse for the given filename, plus a found-bool. Returns
// (nil, false) for production parses or unknown filenames.
//
// Phase 5 (Plan 05) uses this to enumerate `def test_*` functions for
// discovery (D5-A1). Plan 04 already populates testGlobals so the
// runner (Plan 04 Task 3) can drive single-file tests end-to-end.
//
// Returns the LIVE map; callers MUST NOT mutate.
func (p *Parser) TestGlobals(filename string) (starlark.StringDict, bool) {
	g, ok := p.testGlobals[filename]
	return g, ok
}

// FileBytes returns the parser session's cached file bytes keyed by absolute
// path (the same path stored on captured lambda syntax.Position.Filename()).
// Used by the Phase 4 AST re-parse path (pkg/parser/ctx_walk.go, plan 04-02)
// to recover *syntax.File for the ctx.<name> attribute walk —
// *starlark.Function does NOT retain AST after compilation per
// 04-RESEARCH §Pattern 3 critical finding.
//
// Returns the LIVE map; callers MUST NOT mutate. Empty when called
// before any ParseFile/ParseSource invocation.
func (p *Parser) FileBytes() map[string][]byte { return p.fileBytes }

// ParseFile reads a .star file from disk and parses it (and any files
// reached via load()). Returns the parser session's flow map keyed by flow
// name. Errors are *dag.ParseError or *dag.ValidationError.
func (p *Parser) ParseFile(path string) (map[string]*dag.Flow, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, &dag.ParseError{Msg: fmt.Sprintf("resolve %q: %v", path, err)}
	}
	src, err := os.ReadFile(absPath)
	if err != nil {
		return nil, &dag.ParseError{Msg: fmt.Sprintf("read %q: %v", path, err)}
	}
	return p.parse(absPath, src)
}

// ParseSource parses an in-memory source. Filename is used for error
// attribution and lambda IDs (D-18 keys off the filename via Position).
// Convenience for tests; production code uses ParseFile.
func (p *Parser) ParseSource(filename string, src []byte) (map[string]*dag.Flow, error) {
	return p.parse(filename, src)
}

// parse is the shared engine for ParseFile / ParseSource.
//
// Pass sequence (resolved per RESEARCH.md Open Questions #3):
//  1. Lazy-init parseTimeGlobals (run extension Initialize once if needed).
//  2. Cache file bytes for D-18 lambda IDs.
//  3. Allocate a FRESH *starlark.Thread (Pitfall #1).
//  4. starlark.ExecFileOptions with explicit FileOptions (NOT deprecated
//     ExecFile) — drives all transitive load() calls.
//  5. finalize() — cross-flow resolution + lint passes (built in tasks 2-3).
//
// PARSE-05: a top-level recover() converts any panic in Starlark code or
// our builtins into a *dag.ParseError so the caller never sees a runtime
// crash on malformed input.
func (p *Parser) parse(filename string, src []byte) (result map[string]*dag.Flow, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &dag.ParseError{Msg: fmt.Sprintf("internal panic during parse of %s: %v", filename, r)}
			result = nil
		}
	}()

	if p.parseTimeGlobals == nil {
		// D-08 lifecycle: Initialize each registered extension exactly once
		// per parser. Use a transient thread for the Initialize calls — the
		// actual parse uses its own thread below.
		initThread := &starlark.Thread{Name: "init-extensions:" + filename}
		gs, gerr := newParseTimeGlobals(p, initThread)
		if gerr != nil {
			return nil, gerr
		}
		p.parseTimeGlobals = gs
	}

	// Cache file bytes for D-18 lambda ID hashing. The ORIGINAL src is
	// stored — preExecBuildResults below rewrites a separate execSrc
	// that's only handed to starlark.ExecFileOptions; lambda IDs and
	// AST re-parse paths still see the user's original bytes.
	p.fileBytes[filename] = src

	// Phase 04.2 pre-exec pass (D4.2-02): scan for `result(value={...})`
	// calls; build *dag.Result objects from the AST upfront; rewrite
	// each call's value= dict-literal in execSrc to a `0` sentinel
	// (length-preserved) so Starlark can evaluate the file even when
	// the dict references `ctx.<name>` at top-level (not in scope).
	// builtinResult retrieves the pre-built node by call position.
	execSrc, err := p.preExecBuildResults(filename, src)
	if err != nil {
		return nil, err
	}

	// FRESH thread per parse (Pitfall #1).
	thread := &starlark.Thread{
		Name: "parse:" + filename,
		Load: p.makeLoad(),
		Print: func(_ *starlark.Thread, msg string) {
			slog.Default().Info("starlark print at parse time", "file", filename, "msg", msg)
		},
	}
	thread.SetMaxExecutionSteps(p.maxExecSteps)

	opts := defaultFileOptions()
	if p.testMode {
		// Phase 5 Plan 04: capture the file's top-level globals so the
		// runner (pkg/testing/runner.go) can enumerate `def test_*`
		// functions via Parser.TestGlobals(filename). The captured
		// StringDict includes user-defined functions, top-level
		// variables, and the parse-time predeclared globals — Plan 05
		// will filter for `test_`-prefixed *starlark.Function entries.
		globals, execErr := starlark.ExecFileOptions(opts, thread, filename, execSrc, p.parseTimeGlobals)
		if execErr != nil {
			return nil, wrapStarlarkError(execErr)
		}
		if p.testGlobals == nil {
			p.testGlobals = map[string]starlark.StringDict{}
		}
		p.testGlobals[filename] = globals
	} else {
		if _, execErr := starlark.ExecFileOptions(opts, thread, filename, execSrc, p.parseTimeGlobals); execErr != nil {
			return nil, wrapStarlarkError(execErr)
		}
	}

	if ferr := p.finalize(); ferr != nil {
		return nil, ferr
	}
	return p.flows, nil
}

// defaultFileOptions returns the *syntax.FileOptions Phase 1 uses for every
// parse. Always pass an explicit FileOptions to ExecFileOptions — the
// deprecated `starlark.ExecFile` would silently use legacy global resolve
// flags (Pitfall #4).
//
// Choices:
//   - Set:               false — Starlark's set() is off-by-default and
//     stays off (D-20 spirit, applied at parse time too).
//   - While:             false — forbid `while` at top level (forces
//     non-determinism risk; consultants use lambdas + iteration helpers).
//   - TopLevelControl:   true  — top-level if/for is allowed (a fixture may
//     gate flow registration on a constant; harmless).
//   - GlobalReassign:    false — top-level reassignment forbidden (D-20
//     spirit).
//   - LoadBindsGlobally: false — load bindings stay file-local.
//   - Recursion:         false — bounded execution (forbid `def`s calling
//     themselves transitively).
func defaultFileOptions() *syntax.FileOptions {
	return &syntax.FileOptions{
		Set:               false,
		While:             false,
		TopLevelControl:   true,
		GlobalReassign:    false,
		LoadBindsGlobally: false,
		Recursion:         false,
	}
}
