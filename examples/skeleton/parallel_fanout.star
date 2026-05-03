"""parallel_fanout.star — Phase 04.1 demo of block_fn + for_each_parallel.

Exercises:

  - `for_each_parallel` over a runtime list (`items=lambda ctx: ctx.repos`)
  - `step(block_fn=...)` — runtime-built batch of idempotent gets
  - named steps with interpolation
  - `call_flow` (child workflow invocation)

Same primitive coverage as Phase 4 (block batch + for_each_parallel + call_flow)
but the batch's contents are now built per-item from runtime input.

Demo target:
  skytime run examples/skeleton/parallel_fanout.star \\
    --flow parallel_fanout \\
    --input '{"repos":["octocat/Hello-World","torvalds/linux","golang/go"]}'
"""

gh = http.endpoint(base_url = "https://api.github.com")

# Helper flow — invoked once per repo via call_flow.
flow(
    name = "check_one",
    inputs = {"repo": "string"},
    steps = [
        # block_fn — runtime-built batch of three idempotent reads.
        # All gh.get → idempotent=true, so the parser's best-effort
        # static analysis (D4.1-11) accepts at parse time without
        # falling back to runtime.
        step(
            name = "Read ${ctx.repo} surface",
            block_fn = lambda ctx: [
                gh.get(path = "/repos/" + ctx.repo),
                gh.get(path = "/repos/" + ctx.repo + "/branches"),
                gh.get(path = "/repos/" + ctx.repo + "/contributors"),
            ],
        ),
    ],
)

# Parent flow — fans out across the runtime repo list.
flow(
    name = "parallel_fanout",
    inputs = {"repos": "list"},
    steps = [
        for_each_parallel(
            items = lambda ctx: ctx.repos,
            item = "repo",
            steps = [
                # Inside for_each_parallel.steps, "repo" is in scope
                # per D4-02 stacking. Pass to the helper flow.
                call_flow(
                    name = "check_one",
                    inputs = {"repo": "x"},
                ),
            ],
        ),
    ],
)
