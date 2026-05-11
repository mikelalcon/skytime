package core

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
)

// Compile-time seal assertions for the production type.
var (
	_ extension.TriggerSource = (*cronSource)(nil)
	_ starlark.Value          = (*cronSource)(nil)
)

// callCron evaluates a core.cron(...) expression against a minimal
// Starlark module containing the cron factory. Mirrors the
// callHTTPWebhook helper in pkg/extension/builtin/http/webhook_test.go.
// The .star "filename" is fixed so callerPosition produces a usable
// Pos.Filename in *dag.ParseError values.
func callCron(t *testing.T, inExpr string) (starlark.Value, error) {
	t.Helper()

	mod := &starlarkstruct.Module{
		Name: "core",
		Members: starlark.StringDict{
			"cron": starlark.NewBuiltin("core.cron", cronFactory),
		},
	}
	predeclared := starlark.StringDict{"core": mod}

	thread := &starlark.Thread{Name: "test-cron"}
	src := "result = " + inExpr + "\n"
	globals, err := starlark.ExecFile(thread, "test_cron.star", src, predeclared)
	if err != nil {
		return nil, err
	}
	v, ok := globals["result"]
	require.True(t, ok, "expected 'result' in globals")
	return v, nil
}

// ----- happy-path parse tests -----

// TestCron_BasicParse asserts the canonical full-kwargs call succeeds
// and the result satisfies both extension.TriggerSource and exposes the
// accessor methods Plan 02's reconciler relies on.
func TestCron_BasicParse(t *testing.T) {
	v, err := callCron(t, `core.cron(schedule="0 9 * * 1", timezone="America/New_York", overlap="skip")`)
	require.NoError(t, err)
	require.NotNil(t, v)

	src, ok := v.(extension.TriggerSource)
	require.True(t, ok, "value must satisfy extension.TriggerSource; got %T", v)
	require.Equal(t, "core.cron", src.Kind())

	concrete, ok := v.(*cronSource)
	require.True(t, ok, "value must be *cronSource; got %T", v)
	require.Equal(t, "0 9 * * 1", concrete.Schedule())
	require.Equal(t, "America/New_York", concrete.Timezone())
	require.Equal(t, "skip", concrete.Overlap())

	d, present := concrete.CatchupWindow()
	require.False(t, present, "catchup_window not set ⇒ second return is false")
	require.Equal(t, time.Duration(0), d)
}

// TestCron_DefaultsApplied asserts only-required-kwarg call applies the
// documented defaults.
func TestCron_DefaultsApplied(t *testing.T) {
	v, err := callCron(t, `core.cron(schedule="0 9 * * 1")`)
	require.NoError(t, err)
	concrete := v.(*cronSource)
	require.Equal(t, "UTC", concrete.Timezone())
	require.Equal(t, "skip", concrete.Overlap())
	d, present := concrete.CatchupWindow()
	require.False(t, present)
	require.Equal(t, time.Duration(0), d)
}

// TestCron_CatchupWindowParsed asserts a valid duration string roundtrips
// to a time.Duration.
func TestCron_CatchupWindowParsed(t *testing.T) {
	v, err := callCron(t, `core.cron(schedule="0 9 * * 1", catchup_window="5m")`)
	require.NoError(t, err)
	concrete := v.(*cronSource)
	d, present := concrete.CatchupWindow()
	require.True(t, present, "catchup_window present ⇒ second return is true")
	require.Equal(t, 5*time.Minute, d)
}

// ----- parse-time rejection tests -----

// assertParseError extracts a *dag.ParseError from err (via errors.As)
// and asserts the position is attributed to the test .star file.
func assertParseError(t *testing.T, err error, wantSubstrs ...string) *dag.ParseError {
	t.Helper()
	require.Error(t, err)
	var pe *dag.ParseError
	require.True(t, errors.As(err, &pe), "expected *dag.ParseError; got %T: %v", err, err)
	for _, s := range wantSubstrs {
		require.Contains(t, pe.Msg, s, "ParseError.Msg must contain %q; got %q", s, pe.Msg)
	}
	// Position is attributed when called from a .star file (positive
	// confirmation that callerPosition pulls the call-frame, not the
	// builtin definition site).
	require.True(t, pe.Pos.IsValid(), "ParseError.Pos must be valid (call-site attribution); got %v", pe.Pos)
	require.Contains(t, pe.Pos.Filename(), "test_cron.star",
		"ParseError.Pos.Filename must point at the test .star file; got %q", pe.Pos.Filename())
	return pe
}

