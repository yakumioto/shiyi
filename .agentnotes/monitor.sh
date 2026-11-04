#!/usr/bin/env bash
# Poll implementer worktrees for new commits beyond the base.
# Prints one line per poll cycle, unbuffered.
set -u
ROOT=/home/mioto/workspace/me/shiyi
DEADLINE=$(( $(date +%s) + ${1:-3600} ))
WORKTREES=(01a-config-store-classify 01b-llm-client 02a-search-answer 02b-httpapi 06-web 05-cli 08-container-docs)
declare -A BASE
for w in "${WORKTREES[@]}"; do
  BASE[$w]=$(git -C "$ROOT/.worktrees/$w" rev-list --count HEAD 2>/dev/null || echo 0)
done

while [ "$(date +%s)" -lt "$DEADLINE" ]; do
  done_count=0
  line=""
  for w in "${WORKTREES[@]}"; do
    now=$(git -C "$ROOT/.worktrees/$w" rev-list --count HEAD 2>/dev/null || echo 0)
    if [ "$now" -gt "${BASE[$w]}" ]; then
      done_count=$((done_count+1))
      line="$line $w=OK"
    else
      line="$line $w=.."
    fi
  done
  echo "[$(date +%H:%M:%S)] $done_count/${#WORKTREES[@]} $line"
  if [ "$done_count" -eq "${#WORKTREES[@]}" ]; then
    echo "ALL WORKTREES HAVE NEW COMMITS"
    exit 0
  fi
  sleep 30
done
echo "TIMEOUT" >&2
exit 1
