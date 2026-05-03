# FIXTURE: valid — single ${ctx.x} interpolation in step name + action kwarg
gh = http.endpoint(base_url = "https://api.github.com")
flow(
    name = "interp_valid_simple",
    inputs = {"repo": "string"},
    steps = [
        step(
            name = "Get repo ${ctx.repo}",
            action = gh.get(path = "/repos/${ctx.repo}"),
        ),
    ],
)
