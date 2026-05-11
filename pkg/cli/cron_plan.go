package cli

import (
	"fmt"
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

			renderCronPlan(logger, plan, rootdir)
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

// renderCronPlan emits the plan via the supplied slog.Logger. The output
// shape is identical regardless of --json-log; the only thing the flag
// controls is the underlying handler (charm-log Bazel-style by default;
// JSON via slog.NewJSONHandler when --json-log is set, plumbed by
// setupServerLogging upstream).
//
// One slog.Info record per entry (one per create / update / delete) so
// charm-log produces one bullet per line and JSON consumers can stream
// the records.
//
// Output matches RESEARCH.md § Specifics example:
//
//	cron-plan reading {rootdir}
//	cluster Skytime-managed schedules     (creates, updates, deletes)
//	cron-plan entry action=CREATE schedule_id=... cron=... timezone=... overlap=...
//	cron-plan entry action=UPDATE schedule_id=... reason=...
//	cron-plan entry action=DELETE schedule_id=... reason="no matching trigger in registry"
//	cron-plan summary creates=N updates=M deletes=K applied=false
func renderCronPlan(logger *slog.Logger, plan *schedules.Plan, rootdir string) {
	logger.Info("cron-plan reading", "rootdir", rootdir)
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
	logger.Info("cron-plan summary",
		"creates", len(plan.Creates),
		"updates", len(plan.Updates),
		"deletes", len(plan.Deletes),
		"applied", false)
}
