#!/usr/bin/env bash
set -euo pipefail

repository_root=$(git rev-parse --show-toplevel)
exec go run "$repository_root/tests/tools/release-qualification" "$@"
