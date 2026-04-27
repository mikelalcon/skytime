package extension

import (
	"context"
	"reflect"
)

// OperationFunc is the Go implementation of one extension operation. It runs
// INSIDE the generic activity (Phase 2), NOT inside a Temporal workflow.
//
// CRITICAL: the first parameter is context.Context (stdlib), NEVER
// workflow.Context. PROJECT.md "no context bleed" forbids exposing
// workflow.Context to extension authors — it would let extension code call
// workflow APIs from inside an activity, which is undefined behavior.
//
// Phase 1 only declares the type. Phase 2's generic activity calls
// OperationFunc with a stdlib context.Context derived from
// activity.Context() (which itself implements context.Context).
type OperationFunc func(ctx context.Context, args any, cred Credential) (output any, err error)

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
}

// Ptr is a helper for constructing *T literals at registration sites.
// Most useful for Idempotent: `Idempotent: extension.Ptr(true)`.
func Ptr[T any](v T) *T { return &v }
