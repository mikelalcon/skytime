// Package parser — interpolation_collision_test.go
//
// Regression tests for quick 260512-w7c: multi-${ctx.expr}-kwarg lambda
// ID collision. Each test FAILS deterministically when the disambiguator
// threading is reverted (verify locally by stashing the Task 1 fix and
// re-running `go test -run TestInterpolation_MultiKwarg`).
//
// Bug shape (pre-fix): desugarInterpolation used `ar.Pos` (the parent
// ActionRef's call position) as the sole input to D-18 lambda ID
// computation. For two `${...}` kwargs on the SAME factory call, ar.Pos
// is identical → both synthesized lambdas hashed to the SAME ID →
// p.lambdas[id] = captured was last-wins → both kwargs pointed at the
// second lambda's body → both evaluated to the SECOND kwarg's value at
// workflow-resolve time. Cf. examples/http-github-webhook/public_repo_check.star
// which calls gh.list_open_issues(owner="${ctx.rp.owner}", repo="${ctx.rp.repo}").
//
// Fix: the kwargs-desugarer (desugarActionRefKwargs) threads the kwarg
// KEY through desugarInterpolation → captureLambdaAtPosition as a
// disambiguator that gets appended to the base D-18 ID. Distinct keys
// → distinct IDs → p.lambdas stores both.
package parser

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"github.com/mikelalcon/skytime/pkg/bridge"
	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
)

// =============================================================================
// multiKwargExtension — test helper specifically for the collision test
//
// The existing fakeExtension in builtins_test.go exposes echo(msg=...) and
// post(payload=...), each with rigid single-kwarg signatures. The
// collision regression needs an op accepting MULTIPLE kwargs on a single
// factory call. multiKwargExtension provides exactly one op, `op`, that
// stores whatever kwargs it receives into the *dag.ActionRef.Kwargs dict
// — mirroring the github extension's newOpBuiltin shape.
// =============================================================================

type multiKwargExtension struct{}

func (*multiKwargExtension) Name() string { return "fake" }

func (*multiKwargExtension) Initialize(thread *starlark.Thread, kwargs []starlark.Tuple) (starlark.Value, error) {
	opFn := starlark.NewBuiltin("op", func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kw []starlark.Tuple) (starlark.Value, error) {
		// Mirrors examples/http-github-webhook/extensions/github/github.go::newOpBuiltin:
		// pack kwargs into a starlark.Dict, freeze it, return *dag.ActionRef
		// with Pos = caller's .star call-site (used as openPos for the
		// kwargs-desugarer).
		outDict := starlark.NewDict(len(kw))
		for _, pair := range kw {
			if err := outDict.SetKey(pair[0], pair[1]); err != nil {
				return nil, fmt.Errorf("fake.op: bad kwarg: %w", err)
			}
		}
		outDict.Freeze()
		return &dag.ActionRef{
			Pos:    callerPosition(thread),
			Kind_:  "fake.op",
			Kwargs: outDict,
		}, nil
	})
	return &starlarkstruct.Module{
		Name: "fake",
		Members: starlark.StringDict{
			"op": opFn,
		},
	}, nil
}

// Operations declares `op` with KwargsType = struct{A, B string}. The
// per-call factory does NOT enforce the schema (it accepts arbitrary
// kwargs and stuffs them into a Dict), but the finalize pass
// (validateActionRefKwargs) cross-validates against this type. The
// schema package's D4.1-05 *StarlarkLambda short-circuit lets
// lambda-wrapped values land cleanly against string-tagged fields.
func (*multiKwargExtension) Operations() map[string]*extension.OperationSpec {
	return map[string]*extension.OperationSpec{
		"op": {
			Name:       "op",
			Idempotent: extension.Ptr(true),
			Func: func(ctx context.Context, args any, cred extension.Credential) (dag.OperationOutput, error) {
				return nil, nil
			},
			KwargsType: reflect.TypeOf(struct {
				A string `star:"a,required"`
				B string `star:"b,required"`
			}{}),
		},
	}
}

// =============================================================================
// TestInterpolation_MultiKwarg_DistinctLambdaIDs
// =============================================================================

