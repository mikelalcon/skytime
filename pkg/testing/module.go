package testing

import (
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// WorkflowSpec is the per-test-file workflow declaration captured by
// the LAST tester.workflow(...) call inside any def test_*() (or at
// file scope as a default). Plan 04's tester.run reads this to
// configure dag.WorkflowInput + StartWorkflowOptions.
//
// Re-declaring tester.workflow inside a def test_*() overrides
// (last-write-wins per RESEARCH Open Q5; documented behavior).
//
// RetryPolicy and Timeouts are reserved for Plan 04 — Phase 5 initial
// drop accepts the kwargs but does not yet thread them into
// StartWorkflowOptions.
type WorkflowSpec struct {
	Name      string
	InitState map[string]any
}

// NewTesterModule returns the `tester` namespace value bound under the
// global name "tester" in test-mode parses. The Module exposes:
//
//   - workflow(name, init_state, retry_policy, timeouts) — populates ws
//   - mock_action(extension, op, mock_fn, match) — registers a MockEntry
//     in reg's active frame (D5-A4 stack)
//   - run(flow=...) — invokes the production flow twice via
//     RunOnceCapturing (D5-D1 always-on replay) and reports D5-D2
//     divergence via the active starlarktest reporter. Rejects file-
//     scope use (Pitfall 4: must be inside a def test_*()).
//
// reg + ws are owned by the parser session that called
// parser.WithTestModule. Plan 04's runner pushes/pops per-test frames
// on reg before invoking each def test_*().
//
// For non-runner callers (raw parser exercises, Plan 02 module tests)
// the runContext slot is nil; tester.run will surface a clear
// "runner context not initialized" error.
//
// D5-A3, D5-B1, D5-A4, D5-D1, D5-D2, D5 Pitfall 4. Pattern from
// pkg/extension/builtin/http/http.go::Initialize.
func NewTesterModule(reg *MockRegistry, ws *WorkflowSpec) starlark.Value {
	var ctxRef *runContext
	return newTesterModuleWithCtx(reg, ws, &ctxRef)
}

// newTesterModuleWithCtx is the runner-facing constructor. The runner
// holds a stable pointer-to-pointer (**runContext) so it can mutate
// the active context per def test_*() without rebuilding the Module.
func newTesterModuleWithCtx(reg *MockRegistry, ws *WorkflowSpec, ctxRef **runContext) starlark.Value {
	return &starlarkstruct.Module{
		Name: "tester",
		Members: starlark.StringDict{
			"workflow":    starlark.NewBuiltin("workflow", makeBuiltinTesterWorkflow(ws)),
			"mock_action": starlark.NewBuiltin("mock_action", makeBuiltinTesterMockAction(reg)),
			"run":         starlark.NewBuiltin("run", makeBuiltinTesterRun(reg, ws, ctxRef)),
		},
	}
}
