# expects: missing argument for name
flow(
    # missing 'name' kwarg
    inputs = {},
    steps = [step(action = fake_ext.echo(msg = "x"))],
)
