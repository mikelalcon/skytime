flow(
    name="guard",
    inputs={"r": "string"},
    steps=[
        if_cond(
            cond=lambda ctx: ctx.r == "",
            then=[fail("repo required")],
            else_=[step(action=fake_ext.echo(msg="ok"))],
        ),
    ],
)
