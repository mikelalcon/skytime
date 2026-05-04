package interpreter

import (
	"fmt"

	"go.starlark.net/starlark"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// NewWorkflow returns the SkytimeWorkflow function bound to a frozen
// FlowRegistry. The worker bootstrap (pkg/worker, plan 03-04) calls this
// and registers the result via worker.RegisterWorkflowWithOptions(skywf,
// workflow.RegisterOptions{Name: "SkytimeWorkflow"}).
//
// The returned closure captures `registry` so per-workflow lookups happen
// against the same in-memory map at every replay tick (Option B per D3-01).
//
// On lookup miss, the workflow returns a non-retryable
// FlowNotInRegistry application error whose message includes the
// remediation path ("use Build IDs to drain old workflows") so operators
// see the right next step in Temporal's failure detail UI.
//
// Quick 260502-guu Fix B: emits flow_start at entry and flow_complete at
// exit through workflow.GetLogger(ctx); the cli's progressHandler routes
// these to the Bazel renderer.
func NewWorkflow(registry *FlowRegistry) func(workflow.Context, dag.WorkflowInput) (map[string]any, error) {
	return func(ctx workflow.Context, input dag.WorkflowInput) (map[string]any, error) {
		info := workflow.GetInfo(ctx)
		logger := workflow.GetLogger(ctx)
		logger.Info("skytime workflow start",
			"flow_name", input.FlowName,
			"content_hash", input.ContentHash,
			"binary_checksum", info.BinaryChecksum,
			"run_id", info.WorkflowExecution.RunID,
		)

		parsed, ok := registry.Lookup(input.FlowName, input.ContentHash)
		if !ok {
			return nil, temporal.NewNonRetryableApplicationError(
				fmt.Sprintf("flow %s@%s not found in worker registry; use Build IDs to drain old workflows",
					input.FlowName, input.ContentHash),
				"FlowNotInRegistry",
				nil,
			)
		}

		// Bazel renderer entry banner. workflow.Now is deterministic
		// per Temporal docs (replay-safe).
		startTime := workflow.Now(ctx)

		// D4.1-16: resolve flow.NameFn ONCE at flow_start so the
		// rendered flow_name attr reflects the lambda's output. The
		// resolution happens inside the workflow goroutine — replay-safe
		// because evalLambda is pure CPU (INTRP-03) and the captured
		// lambda body is deterministic by D-19 (frozen FreeVars).
		// Construct the interpreter BEFORE flow_start so the helper has
		// access to i.state for ctx access.
		i := newInterpreter(ctx, registry, parsed, input.ContentHash, input.InitState, logger)
		flowDisplayName := i.resolveFlowName(ctx)
		logger.Info("skytime",
			"event", "flow_start",
			"flow_name", flowDisplayName,
			"step_count", len(parsed.Flow.Body),
		)

		walkErr := i.walkBody(ctx, parsed.Flow.Body)

		// Bazel renderer exit banner. v1 simplification: ok_count is
		// the body length on success and zero on error; err_count is
		// 1 on error (the failing step) and 0 on success. Per-step
		// success/failure detail lives in step_complete events.
		endTime := workflow.Now(ctx)
		okCount := len(parsed.Flow.Body)
		errCount := 0
		if walkErr != nil {
			okCount = 0
			errCount = 1
		}
		logger.Info("skytime",
			"event", "flow_complete",
			"ok_count", okCount,
			"err_count", errCount,
			"total_ms", endTime.Sub(startTime).Milliseconds(),
		)
		if walkErr != nil {
			return nil, walkErr
		}
		return i.state.snapshot(), nil
	}
}

// interpreter is the per-workflow walker context. Plan 03-03 adds the
// per-node walker method BODIES (walkStep, walkScript, walkIfCond,
// walkForEach, walkCallFlow). This plan provides the walkBody dispatcher
// and the FINAL interpreter struct shape — plan 03-03 does NOT retrofit
// fields or signatures; only walker bodies will change.
type interpreter struct {
	ctx         workflow.Context
	registry    *FlowRegistry // for call_flow's child-flow lookup (plan 03-03)
	parsed      *ParsedFlow
	flow        *dag.Flow // alias: parsed.Flow
	contentHash string    // owning flow's content_hash; threaded for error messages and call_flow consistency (plan 03-03)
	state       *state
	logger      log.Logger

	// stepIdx, stepTot, stepPath are WALKER-LOCAL context, not shared state.
	// walkBody mutates them via save+restore, safe for single-threaded recursion.
	// walkForEach MUST shallow-copy `i` per branch (`branchInterp := *i`) and
	// mutate the COPY before spawning workflow.Go. Direct mutation of `*i` from
	// multiple goroutines is a data race + non-deterministic step numbering
	// under Temporal replay.
	stepIdx  int    // 1-indexed counter for the current sibling at this nesting level
	stepTot  int    // total siblings at this nesting level
	stepPath string // current nesting prefix; "" for top-level (renderer falls back to %d of stepIdx)
}

// newInterpreter is the FINAL signature — plan 03-03 does NOT change it.
// contentHash is required so evalLambda's error path (plan 03-03) can
// reference the owning flow's hash without re-querying the registry.
func newInterpreter(ctx workflow.Context, registry *FlowRegistry, parsed *ParsedFlow, contentHash string, initState map[string]any, logger log.Logger) *interpreter {
	return &interpreter{
		ctx:         ctx,
		registry:    registry,
		parsed:      parsed,
		flow:        parsed.Flow,
		contentHash: contentHash,
		state:       newState(initState),
		logger:      logger,
	}
}

// walkBody iterates a Node slice and dispatches each node to its walker.
// Sequential iteration — order matters per DSL semantics.
//
// Quick 260502-guu Fix B: walkBody owns sibling-counter context. It saves
// the parent's stepIdx/stepTot, sets stepTot to len(body) and stepIdx
// to k+1 inside the loop so each walker reads the right counters, then
// restores on exit. This is single-threaded mutation; concurrent fan-out
// in walkForEach uses shallow copies of `i` to avoid race + non-determinism.
func (i *interpreter) walkBody(ctx workflow.Context, body []dag.Node) error {
	savedIdx, savedTot := i.stepIdx, i.stepTot
	i.stepTot = len(body)
	defer func() { i.stepIdx, i.stepTot = savedIdx, savedTot }()
	for k, node := range body {
		i.stepIdx = k + 1
		if err := i.walkNode(ctx, node); err != nil {
			return err
		}
	}
	return nil
}

// walkNode dispatches one node to its concrete walker. Plan 03-02 stub:
// every concrete walker returns a non-retryable error tagged with the
// node Kind. Plan 03-03 replaces these stubs with real implementations.
//
// dag.Node is a sealed interface (unexported nodeMarker), so the default
// branch is structurally unreachable — kept as a defense-in-depth tagged
// error rather than a panic so any future Node addition surfaces with
// position info instead of crashing the workflow goroutine.
func (i *interpreter) walkNode(ctx workflow.Context, node dag.Node) error {
	switch n := node.(type) {
	case *dag.Step:
		return i.walkStep(ctx, n)
	case *dag.IfCond:
		return i.walkIfCond(ctx, n)
	case *dag.Script:
		return i.walkScript(ctx, n)
	case *dag.ForEachParallel:
		return i.walkForEach(ctx, n)
	case *dag.CallFlow:
		return i.walkCallFlow(ctx, n)
	case *dag.Fail:
		// D4.2-07: fail() is legal as a procedural-mode if_cond branch
		// node (procedural_demo demo in examples/skeleton/expression_if.star).
		// Expression-mode last-position fail() is dispatched directly by
		// walkIfCond and does NOT reach walkNode. raiseFail handles
		// both the literal and interpolated (MessageFn) forms.
		return i.raiseFail(ctx, n)
	case *dag.Result:
		// Defensive: result(...) is only legal as the last node of an
		// expression-mode if_cond branch. The parse-time validator
		// (validateResultPlacement + walkValidateIfCondExpression's
		// orphan detector, plans 02-03) rejects orphan Results. A
		// Result reaching walkNode means the validator regressed —
		// surface as a deterministic NonRetryableError rather than
		// silently binding to nothing.
		return temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("orphan result(...) at %s — must be the last node of an expression-mode if_cond branch", n.Pos),
			"OrphanResultNode",
			nil,
		)
	default:
		return temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("unknown node kind %s at %s", node.Kind(), node.Position()),
			"UnknownNodeType",
			nil,
		)
	}
}

