# GitHub webhook walkthrough

This walkthrough takes you from a fresh `git clone` to a real
GitHub-event-triggered Skytime workflow running on a local Temporal
cluster — without tunnels, OAuth app registration, or a publicly
exposed URL. Along the way, you will see Temporal's headline
durability story end-to-end: kill the worker between two activities,
restart, and watch the workflow continue from event history.

The flow this walkthrough drives is
[`examples/http-github-webhook/webhook_demo.star`](../../examples/http-github-webhook/webhook_demo.star).
It posts a "received, processing..." comment on every newly opened
issue, then applies a "processed" label. The kill-restart point sits
between those two activities — Temporal records step 1's completion
in event history, so restarting the worker resumes at step 2 from
durable state.

For the 5-minute version, see
[`examples/http-github-webhook/README.md`](../../examples/http-github-webhook/README.md)
section "GitHub webhook walkthrough (5 minutes)".

## Prerequisites

You need the following installed and configured before starting.

- **gh CLI** — installed and authenticated. Install from
  <https://cli.github.com/>. Verify with `gh auth status`.
- **gh-webhook extension** — NOT bundled with `gh`. Install it
  explicitly:

  ```bash
  gh extension install cli/gh-webhook
  gh webhook forward --help   # confirm the extension is wired
  ```

  If `gh webhook forward` reports `unknown command "webhook"`, the
  extension isn't installed — re-run the install command above.

- **Temporal CLI** — `temporal server start-dev` runs an in-memory
  Temporal cluster on `localhost:7233`. Install:

  ```bash
  brew install temporal       # macOS
  ```

  Linux/Windows install steps: <https://docs.temporal.io/cli>.

- **A test GitHub repository** — one where you have admin access. You
  will deliver real webhook events from this repo via `gh webhook
  forward`. A scratch repo (e.g., `your-username/test-repo`) is fine.

- **Skytime example built or runnable** — either `git clone` the repo
  and run via `go run ./examples/http-github-webhook/cmd/extbin ...`,
  or build a binary with `go build -o ./extbin
  ./examples/http-github-webhook/cmd/extbin`.

## Step 1: Generate a webhook signing secret

GitHub signs every webhook delivery with HMAC-SHA256 using a shared
secret. Generate a random secret value and export it for this shell:

```bash
export GITHUB_WEBHOOK_SECRET=$(openssl rand -hex 32)
```

The same secret will be passed to BOTH `gh webhook forward
--secret=...` AND your Skytime credfile. If they don't match, the
receiver returns 401 on every delivery (see Troubleshooting).

## Step 2: Add the secret to your credfile

Skytime resolves credentials from `~/.skytime-credentials` (TOML).
Append the webhook signing entry:

```bash
cat >> ~/.skytime-credentials <<EOF

[credentials.github_webhook_secret]
type = "bearer"
token = "$GITHUB_WEBHOOK_SECRET"
EOF
chmod 600 ~/.skytime-credentials
```

The `chmod 600` is required: Skytime's credfile resolver refuses to
load world-readable files in strict mode. The example credfile
schema lives at
[`examples/http-github-webhook/.skytime-credentials.example`](../../examples/http-github-webhook/.skytime-credentials.example)
and already documents this entry.

## Step 3: Start the local Temporal dev server

In **terminal 1**, run:

```bash
skytime dev-temporal
```

This wraps `temporal server start-dev` — Temporal's in-memory dev
cluster. Leave it running; press Ctrl-C when you're done with the
whole walkthrough. You should see lines indicating the server is
listening on `localhost:7233` (workflow API) and `localhost:8233`
(web UI).

## Step 4: Start the Skytime server with the webhook_demo flow

In **terminal 2**:

```bash
go run ./examples/http-github-webhook/cmd/extbin server \
  --rootdir=examples/http-github-webhook/ \
  --task-queue=demo \
  --address=localhost:7233 \
  --addr=:8080
```

The startup banner lists registered flows and triggers. Look for the
`webhook_demo` trigger entry, which includes a `mount` key showing
`POST /webhook/github` — that's where webhook deliveries go.

