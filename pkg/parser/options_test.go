package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWithRoot pins WithRoot's effect: sets p.root verbatim.
func TestWithRoot(t *testing.T) {
	p, err := NewParser(WithRoot("/tmp/skytime-test"))
	require.NoError(t, err)
	assert.Equal(t, "/tmp/skytime-test", p.root)
}

// TestWithExtensions registers a valid extension via the option.
func TestWithExtensions(t *testing.T) {
	p, err := NewParser(WithExtensions(&minimalExtension{name: "demo"}))
	require.NoError(t, err)
	got, ok := p.registry.Get("demo")
	require.True(t, ok, "extension 'demo' must be registered after WithExtensions")
	assert.Equal(t, "demo", got.Name())
}

// TestWithMaxExecutionSteps overrides the D-22 default.
func TestWithMaxExecutionSteps(t *testing.T) {
	p, err := NewParser(WithMaxExecutionSteps(500))
	require.NoError(t, err)
	assert.Equal(t, uint64(500), p.maxExecSteps)
}

// TestOptions_FailFast: NewParser returns the FIRST option error and
// short-circuits — validates that a bad option doesn't silently succeed.
func TestOptions_FailFast(t *testing.T) {
	rootCalled := false
	flagOpt := Option(func(p *Parser) error {
		rootCalled = true
		return nil
	})
	_, err := NewParser(
		WithExtensions(&nilIdempotentExtension{}), // fails first
		flagOpt,                                   // should NOT run
	)
	require.Error(t, err)
	assert.False(t, rootCalled, "subsequent options must not run after a failed option")
}
