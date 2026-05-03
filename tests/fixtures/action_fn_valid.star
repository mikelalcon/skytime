# FIXTURE: valid — step(action_fn=lambda ctx: gh.get(...)) builds ActionRef at runtime
gh = http.endpoint(base_url = "https://api.github.com")
flow(
    name = "action_fn_valid",
    inputs = {"repo": "string"},
    steps = [
        step(action_fn = lambda ctx: gh.get(path = "/repos/" + ctx.repo)),
    ],
)
