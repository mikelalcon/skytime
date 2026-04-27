# expects: call_flow target not found
flow(
    name = "caller",
    inputs = {},
    steps = [call_flow(name = "does_not_exist", inputs = {})],
)
