# mutable_closure.star — map lambda closes over a mutable module-level
# variable. Should error at lambda capture (Phase 1 free-var lint).

counter = [0]  # mutable list — Phase 1 free-var lint catches reads of mutable globals

flow(name = "check_user", steps = [])

trigger(
    flow = "check_user",
    source = fake.webhook(req_fields = ["payload"]),
    map = lambda req: counter[0],  # forbidden — counter is mutable
    idempotency_key = lambda req: "k",
)
