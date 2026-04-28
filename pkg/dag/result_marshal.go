package dag

import (
	"encoding/json"
	"fmt"
)

// ActionResult JSON wire format
// =============================
//
// Each kind marshals to a `{"kind": "<Kind>", ...}` envelope so the consumer
// (Phase 3 interpreter; ExecuteBatch test asserts via type-switch) can recover
// the concrete type after a round-trip through Temporal's default JSON
// DataConverter.
//
// Why this exists in pkg/dag (not pkg/activity)
// ----------------------------------------------
// The activity returns `[]ActionResult` from ExecuteBatch; Temporal serializes
// it via encoding/json. On the consumer side, json.Unmarshal cannot pick a
// concrete type for an interface slice without help — the discriminator
// envelope + a typed slice (ActionResults) closes the gap.
//
// Why a typed slice (ActionResults)
// ---------------------------------
// `[]ActionResult` is `[]interface{}` from json's perspective; we cannot
// extend its UnmarshalJSON behavior. The typed slice ActionResults has a
// pointer-receiver UnmarshalJSON that scans each element's `kind` field and
// dispatches to the right concrete type. The activity still returns
// `[]ActionResult` for API symmetry; consumers (tests, Phase 3 interpreter)
// decode into ActionResults to recover types.
//
// OkResult.Output handling
// ------------------------
// OkResult carries an OperationOutput (sealed marker interface — see
// output.go). On marshal, the concrete output type serializes via its own
// json contract. On unmarshal, we don't know the concrete type — we capture
// the raw JSON bytes in a RawOperationOutput passthrough and let the
// interpreter (Phase 3) decode them into the op-specific output type using
// the dispatch table's KwargsType-style reflection. For Phase 2 tests, the
// IsType(OkResult{}, results[0]) assertion is satisfied by the wrapper alone;
// the test does not need to inspect Output's concrete type.

// ResultKind* constants are the discriminator strings emitted by each
// ActionResult kind's MarshalJSON. Centralized here so both the marshal and
// unmarshal paths reference the same labels.
const (
	ResultKindOk            = "Ok"
	ResultKindRetryable     = "RetryableErr"
	ResultKindNonRetryable  = "NonRetryableErr"
	ResultKindSkipped       = "Skipped"
)

// RawOperationOutput is the placeholder OperationOutput that
// ActionResults.UnmarshalJSON puts in OkResult.Output when the concrete
// output type is unknown at decode time. The Phase 3 interpreter will read
// the dispatch table to determine the op's typed Output and re-decode
// Bytes into it.
//
// Phase 2 tests that need to inspect Output should bypass the round-trip
// (decode the raw payload directly in the test) — this type exists so the
// envelope decode never fails on the unknown-type problem.
type RawOperationOutput struct {
	// Bytes is the raw JSON of the op's output payload as it crossed the
	// wire. Empty when OkResult.Output was nil at marshal time.
	Bytes json.RawMessage `json:"bytes,omitempty"`
}

// IsOperationOutput satisfies the marker interface so RawOperationOutput
// is a legal OperationOutput value (used as a placeholder during decode).
func (RawOperationOutput) IsOperationOutput() {}

// Compile-time guarantee.
var _ OperationOutput = RawOperationOutput{}

// okResultJSON is the marshal-time shape of OkResult. The Output field is
// emitted as raw JSON via json.RawMessage so any concrete OperationOutput
// type can serialize without the envelope needing to know its schema.
type okResultJSON struct {
	Kind   string          `json:"kind"`
	Idx    int             `json:"idx"`
	Output json.RawMessage `json:"output,omitempty"`
}

// MarshalJSON emits OkResult with the "Ok" discriminator. Output is serialized
// via its own json contract and embedded as a raw message.
func (r OkResult) MarshalJSON() ([]byte, error) {
	var rawOut json.RawMessage
	if r.Output != nil {
		b, err := json.Marshal(r.Output)
		if err != nil {
			return nil, fmt.Errorf("OkResult.Output marshal: %w", err)
		}
		rawOut = b
	}
	return json.Marshal(okResultJSON{
		Kind:   ResultKindOk,
		Idx:    r.Idx,
		Output: rawOut,
	})
}

// errResultJSON is the marshal-time shape for the two error kinds. The Err
// field is emitted as a string (err.Error()) — error values are not
// JSON-serializable in general, and the wire-format consumer only needs the
// human-readable message.
type errResultJSON struct {
	Kind string `json:"kind"`
	Idx  int    `json:"idx"`
	Err  string `json:"err"`
}

// MarshalJSON emits RetryableErrResult with the "RetryableErr" discriminator.
// Err is rendered as its Error() string (the Phase 2 activity short-circuits
// on retryable mid-batch failures per D2-13, so this kind is not emitted in
// the v1 result list — but the marshal path is implemented for completeness
// since the type spine commits to it).
func (r RetryableErrResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(errResultJSON{
		Kind: ResultKindRetryable,
		Idx:  r.Idx,
		Err:  errString(r.Err),
	})
}

