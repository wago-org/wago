#!/usr/bin/env bash
# Small native-first RISE qualification; no benchmarks or broad corpus claims.
set -euo pipefail
if [[ $(uname -m) != riscv64 ]]; then
  echo "Native Linux/RV64 runner required." >&2
  exit 1
fi
test "$(go env GOOS)/$(go env GOARCH)" = linux/riscv64
go version
go env GOOS GOARCH GORISCV64 CGO_ENABLED GOMAXPROCS
mode=${1:-}
case "$mode" in
  smoke)
    # Actual mmap/mprotect/process-wide flush precede executable calls here.
    # Spy-only tests additionally check exact publication ordering and cleanup.
    go test ./src/core/runtime/riscv64 -count=1 -parallel=2 -timeout=3m \
      -run 'Test(NativeGrownImagePublication|NativeABIRegistersAndAlignment|RawStackBoundsBeforeWrites|NativeArithmeticTrapAndReuse|NativeCrossThreadPublicationAndGC|PublicationOrderAndWX|PublicationFailuresAndCleanupRetry)$' -v
    go test ./src/core/compiler/backend/railshot/riscv64 -count=1 -parallel=2 -timeout=3m \
      -run 'TestNative(RawReturnHookAndPrologueFence|IntegerControl|IntegerDeepFrameAndZeroAlloc|MemoryZeroLength|MemoryHighCapacityContextAndAllocations|GlobalsOnlyI32WidthsAndImmutability|BulkZeroMemoryAndGrowth|BulkPassiveOnlyDrop|DirectCallLeafWidthsAndEarlyReturn|DirectCallSharedThenRecursiveFence)$' -v
    go test ./src/core/compiler/backend/railshot/riscv64 -count=1 -parallel=2 -timeout=3m \
      -run 'TestMultivalue(IntegerExecution|ArityAndTrapDoNotCommit)$' -v
    ;;
  supported)
    go test ./src/core/encoder/riscv64 ./src/core/runtime/riscv64 \
      ./src/core/compiler/backend/railshot/riscv64 -count=1 -parallel=2 -timeout=5m -v
    go test -tags=wago_regalloccheck ./src/core/compiler/backend/railshot/riscv64 \
      -count=1 -parallel=2 -timeout=5m -v
    ;;
  public)
    # This is the bounded public profile, not a whole-Core conformance claim.
    # Artifact loading is exercised in a genuinely compiler-free executable.
    artifact_dir=$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/wago-rv64-public.XXXXXX")
    trap 'rm -rf -- "$artifact_dir"' EXIT
    unset WAGO_RV64_TABLE_STAGE_ARTIFACT_DIR WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE
    for check in ordinary checked; do
      tags=wago_rv64_experimental
      if [[ "$check" == checked ]]; then
        tags+=,wago_regalloccheck
      fi
      go test -tags="$tags" ./src/wago -count=1 -parallel=2 -timeout=5m -v \
        -run '^Test(RetainedResourceCleanup|ReferenceEntryCleanup|PhysicalFinalizerChain|ResourceCleanup|ImportDedup|FunctionImportDrain|BuilderRollbackContinues|Dependency|RV64.*(Reference|Global|NativeImport|Start|Table|Rollback))'
      WAGO_RV64_TABLE_STAGE_ARTIFACT_DIR="$artifact_dir/$check" \
        WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE=prepare \
        go test -tags="$tags" ./src/wago -count=1 -parallel=2 -timeout=5m -v \
        -run '^TestRV64PublicLocalMutableReference'
      WAGO_RV64_TABLE_STAGE_ARTIFACT_DIR="$artifact_dir/$check" \
        WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE=load \
        go test -tags="$tags,wago_precompiled" ./src/wago -count=1 -parallel=2 -timeout=5m -v \
        -run '^Test(RV64PublicLocalMutableReference|RV64ActualImportedTablePrecompiledCompilerGate)'
    done
    ;;
  *)
    echo "Usage: scripts/ci-riscv64.sh {smoke|supported|public}" >&2
    exit 2
    ;;
esac
