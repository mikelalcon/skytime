flow(
    name="bad_types",
    inputs={},
    steps=[
        if_cond(
            output_alias="X",
            cond=lambda ctx: True,
            then=[result(value={"x": 1})],
            else_=[result(value={"x": 1.5})],
        ),
    ],
)
