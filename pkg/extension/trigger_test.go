package extension_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
)

// Compile-time seal proof: *FakeTriggerSource satisfies BOTH the
// extension.TriggerSource sealed interface AND the dag.TriggerSource
// structural interface. Drift in either signature breaks the build here.
var _ extension.TriggerSource = (*extension.FakeTriggerSource)(nil)
var _ dag.TriggerSource = (*extension.FakeTriggerSource)(nil)

// TestTriggerSource_Sealed asserts the FakeTriggerSource stub satisfies the
// extension.TriggerSource sealed interface (compile-time + runtime).
func TestTriggerSource_Sealed(t *testing.T) {
	var src extension.TriggerSource = &extension.FakeTriggerSource{
		KindName:  "x",
		ReqFields: []string{"a"},
	}
	require.NotNil(t, src)
	assert.Equal(t, "x", src.Kind())
	assert.Equal(t, []string{"a"}, src.ReqSchema())

	// Runtime type-assertion echoes the compile-time guarantee.
	_, ok := interface{}(src).(extension.TriggerSource)
	assert.True(t, ok, "*FakeTriggerSource must satisfy extension.TriggerSource at runtime")
}

// TestTriggerSource_dagInterfaceSatisfied confirms FakeTriggerSource also
// satisfies dag.TriggerSource (Kind + MarshalJSON only). This is the
// compile-time bridge that lets *dag.Trigger.Source hold extension values.
func TestTriggerSource_dagInterfaceSatisfied(t *testing.T) {
	var src dag.TriggerSource = &extension.FakeTriggerSource{KindName: "y"}
	require.NotNil(t, src)
	assert.Equal(t, "y", src.Kind())
	b, err := src.MarshalJSON()
	require.NoError(t, err)
	assert.Contains(t, string(b), `"kind":"y"`)
}

// TestRegisterTriggerSourceFactory_RoundTrip registers a factory for kind
// "skytime.test.roundtrip", marshals a FakeTriggerSource through
// dag.Trigger, and verifies dag.Trigger.UnmarshalJSON dispatches via the
// kind-keyed registry to recover the source.
func TestRegisterTriggerSourceFactory_RoundTrip(t *testing.T) {
	require.NoError(t, extension.RegisterTriggerSourceFactory(
		"skytime.test.roundtrip",
		func(data []byte) (extension.TriggerSource, error) {
			var cfg struct {
				ReqFields            []string `json:"req_fields"`
				CredentialIDInConfig string   `json:"credential_id"`
			}
			if err := json.Unmarshal(data, &cfg); err != nil {
				return nil, err
			}
			return &extension.FakeTriggerSource{
				KindName:             "skytime.test.roundtrip",
				ReqFields:            cfg.ReqFields,
				CredentialIDInConfig: cfg.CredentialIDInConfig,
			}, nil
		},
	))

	trig := &dag.Trigger{
		FlowName: "demo",
		Source: &extension.FakeTriggerSource{
			KindName:             "skytime.test.roundtrip",
			ReqFields:            []string{"payload", "headers"},
			CredentialIDInConfig: "my-id",
		},
		CredentialID: "my-id",
	}
	out, err := json.Marshal(trig)
	require.NoError(t, err)

	var got dag.Trigger
	require.NoError(t, json.Unmarshal(out, &got))
	require.NotNil(t, got.Source)
	assert.Equal(t, "skytime.test.roundtrip", got.Source.Kind())

	ftSrc, ok := got.Source.(*extension.FakeTriggerSource)
	require.True(t, ok, "Source should round-trip back to *FakeTriggerSource, got %T", got.Source)
	assert.Equal(t, []string{"headers", "payload"}, ftSrc.ReqSchema(),
		"ReqSchema must round-trip sorted")
	assert.Equal(t, "my-id", ftSrc.CredentialIDInConfig)
}

// TestRegisterTriggerSourceFactory_Duplicate covers the strict-collision
// contract: registering the same kind twice returns a position-aware error
// even when the function pointer would be identical.
func TestRegisterTriggerSourceFactory_Duplicate(t *testing.T) {
	fn := func(_ []byte) (extension.TriggerSource, error) { return nil, nil }
	require.NoError(t, extension.RegisterTriggerSourceFactory("skytime.test.duplicate", fn))

	err := extension.RegisterTriggerSourceFactory("skytime.test.duplicate", fn)
	require.Error(t, err)
	assert.Equal(t,
		`extension: trigger source kind "skytime.test.duplicate" already registered`,
		err.Error(),
	)
}

// TestRegisterTriggerSourceFactory_EmptyKind rejects an empty kind so
// callers can't accidentally register a wildcard.
func TestRegisterTriggerSourceFactory_EmptyKind(t *testing.T) {
	err := extension.RegisterTriggerSourceFactory("", func(_ []byte) (extension.TriggerSource, error) {
		return nil, nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "trigger source kind required")
}

// TestRegisterTriggerSourceFactory_NilFn rejects a nil factory function so
// dispatch never panics.
func TestRegisterTriggerSourceFactory_NilFn(t *testing.T) {
	err := extension.RegisterTriggerSourceFactory("skytime.test.nilfn", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "factory function required")
}

// TestExtensionTriggerUnmarshaler_NoFactory verifies that unmarshaling a
// {kind, config} envelope with an unregistered kind surfaces a
// debuggable "no factory registered" error.
func TestExtensionTriggerUnmarshaler_NoFactory(t *testing.T) {
	// Build a Trigger envelope referencing an unregistered kind.
	envelope := []byte(`{"kind":"Trigger","flow_name":"demo","source":{"kind":"skytime.test.unknown","config":{}}}`)
	var trig dag.Trigger
	err := json.Unmarshal(envelope, &trig)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no factory registered for trigger source kind "skytime.test.unknown"`)
}

// TestExtensionTriggerUnmarshaler_MissingKind rejects an envelope that
// lacks a "kind" discriminator.
func TestExtensionTriggerUnmarshaler_MissingKind(t *testing.T) {
	envelope := []byte(`{"kind":"Trigger","flow_name":"demo","source":{"config":{}}}`)
	var trig dag.Trigger
	err := json.Unmarshal(envelope, &trig)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing kind")
}

// TestFakeTriggerSource_NoSecretInConfig is the unit-level credential
// redaction test (D-07-09 / D-07-10). FakeTriggerSource.MarshalJSON must
// emit credential_id as a plain string but never produce an extension.Secret
// String() value ("<redacted>") in the bytes.
func TestFakeTriggerSource_NoSecretInConfig(t *testing.T) {
	src := &extension.FakeTriggerSource{
		KindName:             "ext.k",
		ReqFields:            []string{"payload"},
		CredentialIDInConfig: "my-id",
	}
	b, err := src.MarshalJSON()
	require.NoError(t, err)

	assert.True(t, bytes.Contains(b, []byte(`"credential_id":"my-id"`)),
		"credential_id must round-trip: %s", string(b))
	assert.False(t, bytes.Contains(b, []byte("<redacted>")),
		"no Secret String() value may appear in MarshalJSON output: %s", string(b))
	// Defense-in-depth: no field name resembling Secret/Reveal in the bytes.
	assert.False(t, strings.Contains(strings.ToLower(string(b)), "reveal"),
		"no Reveal-shaped field may appear in MarshalJSON output: %s", string(b))
}
