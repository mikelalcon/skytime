"""webhook_demo.star — D-7.1-16 crash-recovery walkthrough flow.

Trigger fires on GitHub `issues` events. The flow comments on the issue,
then labels it. Two activities, no explicit sleep — durability is
demonstrated by killing the worker between steps and restarting:

  1. Open an issue in your test repo (gh webhook forward delivers).
  2. Watch the comment "received, processing..." appear within seconds.
  3. Kill the skytime server with Ctrl-C BEFORE the label step fires.
     (In practice: run `gh issue list` to confirm the comment landed,
     then kill the server immediately.)
  4. Restart the server with the same command.
  5. Watch the label "processed" appear shortly after — Temporal
     continued the workflow from event history. The workflow state
     lives on the Temporal server (not the worker process), so the
     kill-restart does NOT lose progress.

Demonstrates Temporal's headline durability story end-to-end against a
real provider (GitHub) without tunnels or OAuth app registration.

NOTE: v1 does NOT have a first-class durable sleep primitive in the
.star DSL. The original D-7.1-16 description suggested a 30-second
sleep between the comment and label steps; that was downscoped during
Phase 7.1 planning because the bridge's triggerTimeGlobals exposes
lambda-time globals only — not workflow.Sleep. The two-activity
shape achieves the same durability demonstration: kill between
activities, restart, second activity still fires from event history.
Future phases may add a `core.sleep(seconds=N)` primitive that
translates to workflow.Sleep at execution time.

See docs/walkthroughs/github-webhook.md (Plan 08) for the full reader
walkthrough including the credfile setup and gh extension install steps.
"""

gh = github.client(credential = "github_token")

# ---------------------------------------------------------------------------
# Trigger: GitHub `issues` events → webhook_demo flow.
#
# Lambda nested-attribute access (req.payload.repository.full_name,
# req.payload.issue.number, req.headers["X-GitHub-Delivery"]) is
# TOLERATED by the parser walker (verified during planning):
# pkg/parser/req_walk.go::findFreeVarAccesses only validates the
# direct .payload and .headers attrs against ReqSchema; deeper chains
# pass through unchecked and resolve at runtime.
# ---------------------------------------------------------------------------
trigger(
    flow = "webhook_demo",
    source = github.webhook(
        events = ["issues"],
        secret_credential = "github_webhook_secret",
    ),
    map = lambda req: {
        "repo": req.payload.repository.full_name,
        "issue_number": req.payload.issue.number,
    },
    idempotency_key = lambda req: req.headers["X-GitHub-Delivery"],
    credential = "github_webhook_secret",
)

# ---------------------------------------------------------------------------
# Flow: comment → label. Durable across worker kill (no explicit sleep
# step in v1; durability demonstrated via cross-activity continuation).
# ---------------------------------------------------------------------------
flow(
    name = "webhook_demo",
    inputs = {
        "repo": "string",          # "owner/repo" full_name from req.payload.repository
        "issue_number": "int",     # req.payload.issue.number
    },
    steps = [
        # Step 1: post the "received, processing..." comment immediately.
        # Uses action_fn + ctx.repo.split() to derive owner/repo (matches
        # the action_fn pattern from issue_triage.star).
        step(
            name = "Post received comment",
            action_fn = lambda ctx: gh.add_comment(
                owner = ctx.repo.split("/")[0],
                repo = ctx.repo.split("/")[1],
                number = ctx.issue_number,
                body = "received, processing...",
            ),
            retry = {"max_attempts": 3, "initial_interval": "1s"},
            timeout = {"start_to_close": "30s"},
        ),

        # Step 2: post the "processed" label. The kill-restart happens
        # BETWEEN step 1 and step 2 in the walkthrough — Temporal's
        # event history records step 1's completion, restart picks up
        # at step 2. NO explicit sleep needed for the durability demo.
        step(
            name = "Apply processed label",
            action_fn = lambda ctx: gh.add_label(
                owner = ctx.repo.split("/")[0],
                repo = ctx.repo.split("/")[1],
                number = ctx.issue_number,
                label = "processed",
            ),
            retry = {"max_attempts": 3, "initial_interval": "1s"},
            timeout = {"start_to_close": "30s"},
        ),
    ],
)
