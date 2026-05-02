"""parallel_fanout.star — Phase 4 differential corpus fixture (D4-17).

Exercises three DSL primitives the simple_check.star file does not:

  - `for_each_parallel` (bounded fan-out, item visible inside body)
  - `step(block=[...])` (block-batch — multiple idempotent gets in one
     activity invocation)
  - `call_flow` (child workflow invocation)

Combined with simple_check.star, this corpus covers all six DSL
primitives. The differential test (tests/differential_test.go) parses
this through both the static validator AND a dry-run interpreter and
asserts they agree on accept/reject.
"""

gh = http.endpoint(base_url = "https://api.github.com")

# Helper flow — the call_flow target. Receives `repo` as input.
flow(
    name = "check_one",
    inputs = {"repo": "string"},
    steps = [
        # Block batch — three idempotent gets in one activity invocation.
        # All idempotent (D4-14 says http.get is idempotent), so the
        # parser's mixed-idempotency lint (D2-05) accepts this. Three
        # real GET endpoints on the public octocat/Hello-World repo,
        # each returns 200 against api.github.com.
        step(
            block = [
                gh.get(path = "/repos/octocat/Hello-World"),
                gh.get(path = "/repos/octocat/Hello-World/branches"),
                gh.get(path = "/repos/octocat/Hello-World/contributors"),
            ],
        ),
    ],
)

# Parent flow — uses for_each_parallel + call_flow together.
flow(
    name = "parallel_fanout",
    inputs = {"repos": "list"},
    steps = [
        for_each_parallel(
            items = lambda ctx: ctx.repos,
            item = "repo",
            steps = [
                # Inside for_each_parallel.steps, "repo" is a state
                # schema entry per D4-02 stacking rules. call_flow
                # passes it to the helper flow.
                call_flow(
                    name = "check_one",
                    inputs = {"repo": "x"},
                ),
            ],
        ),
    ],
)
