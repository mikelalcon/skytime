# Dashboard (Phase 7.3) — Walkthrough

> **Status:** Skeleton (Wave 0). Manual UAT script lands in Plan 05.

## What you get

TODO(plan-05): Three sections — live workflow list, recent webhook deliveries, manual trigger form. Live updates via Server-Sent Events.

## Prerequisites

TODO(plan-05): `temporal server start-dev`, the `gh` CLI for `gh webhook forward`, a checkout of `examples/http-github-webhook/`.

## Step 1 — Start the dev cluster

TODO(plan-05).

## Step 2 — Start `skytime server`

TODO(plan-05).

## Step 3 — Open the dashboard

TODO(plan-05): http://localhost:8080/

## Step 4 — Fire a webhook

TODO(plan-05): `gh webhook forward` to localhost:8080.

## Step 5 — Manually trigger a flow

TODO(plan-05): Pick a flow from the dropdown, paste `{}`, click Run.

## Browser UAT

TODO(plan-05): Step-by-step manual verification covering:
- Workflow row appears within one SSE event
- Delivery row appears with redacted headers (Authorization, X-Hub-Signature-256, etc. all show `<redacted>`)
- Delivery's mapped-workflow link scrolls + flashes the corresponding workflow row
- Manual trigger inline feedback shows `✓ Started workflow ...`
- Connection indicator shows `● Connected` and survives a server restart

## Security Note

TODO(plan-05): The dashboard ships with **no auth and no CSRF token** (per Research Open Question 3). It is intended for localhost / internal-network use only. Production deploys MUST place the dashboard behind an authenticating reverse proxy (nginx + basic-auth, oauth2-proxy, Tailscale Serve). Auth integration is Phase 7.5+.

## Troubleshooting

TODO(plan-05).
