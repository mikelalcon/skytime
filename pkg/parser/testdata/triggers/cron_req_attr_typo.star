# cron_req_attr_typo.star — req.payload (NOT in core.cron's ReqSchema).
# Source declares ["scheduled_time", "actual_time"]. Should error at finalize:
#   trigger map lambda: req has no attribute "payload"; available: [actual_time scheduled_time] (declared by source kind "core.cron")

flow(name = "weekly_digest", steps = [])

trigger(
    flow = "weekly_digest",
    source = core.cron(schedule = "0 9 * * 1", timezone = "America/New_York"),
    map = lambda req: {"value": req.payload},  # INVALID: cron req schema is [scheduled_time, actual_time]
    idempotency_key = lambda req: req.scheduled_time,
)
