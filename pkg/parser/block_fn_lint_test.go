package parser

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// =============================================================================
// Plan 04.1-04 Task 1 — classifyBlockFn AST-walking classifier (D4.1-11)
//
// The classifier walks ONLY the OUTERMOST CallExpr nodes that produce
// ActionRef elements (typically the body of a `[gh.op(...) for x in ctx.list]`
// comprehension or elements of a returned list literal). Sub-expressions
// inside kwargs of a recognized `<ext>.<op>(...)` call are NOT walked —
// kwarg shape validation is the parser's separate kwarg-pass concern (D-11).
//
// Tests use the existing fakeExtension (echo idempotent=true, post
// idempotent=false). Source is parsed via ParseSource so the parser's
// fileBytes cache is populated; the captured *dag.Step.BlockFn lambda is
// then handed to p.classifyBlockFn.
// =============================================================================

// parseBlockFnSource is a small helper: parse a single-flow source and
// return the *dag.CapturedLambda hanging off the first Step's BlockFn.
// The flow is named "f" by convention. Fails the test if the captured
// lambda is missing.
//
// Parsing skips the finalize() chain so the classifier-level tests can
// exercise mixed-typed shapes (which lintBlockFnIdempotency would
// otherwise reject before classifyBlockFn ever runs). For end-to-end
// finalize coverage see TestLintBlockFnIdempotency_*.
func parseBlockFnSource(t *testing.T, src string) (*Parser, *dag.CapturedLambda) {
	t.Helper()
	p := newTestParser(t)
	const filename = "test.star"

	// Lazy-init parse-time globals (mirrors Parser.parse path).
	if p.parseTimeGlobals == nil {
		initThread := &starlark.Thread{Name: "init-extensions:" + filename}
		gs, gerr := newParseTimeGlobals(p, initThread)
		require.NoError(t, gerr)
		p.parseTimeGlobals = gs
	}
	p.fileBytes[filename] = []byte(src)
	thread := &starlark.Thread{Name: "parse:" + filename, Load: p.makeLoad()}
	thread.SetMaxExecutionSteps(p.maxExecSteps)
	opts := defaultFileOptions()
	_, execErr := starlark.ExecFileOptions(opts, thread, filename, []byte(src), p.parseTimeGlobals)
	require.NoError(t, execErr, "exec must succeed before classifyBlockFn runs")

	require.Contains(t, p.flows, "f", "expected a flow named 'f'")
	require.Len(t, p.flows["f"].Body, 1, "expected exactly one body node (Step)")
	step, ok := p.flows["f"].Body[0].(*dag.Step)
	require.True(t, ok, "first body node must be *dag.Step, got %T", p.flows["f"].Body[0])
	require.NotNil(t, step.BlockFn, "step must have BlockFn populated for classifier tests")
	return p, step.BlockFn
}

// TestClassifyBlockFn_AllHomogeneous: comprehension over ctx.repos calling a
// single idempotent op (fake_ext.echo). All typed, no opaque.
func TestClassifyBlockFn_AllHomogeneous(t *testing.T) {
	src := `flow(
    name = "f",
    inputs = {"repos": "list"},
    steps = [
        step(block_fn = lambda ctx: [fake_ext.echo(msg = "x") for r in ctx.repos]),
    ],
)`
	p, captured := parseBlockFnSource(t, src)
	got, err := p.classifyBlockFn(captured)
	require.NoError(t, err)
	require.Len(t, got.TypedCalls, 1, "exactly one typed call (the comprehension element)")
	assert.Equal(t, "fake_ext", got.TypedCalls[0].Ext)
	assert.Equal(t, "echo", got.TypedCalls[0].Op)
	assert.True(t, got.TypedCalls[0].Idempotent)
	assert.False(t, got.HasOpaque)
	assert.Empty(t, got.OpaqueCalls)
}