The flow being mounted lives at
[`examples/http-github-webhook/webhook_demo.star`](../../examples/http-github-webhook/webhook_demo.star).
It declares one trigger and one flow.

## Step 5: Forward GitHub webhooks to your local server

In **terminal 3**:

```bash
gh webhook forward \
  --repo=$USER/test-repo \
  --events=issues \
  --url=http://localhost:8080/webhook/github \
  --secret=$GITHUB_WEBHOOK_SECRET
```

`gh webhook forward` opens a long-lived connection to GitHub's webhook
proxy and delivers each matching event to your local URL with the
HMAC-SHA256 signature in the `X-Hub-Signature-256` header. The
`--secret` value MUST match the credfile entry you added in step 2 —
the receiver compares using `hmac.Equal` for constant-time
verification.

Replace `$USER/test-repo` with your actual `owner/repo`. The
`--events=issues` flag asks for `issues` events only (issue
opened/edited/closed/etc.); the trigger's `events=["issues"]` filter
in `webhook_demo.star` matches that.

## Step 6: Trigger an event

In **terminal 4**:

```bash
gh issue create --repo=$USER/test-repo --title "test" --body "hello"
```

Expected behavior:

- Within seconds, a `"received, processing..."` comment appears on
  the issue. The first activity (`gh.add_comment`) ran successfully.
- Shortly after, a `processed` label appears on the same issue. The
  second activity (`gh.add_label`) ran. Because v1 has no explicit
  sleep step in the `.star` DSL, the second activity fires as soon as
  Temporal moves the workflow forward.
- The skytime server's terminal 2 shows one structured log line per
  delivery (see "Decoding the structured log line" below) with
  `error_class=ok` and a `workflow_ids=[webhook_demo/...]` entry.

## Step 7: Crash-recovery demonstration

This is the headline durability proof. Plan 07's `webhook_demo.star`
runs two activities (`gh.add_comment` then `gh.add_label`) — the
kill-restart point sits BETWEEN them.

1. With the comment posted but BEFORE the label step fires, kill the
   skytime server with Ctrl-C in terminal 2. In practice: watch the
   server logs; kill once the comment activity completes but before
   the label activity starts. If you miss the window, retrigger by
   creating another issue.
2. Restart the skytime server with the SAME command from step 4.
3. Watch the `processed` label appear shortly after restart — NOT a
   brand-new dispatch but a continuation of the original workflow.

Headline takeaway: the workflow's state lives on Temporal's server
(durable history). The worker process is stateless. Killing it does
not lose progress; restarting picks up at the next pending activity
from event history.

Note: this walkthrough does not promise a fixed wall-clock delay
between the two activities. v1 has no first-class durable sleep
primitive in the `.star` DSL; the demo proves durability via
cross-activity continuation. Future phases may add a
`core.sleep(seconds=N)` primitive that translates to
`workflow.Sleep` at execution time.

## Troubleshooting

### 1. 401 Unauthorized on every delivery

The webhook signature didn't validate. Causes:

- The `--secret` passed to `gh webhook forward` does not match the
  `[credentials.github_webhook_secret]` token in
  `~/.skytime-credentials`. Recreate both with the same value:

  ```bash
  export GITHUB_WEBHOOK_SECRET=$(openssl rand -hex 32)
  echo $GITHUB_WEBHOOK_SECRET   # use this exact string in both places
  ```

- The credfile entry is missing or the file is not at
  `~/.skytime-credentials`. Verify with `cat ~/.skytime-credentials |
  grep github_webhook_secret`.

The receiver's response body is `{"error":"unauthorized"}` with no
detail (D-7.1-14 forbids leaking whether the secret is wrong vs
missing — both produce the same opaque 401).

### 2. `unknown command "webhook"` from gh

The gh-webhook extension isn't installed. Run:

```bash
gh extension install cli/gh-webhook
```

This is Pitfall 8 from the planning notes — `gh-webhook` is NOT built
into `gh`. Many readers hit this on first run.

### 3. Status 502 Bad Gateway

