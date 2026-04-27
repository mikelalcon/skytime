# D-15: multiple flow() calls in one file collected into the parser-session map.
flow(name = "approve_pr", inputs = {"pr": "int"}, steps = [step(action = fake_ext.echo(msg = "approve"))])
flow(name = "reject_pr", inputs = {"pr": "int"}, steps = [step(action = fake_ext.echo(msg = "reject"))])
flow(name = "comment_pr", inputs = {"pr": "int"}, steps = [step(action = fake_ext.echo(msg = "comment"))])
