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
    unset WAGO_RV64_TABLE_STAGE_ARTIFACT_DIR WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE \
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
      tags=wago_rv64_experimental
      if [[ "$check" == checked ]]; then
        tags+=,wago_regalloccheck
      fi
      go test -tags="$tags" ./src/wago -count=1 -parallel=2 -timeout=5m -v \
        -run '^Test(RetainedResourceCleanup|ReferenceEntryCleanup|PhysicalFinalizerChain|ResourceCleanup|ImportDedup|FunctionImportDrain|BuilderRollbackContinues|Dependency|RV64.*(Reference|Global|I31|NativeImport|Invocation|Start|Table|Rollback))'
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
        go test -tags="$tags" ./src/wago -count=1 -parallel=2 -timeout=5m -v \
        -run '^TestRV64Public(I31(CoreExecutionAndArtifact|OfficialCoreExecutionAndArtifact|OfficialTableExecutionAndArtifact|OfficialTableGlobalInitializerExecutionAndArtifact|OfficialGlobalInitializerExecutionAndArtifact|OfficialAnyGlobalExecutionAndArtifact|OfficialAnyTableExecutionAndArtifact|OfficialRefTestExecutionAndArtifact|RefEqPilotExecutionAndArtifact)|OfficialGCRefEqExecutionAndArtifact|GCRefTest(Table|Concrete)ExecutionAndArtifact|GCStruct(Get|Mutation|Null|Packed|Basic|NumericGlobals)ExecutionAndArtifact|GCArray(Local|Uniform|FixedLocal|FixedOfficial|DefaultOfficial|PackedData|Reference|InitElem|InitData(Wide)?|Fill|Copy)ExecutionAndArtifact|GCArrayReferenceCloseDropsOnlyOwnedSegmentRoots|LocalMutableReference)'
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
        go test -tags="$tags,wago_precompiled" ./src/wago -count=1 -parallel=2 -timeout=5m -v \
        -run '^Test(RV64Public(I31(CoreExecutionAndArtifact|OfficialCoreExecutionAndArtifact|OfficialTableExecutionAndArtifact|OfficialTableGlobalInitializerExecutionAndArtifact|OfficialGlobalInitializerExecutionAndArtifact|OfficialAnyGlobalExecutionAndArtifact|OfficialAnyTableExecutionAndArtifact|OfficialRefTestExecutionAndArtifact|RefEqPilotExecutionAndArtifact|OfficialCompilerFreeRejectsSource|OfficialTableCompilerFreeRejectsSource|OfficialTableGlobalInitializerCompilerFreeRejectsSource|OfficialGlobalInitializerCompilerFreeRejectsSource|OfficialAnyGlobalCompilerFreeRejectsSource|OfficialAnyTableCompilerFreeRejectsSource|OfficialRefTestCompilerFreeRejectsSource|RefEqPilotCompilerFreeRejectsSource)|OfficialGCRefEq(ExecutionAndArtifact|CompilerFreeRejectsSource)|GCRefTest(Table|Concrete)(ExecutionAndArtifact|CompilerFreeRejectsSource)|GCStruct(Get|Mutation|Null|Packed|Basic|NumericGlobals)(ExecutionAndArtifact|CompilerFreeRejectsSource)|GCArray(Local|Uniform|FixedLocal|FixedOfficial|DefaultOfficial|PackedData|Reference|InitElem|InitData(Wide)?|Fill|Copy)(ExecutionAndArtifact|CompilerFreeRejectsSource)|GCArrayReferenceCloseDropsOnlyOwnedSegmentRoots|LocalMutableReference)|RV64ActualImportedTablePrecompiledCompilerGate)'
    done
    ;;
  *)
    echo "Usage: scripts/ci-riscv64.sh {smoke|supported|public}" >&2
    exit 2
    ;;
esac
