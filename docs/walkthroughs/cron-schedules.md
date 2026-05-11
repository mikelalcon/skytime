# Cron schedules walkthrough

Schedule durable workflows to run on a cron schedule, backed by
Temporal Schedules. Schedules survive worker crashes — if your worker
dies between two firings, the next firing still happens (and any
missed firings within the catchup window run when the cluster catches
up). This walkthrough takes you from a fresh `git clone` to a real
`core.cron(...)`-declared trigger applied as a Temporal Schedule on a
local dev cluster, in about ten minutes.

The flow this walkthrough drives is
[`examples/http-github-webhook/weekly_digest.star`](../../examples/http-github-webhook/weekly_digest.star).
Its cron trigger fires Mondays at 09:00 America/New_York; the optional
section near the end shows how to bump the schedule to `* * * * *` and
watch a workflow execution land in the Temporal UI.

For the 5-minute version, see
[`examples/http-github-webhook/README.md`](../../examples/http-github-webhook/README.md)
section "Cron walkthrough (5 minutes)".

## Prerequisites

- **Go 1.25+** — required to build the example binary.
- **Temporal CLI** — `temporal server start-dev` runs an in-memory
  Temporal cluster on `localhost:7233`. Install:

  ```bash
  brew install temporal       # macOS
  ```

  Linux/Windows: <https://docs.temporal.io/cli>.

- **A running Temporal cluster** — the walkthrough uses `extbin
  dev-temporal` for local. The same commands work against Temporal
  Cloud or a self-hosted server; the only thing that changes is the
  `--address` flag (and TLS / API key flags, which the global flag
  inventory covers).

- **Skytime example built or runnable** — either run via
  `go run ./examples/http-github-webhook/cmd/extbin ...` (slower
  first-launch, no build step needed), or build the binary up front:

  ```bash
  go build -o ./extbin ./examples/http-github-webhook/cmd/extbin
  ```

  This walkthrough assumes the `extbin` binary is on your PATH or
  invoked from the repo root. Substitute
  `go run ./examples/http-github-webhook/cmd/extbin ...` wherever you
  see `extbin ...` if you prefer.

## What this walkthrough demonstrates

1. Declaring a cron trigger in a `.star` file using `core.cron(...)`.
2. Previewing the create / update / delete plan with `skytime
   cron-plan` (dry-run, zero cluster mutations).
3. Applying the plan with `skytime server --cron-reconcile`.
4. Verifying the Schedule was created via `temporal schedule list`.
5. Removing the trigger and watching the orphan get deleted on the
   next reconcile.
6. (Optional, ~80s) Watching a `* * * * *` (every-minute) schedule
   fire and observing the workflow execution in `temporal workflow
   list`.

## 1. Inspect the example flow

Open
[`examples/http-github-webhook/weekly_digest.star`](../../examples/http-github-webhook/weekly_digest.star)
— the canonical Phase 7.2 example. The trigger declaration:

```python
trigger(
    flow = "weekly_digest",
    source = core.cron(
        schedule = "0 9 * * 1",
        timezone = "America/New_York",
        overlap  = "skip",
    ),
    map = lambda req: {
        "scheduled_time": req.scheduled_time,
        "actual_time":    req.actual_time,
    },
    idempotency_key = lambda req: req.scheduled_time,
)
```

Notes:

- `core.cron` is a Skytime-native source factory — it lives under the
  `core.*` namespace, NOT a domain extension. Any binary that
  registers `skycore.New()` in `cli.WithExtensions(...)` accepts it
  out of the box. Both `cmd/skytime` and
  `examples/http-github-webhook/cmd/extbin` already register it.
- 5-field POSIX cron syntax only. `@hourly` / `@daily` / `@midnight`
  macros and 6-field forms (with seconds) are REJECTED at parse time.
  Use the explicit `0 * * * *` for hourly, `0 0 * * *` for daily.
- `timezone` accepts any IANA name (e.g., `America/New_York`,
  `Europe/London`, `Asia/Tokyo`). UTC is the default.
- `overlap` controls what Temporal does if a previous run is still in
  flight when the next fire-time arrives. `skip` is the safe default;
  the other allowed values are `allow`, `buffer_one`, and
  `cancel_other`.
