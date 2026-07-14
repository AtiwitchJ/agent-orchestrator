#!/usr/bin/env bash
#
# Manual smoke test for the hybrid approval-gate policy pipeline (Phase 1):
# boots the daemon, registers a project, enables policy, opens a real PR via
# gh, and taps the CDC event stream so an operator can watch events land.
#
# Requires: `ao` on PATH (built from this branch), `git`, and `gh`
# authenticated against a real GitHub repo you have push access to.
#
# Usage:
#   test/cli/run-policy-smoke.sh <path-to-git-repo>
#
# <path-to-git-repo> must be a local clone of a GitHub repo you can push a
# branch to and open PRs against, with `gh auth status` already passing. This
# script creates a throwaway branch + PR and cleans both up on exit — it never
# touches the repo's default branch.
#
# Note: the policy *engine* (gate execution and the policy_run_started /
# gate_passed / ... CDC events it will emit) is not implemented yet — only
# the config surface (`ao policy show|set`) and the DB schema
# (migration 0029) exist. This script exercises what IS wired today: daemon
# boot, project registration, the policy config round-trip, PR creation, and
# the raw CDC event stream. Once the engine lands, extend the "watching
# events" step to also assert policy_run_* events appear for the opened PR.

set -euo pipefail

REPO_PATH="${1:-}"
if [ -z "$REPO_PATH" ]; then
  echo "usage: $0 <path-to-git-repo>" >&2
  echo "  <path-to-git-repo> must be a local clone with 'gh' authenticated" >&2
  echo "  against its GitHub remote (push access required)." >&2
  exit 2
fi
REPO_PATH="$(cd "$REPO_PATH" && pwd)"

AO_BIN="${AO_BIN:-ao}"
fail() { echo "FAIL: $1" >&2; exit 1; }

command -v "$AO_BIN" >/dev/null || fail "$AO_BIN not on PATH"
command -v git >/dev/null || fail "git not on PATH"
command -v gh >/dev/null || fail "gh not on PATH"
gh auth status >/dev/null 2>&1 || fail "gh is not authenticated (run 'gh auth login')"
git -C "$REPO_PATH" rev-parse --git-dir >/dev/null 2>&1 || fail "$REPO_PATH is not a git repo"
git -C "$REPO_PATH" remote get-url origin >/dev/null 2>&1 || fail "$REPO_PATH has no 'origin' remote"

tmp="$(mktemp -d)"
export AO_RUN_FILE="$tmp/running.json"
export AO_DATA_DIR="$tmp/data"
export AO_PORT="${AO_PORT:-39191}"

suffix="$$"
PROJECT_ID="policy-smoke-$suffix"
BRANCH="policy-smoke/$suffix"
EVENTS_LOG="$tmp/events.log"
DAEMON_PID=""
EVENTS_PID=""
PR_URL=""

cleanup() {
  set +e
  if [ -n "$EVENTS_PID" ]; then
    kill "$EVENTS_PID" >/dev/null 2>&1
    wait "$EVENTS_PID" 2>/dev/null
  fi
  if [ -n "$PR_URL" ]; then
    echo "==> closing $PR_URL"
    gh pr close "$PR_URL" --delete-branch >/dev/null 2>&1
  fi
  if git -C "$REPO_PATH" rev-parse --verify "$BRANCH" >/dev/null 2>&1; then
    git -C "$REPO_PATH" push origin --delete "$BRANCH" >/dev/null 2>&1
    git -C "$REPO_PATH" checkout - >/dev/null 2>&1
    git -C "$REPO_PATH" branch -D "$BRANCH" >/dev/null 2>&1
  fi
  if [ -n "$DAEMON_PID" ]; then
    "$AO_BIN" stop >/dev/null 2>&1
    wait "$DAEMON_PID" 2>/dev/null
  fi
  rm -rf "$tmp"
}
trap cleanup EXIT

echo "==> booting daemon (port $AO_PORT)"
"$AO_BIN" daemon &
DAEMON_PID=$!
deadline=$((SECONDS + 15))
until "$AO_BIN" status --json 2>/dev/null | grep -q '"state": "ready"'; do
  if [ "$SECONDS" -ge "$deadline" ]; then
    fail "daemon did not become ready within 15s"
  fi
  sleep 0.5
done
echo "==> daemon ready"

echo "==> registering project $PROJECT_ID at $REPO_PATH"
"$AO_BIN" project add --path "$REPO_PATH" --id "$PROJECT_ID" >/dev/null || fail "project add"

echo "==> enabling policy for $PROJECT_ID"
"$AO_BIN" policy set "$PROJECT_ID" --enabled --tracker-label agent-ready >/dev/null || fail "policy set"

echo "==> verifying policy config"
show_out="$("$AO_BIN" policy show "$PROJECT_ID")"
echo "$show_out" | grep -q '"enabled": true' || fail "policy show did not report enabled: true; got: $show_out"
echo "$show_out"

echo "==> watching the CDC event stream in the background"
curl -sN "http://127.0.0.1:${AO_PORT}/api/v1/events" >"$EVENTS_LOG" &
EVENTS_PID=$!

echo "==> opening a throwaway PR via gh"
git -C "$REPO_PATH" checkout -b "$BRANCH"
date >"$REPO_PATH/.ao-policy-smoke"
git -C "$REPO_PATH" add .ao-policy-smoke
git -C "$REPO_PATH" -c user.email="ao-smoke@example.com" -c user.name="ao policy smoke" \
  commit -m "chore: ao policy smoke test (safe to discard)" >/dev/null
git -C "$REPO_PATH" push -u origin "$BRANCH" >/dev/null
PR_URL="$(gh pr create \
  --repo "$(git -C "$REPO_PATH" remote get-url origin)" \
  --head "$BRANCH" \
  --title "AO policy smoke test (safe to close)" \
  --body "Opened by test/cli/run-policy-smoke.sh; closed automatically on script exit." \
  | tail -1)"
echo "==> opened $PR_URL"

echo "==> letting the event stream run for 5s"
sleep 5
kill "$EVENTS_PID" >/dev/null 2>&1
wait "$EVENTS_PID" 2>/dev/null
EVENTS_PID=""
echo "==> captured $(wc -l <"$EVENTS_LOG" | tr -d ' ') event-stream line(s):"
cat "$EVENTS_LOG"

echo
echo "policy smoke: OK (daemon boot, project register, policy enable/show, PR open, event stream all worked)"
echo "note: gate/run CDC events (policy_run_started, gate_passed, ...) require the not-yet-implemented policy Engine — their absence above is expected until that lands."
