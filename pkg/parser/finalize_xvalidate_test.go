package parser

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
)

// xvalArgs declares one required kwarg ("name"). xvalExt below registers
// "xv.do" with this KwargsType so cross-validate can rediscover the
// missing required field.
type xvalArgs struct {
	Name string `star:"name,required"`
}

// xvalExt is a minimal extension used only by the cross-validate test. It
// registers a single operation `xv.do` whose schema demands `name`. The
// test bypasses the per-call extension factory by hand-building an
// ActionRef with empty Kwargs — proving validateActionRefKwargs catches
// the schema mismatch at finalize time.
type xvalExt struct{}

func (xvalExt) Name() string { return "xv" }

func (xvalExt) Initialize(_ *starlark.Thread, _ []starlark.Tuple) (starlark.Value, error) {
	return starlark.None, nil
}

func (xvalExt) Operations() map[string]*extension.OperationSpec {
	return map[string]*extension.OperationSpec{
		"do": {
			Name:       "do",
			Idempotent: extension.Ptr(true),
			Func: func(_ context.Context, _ any, _ extension.Credential) (dag.OperationOutput, error) {
				return nil, nil
			},
			KwargsType: reflect.TypeOf(xvalArgs{}),
		},
	}
}

// TestFinalize_KwargCrossValidate proves validateActionRefKwargs catches
// schema mismatches when an ActionRef is hand-built (test fixtures, future
// programmatic callers) bypassing the per-call extension factory.
//
// The test directly inserts a *dag.Flow into p.flows with an ActionRef
// whose Kwargs is an empty (frozen) *starlark.Dict — missing the required
// "name" kwarg. validateActionRefKwargs must surface a *dag.ValidationError
// with Action = "xv.do".
func TestFinalize_KwargCrossValidate(t *testing.T) {
	p, err := NewParser(WithExtensions(xvalExt{}))
	require.NoError(t, err)

	// Empty Kwargs Dict — missing the required "name" field.
	emptyDict := starlark.NewDict(0)
	emptyDict.Freeze()
	ar := &dag.ActionRef{Kind_: "xv.do", Kwargs: emptyDict}
	p.flows["bad"] = &dag.Flow{
		Name: "bad",
		Body: []dag.Node{&dag.Step{Actions: []*dag.ActionRef{ar}}},
	}

	err = p.validateActionRefKwargs()
	require.Error(t, err)

	var ve *dag.ValidationError
	require.True(t, errors.As(err, &ve), "expected *dag.ValidationError, got %T: %v", err, err)
	require.Equal(t, "bad", ve.Flow)
	require.Equal(t, "xv.do", ve.Action)
	require.Contains(t, ve.Msg, "kwarg cross-validate")
}

// TestFinalize_KwargCrossValidate_NoFalsePositive proves a well-formed
// flow (kwargs satisfy the schema) passes the cross-validate without
// surfacing a false positive. Counterpart to the negative test above.
func TestFinalize_KwargCrossValidate_NoFalsePositive(t *testing.T) {
	p, err := NewParser(WithExtensions(xvalExt{}))
	require.NoError(t, err)

	goodDict := starlark.NewDict(1)
	require.NoError(t, goodDict.SetKey(starlark.String("name"), starlark.String("widget")))
	goodDict.Freeze()
	ar := &dag.ActionRef{Kind_: "xv.do", Kwargs: goodDict}
	p.flows["ok"] = &dag.Flow{
		Name: "ok",
		Body: []dag.Node{&dag.Step{Actions: []*dag.ActionRef{ar}}},
	}

	require.NoError(t, p.validateActionRefKwargs())
}
