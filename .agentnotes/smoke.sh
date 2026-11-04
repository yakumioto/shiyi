#!/usr/bin/env bash
# End-to-end smoke test: a fake OpenAI-compatible server, the real shiyi
# binary, a real data directory and real ripgrep.
set -uo pipefail

ROOT=/home/mioto/workspace/me/shiyi
export GOENV=$ROOT/.goenv/env
WORK=$(mktemp -d)
trap 'kill $LLM_PID $SHIYI_PID 2>/dev/null; rm -rf "$WORK"' EXIT

DATA="$WORK/data"
mkdir -p "$DATA"

# ---------------------------------------------------------------------------
# A fake model server. It decides record/query/unrecognized from the prompt it
# is given, and answers queries by citing whatever search result came back.
# ---------------------------------------------------------------------------
cat > "$WORK/fake_llm.py" <<'PY'
import json, re, sys
from http.server import BaseHTTPRequestHandler, HTTPServer

STATE = {"turns": 0}

class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        body = json.loads(self.rfile.read(length) or b"{}")
        messages = body.get("messages", [])
        system = next((m.get("content", "") for m in messages if m.get("role") == "system"), "")
        user = next((m.get("content", "") for m in reversed(messages) if m.get("role") == "user"), "")

        if "意图判定器" in system:
            # Classify by a marker the test puts in the text.
            if user.startswith("QUERY:"):
                kind = "query"
            elif user.startswith("SKIP:"):
                kind = "unrecognized"
            else:
                kind = "record"
            content = json.dumps({"kind": kind}, ensure_ascii=False)
        else:
            # Answer stage: look for a tool result in the conversation.
            tool_msgs = [m for m in messages if m.get("role") == "tool"]
            if not tool_msgs:
                # Ask for a search first.
                content = None
                payload = {
                    "model": body.get("model", "fake"),
                    "choices": [{
                        "index": 0,
                        "message": {
                            "role": "assistant",
                            "content": "",
                            "tool_calls": [{
                                "id": "call_1",
                                "type": "function",
                                "function": {
                                    "name": "search_memory",
                                    "arguments": json.dumps({"keywords": ["茶"], "limit": 5}),
                                },
                            }],
                        },
                        "finish_reason": "tool_calls",
                    }],
                }
            else:
                hits = json.loads(tool_msgs[-1].get("content") or "[]")
                times = [h["time"] for h in hits]
                if times:
                    answer = {"answer": "根据记录，你提到过茶。", "citations": times}
                else:
                    answer = {"answer": "巴黎是法国首都。", "citations": []}
                content = json.dumps(answer, ensure_ascii=False)
                payload = {
                    "model": body.get("model", "fake"),
                    "choices": [{
                        "index": 0,
                        "message": {"role": "assistant", "content": content},
                        "finish_reason": "stop",
                    }],
                }
            if content is None:
                pass
            else:
                pass

        if content is not None:
            payload = {
                "model": body.get("model", "fake"),
                "choices": [{
                    "index": 0,
                    "message": {"role": "assistant", "content": content},
                    "finish_reason": "stop",
                }],
            }

        data = json.dumps(payload, ensure_ascii=False).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

port = int(sys.argv[1])
HTTPServer(("127.0.0.1", port), Handler).serve_forever()
PY

LLM_PORT=18923
python3 "$WORK/fake_llm.py" $LLM_PORT &
LLM_PID=$!
sleep 1

cat > "$WORK/config.toml" <<EOF
listen = "127.0.0.1:18924"
data_dir = "$DATA"
shard = "monthly"

[answer]
base_url = "http://127.0.0.1:$LLM_PORT"
model = "fake-model"
api_key_env = "SHIYI_TEST_KEY"
timeout = "10s"
temperature = 0
EOF

export SHIYI_CONFIG="$WORK/config.toml"
export SHIYI_TEST_KEY="test-key-value"

"$ROOT/shiyi" serve > "$WORK/serve.log" 2>&1 &
SHIYI_PID=$!
sleep 1.5

fail=0
check() {
  local name="$1" want="$2" got="$3"
  if [[ "$got" == *"$want"* ]]; then
    echo "  PASS  $name"
  else
    echo "  FAIL  $name"
    echo "        want substring: $want"
    echo "        got: $got"
    fail=$((fail+1))
  fi
}

echo "== healthz =="
H=$(curl -sS -w '\n%{http_code}' http://127.0.0.1:18924/healthz)
check "healthz status" "200" "$H"
check "healthz body" '"status":"ok"' "$H"

echo "== record =="
R=$(curl -sS -X POST -H 'Content-Type: application/json' -d '{"content":"我喜欢喝茶"}' http://127.0.0.1:18924/messages)
check "record kind" '"kind":"record"' "$R"
check "record version" '"version":1' "$R"

echo "== stored row shape =="
ROW=$(head -1 "$DATA"/*.jsonl)
check "row has version" '"version":1' "$ROW"
check "row has content" '我喜欢喝茶' "$ROW"
check "row has time" '"time":"' "$ROW"

echo "== unrecognized writes nothing =="
BEFORE=$(cat "$DATA"/*.jsonl | wc -l)
U=$(curl -sS -X POST -H 'Content-Type: application/json' -d '{"content":"SKIP:随便聊聊"}' http://127.0.0.1:18924/messages)
check "unrecognized kind" '"kind":"unrecognized"' "$U"
AFTER=$(cat "$DATA"/*.jsonl | wc -l)
if [[ "$BEFORE" == "$AFTER" ]]; then echo "  PASS  unrecognized wrote no row"; else echo "  FAIL  unrecognized wrote a row"; fail=$((fail+1)); fi

echo "== query with citation =="
Q=$(curl -sS -X POST -H 'Content-Type: application/json' -d '{"content":"QUERY:我喜欢什么茶？"}' http://127.0.0.1:18924/messages)
check "query kind" '"kind":"query"' "$Q"
check "query status ok" '"status":"ok"' "$Q"
check "citation carries original text" '我喜欢喝茶' "$Q"

echo "== query wrote nothing =="
AFTER2=$(cat "$DATA"/*.jsonl | wc -l)
if [[ "$AFTER" == "$AFTER2" ]]; then echo "  PASS  query wrote no row"; else echo "  FAIL  query wrote a row"; fail=$((fail+1)); fi

echo "== empty content is 400 =="
E=$(curl -sS -w '\n%{http_code}' -X POST -H 'Content-Type: application/json' -d '{"content":"   "}' http://127.0.0.1:18924/messages)
check "empty content 400" "400" "$E"
check "empty content code" '"code":"invalid_request"' "$E"

echo "== malformed JSON is 400 =="
M=$(curl -sS -w '\n%{http_code}' -X POST -H 'Content-Type: application/json' -d '{not json' http://127.0.0.1:18924/messages)
check "bad json 400" "400" "$M"

echo "== web page served =="
W=$(curl -sS -D - -o /dev/null http://127.0.0.1:18924/ 2>&1)
check "web 200" "200" "$W"
check "web is html" 'text/html' "$W"

echo
if [[ $fail -eq 0 ]]; then
  echo "ALL SMOKE CHECKS PASSED"
  exit 0
fi
echo "$fail SMOKE CHECK(S) FAILED"
exit 1
