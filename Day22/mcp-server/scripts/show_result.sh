#!/usr/bin/env bash
# show_result.sh <id> — reads newline-delimited JSON-RPC responses from stdin,
# finds the response with the given id, and prints the tool result text.
DIR="$(cd "$(dirname "$0")" && pwd)"
python3 "$DIR/show_result.py" "${1:-2}"
