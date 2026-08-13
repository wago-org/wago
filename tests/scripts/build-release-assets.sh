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
  local output="$test_root/out-$goos"
  local capture="$test_root/strip-$goos"
  mkdir -p "$output"
  : >"$capture"
  PATH="$test_root/bin:$PATH" \
    CAPTURE_STRIP="$capture" \
    GOOS="$goos" \
    GOARCH=arm64 \
    WAGO_VERSION=v0.0.0-test \
    OUT_DIR="$output" \
    "$repository_root/scripts/build-release-assets.sh"
}

run_case darwin
darwin_capture="$test_root/strip-darwin"
grep -Fx -- "-x $test_root/out-darwin/wago-runtime-standard-tiny-darwin-arm64" "$darwin_capture" >/dev/null
grep -Fx -- "-x $test_root/out-darwin/wago-runtime-minimal-tiny-darwin-arm64" "$darwin_capture" >/dev/null
[[ $(wc -l <"$darwin_capture") -eq 2 ]]

run_case linux
linux_capture="$test_root/strip-linux"
grep -Fx -- "-s --strip-section-headers --remove-section=.eh_frame --remove-section=.eh_frame_hdr --remove-section=.comment $test_root/out-linux/wago-runtime-standard-tiny-linux-arm64" "$linux_capture" >/dev/null
grep -Fx -- "-s --strip-section-headers --remove-section=.eh_frame --remove-section=.eh_frame_hdr --remove-section=.comment $test_root/out-linux/wago-runtime-minimal-tiny-linux-arm64" "$linux_capture" >/dev/null
[[ $(wc -l <"$linux_capture") -eq 2 ]]

echo "build-release-assets tests passed"
