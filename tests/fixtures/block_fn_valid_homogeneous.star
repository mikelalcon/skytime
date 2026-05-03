# FIXTURE: valid — block_fn produces all-idempotent batch via comprehension; classifier sees all typed homogeneous calls
gh = http.endpoint(base_url = "https://api.github.com")
flow(
    name = "block_fn_valid_homogeneous",
    inputs = {"repos": "list"},
    steps = [
        step(block_fn = lambda ctx: [gh.get(path = "/repos/" + r) for r in ctx.repos]),
    ],
)
