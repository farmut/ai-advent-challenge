#!/usr/bin/env python3
"""Read newline-delimited JSON-RPC responses from stdin, print tool list."""
import sys
import json

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        obj = json.loads(line)
    except json.JSONDecodeError:
        continue
    result = obj.get("result", {})
    if isinstance(result, dict) and "tools" in result:
        tools = result["tools"]
        print(f"  Found {len(tools)} tools:")
        for t in tools:
            desc = t.get("description", "")[:70]
            print(f"    • {t['name']}: {desc}")
