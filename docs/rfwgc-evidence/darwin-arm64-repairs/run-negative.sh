#!/bin/bash
set -eu
root=/home/dev/.cache/wago-arm64-recovery
export GOOS=linux GOARCH=arm64 CGO_ENABLED=0 GOWORK=off GOCACHE="$root" GOROOT=/home/dev/.local/share/mise/installs/go/1.27.1
cd "$root/review-source"
"$GOROOT/bin/go" test -overlay "$root/negative-overlay.json" -c -o "$root/negative.test" ./src/core/compiler/backend/railshot/arm64 > "$root/logs/negative-build.log" 2>&1
set +e
/tmp/qemu-x/usr/bin/qemu-aarch64 "$root/negative.test" -test.run '^TestDynamicImportUnwindLocalJoinARM64$' -test.v > "$root/logs/negative.log" 2>&1
status=$?
echo "negative test=$status (expected 1)"
test "$status" = 1