// TestCron_RejectSixField asserts a 6-field cron string fails with both
// the user-facing "invalid 5-field POSIX cron" prefix AND the underlying
// robfig/cron parser message.
func TestCron_RejectSixField(t *testing.T) {
	_, err := callCron(t, `core.cron(schedule="0 0 9 * * 1")`)
	pe := assertParseError(t, err, `invalid 5-field POSIX cron`, `"0 0 9 * * 1"`)
	// robfig/cron's underlying error mentions field count somewhere.
	require.Contains(t, pe.Msg, "5", "underlying parser error must reference field count")
}

// TestCron_RejectMacros asserts @hourly / @daily are rejected per
// Pitfall 6 (no cron.Descriptor flag in fiveFieldParser).
func TestCron_RejectMacros(t *testing.T) {
	for _, macro := range []string{"@hourly", "@daily"} {
		t.Run(macro, func(t *testing.T) {
			_, err := callCron(t, `core.cron(schedule="`+macro+`")`)
			assertParseError(t, err, `invalid 5-field POSIX cron`, `"`+macro+`"`)
		})
	}
}

// TestCron_RejectMalformed asserts free-form garbage is rejected.
func TestCron_RejectMalformed(t *testing.T) {
	_, err := callCron(t, `core.cron(schedule="not a cron")`)
	assertParseError(t, err, `invalid 5-field POSIX cron`, `"not a cron"`)
}

// TestCron_RejectUnknownTimezone asserts unknown IANA names produce an
// IANA-format hint in the message.
func TestCron_RejectUnknownTimezone(t *testing.T) {
	_, err := callCron(t, `core.cron(schedule="0 9 * * 1", timezone="America/Atlantis")`)
	assertParseError(t, err, `invalid timezone`, `"America/Atlantis"`, `IANA name`)
}

// TestCron_RejectInvalidOverlap asserts overlap not in the allowlist is
// rejected with a sorted valid-set rendering.
func TestCron_RejectInvalidOverlap(t *testing.T) {
	_, err := callCron(t, `core.cron(schedule="0 9 * * 1", overlap="yolo")`)
	// Sorted set per sortedKeys(): [allow buffer_one cancel_other skip].
	assertParseError(t, err, `overlap`, `"yolo"`, `[allow buffer_one cancel_other skip]`)
}

// TestCron_RejectNegativeCatchup asserts a negative duration is rejected.
func TestCron_RejectNegativeCatchup(t *testing.T) {
	_, err := callCron(t, `core.cron(schedule="0 9 * * 1", catchup_window="-1m")`)
	assertParseError(t, err, `catchup_window must be non-negative`)
}

// TestCron_RejectMalformedCatchup asserts time.ParseDuration failures
// surface as *dag.ParseError.
func TestCron_RejectMalformedCatchup(t *testing.T) {
	_, err := callCron(t, `core.cron(schedule="0 9 * * 1", catchup_window="notaduration")`)
	assertParseError(t, err, `catchup_window`, `"notaduration"`, `not a valid duration`)
}

// ----- ReqSchema + round-trip + Starlark value contract tests -----

// TestCron_ReqSchema asserts ReqSchema() returns the locked semantic-
// priority order (D-7.2-14 erratum): scheduled_time first, actual_time
// second. The walker's error-render code is responsible for alphabetical
// sorting at render time (tested in pkg/parser/trigger_test.go).
func TestCron_ReqSchema(t *testing.T) {
	v, err := callCron(t, `core.cron(schedule="0 9 * * 1")`)
	require.NoError(t, err)
	src := v.(extension.TriggerSource)
	require.Equal(t, []string{"scheduled_time", "actual_time"}, src.ReqSchema(),
		"D-7.2-14: ReqSchema() must return semantic priority order, not alphabetical")
}

