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
  local package="$3"
  local -a args=(build -trimpath -ldflags="-s -w -X main.version=${WAGO_VERSION}")
  if [[ -n "$tags" ]]; then
    args+=(-tags "$tags")
  fi
  CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go "${args[@]}" -o "$output" "$package"
}

manager="${release_out}/wago-${release_target}"
installer="${release_out}/wago-installer-${release_target}"
standard_normal="${release_out}/wago-runtime-standard-normal-${release_target}"
minimal_normal="${release_out}/wago-runtime-minimal-normal-${release_target}"
standard_tiny="${release_out}/wago-runtime-standard-tiny-${release_target}"
minimal_tiny="${release_out}/wago-runtime-minimal-tiny-${release_target}"

go_build "" "$manager" ./cli/wago
go_build "" "$installer" ./cli/installer
go_build wago_runtime "$standard_normal" ./cli/wago
go_build wago_runtime,wago_minimal "$minimal_normal" ./cli/wago

tinygo_build() {
  local tags="$1"
  local output="$2"
  local package="$3"
  local -a args=(
    build -scheduler="${TINYGO_SCHEDULER:-tasks}" -no-debug -opt=z -gc=conservative
    -ldflags "-X main.version=${WAGO_VERSION}"
  )
  if [[ -n "$tags" ]]; then
    args+=(-tags "$tags")
  fi
  GOOS="$GOOS" GOARCH="$GOARCH" tinygo "${args[@]}" -o "$output" "$package"
}

try_tinygo_build() {
  local tags="$1"
  local output="$2"
  local package="$3"
  if tinygo_build "$tags" "$output" "$package"; then
    return 0
  fi
  rm -f "$output"
  printf 'warning: TinyGo could not build %s/%s; omitting unsupported asset\n' \
    "$GOOS" "$(basename "$output")" >&2
  return 0
}

try_tinygo_build wago_runtime "$standard_tiny" ./cli/wago
try_tinygo_build wago_runtime,wago_lean,wago_minimal "$minimal_tiny" ./cli/wago

# TinyGo's no-debug Linux binaries do not use DWARF unwind tables for panic
# reporting or Wago's native trap path. Removing them saves file and mapped
# read-only data without weakening diagnostics. Other object formats retain
# their platform-native metadata.
if [[ "$GOOS" == "linux" ]]; then
  for asset in "$standard_tiny" "$minimal_tiny"; do
    if [[ -s "$asset" ]]; then
      strip -s --strip-section-headers \
        --remove-section=.eh_frame --remove-section=.eh_frame_hdr \
        --remove-section=.comment "$asset"
    fi
  done
fi

for asset in \
  "$manager" "$installer" \
  "$standard_normal" "$minimal_normal" \
  "$standard_tiny" "$minimal_tiny"; do
  if [[ ! -s "$asset" ]]; then
    continue
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$asset" > "${asset}.sha256"
  else
    shasum -a 256 "$asset" > "${asset}.sha256"
  fi
done
