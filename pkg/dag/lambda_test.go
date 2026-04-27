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
