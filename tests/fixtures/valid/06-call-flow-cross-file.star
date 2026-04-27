# D-16: call_flow resolves across loaded files at parse time.
load("./06-call-flow-helper.star", "load_marker")
flow(
    name = "parent",
    inputs = {},
    steps = [
        call_flow(name = "child", inputs = {"x": 1}),
    ],
)
