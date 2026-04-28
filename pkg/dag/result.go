package dag

// ActionResult is the per-action outcome of one ExecuteBatch invocation.
// Sealed sum: only the four types below satisfy this interface. Phase 3's
// interpreter consumes these via type switch.
//
// Decision references:
//   - D2-01 (locked): sealed sum interface, four kinds, lives in pkg/dag.
//   - D2-02 (locked): SkippedResult is defined but unused in v1; Policy D
//     (D2-05) prevents the v1 paths that would emit it. The variant is
//     reserved for future precondition-failed / conditional-skip paths.
//
// Sealed via the unexported isActionResult() method — downstream packages
// cannot fabricate a new kind by accident. Adding a kind requires editing
// this file, which is a deliberate API-evolution gate (mirrors the
// pkg/extension.Credential pattern from Phase 1).
type ActionResult interface {
	// ActionIndex returns the index of the action in the input batch this
	// result corresponds to. Phase 3's interpreter uses this to correlate
	// results back to the originating ActionRef in the batch slice.
	ActionIndex() int

	// isActionResult is the seal. Only the four types below implement it.
	isActionResult()
}

// OkResult is the success outcome: the operation returned a typed Output
// implementing OperationOutput.
type OkResult struct {
	// Idx is the position of this action in the input batch.
	Idx int
	// Output is the typed return value from the operation. Phase 3's
	// interpreter type-switches on the concrete type.
	Output OperationOutput
}

// ActionIndex returns the input-batch position.
func (r OkResult) ActionIndex() int { return r.Idx }

// isActionResult is the sum-type seal.
func (r OkResult) isActionResult() {}

// RetryableErrResult is a failure that the activity (Phase 2) classifies as
// transient. Per D2-13, the activity does NOT collect these into the result
// slice; it returns the error to Temporal and lets the SDK retry the whole
// batch. RetryableErrResult exists in the type spine so Phase 3 (and tests)
// have a way to construct/inspect it; runtime emission is the activity's
// concern.
type RetryableErrResult struct {
	Idx int
	Err error
}

// ActionIndex returns the input-batch position.
func (r RetryableErrResult) ActionIndex() int { return r.Idx }

// isActionResult is the sum-type seal.
func (r RetryableErrResult) isActionResult() {}

// NonRetryableErrResult is a failure that the activity (Phase 2) classifies
// as terminal — Temporal does not retry. Per D2-14, the activity DOES return
// these in the result slice along with successes-so-far and SkippedResult
// placeholders for actions after the failure. The Phase 3 interpreter
// decides whether the workflow itself fails.
//
// Examples (D2-12): handler returned an error wrapping
// extension.ErrUnknownCredential; an op returned a typed
// non-retryable wrapper; the input violates a sanity check (e.g.,
// mixed-idempotency batch reaching the activity in violation of D2-05).
type NonRetryableErrResult struct {
	Idx int
	Err error
}

// ActionIndex returns the input-batch position.
func (r NonRetryableErrResult) ActionIndex() int { return r.Idx }

// isActionResult is the sum-type seal.
func (r NonRetryableErrResult) isActionResult() {}

// SkippedResult marks an action that was not executed. D2-02: defined for
// type-spine completeness, NOT emitted by any v1 code path (Policy D
// prevents the conditions that would justify a skip). Reserved for future
// preconditions, conditional steps, or post-failure tail-of-batch
// placeholders if the activity ever changes its D2-14 stance.
type SkippedResult struct {
	Idx    int
	Reason string
}

// ActionIndex returns the input-batch position.
func (r SkippedResult) ActionIndex() int { return r.Idx }

// isActionResult is the sum-type seal.
func (r SkippedResult) isActionResult() {}

// Compile-time seal verification — these declarations would fail to
// compile if any concrete kind dropped its isActionResult() method.
var (
	_ ActionResult = OkResult{}
	_ ActionResult = RetryableErrResult{}
	_ ActionResult = NonRetryableErrResult{}
	_ ActionResult = SkippedResult{}
)
