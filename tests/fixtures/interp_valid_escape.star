# FIXTURE: valid — doubled $$ escapes, produces literal "${repo}" in the path; no interpolation
gh = http.endpoint(base_url = "https://api.github.com")
flow(
    name = "interp_valid_escape",
    inputs = {"repo": "string"},
    steps = [
        step(action = gh.get(path = "/repos/$${repo}/literal")),
    ],
)
