"""weekly_digest.star — aggregation demo.

Exercises:

  - sequential step with retry policy
  - script producing a list the for_each_parallel iterates
  - for_each_parallel building per-author summary blocks
  - sequential step with action_fn posting the digest to webhook

Coverage row 5 of D-FLOWS-COVERAGE-MATRIX: sequential + for_each_parallel +
script + retries + credentials.

Demo target (after credfile setup):
  skytime run examples/http-github-webhook/weekly_digest.star \\
    --flow weekly_digest --input '{"owner":"octocat","repo":"Hello-World"}'
"""

gh = github.client(credential = "github_token")
wh = webhook.client(credential = "webhook_url")

flow(
    name = "weekly_digest",
    inputs = {"owner": "string", "repo": "string"},
    steps = [
        step(
            name = "Recent merged PRs",
            action = gh.list_recent_merged_prs(owner = "${ctx.owner}", repo = "${ctx.repo}"),
            retry = {"max_attempts": 3, "initial_interval": "2s"},
        ),
        # v1 does not auto-bind step outputs into ctx, so the per-author
        # list is seeded from inputs; production flows would swap this for
        # a walk over the step output once that wiring lands.
        script(
            id = "group",
            fn = lambda ctx: {"authors": [ctx.owner]},
            output_alias = "grouped",
        ),
        for_each_parallel(
            items = lambda ctx: ctx.grouped.authors,
            item = "author",
            steps = [
                script(
                    id = "line",
                    fn = lambda ctx: {"author": ctx.author, "count": 1},
                    output_alias = "line",
                ),
            ],
            max_concurrency = 8,
        ),
        step(
            name = "Post digest",
            action_fn = lambda ctx: wh.post(
                body = '{"digest": "' + str(len(ctx.grouped.authors)) + ' authors merged PRs this week"}',
            ),
        ),
    ],
)
