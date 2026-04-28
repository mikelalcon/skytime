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

// TestParser_DefaultMaxBlockSize verifies NewParser() with no options sets
// maxBlockSize to the D2-07 default of 50.
func TestParser_DefaultMaxBlockSize(t *testing.T) {
	p, err := NewParser()
	require.NoError(t, err)
	assert.Equal(t, 50, p.maxBlockSize, "D2-07 default maxBlockSize is 50")
}

// TestWithMaxBlockSize verifies WithMaxBlockSize(N) overrides the default
// cap on the Parser instance.
func TestWithMaxBlockSize(t *testing.T) {
	p, err := NewParser(WithMaxBlockSize(3))
	require.NoError(t, err)
	assert.Equal(t, 3, p.maxBlockSize)

	// Boundary: a cap of 1 (smallest allowed) is accepted.
	p, err = NewParser(WithMaxBlockSize(1))
	require.NoError(t, err)
	assert.Equal(t, 1, p.maxBlockSize)
}

// TestWithMaxBlockSize_InvalidNonPositive verifies values < 1 (zero,
// negative) are rejected at parser construction time. We pick "error" over
// "no cap" so misconfiguration is a fast-fail rather than a silent
// permissive default — the activity (Phase 2) defensively re-enforces, but
// the parser-side fast-fail surfaces the bug at the call site.
func TestWithMaxBlockSize_InvalidNonPositive(t *testing.T) {
	cases := []int{-1, 0, -100}
	for _, n := range cases {
		t.Run("", func(t *testing.T) {
			_, err := NewParser(WithMaxBlockSize(n))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid max block size")
		})
	}
}
