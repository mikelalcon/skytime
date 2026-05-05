package testing

import (
	"fmt"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarktest"
)

// testReporter is the minimal subset of *testing.T that runOneTest
// needs. *testing.T satisfies this interface via duck-typing; tests
// can supply a fake (e.g., a recording shim) to observe failures
// without poisoning the parent test.
//
// Composing starlarktest.Reporter (Error(...)) with Helper() and
// Errorf(...) covers every method runOneTest invokes.
type testReporter interface {
	Helper()
	Errorf(format string, args ...any)
	starlarktest.Reporter // Error(args ...any)
}

// runOneTest invokes a single `def test_*()` function under subT
// (typically *testing.T from t.Run; tests may supply a recording
// shim that satisfies testReporter). Per Phase 5 D5-F1:
//
//   - Constructs a fresh *starlark.Thread per test (Pitfall #1: no
//     thread reuse across calls — the thread carries per-call locals
//     including the starlarktest reporter).
//   - Calls starlarktest.SetReporter(thread, subT) so assert.*
//     failures route to subT.Error (D5-F1, TEST-05). The library
//     default accumulates failures within a single def test_*()
//     (D5-F2) — a single test_x() with two failing assert.eq calls
//     produces two subT.Error invocations.
//   - Pushes a per-test frame on the MockRegistry on entry; pops on
//     exit (D5-A4 mock scope stack). tester.mock_action calls inside
//     this def test_*() land on the per-test frame and are popped
//     when the test finishes.
//   - Invokes fn via starlark.Call with no args (def test_* must
//     accept () — validated at discovery time, Plan 05).
//   - Catches non-assertion eval errors and surfaces them via
//     subT.Errorf. assert.* failures already routed to subT.Error
//     via the reporter; do NOT duplicate (starlarktest's assert
//     helpers call Reporter.Error themselves and return without
//     raising an *EvalError).
//
// reg + ws are owned by the runner; the per-test frame is the layer
// where tester.mock_action calls inside this def test_*() land.
func runOneTest(subT testReporter, fn *starlark.Function, reg *MockRegistry, ws *WorkflowSpec) {
	subT.Helper()
	_ = ws // ws is held by the runner; runOneTest does not consume it directly.

	thread := &starlark.Thread{
		Name: fmt.Sprintf("test:%s:%s", fn.Position().Filename(), fn.Name()),
	}
	starlarktest.SetReporter(thread, subT)

	reg.PushTestFrame()
	defer reg.PopTestFrame()

	_, err := starlark.Call(thread, fn, nil, nil)
	if err != nil {
		// Starlark *EvalError already includes file:line:col via
		// err.Error(). Plain Go errors fall back to the same path.
		subT.Errorf("%s: %v", fn.Name(), err)
	}
}
