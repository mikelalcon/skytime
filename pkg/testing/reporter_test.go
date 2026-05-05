package testing

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarktest"

	"github.com/mikelalcon/skytime/pkg/parser"
)

// helperGetTestFn parses src in test mode and returns the
// def test_<name> function from the captured globals.
func helperGetTestFn(t *testing.T, src, filename, name string) *starlark.Function {
	t.Helper()
	reg := NewMockRegistry()
	ws := &WorkflowSpec{}
	p, err := parser.NewParser(
		parser.WithTestMode(),
		parser.WithTestModule(func(_ *parser.Parser, _ *starlark.Thread) starlark.Value {
			return NewTesterModule(reg, ws)
		}),
		parser.WithTestPredeclared(MockLambdaParseTimeBuilders()),
	)
	require.NoError(t, err)
	_, err = p.ParseSource(filename, []byte(src))
	require.NoError(t, err)
	g, ok := p.TestGlobals(filename)
	require.True(t, ok)
	fn, ok := g[name].(*starlark.Function)
	require.True(t, ok, "missing %s in globals", name)
	return fn
}

// capturingReporter records starlarktest.Reporter.Error calls so
// reporter_test can assert on the failure messages directly.
type capturingReporter struct{ calls [][]any }

func (c *capturingReporter) Error(args ...any) {
	c.calls = append(c.calls, args)
}

// AllArgs joins every recorded call's args into a single newline-
// separated string suitable for substring assertions.
func (c *capturingReporter) AllArgs() string {
	var sb strings.Builder
	for _, call := range c.calls {
		sb.WriteString(fmt.Sprintln(call...))
	}
	return sb.String()
}

// driveWithReporter parses src + invokes the named def test_*()
// under a custom reporter. Bypasses the t.Run subtest layer so the
// test file can assert on rep.calls directly.
func driveWithReporter(t *testing.T, src, filename, name string, rep starlarktest.Reporter) {
	t.Helper()
	fn := helperGetTestFn(t, src, filename, name)
	reg := NewMockRegistry()
	thread := &starlark.Thread{Name: "test:" + name}
	starlarktest.SetReporter(thread, rep)
	reg.PushTestFrame()
	defer reg.PopTestFrame()
	if _, err := starlark.Call(thread, fn, nil, nil); err != nil {
		// Report eval errors via the same reporter so tests can see
		// them; mirrors what runOneTest does via subT.Errorf.
		rep.Error(err.Error())
	}
}

// TestAssert_FailureSurfacesInSubtestT — VALIDATION.md per-task map cite.
//
// A def test_*() that calls assert.eq with non-equal values must
// produce ≥1 Reporter.Error call whose joined args contain both the
// expected and actual values plus a Starlark file:line: position.
func TestAssert_FailureSurfacesInSubtestT(t *testing.T) {
	src := "def test_failing():\n    assert.eq(\"octocat\", \"default-user\")\n"
	cap := &capturingReporter{}
	driveWithReporter(t, src, "users_test.star", "test_failing", cap)
	require.NotEmpty(t, cap.calls, "assert.eq mismatch should call Reporter.Error")
	msg := cap.AllArgs()
	assert.Contains(t, msg, "users_test.star")
	assert.Contains(t, msg, "octocat")
	assert.Contains(t, msg, "default-user")
}

// TestAssert_AccumulatesMultipleFailuresInSubtest — VALIDATION.md
// per-task map cite. D5-F2 library default: two failing assert.eq
// calls in one def test_*() produce ≥2 Reporter.Error calls.
func TestAssert_AccumulatesMultipleFailuresInSubtest(t *testing.T) {
	src := "def test_two_failures():\n    assert.eq(1, 2)\n    assert.eq(3, 4)\n"
	cap := &capturingReporter{}
	driveWithReporter(t, src, "t.star", "test_two_failures", cap)
	assert.GreaterOrEqual(t, len(cap.calls), 2,
		"library default = accumulate; expected ≥2 Reporter.Error calls, got %d", len(cap.calls))
}

// recordingT is a minimal testReporter that records whether any
// Error/Errorf was called. Used to assert per-test isolation without
// the propagation that real *testing.T t.Run subtests cause.
type recordingT struct {
	failed bool
}

func (r *recordingT) Helper()                                {}
func (r *recordingT) Error(args ...any)                      { r.failed = true }
func (r *recordingT) Errorf(format string, args ...any)      { r.failed = true }

// TestRunOneTest_SubtestIsolation: test_a fails, test_b passes; verify
// per-test reporters are independent. Each runOneTest invocation gets
// a fresh recording shim so assert.* failures only land where they
// were emitted (analogous to t.Run's per-subtest *testing.T).
//
// Using a recording shim instead of nested t.Run avoids Go's testing
// failure-propagation: a failed inner subtest fails the parent T
// transitively, which would falsely fail this very test.
func TestRunOneTest_SubtestIsolation(t *testing.T) {
	srcA := "def test_a():\n    assert.eq(1, 2)\n"
	srcB := "def test_b():\n    assert.eq(1, 1)\n"
	fnA := helperGetTestFn(t, srcA, "a.star", "test_a")
	fnB := helperGetTestFn(t, srcB, "b.star", "test_b")

	repA := &recordingT{}
	regA := NewMockRegistry()
	runOneTest(repA, fnA, regA, &WorkflowSpec{})

	repB := &recordingT{}
	regB := NewMockRegistry()
	runOneTest(repB, fnB, regB, &WorkflowSpec{})

	assert.True(t, repA.failed, "test_a should have produced a Reporter failure")
	assert.False(t, repB.failed, "test_b should NOT have produced a Reporter failure")
}
