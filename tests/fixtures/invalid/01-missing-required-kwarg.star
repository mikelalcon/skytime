# expects: missing required
flow(
    # missing 'name' kwarg
    inputs = {},
    steps = [step(action = fake_ext.echo(msg = "x"))],
)
