package testing

import (
	"context"
	"errors"
	"fmt"

	"go.starlark.net/starlark"

	sdkactivity "go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"

	"github.com/stretchr/testify/mock"

	"github.com/mikelalcon/skytime/pkg/bridge"
	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
)

// noMockMessageFor formats the verbatim D5-B2 message:
//
//	no mock for gh.delete at users.star:14:5 (step "fetch user")
//
// stepName may be empty (block batches with no name); Go's %q renders
// an empty string as `""`. ref.Pos.String() emits "<filename>:<line>:<col>".
func noMockMessageFor(ref *dag.ActionRef, stepName string) string {
	return fmt.Sprintf("no mock for %s at %s (step %q)", ref.Kind_, ref.Pos.String(), stepName)
}

// StepIndexLookup maps a *dag.ActionRef to (stepIdx, actionIdx,
// stepName) within a parsed flow. Plan 04's tester.run captures
// parsed once and builds a lookup via walking Flow.Steps; for Plan
// 02's router unit tests we let the lookup be nil and the router
// falls back to (idx, idx, "").
type StepIndexLookup func(ref *dag.ActionRef) (stepIdx, actionIdx int, stepName string, found bool)

// buildExecuteBatchCallback returns the Go callback wired into
// TestWorkflowEnvironment.OnActivity. The signature MUST exactly
// match pkg/activity.ExecuteBatch's signature so testify/mock's
// reflective call dispatch passes args correctly (RESEARCH Pitfall
// 1, Investigation 1).
//
// reg + attempts + lookup are closure-captured. The callback runs in
// the activity goroutine while the workflow goroutine is blocked on
// ExecuteActivity.Get; concurrency-safe per RESEARCH §Pattern 2.
//
// Per-action processing:
//  1. Compute ActionKey via lookup; absent → use (flowName, -1, idx)
//     as a defensive fallback.
//  2. Increment attempt via attempts.NextFor(key).
//  3. reg.Match(ref, kwargsAsString) — if not found, return a
//     NonRetryableErrResult with the verbatim D5-B2 message
//     wrapping extension.ErrNonRetryable.
//  4. evalMockLambda: starlark.Call with (frozenKwargsDict, attempt).
//     Return value type-switched via AsMockResult; convert to
//     dag.ActionResult per case.
//  5. defer recover() — any panic surfaces as NonRetryableErrResult
//     with the panic message (Pitfall 8).
//
// flowName is captured for ActionKey.FlowName.
func buildExecuteBatchCallback(
	flowName string,
	reg *MockRegistry,
	attempts *AttemptCounter,
	lookup StepIndexLookup,
) func(context.Context, []*dag.ActionRef) ([]dag.ActionResult, error) {
	return func(_ context.Context, batch []*dag.ActionRef) (results []dag.ActionResult, retErr error) {
		// Pitfall 8: convert any panic into a NonRetryableErrResult so
		// the workflow surfaces a clean failure instead of an opaque
		// activity panic. Replace the entire results slice with a
		// single NonRetryable so the caller sees the failure at idx 0;
		// other actions in the batch are not processed because the
		// panic short-circuited the loop.
		defer func() {
			if r := recover(); r != nil {
				results = []dag.ActionResult{
					dag.NonRetryableErrResult{
						Idx: 0,
						Err: fmt.Errorf("mock router panic: %v: %w", r, extension.ErrNonRetryable),
					},
				}
				retErr = nil
			}
		}()

		results = make([]dag.ActionResult, 0, len(batch))
		for idx, ref := range batch {
			stepIdx, actionIdx, stepName := -1, idx, ""
			if lookup != nil {
				if si, ai, sn, found := lookup(ref); found {
					stepIdx, actionIdx, stepName = si, ai, sn
				}
			}

			key := ActionKey{FlowName: flowName, StepIdx: stepIdx, ActionIdx: actionIdx}
			attempt := attempts.NextFor(key)

			kwargsAsStr := kwargsAsStringMap(ref.Kwargs)
			entry, found := reg.Match(ref, kwargsAsStr)
			if !found {
				// D5-B2: per-action NonRetryableErrResult so the
				// activity itself succeeds (returning the result slice)
				// but the workflow's walk_step path observes a
				// NonRetryable in the slice and fails the step.
				// Mirrors quick-260502-onc 4xx classification.
				results = append(results, dag.NonRetryableErrResult{
					Idx: idx,
					Err: fmt.Errorf("%s: %w", noMockMessageFor(ref, stepName), extension.ErrNonRetryable),
				})
				continue
			}

			mockResult, evalErr := evalMockLambda(entry.Lambda, ref.Kwargs, ref.CredentialID, attempt)
			if evalErr != nil {
				// Lambda evaluation error (Starlark *EvalError) →
				// NonRetryableErr with callsite preserved by Starlark
				// itself (CallStack-aware).
				results = append(results, dag.NonRetryableErrResult{
					Idx: idx,
					Err: fmt.Errorf("mock lambda eval error: %w", joinNonRetryable(evalErr)),
				})
				continue
			}

			// D5-C4: None / non-mock-result → NonRetryableErr
			// "mock must return ok/err/nonretryable".
			mr, isMock := AsMockResult(mockResult)
			if !isMock {
				returnedType := "None"
				if mockResult != nil {
					returnedType = mockResult.Type()
				}
				results = append(results, dag.NonRetryableErrResult{
					Idx: idx,
					Err: fmt.Errorf("mock must return ok/err/nonretryable (lambda at %s returned %s): %w",
						entry.Lambda.BodyPos.String(), returnedType, extension.ErrNonRetryable),
				})
				continue
			}

			switch v := mr.(type) {
			case MockOk:
				output, err := buildMockOutput(v.Value)
				if err != nil {
					results = append(results, dag.NonRetryableErrResult{
						Idx: idx,
						Err: fmt.Errorf("ok(value=...) conversion failed: %w", joinNonRetryable(err)),
					})
					continue
				}
				results = append(results, dag.OkResult{Idx: idx, Output: output})
			case MockErr:
				results = append(results, dag.RetryableErrResult{
					Idx: idx,
					Err: errors.New(v.Msg),
				})
			case MockNonRetryable:
				results = append(results, dag.NonRetryableErrResult{
					Idx: idx,
					Err: fmt.Errorf("%s: %w", v.Msg, extension.ErrNonRetryable),
				})
			default:
				// Defensive — sealed sum should make this unreachable.
				results = append(results, dag.NonRetryableErrResult{
					Idx: idx,
					Err: fmt.Errorf("internal: unknown MockResult type %T: %w", v, extension.ErrNonRetryable),
				})
			}
		}
		return results, nil
	}
}

