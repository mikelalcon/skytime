package testing

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.starlark.net/starlark"

	"github.com/mikelalcon/skytime/pkg/parser"
)

// helperParseTestSrc constructs a Parser in test mode + test module
// builder + mock-builder predeclared globals, then ParseSource on the
// supplied src. Returns the resulting registry, workflow spec, and any
// parse error.
//
// The three-option setup mirrors what pkg/cli/test.go (Plan 06) wires
// for `skytime test`: WithTestMode + WithTestModule (tester namespace)
// + WithTestPredeclared (ok/err/nonretryable builders so mock_fn
// lambda bodies resolve at parse time).
func helperParseTestSrc(t *testing.T, src string) (*MockRegistry, *WorkflowSpec, error) {
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
	_, parseErr := p.ParseSource("test_users.star", []byte(src))
	return reg, ws, parseErr
}

// TestTesterModule_RegistersBuiltins is THE named test from
// VALIDATION.md per-task map (TEST-01). Asserts that test-mode parsing
// accepts a file referencing the tester module without "undefined: tester".
//
// The tester.workflow + tester.mock_action calls inside def test_*()
// don't fire at parse time — they would only fire when Plan 04's
// runner invokes the test function. For Plan 02 we verify that the
// parser ACCEPTS the source.
func TestTesterModule_RegistersBuiltins(t *testing.T) {
	src := `
def test_users():
    tester.workflow(name="users", init_state={"u":"o"})
    tester.mock_action(extension="gh", op="get",
        mock_fn=lambda kwargs, attempt: ok(value={}))
`
	_, _, err := helperParseTestSrc(t, src)
	require.NoError(t, err)
}

// TestTesterModule_FileScopeRegistration — file-scope tester.mock_action
// calls fire at parse time and populate the file frame.
func TestTesterModule_FileScopeRegistration(t *testing.T) {
	src := `
tester.mock_action(extension="gh", op="get",
    mock_fn=lambda kwargs, attempt: ok(value={"login":"octocat"}))
`
	reg, _, err := helperParseTestSrc(t, src)
	require.NoError(t, err)
	require.Len(t, reg.file.Entries, 1)
	e := reg.file.Entries[0]
	assert.Equal(t, "gh", e.Extension)
	assert.Equal(t, "get", e.Op)
	assert.Empty(t, e.Match)
	require.NotNil(t, e.Lambda)
	require.NotNil(t, e.Lambda.Fn)
	assert.NotEmpty(t, e.Lambda.ID, "captured lambda must have a non-empty ID")
}

// TestTesterModule_WorkflowPopulatesSpec — file-scope tester.workflow
// populates ws.Name + ws.InitState.
func TestTesterModule_WorkflowPopulatesSpec(t *testing.T) {
	src := `
tester.workflow(name="users", init_state={"u":"o"})
`
	_, ws, err := helperParseTestSrc(t, src)
	require.NoError(t, err)
	assert.Equal(t, "users", ws.Name)
	require.NotNil(t, ws.InitState)
	assert.Equal(t, "o", ws.InitState["u"])
}

// TestTesterModule_WorkflowReCallLastWriteWins — second tester.workflow
// call overrides the first (RESEARCH Open Q5).
func TestTesterModule_WorkflowReCallLastWriteWins(t *testing.T) {
	src := `
tester.workflow(name="users", init_state={"u":"o"})
tester.workflow(name="admins", init_state={"u":"r"})
`
	_, ws, err := helperParseTestSrc(t, src)
	require.NoError(t, err)
	assert.Equal(t, "admins", ws.Name)
	assert.Equal(t, "r", ws.InitState["u"])
}

// TestTesterModule_MatchRegexCompilesAtRegistration — Match regexes
// are compiled at registration time per D5-B5.
func TestTesterModule_MatchRegexCompilesAtRegistration(t *testing.T) {
	src := `
tester.mock_action(extension="gh", op="get",
    match={"path": "^/users/[a-z]+$"},
    mock_fn=lambda kwargs, attempt: ok(value={}))
`
	reg, _, err := helperParseTestSrc(t, src)
	require.NoError(t, err)
	require.Len(t, reg.file.Entries, 1)
	re := reg.file.Entries[0].Match["path"]
	require.NotNil(t, re)
	assert.True(t, re.MatchString("/users/octocat"))
	assert.False(t, re.MatchString("/users/123"))
}

