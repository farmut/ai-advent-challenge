#!/usr/bin/env bash
# show_tools.sh — reads newline-delimited JSON-RPC responses from stdin,
# finds the tools/list response, and prints each tool name + description.
DIR="$(cd "$(dirname "$0")" && pwd)"
python3 "$DIR/show_tools.py"
