#!/usr/bin/env bash
set -euo pipefail
repo="$GITHUB_WORKSPACE"
out="$RUNNER_TEMP/specialization-evidence"
campaign="$RUNNER_TEMP/specialization-controls"
mkdir -p "$out" "$campaign/locks"
cp .campaign/native-pairs.sh "$RUNNER_TEMP/native-pairs.sh"
cp .campaign/run.py "$campaign/run.py"
bash "$RUNNER_TEMP/native-pairs.sh" "$repo" c4d0b1ce87c21717975222bfef19f88731f8bfbf b99880e7d83e5d3d72898cfea7e5fb295ba4b0ba "$out" "$campaign"
