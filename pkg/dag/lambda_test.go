package dag

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// idRegex matches the D-18 ID format: sha256(fileBytes)[:4] hex (8 chars) +
// ":" + line + ":" + col.
var idRegex = regexp.MustCompile(`^[a-f0-9]{8}:\d+:\d+$`)

func makePos(t *testing.T, file string, line, col int32) syntax.Position {
	t.Helper()
	return syntax.MakePosition(&file, line, col)
}

func TestComputeLambdaID_FormatMatchesRegex(t *testing.T) {
	fileBytes := []byte("# anything\n")
	id := ComputeLambdaID(fileBytes, makePos(t, "f.star", 5, 10))
	assert.True(t, idRegex.MatchString(id), "id %q does not match %s", id, idRegex)
}

func TestComputeLambdaID_Deterministic(t *testing.T) {
	fileBytes := []byte("flow(name='x', steps=[])\n")
	pos := makePos(t, "f.star", 1, 1)

	id1 := ComputeLambdaID(fileBytes, pos)
	id2 := ComputeLambdaID(fileBytes, pos)
	assert.Equal(t, id1, id2, "same fileBytes + pos must produce same ID across calls")
}

func TestComputeLambdaID_ContentSensitive(t *testing.T) {
	// Different fileBytes (one byte changed) must change the ID prefix.
	pos := makePos(t, "f.star", 5, 10)
	id1 := ComputeLambdaID([]byte("a"), pos)
	id2 := ComputeLambdaID([]byte("b"), pos)
	assert.NotEqual(t, id1, id2, "different content → different ID")
}

func TestComputeLambdaID_PositionSensitive(t *testing.T) {
	// Same content, different position → different IDs (line/col suffix changes).
	fileBytes := []byte("flow(name='x', steps=[])\n")
	id1 := ComputeLambdaID(fileBytes, makePos(t, "f.star", 1, 1))
	id2 := ComputeLambdaID(fileBytes, makePos(t, "f.star", 2, 1))
	assert.NotEqual(t, id1, id2)
}

// TestCapturedLambda_HasBodyPos — RESEARCH §Pattern 2: synthesized lambdas
// (D4.1-01 interpolation desugarer) need an independently settable position
// for the lambda body so the D4-02 ctx-walker can find the body in the
// re-parsed source while errors keep pointing at the user's `${`.
func TestCapturedLambda_HasBodyPos(t *testing.T) {
	posA := makePos(t, "user.star", 10, 5)
	posB := makePos(t, "<interp:abc>", 1, 1)
	cl := &CapturedLambda{Pos: posA, BodyPos: posB}
	require.NotEqual(t, cl.Pos.Filename(), cl.BodyPos.Filename(),
		"BodyPos must be independently settable from Pos for synthesized lambdas")
	assert.Equal(t, "user.star", cl.Pos.Filename())
	assert.Equal(t, "<interp:abc>", cl.BodyPos.Filename())
}

// TestCapturedLambda_BodyPosZeroValueDocumented — the zero value
// (syntax.Position{}) is the documented sentinel for "no body-pos override
// — fall back to Pos for AST walks". Callers detect via !BodyPos.IsValid().
func TestCapturedLambda_BodyPosZeroValueDocumented(t *testing.T) {
	cl := &CapturedLambda{}
	assert.False(t, cl.BodyPos.IsValid(),
		"zero-value BodyPos must be invalid; callers fall back to Pos for AST walks")
	zero := syntax.Position{}
	assert.False(t, zero.IsValid(), "syntax.Position{} sentinel must be invalid")
}

// TestStarlarkLambda_TypeAndString — wrapper exposes Starlark Type tag and a
// debug-friendly String() containing the captured ID.
func TestStarlarkLambda_TypeAndString(t *testing.T) {
	cl := &CapturedLambda{ID: "abc12345:7:3"}
	sl := NewStarlarkLambda(cl)
	require.NotNil(t, sl)
	assert.Equal(t, "CapturedLambda", sl.Type())
	assert.Equal(t, "CapturedLambda(abc12345:7:3)", sl.String())
}

// TestStarlarkLambda_Truth — every captured lambda is truthy. The method
// exists only to satisfy starlark.Value; it is not exercised by user flows.
func TestStarlarkLambda_Truth(t *testing.T) {
	cl := &CapturedLambda{ID: "x"}
	assert.Equal(t, starlark.True, NewStarlarkLambda(cl).Truth())
}

