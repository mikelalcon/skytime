flow(
    name="bad_last",
    inputs={},
    steps=[
        if_cond(
            output_alias="X",
            cond=lambda ctx: True,
            then=[step(action=fake_ext.echo(msg="x"))],
            else_=[result(value={"x": 1})],
        ),
    ],
)
