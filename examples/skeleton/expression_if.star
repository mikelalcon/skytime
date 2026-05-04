"""expression_if.star — Phase 04.2 demo of expression-mode if_cond + dual fail().

Exercises:

  - procedural-mode if_cond (D4.2-08 preserved — no output_alias)
  - expression-mode if_cond with output_alias + result(value={...}) in
    both branches (D4.2-09 happy path)
  - asymmetric pattern: one branch binds via result(), the other
    terminates via fail("...${ctx.expr}...") with interpolation
  - downstream lambda reading ctx.<alias> proves end-to-end state
    propagation through the typed schema (D4.2-13)

Same primitive coverage as Phase 04.1 (sequential step + script + if_cond +
interpolation) PLUS the new expression-mode binding and top-level fail.

Demo target:
  skytime run examples/skeleton/expression_if.star \\
    --flow check_user --input '{"user_id":"42"}'

  skytime run examples/skeleton/expression_if.star \\
    --flow check_user --input '{"user_id":""}'   # triggers the fail() path
"""

gh = http.endpoint(base_url = "https://api.github.com")

# ---------------------------------------------------------------------------
# Flow 1: procedural-mode if_cond preserved from Phase 4 + 04.1.
# No output_alias — branches contain procedural nodes only.
# ---------------------------------------------------------------------------
flow(
    name = "procedural_demo",
    inputs = {"repo": "string"},
    steps = [
        script(
            id = "check_repo_set",
            fn = lambda ctx: {"has_repo": ctx.repo != ""},
            output_alias = "validation",
        ),
        # Procedural if_cond — same shape as Phase 4 simple_check.star.
        # fail() inside a procedural-mode branch is allowed (D4.2-07);
        # the parse-time validator does NOT require output_alias here.
        if_cond(
            cond = lambda ctx: ctx.validation,
            then = [
                step(
                    name = "Get ${ctx.repo}",
                    action = gh.get(path = "/repos/${ctx.repo}"),
                ),
            ],
            else_ = [
                fail("repo input is required"),
            ],
        ),
    ],
)

# ---------------------------------------------------------------------------
# Flow 2: expression-mode if_cond with both branches binding.
# output_alias="classification" triggers expression-mode validation.
# Both branches end in result(value={...}) with matching key sets and
# matching string types — passes D4.2-09 case 4 + 5 strict equality.
# ---------------------------------------------------------------------------
flow(
    name = "classify_repo_size",
    inputs = {"size_bytes": "int"},
    steps = [
        if_cond(
            output_alias = "classification",
            cond = lambda ctx: ctx.size_bytes > 1000000,
            then = [
                result(value = {
                    "tier": "large",
                    "label": "needs sharding",
                }),
            ],
            else_ = [
                result(value = {
                    "tier": "small",
                    "label": "single host fine",
                }),
            ],
        ),
        # Downstream consumer — reads ctx.classification.tier (typed
        # via the new state schema). Proves end-to-end propagation.
        script(
            id = "log_tier",
            fn = lambda ctx: {"logged": ctx.classification},
            output_alias = "audit",
        ),
    ],
)

# ---------------------------------------------------------------------------
# Flow 3: asymmetric expression-mode if_cond — one branch binds, the
# other terminates via fail() with ${ctx.expr} interpolation.
# The conventional shape per D4.2-specifics: one result branch, one
# fail branch. Branch-equality check applies only to result branches
# (D4.2-09 case 5 skips when one side is *dag.Fail).
# ---------------------------------------------------------------------------
flow(
    name = "check_user",
    inputs = {"user_id": "string"},
    steps = [
        if_cond(
            output_alias = "user",
            cond = lambda ctx: ctx.user_id != "",
            then = [
                result(value = {
                    "id": ctx.user_id,
                    "ok": True,
                }),
            ],
            else_ = [
                fail("invalid user_id: '${ctx.user_id}'"),
            ],
        ),
        # Downstream consumer — only reachable when then-branch ran.
        # If else-branch (fail) ran, the workflow has already raised
        # NonRetryableErr and this step never executes.
        step(
            name = "Fetch user ${ctx.user.id}",
            action = gh.get(path = "/users/${ctx.user.id}"),
        ),
    ],
)
