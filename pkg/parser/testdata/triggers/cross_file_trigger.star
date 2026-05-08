# cross_file_trigger.star — loads cross_file_flow.star, then declares
# a trigger against the flow defined there. Validates D-07-12 cross-file
# flow-name resolution at finalize.

load("cross_file_flow.star", "_loaded")

# `_loaded` itself is unused at runtime; reading the binding satisfies
# Starlark's "every imported name must be referenced" expectation in some
# strict environments, and is harmless under our defaults.
_ignore = _loaded

trigger(
    flow = "check_user",  # defined in the loaded file
    source = fake.webhook(req_fields = ["payload"]),
    map = lambda req: req.payload,
    idempotency_key = lambda req: "k",
)
