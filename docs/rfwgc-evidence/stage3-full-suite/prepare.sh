#!/usr/bin/env bash
# Invoke from the assigned branch worktree. No checkout mutation.
set -euo pipefail
revision=d546a1af4513c63e4bda6fae7d19cf9cd86f332a
destination=${1:-.tools/stage3-full-suite/current}
bash docs/rfwgc-evidence/stage3-toolchain/prepare-export.sh "$revision" "$destination"
bash docs/rfwgc-evidence/stage3-full-suite/fixtures.sh "$revision" "$destination"
# PowerShell and other installed commands may resolve through mise shims.
# Trust only the pinned private export's inspected configuration.
mise trust "$destination/mise.toml"
python3 docs/rfwgc-evidence/stage3-full-suite/verify-export.py "$revision" "$destination"
