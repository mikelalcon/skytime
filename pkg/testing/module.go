package testing

import (
	"fmt"

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
//   - run(flow=...) — Plan 04 wires the ExecuteWorkflow driver here;
//     this plan ships a placeholder that surfaces a clear "not yet
//     implemented" error if invoked at parse time
//
// reg + ws are owned by the parser session that called
// parser.WithTestModule. Plan 04's runner pushes/pops per-test frames
// on reg before invoking each def test_*().
//
// D5-A3, D5-B1, D5-A4. Pattern from
// pkg/extension/builtin/http/http.go::Initialize.
func NewTesterModule(reg *MockRegistry, ws *WorkflowSpec) starlark.Value {
	return &starlarkstruct.Module{
		Name: "tester",
		Members: starlark.StringDict{
			"workflow":    starlark.NewBuiltin("workflow", makeBuiltinTesterWorkflow(ws)),
			"mock_action": starlark.NewBuiltin("mock_action", makeBuiltinTesterMockAction(reg)),
			"run":         starlark.NewBuiltin("run", makeBuiltinTesterRunPlaceholder()),
		},
	}
}

// makeBuiltinTesterRunPlaceholder is the Plan 02 stub for tester.run.
// Plan 04 replaces this builder with the real ExecuteWorkflow driver
// once RunOnceCapturing (lifted in Plan 03) is available.
//
// Returning a clear error keeps the surface honest for any consultant
// who runs `skytime test` against a Plan 02 build: "tester.run: not yet
// implemented (Phase 5 Plan 04)".
func makeBuiltinTesterRunPlaceholder() func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
		return nil, fmt.Errorf("tester.run: not yet implemented (Phase 5 Plan 04)")
	}
}
