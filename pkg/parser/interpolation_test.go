package parser

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
	httpext "github.com/mikelalcon/skytime/pkg/extension/builtin/http"
)

// =============================================================================
// Task 1 — scanInterpolation state-machine tests (D4.1-04)
//
// The scanner walks already-unquoted string content tracking ${...}
// markers. State machine (RESEARCH §Pitfall 2):
//   - text: literal scanning; $$ → literal $; ${ → enter expr.
//   - expr: bracket-counting expression mode; tracks string-literal sub-mode.
//   - expr-str: inside a "..." or '...' literal in expr; honors \ escapes.
//
// Errors are *dag.ParseError with verbatim substrings for each failure
// mode (the messages are what consultants see in their .star errors).
// =============================================================================

// userPos is a fixed user-source position used to seed scanner errors.
// The scanner adjusts positions by counting newlines inside `raw` so the
// returned expr Pos reflects the offset of each ${ within the literal.
func userPos() syntax.Position {
	name := "user.star"
	return syntax.MakePosition(&name, 10, 5)
}

// TestScanInterpolation_NoMarkers: a string with no ${...} returns
// HasInterpolation=false. Parts may be empty (caller short-circuits and
// stores the literal string as-is).
func TestScanInterpolation_NoMarkers(t *testing.T) {
	got, err := scanInterpolation("just a plain string", userPos())
	require.NoError(t, err)
	require.False(t, got.HasInterpolation, "no markers → HasInterpolation must be false")
}

// TestScanInterpolation_SingleMarker: "before${ctx.x}after" → 3 parts in
// order (text, expr, text). HasInterpolation=true.
func TestScanInterpolation_SingleMarker(t *testing.T) {
	got, err := scanInterpolation("before${ctx.x}after", userPos())
	require.NoError(t, err)
	require.True(t, got.HasInterpolation)
	require.Len(t, got.Parts, 3)
	assert.Equal(t, "text", got.Parts[0].Kind)
	assert.Equal(t, "before", got.Parts[0].Text)
	assert.Equal(t, "expr", got.Parts[1].Kind)
	assert.Equal(t, "ctx.x", got.Parts[1].Expr)
	assert.Equal(t, "text", got.Parts[2].Kind)
	assert.Equal(t, "after", got.Parts[2].Text)
}

// TestScanInterpolation_MultipleMarkers: "a${ctx.x}b${ctx.y}c" → 5 parts
// in order.
func TestScanInterpolation_MultipleMarkers(t *testing.T) {
	got, err := scanInterpolation("a${ctx.x}b${ctx.y}c", userPos())
	require.NoError(t, err)
	require.True(t, got.HasInterpolation)
	require.Len(t, got.Parts, 5)
	assert.Equal(t, []string{"text", "expr", "text", "expr", "text"},
		[]string{got.Parts[0].Kind, got.Parts[1].Kind, got.Parts[2].Kind, got.Parts[3].Kind, got.Parts[4].Kind})
	assert.Equal(t, "a", got.Parts[0].Text)
	assert.Equal(t, "ctx.x", got.Parts[1].Expr)
	assert.Equal(t, "b", got.Parts[2].Text)
	assert.Equal(t, "ctx.y", got.Parts[3].Expr)
	assert.Equal(t, "c", got.Parts[4].Text)
}

// TestScanInterpolation_EscapeDoubleDollar: doubled $$ produces literal
// "${...}" — no interpolation, the entire string collapses to one text
// part with HasInterpolation=false.
func TestScanInterpolation_EscapeDoubleDollar(t *testing.T) {
	got, err := scanInterpolation("$${literal}", userPos())
	require.NoError(t, err)
	require.False(t, got.HasInterpolation, "$$ escape → HasInterpolation must be false")
	require.Len(t, got.Parts, 1)
	assert.Equal(t, "text", got.Parts[0].Kind)
	assert.Equal(t, "${literal}", got.Parts[0].Text)
}

