package parser

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// =============================================================================
// D-19 (positive): module-level def is allowed (Pitfall #5 closure)
// =============================================================================

func TestFreeVars_ModuleLevelDefAllowed(t *testing.T) {
	src := []byte(`def helper(x):
    return x * 2

flow(name="x", inputs={}, steps=[
    script(id="s", fn=lambda ctx: helper(ctx.v), output_alias="r"),
])`)
	p, _ := NewParser(WithExtensions(&fakeExtension{}))
	_, err := p.ParseSource("test.star", src)
	require.NoError(t, err,
		"Pitfall #5: module-level def referenced from a lambda must parse cleanly")
}

// =============================================================================
// D-19 (positive): module-level constant is allowed
// =============================================================================

func TestFreeVars_ModuleConstAllowed(t *testing.T) {
	src := []byte(`MAX = 10

flow(name="x", inputs={}, steps=[
    if_cond(cond=lambda ctx: ctx.v < MAX, then=[step(action=fake_ext.echo(msg="t"))]),
])`)
	p, _ := NewParser(WithExtensions(&fakeExtension{}))
	_, err := p.ParseSource("test.star", src)
	require.NoError(t, err,
		"D-19: module-level constants are valid free vars")
}

// =============================================================================
// D-19 (negative): nested-scope capture rejected with position-aware error
// =============================================================================

func TestFreeVars_NestedDefRejected(t *testing.T) {
	// `make_lambda()`'s body declares `counter` as a local, then returns
	// a lambda that closes over it. The captured closure violates D-19:
	// `counter` is bound inside the def body (not at column 1).
	src := []byte(`def make_lambda():
    counter = [0]
    return lambda ctx: counter.append(len(counter)) or counter[-1]

flow(name="bad", inputs={}, steps=[
    script(id="s", fn=make_lambda(), output_alias="v"),
])`)
	p, _ := NewParser(WithExtensions(&fakeExtension{}))
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	var pe *dag.ParseError
	require.True(t, errors.As(err, &pe))
	assert.Contains(t, pe.Error(), "lambda captures non-module-level variable",
		"D-19 violation must surface a clear error message")
	assert.Contains(t, pe.Error(), `"counter"`,
		"error must name the offending free variable")
}

// =============================================================================
// D-19 (negative): error position points at the binding (not the lambda)
// =============================================================================

func TestFreeVars_PositionPointsAtBinding(t *testing.T) {
	src := []byte(`def f():
    inner = 42
    return lambda ctx: ctx.x + inner

flow(name="x", inputs={}, steps=[script(id="s", fn=f(), output_alias="r")])`)
	p, _ := NewParser(WithExtensions(&fakeExtension{}))
	_, err := p.ParseSource("source.star", src)
	require.Error(t, err)

	var pe *dag.ParseError
	require.True(t, errors.As(err, &pe))
	require.True(t, pe.Pos.IsValid(), "error position must be valid")

	// `inner = 42` is on line 2 (1-based). The error position must point
	// at the binding line, not the lambda line (line 3).
	assert.Equal(t, int32(2), pe.Pos.Line,
		"D-19 error must point at the binding site, not the lambda site")
}

// =============================================================================
// D2-05 (mixed-idempotency reject) — Plan 02-01 Task 3 lintMixedIdempotency
// =============================================================================

// TestLinter_MixedIdempotency_Rejects builds a step(block=[idem, nonidem])
// from the fakeExtension's two ops and asserts the parser rejects it with
// a *dag.ValidationError whose Msg starts with the canonical D2-05 wording
// AND whose Pos.Line matches the step()'s line.
func TestLinter_MixedIdempotency_Rejects(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="mixed", inputs={}, steps=[
    step(block=[
        fake_ext.echo(msg="ok"),
        fake_ext.post(payload="x"),
    ]),
])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)

	var ve *dag.ValidationError
	require.True(t, errors.As(err, &ve), "expected *dag.ValidationError, got %T: %v", err, err)
	assert.True(t, strings.HasPrefix(ve.Msg, "cannot mix idempotent and non-idempotent operations in a block"),
		"Msg must START with the canonical D2-05 phrase, got: %s", ve.Msg)
	assert.Contains(t, ve.Msg, "fake_ext.echo")
	assert.Contains(t, ve.Msg, "fake_ext.post")
	require.True(t, ve.Pos.IsValid(), "error position must be valid")
	assert.Equal(t, int32(2), ve.Pos.Line, "error must point at the step() line")
	assert.Equal(t, "mixed", ve.Flow, "Flow attribution must match")
}

