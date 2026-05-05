package testing

import (
	"fmt"

	"go.starlark.net/starlark"

	"github.com/mikelalcon/skytime/pkg/bridge"
)

// makeBuiltinTesterWorkflow returns the Starlark builtin closure for
// tester.workflow(name, init_state, retry_policy, timeouts). Last
// invocation wins (RESEARCH Open Q5 silent last-write-wins). D5-A3.
//
// retry_policy and timeouts are accepted as opaque starlark.Value in
// this plan; Plan 04 deserializes them into Temporal RetryPolicy +
// StartWorkflowOptions when wiring tester.run.
func makeBuiltinTesterWorkflow(ws *WorkflowSpec) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("tester.workflow: positional args not supported (use kwargs)")
		}
		var (
			name        string
			initState   starlark.Value = starlark.None
			retryPolicy starlark.Value = starlark.None
			timeouts    starlark.Value = starlark.None
		)
		if err := starlark.UnpackArgs("tester.workflow", args, kwargs,
			"name", &name,
			"init_state?", &initState,
			"retry_policy?", &retryPolicy,
			"timeouts?", &timeouts,
		); err != nil {
			return nil, err
		}

		ws.Name = name
		// Convert init_state dict → map[string]any. Reset to nil when
		// the kwarg is omitted (None) so re-declaring tester.workflow
		// without init_state correctly clears the previous value.
		if initState != nil && initState != starlark.None {
			d, ok := initState.(*starlark.Dict)
			if !ok {
				return nil, fmt.Errorf("tester.workflow: init_state must be a dict, got %s", initState.Type())
			}
			goVal, err := bridge.FromStarlarkValue(d)
			if err != nil {
				return nil, fmt.Errorf("tester.workflow: init_state conversion: %w", err)
			}
			m, isMap := goVal.(map[string]any)
			if !isMap {
				return nil, fmt.Errorf("tester.workflow: init_state did not convert to map[string]any")
			}
			ws.InitState = m
		} else {
			ws.InitState = nil
		}
		// retry_policy / timeouts are reserved for Plan 04. Accept and
		// drop here so consultants can write Plan-04-shaped tester.workflow
		// calls today without breakage.
		_ = retryPolicy
		_ = timeouts
		return starlark.None, nil
	}
}
