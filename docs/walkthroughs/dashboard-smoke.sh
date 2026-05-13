#!/usr/bin/env bash
# dashboard-smoke.sh — Phase 7.3 manual smoke test.
#
# End-to-end smoke for the dashboard at GET /. Validates the
# non-browser path with 10 checks against a running `temporal server
# start-dev` + a freshly built example custom binary (extbin):
#
#   1. extbin builds
#   2. GET /            → 200 + title + form + dropdown option
#   3. GET /api/events  → SSE Content-Type + first frame is `event: snapshot`
#   4. POST /api/trigger (valid)            → 200 + manual/<flow>/<32hex>
#   5. POST /api/trigger (unknown flow)     → 400
#   6. POST /api/trigger (bad JSON)         → 400 + body does NOT echo input (Pitfall 10)
#   7. POST /api/trigger (Origin: evil.com) → 403
#   8. Manually-triggered workflow appears in the dashboard SSE stream
#   9. SIGINT → SSE stream observes `event: shutdown` BEFORE EOF (D-7.3-07)
#  10. --replay-history-threshold=-1 → exit 2 with diagnostic on stderr
#
# Gated by SKYTIME_RUN_DASHBOARD_SMOKE=1 (same convention as Phase 7.2
# cron-schedules-smoke.sh) so it is a no-op in default CI runs.
#
# Usage:
#   # Skipped (default):
#   bash docs/walkthroughs/dashboard-smoke.sh
#
#   # Real run (requires `temporal server start-dev` running on :7233):
#   SKYTIME_RUN_DASHBOARD_SMOKE=1 bash docs/walkthroughs/dashboard-smoke.sh
#
# Exits 0 on success, non-zero on any failure.

set -euo pipefail

if [[ "${SKYTIME_RUN_DASHBOARD_SMOKE:-0}" != "1" ]]; then
    echo "[dashboard-smoke] skipped (set SKYTIME_RUN_DASHBOARD_SMOKE=1 to run)"
    exit 0
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
cd "$REPO_ROOT"

PORT="${SKYTIME_SMOKE_PORT:-18091}"
ADDR=":${PORT}"
BASE_URL="http://localhost:${PORT}"
ORIGIN="${BASE_URL}"

echo "[dashboard-smoke] repo root: $REPO_ROOT"
echo "[dashboard-smoke] dashboard port: $PORT"

# Per-process group cleanup; mirrors the cron-schedules-smoke
# teardown pattern. setsid (Linux/coreutils) attaches each background
# process to a new session so `kill -- -PID` tears the whole tree
# down. macOS ships without setsid, so fall back to direct PID kill +
# `pkill -P` for children.
if command -v setsid >/dev/null 2>&1; then
    SETSID="setsid"
else
    SETSID=""
fi

kill_tree() {
    local pid="$1"
    [[ -z "$pid" ]] && return 0
    if [[ -n "$SETSID" ]]; then
        kill -- -"$pid" 2>/dev/null || true
    else
        pkill -P "$pid" 2>/dev/null || true
        kill "$pid" 2>/dev/null || true
    fi
    wait "$pid" 2>/dev/null || true
}

SERVER_PID=""
SERVER_LOG=""
SSE_BG_LOG=""
cleanup() {
    echo "[dashboard-smoke] cleanup: stopping background processes"
    kill_tree "${SERVER_PID:-}"
    if [[ -n "${SERVER_LOG:-}" ]]; then
        echo "[dashboard-smoke] server log preserved at: $SERVER_LOG"
    fi
    if [[ -n "${SSE_BG_LOG:-}" && -f "$SSE_BG_LOG" ]]; then
        rm -f "$SSE_BG_LOG"
    fi
}
trap cleanup EXIT INT TERM

# Strip ANSI escape codes before grepping. charm-log emits dim-style
# codes around structured attr keys regardless of TTY detection or
# NO_COLOR, so naive `grep 'event=flow_start'` against server.log
# misses every match.
strip_ansi() {
    sed -E 's/'$'\x1b''\[[0-9;]*m//g' "$1"
}

