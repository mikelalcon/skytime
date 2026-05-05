// Package testing provides the Tier-3 E2E test harness for Skytime
// flows. Consultants write tests in *_test.star files using the
// tester.* Starlark builtins; this package translates those test files
// into go.temporal.io/sdk/testsuite-driven workflow runs against the
// production SkytimeWorkflow.
//
// Phase 5 owns this package. The harness STRADDLES the parse/execute
// split:
//
//   - tester.workflow / tester.mock_action / tester.run are PARSE-TIME
//     globals registered via parser.WithTestMode + WithTestModule.
//     They run when the .star file is parsed; their effect is to
//     populate the per-file MockRegistry and the per-test WorkflowSpec.
//
//   - The mocks themselves are CONSUMED at execute time: a callback
//     wired into testsuite.TestWorkflowEnvironment.OnActivity intercepts
//     ExecuteBatch invocations and routes per-action calls back into
//     Starlark via bridge.CallLambda — using the SAME interpreter,
//     SAME workflow type, and SAME bridge as production. There is NO
//     parallel SkytimeWorkflow_test type.
//
// The mock-lambda environment is lambdaTimeGlobals ∪ {ok, err,
// nonretryable}. Production lambda evaluation is unchanged (D5-C2;
// D1-20 invariant).
package testing
