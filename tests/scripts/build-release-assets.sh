#!/usr/bin/env bash
set -euo pipefail

repository_root=$(git rev-parse --show-toplevel)
test_root=$(mktemp -d)
trap 'rm -rf "$test_root"' EXIT

mkdir -p "$test_root/bin"

cat >"$test_root/bin/go" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
output=""
while (($# > 0)); do
  if [[ "$1" == "-o" ]]; then
    output="$2"
    break
  fi
  shift
done
[[ -n "$output" ]]
printf 'normal\n' >"$output"
EOF

cat >"$test_root/bin/tinygo" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf "called\n" >> "$CAPTURE_TINYGO"
output=""
while (($# > 0)); do
  if [[ "$1" == "-o" ]]; then
    output="$2"
    break
  fi
  shift
done
[[ -n "$output" ]]
printf 'tiny\n' >"$output"
EOF

cat >"$test_root/bin/strip" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$CAPTURE_STRIP"
EOF

chmod +x "$test_root/bin/go" "$test_root/bin/tinygo" "$test_root/bin/strip"

run_case() {
  local goos="$1"
  local arch="${2:-arm64}"
  local output="$test_root/out-$goos-$arch"
  local capture="$test_root/strip-$goos-$arch"
  mkdir -p "$output"
  : >"$capture"
  PATH="$test_root/bin:$PATH" \
    CAPTURE_STRIP="$capture" \
    CAPTURE_TINYGO="$test_root/tinygo-$goos-$arch" \
    GOOS="$goos" \
    GOARCH="$arch" \
    WAGO_VERSION=v0.0.0-test \
    OUT_DIR="$output" \
    "$repository_root/scripts/build-release-assets.sh"
}

run_case darwin
darwin_capture="$test_root/strip-darwin-arm64"
grep -Fx -- "-x $test_root/out-darwin-arm64/wago-runtime-standard-tiny-darwin-arm64" "$darwin_capture" >/dev/null
grep -Fx -- "-x $test_root/out-darwin-arm64/wago-runtime-minimal-tiny-darwin-arm64" "$darwin_capture" >/dev/null
[[ $(wc -l <"$darwin_capture") -eq 2 ]]

run_case linux
linux_capture="$test_root/strip-linux-arm64"
grep -Fx -- "-s --strip-section-headers --remove-section=.eh_frame --remove-section=.eh_frame_hdr --remove-section=.comment $test_root/out-linux-arm64/wago-runtime-standard-tiny-linux-arm64" "$linux_capture" >/dev/null
grep -Fx -- "-s --strip-section-headers --remove-section=.eh_frame --remove-section=.eh_frame_hdr --remove-section=.comment $test_root/out-linux-arm64/wago-runtime-minimal-tiny-linux-arm64" "$linux_capture" >/dev/null
[[ $(wc -l <"$linux_capture") -eq 2 ]]

run_case linux riscv64
[[ ! -e "$test_root/tinygo-linux-riscv64" ]]
[[ ! -s "$test_root/strip-linux-riscv64" ]]
"$repository_root/scripts/verify-channel-assets.sh" "$test_root/out-linux-riscv64" linux-riscv64
# Normal-only riscv64 assets are complete, but Linux ARM64 still requires Tiny.
rm "$test_root/out-linux-arm64/wago-runtime-standard-tiny-linux-arm64"
if "$repository_root/scripts/verify-channel-assets.sh" "$test_root/out-linux-arm64" linux-arm64; then
  echo "missing supported TinyGo asset was accepted" >&2
  exit 1
fi
# Unsupported binaries must never slip into the release's wildcard upload.
printf 'bad\n' > "$test_root/out-linux-riscv64/wago-runtime-standard-tiny-linux-riscv64"
if "$repository_root/scripts/verify-channel-assets.sh" "$test_root/out-linux-riscv64" linux-riscv64; then
  echo "unsupported riscv64 TinyGo asset was accepted" >&2
  exit 1
fi
echo "build-release-assets tests passed"
