# Dashboard — `skytime server` walkthrough

The Skytime dashboard is a stdlib-only HTML page served by `skytime server` at `GET /`. It shows three live panels — current workflows, recent webhook deliveries, and a manual trigger form — and streams updates over Server-Sent Events so a workflow that completes appears as `● Completed` without a manual page reload. The dashboard is single-tenant and localhost-by-default; the trust boundary is the worker process itself (see the Security Note section below).

This walkthrough takes you from a clean checkout to a verified browser UAT in fewer than ten commands. The non-browser path is automated by `docs/walkthroughs/dashboard-smoke.sh` (see the smoke script below) for fast regression checks.

## Prerequisites

- Go 1.25+ toolchain (`go version`)
- `temporal server start-dev` binary — install via `brew install temporal` or `curl -sSf https://temporal.download/cli.sh | sh`
- Optional: the `gh` CLI logged in (`gh auth login`) plus the webhook-forward extension (`gh extension install cli/gh-webhook`) for Step 5 only
- This repo checked out at `/path/to/skytime`

## Step 1 — Start the dev Temporal cluster

```bash
temporal server start-dev
```

Expected: starts on `localhost:7233` with the Temporal Web UI bound to `http://localhost:8233`. Leave it running in this terminal.

## Step 2 — Start `skytime server`

In a second terminal, build the example custom binary and launch the server pointed at the bundled example project. The example binary registers HTTP + GitHub + Webhook extensions and a credfile-backed credential handler; the dashboard works equally well with the stock `skytime` binary, but `extbin` is the canonical "build your own binary" shape.

```bash
cd /path/to/skytime
go build -o /tmp/extbin ./examples/http-github-webhook/cmd/extbin
/tmp/extbin server \
    --rootdir=examples/http-github-webhook/ \
    --task-queue=demo \
    --addr=:8080 \
    --temporal-web-ui=http://localhost:8233 \
    --replay-history-threshold=50
```

Expected log lines (in this order): a `starting server` banner with `rootdir` / `task-queue` / `addr` fields; a `registered flows` line listing the example project's flows; a `registered triggers` line; `HTTP listener bound addr=:8080`; and finally `worker started; SIGTERM/SIGINT to drain`.

## Step 3 — Open the dashboard

Open `http://localhost:8080/` in a browser.

Expected: the page renders with the title `Skytime Dashboard` and a `● Connected` (green) indicator in the page header. Three panels appear below — Workflows, Recent webhook deliveries, and Manual trigger. The Workflows panel shows an empty-state hint row reading `No workflows yet — fire a webhook, click Run below, or wait for cron.` The Manual trigger panel's flow dropdown enumerates the example project's registered flows (including `public_repo_check`, `pr_to_webhook`, `issue_triage`, `batch_label_issues`, `weekly_digest`).

## Step 4 — Trigger a flow manually

1. Select `public_repo_check` from the flow dropdown.
2. Paste `{"repo": "octocat/Hello-World"}` into the JSON textarea.
3. Click **Run**.

Expected: inline feedback below the Run button shows `✓ Started workflow manual/public_repo_check/<32-hex>` in green within ~100ms. Within ~2s (one SSE poll cycle) a new row appears at the top of the Workflows panel; the status dot is blue (Running). The dot transitions to green when the workflow completes; the Status text and Last Updated column flip in lock-step.

## Step 5 — Fire a webhook (optional, requires `gh`)

In a third terminal:

```bash
gh webhook forward --repo=<your-test-repo> --events=issues --url=http://localhost:8080/webhook/github
```

Open or label an issue in `<your-test-repo>` (or trigger any `issues` event).

Expected: the Recent webhook deliveries panel shows a new row within ~1s. The Headers cell shows `Authorization: <redacted>` and `X-Hub-Signature-256: <redacted>` (the redaction substring list is `authorization` / `secret` / `token` / `key` / `signature`, case-insensitive — full list pinned by the redaction unit tests). The Payload cell shows compact JSON like `{"action":"opened","issue":{...}}` truncated to ~200 chars; the full untruncated payload is available via the row's `<details>` disclosure. The Mapped Workflow column shows an anchor link to the matching workflow row above.

Click the mapped-workflow link.

Expected: the page scrolls to the matching workflow row and the row flashes briefly (~1s, pure CSS via the `:target` selector and an `@keyframes flash` rule — no JavaScript animation library).

## Step 6 — Crash-recovery demo

1. Start a long-running workflow via the manual trigger (any flow; `pr_to_webhook` is a good choice).
2. Note the workflow ID in the dashboard (you can copy from the WorkflowID column — middle-ellipsis truncation; full ID on hover via the `title` attribute).
3. In the terminal running `skytime server`, press `Ctrl-C` to send SIGINT.

