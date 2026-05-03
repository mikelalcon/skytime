# FIXTURE: invalid — block_fn returns mixed idempotent (gh.get) + non-idempotent (gh.post) calls; parse-time classifier rejects
gh = http.endpoint(base_url = "https://api.github.com")
flow(
    name = "block_fn_invalid_mixed",
    inputs = {"repo": "string"},
    steps = [
        step(block_fn = lambda ctx: [gh.get(path = "/repos/" + ctx.repo), gh.post(path = "/repos", body = "x")]),
    ],
)