// kwargsAsStringMap projects a *starlark.Dict to map[string]string for
// regex matching. Non-string values are skipped (D5-B6: match keys
// only target string kwargs). Insertion order is preserved by
// Items() per Phase 04.1's documented language contract.
func kwargsAsStringMap(d *starlark.Dict) map[string]string {
	if d == nil {
		return nil
	}
	out := make(map[string]string, d.Len())
	for _, item := range d.Items() {
		k, isStr := item[0].(starlark.String)
		if !isStr {
			continue
		}
		v, isStrV := item[1].(starlark.String)
		if !isStrV {
			continue
		}
		out[string(k)] = string(v)
	}
	return out
}

// evalMockLambda invokes the captured mock function with (kwargs,
// attempt) positional args. The mock function's free variables (ok,
// err, nonretryable) were bound at parse time via WithTestPredeclared
// (Plan 02), so the same closure values resolve at execute time.
//
// kwargs is the resolved action kwargs dict (frozen, post-interpolation,
// post-credential-resolve). credentialID is exposed via
// kwargs["_credential_id"] before the lambda runs (D5-C1a; raw Secret
// is NEVER passed to the lambda).
func evalMockLambda(captured *dag.CapturedLambda, refKwargs *starlark.Dict, credID string, attempt int) (starlark.Value, error) {
	// Build augmented kwargs dict (D5-C1a credential exposure).
	augKwargs := starlark.NewDict(0)
	if refKwargs != nil {
		augKwargs = starlark.NewDict(refKwargs.Len() + 1)
		for _, item := range refKwargs.Items() {
			if err := augKwargs.SetKey(item[0], item[1]); err != nil {
				return nil, err
			}
		}
	}
	if credID != "" {
		if err := augKwargs.SetKey(starlark.String("_credential_id"), starlark.String(credID)); err != nil {
			return nil, err
		}
	}
	augKwargs.Freeze()

	thread := &starlark.Thread{Name: "mock:" + captured.ID}
	// Call with two positional args (kwargs, attempt). Free variables
	// in the lambda body (ok/err/nonretryable) resolve to the closures
	// captured at parse time — they are STATELESS so the same builder
	// closure works at parse time AND execute time.
	return starlark.Call(
		thread,
		captured.Fn,
		starlark.Tuple{augKwargs, starlark.MakeInt(attempt)},
		nil,
	)
}

// buildMockOutput converts a Starlark MockOk.Value to a
// dag.OperationOutput. dict → MockOperationOutput{Value: m}; non-dict
// (lists, scalars) wraps as MockOperationOutput{Value: {"value": v}}
// per RESEARCH Investigation 5 recommendation (always-emit JSON object).
func buildMockOutput(v starlark.Value) (dag.OperationOutput, error) {
	if v == nil || v == starlark.None {
		return MockOperationOutput{Value: map[string]any{}}, nil
	}
	goVal, err := bridge.FromStarlarkValue(v)
	if err != nil {
		return nil, err
	}
	if m, ok := goVal.(map[string]any); ok {
		return MockOperationOutput{Value: m}, nil
	}
	return MockOperationOutput{Value: map[string]any{"value": goVal}}, nil
}

// joinNonRetryable wraps err with extension.ErrNonRetryable if it
// isn't already in the chain. The activity boundary uses errors.Is to
// classify, so wrapping makes mock-eval errors immediate-fail.
func joinNonRetryable(err error) error {
	if errors.Is(err, extension.ErrNonRetryable) {
		return err
	}
	return fmt.Errorf("%w: %w", err, extension.ErrNonRetryable)
}

// WireMockCallback registers a fake "ExecuteBatch" activity (mirroring
// pkg/interpreter/walk_step_test.go::helperRegisterFakeExecuteBatch)
// then attaches the dynamic mock callback. mock.Anything is passed
// EXACTLY twice (Pitfall 1: ExecuteBatch has 2 args).
//
// Plan 04's tester.run / RunOnceCapturing helper calls this.
func WireMockCallback(env *testsuite.TestWorkflowEnvironment, callback func(context.Context, []*dag.ActionRef) ([]dag.ActionResult, error)) {
	fake := func(_ context.Context, _ []*dag.ActionRef) ([]dag.ActionResult, error) { return nil, nil }
	env.RegisterActivityWithOptions(fake, sdkactivity.RegisterOptions{Name: "ExecuteBatch"})
	env.OnActivity("ExecuteBatch", mock.Anything, mock.Anything).Return(callback)
}