// MarshalJSON emits NonRetryableErrResult with the "NonRetryableErr"
// discriminator. Err is rendered as its Error() string.
func (r NonRetryableErrResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(errResultJSON{
		Kind: ResultKindNonRetryable,
		Idx:  r.Idx,
		Err:  errString(r.Err),
	})
}

// skippedResultJSON is the marshal-time shape of SkippedResult.
type skippedResultJSON struct {
	Kind   string `json:"kind"`
	Idx    int    `json:"idx"`
	Reason string `json:"reason"`
}

// MarshalJSON emits SkippedResult with the "Skipped" discriminator.
func (r SkippedResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(skippedResultJSON{
		Kind:   ResultKindSkipped,
		Idx:    r.Idx,
		Reason: r.Reason,
	})
}

// errString returns err.Error() or "" if err is nil.
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// errFromString is the inverse of errString — empty string returns nil,
// any other returns an error wrapping the message verbatim. The wire
// representation deliberately loses the typed-error chain (errors.Is /
// errors.As won't recover); consumers that need the original error type
// must inspect the activity's return value directly (not through the
// JSON round-trip).
func errFromString(s string) error {
	if s == "" {
		return nil
	}
	return errMsg(s)
}

// errMsg is a minimal error type that carries a static message. Exported
// methods only — the consumer can match on the message string but not on
// the original Go type (which is by design; error chains do not survive
// JSON serialization).
type errMsg string

func (e errMsg) Error() string { return string(e) }

// ActionResults is the typed-slice companion to ActionResult that supports
// JSON round-trip via custom UnmarshalJSON. The activity returns
// `[]ActionResult` from ExecuteBatch; consumers that need to deserialize the
// payload into typed kinds use this slice type.
//
// Usage:
//
//	var results dag.ActionResults
//	err := encoded.Get(&results)
//	// results[0].(dag.OkResult), results[1].(dag.NonRetryableErrResult), ...
//
// The slice is a `[]ActionResult` underneath, so direct iteration works
// the same way; the only difference is the UnmarshalJSON method.
type ActionResults []ActionResult

// MarshalJSON delegates to the default slice marshaling (each element's
// own MarshalJSON is invoked, producing the discriminated envelope).
// Provided so the symmetry with UnmarshalJSON is explicit; without it,
// json.Marshal still produces the same output via the slice's elements.
func (r ActionResults) MarshalJSON() ([]byte, error) {
	if r == nil {
		return []byte("null"), nil
	}
	// Detour through []ActionResult so we don't recurse into ActionResults's
	// MarshalJSON. Each element's typed MarshalJSON drives the envelope.
	plain := []ActionResult(r)
	return json.Marshal(plain)
}

// UnmarshalJSON reads the JSON discriminated envelope produced by each
// kind's MarshalJSON and dispatches to the right concrete type. Unknown
// "kind" values produce an error so a future kind addition without an
// UnmarshalJSON update fails loudly rather than silently dropping results.
func (r *ActionResults) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*r = nil
		return nil
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(data, &raws); err != nil {
		return fmt.Errorf("ActionResults: %w", err)
	}
	out := make(ActionResults, 0, len(raws))
	for i, raw := range raws {
		ar, err := UnmarshalActionResult(raw)
		if err != nil {
			return fmt.Errorf("ActionResults[%d]: %w", i, err)
		}
		out = append(out, ar)
	}
	*r = out
	return nil
}

// UnmarshalActionResult decodes one JSON envelope into the concrete
// ActionResult kind named by the "kind" discriminator. Used by
// ActionResults.UnmarshalJSON; exported so callers that have a single
// payload can decode it without wrapping in a slice.
func UnmarshalActionResult(data []byte) (ActionResult, error) {
	var head struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, fmt.Errorf("ActionResult: read kind: %w", err)
	}
	switch head.Kind {
	case ResultKindOk:
		var v okResultJSON
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("ActionResult Ok: %w", err)
		}
		var out OperationOutput
		if len(v.Output) > 0 && string(v.Output) != "null" {
			out = RawOperationOutput{Bytes: v.Output}
		}
		return OkResult{Idx: v.Idx, Output: out}, nil
	case ResultKindRetryable:
		var v errResultJSON
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("ActionResult RetryableErr: %w", err)
		}
		return RetryableErrResult{Idx: v.Idx, Err: errFromString(v.Err)}, nil
	case ResultKindNonRetryable:
		var v errResultJSON
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("ActionResult NonRetryableErr: %w", err)
		}
		return NonRetryableErrResult{Idx: v.Idx, Err: errFromString(v.Err)}, nil
	case ResultKindSkipped:
		var v skippedResultJSON
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("ActionResult Skipped: %w", err)
		}
		return SkippedResult{Idx: v.Idx, Reason: v.Reason}, nil
	default:
		return nil, fmt.Errorf("ActionResult: unknown kind %q", head.Kind)
	}
}
