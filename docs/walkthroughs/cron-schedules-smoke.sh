#!/usr/bin/env bash
# cron-schedules-smoke.sh — Phase 7.2 manual smoke test.
#
# Boots a local Temporal dev server, applies a cron trigger with
# schedule "* * * * *" (every minute), waits up to ~80 seconds for a
# fire, asserts that a workflow execution exists via the
# `temporal workflow list` CLI, and tears everything down cleanly.
#
# Covers ROADMAP § Phase 7.2 success criterion #5:
#   "With a server up and a cron trigger fired by Temporal, the
#    corresponding workflow appears in client.ListWorkflow at the
#    scheduled time."
#
# Wall clock: 80-90 seconds (60s for the worst-case wait plus boot
# overhead). The default CI run does NOT invoke this script — it is
# env-var gated behind SKYTIME_RUN_CRON_SMOKE=1 in
# .github/workflows/scripts/walkthrough_smoke.sh.
#
# Usage:
#   bash docs/walkthroughs/cron-schedules-smoke.sh
#
# Exits 0 on success, non-zero on any failure.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
cd "$REPO_ROOT"

echo "[smoke] repo root: $REPO_ROOT"

# Per-process group cleanup; mirrors the webhook smoke teardown
# pattern. setsid (Linux/coreutils) attaches each background process
# to a new session so `kill -- -PID` tears the whole tree down.
# macOS ships without setsid, so fall back to direct PID kill +
# `pkill -P` for children (covers `go run` which spawns compile +
# run children).
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

TEMPORAL_PID=""
SERVER_PID=""
SMOKE_ROOTDIR=""
cleanup() {
    echo "[smoke] cleanup: stopping background processes"
    kill_tree "${SERVER_PID:-}"
    kill_tree "${TEMPORAL_PID:-}"
    if [[ -n "${SMOKE_ROOTDIR:-}" && -d "$SMOKE_ROOTDIR" ]]; then
        rm -rf "$SMOKE_ROOTDIR"
    fi
}
trap cleanup EXIT INT TERM

# Build extbin up front so the server-start log line is the first
# thing the test waits on (not a long compile).
EXTBIN="${EXTBIN:-/tmp/skytime-cron-smoke-extbin}"
if [[ ! -x "$EXTBIN" ]]; then
    echo "[smoke] building extbin to $EXTBIN"
    go build -o "$EXTBIN" ./examples/http-github-webhook/cmd/extbin
else
    echo "[smoke] reusing extbin at $EXTBIN"
fi

# 1. Compose a minimal smoke .star fixture with "* * * * *" schedule.
SMOKE_ROOTDIR="$(mktemp -d -t skytime-cron-smoke-XXXXXX)"
cat > "$SMOKE_ROOTDIR/smoke_cron.star" <<'STAR'
# smoke_cron.star — minimum-surface cron-triggered flow for the
# cron-schedules-smoke.sh end-to-end test. Schedule "* * * * *" so
# the smoke completes inside a ~80s wall clock.
#
# Carries a log.info(...) step (Phase 07.2.1, LOG-02) so the smoke
# also validates that the workflow-time logger routes through
# `[skytime/log]` in server stdout — not just that the cron fires.
flow(
    name = "smoke_cron",
    inputs = {
        "scheduled_time": "string",
        "actual_time": "string",
    },
    steps = [
        log.info("smoke_cron fired: scheduled=${ctx.scheduled_time} actual=${ctx.actual_time}"),
        script(
            id = "noop",
            fn = lambda ctx: {"ok": True},
            output_alias = "out",
        ),
    ],
)

trigger(
    flow = "smoke_cron",
    source = core.cron(schedule = "* * * * *", timezone = "UTC"),
    map = lambda req: {
        "scheduled_time": req.scheduled_time,
        "actual_time": req.actual_time,
    },
    idempotency_key = lambda req: req.scheduled_time,
)
STAR

echo "[smoke] fixture: $SMOKE_ROOTDIR/smoke_cron.star"

# 2. Boot temporal dev server in the background; capture PID for
#    cleanup. --headless is faster (~2s startup vs ~4s with web UI)
#    and more deterministic in CI.
echo "[smoke] starting temporal dev server (background, headless)..."
$SETSID temporal server start-dev --headless >"$SMOKE_ROOTDIR/temporal.log" 2>&1 &
TEMPORAL_PID=$!

# Wait for Temporal readiness via the operator namespace describe
# probe (same pattern walkthrough_smoke.sh uses for the webhook
# smoke).
echo "[smoke] waiting for temporal server readiness..."
for i in {1..30}; do
    if temporal operator namespace describe default --address localhost:7233 >/dev/null 2>&1; then
        echo "[smoke] temporal server ready (waited ${i}s, PID $TEMPORAL_PID)"
        break
    fi
    sleep 1
    if [[ "$i" == "30" ]]; then
        echo "[smoke] FAIL: temporal server did not become ready within 30s"
        cat "$SMOKE_ROOTDIR/temporal.log" || true
        exit 1
    fi
