package parser

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.starlark.net/resolve"
)

// TestResolveAllowLambdaIsSet pins DSL-10 / D-10: parser package init
// explicitly assigns resolve.AllowLambda = true even though the flag is
// documented as obsolete. Removing the init() function would let this test
// fail if a future starlark-go release flips the default.
func TestResolveAllowLambdaIsSet(t *testing.T) {
	require.True(t, resolve.AllowLambda,
		"DSL-10 requires resolve.AllowLambda = true after pkg/parser package init")
}
