package parser

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// =============================================================================
// Phase 07.2.1 Plan 02 Task 1: log.<level>(...) parser tests
//
// Accept paths exercise the four levels {info,warn,error,debug} × the four
// authored shapes {literal, interpolated, with attrs, empty, multi-line}.
// Reject paths exercise non-literal msg (string-literal-only contract, D-7.2.1-17)
// and malformed ${...} interpolation (delegated to the shared desugarer).
//
// All tests use the canonical newTestParser(t) + p.ParseSource(filename, src)
// pattern from builtins_test.go — there is NO newParserForTest, no p.ParseFile
// helper in test code.
// =============================================================================

func TestBuiltinLog_AcceptLiteral_AllLevels(t *testing.T) {
	cases := []struct{ level string }{{"info"}, {"warn"}, {"error"}, {"debug"}}
	for _, c := range cases {
		t.Run(c.level, func(t *testing.T) {
			p := newTestParser(t)
			src := []byte(fmt.Sprintf(`flow(name="x", inputs={}, steps=[log.%s("hello")])`, c.level))
			flows, err := p.ParseSource("test.star", src)
			require.NoError(t, err)
			require.Contains(t, flows, "x")
			body := flows["x"].Body
			require.Len(t, body, 1)
			ls, ok := body[0].(*dag.LogStep)
			require.True(t, ok, "expected *dag.LogStep, got %T", body[0])
			assert.Equal(t, c.level, ls.Level)
			assert.Equal(t, "hello", ls.Msg)
			assert.Nil(t, ls.MsgFn, "no interpolation present")
			assert.Empty(t, ls.AttrsLambdaID, "no attrs= passed")
		})
	}
}

func TestBuiltinLog_AcceptInterpolated(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={"y": "int"}, steps=[log.warn("v=${ctx.y}")])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)
	body := flows["x"].Body
	require.Len(t, body, 1)
	ls, ok := body[0].(*dag.LogStep)
	require.True(t, ok)
	assert.Equal(t, "warn", ls.Level)
	assert.Equal(t, "v=${ctx.y}", ls.Msg)
	require.NotNil(t, ls.MsgFn, "${ctx.y} should desugar to MsgFn")
	assert.NotEmpty(t, ls.MsgFn.ID)
}

func TestBuiltinLog_AcceptWithAttrs(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={}, steps=[log.error("oops", attrs=lambda ctx: {"key": "val"})])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)
	ls, ok := flows["x"].Body[0].(*dag.LogStep)
	require.True(t, ok)
	assert.Equal(t, "error", ls.Level)
	assert.NotEmpty(t, ls.AttrsLambdaID, "attrs lambda should be captured")
}

func TestBuiltinLog_AcceptEmptyMsg(t *testing.T) {
	// D-7.2.1-03: empty msg allowed.
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={}, steps=[log.debug("")])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)
	ls := flows["x"].Body[0].(*dag.LogStep)
	assert.Equal(t, "", ls.Msg)
}

func TestBuiltinLog_AcceptMultilineMsg(t *testing.T) {
	// D-7.2.1-04: multi-line msg allowed.
	p := newTestParser(t)
	src := []byte(`flow(name="x", inputs={}, steps=[log.info("""line one
line two""")])`)
	flows, err := p.ParseSource("test.star", src)
	require.NoError(t, err)
	ls := flows["x"].Body[0].(*dag.LogStep)
	assert.Contains(t, ls.Msg, "line one")
	assert.Contains(t, ls.Msg, "line two")
}

func TestBuiltinLog_RejectNonLiteralMsg(t *testing.T) {
	// D-7.2.1-17: msg must be a string literal — variable reference rejected.
	levels := []string{"info", "warn", "error", "debug"}
	for _, level := range levels {
		t.Run(level, func(t *testing.T) {
			p := newTestParser(t)
			src := []byte(fmt.Sprintf(`
MSG = "hello"
flow(name="x", inputs={}, steps=[log.%s(MSG)])
`, level))
			_, err := p.ParseSource("test.star", src)
			require.Error(t, err)
			var pe *dag.ParseError
			require.True(t, errors.As(err, &pe), "expected *dag.ParseError, got %T: %v", err, err)
			assert.Contains(t, pe.Msg, fmt.Sprintf("log.%s: msg must be a string literal", level))
		})
	}
}

func TestBuiltinLog_RejectMalformedInterp(t *testing.T) {
	cases := []struct{ name, src string }{
		{"empty_interp", `flow(name="x", inputs={}, steps=[log.info("${ctx.}")])`},
		{"unterminated_interp", `flow(name="x", inputs={}, steps=[log.info("${ctx.x")])`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := newTestParser(t)
			_, err := p.ParseSource("test.star", []byte(c.src))
			require.Error(t, err)
			var pe *dag.ParseError
			require.True(t, errors.As(err, &pe), "expected *dag.ParseError, got %T: %v", err, err)
			// Position attribution from the desugarer.
			assert.True(t, pe.Pos.IsValid(), "ParseError should carry a valid position")
		})
	}
}
