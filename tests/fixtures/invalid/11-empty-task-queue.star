# expects: task_queue must be non-empty
# D3-19: empty-string task_queue is rejected at parse time. Distinguishes
# "kwarg supplied as empty" (rejected) from "kwarg omitted" (allowed,
# defaults to empty meaning "inherit worker default").
flow(
    name = "bad",
    task_queue = "",  # invalid: empty string
    steps = [step(action = fake_ext.echo(msg = "x"))],
)
