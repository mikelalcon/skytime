package testing

import (
	"fmt"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarktest"

	"github.com/mikelalcon/skytime/pkg/interpreter"
)

// runContext bundles the per-test-file pieces tester.run needs that
// are NOT part of the (reg, ws) closure: the parsed flow lookup +
// content hashes + initial state. The runner
// (pkg/testing/runner.go::Run) builds one runContext per test file,
// shared across every def test_*() in that file.
//
// The runner holds a pointer-to-pointer (**runContext) so the active
// context can be swapped per test without rebuilding the tester
// Module. For unit-test paths (no runner), ctxRef points at a nil
// *runContext and tester.run surfaces a clear "runner context not
// initialized" error.
type runContext struct {
	flows         map[string]*interpreter.ParsedFlow // name → parsed flow
	contentHashes map[string]string                  // name → content_hash
}

// makeBuiltinTesterRun replaces Plan 02's placeholder. The closure
// captures *runContextRef (pointer-to-pointer) so the runner can swap
// the active context per test without rebuilding the tester Module.
//
// Behavior summary (D5 Pitfall 4 + D5-D1 + D5-D2):
//
//   - Reject file-scope invocation: scan thread.CallStack() for a
//     user-Function frame whose name starts with "test_". If absent,
//     return an error containing the verbatim Pitfall 4 message.
//   - Look up the flow + content hash in the active runContext.
//   - Run the production flow TWICE via RunOnceCapturing (D5-D1
//     always-on replay).
//   - If the two runs diverge, report via the active starlarktest
//     reporter (which the runner bound to the subtest's *testing.T).
//     If no reporter is wired, surface as a Starlark error so callers
//     see something.
func makeBuiltinTesterRun(reg *MockRegistry, ws *WorkflowSpec, ctxRef **runContext) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		// D5 Pitfall 4: tester.run must be called inside a def test_*().
		if !isInsideDefTestStar(thread) {
			pos := callerPosFromThread(thread)
			return nil, fmt.Errorf("tester.run must be called inside a def test_*() function (at %s)", pos.String())
		}

		if len(args) != 0 {
			return nil, fmt.Errorf("tester.run: positional args not supported (use kwargs)")
		}
		var flowName string
		if err := starlark.UnpackArgs("tester.run", args, kwargs, "flow", &flowName); err != nil {
			return nil, err
		}
		if flowName == "" {
			return nil, fmt.Errorf("tester.run: flow must be non-empty")
		}

		ctx := *ctxRef
		if ctx == nil {
			return nil, fmt.Errorf("tester.run: runner context not initialized (was tester.run called outside pkg/testing.Run?)")
		}

		parsed, ok := ctx.flows[flowName]
		if !ok {
			return nil, fmt.Errorf("tester.run: flow %q not found in parser session", flowName)
		}
		hash, ok := ctx.contentHashes[flowName]
		if !ok {
			return nil, fmt.Errorf("tester.run: content_hash for flow %q not found", flowName)
		}

		initState := ws.InitState
		if initState == nil {
			initState = map[string]any{}
		}

		// D5-D1: always-on replay. Run twice with shared mock
		// registry + shared attempt counter so retry-style mocks
		// (D5-C1 attempt arg) see consistent counts across both runs.
		attempts := NewAttemptCounter()

		cap1, _, err1 := RunOnceCapturing(parsed, hash, initState, reg, attempts, nil)
		if err1 != nil {
			return nil, fmt.Errorf("tester.run (run 1): workflow error: %w", err1)
		}
		cap2, _, err2 := RunOnceCapturing(parsed, hash, initState, reg, attempts, nil)
		if err2 != nil {
			return nil, fmt.Errorf("tester.run (run 2): workflow error: %w", err2)
		}

		testCallsite := callerPosFromThread(thread)
		if d := FirstDivergentEvent(cap1, cap2, testCallsite); d != nil {
			rep := starlarktest.GetReporter(thread)
			if rep != nil {
				rep.Error(d.Format())
			} else {
				// Defensive: no reporter wired (raw eval) — surface
				// as a Starlark error so the caller sees something.
				return nil, fmt.Errorf("%s", d.Format())
			}
		}

		return starlark.None, nil
	}
}

// isInsideDefTestStar walks the thread's call stack looking for a
// user-Function frame whose name starts with "test_". The call stack
// from go.starlark.net is ordered outermost-first; we scan all
// frames. Skips internal frames (<toplevel>, <builtin>, the tester.*
// builtin frames themselves).
func isInsideDefTestStar(thread *starlark.Thread) bool {
	cs := thread.CallStack()
	for _, frame := range cs {
		name := frame.Name
		switch {
		case name == "":
			continue
		case name == "<toplevel>":
			continue
		case name == "<builtin>":
			continue
		case strings.HasPrefix(name, "tester."):
			// tester.run / tester.workflow / tester.mock_action
			continue
		}
		// A NAMED user function whose name starts with test_ is a
		// def test_*() body — Pitfall 4 satisfied.
		if strings.HasPrefix(name, "test_") {
			return true
		}
	}
	return false
}

// callerPosFromThread is shared with builtin_mock_action.go; defined
// there to avoid duplication. Skips <builtin> + tester.* frames so the
// resulting position lands on the user's .star line.
