# expects: block has 51 actions; maximum is 50

# This fixture exercises D2-07: step(block=[...]) cannot exceed the
# parser's maxBlockSize cap (default 50). The fixture builds 51
# idempotent echo() calls via list comprehension (Starlark's standard
# `[expr for x in range(n)]` form is available at parse time per the
# parser's defaultFileOptions: TopLevelControl=true) and asserts the
# parser's lintBlockSize pass rejects the over-cap step.

flow(
    name = "oversized",
    inputs = {},
    steps = [
        step(block = [fake_ext.echo(msg = "m") for _ in range(51)]),
    ],
)
