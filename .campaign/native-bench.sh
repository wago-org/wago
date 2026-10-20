#!/usr/bin/env bash
set -euo pipefail
repo="$GITHUB_WORKSPACE"
out="$RUNNER_TEMP/specialization-evidence"
campaign="$RUNNER_TEMP/specialization-controls"
mkdir -p "$out" "$campaign/locks"
cp .campaign/run.py "$campaign/run.py"
bash .campaign/native-pairs.sh "$repo" dfec8bf66ee360b1f21c7611d230c0498dfa61ab 21fb52e98cfd4f173f2d74350e8b4417dec09a31 "$out" "$campaign"
