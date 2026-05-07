"""issue_triage_test.star — Tier-3 tests for issue_triage.star.

Demonstrates Phase 5's `temporal_test` harness against the example's
GitHub extension:

  - File-scope tester.mock_action — default mocks for every test below
  - Per-test tester.mock_action — shadows file-scope for one test
  - attempt argument — first-call-fails-second-succeeds retry pattern
    (TEST-03; this exercises pkg/testing's per-(flow,step,action_idx)
    AttemptCounter end-to-end through the example's real GitHub op set)
  - Credential routing assertion — kwargs["_credential_id"] surfaces
    the credential ID inside the mock body (D5-C1a)
  - Replay determinism — every tester.run(...) is doubled by the harness
    (D5-D1, ALWAYS-ON); a successful run proves no divergence

v1 has no load() across files (single-file scope only — see
pkg/testing/runner.go::parseTestFile), so the two flows below are
REDECLARED inline. They mirror examples/http-github-webhook/issue_triage.star
byte-for-byte modulo the placeholder classify lambda (the real one's
predicate isn't load-bearing for the harness assertions).

Run:
  extbin test ./examples/http-github-webhook/
"""

# `gh` is the LOCAL Starlark variable; the REGISTERED extension name is
# "github" (see examples/http-github-webhook/extensions/github/github.go
# Name() method). Pitfall 2: tester.mock_action(extension=...) MUST use
# the registered name "github", NOT this local "gh" alias.
gh = github.client(credential = "github_token")

# ---------------------------------------------------------------------------
# Sub-flow: triage_issue. Mirrors issue_triage.star (06-06) verbatim.
# v1 forbids ctx-expression inputs to call_flow (D-19) — the parent passes
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
        # Placeholder classify — production uses `ctx.number > 0`; the
        # harness assertions don't care which predicate fires, only that
        # the if_cond branch executes.
        script(
            id = "classify",
            fn = lambda ctx: {"is_old": True},
            output_alias = "classification",
        ),
        if_cond(
            cond = lambda ctx: ctx.classification.is_old,
            then = [
                # block_fn (not static block) because the kwargs reference
                # int-typed ctx.number — the parser cannot resolve it in
                # a static block. Mirrors 06-06's choice exactly.
                step(
                    block_fn = lambda ctx: [
                        gh.add_comment(
                            owner = ctx.owner,
                            repo = ctx.repo,
                            number = ctx.number,
                            body = "Auto-triage: stale issue.",
                        ),
                    ],
                ),
            ],
            else_ = [],
        ),
    ],
)

# ---------------------------------------------------------------------------
# Top-level flow: issue_triage. Lists issues, then fans out over a
# placeholder list and invokes triage_issue per item.
# ---------------------------------------------------------------------------
flow(
    name = "issue_triage",
    inputs = {"owner": "string", "repo": "string"},
    steps = [
        step(
            name = "List open issues",
            action = gh.list_open_issues(owner = "${ctx.owner}", repo = "${ctx.repo}"),
        ),
        # Placeholder issue numbers — v1 cannot wire ctx.list_open_issues
        # into the for_each items because step outputs aren't auto-bound
        # to state (see 06-06 SUMMARY decision #1).
        script(
            id = "issue_numbers",
            fn = lambda ctx: {"numbers": [1, 2]},
            output_alias = "nums",
        ),
        for_each_parallel(
            items = lambda ctx: ctx.nums.numbers,
            item = "num",
            steps = [
                # call_flow inputs are LITERAL only (D-19) — no
                # ${ctx.x} interpolation, no ctx-expression.
                call_flow(
                    name = "triage_issue",
                    inputs = {"owner": "octocat", "repo": "Hello-World", "number": 1},
                ),
            ],
            max_concurrency = 4,
        ),
    ],
)

# File-scope mocks: every test below inherits these. NOTE the extension
# name is "github" (the REGISTERED name from github.go Name()), NOT "gh"
# (the local variable above). Per-test tester.mock_action calls in the
# def test_*() bodies SHADOW these for the duration of one test.
tester.mock_action(
    extension = "github",
    op = "list_open_issues",
    mock_fn = lambda kwargs, attempt: ok(value = {"issues": [{"number": 1}, {"number": 2}]}),
)
tester.mock_action(
    extension = "github",
    op = "get_issue",
    mock_fn = lambda kwargs, attempt: ok(value = {"number": 1, "title": "Test", "state": "open", "created_at": "2024-01-01T00:00:00Z"}),
)
tester.mock_action(
    extension = "github",
    op = "add_comment",
    mock_fn = lambda kwargs, attempt: ok(value = {"id": 1}),
)

tester.workflow(
    name = "issue_triage",
    init_state = {"owner": "octocat", "repo": "Hello-World"},
)

def test_happy_path():
    """Inherits file-scope mocks; replay-determinism asserted automatically (D5-D1)."""
    tester.run(flow = "issue_triage")

def test_get_issue_retries_then_succeeds():
    """TEST-03: first attempt fails (retryable), second succeeds. Exercises
    EX-03's attempt-aware retry path. The harness re-runs this entire test
    for replay determinism — both runs must produce the same retry pattern
    (D5-D1 + AttemptCounter reuse, pkg/testing/builtin_run.go)."""
    tester.mock_action(
        extension = "github",
        op = "get_issue",
        mock_fn = lambda kwargs, attempt: (
            err(msg = "transient HTTP 503") if attempt == 1 else ok(value = {"number": kwargs["number"], "title": "Triaged", "state": "open", "created_at": "2024-01-01T00:00:00Z"})
        ),
    )
    tester.run(flow = "issue_triage")

def test_add_comment_routes_credential():
    """Asserts the credential ID flows to the mock as kwargs['_credential_id']
    (verified pkg/testing/router.go:217). Wrong credential → nonretryable() →
    tester.run surfaces the failure as a test failure."""
    tester.mock_action(
        extension = "github",
        op = "add_comment",
        mock_fn = lambda kwargs, attempt: (
            ok(value = {"id": 1}) if kwargs["_credential_id"] == "github_token" else nonretryable(msg = "wrong credential routed: " + str(kwargs["_credential_id"]))
        ),
    )
    tester.run(flow = "issue_triage")
