package dag

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// fakeTSrc is a minimal TriggerSource for testing dag.Trigger marshaling.
// It satisfies the dag-local TriggerSource interface (Kind + MarshalJSON);
// the extension-side seal (triggerSourceMarker) is irrelevant here because
// the dag package does not enforce it.
type fakeTSrc struct {
	kindName string
	configB  []byte // pre-rendered config bytes (raw JSON object, must be valid JSON)
}

func (f *fakeTSrc) Kind() string { return f.kindName }
func (f *fakeTSrc) MarshalJSON() ([]byte, error) {
	// Envelope: {"kind":"<kindName>","config":<configB>}
	return []byte(fmt.Sprintf(`{"kind":%q,"config":%s}`, f.kindName, string(f.configB))), nil
}

// freezableTSrc records whether Freeze was called — used to verify that
// Trigger.Freeze cascades into Source when the concrete type implements
// the interface{ Freeze() } shape.
type freezableTSrc struct {
	fakeTSrc
	frozen bool
}

func (f *freezableTSrc) Freeze() { f.frozen = true }

// --- Test 1: full round-trip MarshalJSON contains expected fields -----------

func TestTrigger_MarshalRoundTrip(t *testing.T) {
	src := &fakeTSrc{kindName: "fake.webhook", configB: []byte(`{"path":"/hook"}`)}
	trig := &Trigger{
		FlowName:          "check_user",
		Source:            src,
		MapLambda:         &CapturedLambda{ID: "abc:1:2"},
		IdempotencyLambda: &CapturedLambda{ID: "def:3:4"},
		CredentialID:      "gh-secret",
	}
	b, err := json.Marshal(trig)
	require.NoError(t, err)

	// Discriminator and primitive fields
	assert.Contains(t, string(b), `"kind":"Trigger"`)
	assert.Contains(t, string(b), `"flow_name":"check_user"`)
	assert.Contains(t, string(b), `"map_lambda_id":"abc:1:2"`)
	assert.Contains(t, string(b), `"idempotency_lambda_id":"def:3:4"`)
	assert.Contains(t, string(b), `"credential_id":"gh-secret"`)

	// Source delegated to TriggerSource.MarshalJSON — its kind appears under
	// the "source" envelope.
	assert.Contains(t, string(b), `"source":{`)
	assert.Contains(t, string(b), `"kind":"fake.webhook"`)
	assert.Contains(t, string(b), `"path":"/hook"`)
}

// --- Test 2: Pos NEVER serializes ------------------------------------------

func TestTrigger_MarshalRoundTrip_NoPos(t *testing.T) {
	fname := "/tmp/abs/path.star"
	trig := &Trigger{
		Pos:      syntax.MakePosition(&fname, 42, 7),
		FlowName: "x",
		Source:   &fakeTSrc{kindName: "ext.webhook", configB: []byte(`{}`)},
	}
	require.True(t, trig.Pos.IsValid(), "fixture sanity: Pos must be valid before marshal")
	b, err := json.Marshal(trig)
	require.NoError(t, err)
	assert.False(t, bytes.Contains(b, []byte("/tmp/abs/path.star")), "Pos.Filename must not leak: %s", string(b))
	assert.False(t, bytes.Contains(b, []byte(`"line":42`)), "Pos.Line must not leak: %s", string(b))
	assert.False(t, bytes.Contains(b, []byte(`"pos"`)), "no pos key allowed: %s", string(b))
	// Field name "Pos" should also not appear in any cased form.
	assert.False(t, strings.Contains(strings.ToLower(string(b)), `"pos`), "no pos-prefixed field: %s", string(b))
}

// --- Test 3: credential never leaks; omitempty when blank --------------------

func TestTrigger_MarshalRoundTrip_NoCredentialLeak(t *testing.T) {
	// Case A: with credential — id appears, no Secret-shaped value.
	trig := &Trigger{
		FlowName:     "x",
		Source:       &fakeTSrc{kindName: "ext.k", configB: []byte(`{}`)},
		CredentialID: "my-secret-id",
	}
	b, err := json.Marshal(trig)
	require.NoError(t, err)
	assert.True(t, bytes.Contains(b, []byte(`"credential_id":"my-secret-id"`)), "credential_id must serialize: %s", string(b))
	assert.False(t, bytes.Contains(b, []byte(`"secret"`)), "no Secret-shaped field: %s", string(b))
	assert.False(t, bytes.Contains(b, []byte(`"reveal"`)), "no Reveal-shaped field: %s", string(b))

	// Case B: blank credential — credential_id field omitted entirely.
	trig2 := &Trigger{
		FlowName: "x",
		Source:   &fakeTSrc{kindName: "ext.k", configB: []byte(`{}`)},
	}
	b2, err := json.Marshal(trig2)
	require.NoError(t, err)
	assert.False(t, bytes.Contains(b2, []byte("credential_id")), "credential_id must be omitted when blank: %s", string(b2))
}

// --- Test 4: round-trip Marshal -> Unmarshal preserves IDs ------------------