// TestScanInterpolation_EmptyExprError: empty ${} is rejected with the
// substring "empty interpolation".
func TestScanInterpolation_EmptyExprError(t *testing.T) {
	_, err := scanInterpolation("${}", userPos())
	require.Error(t, err)
	var pe *dag.ParseError
	require.True(t, errors.As(err, &pe), "expected *dag.ParseError, got %T: %v", err, err)
	assert.True(t, strings.Contains(pe.Error(), "empty interpolation"),
		"error must contain 'empty interpolation', got: %v", pe.Error())
}

// TestScanInterpolation_UnterminatedError: a ${ with no matching } is
// rejected with the substring "unterminated interpolation".
func TestScanInterpolation_UnterminatedError(t *testing.T) {
	_, err := scanInterpolation("/repos/${ctx.repo", userPos())
	require.Error(t, err)
	var pe *dag.ParseError
	require.True(t, errors.As(err, &pe), "expected *dag.ParseError, got %T: %v", err, err)
	assert.True(t, strings.Contains(pe.Error(), "unterminated interpolation"),
		"error must contain 'unterminated interpolation', got: %v", pe.Error())
}

// TestScanInterpolation_MultilineError: a ${...} containing a newline is
// rejected with the substring "multi-line interpolation".
func TestScanInterpolation_MultilineError(t *testing.T) {
	_, err := scanInterpolation("/repos/${\nctx.repo}", userPos())
	require.Error(t, err)
	var pe *dag.ParseError
	require.True(t, errors.As(err, &pe), "expected *dag.ParseError, got %T: %v", err, err)
	assert.True(t, strings.Contains(pe.Error(), "multi-line interpolation"),
		"error must contain 'multi-line interpolation', got: %v", pe.Error())
}

// TestScanInterpolation_NestedBraces: "${ctx.foo({\"k\":1})}" — bracket-
// counting honors string-literal mode (the {} inside "k":1 is text, not a
// depth bump on the OUTER ${}).
func TestScanInterpolation_NestedBraces(t *testing.T) {
	got, err := scanInterpolation(`${ctx.foo({"k":1})}`, userPos())
	require.NoError(t, err)
	require.True(t, got.HasInterpolation)
	require.Len(t, got.Parts, 1)
	assert.Equal(t, "expr", got.Parts[0].Kind)
	assert.Equal(t, `ctx.foo({"k":1})`, got.Parts[0].Expr)
}

// TestScanInterpolation_StringInsideExpr: "${ctx.s.replace(\"}\", \"X\")}" —
// the } inside the string literal does NOT terminate the expression.
func TestScanInterpolation_StringInsideExpr(t *testing.T) {
	got, err := scanInterpolation(`${ctx.s.replace("}", "X")}`, userPos())
	require.NoError(t, err)
	require.True(t, got.HasInterpolation)
	require.Len(t, got.Parts, 1)
	assert.Equal(t, "expr", got.Parts[0].Kind)
	assert.Equal(t, `ctx.s.replace("}", "X")`, got.Parts[0].Expr)
}

// TestScanInterpolation_DollarSignAlone: a bare $ not followed by { is
// literal — no error, no interpolation.
func TestScanInterpolation_DollarSignAlone(t *testing.T) {
	got, err := scanInterpolation("price: $5", userPos())
	require.NoError(t, err)
	require.False(t, got.HasInterpolation)
	require.Len(t, got.Parts, 1)
	assert.Equal(t, "text", got.Parts[0].Kind)
	assert.Equal(t, "price: $5", got.Parts[0].Text)
}

// =============================================================================
// Task 2 — desugarInterpolation tests (D4.1-01..05, RESEARCH §Pattern 1+2)
//
// The desugarer turns a scanned template into a *CapturedLambda whose Fn
// is a real *starlark.Function. Pos points at the user's source (opening
// ${); BodyPos points at the synthetic-file location for D4-02 walks.
// =============================================================================

