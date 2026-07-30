#!/usr/bin/env bash
set -euo pipefail

: "${GOOS:?GOOS is required}"
: "${GOARCH:?GOARCH is required}"
: "${WAGO_VERSION:?WAGO_VERSION is required}"

release_out="${OUT_DIR:-.}"
release_target="${GOOS}-${GOARCH}"
mkdir -p "$release_out"

go_build() {
  local tags="$1"
  local output="$2"
  local -a args=(build -trimpath -ldflags="-s -w -X main.version=${WAGO_VERSION}")
  if [[ -n "$tags" ]]; then
    args+=(-tags "$tags")
  fi
  CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go "${args[@]}" -o "$output" ./cli/wago
}

manager="${release_out}/wago-${release_target}"
standard_normal="${release_out}/wago-runtime-standard-normal-${release_target}"
lite_normal="${release_out}/wago-runtime-lite-normal-${release_target}"
minimal_normal="${release_out}/wago-runtime-minimal-normal-${release_target}"
standard_tiny="${release_out}/wago-runtime-standard-tiny-${release_target}"
lite_tiny="${release_out}/wago-runtime-lite-tiny-${release_target}"
minimal_tiny="${release_out}/wago-runtime-minimal-tiny-${release_target}"

go_build wago_manager "$manager"
go_build "" "$standard_normal"
go_build wago_lite "$lite_normal"
go_build wago_minimal "$minimal_normal"

tinygo_build() {
  local tags="$1"
  local output="$2"
  local -a args=(
    build -scheduler="${TINYGO_SCHEDULER:-tasks}" -no-debug -opt=z -gc=conservative
    -ldflags "-X main.version=${WAGO_VERSION}"
  )
  if [[ -n "$tags" ]]; then
    args+=(-tags "$tags")
  fi
  GOOS="$GOOS" GOARCH="$GOARCH" tinygo "${args[@]}" -o "$output" ./cli/wago
}

try_tinygo_build() {
  local tags="$1"
  local output="$2"
  if tinygo_build "$tags" "$output"; then
    return 0
  fi
  rm -f "$output"
  printf 'warning: TinyGo could not build %s/%s; omitting unsupported asset\n' \
    "$GOOS" "$(basename "$output")" >&2
  return 0
}

try_tinygo_build "" "$standard_tiny"
try_tinygo_build wago_lite "$lite_tiny"
try_tinygo_build wago_lean,wago_minimal "$minimal_tiny"

for asset in \
  "$manager" \
  "$standard_normal" "$lite_normal" "$minimal_normal" \
  "$standard_tiny" "$lite_tiny" "$minimal_tiny"; do
  if [[ ! -s "$asset" ]]; then
    continue
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$asset" > "${asset}.sha256"
  else
    shasum -a 256 "$asset" > "${asset}.sha256"
  fi
done
