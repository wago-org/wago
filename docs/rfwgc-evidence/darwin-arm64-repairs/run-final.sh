#!/bin/bash
set -u
export GOOS=linux GOARCH=arm64 CGO_ENABLED=0 GOWORK=off GOCACHE=/home/dev/.cache/wago-arm64-recovery GOROOT=/home/dev/.local/share/mise/installs/go/1.27.1
root=/home/dev/.cache/wago-arm64-recovery
cd "$root/final-source/src/core/compiler/backend/railshot/arm64"
for mode in ordinary wagodebug wago_regalloccheck; do
 for lane in focused diagnostics; do
  tags="$mode"
  if [ "$mode" = ordinary ]; then tags=""; fi
  if [ "$lane" = diagnostics ]; then tags="${tags:+$tags,}wago_codegenstats"; fi
  "$GOROOT/bin/go" test -tags "$tags" -c -o "$root/final-$mode-$lane.test" . > "$root/logs/final-$mode-$lane-build.log" 2>&1
  build=$?
  if [ "$lane" = focused ]; then
   select='^(TestDynamicImportUnwindLocalJoinARM64|TestNativeLeafContextShortcutGCPolicyARM64|TestTable.*Lowering|TestBulkPhysicalArgumentSlots|TestTableSetHelperCallHintsArm64)$'
  else
   select='^TestCompileWorkers(DeterministicArm64|SizeSharedAdaptersDeterministicArm64)$/^synthetic/'
  fi
  status=125
  if [ "$build" = 0 ]; then
   /tmp/qemu-x/usr/bin/qemu-aarch64 "$root/final-$mode-$lane.test" -test.run "$select" -test.v > "$root/logs/final-$mode-$lane.log" 2>&1
   status=$?
  fi
  echo "$mode $lane build=$build test=$status"
 done
done