// TestDesugarInterpolation_NoMarkers: scanInterpolation reports no
// interpolation → desugarInterpolation returns (nil, nil) — caller
// stores the literal string unchanged.
func TestDesugarInterpolation_NoMarkers(t *testing.T) {
	p := newTestParser(t)
	cl, err := p.desugarInterpolation("plain string", userPos(), "")
	require.NoError(t, err)
	require.Nil(t, cl, "no markers → no CapturedLambda")
}

// TestDesugarInterpolation_BasicRoundtrip: "prefix${ctx.x}suffix" produces
// a *CapturedLambda whose Fn evaluates to "prefixVALsuffix" when given a
// struct {x: "VAL"}.
func TestDesugarInterpolation_BasicRoundtrip(t *testing.T) {
	p := newTestParser(t)
	openPos := makeUserPos(t, p, "TestDesugar.star", "x = \"prefix${ctx.x}suffix\"")
	cl, err := p.desugarInterpolation("prefix${ctx.x}suffix", openPos, "")
	require.NoError(t, err)
	require.NotNil(t, cl)
	require.NotNil(t, cl.Fn)

	// Evaluate the synthesized function with ctx = struct(x="VAL") and
	// assert the resulting Starlark string equals "prefixVALsuffix".
	thread := &starlark.Thread{Name: "test-eval"}
	ctx := starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"x": starlark.String("VAL"),
	})
	result, callErr := starlark.Call(thread, cl.Fn, starlark.Tuple{ctx}, nil)
	require.NoError(t, callErr)
	s, ok := result.(starlark.String)
	require.True(t, ok, "expected starlark.String, got %T", result)
	assert.Equal(t, "prefixVALsuffix", string(s))
}

// TestDesugarInterpolation_StableID: same input twice produces a
// CapturedLambda.ID matching D-18 format (8-hex-char hash + ":line:col").
func TestDesugarInterpolation_StableID(t *testing.T) {
	p := newTestParser(t)
	openPos := makeUserPos(t, p, "TestStableID.star", "x = \"prefix${ctx.x}suffix\"")
	cl, err := p.desugarInterpolation("prefix${ctx.x}suffix", openPos, "")
	require.NoError(t, err)
	require.NotNil(t, cl)
	// D-18 format: 8 hex chars + ":" + line + ":" + col
	matched, mErr := matchString(`^[0-9a-f]{8}:\d+:\d+$`, cl.ID)
	require.NoError(t, mErr)
	assert.True(t, matched, "ID %q must match D-18 format ^[0-9a-f]{8}:\\d+:\\d+$", cl.ID)
}

// TestDesugarInterpolation_PosAtUserSource: synthesized lambda's Pos
// points at the user's filename (NOT the synthetic name).
func TestDesugarInterpolation_PosAtUserSource(t *testing.T) {
	p := newTestParser(t)
	openPos := makeUserPos(t, p, "TestPosUser.star", "x = \"${ctx.x}\"")
	cl, err := p.desugarInterpolation("${ctx.x}", openPos, "")
	require.NoError(t, err)
	require.NotNil(t, cl)
	assert.Equal(t, openPos.Filename(), cl.Pos.Filename(),
		"Pos.Filename must be the user's filename, got %q", cl.Pos.Filename())
	assert.Equal(t, openPos.Line, cl.Pos.Line)
	assert.Equal(t, openPos.Col, cl.Pos.Col)
	assert.False(t, strings.Contains(cl.Pos.Filename(), "<interp:"),
		"Pos.Filename must not be a synthetic name, got %q", cl.Pos.Filename())
}

// TestDesugarInterpolation_BodyPosAtSynthetic: synthesized lambda's
// BodyPos.Filename begins with "<interp:" (the synthetic prefix);
// BodyPos.IsValid() returns true.
func TestDesugarInterpolation_BodyPosAtSynthetic(t *testing.T) {
	p := newTestParser(t)
	openPos := makeUserPos(t, p, "TestBodyPos.star", "x = \"${ctx.x}\"")
	cl, err := p.desugarInterpolation("${ctx.x}", openPos, "")
	require.NoError(t, err)
	require.NotNil(t, cl)
	assert.True(t, cl.BodyPos.IsValid(), "BodyPos must be valid for synthesized lambda")
	assert.True(t, strings.HasPrefix(cl.BodyPos.Filename(), "<interp:"),
		"BodyPos.Filename must begin with '<interp:', got %q", cl.BodyPos.Filename())
}

