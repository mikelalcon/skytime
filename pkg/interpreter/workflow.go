package interpreter

import (
	"fmt"

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

		i := newInterpreter(ctx, registry, parsed, input.ContentHash, input.InitState, logger)
		if err := i.walkBody(ctx, parsed.Flow.Body); err != nil {
			return nil, err
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
// Plan 03-02 (this plan) returns "walker not implemented yet" errors for
// every concrete node type. Plan 03-03 fills in the real walkers.
func (i *interpreter) walkBody(ctx workflow.Context, body []dag.Node) error {
	for _, node := range body {
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
	default:
		return temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("unknown node kind %s at %s", node.Kind(), node.Position()),
			"UnknownNodeType",
			nil,
		)
	}
}

// Plan 03-02 stubs — plan 03-03 replaces each function BODY (signatures
// stay identical). Each stub returns a non-retryable
// "WalkerNotImplemented" application error tagged with the node's
// position so plan 03-03 has clear "fill me in" markers and any
// accidental early consumer (e.g., a smoke test that builds a non-empty
// Body before plan 03-03 lands) sees a precise error instead of a silent
// no-op.

func (i *interpreter) walkStep(_ workflow.Context, n *dag.Step) error {
	return temporal.NewNonRetryableApplicationError(
		fmt.Sprintf("walkStep not implemented (plan 03-03): step at %s", n.Pos),
		"WalkerNotImplemented", nil)
}

func (i *interpreter) walkIfCond(_ workflow.Context, n *dag.IfCond) error {
	return temporal.NewNonRetryableApplicationError(
		fmt.Sprintf("walkIfCond not implemented (plan 03-03): if_cond at %s", n.Pos),
		"WalkerNotImplemented", nil)
}

func (i *interpreter) walkScript(_ workflow.Context, n *dag.Script) error {
	return temporal.NewNonRetryableApplicationError(
		fmt.Sprintf("walkScript not implemented (plan 03-03): script at %s", n.Pos),
		"WalkerNotImplemented", nil)
}

func (i *interpreter) walkForEach(_ workflow.Context, n *dag.ForEachParallel) error {
	return temporal.NewNonRetryableApplicationError(
		fmt.Sprintf("walkForEach not implemented (plan 03-03): for_each_parallel at %s", n.Pos),
		"WalkerNotImplemented", nil)
}

func (i *interpreter) walkCallFlow(_ workflow.Context, n *dag.CallFlow) error {
	return temporal.NewNonRetryableApplicationError(
		fmt.Sprintf("walkCallFlow not implemented (plan 03-03): call_flow at %s", n.Pos),
		"WalkerNotImplemented", nil)
}
