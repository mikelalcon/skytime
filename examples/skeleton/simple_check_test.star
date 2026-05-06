"""simple_check_test.star — Tier-3 tests for simple_check.star.

Exercises the harness end-to-end against a flow that mirrors
simple_check.star: sequential `step` with name interpolation, a `script`,
and an `if_cond`. The flow is REDECLARED inline because v1 of the runner
does not support load() across files (pkg/testing/runner.go: "Single-file
scope only — load() across files is a Phase 6 concern").

Run:
  skytime test examples/skeleton/

Demonstrates:
  - File-scope tester.mock_action — default mock for every test below
  - Per-test tester.mock_action — shadows file-scope for one test
  - tester.workflow with init_state — drives ctx.repo through the flow
  - attempt argument — first-call-fails-second-succeeds retry pattern

v1 has no "expect_failure" assertion: every workflow failure surfaces as
a test failure. To demo the FAIL render block, mock nonretryable() in a
test (you'll see the failing render in human/json output).
"""

gh = http.endpoint(base_url = "https://api.github.com")

flow(
    name = "simple_check",
    inputs = {"repo": "string"},
    steps = [
        step(
            name = "Get repo ${ctx.repo}",
            action = gh.get(path = "/repos/${ctx.repo}"),
        ),
        script(
            id = "extract_status",
            fn = lambda ctx: {"healthy": True, "repo": ctx.repo},
            output_alias = "health",
        ),
        if_cond(
            cond = lambda ctx: ctx.health,
            then = [
                step(
                    name = "Get branches for ${ctx.repo}",
                    action = gh.get(path = "/repos/${ctx.repo}/branches"),
                ),
            ],
            else_ = [
                step(
                    name = "Re-fetch ${ctx.repo}",
                    action = gh.get(path = "/repos/${ctx.repo}"),
                ),
            ],
        ),
    ],
)

# File-scope: every test below inherits this default mock. Note the
# extension is "http" (the registered name), not "gh" (the local
# variable). gh = http.endpoint(...) just binds the endpoint config
# locally; dispatch routes through the http extension's get op.
tester.mock_action(
    extension = "http",
    op = "get",
    mock_fn = lambda kwargs, attempt: ok(value = {"name": "Hello-World"}),
)

tester.workflow(
    name = "simple_check",
    init_state = {"repo": "octocat/Hello-World"},
)

def test_happy_path():
    """Inherits the file-scope mock; every gh.get returns 200."""
    tester.run(flow = "simple_check")

def test_with_retry():
    """First attempt fails (retryable), second succeeds — exercises
    the per-(flow,step,action_idx) attempt counter."""
    tester.mock_action(
        extension = "http",
        op = "get",
        mock_fn = lambda kwargs, attempt: err(msg = "transient") if attempt == 1 else ok(value = {"name": "ok"}),
    )
    tester.run(flow = "simple_check")
