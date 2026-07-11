#!/usr/bin/env python3
"""Read newline-delimited JSON-RPC responses from stdin, print result for given id."""
import sys
import json

target = int(sys.argv[1]) if len(sys.argv) > 1 else 2

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        obj = json.loads(line)
    except json.JSONDecodeError:
        continue
    try:
        msg_id = int(obj.get("id", -1))
    except (TypeError, ValueError):
        continue
    if msg_id == target:
        result = obj.get("result", {})
        if isinstance(result, dict):
            for item in result.get("content", []):
                print(item.get("text", ""))
            if result.get("isError"):
                print("[TOOL ERROR]", file=sys.stderr)
        else:
            print(json.dumps(result, indent=2))
