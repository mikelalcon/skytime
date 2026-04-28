package activity

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/extension"
)

// TestOperationDispatch_LookupHit verifies that OperationDispatch is a usable
// map[string]extension.OperationSpec — lookup returns the registered spec on
// hit, and the zero-value + ok=false on miss.
//
// D2-17: dispatch is keyed by "<extName>.<opName>" (e.g., "github.create_issue").
// The activity reads ActionRef.Kind_ verbatim as the lookup key.
func TestOperationDispatch_LookupHit(t *testing.T) {
	idemp := true
	echoSpec := extension.OperationSpec{
		Name:           "echo",
		Idempotent:     &idemp,
		Func:           nil, // not invoked here — lookup test only
		KwargsType:     reflect.TypeOf(struct{}{}),
		DefaultTimeout: 30 * time.Second,
	}
	dispatch := OperationDispatch{
		"fake.echo": echoSpec,
	}

	got, ok := dispatch["fake.echo"]
	require.True(t, ok, "expected hit on fake.echo")
	require.Equal(t, "echo", got.Name)
	require.Equal(t, &idemp, got.Idempotent)
	require.Equal(t, 30*time.Second, got.DefaultTimeout)

	// Miss: zero-value + ok=false.
	miss, ok := dispatch["fake.nonexistent"]
	require.False(t, ok)
	require.Equal(t, extension.OperationSpec{}, miss)
}
