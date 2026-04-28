package extension

import (
	"context"
	"reflect"
	"time"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// OperationFunc is the Go implementation of one extension operation. It runs
// INSIDE the generic activity (Phase 2), NOT inside a Temporal workflow.
//
// CRITICAL: the first parameter is context.Context (stdlib), NEVER
// workflow.Context. PROJECT.md "no context bleed" forbids exposing
// workflow.Context to extension authors — it would let extension code call
// workflow APIs from inside an activity, which is undefined behavior.
//
// Phase 2 (D2-04) narrows the return type from `any` to dag.OperationOutput.
// This is a Phase-1-to-Phase-2 backward-incompatible change: extension
// authors must declare a typed Output struct that implements OperationOutput
// (via an unexported isOperationOutput() method). Phase 1 ships no real
// extensions, so the migration is internal — the only consumers are test
// fakes that update inline.
//
// Op-author contract (D2-15): operations receive a context.Context whose
// deadline reflects min(activity StartToCloseTimeout, OperationSpec.DefaultTimeout).
// For HTTP calls, pass ctx to http.NewRequestWithContext so the transport
// cancels in flight; for CPU loops, poll ctx.Done().
//
// Returning nil for output is permitted (the typed-output marker accepts the
// nil interface value). Authors with no useful return value typically
// declare an empty struct that implements isOperationOutput() and return a
// zero-value of it for clarity, but `return nil, err` is also legal.
type OperationFunc func(ctx context.Context, args any, cred Credential) (output dag.OperationOutput, err error)

// OperationSpec is the registration record for one operation.
//
// Idempotent is *bool (NOT bool) so registration can detect "author forgot to
// declare" (Idempotent == nil) vs. "author explicitly chose false"
// (*Idempotent == false). The Registry rejects nil — D-12 "required, no
// default". Authors write either:
//
//	Idempotent: extension.Ptr(true)
//	Idempotent: extension.Ptr(false)
//
// at the registration site.
type OperationSpec struct {
	// Name is the operation name as Starlark callers see it
	// (e.g. "create_issue"). Matches the attribute name on the extension's
	// Starlark module.
	Name string

	// Idempotent is REQUIRED. Registry.Register returns ErrIdempotentRequired
	// if nil. *bool (not bool) so "missing" is distinguishable from "false".
	Idempotent *bool

	// Func is the Go implementation invoked by Phase 2's generic activity.
	Func OperationFunc

	// KwargsType is a struct type with `star:"name,required"` tags;
	// schema.ParseSchema reflects on it to build the FieldSpec list the
	// parser uses to validate kwargs at *starlark.Builtin call sites.
	KwargsType reflect.Type

	// DefaultTimeout is the per-action timeout (D2-15). Zero means no
	// per-action timeout enforced — only the activity-level
	// StartToCloseTimeout applies. The Phase 3 interpreter sums these
	// up to compute the activity StartToCloseTimeout per D2-15.
	//
	// Op authors that have a known upper bound (e.g., HTTP request to a
	// remote API: 30s) should set this. Op authors that don't know
	// (e.g., long-running file-system scans) should leave it zero and
	// let the workflow author set the activity-level timeout.
	DefaultTimeout time.Duration
}

// Ptr is a helper for constructing *T literals at registration sites.
// Most useful for Idempotent: `Idempotent: extension.Ptr(true)`.
func Ptr[T any](v T) *T { return &v }