// TestDesugarInterpolation_StoredInLambdasMap: after desugar the captured
// lambda is registered under its ID in p.lambdas.
func TestDesugarInterpolation_StoredInLambdasMap(t *testing.T) {
	p := newTestParser(t)
	openPos := makeUserPos(t, p, "TestStored.star", "x = \"${ctx.x}\"")
	cl, err := p.desugarInterpolation("${ctx.x}", openPos, "")
	require.NoError(t, err)
	require.NotNil(t, cl)
	got, ok := p.Lambdas()[cl.ID]
	require.True(t, ok, "captured lambda must be registered under its ID")
	assert.Same(t, cl, got, "stored pointer must equal returned pointer")
}

// TestDesugarInterpolation_FileBytesCached: after desugar, the synthesized
// source bytes are cached under BodyPos.Filename() so D4-02's findCtxAccesses
// can re-parse them.
func TestDesugarInterpolation_FileBytesCached(t *testing.T) {
	p := newTestParser(t)
	openPos := makeUserPos(t, p, "TestCached.star", "x = \"prefix${ctx.x}suffix\"")
	cl, err := p.desugarInterpolation("prefix${ctx.x}suffix", openPos, "")
	require.NoError(t, err)
	require.NotNil(t, cl)
	src, ok := p.FileBytes()[cl.BodyPos.Filename()]
	require.True(t, ok, "synthetic file bytes must be cached")
	srcStr := string(src)
	// Source should contain "lambda ctx:" and "str(ctx.x)" and the literal pieces.
	assert.True(t, strings.Contains(srcStr, "lambda ctx:"),
		"synthesized source must contain 'lambda ctx:', got %q", srcStr)
	assert.True(t, strings.Contains(srcStr, "str(ctx.x)"),
		"synthesized source must contain 'str(ctx.x)' (unconditional D4.1-04 wrap), got %q", srcStr)
	assert.True(t, strings.Contains(srcStr, "prefix"),
		"synthesized source must contain literal 'prefix', got %q", srcStr)
	assert.True(t, strings.Contains(srcStr, "suffix"),
		"synthesized source must contain literal 'suffix', got %q", srcStr)
}

// TestDesugarInterpolation_StrWrapUnconditional: int → str via the
// unconditional str() wrap (D4.1-04). "x=${ctx.n}" with ctx.n=42 yields
// "x=42".
func TestDesugarInterpolation_StrWrapUnconditional(t *testing.T) {
	p := newTestParser(t)
	openPos := makeUserPos(t, p, "TestStrWrap.star", "x = \"x=${ctx.n}\"")
	cl, err := p.desugarInterpolation("x=${ctx.n}", openPos, "")
	require.NoError(t, err)
	require.NotNil(t, cl)
	thread := &starlark.Thread{Name: "test-eval"}
	ctx := starlarkstruct.FromStringDict(starlarkstruct.Default, starlark.StringDict{
		"n": starlark.MakeInt(42),
	})
	result, callErr := starlark.Call(thread, cl.Fn, starlark.Tuple{ctx}, nil)
	require.NoError(t, callErr, "int via str() wrap must not type-error")
	s, ok := result.(starlark.String)
	require.True(t, ok)
	assert.Equal(t, "x=42", string(s))
}