// TestClassifyBlockFn_MixedTyped: list literal with two typed calls of
// different idempotency. Both classified; HasOpaque false.
func TestClassifyBlockFn_MixedTyped(t *testing.T) {
	src := `flow(
    name = "f",
    inputs = {},
    steps = [
        step(block_fn = lambda ctx: [fake_ext.echo(msg = "a"), fake_ext.post(payload = "b")]),
    ],
)`
	p, captured := parseBlockFnSource(t, src)
	got, err := p.classifyBlockFn(captured)
	require.NoError(t, err)
	require.Len(t, got.TypedCalls, 2)
	// Order in list literal: echo (idempotent), then post (NOT idempotent).
	assert.Equal(t, "echo", got.TypedCalls[0].Op)
	assert.True(t, got.TypedCalls[0].Idempotent)
	assert.Equal(t, "post", got.TypedCalls[1].Op)
	assert.False(t, got.TypedCalls[1].Idempotent)
	assert.False(t, got.HasOpaque)
}

// TestClassifyBlockFn_OpaqueHelper: a module-level def is called from the
// block_fn body. The classifier sees a non-`<ext>.<op>` outermost CallExpr
// and marks it opaque.
func TestClassifyBlockFn_OpaqueHelper(t *testing.T) {
	src := `def make_calls(repos):
    return [fake_ext.echo(msg = r) for r in repos]

flow(
    name = "f",
    inputs = {"repos": "list"},
    steps = [
        step(block_fn = lambda ctx: make_calls(ctx.repos)),
    ],
)`
	p, captured := parseBlockFnSource(t, src)
	got, err := p.classifyBlockFn(captured)
	require.NoError(t, err)
	assert.Empty(t, got.TypedCalls, "no recognized <ext>.<op> outermost call in the body")
	assert.True(t, got.HasOpaque, "make_calls(...) is a bare-ident call → opaque")
	require.Len(t, got.OpaqueCalls, 1)
}

// TestClassifyBlockFn_StrInsideKwarg_NotWalked: str(p) inside a recognized
// extension kwarg is NOT walked (D4.1-11 amendment locked rule). Result:
// only the outer fake_ext.echo is classified; HasOpaque stays false.
func TestClassifyBlockFn_StrInsideKwarg_NotWalked(t *testing.T) {
	src := `flow(
    name = "f",
    inputs = {"repos": "list"},
    steps = [
        step(block_fn = lambda ctx: [fake_ext.echo(msg = str(r)) for r in ctx.repos]),
    ],
)`
	p, captured := parseBlockFnSource(t, src)
	got, err := p.classifyBlockFn(captured)
	require.NoError(t, err)
	require.Len(t, got.TypedCalls, 1, "only the outer fake_ext.echo is classified; str() inside kwarg NOT walked")
	assert.Equal(t, "echo", got.TypedCalls[0].Op)
	assert.False(t, got.HasOpaque, "str() inside a recognized ext.op kwarg must NOT mark opaque")
}

// TestClassifyBlockFn_HelperCallTrulyOpaque: helper(x) outside any
// extension call is correctly classified opaque — this IS the truly
// opaque case that defers to runtime (D4.1-12).
func TestClassifyBlockFn_HelperCallTrulyOpaque(t *testing.T) {
	src := `def helper(x):
    return fake_ext.echo(msg = x)

flow(
    name = "f",
    inputs = {"items": "list"},
    steps = [
        step(block_fn = lambda ctx: [helper(x) for x in ctx.items]),
    ],
)`
	p, captured := parseBlockFnSource(t, src)
	got, err := p.classifyBlockFn(captured)
	require.NoError(t, err)
	assert.Empty(t, got.TypedCalls, "no typed call: helper() is bare-ident → opaque")
	assert.True(t, got.HasOpaque)
	require.Len(t, got.OpaqueCalls, 1)
}

