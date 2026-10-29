#!/usr/bin/env bash
# Native-first RV64 qualification; QEMU checks establish functional behavior.
set -euo pipefail
test_exec=()
case "${WAGO_RV64_TEST_RUNNER:-native}" in
  native)
    if [[ $(uname -m) != riscv64 ]]; then
      echo "Native Linux/RV64 runner required." >&2
      exit 1
    fi
    ;;
  qemu)
    command -v qemu-riscv64 >/dev/null
    test_exec=(-exec "$(cd -- "$(dirname -- "$0")" && pwd)/qemu-riscv64.sh")
    echo "QEMU functional checks on $(uname -m); native qualification is separate."
    ;;
  *) echo "Unknown RV64 test runner." >&2; exit 2 ;;
esac
go_test() {
  if [[ ${WAGO_RV64_TEST_RUNNER:-native} == qemu ]]; then
    local args=() arg
    for arg in "$@"; do
      case "$arg" in
        -timeout=*) args+=("-timeout=${WAGO_RV64_TEST_TIMEOUT:-15m}") ;;
        *) args+=("$arg") ;;
      esac
    done
    go test "${test_exec[@]}" "${args[@]}"
  else
    go test "$@"
  fi
}
test "$(go env GOOS)/$(go env GOARCH)" = linux/riscv64
go version
go env GOOS GOARCH GORISCV64 CGO_ENABLED GOMAXPROCS GOFLAGS
mode=${1:-}
checks=(ordinary checked)
case "${WAGO_RV64_CI_CHECK:-all}" in
  all) ;;
  ordinary|checked) checks=("$WAGO_RV64_CI_CHECK") ;;
  *) echo "Unknown RV64 CI check: $WAGO_RV64_CI_CHECK" >&2; exit 2 ;;
esac
case "$mode" in
  smoke)
    # Actual mmap/mprotect/process-wide flush precede executable calls here.
    # Spy-only tests additionally check exact publication ordering and cleanup.
    go_test ./src/core/runtime/riscv64 -count=1 -parallel=2 -timeout=3m \
      -run 'Test(NativeGrownImagePublication|NativeABIRegistersAndAlignment|RawStackBoundsBeforeWrites|NativeArithmeticTrapAndReuse|NativeCrossThreadPublicationAndGC|PublicationOrderAndWX|PublicationFailuresAndCleanupRetry)$' -v
    go_test ./src/core/compiler/backend/railshot/riscv64 -count=1 -parallel=2 -timeout=3m \
      -run 'TestNative(RawReturnHookAndPrologueFence|IntegerControl|IntegerDeepFrameAndZeroAlloc|MemoryZeroLength|MemoryHighCapacityContextAndAllocations|GlobalsOnlyI32WidthsAndImmutability|BulkZeroMemoryAndGrowth|BulkPassiveOnlyDrop|DirectCallLeafWidthsAndEarlyReturn|DirectCallSharedThenRecursiveFence)$' -v
    go_test ./src/core/compiler/backend/railshot/riscv64 -count=1 -parallel=2 -timeout=3m \
      -run 'TestMultivalue(IntegerExecution|ArityAndTrapDoNotCommit)$' -v
    ;;
  supported)
    go_test ./src/core/encoder/riscv64 ./src/core/runtime/riscv64 \
      ./src/core/compiler/backend/railshot/riscv64 -count=1 -parallel=2 -timeout=5m -v
    go_test -tags=wago_regalloccheck ./src/core/compiler/backend/railshot/riscv64 \
      -count=1 -parallel=2 -timeout=5m -v
    ;;
  guards)
    for check in "${checks[@]}"; do
      tags=wago_guardpage
      if [[ "$check" == checked ]]; then
        tags+=,wago_regalloccheck
      fi
      go_test -tags="$tags" ./src/core/runtime ./src/wago \
        -count=1 -parallel=2 -timeout=5m -v \
        -run '^Test(RV64Guard|Guard|ClosedGuarded|MemoryBytesAfterGrowGuardPage|MemoryGrowExported|MemoryGrow|RV64Atomic|RV64SharedGrowth|Threads|InstallLinuxSignalHandlers|Memory64ActiveDataGlobalOffsetPreservesHighBits|Core1ActiveSegmentPreflight)'
    done
    ;;
  public)
    # Qualify public source compilation, execution, ownership, and cleanup.
    for check in "${checks[@]}"; do
      tags=""
      if [[ "$check" == checked ]]; then tags=wago_regalloccheck; fi
      go_test -tags="$tags" ./src/wago -count=1 -parallel=2 -timeout=15m -v \
        -run '^Test(RetainedResourceCleanup|ReferenceEntryCleanup|PhysicalFinalizerChain|ResourceCleanup|ImportDedup|FunctionImportDrain|BuilderRollbackContinues|Dependency|WideArithmetic|Threads|RV64)'
    done
    ;;
  *)
    echo "Usage: scripts/ci-riscv64.sh {smoke|supported|public|guards}" >&2
    exit 2
    ;;
esac
