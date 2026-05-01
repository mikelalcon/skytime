package dryrun

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
)

// fakeArgs is the kwargs-target struct fakeExt.doit advertises via
// KwargsType. Empty body — the mock never decodes into it; AlwaysOkDispatch
// preserves KwargsType so the activity's own DecodeKwargsFromDict path can
// still fire on bad inputs.
type fakeArgs struct{}

// fakeExt is a minimal Extension used by TestAlwaysOkDispatch. Implements
// the three Extension methods (Name, Initialize, Operations) and exposes a
// single op "doit" with a real Func that intentionally returns a panic-y
// sentinel — AlwaysOkDispatch must REPLACE the Func, not call it.
type fakeExt struct{}

func (fakeExt) Name() string { return "fake" }

func (fakeExt) Initialize(_ *starlark.Thread, _ []starlark.Tuple) (starlark.Value, error) {
	return starlark.None, nil
}

func (fakeExt) Operations() map[string]*extension.OperationSpec {
	return map[string]*extension.OperationSpec{
		"doit": {
			Name:       "doit",
			Idempotent: extension.Ptr(true),
			Func: func(_ context.Context, _ any, _ extension.Credential) (dag.OperationOutput, error) {
				panic("fakeExt.doit Func should NOT be called; AlwaysOkDispatch must replace it")
			},
			KwargsType: reflect.TypeOf(fakeArgs{}),
		},
	}
}

// TestAlwaysOkDispatch verifies the dispatch wraps each registered op
// preserving Name, Idempotent, KwargsType, and DefaultTimeout, and that
// the replacement Func returns (nil, nil) without panicking (proving
// fakeExt's real Func was not invoked).
func TestAlwaysOkDispatch(t *testing.T) {
	d := AlwaysOkDispatch([]extension.Extension{fakeExt{}})

	require.Contains(t, d, "fake.doit")

	spec := d["fake.doit"]
	require.Equal(t, "doit", spec.Name)
	require.NotNil(t, spec.Idempotent)
	require.True(t, *spec.Idempotent)
	require.NotNil(t, spec.KwargsType, "KwargsType must be preserved so schema checks still fire")
	require.NotNil(t, spec.Func, "Func must be replaced, not nilled")

	out, err := spec.Func(context.Background(), nil, nil)
	require.NoError(t, err)
	require.Nil(t, out, "AlwaysOkDispatch's Func returns nil typed output")
}

// TestAlwaysOkDispatch_MultipleExtensions ensures the dispatch keys are
// "<extName>.<opName>" matching ActionRef.Kind_ verbatim, and multiple
// extensions stack correctly without collision.
func TestAlwaysOkDispatch_MultipleExtensions(t *testing.T) {
	d := AlwaysOkDispatch([]extension.Extension{fakeExt{}, secondExt{}})
	require.Contains(t, d, "fake.doit")
	require.Contains(t, d, "second.act")
	require.Len(t, d, 2)
}

type secondExt struct{}

func (secondExt) Name() string { return "second" }

func (secondExt) Initialize(_ *starlark.Thread, _ []starlark.Tuple) (starlark.Value, error) {
	return starlark.None, nil
}

func (secondExt) Operations() map[string]*extension.OperationSpec {
	return map[string]*extension.OperationSpec{
		"act": {
			Name:       "act",
			Idempotent: extension.Ptr(false),
			Func: func(_ context.Context, _ any, _ extension.Credential) (dag.OperationOutput, error) {
				return nil, nil
			},
			KwargsType: reflect.TypeOf(fakeArgs{}),
		},
	}
}
