# FIXTURE: invalid — embedded newline inside ${...} is a parse error (single-line only)
gh = http.endpoint(base_url = "https://api.github.com")
flow(
    name = "interp_invalid_multiline",
    inputs = {"repo": "string"},
    steps = [
        step(action = gh.get(path = """/repos/${
ctx.repo}""")),
    ],
)
