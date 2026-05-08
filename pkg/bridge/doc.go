// Package bridge handles state↔starlark conversion (ToStarlarkStruct), lambda
// invocation (CallLambda), and the locked lambda-time global subset. May not
// import temporal.
//
// ENVIRONMENT DISTINCTION (D-20 + D-07-01 + D-07-04):
//
//   - lambdaTimeGlobals (Phase 1, locked at 20 keys): the strict subset
//     available inside lambdas at WORKFLOW EXECUTION time. No
//     non-determinism (no time, no random, no I/O). Workflows replay
//     deterministically; any non-determinism breaks Temporal's
//     event-sourcing guarantee.
//
//   - triggerTimeGlobals (Phase 7, 22 keys = lambdaTimeGlobals + json
//     + time): the env for trigger map and idempotency_key lambdas.
//     These run ONCE at HTTP ingress (Phase 7.1+) BEFORE
//     client.ExecuteWorkflow — the result is the workflow input,
//     frozen at that point. Non-determinism (time.now) is observably
//     safe because the workflow never re-evaluates the lambda.
//
// Conflating the two environments would silently break replay
// determinism for workflow lambdas — keep them strictly separate. Tests
// (TestLambdaTimeGlobalsLocked, TestTriggerTimeGlobalsLocked) gate the
// surfaces against drift.
package bridge
