package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// TestRenderer_StarlarkFirst_ValidationError verifies D4-18: a typed
// *dag.ValidationError is rendered with the canonical
// "<file>:<line>:<col> [flow > step > action]: <msg>" format.
func TestRenderer_StarlarkFirst_ValidationError(t *testing.T) {
	file := "flows/x.star"
	ve := &dag.ValidationError{
		Pos:    syntax.MakePosition(&file, 10, 5),
		Flow:   "my_flow",
		Step:   "step_2",
		Action: "github.create_issue",
		Msg:    "missing kwarg",
	}
	var buf bytes.Buffer
	renderError(&buf, ve, false)
	require.Equal(t, "flows/x.star:10:5 [my_flow > step_2 > github.create_issue]: missing kwarg\n", buf.String())
}

// TestRenderer_StarlarkFirst_ParseError verifies a typed *dag.ParseError
// is rendered with "<file>:<line>:<col>: <msg>".
func TestRenderer_StarlarkFirst_ParseError(t *testing.T) {
	file := "flows/y.star"
	pe := &dag.ParseError{
		Pos: syntax.MakePosition(&file, 7, 3),
		Msg: "unknown extension",
	}
	var buf bytes.Buffer
	renderError(&buf, pe, false)
	require.Equal(t, "flows/y.star:7:3: unknown extension\n", buf.String())
}

// TestRenderer_DropsWrappedChainByDefault: D4-18 mandates wrapped Go
// errors stay hidden in default output.
func TestRenderer_DropsWrappedChainByDefault(t *testing.T) {
	file := "flows/z.star"
	inner := errors.New("Go internal: nil pointer dereference at line 42")
	ve := &dag.ValidationError{
		Pos:     syntax.MakePosition(&file, 1, 1),
		Flow:    "f",
		Msg:     "validation problem",
		Wrapped: inner,
	}
	var buf bytes.Buffer
	renderError(&buf, ve, false)
	require.NotContains(t, buf.String(), "Go internal", "Wrapped chain must NOT appear in default output")
	require.Contains(t, buf.String(), "validation problem")
	require.Contains(t, buf.String(), "[f]")
}

// TestRenderer_DebugUnwrapsChain: D4-19 mandates --debug reveals the
// Wrapped chain.
func TestRenderer_DebugUnwrapsChain(t *testing.T) {
	file := "flows/z.star"
	inner := errors.New("Go internal: nil pointer dereference at line 42")
	ve := &dag.ValidationError{
		Pos:     syntax.MakePosition(&file, 1, 1),
		Flow:    "f",
		Msg:     "validation problem",
		Wrapped: inner,
	}
	var buf bytes.Buffer
	renderError(&buf, ve, true)
	out := buf.String()
	require.Contains(t, out, "validation problem")
	require.Contains(t, out, "Go internal: nil pointer dereference") // chain visible
	require.Contains(t, out, "cause:")                                // unwrap marker
	require.Equal(t, 2, strings.Count(out, "\n"), "expected exactly two newlines: primary line + cause line")
}

// TestRenderer_UntypedErrorPrintsMessage: when the error chain has
// no typed dag error, we still print the err.Error() string so the
// user sees something actionable.
func TestRenderer_UntypedErrorPrintsMessage(t *testing.T) {
	e := errors.New("some random Go error")
	var buf bytes.Buffer
	renderError(&buf, e, false)
	require.Equal(t, "some random Go error\n", buf.String())
}
