package schedules

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"

	"github.com/mikelalcon/skytime/pkg/dag"
	skycore "github.com/mikelalcon/skytime/pkg/extension/builtin/core"
	"github.com/mikelalcon/skytime/pkg/interpreter"
)

// skytimeCanonicalMemoKey is the Memo key under which the reconciler
// stashes the user's canonical config JSON at Create time. Used by Diff
// to detect drift in one List call without per-Schedule Describe round
// trips (Pitfall 1 strategy B).
const skytimeCanonicalMemoKey = "skytime_canonical"

// SkytimeWorkflowType is the Temporal workflow type name the generic
// interpreter registers (Phase 3). Cron Schedules dispatch to it with
// dag.WorkflowInput{} as the single argument.
const SkytimeWorkflowType = "SkytimeWorkflow"

// Plan is the diff result returned by Diff (and ReconcileCronSchedules
// when apply=false). All three buckets are sorted ascending by Schedule
// ID for stable test/log output.
type Plan struct {
	Creates []*dag.Trigger
	Updates []*UpdatePlan
	Deletes []string
}

// UpdatePlan carries the trigger + ID + human-readable reason that an
// Update is needed. Reason is the text shown by skytime cron-plan to
// operators.
type UpdatePlan struct {
	ScheduleID string
	Trigger    *dag.Trigger
	Reason     string
}

// canonicalConfig is the deterministic JSON representation of a cron
// trigger's desired state for drift comparison. The shape includes
// cron + timezone + overlap + catchup_window AND the current
// ContentHash for the trigger's flow (Pitfall 7). The ContentHash
// inclusion is what causes the reconciler to emit an Update on every
// .star edit of a cron-triggered flow.
type canonicalConfig struct {
	Cron            string `json:"cron"`
	Timezone        string `json:"timezone"`
	Overlap         string `json:"overlap"`
	CatchupWindowNs int64  `json:"catchup_window_ns,omitempty"`
	ContentHash     string `json:"content_hash"`
}

// desiredState pairs a trigger with the JSON-encoded canonical config
// the diff compares against the cluster-side actualState.
type desiredState struct {
	trigger   *dag.Trigger
	canonical string
}

// actualState carries an existing Schedule's ID and the canonical
// config string previously stashed in its Memo. Missing/malformed Memo
// produces canonical == "", which the diff treats as drift (correct
// behavior — repair).
type actualState struct {
	id        string
	canonical string
}

// ReconcileCronSchedules walks the parsed trigger registry, lists
// Skytime-managed Schedules on the cluster, computes the diff, and
// (when apply=true) applies create/update/delete via the supplied
// ScheduleClient. Returns the Plan + any aggregated failures.
//
// Per D-7.2-12 there is no internal retry; the caller exits non-zero on
// error and K8s CrashLoopBackoff provides retry.
func ReconcileCronSchedules(
	ctx context.Context,
	sc client.ScheduleClient,
	triggers []*dag.Trigger,
	flows *interpreter.FlowRegistry,
	taskQueue string,
	apply bool,
	logger *slog.Logger,
) (*Plan, error) {
	desired, err := buildDesired(triggers, flows)
	if err != nil {
		return nil, err
	}

	actual, err := listSkytimeManaged(ctx, sc)
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}

	plan := diff(desired, actual)
	if !apply {
		return plan, nil
	}

	var errs []error
	for _, t := range plan.Creates {
		opts, err := scheduleOptionsFor(t, flows, taskQueue)
		if err != nil {
			errs = append(errs, fmt.Errorf("create %s: schedule options: %w", t.FlowName, err))
			continue
		}
		if _, err := sc.Create(ctx, opts); err != nil {
			if isAlreadyExists(err) {
				logger.Warn("schedule already exists; another reconciler beat us",
					"id", opts.ID, "error", err.Error())
				continue
			}
			errs = append(errs, fmt.Errorf("create %s: %w", opts.ID, err))
		}
	}
	for _, up := range plan.Updates {
		if err := updateSchedule(ctx, sc, up, flows, taskQueue); err != nil {
			errs = append(errs, fmt.Errorf("update %s: %w", up.ScheduleID, err))
		}
	}
	for _, id := range plan.Deletes {
		h := sc.GetHandle(ctx, id)
		if err := h.Delete(ctx); err != nil {
			errs = append(errs, fmt.Errorf("delete %s: %w", id, err))
		}
	}
	return plan, errors.Join(errs...)
}

