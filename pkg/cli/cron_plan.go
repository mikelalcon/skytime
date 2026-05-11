package cli

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/spf13/cobra"
	"go.temporal.io/sdk/client"

	skycore "github.com/mikelalcon/skytime/pkg/extension/builtin/core"
	"github.com/mikelalcon/skytime/pkg/extension/schedules"
)

// newCronPlanCommand returns the `skytime cron-plan` subcommand.
//
// SCHED-03 dry-run mode (D-7.2-09): reads --rootdir for .star files,
// lists Skytime-managed Schedules on the connected Temporal cluster,
// prints the diff (creates / updates / deletes), and exits 0. NO cluster
// mutations.
//
// Operators run this out-of-band: pre-deploy script in CI, manual review
// before `--cron-reconcile`, ad-hoc debugging. Invocation pattern is the
// operator's call — Skytime ships the subcommand, not the deployment
// topology.
func newCronPlanCommand(cfg *config) *cobra.Command {
	var (
		rootdir      string
		taskQueue    string
		credfilePath string
		jsonLog      bool
	)

	cmd := &cobra.Command{
		Use:   "cron-plan",
		Short: "Diff parsed cron triggers against the cluster's Skytime-managed Schedules (dry-run)",
		Long: "Reads .star files in --rootdir, lists Schedule resources on the " +
			"connected Temporal cluster whose IDs start with 'skytime/', and " +
			"prints the create/update/delete plan that 'skytime server --cron-reconcile' " +
			"would apply. NO cluster mutations. Use this for pre-deploy review " +
			"or to confirm what reconciliation will change.",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := setupServerLogging(cfg.debug, jsonLog)

			// credfile sanity check (mirrors server.go's handling).
			if credfilePath != "" {
				if cfg.credHandler == nil {
					return fmt.Errorf("--credfile=%s requires the binary to be built with cli.WithCredentialHandler (see docs/cli-binary.md)", credfilePath)
				}
				setter, ok := cfg.credHandler.(interface{ SetCredfilePath(string) error })
				if !ok {
					return fmt.Errorf("--credfile=%s: this binary's credential handler does not support runtime path overrides", credfilePath)
				}
				if err := setter.SetCredfilePath(credfilePath); err != nil {
					return fmt.Errorf("--credfile=%s: %w", credfilePath, err)
				}
			}

			// Parse the rootdir BEFORE dialing Temporal — fast feedback on
			// parse errors without paying the network round-trip cost.
			flowReg, trigReg, err := loadRegistries(cmd.Context(), rootdir, cfg.exts)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "load rootdir: %s\n", err.Error())
				return errSilent
			}

			c, err := connectClient(cfg)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "connect: %s\n", err.Error())
				return errSilent
			}
			defer c.Close()

			var sc client.ScheduleClient
			if cfg.scheduleFactory != nil {
				sc = cfg.scheduleFactory(c)
			} else {
				sc = c.ScheduleClient()
			}

			plan, err := schedules.ReconcileCronSchedules(
				cmd.Context(),
				sc,
				trigReg.All(),
				flowReg,
				taskQueue,
				false, // apply=false (dry-run)
				logger,
			)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "cron-plan failed: %s\n", err.Error())
				return errSilent
			}

			renderCronPlan(logger, cmd.OutOrStdout(), plan, rootdir, jsonLog)
			return nil
		},
	}

	cmd.Flags().StringVar(&rootdir, "rootdir", "", "directory containing .star files (required)")
	cmd.Flags().StringVar(&taskQueue, "task-queue", "skytime", "Temporal task queue (used for canonical action shape)")
	cmd.Flags().StringVar(&credfilePath, "credfile", "", "credential file path (overrides binary's build-time default)")
	cmd.Flags().BoolVar(&jsonLog, "json-log", false, "emit plan output as JSON instead of charm-log Bazel-style")
	_ = cmd.MarkFlagRequired("rootdir")
	return cmd
}

// renderCronPlan emits the plan in two modes:
//
//   - default: terraform-style human-readable block to `w` (stdout), plus
//     one-line slog breadcrumbs ("cron-plan reading", "cron-plan summary")
//     to stderr for operators tailing logs.
//   - jsonLog=true: one slog.Info record per entry so JSON consumers can
//     stream the records; no pretty output to `w`.
//
// Either mode logs "cron-plan summary" via slog so operators see a single
// machine-tailable line regardless of formatting choice.
func renderCronPlan(logger *slog.Logger, w io.Writer, plan *schedules.Plan, rootdir string, jsonLog bool) {
	logger.Info("cron-plan reading", "rootdir", rootdir)

	if jsonLog {
		emitCronPlanRecords(logger, plan)
	} else {
		writeCronPlanPretty(w, plan)
	}

	logger.Info("cron-plan summary",
		"creates", len(plan.Creates),
		"updates", len(plan.Updates),
		"deletes", len(plan.Deletes),
		"applied", false)
}