// TestDesugarInterpolation_InnerExprSyntaxErrorReports: invalid Starlark
// inside ${...} surfaces as *dag.ParseError attributed to the user's
// source position (NOT the synthetic file).
func TestDesugarInterpolation_InnerExprSyntaxErrorReports(t *testing.T) {
	p := newTestParser(t)
	openPos := makeUserPos(t, p, "TestExprErr.star", "x = \"${ctx..bad}\"")
	_, err := p.desugarInterpolation("${ctx..bad}", openPos, "")
	require.Error(t, err)
	var pe *dag.ParseError
	require.True(t, errors.As(err, &pe), "expected *dag.ParseError, got %T: %v", err, err)
	assert.False(t, strings.Contains(pe.Pos.Filename(), "<interp:"),
		"error position must point at user's source, got synthetic %q", pe.Pos.Filename())
	assert.Equal(t, openPos.Filename(), pe.Pos.Filename())
}

// =============================================================================
// Task 3 — BodyPos wiring + end-to-end typo rejection
// =============================================================================

// TestCtxWalk_RespectsBodyPos: when CapturedLambda.BodyPos is set, the
// validator (checkLambdaCtx) walks the BodyPos file's source and finds
// the synthesized lambda inside it. The negative half: a typo
// (${ctx.tyop}) where `tyop` is NOT in the state set MUST surface a
// *dag.ValidationError. If the walker were still using Pos for the
// re-parse, the user file (which does not contain `lambda`) would yield
// zero accesses and the typo would slip through.
func TestCtxWalk_RespectsBodyPos(t *testing.T) {
	p := newTestParser(t)
	userFilename := "user_no_lambda.star"
	userSrc := []byte("# user file with NO lambda body\n\n\n\n\nx = 1\n")
	p.fileBytes[userFilename] = userSrc

	userPosForLambda := syntax.MakePosition(&userFilename, 5, 10)
	cl, err := p.desugarInterpolation("${ctx.tyop}", userPosForLambda, "")
	require.NoError(t, err)
	require.NotNil(t, cl)
	require.True(t, cl.BodyPos.IsValid(), "synthesized lambda must populate BodyPos")

	// State only declares `repo` — `tyop` is undeclared. The walker MUST
	// walk the synthetic file (where ctx.tyop actually lives) and reject.
	flow := &dag.Flow{Name: "f"}
	state := newStateSchema()
	state.addUntyped("repo")
	walkErr := p.checkLambdaCtx(flow, cl.ID, state)
	require.Error(t, walkErr, "BodyPos walker must catch ctx.tyop against state {repo}")
	var ve *dag.ValidationError
	require.True(t, errors.As(walkErr, &ve), "expected *dag.ValidationError, got %T: %v", walkErr, walkErr)
	assert.Contains(t, ve.Msg, "tyop")

	// And the positive: when state HAS the attr, no error.
	p2 := newTestParser(t)
	p2.fileBytes[userFilename] = userSrc
	cl2, err := p2.desugarInterpolation("${ctx.repo}", userPosForLambda, "")
	require.NoError(t, err)
	state2 := newStateSchema()
	state2.addUntyped("repo")
	require.NoError(t, p2.checkLambdaCtx(flow, cl2.ID, state2),
		"BodyPos walker must validate ctx.repo against state {repo}")
}

// TestCtxWalk_FallsBackToPos: when BodyPos is zero (hand-written lambda),
// the walker uses Pos.Filename() — backward compatibility for existing
// captureLambda usage.
func TestCtxWalk_FallsBackToPos(t *testing.T) {
	// A normal hand-written lambda flow. The existing test corpus already
	// covers this end-to-end (TestFinalize_CtxAccess_Valid etc.); pin it
	// here as a unit on findCtxAccesses with BodyPos zero.
	p := newTestParser(t)
	src := []byte(`flow(name="ok", inputs={"repo_name":"string"}, steps=[
    script(id="s", fn=lambda ctx: {"out": ctx.repo_name}, output_alias="r"),
])`)
	_, err := p.ParseSource("ok.star", src)
	require.NoError(t, err)
	// One captured lambda — and its BodyPos must be zero (hand-written).
	require.Len(t, p.Lambdas(), 1)
	for _, cl := range p.Lambdas() {
		assert.False(t, cl.BodyPos.IsValid(),
			"hand-written lambda must have zero BodyPos (fallback-to-Pos contract)")
	}
}

