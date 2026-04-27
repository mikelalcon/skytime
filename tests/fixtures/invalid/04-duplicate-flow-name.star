# expects: duplicate flow name
flow(name = "dup", inputs = {}, steps = [step(action = fake_ext.echo(msg = "a"))])
flow(name = "dup", inputs = {}, steps = [step(action = fake_ext.echo(msg = "b"))])
