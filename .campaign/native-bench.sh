#!/usr/bin/env bash
set -euo pipefail
repo="$GITHUB_WORKSPACE"
out="$RUNNER_TEMP/specialization-evidence"
campaign="$RUNNER_TEMP/specialization-controls"
mkdir -p "$out" "$campaign/locks"
cp .campaign/run.py "$campaign/run.py"
bash .campaign/native-pairs.sh "$repo" b67b4c48d2500bc6032f793ac46dfe5262e083a1 4a47d4f2ce0f775369734b9583f6f3774cab83d3 "$out" "$campaign"
