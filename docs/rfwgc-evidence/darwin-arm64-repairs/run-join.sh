#!/bin/bash
set -eu
export GOOS=linux GOARCH=arm64 CGO_ENABLED=0 GOWORK=off GOCACHE=/home/dev/.cache/wago-arm64-recovery
export GOROOT=/home/dev/.local/share/mise/installs/go/1.27.1
cd /home/dev/.cache/wago-arm64-recovery/join-source
for mode in ordinary wagodebug wago_regalloccheck; do
 tags=()
 if [ "$mode" != ordinary ]; then tags=(-tags "$mode"); fi
 /home/dev/.local/share/mise/installs/go/1.27.1/bin/go test "${tags[@]}" -c -o "/home/dev/.cache/wago-arm64-recovery/join-$mode.test" ./src/core/compiler/backend/railshot/arm64 > "/home/dev/.cache/wago-arm64-recovery/logs/join-$mode-build.log" 2>&1
 /tmp/qemu-x/usr/bin/qemu-aarch64 "/home/dev/.cache/wago-arm64-recovery/join-$mode.test" -test.run '^(TestDynamicImportUnwindLocalJoinARM64|TestNativeLeafContextShortcutGCPolicyARM64)$' -test.v > "/home/dev/.cache/wago-arm64-recovery/logs/join-$mode.log" 2>&1
 echo "$mode PASS"
done
