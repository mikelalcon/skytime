# D-08 user authoring example: gh = github.endpoint("admin")
# Verifies the credential ID propagates from the endpoint factory through
# the closure into the resulting *dag.ActionRef carried by the step.
gh = github.endpoint("admin")

flow(
    name = "issue_creation",
    inputs = {"repo": "string", "title": "string"},
    steps = [
        step(action = gh.create_issue(repo = "acme/widget", title = "demo")),
    ],
)
