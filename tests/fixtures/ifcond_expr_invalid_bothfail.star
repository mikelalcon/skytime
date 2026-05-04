flow(
    name="both_fail",
    inputs={},
    steps=[
        if_cond(
            output_alias="X",
            cond=lambda ctx: True,
            then=[fail("a")],
            else_=[fail("b")],
        ),
    ],
)