// TestStarlarkLambda_NotHashable — wrapper refuses Hash() like ActionRef
// does. Captured lambdas have no canonical equality across reparses
// (file-content-hash IDs change on cosmetic edits).
func TestStarlarkLambda_NotHashable(t *testing.T) {
	sl := NewStarlarkLambda(&CapturedLambda{ID: "x"})
	_, err := sl.Hash()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not hashable")
}

// TestStarlarkLambda_FreezeIsNoop — Freeze can be called repeatedly without
// panic and without mutating observable state. CapturedLambda is treated as
// immutable after construction.
func TestStarlarkLambda_FreezeIsNoop(t *testing.T) {
	cl := &CapturedLambda{ID: "x"}
	sl := NewStarlarkLambda(cl)
	require.NotPanics(t, func() {
		sl.Freeze()
		sl.Freeze()
	})
	assert.Equal(t, "x", sl.Captured.ID, "Freeze must not mutate the captured pointer")
}

// TestStarlarkLambda_FitsInDict — load-bearing case proving the wrapper can
// ride inside *starlark.Dict.SetKey(...) without rejection. The
// interpreter's resolveKwargs (D4.1-14) round-trips lambda-valued kwargs
// through this exact path.
func TestStarlarkLambda_FitsInDict(t *testing.T) {
	cl := &CapturedLambda{ID: "abc:1:1"}
	sl := NewStarlarkLambda(cl)
	d := starlark.NewDict(1)
	require.NoError(t, d.SetKey(starlark.String("k"), sl))
	got, found, err := d.Get(starlark.String("k"))
	require.NoError(t, err)
	require.True(t, found)
	unwrapped, ok := UnwrapStarlarkLambda(got)
	require.True(t, ok, "expected UnwrapStarlarkLambda to succeed")
	require.Same(t, cl, unwrapped, "expected same pointer round-trip")
}

// TestStarlarkLambda_Unwrap — UnwrapStarlarkLambda is the typed accessor that
// the interpreter uses to detect lambda-valued kwargs inside ActionRef.Kwargs.
func TestStarlarkLambda_Unwrap(t *testing.T) {
	cl := &CapturedLambda{ID: "round-trip"}
	sl := NewStarlarkLambda(cl)
	got, ok := UnwrapStarlarkLambda(sl)
	require.True(t, ok)
	require.Same(t, cl, got)

	// Non-wrapper Starlark values must NOT unwrap successfully.
	_, ok = UnwrapStarlarkLambda(starlark.String("not a lambda"))
	assert.False(t, ok)

	// Nil safety — passing nil still produces (nil, false), not a panic.
	require.NotPanics(t, func() {
		_, ok := UnwrapStarlarkLambda(nil)
		assert.False(t, ok)
	})

	// NewStarlarkLambda(nil) is a defensive no-op returning a typed nil; the
	// caller can still safely chain through UnwrapStarlarkLambda.
	require.Nil(t, NewStarlarkLambda(nil))
}

func TestCapturedLambda_ConstructsCleanly_RealStarlarkFunction(t *testing.T) {
	// Sanity check: exec a tiny Starlark snippet to obtain a real *starlark.Function,
	// then build a CapturedLambda around it. This catches API drift in
	// go.starlark.net (e.g., if Function.Position() changes shape).
	src := `
def _make():
    return lambda x: x + 1

f = _make()
`
	thread := &starlark.Thread{Name: "lambda_test"}
	globals, err := starlark.ExecFileOptions(syntax.LegacyFileOptions(), thread, "snippet.star", src, nil)
	require.NoError(t, err)

	val, ok := globals["f"]
	require.True(t, ok, "global f must exist")
	fn, ok := val.(*starlark.Function)
	require.True(t, ok, "f must be a *starlark.Function, got %T", val)

	pos := fn.Position()
	id := ComputeLambdaID([]byte(src), pos)
	require.True(t, idRegex.MatchString(id), "id %q does not match %s", id, idRegex)

	// FreeVars empty for this snippet (no captures) — but the StringDict
	// constructs cleanly.
	cl := &CapturedLambda{
		ID:       id,
		Fn:       fn,
		Pos:      pos,
		FreeVars: starlark.StringDict{},
	}
	assert.Equal(t, id, cl.ID)
	assert.NotNil(t, cl.Fn)
	assert.True(t, cl.Pos.IsValid())
}
