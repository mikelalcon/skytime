# typo.star — req.payloud (typo for payload). Source declares
# ["payload", "headers"]. Should error at finalize:
#   trigger map lambda: req has no attribute "payloud"; available: [headers payload] (declared by source kind "skytime.test.webhook")

flow(name = "check_user", steps = [])

trigger(
    flow = "check_user",
    source = fake.webhook(req_fields = ["payload", "headers"]),
    map = lambda req: req.payloud,
    idempotency_key = lambda req: "static-key",
)
