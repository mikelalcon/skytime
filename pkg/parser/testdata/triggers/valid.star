# valid.star — clean trigger; req.payload + req.headers reference a
# source that declares both attributes; should parse without error.

flow(
    name = "check_user",
    inputs = {"repo": "string"},
    steps = [],
)

trigger(
    flow = "check_user",
    source = fake.webhook(req_fields = ["payload", "headers"]),
    map = lambda req: {"repo": str(req.payload)},
    idempotency_key = lambda req: str(req.headers),
    credential = "github-app-prod",
)
