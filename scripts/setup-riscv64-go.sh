#!/usr/bin/env bash
# Install a checksum-verified Go toolchain on the native RISE riscv64 runner.
set -euo pipefail
: "${GO_VERSION:?GO_VERSION is required}"
: "${RUNNER_TEMP:?RUNNER_TEMP is required}"
: "${GITHUB_PATH:?GITHUB_PATH is required}"
: "${GITHUB_ENV:?GITHUB_ENV is required}"
[[ $(uname -s)/$(uname -m) == Linux/riscv64 ]]
[[ "$GO_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]
tools_dir="$RUNNER_TEMP/wago-riscv64-tools"
mkdir -p "$tools_dir"
curl --fail --silent --show-error --location --retry 2 --max-time 60 \
  'https://go.dev/dl/?mode=json&include=all' > "$tools_dir/releases.json"
jq -er --arg version "go$GO_VERSION" \
  '[.[] | select(.version == $version) | .files[] |
    select(.os == "linux" and .arch == "riscv64" and .kind == "archive")] |
    if length == 1 then .[0] | [.filename,.sha256] | @tsv else error("Go release unavailable") end' \
  "$tools_dir/releases.json" > "$tools_dir/release.tsv"
read -r archive checksum < "$tools_dir/release.tsv"
[[ "$archive" == "go$GO_VERSION.linux-riscv64.tar.gz" ]]
[[ "$checksum" =~ ^[0-9a-f]{64}$ ]]
curl --fail --silent --show-error --location --retry 2 --max-time 180 \
  "https://go.dev/dl/$archive" -o "$tools_dir/$archive"
(cd "$tools_dir" && printf '%s  %s\n' "$checksum" "$archive" | sha256sum --check --strict)
tar -C "$tools_dir" -xzf "$tools_dir/$archive"
echo "$tools_dir/go/bin" >> "$GITHUB_PATH"
{
  echo "GOCACHE=$RUNNER_TEMP/wago-riscv64-go-cache"
  echo "GOMODCACHE=$RUNNER_TEMP/wago-riscv64-module-cache"
  echo 'GOTOOLCHAIN=local'
  echo 'CGO_ENABLED=0'
  echo 'GORISCV64=rva20u64'
  echo 'GOMAXPROCS=2'
  # Go 1.27.1's native riscv64 compiler needs serial function compilation.
  echo 'GOFLAGS=-gcflags=-c=1'
} >> "$GITHUB_ENV"
