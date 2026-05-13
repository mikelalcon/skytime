#!/usr/bin/env bash
# walkthrough_smoke.sh — runs the EX-04 ≤5-command walkthrough end-to-end.
#
# This is the script the CI workflow invokes for the "walkthrough smoke"
# step (.github/workflows/ci.yml). It is also the canonical local-repro
# script: any human can run `bash .github/workflows/scripts/walkthrough_smoke.sh`
# from the repo root to reproduce a CI failure on their own machine.
#
# The smoke mirrors examples/http-github-webhook/README.md "Quick start"
# commands 2-4 verbatim. Any drift between this script and the README's
# documented commands is a CI failure.
#
# Requirements:
#   - `go` toolchain (Go 1.25+)
#   - `temporal` CLI (install: brew install temporal | curl -sSf https://temporal.download/cli.sh | sh)
#   - Internet access to api.github.com (rate limit: 60/hr unauth — see RESEARCH Pitfall 3)
#
# CWD note (m13): this script uses absolute paths via $REPO_ROOT for CI hermeticity
# — it `cd`s to the repo root and runs `extbin run examples/http-github-webhook/public_repo_check.star ...`.
# Human readers running the README's relative-path commands (`cd examples/http-github-webhook && ./extbin run public_repo_check.star ...`)
# exercise the SAME effective command from a different cwd. The cwd difference
# is intentional and BOTH forms must produce the same outcome — the optional
# final sanity step below mirrors the README's relative-path form to catch
# drift in either direction.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$REPO_ROOT"

echo "==> Repo root: $REPO_ROOT"

# Build the example binary (skip if already built by an earlier CI step at /tmp/extbin).
EXTBIN="${EXTBIN:-/tmp/extbin}"
if [[ ! -x "$EXTBIN" ]]; then
    echo "==> Building extbin to $EXTBIN"
    go build -o "$EXTBIN" ./examples/http-github-webhook/cmd/extbin
else
    echo "==> Reusing extbin at $EXTBIN"
fi

# Start the Temporal dev server in the background. --headless is faster
# (~2s startup vs ~4s with web UI) and more deterministic in CI.
echo "==> Starting temporal dev server (background, headless)"
temporal server start-dev --headless &
TEMPORAL_PID=$!

# Trap-based cleanup — guarantees the temporal dev server is stopped even
# if the smoke fails partway through. `kill %1` would also work for the
# immediate-foreground case but the trap survives any exit path.
cleanup() {
    echo "==> Cleaning up temporal dev server (pid=$TEMPORAL_PID)"
    kill "$TEMPORAL_PID" 2>/dev/null || true
    wait "$TEMPORAL_PID" 2>/dev/null || true
}
trap cleanup EXIT

# Wait for the temporal dev server to be ready (up to 30s). RESEARCH § 5 documents
# this poll loop; alternative is `--retry-attempts 30` flag on the describe
# subcommand but the explicit loop gives clearer failure logs.
echo "==> Waiting for temporal server readiness"
for i in {1..30}; do
    if temporal operator namespace describe default --address localhost:7233 >/dev/null 2>&1; then
        echo "==> Temporal server ready after ${i}s"
        break
    fi
    if [[ "$i" == "30" ]]; then
        echo "ERROR: temporal server did not become ready within 30s"
        exit 1
    fi
    sleep 1
done

# Run the headline flow. This MUST match examples/http-github-webhook/README.md
# Quick Start step 4 BYTE-FOR-BYTE (modulo cwd: the README runs from the
# example dir, this script runs from the repo root).
echo "==> Running public_repo_check.star against api.github.com (octocat/Hello-World)"
# Capture stderr too: the renderer (pkg/cli/run.go:88) prints progress lines
# (incl. the `flow complete` terminator) to stderr; only the JSON result goes
# to stdout. Without 2>&1 the grep below can never match.
output=$("$EXTBIN" run examples/http-github-webhook/public_repo_check.star \
    --flow public_repo_check \
    --input '{"repo":"octocat/Hello-World"}' 2>&1)

echo "----- extbin run output -----"
echo "$output"
echo "----- end output -----"

# m13 sanity: re-run the SAME flow from inside the example directory, mirroring the
# README's relative-path command verbatim. Catches drift between the README's
# `cd examples/http-github-webhook && ./extbin run public_repo_check.star ...` form
# and this script's repo-root absolute-path form. If either form breaks, CI fails.
echo "==> [m13 sanity] Re-running from inside examples/http-github-webhook/ (mirrors README's relative-path form)"
pushd "$REPO_ROOT/examples/http-github-webhook" >/dev/null
output_relative=$("$EXTBIN" run public_repo_check.star \
    --flow public_repo_check \
    --input '{"repo":"octocat/Hello-World"}' 2>&1)
popd >/dev/null
echo "----- relative-path run output -----"
echo "$output_relative"
echo "----- end output -----"
if ! echo "$output_relative" | grep -q "flow complete"; then
    echo "ERROR: [m13 sanity] relative-path form did NOT produce 'flow complete' — README-form drift"
    exit 1
fi
echo "==> [m13 sanity] OK — both absolute and relative cwd forms produced 'flow complete'"

# Assert: the renderer prints `flow complete` (space, not underscore) at successful flow termination
# (verified pkg/cli/progress_static.go:245 and pkg/cli/progress_live.go:314). If you see
# `flow failed` instead, the run failed; check rate-limiting (RESEARCH Pitfall 3
# documents the 60/hr unauthenticated cap) before assuming a code regression.
# Note: the slog EVENT KIND is `flow_complete` (underscore) — that's an internal
# name; the renderer translates underscore → space when printing the terminator.
if echo "$output" | grep -q "flow complete"; then
    echo "==> SUCCESS: 'flow complete' substring present in output"
    SMOKE_RC=0
else
    echo "ERROR: 'flow complete' substring NOT found in extbin output"
    echo "       (Possible causes: rate-limiting, network down, regression in renderer)"
    SMOKE_RC=1
fi

# Phase 7.2 cron smoke — exercises the cron-reconcile → Schedule → workflow
# execution path end-to-end against a fresh ephemeral dev-temporal. Wall
# clock ~80 seconds; gated behind SKYTIME_RUN_CRON_SMOKE=1 so the default
# CI run isn't burdened. Humans can invoke it locally to validate ROADMAP
# Phase 7.2 success criterion #5. The exit code propagates: if the cron
# smoke fails, the overall walkthrough smoke fails.
if [[ "${SKYTIME_RUN_CRON_SMOKE:-}" == "1" ]]; then
    # The existing temporal server start-dev process is still running
    # under the EXIT trap above. Stop it so cron-schedules-smoke.sh can
    # spawn its own clean instance without a port collision on 7233.
    echo "==> [cron smoke] stopping the webhook walkthrough's temporal dev server before handoff"
    kill "$TEMPORAL_PID" 2>/dev/null || true
    wait "$TEMPORAL_PID" 2>/dev/null || true
    TEMPORAL_PID=""  # disarm the cleanup trap's second kill attempt

    echo "==> [cron smoke] running docs/walkthroughs/cron-schedules-smoke.sh (~80s)"
    if EXTBIN="$EXTBIN" bash "$REPO_ROOT/docs/walkthroughs/cron-schedules-smoke.sh"; then
        echo "==> [cron smoke] PASS"
    else
        echo "ERROR: cron-schedules-smoke.sh failed"
        SMOKE_RC=1
    fi
else
    echo "==> [cron smoke] skipping (set SKYTIME_RUN_CRON_SMOKE=1 to enable; ~80s wall clock)"
fi

exit "$SMOKE_RC"