Expected: server logs `listener_shutdown_started` then `listener_shutdown_complete` then `drain_started` then `drain_completed` (or `drain_timeout` if the worker is busy past `--drain-timeout`). The browser EventSource indicator flips to `○ Reconnecting…` within ~200ms — the SSE handler observes the broadcaster's `event: shutdown` frame before EOF (verified by the strict `TestServerCmd_DrainOnSIGTERM` regression test, which fails loudly if a future shutdown reorder breaks this contract).

4. Restart `skytime server` with the same flags.
5. Refresh the browser (or wait — the EventSource auto-reconnects within ~3s and the dashboard receives a fresh `snapshot`).

Expected: the previously-running workflow appears in the Workflows table again. If `HistoryLength` jumped by more than `--replay-history-threshold` events between the last poll before shutdown and the first poll after restart, the row shows `● Running (replayed N times)` (`orange` dot). On completion the row updates to `● Completed` (`green` dot).

This validates Temporal's replay-after-crash promise visually — the workflow continues from its event history without manual intervention, and the dashboard makes the durability story observable in the browser.

## Browser UAT

The exhaustive manual verification matrix. Run through every row before signing off Phase 7.3.

| Check | Action | Expected |
|-------|--------|----------|
| Connection indicator | Load page | `● Connected` (green) in the header |
| Disconnect indicator | Stop server (Ctrl-C) | `○ Reconnecting…` (gray) within ~200ms |
| Reconnect | Restart server | `● Connected` returns + fresh `snapshot` arrives |
| SSE long-lived | Leave the tab open 60+ seconds | Indicator stays `● Connected` (no 30s flicker — proves Pitfall 8 `SetWriteDeadline(time.Time{})` override defeats `http.Server.WriteTimeout=30s`) |
| Manual trigger feedback | Click Run with valid JSON | `✓ Started workflow ...` (green) within ~100ms |
| New workflow row appears | After successful Run click | Row appears at top of the Workflows table within ~2s (one SSE poll cycle) |
| Status transition | Wait for completion | Dot transitions blue → green; status text updates accordingly |
| Replay detection | Restart server mid-flight | Surviving row shows `(replayed N times)` (best-effort, threshold-tuneable via `--replay-history-threshold`) |
| Delivery row redaction | Fire a webhook | `Authorization` + `X-Hub-Signature-256` cells show `<redacted>` (substring match: `authorization`/`secret`/`token`/`key`/`signature`) |
| Delivery → workflow anchor | Click mapped-workflow link in a delivery row | Page scrolls to matching workflow row + row flashes ~1s via CSS `:target` |
| Empty state | Fresh server, no traffic yet | Hint row `No workflows yet — fire a webhook, click Run below, or wait for cron.` |
| WorkflowID truncation | Long workflow IDs | Middle-ellipsis (`weekly_digest/dLKtq…20:51:00Z`); full text on hover via `title` attribute |
| Temporal Web UI deep-link | Click a workflow ID | Opens `http://localhost:8233/namespaces/default/workflows/<id>/<runid>` (when `--temporal-web-ui` is set) |
| Bad JSON | Paste `not-json` into the textarea | Run button stays disabled (client-side `JSON.parse` gate) |
| Cross-origin block | `curl -X POST http://localhost:8080/api/trigger -H 'Content-Type: application/json' -H 'Origin: http://evil.com' -d '{"flow":"public_repo_check","input":{}}'` | HTTP `403` + body `{"error":"origin not allowed"}` (M3 JSON-strict same-origin check) |

## Security Note

The dashboard ships with **no auth and no CSRF token** (Phase 7.5+ governs production auth integration). It is intended for **localhost / internal-network use only**.

Production deploys MUST place the dashboard behind an authenticating reverse proxy:

- **nginx with basic-auth** — simple, no external dependencies
- **oauth2-proxy** — IdP-backed (Google / GitHub / Okta / Azure AD)
- **Tailscale Serve** — zero-config, mesh-only access

The dashboard's `POST /api/trigger` performs a same-origin header check as a defense-in-depth measure (requests with a non-matching `Origin` header are rejected with `403 origin not allowed`). This is **not** a CSRF replacement — an attacker with control of a same-origin page could still trigger workflows. Treat the trust boundary as the worker process: if you expose the dashboard, you are trusted.

**Non-browser clients (curl / CLI).** The same-origin check is **JSON-strict** — if you call `/api/trigger` with `Content-Type: application/json`, the `Origin` header MUST be present and match the dashboard's URL. Browsers set it automatically (the non-simple JSON content type triggers CORS preflight semantics); curl and scripts must add it explicitly:

```bash
curl -X POST http://localhost:8080/api/trigger \
    -H 'Content-Type: application/json' \
    -H 'Origin: http://localhost:8080' \
    -d '{"flow":"public_repo_check","input":{"repo":"octocat/Hello-World"}}'
```

Alternatively, leave `--addr=` unset (or pass it explicitly empty) so the check is disabled — the dashboard then runs in fully-open mode, which is acceptable only behind a trusted proxy. Non-JSON content types (e.g. `text/plain` with `--data`) retain the lenient behavior where an empty `Origin` is allowed. This closes the cross-site form-post hole from Phase 7.3 Research Open Question 3.

