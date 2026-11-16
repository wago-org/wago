#!/usr/bin/env bash
set -euo pipefail
O=gc-arena-cache-gate
python3 "$O/run.py" --binary-dir "$O" --output-dir "$O/audit" --audit > "$O/audit.log" 2>&1
python3 "$O/run.py" --binary-dir "$O" --output-dir "$O/full" > "$O/full.log" 2>&1
