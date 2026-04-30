# D3-19 fixture: per-flow + per-step task_queue override.
# The flow runs on the "critical" task queue; the first step inherits
# the flow's queue (no per-step task_queue=), the second step routes its
# activity to "slow_io". Hierarchy at execute time (Phase 3 interpreter):
#   step.TaskQueue > flow.TaskQueue > worker default.
flow(
    name = "task_queue_overrides",
    task_queue = "critical",
    inputs = {"req": "dict"},
    steps = [
        step(action = fake_ext.echo(msg = "inherits-critical")),
        step(
            action = fake_ext.echo(msg = "routed-to-slow-io"),
            task_queue = "slow_io",
        ),
    ],
)