- `map=` and `idempotency_key=` are required by the trigger() builtin
  — but for cron triggers they're effectively identity lambdas
  reflecting the cron source's `ReqSchema = [scheduled_time,
  actual_time]`. The WorkflowID composes deterministically
  (`weekly_digest/<8-char-hash>`), and Temporal appends a per-fire
  server-side timestamp so backfills dedupe correctly.

## 2. Start a local Temporal cluster

In **terminal 1**, run:

```bash
extbin dev-temporal
```

This wraps `temporal server start-dev` — Temporal's in-memory dev
cluster. Leave it running; press Ctrl-C when you're done with the
whole walkthrough. You should see lines indicating the server is
listening on `localhost:7233` (workflow API) and `localhost:8233`
(web UI).

## 3. Preview the plan (dry-run)

In **terminal 2**:

```bash
extbin cron-plan --rootdir=examples/http-github-webhook/ --address=localhost:7233
```

Expected output (one slog record per planned action; the example
shows the human-readable charm-log format):

```
[skytime] cron-plan reading examples/http-github-webhook/
[skytime] cluster has 0 Skytime-managed schedules
[skytime] CREATE skytime/weekly_digest/aGcRb2Q8   cron="0 9 * * 1" tz=America/New_York overlap=skip
[skytime] plan: 1 create, 0 update, 0 delete
[skytime] no changes applied (dry-run)
```

`cron-plan` performs zero cluster mutations — safe to run in CI,
pre-deploy review, manual sanity checks, etc. The exit code is 0 on
any well-formed plan output; non-zero only if parsing or the
ScheduleClient.List call itself fails.

Pass `--json-log` to emit each plan entry as a JSON record (one per
line) suitable for piping through `jq` or feeding a structured-log
pipeline.

## 4. Apply the schedule

In **terminal 3**:

```bash
extbin server --rootdir=examples/http-github-webhook/ \
              --task-queue=demo \
              --address=localhost:7233 \
              --addr=127.0.0.1:18080 \
              --cron-reconcile
```

The `--cron-reconcile` flag opts THIS replica in to applying the
reconciliation. In a multi-replica deployment, only one replica
should set this flag — the flag IS the leader-election mechanism
(see D-7.2-10 in the phase planning notes).

Expected startup output (your hash will differ; the schedule ID is
`skytime/weekly_digest/<8-char-base64url-sha256>`):

```
[skytime] starting server (rootdir=examples/..., task-queue=demo, addr=127.0.0.1:18080)
[skytime] registered 1 flows: [weekly_digest]
[skytime] registered 1 triggers: [{source:core.cron flow:weekly_digest mount:"cron @ 0 9 * * 1 (America/New_York)"}]
[skytime] cron-reconcile applied: 1 creates, 0 updates, 0 deletes
[skytime] HTTP listener bound 127.0.0.1:18080
[skytime] worker started; SIGTERM/SIGINT to drain
```

The boot order is locked (D-7.2-16): `worker.Start()` →
`cron-reconcile` → HTTP listener bind → drain wait. A reconcile
failure exits non-zero BEFORE the listener binds, so K8s
readinessProbe stays unready and the rollout halts — fail-loud, not
fail-quiet.

## 5. Verify the Schedule

In **terminal 2** (kept open from step 3):

```bash
temporal schedule list --address=localhost:7233
```

Expected output:

```
   Schedule Id                              State
   skytime/weekly_digest/aGcRb2Q8           ...
```

Inspect the Schedule:

```bash
temporal schedule describe --schedule-id skytime/weekly_digest/aGcRb2Q8 --address=localhost:7233
```

Shows the cron spec, timezone, overlap policy, the workflow action
(Workflow Type=`SkytimeWorkflow`, TaskQueue=`demo`), and the Memo
field containing the canonical-config JSON that the next boot's
diff uses for drift detection.

## 6. Orphan-delete demo

Comment out or remove the `trigger(...)` block at the bottom of
`examples/http-github-webhook/weekly_digest.star`:

```python
# trigger(
#     flow = "weekly_digest",
#     source = core.cron(...),
#     ...
# )
```

Stop the server in terminal 3 with Ctrl-C (wait for the drain log).
Restart with the same command:

```bash
extbin server --rootdir=examples/http-github-webhook/ \
              --task-queue=demo \
              --address=localhost:7233 \
              --addr=127.0.0.1:18080 \
              --cron-reconcile