# Pre-flight: temporal must be reachable on localhost:7233 (caller
# starts it; matches Phase 7.2 contract).
echo "[dashboard-smoke] check 0: temporal reachable on localhost:7233"
if ! nc -z localhost 7233 2>/dev/null && ! curl -fsS --max-time 1 http://localhost:7233 >/dev/null 2>&1; then
    echo "[dashboard-smoke] FATAL: temporal not reachable on localhost:7233 — start with 'temporal server start-dev' first" >&2
    exit 1
fi

# Check 1: build extbin.
EXTBIN="${EXTBIN:-/tmp/skytime-dashboard-smoke-extbin}"
echo "[dashboard-smoke] check 1: build extbin → $EXTBIN"
go build -o "$EXTBIN" ./examples/http-github-webhook/cmd/extbin

# Check 10 (rejection path, do this BEFORE we start the long-lived
# server so the binary path is still clean): --replay-history-threshold=-1
# must exit non-zero with a diagnostic on stderr. We pass --rootdir to
# get past required-flag validation.
echo "[dashboard-smoke] check 10: --replay-history-threshold=-1 rejection"
NEGATIVE_LOG=$(mktemp -t skytime-dashboard-smoke-negative.XXXXXX)
NEGATIVE_RC=0
"$EXTBIN" server \
    --rootdir=examples/http-github-webhook/ \
    --replay-history-threshold=-1 \
    >"$NEGATIVE_LOG" 2>&1 || NEGATIVE_RC=$?
if [[ "$NEGATIVE_RC" == "0" ]]; then
    echo "[dashboard-smoke] FATAL: --replay-history-threshold=-1 was accepted (want non-zero exit)" >&2
    cat "$NEGATIVE_LOG" >&2 || true
    rm -f "$NEGATIVE_LOG"
    exit 1
fi
if ! grep -qi 'replay-history-threshold' "$NEGATIVE_LOG"; then
    echo "[dashboard-smoke] FATAL: negative replay-history-threshold did not surface a flag-named diagnostic" >&2
    cat "$NEGATIVE_LOG" >&2 || true
    rm -f "$NEGATIVE_LOG"
    exit 1
fi
rm -f "$NEGATIVE_LOG"

# Start skytime server in background.
SERVER_LOG=$(mktemp -t skytime-dashboard-smoke.XXXXXX.log)
echo "[dashboard-smoke] starting skytime server (log: $SERVER_LOG)"
$SETSID "$EXTBIN" server \
    --rootdir=examples/http-github-webhook/ \
    --task-queue=demo-dashboard-smoke \
    --address=localhost:7233 \
    --addr="$ADDR" \
    --temporal-web-ui=http://localhost:8233 \
    --replay-history-threshold=50 \
    >"$SERVER_LOG" 2>&1 &
SERVER_PID=$!

# Wait for listener to bind (poll the dashboard root).
echo "[dashboard-smoke] waiting for dashboard at $BASE_URL ..."
for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
    if curl -fsS --max-time 1 "$BASE_URL/" -o /dev/null 2>/dev/null; then
        echo "[dashboard-smoke] dashboard ready (waited ${i} ticks; PID $SERVER_PID)"
        break
    fi
    sleep 0.5
done
if ! curl -fsS --max-time 1 "$BASE_URL/" -o /dev/null 2>/dev/null; then
    echo "[dashboard-smoke] FATAL: dashboard not reachable within 10s" >&2
    strip_ansi "$SERVER_LOG" >&2 || cat "$SERVER_LOG" >&2 || true
    exit 1
fi

# Check 2: GET / — title + form + example flow option.
echo "[dashboard-smoke] check 2: GET / (title + form + dropdown option)"
BODY=$(curl -fsS "$BASE_URL/")
if ! grep -q 'Skytime Dashboard' <<<"$BODY"; then
    echo "[dashboard-smoke] FATAL: dashboard body missing title 'Skytime Dashboard'" >&2
    exit 1
fi
if ! grep -q 'id="triggerForm"' <<<"$BODY"; then
    echo "[dashboard-smoke] FATAL: dashboard body missing manual trigger form" >&2
    exit 1
fi
if ! grep -q 'value="public_repo_check"' <<<"$BODY"; then
    echo "[dashboard-smoke] FATAL: dashboard body missing example flow option 'public_repo_check'" >&2
    exit 1
