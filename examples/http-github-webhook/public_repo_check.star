"""public_repo_check.star — README headline demo, no credentials.

Exercises:

  - script (parse "owner/repo" into a struct field)
  - sequential step with action_fn (runtime-built ActionRef)
  - script (output dict the if_cond reads)
  - if_cond procedural with a static block of GET calls in the
    then-branch and a script in the else-branch

Coverage row 1 of D-FLOWS-COVERAGE-MATRIX: sequential + block + if_cond + script.
No credfile setup needed — uses the unauthenticated GitHub public API.

Demo target:
  skytime run examples/http-github-webhook/public_repo_check.star \\
    --flow public_repo_check --input '{"repo":"octocat/Hello-World"}'
"""

gh = github.client()

flow(
    name = "public_repo_check",
    inputs = {"repo": "string"},
    steps = [
        # Pull "owner" + "repo" out of the "owner/repo" input string so
        # the action_fn below can reference them as separate kwargs.
        script(
            id = "parse_repo",
            fn = lambda ctx: {
                "owner": ctx.repo.split("/")[0],
                "repo": ctx.repo.split("/")[1],
            },
            output_alias = "rp",
        ),
        step(
            name = "Inspect ${ctx.repo}",
            action_fn = lambda ctx: gh.get_repo(owner = ctx.rp.owner, repo = ctx.rp.repo),
        ),
        # Predicate sourced from the input only — Skytime v1 does not
        # auto-bind step outputs into ctx, so popular= is illustrative.
        script(
            id = "popularity",
            fn = lambda ctx: {"popular": len(ctx.rp.repo) > 5},
            output_alias = "pop",
        ),
        if_cond(
            cond = lambda ctx: ctx.pop.popular,
            then = [
                # Block of two GETs — both idempotent, so the parser
                # accepts the homogeneous-block invariant (D2-05).
                step(
                    block = [
                        gh.list_open_issues(owner = "${ctx.rp.owner}", repo = "${ctx.rp.repo}"),
                        gh.list_prs(owner = "${ctx.rp.owner}", repo = "${ctx.rp.repo}"),
                    ],
                ),
            ],
            else_ = [
                script(
                    id = "small_note",
                    fn = lambda ctx: {"msg": "small repo"},
                    output_alias = "note",
                ),
            ],
        ),
    ],
)