The skytime server reached the receiver (signature valid) but could
not reach Temporal to start the workflow. Causes:

- Temporal dev server (terminal 1) is not running. Restart it with
  `skytime dev-temporal`.
- The `--address=localhost:7233` flag points to the wrong host/port.

The response body is
`{"error":"upstream","detail":"temporal_unavailable"}`. Source
providers (GitHub) WILL retry on 502 — that's correct behavior.

### 4. Comment posted but workflow stalls (no label)

The first activity ran, the second did not. Check terminal 2's logs
for an `error_class=dispatch_failed` line on a subsequent delivery,
or a Temporal-side workflow failure. The Temporal web UI at
<http://localhost:8233> shows running workflows and any pending
activities — useful for diagnosing why step 2 didn't fire.

If you killed the worker as part of step 7's demo and have not
restarted it, that's expected: the workflow waits in event history
until a worker reattaches. Restart `extbin server` and the label
appears.

### 5. Same delivery fires the workflow twice

This should not happen. The receiver sets
`WorkflowExecutionErrorWhenAlreadyStarted: true` on
`StartWorkflowOptions`, and `WorkflowIDReusePolicy=REJECT_DUPLICATE`
ensures repeat deliveries (same `X-GitHub-Delivery`) collide on
WorkflowID and return 200 OK with `{"status":"duplicate;
skipped","workflow_id":"..."}` rather than starting a new workflow.

If you observe duplicate dispatches, file a bug with the two
deliveries' `X-GitHub-Delivery` headers and the response bodies.

## Decoding the structured log line

The skytime server emits one structured log line per webhook
delivery (D-7.1-15). Charm-log default format:

```
[skytime] POST /webhook/github 200 12ms source=github.webhook event=issues flows=[webhook_demo] workflow_ids=[webhook_demo/aGcRb2Q8/72c89c70] error_class=ok
```

JSON form (when `--json-log` is set on the server subcommand):

```json
{"time":"...","level":"INFO","msg":"webhook delivery","method":"POST","path":"/webhook/github","status":200,"duration_ms":12,"source_kind":"github.webhook","event":"issues","flows_dispatched":["webhook_demo"],"workflow_ids":["webhook_demo/aGcRb2Q8/72c89c70"],"error_class":"ok"}
```

Fields:

- `method` / `path` — HTTP method and URL path.
- `status` — HTTP status code (200 for success, duplicate, or filter
  no-match; 401 for signature mismatch; 400 / 500 / 502 / 415 for
  other failure classes — see D-7.1-14 status mapping).
- `duration_ms` — request handling time.
- `source_kind` — e.g. `github.webhook`, `http.webhook`.
- `event` — present only for GitHub webhooks; the
  `X-GitHub-Event` header value (e.g., `issues`, `pull_request`).
- `flows_dispatched` — list of flow names that fired (possibly empty
  on event-filter no-match).
- `workflow_ids` — list of WorkflowIDs that were started (composed
  per D-7.1-08 as `{flow}/{trigger_pos_hash}/{user_key}`).
- `error_class` — one of the locked taxonomy strings:
  - `ok` — success.
  - `signature_mismatch` — HMAC validation failed (returns 401).
  - `bad_request` — malformed JSON body (returns 400).
  - `lambda_panic` — `map` or `idempotency_key` lambda raised
    (returns 500).
  - `dispatch_failed` — `client.ExecuteWorkflow` returned a
    network/connection error (returns 502).
  - `event_filtered` — no triggers matched the event (returns 200
    with `{"status":"event filtered"}`).
  - `duplicate_skipped` — REJECT_DUPLICATE collision on a
    redelivery (returns 200 with `{"status":"duplicate; skipped"}`).

The log line never contains the request body, response body, header
values, or any secret. It records names only.

## What's next

- The demo flow source — [`webhook_demo.star`](../../examples/http-github-webhook/webhook_demo.star)
- HTTP webhook source factory docs — [`docs/for-flow-authors/`](../for-flow-authors/)
- Architecture overview — [`docs/architecture.md`](../architecture.md)