// resolveFlowName returns the display name for the flow. When
// flow.NameFn is set (D4.1-16), evaluates it ONCE with the initial
// state and returns the resolved string. Otherwise returns flow.Name
// unchanged.
//
// Lambda evaluation goes through i.evalLambda → bridge.CallLambda; the
// cancellation watchdog (D3-21) and step budget (D-22) apply normally.
//
// Defensive fallbacks (in order):
//
//  1. Lambda eval error: log a warn-level "flow_name_lambda_error" event
//     and fall back to the literal flow.Name. The flow name is for
//     display only — a typo would have surfaced at parse time via
//     D4-02, so this branch should never fire in practice. We do NOT
//     fail the workflow over a display-only attribute.
//
//  2. Lambda returned a non-string value: same fallback. The desugarer
//     (D4.1-04) wraps inner expressions with str() so the lambda result
//     is always starlark.String for synthesized lambdas; this branch
//     defends against hand-built lambdas that smuggle non-strings.
func (i *interpreter) resolveFlowName(ctx workflow.Context) string {
	if i.flow.NameFn == nil {
		return i.flow.Name
	}
	val, err := i.evalLambda(ctx, i.flow.NameFn.ID)
	if err != nil {
		workflow.GetLogger(ctx).Warn("skytime",
			"event", "flow_name_lambda_error",
			"flow_name_template", i.flow.Name,
			"error", err.Error(),
		)
		return i.flow.Name
	}
	if s, ok := val.(starlark.String); ok {
		return string(s)
	}
	workflow.GetLogger(ctx).Warn("skytime",
		"event", "flow_name_lambda_type_error",
		"flow_name_template", i.flow.Name,
		"got_type", val.Type(),
	)
	return i.flow.Name
}