// emitCronPlanRecords writes one slog record per plan entry. Used when
// --json-log is set so machine consumers get a parseable stream.
func emitCronPlanRecords(logger *slog.Logger, plan *schedules.Plan) {
	logger.Info("cluster Skytime-managed schedules",
		"creates", len(plan.Creates),
		"updates", len(plan.Updates),
		"deletes", len(plan.Deletes))

	for _, t := range plan.Creates {
		attrs := []any{
			"action", "CREATE",
			"schedule_id", schedules.ComposeScheduleID(t),
			"flow", t.FlowName,
		}
		if src, ok := t.Source.(*skycore.CronSource); ok {
			attrs = append(attrs,
				"cron", src.Schedule(),
				"timezone", src.Timezone(),
				"overlap", src.Overlap())
		}
		logger.Info("cron-plan entry", attrs...)
	}
	for _, up := range plan.Updates {
		logger.Info("cron-plan entry",
			"action", "UPDATE",
			"schedule_id", up.ScheduleID,
			"flow", up.Trigger.FlowName,
			"reason", up.Reason)
	}
	for _, id := range plan.Deletes {
		logger.Info("cron-plan entry",
			"action", "DELETE",
			"schedule_id", id,
			"reason", "no matching trigger in registry")
	}
}

// writeCronPlanPretty renders a terraform-plan-style block:
//
//	Plan: 1 to add, 0 to change, 1 to destroy.
//
//	  + skytime/weekly_digest/a1b2c3d4
//	      flow      weekly_digest
//	      schedule  0 9 * * 1 (America/New_York)
//	      overlap   skip
//
//	  - skytime/old_flow/deadbeef
//	      reason    no matching trigger in registry
//
//	Dry-run: no changes applied. Run `skytime server --cron-reconcile` to apply.
func writeCronPlanPretty(w io.Writer, plan *schedules.Plan) {
	nC, nU, nD := len(plan.Creates), len(plan.Updates), len(plan.Deletes)

	fmt.Fprintln(w)
	fmt.Fprintf(w, "Plan: %d to add, %d to change, %d to destroy.\n\n", nC, nU, nD)

	if nC+nU+nD == 0 {
		fmt.Fprintln(w, "No changes. Cluster schedules match parsed triggers.")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Dry-run: no changes applied.")
		return
	}

	for _, t := range plan.Creates {
		fmt.Fprintf(w, "  + %s\n", schedules.ComposeScheduleID(t))
		fmt.Fprintf(w, "      flow      %s\n", t.FlowName)
		if src, ok := t.Source.(*skycore.CronSource); ok {
			writeCronScheduleLines(w, src)
		}
		fmt.Fprintln(w)
	}
	for _, up := range plan.Updates {
		fmt.Fprintf(w, "  ~ %s\n", up.ScheduleID)
		fmt.Fprintf(w, "      flow      %s\n", up.Trigger.FlowName)
		if src, ok := up.Trigger.Source.(*skycore.CronSource); ok {
			writeCronScheduleLines(w, src)
		}
		fmt.Fprintf(w, "      reason    %s\n", up.Reason)
		fmt.Fprintln(w)
	}
	for _, id := range plan.Deletes {
		fmt.Fprintf(w, "  - %s\n", id)
		fmt.Fprintln(w, "      reason    no matching trigger in registry")
		fmt.Fprintln(w)
	}

	fmt.Fprintln(w, "Dry-run: no changes applied. Run `skytime server --cron-reconcile` to apply.")
}

// writeCronScheduleLines emits the schedule + (optional) when + overlap
// rows under a Create/Update entry. The "when" row is printed only if
// describeCron recognized the expression — otherwise the raw schedule
// stands on its own.
func writeCronScheduleLines(w io.Writer, src *skycore.CronSource) {
	fmt.Fprintf(w, "      schedule  %s (%s)\n", src.Schedule(), src.Timezone())
	if when := describeCron(src.Schedule()); when != "" {
		fmt.Fprintf(w, "      when      %s\n", when)
	}
	fmt.Fprintf(w, "      overlap   %s\n", src.Overlap())
}
