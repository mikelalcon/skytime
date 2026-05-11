// Package schedules is the boot-time reconciler that translates Phase
// 7.2 cron triggers (declared via core.cron(...) in .star files) into
// Temporal Schedule resources on the connected cluster.
//
// Two CLI surfaces consume ReconcileCronSchedules (Plan 03):
//
//   - "skytime server --cron-reconcile" — apply=true: full
//     create/update/delete of Skytime-managed Schedules. Designated
//     replica only (operator opts in via flag per D-7.2-10).
//   - "skytime cron-plan" — apply=false: read-only diff; prints the
//     plan and exits 0 (D-7.2-09).
//
// Isolation: the reconciler ONLY operates on Schedule IDs prefixed with
// "skytime/" (D-7.2-05). User-created Schedules without this prefix are
// categorically out of scope — they are filtered out of the "actual"
// side of the diff in listSkytimeManaged.
//
// Drift detection: the user's canonical config (cron + timezone +
// overlap + catchup + the flow's ContentHash) is stored in Memo at
// Create time (Pitfall 1 strategy B). List+Memo gives drift detection
// in one round trip; no per-Schedule Describe calls in steady state.
//
// ContentHash drift: editing a flow .star file changes its ContentHash;
// the cron config itself may be unchanged. The canonical comparison
// INCLUDES the ContentHash so the reconciler emits an Update on every
// flow edit (Pitfall 7) — the Schedule's persisted Action.Args must
// always carry the live ContentHash so fired workflows look up the
// correct FlowRegistry entry.
//
// Memo staleness after Update (SDK limitation): the Go SDK's
// ScheduleHandle.Update gRPC call does not include the Memo field, so
// after an Update the cluster's Memo keeps the OLD canonical bytes. On
// the next boot, diff sees the same drift and emits the same Update
// again — idempotent thrash bounded to one redundant Update per boot
// per drifted schedule. Functionally correct (the Update payload is
// byte-identical and replaces all the load-bearing fields:
// Spec/Action/Policy); regression-pinned by
// TestReconcile_UpdateMemoStaysStale_DocumentedLimitation. Delete-and-
// recreate is NOT a fix — it loses operator-set State preservation.
// Revisit when the SDK gains UpdateMemo support.
//
// Failure model: per-schedule failures aggregate via errors.Join
// (D-7.2-12 + RESEARCH.md Pattern 4); AlreadyExists on Create is logged
// as Warn and non-fatal (Pitfall 5); no in-process retry (D-7.2-12 —
// K8s CrashLoopBackoff is the retry layer).
package schedules
