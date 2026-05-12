package firewall_test

import "testing"

func TestExecuteWorkflow_CallSiteCount(t *testing.T) {
	// TODO(plan-01): replace with AST walk asserting exactly 2 production
	// c.ExecuteWorkflow call sites (pkg/cli/server/web/flowlaunch/launch.go
	// and pkg/cli/run.go). Mirrors tests/receiver_hmac_only_test.go pattern.
	t.Skip("Wave 0 stub: Plan 01 fills in. Asserts c.ExecuteWorkflow appears at exactly 2 production call sites: pkg/cli/server/web/flowlaunch/launch.go and pkg/cli/run.go. Mirrors tests/receiver_hmac_only_test.go AST walk pattern.")
}

func TestBuildWorkflowInput_CallSiteCount(t *testing.T) {
	// TODO(plan-01): replace with AST walk asserting exactly 3 production
	// flowlaunch.BuildWorkflowInput call sites (self in launch.go,
	// pkg/cli/run.go, pkg/extension/schedules/schedules.go).
	t.Skip("Wave 0 stub: Plan 01 fills in. Asserts flowlaunch.BuildWorkflowInput appears at exactly 3 production call sites: pkg/cli/server/web/flowlaunch/launch.go (self), pkg/cli/run.go, pkg/extension/schedules/schedules.go.")
}
