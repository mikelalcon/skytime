flow(
    name="happy",
    inputs={"n": "int"},
    steps=[
        if_cond(
            output_alias="result_dict",
            cond=lambda ctx: ctx.n > 0,
            then=[result(value={"sign": "positive", "magnitude": ctx.n})],
            else_=[result(value={"sign": "negative", "magnitude": -ctx.n})],
        ),
    ],
)
