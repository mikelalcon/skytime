# bad_arity.star — map lambda has arity 2 instead of 1. Should error
# at builtin call (captureLambdaWithArity layer 2):
#   kwarg "map" lambda must accept exactly 1 positional parameter(s) (convention: req); got 2

flow(name = "check_user", steps = [])

trigger(
    flow = "check_user",
    source = fake.webhook(req_fields = ["payload"]),
    map = lambda req, headers: req.payload,
    idempotency_key = lambda req: "k",
)
