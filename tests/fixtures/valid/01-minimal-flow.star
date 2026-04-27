# Minimal flow: one flow, one step, one fake action.
# Used by TestParseFlow_DSL01.
flow(
    name = "minimal",
    inputs = {"name": "string"},
    steps = [
        step(action = fake_ext.echo(msg = "hello")),
    ],
)
