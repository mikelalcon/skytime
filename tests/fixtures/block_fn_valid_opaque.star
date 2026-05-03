# FIXTURE: valid-at-parse, runtime-check-deferred — block_fn calls a helper the parser cannot statically classify; runtime fallback (D4.1-12) handles
gh = http.endpoint(base_url = "https://api.github.com")

def make_paths(repos):
    return [gh.get(path = "/repos/" + r) for r in repos]

flow(
    name = "block_fn_valid_opaque",
    inputs = {"repos": "list"},
    steps = [
        step(block_fn = lambda ctx: make_paths(ctx.repos)),
    ],
)
