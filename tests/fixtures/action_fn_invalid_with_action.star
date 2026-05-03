# FIXTURE: invalid — action and action_fn together must be rejected at parse time
gh = http.endpoint(base_url = "https://api.github.com")
flow(
    name = "action_fn_invalid_with_action",
    inputs = {"repo": "string"},
    steps = [
        step(
            action = gh.get(path = "/repos/octocat"),
            action_fn = lambda ctx: gh.get(path = "/repos/" + ctx.repo),
        ),
    ],
)