**Header redaction substrings.** The deliveries panel redacts header values whose **names** match (case-insensitively) any of the substrings `authorization`, `secret`, `token`, `key`, `signature`. The redaction applies in BOTH the compact row view AND the expanded `<details>` view — there is no toggle to show secrets. Operators auditing the dashboard see `<redacted>` in place of the original value; the raw bytes never reach the browser.

## Troubleshooting

| Symptom | Likely Cause | Fix |
|---------|--------------|-----|
| Page loads but shows `✗ Disconnected` immediately | Reverse proxy buffering SSE | Add `proxy_buffering off;` to nginx config or set `X-Accel-Buffering: no` on the proxy response |
| EventSource flickers `Reconnecting…` every 30s | `http.Server.WriteTimeout` killing the long-lived SSE response | Already fixed by `SetWriteDeadline(time.Time{})` inside the handler (Pitfall 8). If you observe this on Plan 04+, file a bug |
| Manual trigger shows `✗ origin not allowed` | Loading the dashboard via a different host/port than `--addr`, OR a curl/CLI client sending `Content-Type: application/json` without an `Origin` header | Pass `--addr=` matching the URL you load, run behind a proxy that rewrites `Origin`, or (for CLI clients) add `-H 'Origin: http://localhost:8080'` to the curl request |
| Replay detection never fires | Threshold too high for your workflows | Lower with `--replay-history-threshold=20` (default 50). Negative values are rejected at flag-parse time |
| Workflow IDs render as plain text instead of links | `--temporal-web-ui` not set | Pass `--temporal-web-ui=http://localhost:8233` (default) or set `SKYTIME_TEMPORAL_WEB_UI=...` |
| Headers leak a secret in the row | Bug in `deliveries.RedactHeaders` substring list | File a bug — the redaction substring list is `authorization`/`secret`/`token`/`key`/`signature` and the test matrix pins it |

## CLI Flag Reference

The two flags introduced by Phase 7.3, plus the inherited Phase 7.1 / 7.2 flags relevant to the dashboard:

| Flag | Default | Env Var | Purpose |
|------|---------|---------|---------|
| `--replay-history-threshold` | `50` | — | `HistoryLength` delta per poll cycle above which a Running workflow is marked replayed in the dashboard. Best-effort; tune down if your flows have short histories |
| `--temporal-web-ui` | `http://localhost:8233` | `SKYTIME_TEMPORAL_WEB_UI` | URL prefix for deep-linking each `WorkflowID` cell to Temporal's own Web UI. Leave unset in production deploys without a co-located Web UI; the cells fall back to plain text |
| `--addr` | `:8080` | — | HTTP listener address. The dashboard's same-origin check derives its `AllowedOrigin` from this flag (e.g. `:8080` → `http://localhost:8080`) |
| `--rootdir` | (required) | — | Path to the `.star` flow directory. The Manual Trigger dropdown enumerates flows from this rootdir |
| `--drain-timeout` | `30s` | — | Maximum time the server gives the worker to finish in-flight activities on SIGINT/SIGTERM. The shutdown sequence emits the SSE `event: shutdown` frame before this drain begins |

## What This Phase Validated

- **UI-01**: Live workflow list via SSE — `pkg/cli/server/web/events.Poller` polls `client.ListWorkflow` on a ~2s cadence and fans out deltas through `events.Broadcaster`; one server-side poller serves N browsers, so the dashboard does not multiply Temporal load (Phase 7.3 Plan 03).
- **UI-02**: In-memory ring buffer of last 100 webhook deliveries, source-agnostic, header-redacted — `pkg/cli/server/web/deliveries.RingBuffer` + `RedactHeaders` (Plan 02). The source-agnostic firewall test bans references to provider-specific headers like `X-GitHub-Event` outside provider packages.
- **UI-03**: Manual trigger form — JSON textarea + flow dropdown + Run button → `POST /api/trigger` → `flowlaunch.Execute` (Plan 04). Fresh `manual/<flow>/<32-hex>` workflow IDs prevent rapid-double-click collisions.
- **UI-04**: Single `executeFlow` seam — `pkg/cli/server/web/flowlaunch.Execute` is the only path that calls `client.ExecuteWorkflow`. Webhook ingress, manual trigger, and cron Schedule callbacks all converge on it. Verified by AST firewall tests (`TestExecuteWorkflow_CallSiteCount` + `TestBuildWorkflowInput_CallSiteCount`).

For dashboard internals, the implementation lives under `pkg/cli/server/web/` (template + handlers + mount), `pkg/cli/server/web/events/` (broadcaster + poller), `pkg/cli/server/web/deliveries/` (ring buffer + header redaction), and `pkg/cli/server/web/flowlaunch/` (the single `Execute` seam). The headings firewall test at `tests/walkthrough_dashboard_headings_test.go` pins this document's H2 section structure so future doc edits cannot silently drop a required UAT step.
