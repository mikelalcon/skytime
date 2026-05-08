# unknown_flow.star — trigger references a flow that doesn't exist.
# Should error at finalize (validateTriggerFlowNames):
#   trigger references unknown flow "missing"; known flows: [check_user]

flow(name = "check_user", steps = [])

trigger(
    flow = "missing",
    source = fake.webhook(req_fields = ["payload"]),
    map = lambda req: req.payload,
    idempotency_key = lambda req: "k",
)