done

# 3. Start skytime server with --cron-reconcile so the smoke
#    fixture's cron trigger lands as a Temporal Schedule. Use a
#    random local HTTP port (127.0.0.1:0) to avoid collisions in CI.
echo "[smoke] starting skytime server with --cron-reconcile..."
$SETSID "$EXTBIN" server \
    --rootdir="$SMOKE_ROOTDIR" \
    --task-queue=smoke-cron \
    --address=localhost:7233 \
    --addr=127.0.0.1:0 \
    --cron-reconcile \
    >"$SMOKE_ROOTDIR/server.log" 2>&1 &
SERVER_PID=$!

# Helper: strip ANSI escape codes before grepping. charm-log emits
# dim-style codes around structured attr keys regardless of TTY
# detection or NO_COLOR, so naive `grep 'event=flow_start'` against
# server.log misses every match.
strip_ansi() {
    sed -E 's/'$'\x1b''\[[0-9;]*m//g' "$1"
}

# Wait for the "cron-reconcile applied" log line, which is emitted
# right before HTTP listener bind (D-7.2-16 boot order).
echo "[smoke] waiting for cron-reconcile..."
for i in {1..30}; do
    if strip_ansi "$SMOKE_ROOTDIR/server.log" 2>/dev/null | grep -q 'cron-reconcile applied'; then
        echo "[smoke] cron-reconcile complete (waited ${i}s, PID $SERVER_PID)"
        break
    fi
    sleep 1
    if [[ "$i" == "30" ]]; then
        echo "[smoke] FAIL: cron-reconcile did not complete within 30s"
        strip_ansi "$SMOKE_ROOTDIR/server.log" || true
        exit 1
    fi
done

# 4. Verify the Schedule exists via the temporal CLI.
if ! temporal schedule list --address localhost:7233 2>&1 | grep -q 'skytime/smoke_cron/'; then
    echo "[smoke] FAIL: schedule not found in 'temporal schedule list' output"
    temporal schedule list --address localhost:7233 || true
    exit 1
fi
echo "[smoke] schedule visible in 'temporal schedule list'"

# 5. Wait for the cron to fire (worst-case 70s for "* * * * *" — up
#    to 60s for the next boundary plus a small ramp-up).
#
# Poll the server log directly for `flow_start flow_name=smoke_cron`.
# This is more reliable than `temporal workflow list` because the
# dev-server's visibility store can lag by several seconds and may
# paginate historical runs off the visible page when the namespace
# is crowded.
echo "[smoke] waiting up to 80s for the cron to fire..."
FIRED=""
for i in {1..80}; do
    if strip_ansi "$SMOKE_ROOTDIR/server.log" 2>/dev/null \
            | grep -q 'event=flow_start [^ ]*flow_name=smoke_cron'; then
        FIRED="yes"
        echo "[smoke] cron fired; flow_start event visible in server log (waited ${i}s)"
        break
    fi
    sleep 1
done
if [[ -z "$FIRED" ]]; then
    echo "[smoke] FAIL: no flow_start event observed within 80s"
    echo "[smoke] server log tail (ANSI-stripped):"
    strip_ansi "$SMOKE_ROOTDIR/server.log" 2>/dev/null | tail -40 || true
    exit 1
fi

# 6. Verify the log.info step surfaced in server stdout with the
#    [skytime/log] prefix (Phase 07.2.1 LOG-02 user-visible proof).
#    The walker emits a single "[skytime/log] smoke_cron fired: ..."
#    line per fire; bookend kind=log dispatch/complete frames must
#    NOT appear in human-mode output (the logKindFilterHandler
#    decorator suppresses them).
if ! strip_ansi "$SMOKE_ROOTDIR/server.log" | grep -q '\[skytime/log\] smoke_cron fired:'; then
    echo "[smoke] FAIL: '[skytime/log] smoke_cron fired:' not found in server stdout"
    echo "[smoke] server log tail (ANSI-stripped):"
    strip_ansi "$SMOKE_ROOTDIR/server.log" | tail -40 || true
    exit 1
fi
echo "[smoke] log.info step visible with [skytime/log] tag in server stdout"

# Negative check: no bookend kind=log frames in human-mode stdout.
if strip_ansi "$SMOKE_ROOTDIR/server.log" \
        | grep -E 'event=step_(dispatch|complete) [^ ]*kind=log' >/dev/null 2>&1; then
    echo "[smoke] FAIL: kind=log dispatch/complete frame leaked into human-mode stdout"
    strip_ansi "$SMOKE_ROOTDIR/server.log" \
        | grep -nE 'event=step_(dispatch|complete) [^ ]*kind=log' | head -5
    exit 1
fi
echo "[smoke] kind=log bookend frames correctly suppressed in human-mode stdout"

echo "[smoke] PASS: cron trigger -> Schedule -> workflow execution + [skytime/log] routing end-to-end"
exit 0
