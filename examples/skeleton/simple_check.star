"""simple_check.star — Phase 04.1 demo of dynamic kwargs + interpolation.

Exercises:

  - sequential `step` with name interpolation (${ctx.repo})
  - action kwarg interpolation in gh.get(path=...)
  - script (pure state mutation)
  - if_cond (branch on lambda result)

Same primitive coverage as Phase 4 (sequential step + script + if_cond)
but the request URL now actually depends on `--input repo=...` — the
Phase 4 hardcoded /repos/octocat/Hello-World is gone.

Demo target:
  skytime run examples/skeleton/simple_check.star \\
    --flow simple_check --input '{"repo":"octocat/Hello-World"}'
"""

gh = http.endpoint(base_url = "https://api.github.com")

flow(
    name = "simple_check",
    inputs = {"repo": "string"},
    steps = [
        # Sequential step — single ActionRef built from runtime input
        # via interpolation in the path kwarg. Step name also
        # interpolates so the live block shows the active repo.
        step(
            name = "Get repo ${ctx.repo}",
            action = gh.get(path = "/repos/${ctx.repo}"),
        ),
        # Script — pure state mutation, no I/O. Computes a derived
        # field from the response shape.
        script(
            id = "extract_status",
            fn = lambda ctx: {"healthy": True, "repo": ctx.repo},
            output_alias = "health",
        ),
        # If_cond — branch on the script's output.
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
