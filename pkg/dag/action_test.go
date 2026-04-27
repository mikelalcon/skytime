package dag

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
)

// Compile-time assertion: *ActionRef implements starlark.Value at file scope.
var _ starlark.Value = (*ActionRef)(nil)

func TestActionRef_Type(t *testing.T) {
	a := &ActionRef{Kind_: "github.create_issue"}
	assert.Equal(t, "ActionRef", a.Type())
}

func TestActionRef_String_ContainsKind(t *testing.T) {
	a := &ActionRef{Kind_: "github.create_issue"}
	assert.True(t, strings.Contains(a.String(), "github.create_issue"),
		"String() should contain the action kind: got %q", a.String())
}

func TestActionRef_TruthIsTrue(t *testing.T) {
	a := &ActionRef{Kind_: "x"}
	assert.Equal(t, starlark.True, a.Truth())
}

func TestActionRef_HashReturnsError(t *testing.T) {
	a := &ActionRef{Kind_: "x"}
	_, err := a.Hash()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not hashable")
}

func TestActionRef_FreezeCascade(t *testing.T) {
	// Build a Kwargs dict that contains a mutable List. After ActionRef.Freeze(),
	// both the dict (direct child) AND the inner list (transitive child) must
	// reject mutation. This proves the recursive freeze cascade.
	d := starlark.NewDict(1)
	lst := starlark.NewList([]starlark.Value{starlark.String("x")})
	require.NoError(t, d.SetKey(starlark.String("k"), lst))

	ar := &ActionRef{Kind_: "ext.op", Kwargs: d}
	ar.Freeze()

	// Direct dict mutation rejected
	err := d.SetKey(starlark.String("new"), starlark.String("v"))
	require.Error(t, err, "frozen dict must reject SetKey")
	assert.Contains(t, err.Error(), "frozen")

	// Cascade reached the inner list
	err = lst.Append(starlark.String("y"))
	require.Error(t, err, "frozen inner list must reject Append")
	assert.Contains(t, err.Error(), "frozen")
}

func TestActionRef_FreezeIdempotent(t *testing.T) {
	d := starlark.NewDict(0)
	ar := &ActionRef{Kind_: "ext.op", Kwargs: d}

	require.NotPanics(t, func() {
		ar.Freeze()
		ar.Freeze() // second call is a no-op
	})

	// Dict still frozen after the second call
	err := d.SetKey(starlark.String("k"), starlark.String("v"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "frozen")
}

func TestActionRef_FreezeNilKwargsDoesNotPanic(t *testing.T) {
	ar := &ActionRef{Kind_: "x", Kwargs: nil}
	require.NotPanics(t, func() { ar.Freeze() })
}

func TestActionRef_ActionKindReturnsKindField(t *testing.T) {
	a := &ActionRef{Kind_: "github.create_issue", CredentialID: "admin"}
	assert.Equal(t, "github.create_issue", a.ActionKind())
}
