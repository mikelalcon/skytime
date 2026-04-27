# All six primitives + a module-level def helper called from a lambda.
# Used by TestForEachParallel_BothItemForms, TestIfCond_LambdaCapture, TestScript_LambdaCapture.
DEFAULT_MAX = 10

def double(x):
    return x * 2

flow(
    name = "all_primitives",
    inputs = {"req": "dict"},
    steps = [
        step(action = fake_ext.echo(msg = "start")),
        step(block = [
            fake_ext.echo(msg = "a"),
            fake_ext.echo(msg = "b"),
            fake_ext.echo(msg = "c"),
        ]),
        if_cond(
            cond = lambda ctx: ctx.req.flag,
            then = [step(action = fake_ext.echo(msg = "then"))],
            else_ = [step(action = fake_ext.echo(msg = "else"))],
        ),
        script(
            id = "compute",
            fn = lambda ctx: {"doubled": double(ctx.req.value)},
            output_alias = "result",
        ),
        for_each_parallel(
            items = lambda ctx: ctx.req.items,
            item = "it",
            steps = [step(action = fake_ext.echo(msg = "loop"))],
        ),
        call_flow(name = "minimal", inputs = {"name": "subflow"}),
    ],
)

flow(
    name = "minimal",
    inputs = {"name": "string"},
    steps = [step(action = fake_ext.echo(msg = "minimal"))],
)
