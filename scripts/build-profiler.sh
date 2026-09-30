#!/usr/bin/env bash
set -euo pipefail
repo_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
revision="$(git -C "$repo_dir" rev-parse HEAD)"
dirty=false
if [[ -n "$(git -C "$repo_dir" status --porcelain)" ]]; then dirty=true; fi
output="${1:-/tmp/wagoprof}"
mode="${2:-standalone}"
if [[ "$output" != /* ]]; then output="$PWD/$output"; fi
case "$mode" in
 standalone) package=./cmd/wagoprof; build_dir="$repo_dir/bench" ;;
 --cli) package=./cli/wago; build_dir="$repo_dir" ;;
 *) echo 'usage: build-profiler.sh [output] [--cli]' >&2; exit 2 ;;
esac
cd "$build_dir"
CGO_ENABLED=0 go build -tags=wago_profile -ldflags "-X github.com/wago-org/wago/internal/profcapture.BuildRevision=$revision -X github.com/wago-org/wago/internal/profcapture.BuildDirty=$dirty" -o "$output" "$package"
