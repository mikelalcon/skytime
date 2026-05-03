# FIXTURE: invalid — unterminated ${ at end of string, must surface as ParseError
gh = http.endpoint(base_url = "https://api.github.com")
flow(
    name = "interp_invalid_unterminated",
    inputs = {"repo": "string"},
    steps = [
        step(action = gh.get(path = "/repos/${ctx.repo")),
    ],
)
