# expects: lambda captures non-module-level variable
def make_lambda():
    counter = [0]
    return lambda ctx: counter.append(len(counter)) or counter[-1]

flow(
    name = "bad_capture",
    inputs = {},
    steps = [
        script(id = "s", fn = make_lambda(), output_alias = "v"),
    ],
)
