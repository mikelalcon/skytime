"""simple_check.star — Phase 4 differential corpus fixture (D4-17).

Exercises three DSL primitives against the baked-in http extension:

  - sequential `step` (one action ref → activity)
  - `script` (pure state mutation, no I/O)
  - `if_cond` (branch on lambda result)

Combined with parallel_fanout.star this corpus covers all six DSL
primitives. The differential test (tests/differential_test.go) parses
this through the static validator AND a dry-run interpreter and asserts
they agree on accept/reject.

Note: --input='{"repo_path":"..."}' is illustrative; v1 does not yet
support `step(action=gh.get(path=ctx.repo_path))` — step kwargs are
static at parse time. The corpus path is hardcoded to
/repos/octocat/Hello-World for the happy-path demo (a real public
GitHub endpoint that has returned 200 for over a decade); v1.x will
add script-builds-path when a real consumer needs it.
"""

gh = http.endpoint(base_url = "https://api.github.com")

flow(
    name = "simple_check",
    inputs = {"repo_path": "string"},
    steps = [
        # Sequential step — single ActionRef.
        step(
            action = gh.get(path = "/repos/octocat/Hello-World"),
        ),
        # Script — pure state mutation, no I/O. Computes a derived
        # field from inputs and stores it under output_alias="health".
        script(
            id = "extract_status",
            fn = lambda ctx: {"healthy": True, "repo": ctx.repo_path},
            output_alias = "health",
        ),
        # If_cond — branch on the script's output (visible because
        # script ran first and added "health" to state schema).
        if_cond(
            cond = lambda ctx: ctx.health,
            then = [
                step(action = gh.get(path = "/repos/octocat/Hello-World/branches")),
            ],
            else_ = [
                step(action = gh.get(path = "/repos/octocat/Hello-World")),
            ],
        ),
    ],
)
