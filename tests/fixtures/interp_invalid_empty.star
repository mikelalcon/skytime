# FIXTURE: invalid — empty ${} marker is a parse error
gh = http.endpoint(base_url = "https://api.github.com")
flow(
    name = "interp_invalid_empty",
    inputs = {"repo": "string"},
    steps = [
        step(action = gh.get(path = "/repos/${}")),
    ],
)
