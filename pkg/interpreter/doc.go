// Package interpreter walks parsed dag.Flow values inside a Temporal
// workflow goroutine, dispatching Steps to the single ExecuteBatch
// activity (registered in pkg/activity) and evaluating IfCond / Script /
// ForEachParallel.items lambdas inline via pkg/bridge.CallLambda.
//
// FIREWALL: this is one of three packages allowed to import
// go.temporal.io/sdk/* (the others being pkg/activity (Phase 2) and
// pkg/worker (Phase 3 plan 03-04)). Every other pkg/* directory is
// firewall-blocked by pkg/activity/firewall_test.go::
// TestNoTemporalImportsOutsideAllowList. The forward-compatible allowlist
// pre-permitted pkg/interpreter and pkg/worker before they existed; this
// package landing flips the meta-test (firewall_test.go) from skip-on-empty
// to assertive.
//
// Lambda serialization (D3-01..D3-05): *starlark.Function values do NOT
// cross the Temporal serialization boundary. WorkflowInput carries
// {FlowName, ContentHash, InitState}; the worker's FlowRegistry (this
// package, registry.go) maps (flow_name, content_hash) → *ParsedFlow,
// and the interpreter looks up lambdas by D-18 ID against
// ParsedFlow.Lambdas on every evaluation.
//
// Determinism contract (D3-23, D3-24):
//   - All map iteration sorts keys before reading (sortedKeys helper).
//   - No native `go` keyword — only workflow.Go inside this package.
//     The cancellation watchdog's bridged native channel is a sanctioned
//     exception bounded inside pkg/bridge (see cancel_watchdog.go for the
//     determinism rationale).
//   - No time.Now / rand.* — workflow.Now / workflow.GetInfo only.
//   - workflowcheck ./pkg/interpreter/... must report zero findings.
//
// Plan 03-02 lands the foundations:
//   - registry.go        — FlowRegistry + ParsedFlow + frozen-after-boot semantics
//   - cancel_watchdog.go — workflow.Channel → native chan struct{} bridge (D3-21)
//   - state.go           — sorted-key state map for determinism (D3-23)
//   - workflow.go        — SkytimeWorkflow skeleton + walkBody dispatcher
//
// Plan 03-03 fills in the per-node walker bodies (walkStep, walkIfCond,
// walkScript, walkForEach, walkCallFlow) — without retrofitting the
// `interpreter` struct shape or `newInterpreter`'s signature.
package interpreter
