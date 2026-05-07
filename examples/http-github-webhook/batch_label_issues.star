"""batch_label_issues.star — block_fn dynamic batch demo.

Exercises:

  - sequential step with retry + timeout
  - script filtering a list down to a smaller list
  - block_fn building N gh.add_label ActionRefs at runtime (DSL-12);
    add_label is non-idempotent so each runs as its own activity
    invocation per ACT-03

Coverage row 4 of D-FLOWS-COVERAGE-MATRIX: sequential + block_fn + script +
retries + timeouts + credentials.

Demo target (after credfile setup):
  skytime run examples/http-github-webhook/batch_label_issues.star \\
    --flow batch_label_issues --input '{"owner":"octocat","repo":"Hello-World","label":"triage"}'
"""

gh = github.client(credential = "github_token")

flow(
    name = "batch_label_issues",
    inputs = {"owner": "string", "repo": "string", "label": "string"},
    steps = [
        step(
            name = "List open issues",
            action = gh.list_open_issues(owner = "${ctx.owner}", repo = "${ctx.repo}"),
            retry = {"max_attempts": 3, "initial_interval": "1s"},
            timeout = {"start_to_close": "30s"},
        ),
        # Issue numbers seeded from inputs; the production-grade form
        # would walk a prior step's output once step→state binding lands.
        script(
            id = "filter",
            fn = lambda ctx: {"unlabeled": [1, 2, 3]},
            output_alias = "filter",
        ),
        step(
            name = "Add label '${ctx.label}'",
            block_fn = lambda ctx: [
                gh.add_label(owner = ctx.owner, repo = ctx.repo, number = n, label = ctx.label)
                for n in ctx.filter.unlabeled
            ],
        ),
    ],
)