// currentPath returns the path attribute value to attach to step events.
// When stepPath has been set by an enclosing walker (if_cond branch,
// for_each iteration), use it verbatim; otherwise fall back to the
// numeric stepIdx so top-level events render as "[1/3]".
func (i *interpreter) currentPath() string {
	if i.stepPath != "" {
		return i.stepPath
	}
	return fmt.Sprintf("%d", i.stepIdx)
}

// Plan 03-03: walker bodies live in their own walk_*.go files. The
// dispatcher above type-switches over dag.Node and routes to:
//   - walkStep      → walk_step.go      (Task 1)
//   - walkIfCond    → walk_ifcond.go    (Task 2)
//   - walkScript    → walk_script.go    (Task 2)
//   - walkCallFlow  → walk_callflow.go  (Task 2)
//   - walkForEach   → walk_foreach.go   (Task 3)
//
// Lambda evaluation (used by walkIfCond, walkScript, walkForEach) is
// centralized in lambda_eval.go via (i *interpreter).evalLambda — wires
// the cancellation watchdog (D3-21) and the print-to-workflow-logger
// hook (D3-22) per call.
//
// All walkers are now implemented:
//   - walkStep      → walk_step.go      (Task 1)
//   - walkIfCond    → walk_ifcond.go    (Task 2)
//   - walkScript    → walk_script.go    (Task 2)
//   - walkCallFlow  → walk_callflow.go  (Task 2)
//   - walkForEach   → walk_foreach.go   (Task 3)
