# Target of relative load test.
def shared_step():
    return step(action = fake_ext.echo(msg = "shared"))
