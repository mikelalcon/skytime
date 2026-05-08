# mutable_closure.star — map lambda closes over a non-module-level (local
# inside an enclosing def) variable. Phase 1's free-var lint requires
# captured free vars to bind to module-level (col=1) names; locals inside
# def bodies fail the lint.
#
# Should error: "lambda captures non-module-level variable ..."

def _build_trigger():
    local_var = "captured"  # bound inside a def body — col != 1
    flow(name = "check_user", steps = [])
    trigger(
        flow = "check_user",
        source = fake.webhook(req_fields = ["payload"]),
        map = lambda req: local_var,  # forbidden — local_var is non-module-level
        idempotency_key = lambda req: "k",
    )

_build_trigger()
