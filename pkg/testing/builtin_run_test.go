package testing

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTesterRun_OutsideDefTest_RejectsAtParse — verbatim D5 Pitfall 4
// message. tester.run called at file scope (NOT inside a def test_*())
// surfaces the "must be called inside a def test_*()" error during
// parse time (the call evaluates as part of ExecFileOptions).
//
// VALIDATION.md per-task map cite (Plan 04 Task 2).
func TestTesterRun_OutsideDefTest_RejectsAtParse(t *testing.T) {
	src := `tester.run(flow="x")`
	_, _, err := helperParseTestSrc(t, src)
	require.Error(t, err)
	msg := err.Error()
	assert.True(t,
		strings.Contains(msg, "must be called inside a def test_*()"),
		"expected file-scope rejection message, got: %s", msg)
}

// TestTesterRun_InsideDefTest_ParseAccepts — at parse time the outer
// def test_x() is DEFINED but not yet INVOKED. tester.run does not
// fire during ExecFileOptions; the parser accepts the source. Plan 04
// Task 3's runner is the path that actually invokes the function and
// surfaces tester.run errors.
func TestTesterRun_InsideDefTest_ParseAccepts(t *testing.T) {
	src := `
def test_users():
    tester.run(flow="users")
`
	_, _, err := helperParseTestSrc(t, src)
	require.NoError(t, err, "def-wrapped tester.run must not fire at parse time; got: %v", err)
}
