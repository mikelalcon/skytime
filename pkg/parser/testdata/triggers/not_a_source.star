# not_a_source.star — source kwarg is a string literal, not a
# TriggerSource. Should error at builtin call:
#   trigger.source: expected TriggerSource, got string

flow(name = "check_user", steps = [])

trigger(
    flow = "check_user",
    source = "just a string",
    map = lambda req: req,
    idempotency_key = lambda req: "k",
)
