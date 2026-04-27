# Defines the "child" flow used by 06-call-flow-cross-file.star.
_load_marker = True
flow(name = "child", inputs = {"x": "int"}, steps = [step(action = fake_ext.echo(msg = "child"))])
