"""issue_triage.star — deepest combinator nest, two flows in one file.

Exercises (across both flows):

  - call_flow invoking a sub-flow as a Temporal child workflow
  - for_each_parallel fanning out over a list
  - if_cond branching on a script-derived predicate
  - script classifying issue freshness
  - sequential step with retry + timeout
  - block_fn (dynamic batch — see note below) acting as the if_cond
    then-branch's batched comment+label

Coverage row 3 of D-FLOWS-COVERAGE-MATRIX: every primitive plus retries,
timeouts, and credentials.

Demo target (after credfile setup):
  skytime run examples/http-github-webhook/issue_triage.star \\
    --flow issue_triage --input '{"owner":"octocat","repo":"Hello-World"}'
"""

gh = github.client(credential = "github_token")

# ---------------------------------------------------------------------------
# Sub-flow: triage_issue. Invoked once per issue number by the parent.
# v1 forbids ctx-expression inputs to call_flow (D-19) so the parent passes
# literal placeholders; this sub-flow reads its inputs verbatim.
# ---------------------------------------------------------------------------
flow(
    name = "triage_issue",
    inputs = {"owner": "string", "repo": "string", "number": "int"},
    steps = [
        step(
            name = "Get issue",
            action_fn = lambda ctx: gh.get_issue(
                owner = ctx.owner,
                repo = ctx.repo,
                number = ctx.number,
            ),
            retry = {"max_attempts": 3, "initial_interval": "1s"},
            timeout = {"start_to_close": "30s"},
        ),
        # Independent of step output; v1 has no step→state auto-binding.
        # The script's purpose here is to demonstrate the script primitive
        # plus give the if_cond below a real predicate.
        script(
            id = "classify",
            fn = lambda ctx: {"is_old": ctx.number > 0},
            output_alias = "classification",
        ),
        if_cond(
            cond = lambda ctx: ctx.classification.is_old,
            then = [
                # Both ops are non-idempotent → ACT-03 runs each as its own
                # activity invocation. Used block_fn here (instead of static
                # block) because the kwargs reference int-typed ctx.number,
                # which the parser cannot resolve in a static block.
                step(
                    block_fn = lambda ctx: [
                        gh.add_comment(
                            owner = ctx.owner,
                            repo = ctx.repo,
                            number = ctx.number,
                            body = "Auto-triage: stale issue.",
                        ),
                        gh.add_label(
                            owner = ctx.owner,
                            repo = ctx.repo,
                            number = ctx.number,
                            label = "stale",
                        ),
                    ],
                ),
            ],
            else_ = [],
        ),
    ],
)

# ---------------------------------------------------------------------------
# Top-level flow: issue_triage. Lists issues, then fans out over a small
# numeric placeholder list and invokes triage_issue for each.
# ---------------------------------------------------------------------------
flow(
    name = "issue_triage",
    inputs = {"owner": "string", "repo": "string"},
    steps = [
        step(
            name = "List open issues",
            action = gh.list_open_issues(owner = "${ctx.owner}", repo = "${ctx.repo}"),
        ),
        # Placeholder issue numbers; v1 cannot wire ctx.list_open_issues
        # into the loop body since step outputs aren't bound to state.
        script(
            id = "issue_numbers",
            fn = lambda ctx: {"numbers": [1, 2]},
            output_alias = "nums",
        ),
        for_each_parallel(
            items = lambda ctx: ctx.nums.numbers,
            item = "num",
            steps = [
                # call_flow inputs accept literal data only (D-19); the
                # sub-flow uses its own ctx.owner/.repo/.number from
                # whatever is supplied here.
                call_flow(
                    name = "triage_issue",
                    inputs = {"owner": "octocat", "repo": "Hello-World", "number": 1},
                ),
            ],
            max_concurrency = 4,
        ),
    ],
)
