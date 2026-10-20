#!/usr/bin/env bash
set -euo pipefail
repo="$GITHUB_WORKSPACE"
out="$RUNNER_TEMP/specialization-evidence"
campaign="$RUNNER_TEMP/specialization-controls"
mkdir -p "$out" "$campaign/locks"
cp .campaign/run.py "$campaign/run.py"
bash .campaign/native-pairs.sh "$repo" 9fe81ea747a852cb165d16685181d148249011f2 9fdedcb36288fcab5edaff8956b5363366f9971b "$out" "$campaign"
