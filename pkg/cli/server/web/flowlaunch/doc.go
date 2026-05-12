// Package flowlaunch is the single production seam for
// `c.ExecuteWorkflow("SkytimeWorkflow", dag.WorkflowInput{...})` (UI-04; D-7.3-28..D-7.3-31).
//
// Call sites after Wave 1: webhook ingress (pkg/extension/receiver/handler.go),
// manual trigger POST (pkg/cli/server/web/handlers.go), cron reconcile
// (pkg/extension/schedules/schedules.go — via BuildWorkflowInput only).
//
// The firewall test `tests/firewall_execute_workflow_test.go` asserts exactly 2
// production `c.ExecuteWorkflow` call sites and exactly 3 `BuildWorkflowInput`
// call sites.
package flowlaunch
