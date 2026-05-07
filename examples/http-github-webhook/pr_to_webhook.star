"""pr_to_webhook.star — authenticated fan-out demo.

Exercises:

  - sequential step with retry policy
  - script producing a list the for_each_parallel iterates
  - for_each_parallel posting one webhook per item (webhook.post is
    non-idempotent so each iteration runs as its own activity
    invocation per ACT-03)
  - script summarizing the count

Coverage row 2 of D-FLOWS-COVERAGE-MATRIX: sequential + for_each_parallel +
script + retries + credentials.

Demo target (after credfile setup with github_token + webhook_url):
  skytime run examples/http-github-webhook/pr_to_webhook.star \\
    --flow pr_to_webhook --input '{"owner":"octocat","repo":"Hello-World"}'
"""

gh = github.client(credential = "github_token")
wh = webhook.client(credential = "webhook_url")

flow(
    name = "pr_to_webhook",
    inputs = {"owner": "string", "repo": "string"},
    steps = [
        step(
            name = "List PRs for ${ctx.owner}/${ctx.repo}",
            action = gh.list_prs(owner = "${ctx.owner}", repo = "${ctx.repo}"),
            retry = {"max_attempts": 3, "initial_interval": "1s"},
        ),
        # Targets list seeded from inputs since v1 does not auto-bind
        # step outputs into ctx; the demo posts one webhook per slot.
        script(
            id = "build_targets",
            fn = lambda ctx: {"targets": [ctx.owner + "/" + ctx.repo + "#1", ctx.owner + "/" + ctx.repo + "#2"]},
            output_alias = "tgts",
        ),
        for_each_parallel(
            items = lambda ctx: ctx.tgts.targets,
            item = "tgt",
            steps = [
                step(
                    action_fn = lambda ctx: wh.post(
                        body = '{"text":"PR mention: ' + ctx.tgt + '"}',
                    ),
                ),
            ],
            max_concurrency = 4,
        ),
        script(
            id = "summary",
            fn = lambda ctx: {"posted": len(ctx.tgts.targets)},
            output_alias = "summary",
        ),
    ],
)
