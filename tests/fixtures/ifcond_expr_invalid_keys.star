flow(
    name="bad_keys",
    inputs={},
    steps=[
        if_cond(
            output_alias="X",
            cond=lambda ctx: True,
            then=[result(value={"a": 1})],
            else_=[result(value={"b": 2})],
        ),
    ],
)