// TestCron_RoundTrip asserts MarshalJSON produces the {kind, config}
// envelope shape and the registered factory rehydrates a byte-identical
// *cronSource.
func TestCron_RoundTrip(t *testing.T) {
	v, err := callCron(t, `core.cron(schedule="0 9 * * 1", timezone="America/New_York", overlap="buffer_one", catchup_window="2h")`)
	require.NoError(t, err)
	original := v.(*cronSource)

	// 1. MarshalJSON envelope shape.
	raw, err := original.MarshalJSON()
	require.NoError(t, err)

	var env map[string]any
	require.NoError(t, json.Unmarshal(raw, &env))
	require.Equal(t, "core.cron", env["kind"])
	cfg, ok := env["config"].(map[string]any)
	require.True(t, ok, "config must be a JSON object; got %T", env["config"])
	require.Equal(t, "0 9 * * 1", cfg["schedule"])
	require.Equal(t, "America/New_York", cfg["timezone"])
	require.Equal(t, "buffer_one", cfg["overlap"])
	require.Equal(t, float64(2*time.Hour), cfg["catchup_window_ns"], "catchup_window_ns must be int64 nanoseconds")

	// 2. Round-trip via dag.Trigger envelope so the kind-keyed factory
	//    dispatcher in pkg/extension/trigger_unmarshal.go does the work.
	triggerJSON := []byte(`{"kind":"Trigger","flow_name":"test_flow","source":` + string(raw) + `}`)
	var rehydrated dag.Trigger
	require.NoError(t, rehydrated.UnmarshalJSON(triggerJSON))
	require.NotNil(t, rehydrated.Source)
	require.Equal(t, "core.cron", rehydrated.Source.Kind())

	rebuilt, ok := rehydrated.Source.(*cronSource)
	require.True(t, ok, "reconstructed source must be *cronSource; got %T", rehydrated.Source)
	require.Equal(t, original.schedule, rebuilt.schedule)
	require.Equal(t, original.timezone, rebuilt.timezone)
	require.Equal(t, original.overlap, rebuilt.overlap)
	require.NotNil(t, rebuilt.catchupWindow)
	require.Equal(t, *original.catchupWindow, *rebuilt.catchupWindow)
}

// TestCron_RoundTrip_NoCatchup asserts the catchup_window field is
// omitted from the JSON config when unset (Marshal) and remains nil on
// round-trip.
func TestCron_RoundTrip_NoCatchup(t *testing.T) {
	v, err := callCron(t, `core.cron(schedule="0 9 * * 1")`)
	require.NoError(t, err)
	original := v.(*cronSource)

	raw, err := original.MarshalJSON()
	require.NoError(t, err)
	require.NotContains(t, string(raw), "catchup_window_ns",
		"catchup_window_ns must be omitted when unset")

	triggerJSON := []byte(`{"kind":"Trigger","flow_name":"f","source":` + string(raw) + `}`)
	var rehydrated dag.Trigger
	require.NoError(t, rehydrated.UnmarshalJSON(triggerJSON))
	rebuilt := rehydrated.Source.(*cronSource)
	require.Nil(t, rebuilt.catchupWindow, "round-tripped catchupWindow must remain nil when omitted")
}

// TestCron_StarlarkValue asserts the four starlark.Value methods produce
// the expected outputs.
func TestCron_StarlarkValue(t *testing.T) {
	v, err := callCron(t, `core.cron(schedule="0 9 * * 1", timezone="America/New_York")`)
	require.NoError(t, err)
	src := v.(*cronSource)

	// String includes "core.cron(schedule=" and both literals.
	s := src.String()
	require.Contains(t, s, "core.cron(schedule=")
	require.Contains(t, s, `"0 9 * * 1"`)
	require.Contains(t, s, `"America/New_York"`)

	// Type returns "core.cron".
	require.Equal(t, "core.cron", src.Type())

	// Truth is starlark.True.
	require.Equal(t, starlark.True, src.Truth())

	// Hash returns (0, error) with "unhashable" in the message.
	h, err := src.Hash()
	require.Error(t, err)
	require.Equal(t, uint32(0), h)
	require.Contains(t, err.Error(), "unhashable type: core.cron")
}
