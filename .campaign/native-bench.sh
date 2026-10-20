#!/usr/bin/env bash
set -euo pipefail
repo="$GITHUB_WORKSPACE"
out="$RUNNER_TEMP/specialization-evidence"
campaign="$RUNNER_TEMP/specialization-controls"
mkdir -p "$out" "$campaign/locks"
cp .campaign/run.py "$campaign/run.py"
bash .campaign/native-pairs.sh "$repo" 7997c86c36e05cfba4c4f39cabb026a318f55fc0 a91b328e76fa00808a6dd65253845d61f4ed1b14 "$out" "$campaign"
