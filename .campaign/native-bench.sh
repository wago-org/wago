#!/usr/bin/env bash
set -euo pipefail
repo="$GITHUB_WORKSPACE"
out="$RUNNER_TEMP/specialization-evidence"
campaign="$RUNNER_TEMP/specialization-controls"
mkdir -p "$out" "$campaign/locks"
cp .campaign/native-pairs.sh "$RUNNER_TEMP/native-pairs.sh"
cp .campaign/run.py "$campaign/run.py"
bash "$RUNNER_TEMP/native-pairs.sh" "$repo" b67b4c48d2500bc6032f793ac46dfe5262e083a1 e4abf0817b543c8d85d350cf2fe5ecd0a814eada "$out" "$campaign"