// TestInterpolation_TypoRejected — END-TO-END test pinned by Plan 04.1-03:
// builtinStep now routes string action kwargs through desugarInterpolation,
// so `path = "/repos/${ctx.tyop}"` lands as a *StarlarkLambda inside the
// ActionRef.Kwargs Dict. The D4-02 walker (extended in 04.1-03 Task 2 to
// visit Step.Actions[i].Kwargs entries) re-parses the synthetic file via
// BodyPos and surfaces the typo as *dag.ValidationError citing the user's
// `${` position.
func TestInterpolation_TypoRejected(t *testing.T) {
	root := findModuleRootForFixture(t)
	fixturePath := root + "/tests/fixtures/interp_invalid_typo.star"
	p, err := NewParser(WithExtensions(httpExtensionForTest()))
	require.NoError(t, err)
	_, err = p.ParseFile(fixturePath)
	require.Error(t, err, "ctx.tyop must surface as ValidationError")
	var ve *dag.ValidationError
	require.True(t, errors.As(err, &ve), "expected *dag.ValidationError, got %T: %v", err, err)
	assert.Contains(t, ve.Msg, "tyop",
		"ValidationError must mention the typo'd attribute name")
}

// TestInterpolation_NoTypo — END-TO-END companion: an interp_valid_simple
// fixture parses without error and produces a flow whose Step.Actions[0]
// kwargs include a *StarlarkLambda for the interpolated `path` kwarg.
func TestInterpolation_NoTypo(t *testing.T) {
	root := findModuleRootForFixture(t)
	fixturePath := root + "/tests/fixtures/interp_valid_simple.star"
	p, err := NewParser(WithExtensions(httpExtensionForTest()))
	require.NoError(t, err)
	flows, err := p.ParseFile(fixturePath)
	require.NoError(t, err, "valid interpolation must parse cleanly")
	require.Contains(t, flows, "interp_valid_simple")
	step, ok := flows["interp_valid_simple"].Body[0].(*dag.Step)
	require.True(t, ok)
	require.Len(t, step.Actions, 1)
	v, _, _ := step.Actions[0].Kwargs.Get(starlark.String("path"))
	require.NotNil(t, v)
	_, isLambda := dag.UnwrapStarlarkLambda(v)
	assert.True(t, isLambda, "path kwarg with ${ctx.repo} must round-trip as *StarlarkLambda, got %T", v)
}

// =============================================================================
// helpers
// =============================================================================

// makeUserPos primes Parser.fileBytes for a synthetic test filename and
// returns a syntax.Position for use as openPos. Real parser flows write
// fileBytes during ParseSource; tests need the same setup so
// captureLambdaAtPosition's content-hash lookup succeeds.
func makeUserPos(t *testing.T, p *Parser, filename, src string) syntax.Position {
	t.Helper()
	p.fileBytes[filename] = []byte(src)
	return syntax.MakePosition(&filename, 1, 5)
}

// matchString is a tiny regex helper used by the StableID test.
func matchString(pattern, s string) (bool, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false, err
	}
	return re.MatchString(s), nil
}

// findModuleRootForFixture walks up from CWD looking for go.mod so the
// interpolation e2e tests can reach the tests/fixtures/ corpus regardless
// of the package's working directory. Mirrors tests/differential_test.go's
// findModuleRootCLI but local to pkg/parser to avoid reaching into the
// firewall_test package.
func findModuleRootForFixture(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	for dir := wd; dir != "/" && dir != ""; dir = filepath.Dir(dir) {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
	}
	t.Fatal("could not find go.mod walking up from " + wd)
	return ""
}

// httpExtensionForTest returns the baked-in http extension for fixture
// loading. Imported via extension.Extension so the parser sees the same
// shape it would in production cmd/skytime wiring.
func httpExtensionForTest() extension.Extension {
	return httpext.New()
}