```

Expected log:

```
[skytime] cron-reconcile applied: 0 creates, 0 updates, 1 deletes
```

Confirm in terminal 2:

```bash
temporal schedule list --address=localhost:7233
```

The `skytime/weekly_digest/...` row is gone.

The orphan deletion is unconditional when `--cron-reconcile` is set
(D-7.2-07 — no preserve mode). User-created Schedules WITHOUT the
`skytime/` prefix are NEVER touched — `IsSkytimeManaged` filters the
listed Schedules by prefix before the delete diff bucket is computed.

Uncomment the trigger before continuing.

## 7. (Optional) Watch a schedule fire

Edit `examples/http-github-webhook/weekly_digest.star`: change

```python
schedule = "0 9 * * 1",
```

to

```python
schedule = "* * * * *",
```

Stop the server (Ctrl-C in terminal 3, wait for drain). Restart with
the same `--cron-reconcile` command. The startup log will show
`cron-reconcile applied: 0 creates, 1 updates, 0 deletes` — the
canonical config drifted, so reconcile fired an Update on the
existing Schedule (operator-set State like Paused/Note survives the
Update via the SDK's DoUpdate callback).

Wait 60-80 seconds, then in terminal 2:

```bash
temporal workflow list --address=localhost:7233 --query 'WorkflowType="SkytimeWorkflow"'
```

A workflow execution should appear, with ID shaped
`weekly_digest/<8-char-hash>-<timestamp>` — the timestamp suffix is
appended server-side by Temporal Schedules for per-fire uniqueness.

[`docs/walkthroughs/cron-schedules-smoke.sh`](./cron-schedules-smoke.sh)
automates this entire check end-to-end against a fresh ephemeral
dev-temporal — useful as a manual smoke or as a gated CI run.

Restore `schedule = "0 9 * * 1"` before moving on.

## Workflow ID format

Cron-fired workflows have IDs shaped
`{flow_name}/{trigger_pos_hash}-<timestamp>`:

- `flow_name` — the trigger's `flow=` value.
- `trigger_pos_hash` — first 8 characters of base64url-encoded
  SHA-256 of the trigger's source position (`file:line:col`) in the
  `.star` file. Stable across boots — moving the trigger declaration
  to a different line changes the hash and therefore the Schedule
  ID (which forces a Create + Delete on the next reconcile, as if
  the trigger were brand-new).
- `<timestamp>` — appended server-side by Temporal Schedules itself
  for per-fire uniqueness (Pitfall 2 in our research notes).

This means two backfilled runs at the same scheduled time DO NOT
collide on WorkflowID; Temporal generates a unique suffix per fire.

## Troubleshooting

### `core.cron: invalid 5-field POSIX cron "@hourly"`

Cron macros (`@hourly`, `@daily`, `@midnight`, etc.) are intentionally
rejected at parse time (D-7.2-22). Use the explicit 5-field form:
`0 * * * *` for hourly, `0 0 * * *` for daily, `0 0 * * 0` for
weekly.

### `core.cron: invalid timezone "Amrica/New_York"`

IANA timezone name typo. Use `America/New_York` (note the spelling).
Run `ls /usr/share/zoneinfo/` for the full list on macOS/Linux, or
`timedatectl list-timezones` on systemd-based Linux. The library
embeds Go's `time/tzdata` so scratch/distroless containers resolve
the same names without an OS-side zoneinfo install.

### `cron-reconcile failed: rpc error: code = PermissionDenied`

The Temporal user/API key lacks `temporal:schedules:*` IAM
permission. Grant the IAM role/policy on the namespace or use a
higher-privilege credential. The server exits non-zero before the
listener binds, so K8s readinessProbe stays unready and the
deployment rolls back (D-7.2-11 fail-loud).

### Schedule exists but no workflows fire

Check that the worker is actually polling the same task queue the
Schedule action references:

```bash
temporal schedule describe --schedule-id skytime/weekly_digest/<hash> --address=localhost:7233
```

The `Action` block shows `Workflow Type=SkytimeWorkflow` and the
`Task Queue` value. Compare that against your server's `--task-queue`
flag. A mismatch is silent — Temporal fires the Schedule on time,
but no worker on the action's task queue ever drains it, so the
execution sits indefinitely in the Open state.

### `cron-reconcile applied: 0 creates, 0 updates, 0 deletes` but the cron config changed

You probably edited the cron schedule but not the trigger position
in the `.star` file. The Schedule ID is computed from the trigger's
`file:line:col` position; if neither the position nor the canonical
config (cron / timezone / overlap / catchup_window / ContentHash)
changes, no Update fires. Try one of:

- Edit the cron schedule itself (e.g., `0 9 * * 1` → `0 10 * * 1`),
  which changes the canonical config bytes and fires an Update.
- Edit any other part of the flow file (which bumps ContentHash and
  also fires an Update — see Pitfall 7 in the planning notes).
- Verify via `temporal schedule describe` that the canonical Memo
  matches what you expect.

### Two replicas accidentally have `--cron-reconcile` set

The reconciler treats AlreadyExists on Create as non-fatal (Pitfall
5 in the planning notes): the second replica's reconcile sees the
Schedule already created, logs a Warn, and continues. No boot
failure. This is intentional — operator misconfiguration shouldn't
crash the cluster. But the right long-term fix is to set
`--cron-reconcile` on exactly one replica (the flag IS the
leader-election mechanism for v1).

## What this walkthrough did NOT cover

- **Backfills.** Temporal supports `temporal schedule backfill` for
  catching up missed runs. Skytime's reconciler does NOT trigger
  backfills automatically — operator's call. Set the cron source's
  `catchup_window` kwarg (any Go duration string) to control how
  far back Temporal will dispatch missed firings on its own.
- **Pause / resume from Skytime.** Use `temporal schedule pause` /
  `temporal schedule unpause` directly. Skytime's Update callback
  preserves operator-set paused state across reconciliation, so
  paused Schedules stay paused across worker boots.
- **Multi-replica reconciliation.** Set `--cron-reconcile` on ONLY
  one replica in your deployment. The flag IS the leader-election
  mechanism (D-7.2-10) for v1; subsequent phases may add proper
  leader election if the operator pain ever surfaces.

## Related

- [GitHub webhook walkthrough](./github-webhook.md) — the HTTP-trigger
  counterpart from Phase 7.1.
- [Architecture overview](../architecture.md) — the parse/execute
  split that makes the cron primitive work.
- [Cron schedules smoke script](./cron-schedules-smoke.sh) —
  automated end-to-end test, ~80 second wall clock.
