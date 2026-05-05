package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
)

// TestParser_WithTestMode_FlipsTestModeFlag — WithTestMode() must
// set p.testMode to true. White-box because the field is unexported
// (Plan 02 owns the actual injection logic; this plan adds the field
// and option only).
func TestParser_WithTestMode_FlipsTestModeFlag(t *testing.T) {
	p, err := NewParser(WithTestMode())
	require.NoError(t, err)
	assert.True(t, p.testMode, "WithTestMode() must set p.testMode=true")
}

// TestParser_NoTestOptions_DefaultsTestModeFalse — production parse
// path (no test options) leaves testMode false and testModuleBuilder
// nil. Plan 02 will branch on these in newParseTimeGlobals; until
// then the fields are inert.
func TestParser_NoTestOptions_DefaultsTestModeFalse(t *testing.T) {
	p, err := NewParser()
	require.NoError(t, err)
	assert.False(t, p.testMode)
	assert.Nil(t, p.testModuleBuilder)
	// testGlobals starts nil — Plan 05 allocates lazily.
	assert.Nil(t, p.testGlobals)
}

// TestParser_WithTestModule_WiresBuilder — WithTestModule(fn) stores
// the builder; calling it produces the expected sentinel value. The
// real builder (Plan 02) constructs a *starlarkstruct.Module; for
// this plan we just verify the wire-through.
func TestParser_WithTestModule_WiresBuilder(t *testing.T) {
	sentinel := starlark.String("sentinel-tester-module")
	p, err := NewParser(WithTestModule(func(_ *Parser, _ *starlark.Thread) starlark.Value {
		return sentinel
	}))
	require.NoError(t, err)
	require.NotNil(t, p.testModuleBuilder)

	got := p.testModuleBuilder(p, &starlark.Thread{})
	assert.Equal(t, sentinel, got)
}

// TestParser_WithTestModule_NilBuilderReturnsError — defensive: a
// programming error (calling WithTestMode() but forgetting
// WithTestModule, or passing nil explicitly) surfaces at parser
// construction time, not at first parse.
func TestParser_WithTestModule_NilBuilderReturnsError(t *testing.T) {
	_, err := NewParser(WithTestModule(nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not be nil")
}
