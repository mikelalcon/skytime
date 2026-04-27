# D-13: relative load() — sibling of caller file.
load("./04-load-target.star", "shared_step")
flow(name = "uses_relative_load", inputs = {}, steps = [shared_step()])
