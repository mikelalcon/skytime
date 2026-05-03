# FIXTURE: invalid — ${ctx.tyop} typo, must surface as ValidationError citing the opening ${ position
gh = http.endpoint(base_url = "https://api.github.com")
flow(
    name = "interp_invalid_typo",
    inputs = {"repo": "string"},
    steps = [
        step(action = gh.get(path = "/repos/${ctx.tyop}")),
    ],
)
