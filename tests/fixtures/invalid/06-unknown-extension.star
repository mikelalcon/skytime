# expects: undefined: nonexistent_extension
flow(
    name = "unknown_ext",
    inputs = {},
    steps = [step(action = nonexistent_extension.foo(x = 1))],
)