// TestLinter_AllIdempotentBlock_Accepts confirms a homogeneous all-idempotent
// block parses cleanly — the lint must not flag every block, only mixed ones.
func TestLinter_AllIdempotentBlock_Accepts(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="all-idem", inputs={}, steps=[
    step(block=[
        fake_ext.echo(msg="a"),
        fake_ext.echo(msg="b"),
        fake_ext.echo(msg="c"),
    ]),
])`)
	_, err := p.ParseSource("test.star", src)
	require.NoError(t, err, "homogeneous idempotent blocks must parse cleanly")
}

// TestLinter_SingleNonIdempotentBlock_Accepts confirms a 1-action block
// containing only a non-idempotent op is allowed (D2-06: splitting is the
// interpreter's job; a 1-action block is trivially homogeneous).
func TestLinter_SingleNonIdempotentBlock_Accepts(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="single-nonidem", inputs={}, steps=[
    step(block=[
        fake_ext.post(payload="only-one"),
    ]),
])`)
	_, err := p.ParseSource("test.star", src)
	require.NoError(t, err, "single-non-idempotent blocks are homogeneous and must pass")
}

// TestLinter_MixedIdempotency_NestedInIfCond verifies the lint walks
// recursively into IfCond.Then/Else and ForEachParallel.Steps. A mixed
// block deep inside a conditional is also rejected.
func TestLinter_MixedIdempotency_NestedInIfCond(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="nested", inputs={}, steps=[
    if_cond(
        cond = lambda ctx: True,
        then = [
            step(block=[
                fake_ext.echo(msg="ok"),
                fake_ext.post(payload="x"),
            ]),
        ],
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	var ve *dag.ValidationError
	require.True(t, errors.As(err, &ve), "expected *dag.ValidationError, got %T", err)
	assert.True(t, strings.HasPrefix(ve.Msg, "cannot mix idempotent and non-idempotent operations in a block"),
		"nested mixed block must surface the same canonical error")
}

// TestLinter_MixedIdempotency_NestedInForEachParallel — same recursion
// check for ForEachParallel.Steps.
func TestLinter_MixedIdempotency_NestedInForEachParallel(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="nested-loop", inputs={}, steps=[
    for_each_parallel(
        items = [1, 2, 3],
        item = "x",
        steps = [
            step(block=[
                fake_ext.echo(msg="ok"),
                fake_ext.post(payload="x"),
            ]),
        ],
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	var ve *dag.ValidationError
	require.True(t, errors.As(err, &ve))
	assert.True(t, strings.HasPrefix(ve.Msg, "cannot mix idempotent and non-idempotent operations in a block"))
}

// =============================================================================
// D2-07 (block-size cap) — Plan 02-01 Task 3 lintBlockSize
// =============================================================================

// TestLinter_BlockSizeCap_DefaultRejects51 builds a step(block=[51 idempotent
// echoes]) and asserts the default cap (50) rejects with the canonical
// D2-07 message. All 51 are idempotent so lintMixedIdempotency does not fire
// first (lintBlockSize runs after — order in finalize.go is: resolveCallFlows
// → lintMixedIdempotency → lintBlockSize → validateActionRefKwargs).
func TestLinter_BlockSizeCap_DefaultRejects51(t *testing.T) {
	p := newTestParser(t)
	src := []byte(buildBlockSrc(51))
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	var ve *dag.ValidationError
	require.True(t, errors.As(err, &ve), "expected *dag.ValidationError, got %T: %v", err, err)
	assert.Contains(t, ve.Msg, "block has 51 actions; maximum is 50")
}

// TestLinter_BlockSizeCap_CustomCap verifies WithMaxBlockSize(N) overrides
// the default; a 3-action block with cap=2 must fail with the cap reflected
// in the error message.
func TestLinter_BlockSizeCap_CustomCap(t *testing.T) {
	p, err := NewParser(WithExtensions(&fakeExtension{}), WithMaxBlockSize(2))
	require.NoError(t, err)
	src := []byte(buildBlockSrc(3))
	_, err = p.ParseSource("test.star", src)
	require.Error(t, err)
	var ve *dag.ValidationError
	require.True(t, errors.As(err, &ve))
	assert.Contains(t, ve.Msg, "block has 3 actions; maximum is 2")
}

// TestLinter_BlockSizeCap_AtLimit verifies the boundary: a block with exactly
// p.maxBlockSize actions (default 50) parses cleanly. The cap is a >, not a >=.
func TestLinter_BlockSizeCap_AtLimit(t *testing.T) {
	p := newTestParser(t)
	src := []byte(buildBlockSrc(50))
	_, err := p.ParseSource("test.star", src)
	require.NoError(t, err, "block of size = cap must pass")
}

// =============================================================================
// D3-19 (empty-string task_queue rejected) — Plan 03-01 Task 2
// =============================================================================

// TestLintEmptyTaskQueue_Flow asserts the parser rejects flow(task_queue="")
// with a position-aware ParseError. Empty-string is rejected at the BUILTIN
// level (before constructing dag.Flow) — the kwarg-presence detection
// distinguishes "kwarg supplied as empty" (rejected) from "kwarg omitted"
// (default empty, allowed).
func TestLintEmptyTaskQueue_Flow(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="bad", task_queue="", steps=[step(action=fake_ext.echo(msg="x"))])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	var pe *dag.ParseError
	require.True(t, errors.As(err, &pe), "expected *dag.ParseError, got %T: %v", err, err)
	assert.Contains(t, pe.Msg, "task_queue must be non-empty",
		"D3-19: empty task_queue must surface a clear error")
	require.True(t, pe.Pos.IsValid(), "error must carry a valid position")
}

// TestLintEmptyTaskQueue_Step asserts the same rejection on step(task_queue="").
func TestLintEmptyTaskQueue_Step(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", steps=[step(action=fake_ext.echo(msg="x"), task_queue="")])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err)
	var pe *dag.ParseError
	require.True(t, errors.As(err, &pe), "expected *dag.ParseError, got %T: %v", err, err)
	assert.Contains(t, pe.Msg, "task_queue must be non-empty",
		"D3-19: empty step task_queue must surface a clear error")
	require.True(t, pe.Pos.IsValid(), "error must carry a valid position")
}

// TestLintEmptyTaskQueue_LinterPass exercises the defense-in-depth lintEmptyTaskQueue
// pass directly: a parser session whose flows map already contains a
// directly-constructed *dag.Flow with TaskQueue=="" — i.e., bypassing the
// builtin path. The pass cannot distinguish "absent" from "explicitly empty"
// post-construction so it is documented as a stub; this test pins the
// stub's no-op behavior so future changes (e.g., adding a presence flag)
// surface as test breaks.
func TestLintEmptyTaskQueue_LinterPass(t *testing.T) {
	p := newTestParser(t)
	// Direct construction — no builtin involved.
	p.flows["direct"] = &dag.Flow{
		Name:      "direct",
		TaskQueue: "", // indistinguishable from "absent" post-construction
		Body:      []dag.Node{},
	}
	// lintEmptyTaskQueue is a no-op stub by design; assert no error.
	err := p.lintEmptyTaskQueue()
	require.NoError(t, err,
		"linter pass is a no-op stub for now (cannot distinguish absent vs explicitly empty post-construction)")
}

// buildBlockSrc constructs a flow with one step containing n idempotent
// echoes. Used by the block-size cap tests; written as concatenated strings
// rather than a loop with template indirection so the resulting .star is
// trivial to read in test failures.
func buildBlockSrc(n int) string {
	var sb strings.Builder
	sb.WriteString(`flow(name="x", inputs={}, steps=[step(block=[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(`fake_ext.echo(msg="m")`)
	}
	sb.WriteString(`])])`)
	return sb.String()
}
