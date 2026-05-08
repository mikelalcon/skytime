# duplicate_warn.star — two byte-identical triggers (same flow, same
# source kind + config, same credential). Should parse cleanly with a
# deferred warning accumulated on parser.triggerWarnings (D-07-13).

flow(name = "check_user", steps = [])

trigger(
    flow = "check_user",
    source = fake.webhook(req_fields = ["payload", "headers"]),
    map = lambda req: req.payload,
    idempotency_key = lambda req: "k",
    credential = "github-app",
)

trigger(
    flow = "check_user",
    source = fake.webhook(req_fields = ["payload", "headers"]),
    map = lambda req: req.payload,
    idempotency_key = lambda req: "k",
    credential = "github-app",
)