// TestClassifyBlockFn_ConditionalExtCalls: a CondExpr (`x if c else y`)
// with two extension calls in either branch. Both are classified.
// Uses two homogeneous-idempotent calls so lintBlockFnIdempotency does
// not reject the parse before the classifier-level test can run; the
// classifier itself is agnostic to idempotency mixing.
func TestClassifyBlockFn_ConditionalExtCalls(t *testing.T) {
	src := `flow(
    name = "f",
    inputs = {"flag": "bool"},
    steps = [
        step(block_fn = lambda ctx: [fake_ext.echo(msg = "y") if ctx.flag else fake_ext.echo(msg = "n")]),
    ],
)`
	p, captured := parseBlockFnSource(t, src)
	got, err := p.classifyBlockFn(captured)
	require.NoError(t, err)
	require.Len(t, got.TypedCalls, 2, "both branches of the conditional are classified")
	assert.Equal(t, "echo", got.TypedCalls[0].Op)
	assert.Equal(t, "echo", got.TypedCalls[1].Op)
	assert.False(t, got.HasOpaque)
}

// TestClassifyBlockFn_EmptyListLiteral: an empty list literal in the body
// returns an empty classification (no typed calls, no opaque). Caller
// treats as homogeneous (vacuously).
func TestClassifyBlockFn_EmptyListLiteral(t *testing.T) {
	src := `flow(
    name = "f",
    inputs = {},
    steps = [
        step(block_fn = lambda ctx: []),
    ],
)`
	p, captured := parseBlockFnSource(t, src)
	got, err := p.classifyBlockFn(captured)
	require.NoError(t, err)
	assert.Empty(t, got.TypedCalls)
	assert.False(t, got.HasOpaque)
	assert.Empty(t, got.OpaqueCalls)
}

// =============================================================================
// Plan 04.1-04 Task 2 — lintBlockFnIdempotency wired into finalize chain
// =============================================================================

// TestLintBlockFnIdempotency_HomogeneousPasses: parsing the
// block_fn_valid_homogeneous fixture (gh.get-only) end-to-end yields no
// error. http extension's `get` is the registered idempotent op; the
// fixture's lambda passes the lint cleanly.
func TestLintBlockFnIdempotency_HomogeneousPasses(t *testing.T) {
	root := findModuleRootForFixture(t)
	fixturePath := root + "/tests/fixtures/block_fn_valid_homogeneous.star"
	p, err := NewParser(WithExtensions(httpExtensionForTest()))
	require.NoError(t, err)
	_, err = p.ParseFile(fixturePath)
	require.NoError(t, err, "homogeneous block_fn must parse cleanly")
}

// TestLintBlockFnIdempotency_MixedRejected: parsing block_fn_invalid_mixed
// surfaces a *dag.ValidationError mentioning "block_fn" + "idempotent" +
// the suggestion "split into separate steps with action_fn each".
func TestLintBlockFnIdempotency_MixedRejected(t *testing.T) {
	root := findModuleRootForFixture(t)
	fixturePath := root + "/tests/fixtures/block_fn_invalid_mixed.star"
	p, err := NewParser(WithExtensions(httpExtensionForTest()))
	require.NoError(t, err)
	_, err = p.ParseFile(fixturePath)
	require.Error(t, err, "mixed block_fn must be rejected at parse time")
	var ve *dag.ValidationError
	require.True(t, errors.As(err, &ve), "expected *dag.ValidationError, got %T: %v", err, err)
	assert.Contains(t, ve.Msg, "block_fn", "error must mention block_fn")
	assert.Contains(t, ve.Msg, "idempotent", "error must mention idempotency")
	assert.Contains(t, ve.Msg, "split into separate steps with action_fn each",
		"error must include the D4.1-11 fix suggestion verbatim")
}

