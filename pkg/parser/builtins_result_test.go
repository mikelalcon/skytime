package parser

// Wave 0 RED scaffolding for D4.2-15 result() builtin (plan 02 fills).
// These tests exercise the parser-time surface of the new `result()`
// builtin used inside expression-mode if_cond branches:
//
//   - kwarg-only signature (`result(value={...})`)
//   - dict keys must be string literals at parse time (computed keys reject)
//   - cannot appear outside an if_cond whose output_alias is set
//
// Until plan 02 ships builtinResult these tests fail with "result is not
// in parse-time globals" or analogous wording — that RED state is the
// signal each test will turn GREEN against once plan 02 lands.

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestResult_KwargOnlyValueDictLiteral: result(value={"x": 1}) inside an
// expression-mode if_cond branch parses cleanly. RED until plan 02
// registers the result builtin.
func TestResult_KwargOnlyValueDictLiteral(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={}, steps=[
    if_cond(output_alias="X", cond=lambda ctx: True,
        then=[result(value={"x": 1})],
        else_=[fail("nope")],
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	require.NoError(t, err, "RED until plan 02 ships builtinResult: %v", err)
}

// TestResult_StringLiteralKeysOnly: result(value={ctx.k: 1}) — computed
// key — must reject at parse time with the "result.value: dict keys must
// be string literals" message. RED until plan 02.
func TestResult_StringLiteralKeysOnly(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={"k": "string"}, steps=[
    if_cond(output_alias="X", cond=lambda ctx: True,
        then=[result(value={ctx.k: 1})],
        else_=[fail("nope")],
    ),
])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err, "RED until plan 02 ships builtinResult")
	require.Contains(t, err.Error(), "result.value: dict keys must be string literals")
}

// TestResult_RejectedOutsideExpressionMode: a top-level result() (or
// inside a procedural-mode if_cond, i.e., output_alias unset) is rejected
// at parse time. The error must mention `result` and the `output_alias`
// requirement so users know the fix. RED until plan 02.
func TestResult_RejectedOutsideExpressionMode(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(name="f", inputs={}, steps=[result(value={"x": 1})])`)
	_, err := p.ParseSource("test.star", src)
	require.Error(t, err, "RED until plan 02 ships builtinResult")
	require.Contains(t, err.Error(), "result")
	require.Contains(t, err.Error(), "output_alias")
}
