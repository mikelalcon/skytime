"""generic_http_check.star — D-7.4-07 parse-only HTTP-extension demo.

Audit gap (v1.42.0): extbin registers skyhttp.New() but no .star flow
called http.endpoint(...). This flow closes the registration/usage gap
by exercising the bare HTTP extension surface (the GitHub flows above
use the higher-level github extension, which sits on top of http but
masks the raw http.* primitives).

Demonstrates:
  - http.endpoint(base_url=..., credential=None) — unauthenticated
  - .get / .head per-method builtins on the endpoint module
  - script + step_seq + if_cond primitives (mirrors public_repo_check
    coverage but routes through the generic HTTP extension)

Per D-7.4-07 — PARSE-ONLY coverage. flows_test.go's TestFlows_ParseAll
discovers this file automatically; flows_test.go's TestFlows_CoverageMatrix
adds an entry for "generic_http_check" in Plan 05 Task 3. Live runtime
test is OUT (matches the rest of the corpus's parse-only coverage).

Demo target (parse only):
  skytime validate examples/http-github-webhook/generic_http_check.star
"""

api = http.endpoint(base_url = "https://httpbin.org")

flow(
    name = "generic_http_check",
    inputs = {"path": "string"},
    steps = [
        # Probe whether the endpoint is reachable via HEAD (cheap, no body).
        # Output_alias not declared here — step_seq with action_fn returning
        # an *dag.ActionRef registers as the "step_seq" primitive in the
        # coverage matrix.
        step(
            name = "Probe ${ctx.path}",
            action_fn = lambda ctx: api.head(path = ctx.path),
        ),
        # Compute a small script-side decision payload.
        script(
            id = "decide",
            fn = lambda ctx: {"long_path": len(ctx.path) > 20},
            output_alias = "decision",
        ),
        # if_cond on the script-derived predicate, with a then-branch
        # containing a sequential GET step. else_ branch is a script.
        if_cond(
            cond = lambda ctx: ctx.decision.long_path,
            then = [
                step(
                    name = "Fetch ${ctx.path}",
                    action_fn = lambda ctx: api.get(
                        path = ctx.path,
                        headers = {"Accept": "application/json"},
                    ),
                ),
            ],
            else_ = [
                script(
                    id = "skip_note",
                    fn = lambda ctx: {"reason": "short path; skipped fetch"},
                    output_alias = "_note",
                ),
            ],
        ),
    ],
)