// TestTesterModule_BadRegexRejected — bad regex surfaces as parse error.
func TestTesterModule_BadRegexRejected(t *testing.T) {
	src := `tester.mock_action(extension="gh", op="get", match={"path":"[invalid"}, mock_fn=lambda kwargs, attempt: ok(value={}))`
	_, _, err := helperParseTestSrc(t, src)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "regex compile failed")
}

// TestTesterModule_CrossExtensionWildcardRejected — extension="*" is
// forbidden in v1 (D5-B3).
func TestTesterModule_CrossExtensionWildcardRejected(t *testing.T) {
	src := `tester.mock_action(extension="*", op="get", mock_fn=lambda kwargs, attempt: ok(value={}))`
	_, _, err := helperParseTestSrc(t, src)
	require.Error(t, err)
	assert.True(t,
		strings.Contains(err.Error(), "D5-B3") ||
			strings.Contains(err.Error(), "cross-extension") ||
			strings.Contains(err.Error(), "extension=\"*\""),
		"expected cross-extension rejection, got: %s", err)
}

// TestTesterModule_OpWildcardAccepted — op="*" is a tier-3 wildcard
// (D5-B3 limits the wildcard to op only, not extension).
func TestTesterModule_OpWildcardAccepted(t *testing.T) {
	src := `tester.mock_action(extension="gh", op="*", mock_fn=lambda kwargs, attempt: ok(value={}))`
	reg, _, err := helperParseTestSrc(t, src)
	require.NoError(t, err)
	require.Len(t, reg.file.Entries, 1)
	assert.Equal(t, "*", reg.file.Entries[0].Op)
}

// TestTesterModule_MatchValueMustBeString — D5-B6 restriction: match
// values must be strings (regex patterns).
func TestTesterModule_MatchValueMustBeString(t *testing.T) {
	src := `tester.mock_action(extension="gh", op="get", match={"path": 42}, mock_fn=lambda kwargs, attempt: ok(value={}))`
	_, _, err := helperParseTestSrc(t, src)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "D5-B6")
}

// TestTesterModule_BadArityRejected — mock_fn must accept exactly 2 args.
func TestTesterModule_BadArityRejected(t *testing.T) {
	for _, src := range []string{
		`tester.mock_action(extension="gh", op="get", mock_fn=lambda: ok(value={}))`,
		`tester.mock_action(extension="gh", op="get", mock_fn=lambda x: ok(value={}))`,
		`tester.mock_action(extension="gh", op="get", mock_fn=lambda a, b, c: ok(value={}))`,
	} {
		_, _, err := helperParseTestSrc(t, src)
		require.Error(t, err, "expected arity error for: %s", src)
		assert.Contains(t, err.Error(), "exactly 2 positional args")
	}
}

// TestTesterModule_ProductionParsePathUnchanged — without WithTestMode,
// "tester" must be undefined.
func TestTesterModule_ProductionParsePathUnchanged(t *testing.T) {
	p, err := parser.NewParser()
	require.NoError(t, err)
	_, perr := p.ParseSource("flow.star", []byte(`tester.workflow(name="x")`))
	require.Error(t, perr)
	low := strings.ToLower(perr.Error())
	assert.True(t,
		strings.Contains(low, "undefined: tester") ||
			strings.Contains(low, "name tester is not defined") ||
			strings.Contains(low, "undefined name: tester"),
		"expected undefined-tester error, got: %s", perr)
}

// TestTesterModule_TestModeNoBuilderRejected — calling WithTestMode()
// without WithTestModule is a programming error; surfaces at first parse.
func TestTesterModule_TestModeNoBuilderRejected(t *testing.T) {
	p, err := parser.NewParser(parser.WithTestMode())
	require.NoError(t, err)
	_, perr := p.ParseSource("test_users.star", []byte(`x = 1`))
	require.Error(t, perr)
	assert.Contains(t, perr.Error(), "WithTestModule")
}