// TestInterpolation_MultiKwarg_DistinctLambdaIDs is the regression
// pinpoint for quick 260512-w7c. It parses a flow with two ${...}
// kwargs on a single fake.op factory call and asserts the two synthesized
// CapturedLambda.IDs are NOT equal.
//
// Pre-fix behavior: the two IDs were equal — derived only from
// ar.Pos — and p.lambdas[id] = captured silently overwrote the first
// with the second, so both kwargs pointed at the second lambda body at
// runtime. The Task 1 fix threads the kwarg KEY ("a"/"b" here) as a
// disambiguator that gets appended to the base D-18 ID, yielding
// distinct entries in p.lambdas.
//
// If Task 1's threading is reverted, this test FAILS deterministically
// at the assert.NotEqual call below.
func TestInterpolation_MultiKwarg_DistinctLambdaIDs(t *testing.T) {
	const src = `flow(
    name = "t",
    inputs = {"x": "dict"},
    steps = [
        step(action = fake.op(a = "${ctx.x.foo}", b = "${ctx.x.bar}")),
    ],
)
`
	p, err := NewParser(WithExtensions(&multiKwargExtension{}))
	require.NoError(t, err)

	flows, err := p.ParseSource("t.star", []byte(src))
	require.NoError(t, err)
	require.Contains(t, flows, "t")
	f := flows["t"]
	require.NotNil(t, f)

	// Walk to the first Step and its single ActionRef.
	require.Len(t, f.Body, 1)
	firstStep, ok := f.Body[0].(*dag.Step)
	require.True(t, ok, "first body node should be *dag.Step, got %T", f.Body[0])
	require.Len(t, firstStep.Actions, 1)
	ar := firstStep.Actions[0]
	require.NotNil(t, ar)
	require.NotNil(t, ar.Kwargs)

	// Extract the a and b kwargs as *CapturedLambda via the
	// StarlarkLambda unwrap helper.
	getLambda := func(key string) *dag.CapturedLambda {
		t.Helper()
		v, found, err := ar.Kwargs.Get(starlark.String(key))
		require.NoError(t, err)
		require.True(t, found, "kwarg %q not present in ar.Kwargs", key)
		cl, ok := dag.UnwrapStarlarkLambda(v)
		require.True(t, ok, "kwarg %q should be a *StarlarkLambda, got %T", key, v)
		require.NotNil(t, cl, "kwarg %q captured lambda is nil", key)
		return cl
	}
	a := getLambda("a")
	b := getLambda("b")

	// Core invariant: the two synthesized lambdas MUST have distinct
	// IDs. Pre-fix they collided on the same `ar.Pos`-derived hash.
	assert.NotEqual(t, a.ID, b.ID,
		"lambda IDs for kwargs `a` and `b` collide: a=%q b=%q — disambiguator threading not applied?",
		a.ID, b.ID)

	// Belt + suspenders: both IDs MUST be registered in p.lambdas. If
	// the parser's storage map last-wins-overwrote one of them, the
	// missing entry surfaces clearly here.
	lambdas := p.Lambdas()
	_, aRegistered := lambdas[a.ID]
	_, bRegistered := lambdas[b.ID]
	assert.True(t, aRegistered, "lambda `a` (ID=%q) must be registered in p.lambdas", a.ID)
	assert.True(t, bRegistered, "lambda `b` (ID=%q) must be registered in p.lambdas", b.ID)
}

// =============================================================================
// TestInterpolation_MultiKwarg_EvaluateToDistinctValues
// =============================================================================

// TestInterpolation_MultiKwarg_EvaluateToDistinctValues drives the
// regression one layer deeper: not only must the two lambda IDs be
// distinct (proven by TestInterpolation_MultiKwarg_DistinctLambdaIDs),
// but each captured lambda MUST evaluate against runtime state and
// produce its OWN value — not the OTHER kwarg's value. This is the
// user-visible failure mode: pre-fix, gh.list_open_issues with
// owner=${ctx.rp.owner}+repo=${ctx.rp.repo} would resolve both kwargs
// to ctx.rp.repo (last-wins on p.lambdas), so the live request hit
// GET /repos/Hello-World/Hello-World/issues and 404'd.
//
// Here we parse a flow with two ${ctx.x.foo}/${ctx.x.bar} kwargs and
// evaluate each via bridge.CallLambda against state {x:{foo:"AAA",bar:"BBB"}}.
// `a` MUST evaluate to "AAA", `b` MUST evaluate to "BBB", and the two
// results MUST differ. If the Task 1 fix is reverted, the second
// captured lambda overwrites the first in p.lambdas and both `a` and
// `b` resolve to "BBB" — the assert.Equal on `a` fires.
func TestInterpolation_MultiKwarg_EvaluateToDistinctValues(t *testing.T) {
	const src = `flow(
    name = "t",
    inputs = {"x": "dict"},
    steps = [
        step(action = fake.op(a = "${ctx.x.foo}", b = "${ctx.x.bar}")),
    ],
)
`
	p, err := NewParser(WithExtensions(&multiKwargExtension{}))
	require.NoError(t, err)

	flows, err := p.ParseSource("t.star", []byte(src))
	require.NoError(t, err)
	f := flows["t"]
	require.NotNil(t, f)

	require.Len(t, f.Body, 1)
	firstStep, ok := f.Body[0].(*dag.Step)
	require.True(t, ok, "first body node should be *dag.Step")
	require.Len(t, firstStep.Actions, 1)
	ar := firstStep.Actions[0]

	getLambda := func(key string) *dag.CapturedLambda {
		t.Helper()
		v, found, err := ar.Kwargs.Get(starlark.String(key))
		require.NoError(t, err)
		require.True(t, found, "kwarg %q not present", key)
		cl, ok := dag.UnwrapStarlarkLambda(v)
		require.True(t, ok, "kwarg %q should be *StarlarkLambda, got %T", key, v)
		return cl
	}
	a := getLambda("a")
	b := getLambda("b")

	ctx := context.Background()
	state := map[string]any{
		"x": map[string]any{
			"foo": "AAA",
			"bar": "BBB",
		},
	}

	aResult, err := bridge.CallLambda(ctx, a, state, bridge.CallOptions{})
	require.NoError(t, err, "bridge.CallLambda(a) must succeed")
	bResult, err := bridge.CallLambda(ctx, b, state, bridge.CallOptions{})
	require.NoError(t, err, "bridge.CallLambda(b) must succeed")

	assert.Equal(t, starlark.String("AAA"), aResult,
		"a-kwarg lambda should evaluate to ctx.x.foo='AAA'; got %v (bug: both kwargs collapse to last-captured lambda's body)", aResult)
	assert.Equal(t, starlark.String("BBB"), bResult,
		"b-kwarg lambda should evaluate to ctx.x.bar='BBB'; got %v", bResult)

	// Belt + suspenders: the two results MUST differ. If they're
	// equal, the disambiguator fix has regressed — both lambdas point
	// at the same body and resolve to the same value.
	assert.NotEqual(t, aResult, bResult,
		"a and b lambdas resolved to identical values (%v) — quick 260512-w7c regression?", aResult)
}
