package dag

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// roundTripOutput is a test-only OperationOutput used to verify Output
// marshals through OkResult and is recoverable as a RawOperationOutput on
// the decode side.
type roundTripOutput struct {
	Got string `json:"got"`
}

func (roundTripOutput) IsOperationOutput() {}

// TestActionResult_MarshalJSON_OkResult — OkResult emits the "Ok"
// discriminator and embeds Output as a raw JSON object.
func TestActionResult_MarshalJSON_OkResult(t *testing.T) {
	r := OkResult{Idx: 3, Output: roundTripOutput{Got: "hello"}}
	b, err := json.Marshal(r)
	require.NoError(t, err)
	require.JSONEq(t, `{"kind":"Ok","idx":3,"output":{"got":"hello"}}`, string(b))
}

// TestActionResult_MarshalJSON_OkResult_NilOutput — OkResult.Output may be
// nil; the envelope omits "output" or emits it as null without panicking.
func TestActionResult_MarshalJSON_OkResult_NilOutput(t *testing.T) {
	r := OkResult{Idx: 0, Output: nil}
	b, err := json.Marshal(r)
	require.NoError(t, err)
	// "output" is omitted via omitempty when no payload was attached.
	require.JSONEq(t, `{"kind":"Ok","idx":0}`, string(b))
}

// TestActionResult_MarshalJSON_RetryableErr / NonRetryableErr — both
// emit their respective discriminator + idx + err string.
func TestActionResult_MarshalJSON_RetryableErr(t *testing.T) {
	r := RetryableErrResult{Idx: 1, Err: errors.New("transient")}
	b, err := json.Marshal(r)
	require.NoError(t, err)
	require.JSONEq(t, `{"kind":"RetryableErr","idx":1,"err":"transient"}`, string(b))
}

func TestActionResult_MarshalJSON_NonRetryableErr(t *testing.T) {
	r := NonRetryableErrResult{Idx: 2, Err: errors.New("permanent")}
	b, err := json.Marshal(r)
	require.NoError(t, err)
	require.JSONEq(t, `{"kind":"NonRetryableErr","idx":2,"err":"permanent"}`, string(b))
}

// TestActionResult_MarshalJSON_Skipped — Skipped emits its idx and reason.
func TestActionResult_MarshalJSON_Skipped(t *testing.T) {
	r := SkippedResult{Idx: 5, Reason: "cancelled"}
	b, err := json.Marshal(r)
	require.NoError(t, err)
	require.JSONEq(t, `{"kind":"Skipped","idx":5,"reason":"cancelled"}`, string(b))
}

// TestActionResults_RoundTrip_AllKinds — the slice round-trips through
// json.Marshal + UnmarshalJSON and recovers each concrete kind.
func TestActionResults_RoundTrip_AllKinds(t *testing.T) {
	in := ActionResults{
		OkResult{Idx: 0, Output: roundTripOutput{Got: "ok"}},
		RetryableErrResult{Idx: 1, Err: errors.New("retry me")},
		NonRetryableErrResult{Idx: 2, Err: errors.New("perm")},
		SkippedResult{Idx: 3, Reason: "skip"},
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)

	var out ActionResults
	require.NoError(t, json.Unmarshal(b, &out))
	require.Len(t, out, 4)

	// Kind 0: OkResult — Output is now a RawOperationOutput placeholder
	// (the consumer has to know the typed Output type to decode further).
	ok, isOk := out[0].(OkResult)
	require.True(t, isOk, "got %T", out[0])
	require.Equal(t, 0, ok.Idx)
	raw, isRaw := ok.Output.(RawOperationOutput)
	require.True(t, isRaw, "Output should decode to RawOperationOutput placeholder, got %T", ok.Output)
	// The raw bytes should match the original payload shape — round-trip
	// the placeholder back into the typed output to confirm.
	var got roundTripOutput
	require.NoError(t, json.Unmarshal(raw.Bytes, &got))
	require.Equal(t, "ok", got.Got)

	// Kind 1: RetryableErr — Err is now an errMsg with the original message.
	rerr, isRet := out[1].(RetryableErrResult)
	require.True(t, isRet, "got %T", out[1])
	require.Equal(t, 1, rerr.Idx)
	require.EqualError(t, rerr.Err, "retry me")

	// Kind 2: NonRetryableErr.
	nrerr, isNon := out[2].(NonRetryableErrResult)
	require.True(t, isNon, "got %T", out[2])
	require.Equal(t, 2, nrerr.Idx)
	require.EqualError(t, nrerr.Err, "perm")

	// Kind 3: Skipped.
	skip, isSkip := out[3].(SkippedResult)
	require.True(t, isSkip, "got %T", out[3])
	require.Equal(t, 3, skip.Idx)
	require.Equal(t, "skip", skip.Reason)
}

// TestActionResults_UnmarshalJSON_UnknownKindFails — defense-in-depth: a
// future kind addition without an UnmarshalActionResult update must fail
// loudly rather than silently drop results.
func TestActionResults_UnmarshalJSON_UnknownKindFails(t *testing.T) {
	bad := []byte(`[{"kind":"Mystery","idx":0}]`)
	var out ActionResults
	err := json.Unmarshal(bad, &out)
	require.Error(t, err)
	require.Contains(t, err.Error(), `unknown kind "Mystery"`)
}

// TestActionResults_UnmarshalJSON_Null — "null" decodes to nil slice.
func TestActionResults_UnmarshalJSON_Null(t *testing.T) {
	var out ActionResults
	require.NoError(t, json.Unmarshal([]byte("null"), &out))
	require.Nil(t, out)
}

// TestActionResults_MarshalJSON_NilSlice — nil ActionResults marshals to
// "null" (matches encoding/json's default for nil slices).
func TestActionResults_MarshalJSON_NilSlice(t *testing.T) {
	var r ActionResults
	b, err := json.Marshal(r)
	require.NoError(t, err)
	require.Equal(t, "null", string(b))
}

// TestRawOperationOutput_ImplementsOperationOutput — the placeholder is
// itself a legal OperationOutput so OkResult can hold it post-decode.
func TestRawOperationOutput_ImplementsOperationOutput(t *testing.T) {
	var out OperationOutput = RawOperationOutput{Bytes: []byte(`{"a":1}`)}
	require.NotNil(t, out)
}

// TestUnmarshalActionResult_Single — the exported single-payload entry
// point decodes one envelope without slice wrapping.
func TestUnmarshalActionResult_Single(t *testing.T) {
	payload := []byte(`{"kind":"Skipped","idx":7,"reason":"x"}`)
	got, err := UnmarshalActionResult(payload)
	require.NoError(t, err)
	skip, ok := got.(SkippedResult)
	require.True(t, ok)
	require.Equal(t, 7, skip.Idx)
	require.Equal(t, "x", skip.Reason)
}

// TestActionResult_OkResult_NilOutputRoundTrip — OkResult{Output:nil}
// round-trips with Output remaining nil after decode.
func TestActionResult_OkResult_NilOutputRoundTrip(t *testing.T) {
	in := ActionResults{OkResult{Idx: 0, Output: nil}}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var out ActionResults
	require.NoError(t, json.Unmarshal(b, &out))
	require.Len(t, out, 1)
	ok := out[0].(OkResult)
	require.Nil(t, ok.Output, "nil Output should round-trip as nil, not RawOperationOutput")
}