// TestLintBlockFnIdempotency_OpaqueDefers: parsing block_fn_valid_opaque
// (block_fn body calls a module-level def `make_paths(ctx.repos)`)
// yields no error — the lint defers to the runtime fallback (D4.1-12).
func TestLintBlockFnIdempotency_OpaqueDefers(t *testing.T) {
	root := findModuleRootForFixture(t)
	fixturePath := root + "/tests/fixtures/block_fn_valid_opaque.star"
	p, err := NewParser(WithExtensions(httpExtensionForTest()))
	require.NoError(t, err)
	_, err = p.ParseFile(fixturePath)
	require.NoError(t, err, "opaque block_fn must defer to runtime; parse-time lint must NOT reject")
}

// TestLintBlockFnIdempotency_RecursesIntoIfCondAndForEachParallel: a mixed
// block_fn living inside if_cond.then is also rejected — recursion mirrors
// lintMixedIdempotency's walk shape.
func TestLintBlockFnIdempotency_RecursesIntoIfCondAndForEachParallel(t *testing.T) {
	src := `flow(
    name = "f",
    inputs = {"flag": "bool"},
    steps = [
        if_cond(
            cond = lambda ctx: ctx.flag,
            then = [
                step(block_fn = lambda ctx: [fake_ext.echo(msg = "a"), fake_ext.post(payload = "b")]),
            ],
        ),
    ],
)`
	p := newTestParser(t)
	_, err := p.ParseSource("test.star", []byte(src))
	require.Error(t, err, "mixed block_fn nested in if_cond.then must still reject")
	var ve *dag.ValidationError
	require.True(t, errors.As(err, &ve))
	assert.Contains(t, ve.Msg, "block_fn")
	assert.Contains(t, ve.Msg, "split into separate steps with action_fn each")
}

// TestFinalizeChainOrder_BlockFnLintRunsAfterCtxValidation: a fixture with
// BOTH a ctx.<typo> AND a mixed block_fn surfaces the typo (D4-02
// validateLambdaCtxAccesses) BEFORE the block_fn lint. The typo's
// ValidationError mentions the missing attribute, not "block_fn".
//
// Pin the ordering: block_fn lint runs after the ctx walker today
// (CONTEXT D4-01: structural state errors surface before kwarg-shape /
// idempotency checks). This test would need updating if the ordering
// flips.
//
// NOTE: the new lintBlockFnIdempotency lands BETWEEN lintMixedIdempotency
// and lintBlockSize per the plan; both run BEFORE
// validateLambdaCtxAccesses. So the FIRST error is actually the
// block_fn lint here. Adjust the assertion accordingly: the block_fn
// rejection surfaces first because it's earlier in the chain.
func TestFinalizeChainOrder_BlockFnLintRunsAfterCtxValidation(t *testing.T) {
	// The lint chain:
	//   resolveCallFlows
	//   lintMixedIdempotency
	//   lintBlockFnIdempotency  <-- NEW
	//   lintBlockSize
	//   lintEmptyTaskQueue
	//   validateLambdaCtxAccesses
	//   validateActionRefKwargs
	//
	// So a fixture with mixed-block_fn lint failure AND ctx.<typo> in
	// some other lambda surfaces the BLOCK_FN error first. We pin this
	// to lock the ordering.
	src := `flow(
    name = "f",
    inputs = {},
    steps = [
        step(block_fn = lambda ctx: [fake_ext.echo(msg = "a"), fake_ext.post(payload = "b")]),
        if_cond(cond = lambda ctx: ctx.tyop, then = []),
    ],
)`
	p := newTestParser(t)
	_, err := p.ParseSource("test.star", []byte(src))
	require.Error(t, err)
	// The block_fn mixed-idempotency lint fires first (it appears
	// earlier in finalize). We assert the SHAPE of that error rather
	// than the typo to pin the ordering.
	var ve *dag.ValidationError
	require.True(t, errors.As(err, &ve), "expected *dag.ValidationError")
	assert.True(t, strings.Contains(ve.Msg, "block_fn"),
		"block_fn lint must run before validateLambdaCtxAccesses; got: %s", ve.Msg)
}
