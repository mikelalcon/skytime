# expects: not allowed in lambda
# `time` is not in the parse-time globals nor lambda-time globals (D-20).
# Free-var lookup of `time` will fail at parse-time.
flow(
    name = "forbidden",
    inputs = {},
    steps = [
        script(
            id = "now",
            fn = lambda ctx: time.now(),
            output_alias = "t",
        ),
    ],
)
