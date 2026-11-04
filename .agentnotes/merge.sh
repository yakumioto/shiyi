#!/usr/bin/env bash
# Merge an implementer branch into the integration branch, run the gate,
# and report. Usage: merge.sh <branch> [message]
set -uo pipefail
ROOT=/home/mioto/workspace/me/shiyi
export GOENV=$ROOT/.goenv/env

BRANCH=${1:?usage: merge.sh <branch>}
MSG=${2:-"merge($BRANCH): integrate $(basename "$BRANCH")"}

cd "$ROOT"
if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "REFUSING: integration branch has uncommitted changes" >&2
  git status --short >&2
  exit 1
fi

echo "=== merging $BRANCH into feat/shiyi-v1 ==="
if ! git merge --no-ff "$BRANCH" -m "$MSG"; then
  echo "=== CONFLICT; resolving by keeping ours for frozen seams ===" >&2
  git status --short >&2
  exit 2
fi

echo "=== gate: gofmt / build / vet / test -race ==="
cd "$ROOT"
gofmt -l internal cmd 2>/dev/null | grep -v '^vendor/' && { echo "GOFMT ISSUES ABOVE" >&2; exit 3; }
if ! go build ./...; then echo "BUILD FAILED" >&2; exit 3; fi
if ! go vet ./...; then echo "VET FAILED" >&2; exit 3; fi
if ! go test ./... -race -count=1; then echo "TESTS FAILED" >&2; exit 3; fi

echo "=== OK: $BRANCH merged, gate green ==="
git log --oneline -3
