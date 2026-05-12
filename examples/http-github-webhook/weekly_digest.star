"""weekly_digest.star — Phase 7.2 example flow demonstrating
core.cron(...) durable cron triggers via Temporal Schedules.

Reader: see docs/walkthroughs/cron-schedules.md for the end-to-end
walkthrough (prerequisites, cron-plan dry-run, server cron-reconcile
apply, temporal schedule list verification, troubleshooting, and the
trigger-removal/orphan-delete demo).

This is the first real cron-triggered flow in the example corpus. The
trigger declaration uses core.cron(schedule=, timezone=, overlap=); the
flow body fetches repo metadata via the github extension's get_repo
operation (idempotent read), uses a parallel fan-out per author for the
coverage matrix, and logs a digest summary via a script.

D-7.2-15 spec note: map= and idempotency_key= are intentionally MINIMAL
on cron triggers — they pass through the cron-time ReqSchema
(scheduled_time, actual_time) unchanged because the cron source's
auto-derived InitState IS the workflow input. The trigger() builtin
still requires both kwargs (parser/builtins.go::builtinTrigger), so
this fixture wires them as identity lambdas.

v1 note: step outputs are not auto-bound into ctx, so the digest is
composed from the cron-fired ReqSchema fields (scheduled_time) plus
constant strings — same pattern Phase 6's flows use.
"""

gh = github.client(credential = "github_token")

flow(
    name = "weekly_digest",
    inputs = {
        "scheduled_time": "string",
        "actual_time": "string",
    },
    steps = [
        # Seed the per-author iteration list from a parse-time constant.
        # v1 has no auto-bound step outputs, so the for_each_parallel below
        # walks a script-derived list rather than the result of get_repo.
        script(
            id = "seed_authors",
            fn = lambda ctx: {"authors": ["mikelalcon"]},
            output_alias = "grouped",
        ),
        # Sequential step: idempotent GET of repo metadata. Demonstrates
        # that cron-fired workflows can invoke domain extensions exactly
        # like trigger-fired or skytime-run-fired flows.
        step(
            name = "fetch repo info",
            action_fn = lambda ctx: gh.get_repo(
                owner = "mikelalcon",
                repo = "skytime",
            ),
        ),
        # Parallel fan-out: one summary line per author. Mirrors the
        # for_each_parallel pattern from issue_triage / pr_to_webhook.
        for_each_parallel(
            items = lambda ctx: ctx.grouped.authors,
            item = "author",
            steps = [
                script(
                    id = "line",
                    fn = lambda ctx: {
                        "author": ctx.author,
                        "summary": "weekly digest for " + ctx.author + " at " + ctx.scheduled_time,
                    },
                    output_alias = "line",
                ),
            ],
            max_concurrency = 4,
        ),
        # Visible effect: log.info(...) emits a structured slog record at
        # workflow time via workflow.GetLogger(ctx).Info (Phase 7.2.1
        # walk_log.go) so this line surfaces in `skytime server` stdout
        # on every cron fire decorated with the [skytime/log] tag.
        # Replaces the prior `script(id="log_digest", fn=lambda ctx: print(...) or {"logged": True})`
        # workaround — log is a side-channel step (no output_alias, no
        # state binding), and the human renderer suppresses the bookend
        # step_dispatch/step_complete frames so operators see exactly
        # one [skytime/log] line per fire (D-7.2.1-13).
        log.info(
            "weekly digest complete: scheduled=${ctx.scheduled_time} actual=${ctx.actual_time} authors=${len(ctx.grouped.authors)}",
        ),
    ],
)

# Cron trigger declaration.
#
# DEMO SCHEDULE: "* * * * *" fires every minute so a reader walking
# through docs/walkthroughs/cron-schedules.md sees the [skytime/log]
# digest line surface in `skytime server` stdout within ~60s of
# `--cron-reconcile` — no patience required to verify the loop closed.
#
# FOR REAL USE: swap to a realistic weekly schedule like
#   schedule = "0 9 * * 1"   (Mondays 09:00 in the configured timezone)
# A per-minute schedule against a real downstream system (mail send,
# DB write) will quickly exceed any reasonable rate budget.
#
# Per D-7.2-15, the map / idempotency_key lambdas are minimal — the cron
# source's ReqSchema is [scheduled_time, actual_time], and the workflow
# input mirrors those fields directly. idempotency_key uses
# req.scheduled_time so two backfills at the same scheduled time
# deduplicate (Temporal still appends a server-side timestamp for
# uniqueness — Pitfall 2 in our research notes).
trigger(
    flow = "weekly_digest",
    source = core.cron(
        schedule = "* * * * *",  # demo: every minute. Change to "0 9 * * 1" for real use.
        timezone = "America/New_York",
        overlap = "skip",
    ),
    map = lambda req: {
        "scheduled_time": req.scheduled_time,
        "actual_time": req.actual_time,
    },
    idempotency_key = lambda req: req.scheduled_time,
)
