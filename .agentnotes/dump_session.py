#!/usr/bin/env python3
"""Dump the tail of a DSH subagent session transcript."""
import json
import subprocess
import sys

path = sys.argv[1]
tail = int(sys.argv[2]) if len(sys.argv) > 2 else 20

raw = subprocess.run(
    ["zstd", "-dc", path], capture_output=True
).stdout.decode("utf8", errors="replace")

for line in raw.splitlines():
    line = line.strip()
    if not line:
        continue
    try:
        obj = json.loads(line)
    except Exception:
        continue
    t = obj.get("type", "")
    if t not in ("message", "tool_call", "tool_result", "agent_message", "assistant"):
        continue
    data = obj.get("data", {})
    role = data.get("role") or obj.get("role") or ""
    content = data.get("content") or data.get("text") or data.get("output") or ""
    if isinstance(content, list):
        parts = []
        for c in content:
            if isinstance(c, dict):
                parts.append(c.get("text") or c.get("content") or str(c)[:200])
            else:
                parts.append(str(c)[:200])
        content = " ".join(parts)
    content = str(content)
    if not content.strip():
        continue
    print(f"--- seq={obj.get('seq')} type={t} role={role}")
    print(content[:1500])
    print()
