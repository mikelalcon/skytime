package flowlaunch

import "testing"

func TestExecute_StatusOK(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 01 fills in. Asserts Execute returns StatusOK + workflowID when c.ExecuteWorkflow succeeds.")
}

func TestExecute_DuplicateError(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 01. Asserts Execute maps *serviceerror.WorkflowExecutionAlreadyStarted to StatusDuplicate via errors.As.")
}

func TestExecute_DispatchFailed(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 01. Asserts Execute wraps non-AlreadyStarted errors as StatusDispatchFailed with %w.")
}

func TestBuildWorkflowInput_ShapesInput(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 01. Asserts BuildWorkflowInput returns dag.WorkflowInput{FlowName, ContentHash, InitState} verbatim.")
}