// buildDesired walks the parsed trigger list, filters to core.cron
// triggers, and returns a map keyed by Schedule ID. The map value
// carries both the trigger AND its canonical JSON bytes (for diff).
//
// Non-cron triggers (HTTP webhooks etc.) are silently skipped — the
// reconciler is a cron-only consumer.
func buildDesired(triggers []*dag.Trigger, flows *interpreter.FlowRegistry) (map[string]desiredState, error) {
	out := map[string]desiredState{}
	for _, t := range triggers {
		if t.Source == nil || t.Source.Kind() != "core.cron" {
			continue
		}
		src, ok := t.Source.(*skycore.CronSource)
		if !ok {
			return nil, fmt.Errorf("trigger for flow %q has Source.Kind=core.cron but underlying type is %T (parser bug)", t.FlowName, t.Source)
		}
		contentHash, ok := flows.ContentHashFor(t.FlowName)
		if !ok {
			return nil, fmt.Errorf("flow %q has no resolvable content hash (registry inconsistent)", t.FlowName)
		}
		cfg := canonicalConfig{
			Cron:        src.Schedule(),
			Timezone:    src.Timezone(),
			Overlap:     src.Overlap(),
			ContentHash: contentHash,
		}
		if d, ok := src.CatchupWindow(); ok {
			cfg.CatchupWindowNs = int64(d)
		}
		canonicalBytes, err := json.Marshal(cfg)
		if err != nil {
			return nil, fmt.Errorf("canonical config for %s: %w", t.FlowName, err)
		}
		id := ComposeScheduleID(t)
		out[id] = desiredState{trigger: t, canonical: string(canonicalBytes)}
	}
	return out, nil
}

// listSkytimeManaged pages through ScheduleClient.List and returns a
// map[id]actualState containing only entries whose ID has the skytime/
// prefix. The actualState.canonical is extracted from
// entry.Memo["skytime_canonical"] via the SDK's default DataConverter
// (matches the encoding path scheduleOptionsFor uses at Create time).
// Missing/malformed Memo treats canonical as "" so the diff produces an
// Update (correct behavior — repair).
func listSkytimeManaged(ctx context.Context, sc client.ScheduleClient) (map[string]actualState, error) {
	out := map[string]actualState{}
	it, err := sc.List(ctx, client.ScheduleListOptions{})
	if err != nil {
		return nil, err
	}
	dc := converter.GetDefaultDataConverter()
	for it.HasNext() {
		entry, err := it.Next()
		if err != nil {
			return nil, err
		}
		if entry == nil {
			continue
		}
		if !IsSkytimeManaged(entry.ID) {
			continue
		}
		canonical := ""
		if entry.Memo != nil {
			if fields := entry.Memo.GetFields(); fields != nil {
				if pld, ok := fields[skytimeCanonicalMemoKey]; ok && pld != nil {
					var raw string
					// SDK's default DataConverter encodes string values
					// as JSON payloads; FromPayload reverses that. A
					// decode failure is treated as missing canonical —
					// the diff will produce an Update to repair.
					if err := dc.FromPayload(pld, &raw); err == nil {
						canonical = raw
					}
				}
			}
		}
		out[entry.ID] = actualState{id: entry.ID, canonical: canonical}
	}
	return out, nil
}

// diff computes the three buckets. Updates fire when canonical strings
// differ. Sort each bucket ascending by Schedule ID for stable
// test/log output.
func diff(desired map[string]desiredState, actual map[string]actualState) *Plan {
	plan := &Plan{}
	for id, d := range desired {
		if a, ok := actual[id]; ok {
			if d.canonical != a.canonical {
				plan.Updates = append(plan.Updates, &UpdatePlan{
					ScheduleID: id,
					Trigger:    d.trigger,
					Reason:     fmt.Sprintf("canonical config drift: desired=%s actual=%s", d.canonical, a.canonical),
				})
			}
		} else {
			plan.Creates = append(plan.Creates, d.trigger)
		}
	}
	for id := range actual {
		if _, ok := desired[id]; !ok {
			plan.Deletes = append(plan.Deletes, id)
		}
	}
	sort.Slice(plan.Creates, func(i, j int) bool {
		return ComposeScheduleID(plan.Creates[i]) < ComposeScheduleID(plan.Creates[j])
	})
	sort.Slice(plan.Updates, func(i, j int) bool {
		return plan.Updates[i].ScheduleID < plan.Updates[j].ScheduleID
	})
	sort.Strings(plan.Deletes)
	return plan
}

