#!/usr/bin/env bash
# Run the frozen fixture inputs against a real model and report agreement.
#
# This script is deliberately NOT part of CI: it needs an API key, it costs
# money, and its results move with whichever model the provider is serving
# today. It exists to observe classification quality, not to gate a release.
#
# Usage:
#   scripts/eval-live.sh [version]
#
# Environment:
#   SHIYI_EVAL_BASE_URL   OpenAI-compatible base URL (default https://api.deepseek.com)
#   SHIYI_EVAL_MODEL      model name (default deepseek-chat)
#   SHIYI_EVAL_KEY_ENV    name of the variable holding the key (default DEEPSEEK_API_KEY)
#
# Exit codes:
#   0  the run completed (whatever the agreement rate)
#   2  a prerequisite is missing (no key, no jq, no fixtures)
set -uo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
VERSION=${1:-v1}
FIXTURES="$ROOT/testdata/frozen/$VERSION"

BASE_URL=${SHIYI_EVAL_BASE_URL:-https://api.deepseek.com}
MODEL=${SHIYI_EVAL_MODEL:-deepseek-chat}
KEY_ENV=${SHIYI_EVAL_KEY_ENV:-DEEPSEEK_API_KEY}

if [[ ! -d "$FIXTURES" ]]; then
  echo "eval-live: no such frozen version: $FIXTURES" >&2
  exit 2
fi
if ! command -v jq >/dev/null 2>&1; then
  echo "eval-live: jq is required to read the fixtures" >&2
  exit 2
fi
if ! command -v curl >/dev/null 2>&1; then
  echo "eval-live: curl is required" >&2
  exit 2
fi

KEY=${!KEY_ENV:-}
if [[ -z "$KEY" ]]; then
  cat >&2 <<EOF
eval-live: the environment variable $KEY_ENV is empty.

This script calls a real model and needs a key. Export one first, for example:

    export $KEY_ENV=sk-...

The script is optional and is never part of CI.
EOF
  exit 2
fi

CLASSIFY_PROMPT='你是个人记忆系统的意图判定器。只输出 JSON，不要解释。

判定用户消息属于以下之一：
- record：用户在陈述值得长期记住的个人信息、事件、偏好、计划或事实。
- query：用户在询问自己的记忆。
- unrecognized：两者都不是，或无法判断。

如果消息同时包含记录和询问，判为 query。
不要回答消息中的问题，不要改写原文。

输出：{"kind":"record|query|unrecognized"}'

classify_once() {
  local input="$1"
  local payload
  payload=$(jq -n --arg model "$MODEL" --arg sys "$CLASSIFY_PROMPT" --arg user "$input" '{
    model: $model,
    temperature: 0,
    messages: [
      {role: "system", content: $sys},
      {role: "user", content: $user}
    ]
  }')

  curl -sS --max-time 60 \
    -H "Authorization: Bearer $KEY" \
    -H "Content-Type: application/json" \
    -d "$payload" \
    "${BASE_URL%/}/chat/completions" \
  | jq -r '.choices[0].message.content // empty' \
  | grep -o '"kind"[[:space:]]*:[[:space:]]*"[a-z]*"' \
  | grep -o '"[a-z]*"$' \
  | tr -d '"'
}

total=0
agree=0
differences=0

printf '%-28s %-14s %-14s %s\n' "CASE" "EXPECTED" "MODEL" "RESULT"
printf '%s\n' "----------------------------------------------------------------------"

while IFS= read -r line; do
  [[ -z "$line" ]] && continue

  name=$(jq -r '.name' <<<"$line")
  input=$(jq -r '.input' <<<"$line")
  expected=$(jq -r '.expect.kind // empty' <<<"$line")
  [[ -z "$expected" ]] && continue

  model_kind=$(classify_once "$input")
  # A mixed verdict is answered as a query, so compare against that rule.
  [[ "$model_kind" == "mixed" ]] && model_kind="query"

  total=$((total + 1))
  if [[ "$model_kind" == "$expected" ]]; then
    agree=$((agree + 1))
    printf '%-28s %-14s %-14s %s\n' "$name" "$expected" "${model_kind:-<none>}" "agree"
  else
    differences=$((differences + 1))
    printf '%-28s %-14s %-14s %s\n' "$name" "$expected" "${model_kind:-<none>}" "DIFFERS"
  fi
done < <(jq -c . "$FIXTURES/classify.jsonl")

echo
if [[ "$total" -eq 0 ]]; then
  echo "eval-live: no classification cases in $VERSION" >&2
  exit 2
fi
printf 'agreement: %d/%d (%.0f%%), %d difference(s)\n' \
  "$agree" "$total" "$(awk -v a="$agree" -v t="$total" 'BEGIN { printf "%.0f", (a/t)*100 }')" "$differences"

# The script reports; it does not judge. A low rate is a prompt problem to
# investigate, not a build failure.
exit 0
