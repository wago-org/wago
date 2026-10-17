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
go env GOOS GOARCH GORISCV64 CGO_ENABLED GOMAXPROCS
mode=${1:-}
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
    for check in ordinary checked; do
      tags=wago_guardpage
      if [[ "$check" == checked ]]; then
        tags+=,wago_regalloccheck
      fi
      go_test -tags="$tags" ./src/core/runtime ./src/wago \
        -count=1 -parallel=2 -timeout=5m -v \
        -run '^Test(RV64Guard|Guard|ClosedGuarded|MemoryBytesAfterGrowGuardPage|MemoryGrowExported|MemoryGrow|RV64Atomic|RV64SharedGrowth|Threads|InstallLinuxSignalHandlers|Memory64ActiveDataGlobalOffsetPreservesHighBits|Core1ActiveSegmentPreflight)'
    done
    ;;
  public|artifacts)
    # This is the bounded public profile, not a whole-Core conformance claim.
    # Artifact loading is exercised in a genuinely compiler-free executable.
    artifact_dir=$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/wago-rv64-public.XXXXXX")
    trap 'rm -rf -- "$artifact_dir"' EXIT
    unset WAGO_RV64_TABLE_STAGE_ARTIFACT_DIR WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE \
      WAGO_RV64_PORTABLE_FEATURE_DIR WAGO_RV64_PORTABLE_FEATURE_MODE \
      WAGO_RV64_CUSTOM_ARTIFACT_DIR WAGO_RV64_CUSTOM_ARTIFACT_MODE \
      WAGO_RV64_CORE3_REF_ARTIFACT_DIR WAGO_RV64_CORE3_REF_ARTIFACT_MODE \
      WAGO_RV64_CORE3_REF_IS_NULL_ARTIFACT_DIR WAGO_RV64_CORE3_REF_IS_NULL_ARTIFACT_MODE \
      WAGO_RV64_CORE3_CALL_REF_ARTIFACT_DIR WAGO_RV64_CORE3_CALL_REF_ARTIFACT_MODE \
      WAGO_RV64_I31_ARTIFACT_DIR WAGO_RV64_I31_ARTIFACT_MODE \
      WAGO_RV64_I31_OFFICIAL_ARTIFACT_DIR WAGO_RV64_I31_OFFICIAL_ARTIFACT_MODE \
      WAGO_RV64_I31_TABLE_ARTIFACT_DIR WAGO_RV64_I31_TABLE_ARTIFACT_MODE \
      WAGO_RV64_I31_TABLE_GLOBAL_ARTIFACT_DIR WAGO_RV64_I31_TABLE_GLOBAL_ARTIFACT_MODE \
      WAGO_RV64_I31_GLOBAL_ARTIFACT_DIR WAGO_RV64_I31_GLOBAL_ARTIFACT_MODE \
      WAGO_RV64_I31_ANY_GLOBAL_ARTIFACT_DIR WAGO_RV64_I31_ANY_GLOBAL_ARTIFACT_MODE \
      WAGO_RV64_I31_ANY_TABLE_ARTIFACT_DIR WAGO_RV64_I31_ANY_TABLE_ARTIFACT_MODE \
      WAGO_RV64_I31_REF_TEST_ARTIFACT_DIR WAGO_RV64_I31_REF_TEST_ARTIFACT_MODE \
      WAGO_RV64_I31_REF_EQ_ARTIFACT_DIR WAGO_RV64_I31_REF_EQ_ARTIFACT_MODE \
      WAGO_RV64_GC_REF_EQ_ARTIFACT_DIR WAGO_RV64_GC_REF_EQ_ARTIFACT_MODE \
      WAGO_RV64_GC_REF_TEST_TABLE_ARTIFACT_DIR WAGO_RV64_GC_REF_TEST_TABLE_ARTIFACT_MODE \
      WAGO_RV64_GC_REF_TEST_CONCRETE_ARTIFACT_DIR WAGO_RV64_GC_REF_TEST_CONCRETE_ARTIFACT_MODE \
      WAGO_RV64_GC_REF_TEST_ABSTRACT_ARTIFACT_DIR WAGO_RV64_GC_REF_TEST_ABSTRACT_ARTIFACT_MODE \
      WAGO_RV64_GC_EXTERN_ARTIFACT_DIR WAGO_RV64_GC_EXTERN_ARTIFACT_MODE \
      WAGO_RV64_GC_REF_CAST_ABSTRACT_ARTIFACT_DIR WAGO_RV64_GC_REF_CAST_ABSTRACT_ARTIFACT_MODE \
      WAGO_RV64_GC_REF_CAST_CONCRETE_ARTIFACT_DIR WAGO_RV64_GC_REF_CAST_CONCRETE_ARTIFACT_MODE \
      WAGO_RV64_GC_BR_ON_CAST_ABSTRACT_ARTIFACT_DIR WAGO_RV64_GC_BR_ON_CAST_ABSTRACT_ARTIFACT_MODE \
      WAGO_RV64_GC_BR_ON_CAST_CONCRETE_ARTIFACT_DIR WAGO_RV64_GC_BR_ON_CAST_CONCRETE_ARTIFACT_MODE \
      WAGO_RV64_GC_BR_ON_CAST_NULLABILITY_ARTIFACT_DIR WAGO_RV64_GC_BR_ON_CAST_NULLABILITY_ARTIFACT_MODE \
      WAGO_RV64_GC_TYPE_SUBTYPING_DECLARATIONS_ARTIFACT_DIR WAGO_RV64_GC_TYPE_SUBTYPING_DECLARATIONS_ARTIFACT_MODE \
      WAGO_RV64_GC_TYPE_SUBTYPING_RECURSIVE_FUNCTIONS_ARTIFACT_DIR WAGO_RV64_GC_TYPE_SUBTYPING_RECURSIVE_FUNCTIONS_ARTIFACT_MODE \
      WAGO_RV64_GC_TYPE_SUBTYPING_REF_FUNC_GLOBALS_ARTIFACT_DIR WAGO_RV64_GC_TYPE_SUBTYPING_REF_FUNC_GLOBALS_ARTIFACT_MODE \
      WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_CALL_CAST_ARTIFACT_DIR WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_CALL_CAST_ARTIFACT_MODE \
      WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_FINALITY_CALL_CAST_ARTIFACT_DIR WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_FINALITY_CALL_CAST_ARTIFACT_MODE \
      WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_TYPED_TABLE_CALL_ARTIFACT_DIR WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_TYPED_TABLE_CALL_ARTIFACT_MODE \
      WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_SINGLE_ARTIFACT_DIR WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_SINGLE_ARTIFACT_MODE \
      WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_RECURSIVE_ARTIFACT_DIR WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_RECURSIVE_ARTIFACT_MODE \
      WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_DIRECTION_FALSE_ARTIFACT_DIR WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_DIRECTION_FALSE_ARTIFACT_MODE \
      WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_SUPERTYPE_TRUE_ARTIFACT_DIR WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_SUPERTYPE_TRUE_ARTIFACT_MODE \
      WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_FIRST_ARTIFACT_DIR WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_FIRST_ARTIFACT_MODE \
      WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_PAIRS_ARTIFACT_DIR WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_PAIRS_ARTIFACT_MODE \
      WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_ARTIFACT_DIR WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_ARTIFACT_MODE \
      WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_NOMINAL_FALSE_ARTIFACT_DIR WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_NOMINAL_FALSE_ARTIFACT_MODE \
      WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_NOMINAL_FALSE_DEEP_ARTIFACT_DIR WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_NOMINAL_FALSE_DEEP_ARTIFACT_MODE \
      WAGO_RV64_GC_TYPE_SUBTYPING_LINK_ARTIFACT_DIR WAGO_RV64_GC_TYPE_SUBTYPING_LINK_ARTIFACT_MODE \
      WAGO_RV64_GC_TYPE_SUBTYPING_FINALITY_LINK_ARTIFACT_DIR WAGO_RV64_GC_TYPE_SUBTYPING_FINALITY_LINK_ARTIFACT_MODE \
      WAGO_RV64_GC_TYPE_SUBTYPING_STRUCT_LINK_ARTIFACT_DIR WAGO_RV64_GC_TYPE_SUBTYPING_STRUCT_LINK_ARTIFACT_MODE \
      WAGO_RV64_GC_TYPE_SUBTYPING_STRUCT_PROJECTION_LINK_ARTIFACT_DIR WAGO_RV64_GC_TYPE_SUBTYPING_STRUCT_PROJECTION_LINK_ARTIFACT_MODE \
      WAGO_RV64_GC_TYPE_SUBTYPING_STRUCT_MISMATCH_LINK_ARTIFACT_DIR WAGO_RV64_GC_TYPE_SUBTYPING_STRUCT_MISMATCH_LINK_ARTIFACT_MODE \
      WAGO_RV64_GC_TYPE_SUBTYPING_INDEPENDENT_STRUCT_LINK_ARTIFACT_DIR WAGO_RV64_GC_TYPE_SUBTYPING_INDEPENDENT_STRUCT_LINK_ARTIFACT_MODE \
      WAGO_RV64_GC_TYPE_SUBTYPING_EXTENDED_PROJECTION_LINK_ARTIFACT_DIR WAGO_RV64_GC_TYPE_SUBTYPING_EXTENDED_PROJECTION_LINK_ARTIFACT_MODE \
      WAGO_RV64_GC_TYPE_SUBTYPING_DUPLICATE_RECURSIVE_LINK_ARTIFACT_DIR WAGO_RV64_GC_TYPE_SUBTYPING_DUPLICATE_RECURSIVE_LINK_ARTIFACT_MODE \
      WAGO_RV64_GC_TYPE_SUBTYPING_EXTENDED_RECURSIVE_LINK_ARTIFACT_DIR WAGO_RV64_GC_TYPE_SUBTYPING_EXTENDED_RECURSIVE_LINK_ARTIFACT_MODE \
      WAGO_RV64_GC_TYPE_SUBTYPING_CROSS_GROUP_MISMATCH_LINK_ARTIFACT_DIR WAGO_RV64_GC_TYPE_SUBTYPING_CROSS_GROUP_MISMATCH_LINK_ARTIFACT_MODE \
      WAGO_RV64_GC_TYPE_SUBTYPING_TRANSITIVE_MISMATCH_LINK_ARTIFACT_DIR WAGO_RV64_GC_TYPE_SUBTYPING_TRANSITIVE_MISMATCH_LINK_ARTIFACT_MODE \
      WAGO_RV64_GC_TYPE_SUBTYPING_NON_FLAT_EXPORT_ARTIFACT_DIR WAGO_RV64_GC_TYPE_SUBTYPING_NON_FLAT_EXPORT_ARTIFACT_MODE \
      WAGO_RV64_GC_STRUCT_GET_ARTIFACT_DIR WAGO_RV64_GC_STRUCT_GET_ARTIFACT_MODE \
      WAGO_RV64_GC_STRUCT_MUTATION_ARTIFACT_DIR WAGO_RV64_GC_STRUCT_MUTATION_ARTIFACT_MODE \
      WAGO_RV64_GC_STRUCT_NULL_ARTIFACT_DIR WAGO_RV64_GC_STRUCT_NULL_ARTIFACT_MODE \
      WAGO_RV64_GC_STRUCT_PACKED_ARTIFACT_DIR WAGO_RV64_GC_STRUCT_PACKED_ARTIFACT_MODE \
      WAGO_RV64_GC_STRUCT_BASIC_ARTIFACT_DIR WAGO_RV64_GC_STRUCT_BASIC_ARTIFACT_MODE \
      WAGO_RV64_GC_STRUCT_NUMERIC_GLOBALS_ARTIFACT_DIR WAGO_RV64_GC_STRUCT_NUMERIC_GLOBALS_ARTIFACT_MODE \
      WAGO_RV64_GC_ARRAY_LOCAL_ARTIFACT_DIR WAGO_RV64_GC_ARRAY_LOCAL_ARTIFACT_MODE \
      WAGO_RV64_GC_ARRAY_UNIFORM_ARTIFACT_DIR WAGO_RV64_GC_ARRAY_UNIFORM_ARTIFACT_MODE \
      WAGO_RV64_GC_ARRAY_FIXED_LOCAL_ARTIFACT_DIR WAGO_RV64_GC_ARRAY_FIXED_LOCAL_ARTIFACT_MODE \
      WAGO_RV64_GC_ARRAY_FIXED_OFFICIAL_ARTIFACT_DIR WAGO_RV64_GC_ARRAY_FIXED_OFFICIAL_ARTIFACT_MODE \
      WAGO_RV64_GC_ARRAY_DEFAULT_OFFICIAL_ARTIFACT_DIR WAGO_RV64_GC_ARRAY_DEFAULT_OFFICIAL_ARTIFACT_MODE \
      WAGO_RV64_GC_ARRAY_PACKED_DATA_ARTIFACT_DIR WAGO_RV64_GC_ARRAY_PACKED_DATA_ARTIFACT_MODE \
      WAGO_RV64_GC_ARRAY_REFERENCE_ARTIFACT_DIR WAGO_RV64_GC_ARRAY_REFERENCE_ARTIFACT_MODE \
      WAGO_RV64_GC_ARRAY_INIT_ELEM_ARTIFACT_DIR WAGO_RV64_GC_ARRAY_INIT_ELEM_ARTIFACT_MODE \
      WAGO_RV64_GC_ARRAY_INIT_DATA_ARTIFACT_DIR WAGO_RV64_GC_ARRAY_INIT_DATA_ARTIFACT_MODE \
      WAGO_RV64_GC_ARRAY_INIT_DATA_WIDE_ARTIFACT_DIR WAGO_RV64_GC_ARRAY_INIT_DATA_WIDE_ARTIFACT_MODE \
      WAGO_RV64_GC_ARRAY_FILL_ARTIFACT_DIR WAGO_RV64_GC_ARRAY_FILL_ARTIFACT_MODE \
      WAGO_RV64_GC_ARRAY_COPY_ARTIFACT_DIR WAGO_RV64_GC_ARRAY_COPY_ARTIFACT_MODE
    for check in ordinary checked; do
      tags=""
      if [[ "$check" == checked ]]; then
        tags=wago_regalloccheck
      fi
      mkdir -p "$artifact_dir/$check/portable-features" \
        "$artifact_dir/$check/custom-wide" "$artifact_dir/$check/events"
      if [[ $mode == public ]]; then
      go_test -tags="$tags" ./src/wago -count=1 -parallel=2 -timeout=15m -v \
        -run '^Test(RetainedResourceCleanup|ReferenceEntryCleanup|PhysicalFinalizerChain|ResourceCleanup|ImportDedup|FunctionImportDrain|BuilderRollbackContinues|Dependency|WideArithmetic|Threads|RV64)'
      fi
      WAGO_RV64_PORTABLE_FEATURE_DIR="$artifact_dir/$check/portable-features" \
        WAGO_RV64_PORTABLE_FEATURE_MODE=prepare \
        WAGO_RV64_CUSTOM_ARTIFACT_DIR="$artifact_dir/$check/custom-wide" \
        WAGO_RV64_CUSTOM_ARTIFACT_MODE=prepare \
        WAGO_RV64_TABLE_STAGE_ARTIFACT_DIR="$artifact_dir/$check/events" \
        WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE=prepare \
        go_test -tags="$tags" ./src/wago -count=1 -parallel=2 -timeout=5m -v \
          -run '^TestRV64(PortableFeatureArtifacts|CustomWideDataSourceArtifact|HostEventCompilerFreeArtifact|HostEventCancelCloseAndReentry)$'
      WAGO_RV64_PORTABLE_FEATURE_DIR="$artifact_dir/$check/portable-features" \
        WAGO_RV64_PORTABLE_FEATURE_MODE=load \
        WAGO_RV64_CUSTOM_ARTIFACT_DIR="$artifact_dir/$check/custom-wide" \
        WAGO_RV64_CUSTOM_ARTIFACT_MODE=load \
        WAGO_RV64_TABLE_STAGE_ARTIFACT_DIR="$artifact_dir/$check/events" \
        WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE=load \
        go_test -tags="$tags,wago_precompiled" ./src/wago -count=1 -parallel=2 -timeout=5m -v \
          -run '^TestRV64(PortableFeatureArtifacts|CustomWideDataSourceArtifact|HostEventCompilerFreeArtifact|HostEventCancelCloseAndReentry)$'
      WAGO_RV64_CORE3_REF_ARTIFACT_DIR="$artifact_dir/$check/core3-ref" \
        WAGO_RV64_CORE3_REF_ARTIFACT_MODE=prepare \
        WAGO_RV64_CORE3_REF_IS_NULL_ARTIFACT_DIR="$artifact_dir/$check/core3-ref-is-null" \
        WAGO_RV64_CORE3_REF_IS_NULL_ARTIFACT_MODE=prepare \
        WAGO_RV64_CORE3_CALL_REF_ARTIFACT_DIR="$artifact_dir/$check/core3-call-ref" \
        WAGO_RV64_CORE3_CALL_REF_ARTIFACT_MODE=prepare \
        WAGO_RV64_TABLE_STAGE_ARTIFACT_DIR="$artifact_dir/$check" \
        WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE=prepare \
        WAGO_RV64_I31_ARTIFACT_DIR="$artifact_dir/$check/i31" \
        WAGO_RV64_I31_ARTIFACT_MODE=prepare \
        WAGO_RV64_I31_OFFICIAL_ARTIFACT_DIR="$artifact_dir/$check/i31-official" \
        WAGO_RV64_I31_OFFICIAL_ARTIFACT_MODE=prepare \
        WAGO_RV64_I31_TABLE_ARTIFACT_DIR="$artifact_dir/$check/i31-table" \
        WAGO_RV64_I31_TABLE_ARTIFACT_MODE=prepare \
        WAGO_RV64_I31_TABLE_GLOBAL_ARTIFACT_DIR="$artifact_dir/$check/i31-table-global" \
        WAGO_RV64_I31_TABLE_GLOBAL_ARTIFACT_MODE=prepare \
        WAGO_RV64_I31_GLOBAL_ARTIFACT_DIR="$artifact_dir/$check/i31-global" \
        WAGO_RV64_I31_GLOBAL_ARTIFACT_MODE=prepare \
        WAGO_RV64_I31_ANY_GLOBAL_ARTIFACT_DIR="$artifact_dir/$check/i31-any-global" \
        WAGO_RV64_I31_ANY_GLOBAL_ARTIFACT_MODE=prepare \
        WAGO_RV64_I31_ANY_TABLE_ARTIFACT_DIR="$artifact_dir/$check/i31-any-table" \
        WAGO_RV64_I31_ANY_TABLE_ARTIFACT_MODE=prepare \
        WAGO_RV64_I31_REF_TEST_ARTIFACT_DIR="$artifact_dir/$check/i31-ref-test" \
        WAGO_RV64_I31_REF_TEST_ARTIFACT_MODE=prepare \
        WAGO_RV64_I31_REF_EQ_ARTIFACT_DIR="$artifact_dir/$check/i31-ref-eq" \
        WAGO_RV64_I31_REF_EQ_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_REF_EQ_ARTIFACT_DIR="$artifact_dir/$check/gc-ref-eq" \
        WAGO_RV64_GC_REF_EQ_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_REF_TEST_TABLE_ARTIFACT_DIR="$artifact_dir/$check/gc-ref-test-table" \
        WAGO_RV64_GC_REF_TEST_TABLE_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_REF_TEST_CONCRETE_ARTIFACT_DIR="$artifact_dir/$check/gc-ref-test-concrete" \
        WAGO_RV64_GC_REF_TEST_CONCRETE_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_REF_TEST_ABSTRACT_ARTIFACT_DIR="$artifact_dir/$check/gc-ref-test-abstract" \
        WAGO_RV64_GC_REF_TEST_ABSTRACT_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_EXTERN_ARTIFACT_DIR="$artifact_dir/$check/gc-extern" \
        WAGO_RV64_GC_EXTERN_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_REF_CAST_ABSTRACT_ARTIFACT_DIR="$artifact_dir/$check/gc-ref-cast-abstract" \
        WAGO_RV64_GC_REF_CAST_ABSTRACT_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_REF_CAST_CONCRETE_ARTIFACT_DIR="$artifact_dir/$check/gc-ref-cast-concrete" \
        WAGO_RV64_GC_REF_CAST_CONCRETE_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_BR_ON_CAST_ABSTRACT_ARTIFACT_DIR="$artifact_dir/$check/gc-br-on-cast-abstract" \
        WAGO_RV64_GC_BR_ON_CAST_ABSTRACT_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_BR_ON_CAST_CONCRETE_ARTIFACT_DIR="$artifact_dir/$check/gc-br-on-cast-concrete" \
        WAGO_RV64_GC_BR_ON_CAST_CONCRETE_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_BR_ON_CAST_NULLABILITY_ARTIFACT_DIR="$artifact_dir/$check/gc-br-on-cast-nullability" \
        WAGO_RV64_GC_BR_ON_CAST_NULLABILITY_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_TYPE_SUBTYPING_DECLARATIONS_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-declarations" \
        WAGO_RV64_GC_TYPE_SUBTYPING_DECLARATIONS_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_TYPE_SUBTYPING_RECURSIVE_FUNCTIONS_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-recursive-functions" \
        WAGO_RV64_GC_TYPE_SUBTYPING_RECURSIVE_FUNCTIONS_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_FUNC_GLOBALS_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-ref-func-globals" \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_FUNC_GLOBALS_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_CALL_CAST_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-runtime-call-cast" \
        WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_CALL_CAST_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_FINALITY_CALL_CAST_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-runtime-finality-call-cast" \
        WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_FINALITY_CALL_CAST_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_TYPED_TABLE_CALL_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-runtime-typed-table-call" \
        WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_TYPED_TABLE_CALL_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_SINGLE_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-ref-test-single" \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_SINGLE_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_RECURSIVE_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-ref-test-recursive" \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_RECURSIVE_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_DIRECTION_FALSE_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-ref-test-direction-false" \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_DIRECTION_FALSE_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_SUPERTYPE_TRUE_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-ref-test-supertype-true" \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_SUPERTYPE_TRUE_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_FIRST_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-ref-test-multi-first" \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_FIRST_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_PAIRS_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-ref-test-multi-pairs" \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_PAIRS_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-ref-test-multi" \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_NOMINAL_FALSE_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-ref-test-nominal-false" \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_NOMINAL_FALSE_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_NOMINAL_FALSE_DEEP_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-ref-test-nominal-false-deep" \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_NOMINAL_FALSE_DEEP_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_TYPE_SUBTYPING_LINK_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-link" \
        WAGO_RV64_GC_TYPE_SUBTYPING_LINK_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_TYPE_SUBTYPING_FINALITY_LINK_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-finality-link" \
        WAGO_RV64_GC_TYPE_SUBTYPING_FINALITY_LINK_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_TYPE_SUBTYPING_STRUCT_LINK_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-struct-link" \
        WAGO_RV64_GC_TYPE_SUBTYPING_STRUCT_LINK_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_TYPE_SUBTYPING_STRUCT_PROJECTION_LINK_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-struct-projection-link" \
        WAGO_RV64_GC_TYPE_SUBTYPING_STRUCT_PROJECTION_LINK_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_TYPE_SUBTYPING_STRUCT_MISMATCH_LINK_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-struct-mismatch-link" \
        WAGO_RV64_GC_TYPE_SUBTYPING_STRUCT_MISMATCH_LINK_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_TYPE_SUBTYPING_INDEPENDENT_STRUCT_LINK_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-independent-struct-link" \
        WAGO_RV64_GC_TYPE_SUBTYPING_INDEPENDENT_STRUCT_LINK_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_TYPE_SUBTYPING_EXTENDED_PROJECTION_LINK_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-extended-projection-link" \
        WAGO_RV64_GC_TYPE_SUBTYPING_EXTENDED_PROJECTION_LINK_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_TYPE_SUBTYPING_DUPLICATE_RECURSIVE_LINK_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-duplicate-recursive-link" \
        WAGO_RV64_GC_TYPE_SUBTYPING_DUPLICATE_RECURSIVE_LINK_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_TYPE_SUBTYPING_EXTENDED_RECURSIVE_LINK_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-extended-recursive-link" \
        WAGO_RV64_GC_TYPE_SUBTYPING_EXTENDED_RECURSIVE_LINK_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_TYPE_SUBTYPING_CROSS_GROUP_MISMATCH_LINK_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-cross-group-mismatch-link" \
        WAGO_RV64_GC_TYPE_SUBTYPING_CROSS_GROUP_MISMATCH_LINK_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_TYPE_SUBTYPING_TRANSITIVE_MISMATCH_LINK_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-transitive-mismatch-link" \
        WAGO_RV64_GC_TYPE_SUBTYPING_TRANSITIVE_MISMATCH_LINK_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_TYPE_SUBTYPING_NON_FLAT_EXPORT_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-non-flat-export" \
        WAGO_RV64_GC_TYPE_SUBTYPING_NON_FLAT_EXPORT_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_STRUCT_GET_ARTIFACT_DIR="$artifact_dir/$check/gc-struct-get" \
        WAGO_RV64_GC_STRUCT_GET_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_STRUCT_MUTATION_ARTIFACT_DIR="$artifact_dir/$check/gc-struct-mutation" \
        WAGO_RV64_GC_STRUCT_MUTATION_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_STRUCT_NULL_ARTIFACT_DIR="$artifact_dir/$check/gc-struct-null" \
        WAGO_RV64_GC_STRUCT_NULL_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_STRUCT_PACKED_ARTIFACT_DIR="$artifact_dir/$check/gc-struct-packed" \
        WAGO_RV64_GC_STRUCT_PACKED_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_STRUCT_BASIC_ARTIFACT_DIR="$artifact_dir/$check/gc-struct-basic" \
        WAGO_RV64_GC_STRUCT_BASIC_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_STRUCT_NUMERIC_GLOBALS_ARTIFACT_DIR="$artifact_dir/$check/gc-struct-numeric-globals" \
        WAGO_RV64_GC_STRUCT_NUMERIC_GLOBALS_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_ARRAY_LOCAL_ARTIFACT_DIR="$artifact_dir/$check/gc-array-local" \
        WAGO_RV64_GC_ARRAY_LOCAL_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_ARRAY_UNIFORM_ARTIFACT_DIR="$artifact_dir/$check/gc-array-uniform" \
        WAGO_RV64_GC_ARRAY_UNIFORM_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_ARRAY_FIXED_LOCAL_ARTIFACT_DIR="$artifact_dir/$check/gc-array-fixed-local" \
        WAGO_RV64_GC_ARRAY_FIXED_LOCAL_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_ARRAY_FIXED_OFFICIAL_ARTIFACT_DIR="$artifact_dir/$check/gc-array-fixed-official" \
        WAGO_RV64_GC_ARRAY_FIXED_OFFICIAL_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_ARRAY_DEFAULT_OFFICIAL_ARTIFACT_DIR="$artifact_dir/$check/gc-array-default-official" \
        WAGO_RV64_GC_ARRAY_DEFAULT_OFFICIAL_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_ARRAY_PACKED_DATA_ARTIFACT_DIR="$artifact_dir/$check/gc-array-packed-data" \
        WAGO_RV64_GC_ARRAY_PACKED_DATA_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_ARRAY_REFERENCE_ARTIFACT_DIR="$artifact_dir/$check/gc-array-reference" \
        WAGO_RV64_GC_ARRAY_REFERENCE_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_ARRAY_INIT_ELEM_ARTIFACT_DIR="$artifact_dir/$check/gc-array-init-elem" \
        WAGO_RV64_GC_ARRAY_INIT_ELEM_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_ARRAY_INIT_DATA_ARTIFACT_DIR="$artifact_dir/$check/gc-array-init-data" \
        WAGO_RV64_GC_ARRAY_INIT_DATA_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_ARRAY_INIT_DATA_WIDE_ARTIFACT_DIR="$artifact_dir/$check/gc-array-init-data-wide" \
        WAGO_RV64_GC_ARRAY_INIT_DATA_WIDE_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_ARRAY_FILL_ARTIFACT_DIR="$artifact_dir/$check/gc-array-fill" \
        WAGO_RV64_GC_ARRAY_FILL_ARTIFACT_MODE=prepare \
        WAGO_RV64_GC_ARRAY_COPY_ARTIFACT_DIR="$artifact_dir/$check/gc-array-copy" \
        WAGO_RV64_GC_ARRAY_COPY_ARTIFACT_MODE=prepare \
        go_test -tags="$tags" ./src/wago -count=1 -parallel=2 -timeout=5m -v \
		-run '^TestRV64Public(I31(CoreExecutionAndArtifact|OfficialCoreExecutionAndArtifact|OfficialTableExecutionAndArtifact|OfficialTableGlobalInitializerExecutionAndArtifact|OfficialGlobalInitializerExecutionAndArtifact|OfficialAnyGlobalExecutionAndArtifact|OfficialAnyTableExecutionAndArtifact|OfficialRefTestExecutionAndArtifact|RefEqPilotExecutionAndArtifact)|OfficialGCRefEqExecutionAndArtifact|GCRefTest(Table|Concrete|Abstract)ExecutionAndArtifact|GCExternExecutionAndArtifact|GCRefCast(Abstract|Concrete)(ExecutionAndArtifact|AdmissionIsExact)|GCBrOnCast(Abstract|Concrete|Nullability)(ExecutionAndArtifact|AdmissionIsExact)|GCTypeSubtyping(Declarations|RecursiveFunctions|RuntimeCallCast|RuntimeFinalityCallCast|RuntimeTypedTableCall|NonFlatExport|RefTest(Single|Recursive|DirectionFalse|SupertypeTrue|MultiFirst|MultiPairs|Multi|NominalFalse|NominalFalseDeep))(ExecutionAndArtifact|AdmissionIsExact|RequiresPrivateRuntimePermission)|GCTypeSubtypingLink(ExecutionAndArtifact|AdmissionIsExact|MismatchAndPermission|ExpectedUnlinkables)|GCTypeSubtypingFinalityLink(ExpectedUnlinkables|AdmissionIsExact|MismatchAndPermission)|GCTypeSubtypingStruct(Link|ProjectionLink|MismatchLink)(ExecutionAndArtifact|AdmissionIsExact|MismatchAndPermission)|GCTypeSubtypingIndependentStructLink(ExecutionAndArtifact|AdmissionIsExact|MismatchAndPermission)|GCTypeSubtyping(ExtendedProjection|DuplicateRecursive|ExtendedRecursive|CrossGroupMismatch|TransitiveMismatch)Link(ExecutionAndArtifact|AdmissionIsExact|MismatchAndPermission)|GCStruct(Get|Mutation|Null|Packed|Basic|NumericGlobals)ExecutionAndArtifact|GCArray(Local|Uniform|FixedLocal|FixedOfficial|DefaultOfficial|PackedData|Reference|InitElem|InitData(Wide)?|Fill|Copy)ExecutionAndArtifact|GCArrayReferenceCloseDropsOnlyOwnedSegmentRoots|LocalMutableReference|MutableFuncrefState(Import|Admission|Binding)|MutableFuncrefGlobal(Get|Clear|StoreSelf|Copy|Move|BidirectionalCopy|Local(Copy|Publish|Transfer|Export|Observe))(Admission|Binding)?)|^TestRV64GCTypeSubtypingRefFuncGlobals(SourceAndArtifact|RejectHostileRetainedState)$|^TestRV64Core3Ref(PinnedDeclarations|PinnedDeclaration(SourceGates|ArtifactGates|FeatureGates|RequiresPrivateRuntimePermission)|PinnedInvalidModules)$|^TestRV64Core3RefIsNullExecution(CompilerFreeRejectsSource|FeatureGates)?$|^TestRV64Core3CallRef(UnreachableExecutionAndArtifact|UnreachableFeatureGates|PinnedInvalidModules|Main(ExecutionAndArtifact|FeatureAndSourceGates|RetainedAdmission|RequiresPrivateRuntimePermission))$|^TestRV64PublicReferenceImportInstanceOwnedTable(Binding|Lifecycle)$'
      WAGO_RV64_CORE3_REF_ARTIFACT_DIR="$artifact_dir/$check/core3-ref" \
        WAGO_RV64_CORE3_REF_ARTIFACT_MODE=load \
        WAGO_RV64_CORE3_REF_IS_NULL_ARTIFACT_DIR="$artifact_dir/$check/core3-ref-is-null" \
        WAGO_RV64_CORE3_REF_IS_NULL_ARTIFACT_MODE=load \
        WAGO_RV64_CORE3_CALL_REF_ARTIFACT_DIR="$artifact_dir/$check/core3-call-ref" \
        WAGO_RV64_CORE3_CALL_REF_ARTIFACT_MODE=load \
        WAGO_RV64_TABLE_STAGE_ARTIFACT_DIR="$artifact_dir/$check" \
        WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE=load \
        WAGO_RV64_I31_ARTIFACT_DIR="$artifact_dir/$check/i31" \
        WAGO_RV64_I31_ARTIFACT_MODE=load \
        WAGO_RV64_I31_OFFICIAL_ARTIFACT_DIR="$artifact_dir/$check/i31-official" \
        WAGO_RV64_I31_OFFICIAL_ARTIFACT_MODE=load \
        WAGO_RV64_I31_TABLE_ARTIFACT_DIR="$artifact_dir/$check/i31-table" \
        WAGO_RV64_I31_TABLE_ARTIFACT_MODE=load \
        WAGO_RV64_I31_TABLE_GLOBAL_ARTIFACT_DIR="$artifact_dir/$check/i31-table-global" \
        WAGO_RV64_I31_TABLE_GLOBAL_ARTIFACT_MODE=load \
        WAGO_RV64_I31_GLOBAL_ARTIFACT_DIR="$artifact_dir/$check/i31-global" \
        WAGO_RV64_I31_GLOBAL_ARTIFACT_MODE=load \
        WAGO_RV64_I31_ANY_GLOBAL_ARTIFACT_DIR="$artifact_dir/$check/i31-any-global" \
        WAGO_RV64_I31_ANY_GLOBAL_ARTIFACT_MODE=load \
        WAGO_RV64_I31_ANY_TABLE_ARTIFACT_DIR="$artifact_dir/$check/i31-any-table" \
        WAGO_RV64_I31_ANY_TABLE_ARTIFACT_MODE=load \
        WAGO_RV64_I31_REF_TEST_ARTIFACT_DIR="$artifact_dir/$check/i31-ref-test" \
        WAGO_RV64_I31_REF_TEST_ARTIFACT_MODE=load \
        WAGO_RV64_I31_REF_EQ_ARTIFACT_DIR="$artifact_dir/$check/i31-ref-eq" \
        WAGO_RV64_I31_REF_EQ_ARTIFACT_MODE=load \
        WAGO_RV64_GC_REF_EQ_ARTIFACT_DIR="$artifact_dir/$check/gc-ref-eq" \
        WAGO_RV64_GC_REF_EQ_ARTIFACT_MODE=load \
        WAGO_RV64_GC_REF_TEST_TABLE_ARTIFACT_DIR="$artifact_dir/$check/gc-ref-test-table" \
        WAGO_RV64_GC_REF_TEST_TABLE_ARTIFACT_MODE=load \
        WAGO_RV64_GC_REF_TEST_CONCRETE_ARTIFACT_DIR="$artifact_dir/$check/gc-ref-test-concrete" \
        WAGO_RV64_GC_REF_TEST_CONCRETE_ARTIFACT_MODE=load \
        WAGO_RV64_GC_REF_TEST_ABSTRACT_ARTIFACT_DIR="$artifact_dir/$check/gc-ref-test-abstract" \
        WAGO_RV64_GC_REF_TEST_ABSTRACT_ARTIFACT_MODE=load \
        WAGO_RV64_GC_EXTERN_ARTIFACT_DIR="$artifact_dir/$check/gc-extern" \
        WAGO_RV64_GC_EXTERN_ARTIFACT_MODE=load \
        WAGO_RV64_GC_REF_CAST_ABSTRACT_ARTIFACT_DIR="$artifact_dir/$check/gc-ref-cast-abstract" \
        WAGO_RV64_GC_REF_CAST_ABSTRACT_ARTIFACT_MODE=load \
        WAGO_RV64_GC_REF_CAST_CONCRETE_ARTIFACT_DIR="$artifact_dir/$check/gc-ref-cast-concrete" \
        WAGO_RV64_GC_REF_CAST_CONCRETE_ARTIFACT_MODE=load \
        WAGO_RV64_GC_BR_ON_CAST_ABSTRACT_ARTIFACT_DIR="$artifact_dir/$check/gc-br-on-cast-abstract" \
        WAGO_RV64_GC_BR_ON_CAST_ABSTRACT_ARTIFACT_MODE=load \
        WAGO_RV64_GC_BR_ON_CAST_CONCRETE_ARTIFACT_DIR="$artifact_dir/$check/gc-br-on-cast-concrete" \
        WAGO_RV64_GC_BR_ON_CAST_CONCRETE_ARTIFACT_MODE=load \
        WAGO_RV64_GC_BR_ON_CAST_NULLABILITY_ARTIFACT_DIR="$artifact_dir/$check/gc-br-on-cast-nullability" \
        WAGO_RV64_GC_BR_ON_CAST_NULLABILITY_ARTIFACT_MODE=load \
        WAGO_RV64_GC_TYPE_SUBTYPING_DECLARATIONS_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-declarations" \
        WAGO_RV64_GC_TYPE_SUBTYPING_DECLARATIONS_ARTIFACT_MODE=load \
        WAGO_RV64_GC_TYPE_SUBTYPING_RECURSIVE_FUNCTIONS_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-recursive-functions" \
        WAGO_RV64_GC_TYPE_SUBTYPING_RECURSIVE_FUNCTIONS_ARTIFACT_MODE=load \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_FUNC_GLOBALS_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-ref-func-globals" \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_FUNC_GLOBALS_ARTIFACT_MODE=load \
        WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_CALL_CAST_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-runtime-call-cast" \
        WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_CALL_CAST_ARTIFACT_MODE=load \
        WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_FINALITY_CALL_CAST_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-runtime-finality-call-cast" \
        WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_FINALITY_CALL_CAST_ARTIFACT_MODE=load \
        WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_TYPED_TABLE_CALL_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-runtime-typed-table-call" \
        WAGO_RV64_GC_TYPE_SUBTYPING_RUNTIME_TYPED_TABLE_CALL_ARTIFACT_MODE=load \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_SINGLE_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-ref-test-single" \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_SINGLE_ARTIFACT_MODE=load \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_RECURSIVE_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-ref-test-recursive" \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_RECURSIVE_ARTIFACT_MODE=load \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_DIRECTION_FALSE_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-ref-test-direction-false" \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_DIRECTION_FALSE_ARTIFACT_MODE=load \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_SUPERTYPE_TRUE_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-ref-test-supertype-true" \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_SUPERTYPE_TRUE_ARTIFACT_MODE=load \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_FIRST_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-ref-test-multi-first" \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_FIRST_ARTIFACT_MODE=load \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_PAIRS_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-ref-test-multi-pairs" \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_PAIRS_ARTIFACT_MODE=load \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-ref-test-multi" \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_MULTI_ARTIFACT_MODE=load \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_NOMINAL_FALSE_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-ref-test-nominal-false" \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_NOMINAL_FALSE_ARTIFACT_MODE=load \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_NOMINAL_FALSE_DEEP_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-ref-test-nominal-false-deep" \
        WAGO_RV64_GC_TYPE_SUBTYPING_REF_TEST_NOMINAL_FALSE_DEEP_ARTIFACT_MODE=load \
        WAGO_RV64_GC_TYPE_SUBTYPING_LINK_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-link" \
        WAGO_RV64_GC_TYPE_SUBTYPING_LINK_ARTIFACT_MODE=load \
        WAGO_RV64_GC_TYPE_SUBTYPING_FINALITY_LINK_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-finality-link" \
        WAGO_RV64_GC_TYPE_SUBTYPING_FINALITY_LINK_ARTIFACT_MODE=load \
        WAGO_RV64_GC_TYPE_SUBTYPING_STRUCT_LINK_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-struct-link" \
        WAGO_RV64_GC_TYPE_SUBTYPING_STRUCT_LINK_ARTIFACT_MODE=load \
        WAGO_RV64_GC_TYPE_SUBTYPING_STRUCT_PROJECTION_LINK_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-struct-projection-link" \
        WAGO_RV64_GC_TYPE_SUBTYPING_STRUCT_PROJECTION_LINK_ARTIFACT_MODE=load \
        WAGO_RV64_GC_TYPE_SUBTYPING_STRUCT_MISMATCH_LINK_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-struct-mismatch-link" \
        WAGO_RV64_GC_TYPE_SUBTYPING_STRUCT_MISMATCH_LINK_ARTIFACT_MODE=load \
        WAGO_RV64_GC_TYPE_SUBTYPING_INDEPENDENT_STRUCT_LINK_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-independent-struct-link" \
        WAGO_RV64_GC_TYPE_SUBTYPING_INDEPENDENT_STRUCT_LINK_ARTIFACT_MODE=load \
        WAGO_RV64_GC_TYPE_SUBTYPING_EXTENDED_PROJECTION_LINK_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-extended-projection-link" \
        WAGO_RV64_GC_TYPE_SUBTYPING_EXTENDED_PROJECTION_LINK_ARTIFACT_MODE=load \
        WAGO_RV64_GC_TYPE_SUBTYPING_DUPLICATE_RECURSIVE_LINK_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-duplicate-recursive-link" \
        WAGO_RV64_GC_TYPE_SUBTYPING_DUPLICATE_RECURSIVE_LINK_ARTIFACT_MODE=load \
        WAGO_RV64_GC_TYPE_SUBTYPING_EXTENDED_RECURSIVE_LINK_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-extended-recursive-link" \
        WAGO_RV64_GC_TYPE_SUBTYPING_EXTENDED_RECURSIVE_LINK_ARTIFACT_MODE=load \
        WAGO_RV64_GC_TYPE_SUBTYPING_CROSS_GROUP_MISMATCH_LINK_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-cross-group-mismatch-link" \
        WAGO_RV64_GC_TYPE_SUBTYPING_CROSS_GROUP_MISMATCH_LINK_ARTIFACT_MODE=load \
        WAGO_RV64_GC_TYPE_SUBTYPING_TRANSITIVE_MISMATCH_LINK_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-transitive-mismatch-link" \
        WAGO_RV64_GC_TYPE_SUBTYPING_TRANSITIVE_MISMATCH_LINK_ARTIFACT_MODE=load \
        WAGO_RV64_GC_TYPE_SUBTYPING_NON_FLAT_EXPORT_ARTIFACT_DIR="$artifact_dir/$check/gc-type-subtyping-non-flat-export" \
        WAGO_RV64_GC_TYPE_SUBTYPING_NON_FLAT_EXPORT_ARTIFACT_MODE=load \
        WAGO_RV64_GC_STRUCT_GET_ARTIFACT_DIR="$artifact_dir/$check/gc-struct-get" \
        WAGO_RV64_GC_STRUCT_GET_ARTIFACT_MODE=load \
        WAGO_RV64_GC_STRUCT_MUTATION_ARTIFACT_DIR="$artifact_dir/$check/gc-struct-mutation" \
        WAGO_RV64_GC_STRUCT_MUTATION_ARTIFACT_MODE=load \
        WAGO_RV64_GC_STRUCT_NULL_ARTIFACT_DIR="$artifact_dir/$check/gc-struct-null" \
        WAGO_RV64_GC_STRUCT_NULL_ARTIFACT_MODE=load \
        WAGO_RV64_GC_STRUCT_PACKED_ARTIFACT_DIR="$artifact_dir/$check/gc-struct-packed" \
        WAGO_RV64_GC_STRUCT_PACKED_ARTIFACT_MODE=load \
        WAGO_RV64_GC_STRUCT_BASIC_ARTIFACT_DIR="$artifact_dir/$check/gc-struct-basic" \
        WAGO_RV64_GC_STRUCT_BASIC_ARTIFACT_MODE=load \
        WAGO_RV64_GC_STRUCT_NUMERIC_GLOBALS_ARTIFACT_DIR="$artifact_dir/$check/gc-struct-numeric-globals" \
        WAGO_RV64_GC_STRUCT_NUMERIC_GLOBALS_ARTIFACT_MODE=load \
        WAGO_RV64_GC_ARRAY_LOCAL_ARTIFACT_DIR="$artifact_dir/$check/gc-array-local" \
        WAGO_RV64_GC_ARRAY_LOCAL_ARTIFACT_MODE=load \
        WAGO_RV64_GC_ARRAY_UNIFORM_ARTIFACT_DIR="$artifact_dir/$check/gc-array-uniform" \
        WAGO_RV64_GC_ARRAY_UNIFORM_ARTIFACT_MODE=load \
        WAGO_RV64_GC_ARRAY_FIXED_LOCAL_ARTIFACT_DIR="$artifact_dir/$check/gc-array-fixed-local" \
        WAGO_RV64_GC_ARRAY_FIXED_LOCAL_ARTIFACT_MODE=load \
        WAGO_RV64_GC_ARRAY_FIXED_OFFICIAL_ARTIFACT_DIR="$artifact_dir/$check/gc-array-fixed-official" \
        WAGO_RV64_GC_ARRAY_FIXED_OFFICIAL_ARTIFACT_MODE=load \
        WAGO_RV64_GC_ARRAY_DEFAULT_OFFICIAL_ARTIFACT_DIR="$artifact_dir/$check/gc-array-default-official" \
        WAGO_RV64_GC_ARRAY_DEFAULT_OFFICIAL_ARTIFACT_MODE=load \
        WAGO_RV64_GC_ARRAY_PACKED_DATA_ARTIFACT_DIR="$artifact_dir/$check/gc-array-packed-data" \
        WAGO_RV64_GC_ARRAY_PACKED_DATA_ARTIFACT_MODE=load \
        WAGO_RV64_GC_ARRAY_REFERENCE_ARTIFACT_DIR="$artifact_dir/$check/gc-array-reference" \
        WAGO_RV64_GC_ARRAY_REFERENCE_ARTIFACT_MODE=load \
        WAGO_RV64_GC_ARRAY_INIT_ELEM_ARTIFACT_DIR="$artifact_dir/$check/gc-array-init-elem" \
        WAGO_RV64_GC_ARRAY_INIT_ELEM_ARTIFACT_MODE=load \
        WAGO_RV64_GC_ARRAY_INIT_DATA_ARTIFACT_DIR="$artifact_dir/$check/gc-array-init-data" \
        WAGO_RV64_GC_ARRAY_INIT_DATA_ARTIFACT_MODE=load \
        WAGO_RV64_GC_ARRAY_INIT_DATA_WIDE_ARTIFACT_DIR="$artifact_dir/$check/gc-array-init-data-wide" \
        WAGO_RV64_GC_ARRAY_INIT_DATA_WIDE_ARTIFACT_MODE=load \
        WAGO_RV64_GC_ARRAY_FILL_ARTIFACT_DIR="$artifact_dir/$check/gc-array-fill" \
        WAGO_RV64_GC_ARRAY_FILL_ARTIFACT_MODE=load \
        WAGO_RV64_GC_ARRAY_COPY_ARTIFACT_DIR="$artifact_dir/$check/gc-array-copy" \
        WAGO_RV64_GC_ARRAY_COPY_ARTIFACT_MODE=load \
        go_test -tags="$tags,wago_precompiled" ./src/wago -count=1 -parallel=2 -timeout=5m -v \
		-run '^Test(RV64Public(I31(CoreExecutionAndArtifact|OfficialCoreExecutionAndArtifact|OfficialTableExecutionAndArtifact|OfficialTableGlobalInitializerExecutionAndArtifact|OfficialGlobalInitializerExecutionAndArtifact|OfficialAnyGlobalExecutionAndArtifact|OfficialAnyTableExecutionAndArtifact|OfficialRefTestExecutionAndArtifact|RefEqPilotExecutionAndArtifact)|OfficialGCRefEq(ExecutionAndArtifact|CompilerFreeRejectsSource)|GCRefTest(Table|Concrete|Abstract)(ExecutionAndArtifact|CompilerFreeRejectsSource)|GCExtern(ExecutionAndArtifact|CompilerFreeRejectsSource)|GCRefCast(Abstract|Concrete)(ExecutionAndArtifact|CompilerFreeRejectsSource)|GCBrOnCast(Abstract|Concrete|Nullability)(ExecutionAndArtifact|CompilerFreeRejectsSource)|GCTypeSubtyping(Declarations|RecursiveFunctions|RuntimeCallCast|RuntimeFinalityCallCast|RuntimeTypedTableCall|NonFlatExport|RefTest(Single|Recursive|DirectionFalse|SupertypeTrue|MultiFirst|MultiPairs|Multi|NominalFalse|NominalFalseDeep))(ExecutionAndArtifact|CompilerFreeRejectsSource)|GCTypeSubtypingLink(ExecutionAndArtifact|ExpectedUnlinkables|CompilerFreeRejectsSource)|GCTypeSubtypingFinalityLink(ExpectedUnlinkables|CompilerFreeRejectsSource)|GCTypeSubtypingStruct(Link|ProjectionLink|MismatchLink)(ExecutionAndArtifact|CompilerFreeRejectsSource)|GCTypeSubtypingIndependentStructLink(ExecutionAndArtifact|CompilerFreeRejectsSource)|GCTypeSubtyping(ExtendedProjection|DuplicateRecursive|ExtendedRecursive|CrossGroupMismatch|TransitiveMismatch)Link(ExecutionAndArtifact|CompilerFreeRejectsSource)|GCStruct(Get|Mutation|Null|Packed|Basic|NumericGlobals)(ExecutionAndArtifact|CompilerFreeRejectsSource)|GCArray(Local|Uniform|FixedLocal|FixedOfficial|DefaultOfficial|PackedData|Reference|InitElem|InitData(Wide)?|Fill|Copy)(ExecutionAndArtifact|CompilerFreeRejectsSource)|GCArrayReferenceCloseDropsOnlyOwnedSegmentRoots|LocalMutableReference|MutableFuncrefState(Import|Admission|Binding)|MutableFuncrefGlobal(Get|Clear|StoreSelf|Copy|Move|BidirectionalCopy|Local(Copy|Publish|Transfer|Export|Observe))(Admission|Binding)?)|RV64ActualImportedTablePrecompiledCompilerGate|RV64GCTypeSubtypingRefFuncGlobals(SourceAndArtifact|CompilerFreeRejectsSource)|RV64Core3Ref(PinnedDeclarations|PinnedDeclarationRequiresPrivateRuntimePermission|PinnedInvalidModules|DeclarationCompilerFreeGate)|RV64Core3RefIsNullExecution(CompilerFreeRejectsSource|FeatureGates)?|RV64Core3CallRef(UnreachableExecutionAndArtifact|PinnedInvalidModules|UnreachableCompilerFreeRejectsSource|Main(ExecutionAndArtifact|CompilerFreeRejectsSource|RequiresPrivateRuntimePermission))|RV64PublicReferenceImportInstanceOwnedTable(Binding|Lifecycle))$'
    done
    ;;
  *)
    echo "Usage: scripts/ci-riscv64.sh {smoke|supported|public|artifacts|guards}" >&2
    exit 2
    ;;
esac