// scheduleOptionsFor builds the ScheduleOptions for a Create call. The
// ScheduleWorkflowAction.ID is set to "{flow}/{posHash}"; the Temporal
// server auto-appends a timestamp suffix on each fire so the resulting
// WorkflowID is "{flow}/{posHash}-<timestamp>" (Pitfall 2). The Memo
// stashes the canonical config for List-based drift detection (Pitfall
// 1 strategy B).
func scheduleOptionsFor(t *dag.Trigger, flows *interpreter.FlowRegistry, taskQueue string) (client.ScheduleOptions, error) {
	src, ok := t.Source.(*skycore.CronSource)
	if !ok {
		return client.ScheduleOptions{}, fmt.Errorf("trigger for flow %q is not a *cronSource (got %T)", t.FlowName, t.Source)
	}
	contentHash, ok := flows.ContentHashFor(t.FlowName)
	if !ok {
		return client.ScheduleOptions{}, fmt.Errorf("flow %q has no resolvable content hash", t.FlowName)
	}

	cfg := canonicalConfig{
		Cron:        src.Schedule(),
		Timezone:    src.Timezone(),
		Overlap:     src.Overlap(),
		ContentHash: contentHash,
	}
	if d, ok := src.CatchupWindow(); ok {
		cfg.CatchupWindowNs = int64(d)
	}
	canonicalBytes, err := json.Marshal(cfg)
	if err != nil {
		return client.ScheduleOptions{}, fmt.Errorf("marshal canonical config: %w", err)
	}

	workflowInput := dag.WorkflowInput{
		FlowName:    t.FlowName,
		ContentHash: contentHash,
		InitState:   nil, // map lambda eval deferred per D-7.2-20; default path (D-7.2-15)
	}

	opts := client.ScheduleOptions{
		ID: ComposeScheduleID(t),
		Spec: client.ScheduleSpec{
			CronExpressions: []string{src.Schedule()},
			TimeZoneName:    src.Timezone(),
		},
		Action: &client.ScheduleWorkflowAction{
			ID:        t.FlowName + "/" + posHash(t), // Pitfall 2: server appends "-<timestamp>"
			Workflow:  SkytimeWorkflowType,
			Args:      []interface{}{workflowInput},
			TaskQueue: taskQueue,
		},
		Overlap: mapOverlapToEnum(src.Overlap()),
		Note:    fmt.Sprintf("Skytime: %s (declared at %s)", t.FlowName, t.Pos.String()),
		Memo: map[string]interface{}{
			skytimeCanonicalMemoKey: string(canonicalBytes),
		},
	}
	if d, ok := src.CatchupWindow(); ok {
		opts.CatchupWindow = d
	}
	return opts, nil
}

// updateSchedule constructs the ScheduleUpdateOptions for an existing
// Schedule. The DoUpdate callback preserves
// input.Description.Schedule.State so operator-set pause/note survives
// reconciliation (load-bearing for production safety).
//
// SDK LIMITATION (regression-pinned by
// TestReconcile_UpdateMemoStaysStale_DocumentedLimitation): The Go
// SDK's ScheduleHandle.Update gRPC call does NOT include the Memo
// field, so the cluster-side Memo's skytime_canonical value stays stale
// after Update. This produces idempotent thrash (one redundant Update
// per boot per drifted schedule until next deploy changes the canonical
// again). Functionally correct — Update is idempotent — only one extra
// round trip per drifted schedule per boot. Documented here so the
// next reader doesn't ship a workaround (delete-and-recreate would
// lose State preservation). When the SDK gains UpdateMemo or a Memo
// field on ScheduleUpdate, revisit.
func updateSchedule(ctx context.Context, sc client.ScheduleClient, up *UpdatePlan, flows *interpreter.FlowRegistry, taskQueue string) error {
	opts, err := scheduleOptionsFor(up.Trigger, flows, taskQueue)
	if err != nil {
		return err
	}
	// Translate ScheduleOptions → the {Action, Spec, Policy} the
	// Update callback returns. Plan-level State is reused from the
	// input (operator-paused survives).
	policies := &client.SchedulePolicies{
		Overlap:       opts.Overlap,
		CatchupWindow: opts.CatchupWindow,
	}
	spec := opts.Spec
	return sc.GetHandle(ctx, up.ScheduleID).Update(ctx, client.ScheduleUpdateOptions{
		DoUpdate: func(input client.ScheduleUpdateInput) (*client.ScheduleUpdate, error) {
			return &client.ScheduleUpdate{
				Schedule: &client.Schedule{
					Action: opts.Action,
					Spec:   &spec,
					Policy: policies,
					State:  input.Description.Schedule.State,
				},
			}, nil
		},
	})
}

// mapOverlapToEnum maps the user-facing overlap string (locked 4-value
// set per D-7.2-03) to the Temporal SDK enum value. Unknown strings
// fall through to SKIP — defensive only; the parser rejects unknown
// values at parse time so this is unreachable in production.
func mapOverlapToEnum(s string) enumspb.ScheduleOverlapPolicy {
	switch s {
	case "skip":
		return enumspb.SCHEDULE_OVERLAP_POLICY_SKIP
	case "allow":
		return enumspb.SCHEDULE_OVERLAP_POLICY_ALLOW_ALL
	case "buffer_one":
		return enumspb.SCHEDULE_OVERLAP_POLICY_BUFFER_ONE
	case "cancel_other":
		return enumspb.SCHEDULE_OVERLAP_POLICY_CANCEL_OTHER
	default:
		return enumspb.SCHEDULE_OVERLAP_POLICY_SKIP
	}
}

// isAlreadyExists detects "another reconciler beat us" — Pitfall 5.
// Two replicas accidentally configured with --cron-reconcile will both
// attempt Create on the same Schedule ID; the second gets back this
// error. Non-fatal — log Warn and continue.
func isAlreadyExists(err error) bool {
	var ae *serviceerror.AlreadyExists
	return errors.As(err, &ae)
}
