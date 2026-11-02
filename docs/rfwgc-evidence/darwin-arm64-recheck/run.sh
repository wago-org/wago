#!/bin/bash
set -u

source_commit=b9433aa2050c8069baa3edbaa50bd4e8e9c95b26
base_commit=4f1509996d5a3601f1bd6061b638153153c7f465
evidence_dir=/home/dev/.t3/worktrees/wago/experiment-rfwgc-port/docs/rfwgc-evidence/darwin-arm64-recheck
run_root=/tmp/wago-darwin-arm64-recheck
source_root="$run_root/source"
negative_root="$run_root/negative-source"
go_root=/home/dev/.local/share/mise/installs/go/1.27.1
qemu=/tmp/qemu-x/usr/bin/qemu-aarch64

mkdir -p "$evidence_dir/logs" "$run_root"
rm -rf "$source_root" "$negative_root"
mkdir -p "$source_root" "$negative_root"
git archive "$source_commit" | tar -x -C "$source_root"
git archive "$source_commit" | tar -x -C "$negative_root"
git show "$base_commit:src/core/compiler/backend/railshot/arm64/call.go" > "$negative_root/src/core/compiler/backend/railshot/arm64/call.go"

export GOOS=linux GOARCH=arm64 CGO_ENABLED=0 GOWORK=off GOROOT="$go_root"
export GOCACHE="$run_root/go-cache"

{
  git rev-parse HEAD
  git rev-parse "$source_commit^{commit}"
  git rev-parse "$base_commit^{commit}"
  "$go_root/bin/go" version
  "$qemu" --version
  sha256sum "$qemu"
} > "$evidence_dir/environment.log" 2>&1

focused='^(TestDynamicImportUnwindLocalJoinARM64|TestNativeLeafContextShortcutGCPolicyARM64|TestTable.*Lowering|TestBulkPhysicalArgumentSlots|TestTableSetHelperCallHintsArm64|TestWorkerModuleStatsComparisonARM64)$'
diagnostics='^TestCompileWorkers(DeterministicArm64|SizeSharedAdaptersDeterministicArm64)$/^synthetic/'

: > "$evidence_dir/results.tsv"
for mode in ordinary wagodebug wago_regalloccheck; do
  for lane in focused diagnostics; do
    tags="$mode"
    if [ "$mode" = ordinary ]; then tags=""; fi
    if [ "$lane" = diagnostics ]; then tags="${tags:+$tags,}wago_codegenstats"; fi
    binary="$run_root/$mode-$lane.test"
    build_log="$evidence_dir/logs/$mode-$lane-build.log"
    test_log="$evidence_dir/logs/$mode-$lane.log"
    (
      cd "$source_root/src/core/compiler/backend/railshot/arm64" || exit 126
      "$go_root/bin/go" test -tags "$tags" -c -o "$binary" .
    ) > "$build_log" 2>&1
    build=$?
    test_status=125
    if [ "$build" -eq 0 ]; then
      selector="$focused"
      if [ "$lane" = diagnostics ]; then selector="$diagnostics"; fi
      (
        cd "$source_root/src/core/compiler/backend/railshot/arm64" || exit 126
        "$qemu" "$binary" -test.run "$selector" -test.v
      ) > "$test_log" 2>&1
      test_status=$?
    fi
    printf '%s\t%s\t%s\t%s\n' "$mode" "$lane" "$build" "$test_status" >> "$evidence_dir/results.tsv"
  done
done

negative_binary="$run_root/negative.test"
(
  cd "$negative_root/src/core/compiler/backend/railshot/arm64" || exit 126
  "$go_root/bin/go" test -c -o "$negative_binary" .
) > "$evidence_dir/logs/negative-build.log" 2>&1
negative_build=$?
negative_test=125
if [ "$negative_build" -eq 0 ]; then
  (
    cd "$negative_root/src/core/compiler/backend/railshot/arm64" || exit 126
    "$qemu" "$negative_binary" -test.run '^TestDynamicImportUnwindLocalJoinARM64$' -test.v
  ) > "$evidence_dir/logs/negative.log" 2>&1
  negative_test=$?
fi
printf 'negative-original-call.go\tfocused\t%s\t%s\n' "$negative_build" "$negative_test" >> "$evidence_dir/results.tsv"

sha256sum "$run_root"/*.test > "$evidence_dir/test-binary-sha256.txt"
exit 0