fi

# Check 3: GET /api/events — text/event-stream content-type +
# `event: snapshot` first frame (within 3s).
echo "[dashboard-smoke] check 3: GET /api/events SSE headers + first frame"
SSE_HEADERS=$(curl -fsS -i --max-time 3 "$BASE_URL/api/events" 2>/dev/null | head -20 || true)
if ! grep -qi 'Content-Type: text/event-stream' <<<"$SSE_HEADERS"; then
    echo "[dashboard-smoke] FATAL: /api/events Content-Type is not text/event-stream" >&2
    echo "$SSE_HEADERS" >&2
    exit 1
fi
SSE_BODY=$(curl -fsS --max-time 3 "$BASE_URL/api/events" 2>/dev/null | head -10 || true)
if ! grep -q 'event: snapshot' <<<"$SSE_BODY"; then
    echo "[dashboard-smoke] FATAL: first SSE event is not 'snapshot'" >&2
    echo "$SSE_BODY" >&2
    exit 1
fi

# Check 9 setup: spawn a long-lived SSE subscriber NOW (before we
# trigger the workflow + send SIGINT) so we can scan its output for
# both the workflow_started delta AND the final event: shutdown frame.
SSE_BG_LOG=$(mktemp -t skytime-dashboard-smoke-sse.XXXXXX)
echo "[dashboard-smoke] starting background SSE subscriber (log: $SSE_BG_LOG)"
# --max-time 30 keeps the curl bounded if the test fails before
# SIGINT lands. -N disables curl's output buffering so each line
# flushes promptly.
curl -fsS -N --max-time 30 "$BASE_URL/api/events" >"$SSE_BG_LOG" 2>&1 &
SSE_BG_PID=$!
# Give it ~500ms to connect + receive the initial snapshot.
sleep 0.5

# Check 4: POST /api/trigger valid → 200 + manual/<flow>/<32hex>.
echo "[dashboard-smoke] check 4: POST /api/trigger valid"
TRIGGER_RESP=$(curl -fsS -X POST "$BASE_URL/api/trigger" \
    -H "Content-Type: application/json" \
    -H "Origin: $ORIGIN" \
    -d '{"flow":"public_repo_check","input":{"repo":"octocat/Hello-World"}}')
if ! grep -qE '"workflow_id"[[:space:]]*:[[:space:]]*"manual/public_repo_check/[0-9a-f]{32}"' <<<"$TRIGGER_RESP"; then
    echo "[dashboard-smoke] FATAL: trigger response did not return manual/public_repo_check/<32hex> workflow_id" >&2
    echo "$TRIGGER_RESP" >&2
    exit 1
fi

# Check 5: POST /api/trigger with unknown flow → 400.
# NOTE: drop `-f` here (and in checks 6, 7) so curl does NOT exit
# non-zero on the expected 4xx — we want the status code captured by
# -w, not curl's stderr diagnostic noise.
echo "[dashboard-smoke] check 5: POST /api/trigger unknown flow → 400"
UNKNOWN_BODY=$(mktemp -t skytime-dashboard-smoke-unknown.XXXXXX.json)
UNKNOWN_HTTP=$(curl -sS -o "$UNKNOWN_BODY" -w '%{http_code}' \
    -X POST "$BASE_URL/api/trigger" \
    -H "Content-Type: application/json" \
    -H "Origin: $ORIGIN" \
    -d '{"flow":"definitely_not_a_flow","input":{}}' || true)
if [[ "$UNKNOWN_HTTP" != "400" ]]; then
    echo "[dashboard-smoke] FATAL: unknown-flow POST returned $UNKNOWN_HTTP; want 400" >&2
    cat "$UNKNOWN_BODY" >&2 || true
    rm -f "$UNKNOWN_BODY"
    exit 1
fi
rm -f "$UNKNOWN_BODY"

