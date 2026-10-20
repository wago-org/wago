#!/usr/bin/env bash
set -euo pipefail
repo="$GITHUB_WORKSPACE"
out="$RUNNER_TEMP/specialization-evidence"
campaign="$RUNNER_TEMP/specialization-controls"
mkdir -p "$out" "$campaign/locks"
cp .campaign/run.py "$campaign/run.py"
bash .campaign/native-pairs.sh "$repo" 7997c86c36e05cfba4c4f39cabb026a318f55fc0 8edac8455ea6777847d648e49bd9eeaeb6231017 "$out" "$campaign"
