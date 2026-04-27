# D-13: absolute load() — from configured root.
load("/04-load-target.star", "shared_step")
flow(name = "uses_absolute_load", inputs = {}, steps = [shared_step()])