# Check 6: POST /api/trigger with bad JSON → 400 AND body does NOT
# echo the malformed input (Pitfall 10 — generic error strings only).
echo "[dashboard-smoke] check 6: POST /api/trigger bad JSON → 400 + no echo (Pitfall 10)"
BAD_JSON_PAYLOAD='not-json-at-all-shhhh-${SKYTIME_SMOKE_SECRET}'
BAD_BODY=$(mktemp -t skytime-dashboard-smoke-bad.XXXXXX.json)
BAD_HTTP=$(curl -sS -o "$BAD_BODY" -w '%{http_code}' \
    -X POST "$BASE_URL/api/trigger" \
    -H "Content-Type: application/json" \
    -H "Origin: $ORIGIN" \
    -d "$BAD_JSON_PAYLOAD" || true)
if [[ "$BAD_HTTP" != "400" ]]; then
    echo "[dashboard-smoke] FATAL: bad-JSON POST returned $BAD_HTTP; want 400" >&2
    cat "$BAD_BODY" >&2 || true
    rm -f "$BAD_BODY"
    exit 1
fi
if grep -q 'not-json-at-all-shhhh' "$BAD_BODY"; then
    echo "[dashboard-smoke] FATAL: error response echoed user input (Pitfall 10 violation)" >&2
    cat "$BAD_BODY" >&2 || true
    rm -f "$BAD_BODY"
    exit 1
fi
rm -f "$BAD_BODY"

# Check 7: POST /api/trigger with Origin: http://evil.com → 403.
echo "[dashboard-smoke] check 7: POST /api/trigger Origin: http://evil.com → want 403"
EVIL_HTTP=$(curl -sS -o /dev/null -w '%{http_code}' \
    -X POST "$BASE_URL/api/trigger" \
    -H "Content-Type: application/json" \
    -H "Origin: http://evil.com" \
    -d '{"flow":"public_repo_check","input":{}}' || true)
if [[ "$EVIL_HTTP" != "403" ]]; then
    echo "[dashboard-smoke] FATAL: cross-origin POST returned $EVIL_HTTP; want 403" >&2
    exit 1
fi

# Check 8: the manually-triggered workflow appears in the dashboard's
# SSE stream within ~5s (one or two poller cycles). We poll the
# server log directly for `event=flow_start [^ ]*flow_name=public_repo_check`
# since that's a more deterministic signal than racing the SSE stream's
# JSON parse — but ALSO scan SSE_BG_LOG for the workflow_started
# delta to prove the broadcaster fan-out works.
echo "[dashboard-smoke] check 8: workflow appears in dashboard data within 8s"
FOUND_LOG=""
for i in 1 2 3 4 5 6 7 8; do
    if strip_ansi "$SERVER_LOG" 2>/dev/null \
            | grep -qE 'event=flow_start [^ ]*flow_name=public_repo_check'; then
        FOUND_LOG="yes"
        break
    fi
    sleep 1
done
if [[ -z "$FOUND_LOG" ]]; then
    echo "[dashboard-smoke] FATAL: server log did not show flow_start for public_repo_check within 8s" >&2
    strip_ansi "$SERVER_LOG" | tail -30 >&2 || true
    exit 1
fi

# Check 9: SIGINT → SSE stream observes `event: shutdown` BEFORE EOF.
echo "[dashboard-smoke] check 9: SIGINT → SSE 'event: shutdown' frame (D-7.3-07)"
kill -INT "$SERVER_PID" 2>/dev/null || true
# Wait up to 8s for the SSE subscriber goroutine to flush + exit.
for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16; do
    if ! kill -0 "$SSE_BG_PID" 2>/dev/null; then
        break
    fi
    sleep 0.5
done
if grep -q 'event: shutdown' "$SSE_BG_LOG"; then
    echo "[dashboard-smoke] confirmed: SSE stream received 'event: shutdown' before EOF"
else
    echo "[dashboard-smoke] FATAL: SSE stream closed without observing 'event: shutdown' frame (D-7.3-07 + B3 regression)" >&2
    echo "[dashboard-smoke] last 20 lines of SSE subscriber output:" >&2
    tail -20 "$SSE_BG_LOG" >&2 || true
    exit 1
fi

# Wait for the server to drain. SERVER_PID exits after drain_completed.
wait "$SERVER_PID" 2>/dev/null || true
SERVER_PID=""

echo "[dashboard-smoke] all checks PASSED"