func TestTrigger_UnmarshalRoundTrip(t *testing.T) {
	prev := unmarshalTriggerSource
	t.Cleanup(func() { unmarshalTriggerSource = prev })
	RegisterTriggerSourceUnmarshaler(func(data []byte) (TriggerSource, error) {
		var env struct {
			Kind   string          `json:"kind"`
			Config json.RawMessage `json:"config"`
		}
		if err := json.Unmarshal(data, &env); err != nil {
			return nil, err
		}
		return &fakeTSrc{kindName: env.Kind, configB: env.Config}, nil
	})

	src := &fakeTSrc{kindName: "fake.webhook", configB: []byte(`{"path":"/hook"}`)}
	trig := &Trigger{
		FlowName:          "check_user",
		Source:            src,
		MapLambda:         &CapturedLambda{ID: "abc:1:2"},
		IdempotencyLambda: &CapturedLambda{ID: "def:3:4"},
		CredentialID:      "gh-secret",
	}
	b, err := json.Marshal(trig)
	require.NoError(t, err)

	var got Trigger
	require.NoError(t, json.Unmarshal(b, &got))

	assert.Equal(t, "check_user", got.FlowName)
	require.NotNil(t, got.MapLambda)
	assert.Equal(t, "abc:1:2", got.MapLambda.ID)
	require.NotNil(t, got.IdempotencyLambda)
	assert.Equal(t, "def:3:4", got.IdempotencyLambda.ID)
	assert.Equal(t, "gh-secret", got.CredentialID)
	require.NotNil(t, got.Source)
	assert.Equal(t, "fake.webhook", got.Source.Kind())

	// Pos zero on the unmarshaled value (matches dag.ActionRef precedent).
	assert.False(t, got.Pos.IsValid(), "Pos must be zero on round-tripped Trigger; got %+v", got.Pos)
}

func TestTrigger_UnmarshalRoundTrip_NilSource(t *testing.T) {
	prev := unmarshalTriggerSource
	t.Cleanup(func() { unmarshalTriggerSource = prev })

	// Marshal a Trigger with nil Source — the wire shape contains "source":null
	// and Unmarshal accepts that without invoking the registry.
	trig := &Trigger{FlowName: "x"}
	b, err := json.Marshal(trig)
	require.NoError(t, err)
	assert.True(t, bytes.Contains(b, []byte(`"source":null`)), "nil Source must serialize as null: %s", string(b))

	var got Trigger
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, "x", got.FlowName)
	assert.Nil(t, got.Source)
}

func TestTrigger_UnmarshalRoundTrip_KindMismatchRejected(t *testing.T) {
	bad := []byte(`{"kind":"NotATrigger","flow_name":"x","source":null}`)
	var got Trigger
	err := got.UnmarshalJSON(bad)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "kind=")
}

// --- Test 5: Freeze cascades + idempotent ----------------------------------

func TestTrigger_Freeze(t *testing.T) {
	// Build two real *starlark.Function values via Starlark exec so we can
	// observe Freeze cascading into them. The execution thread is parse-time
	// only; no Temporal context involved.
	thread := &starlark.Thread{Name: "test"}
	predeclared := starlark.StringDict{}
	globals, err := starlark.ExecFile(thread, "test.star", `
m = lambda x: x
i = lambda x: x
`, predeclared)
	require.NoError(t, err)
	mFn, ok := globals["m"].(*starlark.Function)
	require.True(t, ok, "m must be a starlark.Function")
	iFn, ok := globals["i"].(*starlark.Function)
	require.True(t, ok, "i must be a starlark.Function")

	// Freezable Source: tracks whether Freeze was called.
	src := &freezableTSrc{fakeTSrc: fakeTSrc{kindName: "ext.k", configB: []byte(`{}`)}}

	trig := &Trigger{
		FlowName:          "x",
		Source:            src,
		MapLambda:         &CapturedLambda{ID: "m:1:1", Fn: mFn},
		IdempotencyLambda: &CapturedLambda{ID: "i:2:1", Fn: iFn},
	}
	require.False(t, trig.frozen, "Trigger starts unfrozen")
	require.False(t, src.frozen, "Source starts unfrozen")

	// First Freeze: marks trig.frozen, freezes lambdas + Source.
	require.NotPanics(t, func() { trig.Freeze() })
	assert.True(t, trig.frozen, "trigger must be marked frozen after Freeze")
	assert.True(t, src.frozen, "Source.Freeze must be called by Trigger.Freeze")

	// Second Freeze: idempotent — must not panic, must not unfreeze anything.
	src.frozen = false // simulate "should not be re-touched"
	require.NotPanics(t, func() { trig.Freeze() })
	assert.False(t, src.frozen, "Source.Freeze must NOT be re-called when Trigger already frozen")

	// Smoke: lambdas still callable for type-tag purposes (Truth doesn't panic).
	assert.NotNil(t, mFn.Truth)
	assert.NotNil(t, iFn.Truth)
}

func TestTrigger_Freeze_NilSafe(t *testing.T) {
	trig := &Trigger{FlowName: "x"} // nil Source, nil lambdas
	require.NotPanics(t, func() { trig.Freeze() })
	assert.True(t, trig.frozen)
}

// --- Compile-time guard ----------------------------------------------------

var _ TriggerSource = (*fakeTSrc)(nil)
var _ TriggerSource = (*freezableTSrc)(nil)
