# WebAssembly specification testing

Wago keeps separate official corpora for the WebAssembly 1.0 baseline and the
WebAssembly 2.0 closeout gate. Do not repoint one checkout to cover both releases:
the 1.0 certification counts in `SPECTEST.md` must remain reproducible while 2.0
support is completed.

## Pinned corpora

| Release | Repository and revision | Local path | Core corpus |
|---|---|---|---|
| WebAssembly 1.0 baseline | `WebAssembly/testsuite` at `a8bcbafe6d2fb191ce0188de0e18fdc107fa2598` | `tests/spec` | checkout root |
| WebAssembly 2.0 | `WebAssembly/spec` tag `v2.0.0`, commit `05ca4182176763112561ae20153975c12bd689e4` | `tests/spec-v2` | `test/core` |
| WebAssembly 3.0 | `WebAssembly/spec` tag `wg-3.0`, commit `9d36019973201a19f9c9ebb0f10828b2fe2374aa` | `tests/spec-v3` | `test/core` |

The Release 2 source is the specification repository because that tagged tree
contains the official release's complete core tests, including the nested
`test/core/simd` files. The old `WebAssembly/testsuite` checkout is a preserved
pre-reference-types baseline and its proposal directories are not a substitute
for a tagged Release 2 core corpus.

Initialize only the suite needed for a focused run:

```sh
git submodule update --init tests/spec       # WebAssembly 1.0
git submodule update --init tests/spec-v2    # WebAssembly 2.0
git submodule update --init tests/spec-v3    # WebAssembly 3.0
```

## Commands

Install WABT so `wast2json` is on `PATH`, then run:

```sh
make spec1
make spec2
make spec3
make simd
```

`make spec2` sets `WAGO_SPECTEST_DIR` to the `tests/spec-v2` checkout and
`WAGO_SPEC_VERSION=2.0`; the execution harness resolves the tagged repository's
`test/core` layout. CI provisions WABT and runs the full release suites for the
informational CI card. `make simd` is the required native execution gate for the
focused official SIMD proposal corpus on each supported runtime target; broader
spec-suite gaps remain visible in the card without failing the aggregate CI
check.

`make spec3` verifies checksum-pinned WABT 1.0.41 and the official 3.0.0
reference interpreter built from the exact Release 3 pin. WABT remains primary;
28 unsupported text files fall back to the strict binary-script converter. The
current schema-2 inventory processes all 258 files with zero parser failures:
144 green/114 red, modules pass=1,691/skip=535, assertions
pass=51,765/fail=5/skip=6,268. The five reached failures are two in `linking`, one
in `multi-memory/linking0`, and two in `multi-memory/linking3`; the former
`select` funcref wildcard failure is green. Iteration 10 adds dynamic typed-table
alias/lifecycle proofs, exact codec-v23 indexed-memory metadata, and an internal
linux/amd64 local/imported memory-1 size/i32 load/store slice without enabling
public typed references or multi-memory. The schema-2 inventory therefore remains
byte-for-byte unchanged.
Refresh the machine-readable red inventory with `scripts/spec3-baseline.sh`; the
command remains nonzero until the zero-gap completion gate is met.

The validation harness uses the same release discovery and can be run directly:

```sh
WAGO_SPECTEST_DIR="$PWD/tests/spec-v2" WAGO_SPEC_VERSION=2.0 \
  go test -count=1 -run '^TestSpecSuite$' -v ./src/core/compiler/wasm
```

For a bounded proof of the WebAssembly 2.0 declared-function-reference rule,
run the official `ref_func.wast` sites through both validator paths:

```sh
WAGO_SPECTEST_DIR="$PWD/tests/spec-v2" WAGO_SPEC_VERSION=2.0 \
  go test -count=1 -run '^TestRelease2RefFuncValidationSites$' -v \
  ./src/core/compiler/wasm
```

That focused guard locks the three valid module sites at lines 1, 6, and 80 and
the three invalid sites at lines 69, 109, and 113 in the pinned fixture. The
last two invalid modules distinguish an undeclared `ref.func` from an ordinary
out-of-bounds function index. The corresponding execution root is locked with:

```sh
go test -count=1 \
  -run '^(TestRelease2RefFuncGlobalExecution|TestRelease2RefFuncGlobalInitializersWithoutTable|TestRefFuncGlobalDescriptorArenaIsBoundedAndDemandDriven|TestRefFuncGlobalHostImportEgressFailsClosed)$' \
  ./src/wago
```

The WABT-backed test requires exactly 3 passed modules and 10 passed assertions
for the complete pinned `ref_func.wast`: the registered producer, the imported/
local mutable-global execution module, and the declaration-only module. The
local no-table fixture proves that a structural global initializer and a body
`ref.func` share one stable public identity, that the token remains callable in
a same-runtime consumer, and that the descriptor arena is allocated on demand
without manufacturing a Wasm table. Host-imported `ref.func` global egress must
remain fail-closed and issue no token.

For the multiple-table execution gate, run the focused official and local roots
covering nonzero active destinations, cross-table copy/init, table-0 preservation,
nonzero-table `call_indirect`, exact indexed exports, and multiple imported tables
followed by local definitions:

```sh
go test -count=1 \
  -run '^(TestRelease2MultipleTableCopyExecution|TestRelease2NonzeroTableInitExecution|TestRelease2NonzeroTableExportImportExecution|TestRelease2ImportedThenLocalTableExecution|TestRelease2MultipleImportedThenLocalTableExecution|TestImportedThenLocalFuncrefTablesExecuteAndExportExactly|TestMultipleImportedFuncrefTablesExecuteAndExportExactly|TestMultipleImportedFuncrefTablesMayAliasOneHandle|TestMultipleImportedTablesCheckEveryLimit|TestImportedThenLocalTablesRejectSharedMemoryBasedataAlias|TestImportedThenLocalFailedInstantiationRetainsSharedTableWrites|TestMultipleImportedTablesRetainFailedInstancesAcrossEveryHandle|TestImportedThenLocalTableArenaFootprintIsBounded|TestMultipleImportedTableArenaFootprintIsBounded|TestMultipleLocalTableExportsResolveByName|TestMultipleLocalTableArenaFootprintIsBounded|TestMinOnlyTableExportCapacityIsPerTable)$' \
  ./src/wago
```

The locked sites are `table_copy.wast:751`, `table_init.wast:197`,
`imports.wast:376/386`, and `table.wast:12`. The copy/init replays include the
registered five-function producer and require 2 passed modules plus 61 and 31
passed actions/assertions. The indexed-export replay includes the official two-
table exporter, registers it, and imports its second table by the exact name
`table-10-20`; it requires 2 passed modules with no skips. The `table.wast`
replay requires 2 passed modules and locks the official shape with imported table
0 followed by local table 1.

The imported/local ownership guards prove active writes and indexed calls across
multiple imported and local tables, cross-table copy, exact imported re-export
and local export resolution, duplicate import aliases, independent limit and
policy checks, codec-v22 structural round-trip, shared-memory per-instance context rebinding, finite
alias-safe failed-instance retention, preservation of every producer's unrelated
export-handle chain, and consumer-before-producer close ordering. The footprint
guards require a capacity-one second local funcref table to add exactly 56 arena
bytes (40 descriptor bytes plus a 16-byte two-entry directory), a second imported
table to add only that 16-byte directory, and a capacity-one local table after
two imports to add 48 bytes (40 descriptor plus 8-byte directory growth). A lazy
exported `Table` handle remains 64 bytes, and min-only reserve applies only to the
exported sibling. The complete files are green for `exports.wast` at 56/0/0
modules and 9/0/0 assertions, `table_copy.wast` at 52/0/0 and 1,675/0/0, and
`table_init.wast` at 35/0/0 and 677/0/0. `imports.wast` is now 41/0/13 modules
and 16/0/18 assertions; `table.wast` is 7/0/2 modules. Remaining table gaps are
externref tables and unrelated reference boundaries.

For the Release 2 segment-mode rule, run the seven formerly rejected valid
modules through both validator paths:

```sh
WAGO_SPECTEST_DIR="$PWD/tests/spec-v2" WAGO_SPEC_VERSION=2.0 \
  go test -count=1 -run '^TestRelease2SegmentModeValidationSites$' -v \
  ./src/core/compiler/wasm
```

The locked sites are `bulk.wast` lines 154 and 244, `elem.wast` lines 342 and
352, `memory_init.wast` line 219, and `table_init.wast` lines 407 and 431. They
prove that data/element indexes remain valid after active or declarative
segments become implicitly dropped; table element-type mismatch cases remain
invalid and are covered separately by local validator tests.

For the Release 2 stack-polymorphic `br_table` rule, run the remaining formerly
rejected valid module through both validator paths:

```sh
WAGO_SPECTEST_DIR="$PWD/tests/spec-v2" WAGO_SPEC_VERSION=2.0 \
  go test -count=1 -run '^TestRelease2UnreachedBrTableValidationSite$' -v \
  ./src/core/compiler/wasm
```

The guard locks `unreached-valid.wast` line 49. Its branch payload is bottom
after `unreachable`, so it matches equal-arity `f32` and `f64` labels. Local
validator tests separately prove that a reachable heterogeneous numeric payload
is still rejected and label arity remains strict.

For the Release 2 single-memory cardinality rule, run all five official invalid
sites through both validator paths:

```sh
WAGO_SPECTEST_DIR="$PWD/tests/spec-v2" WAGO_SPEC_VERSION=2.0 \
  go test -count=1 -run '^TestRelease2MultipleMemoryValidationSites$' -v \
  ./src/core/compiler/wasm
```

The locked sites are `imports.wast` lines 483, 487, and 491 plus `memory.wast`
lines 10 and 11. They cover two imported memories, one imported plus one local
memory, and two local memories. Multi-memory is not part of WebAssembly 2.0, so
the default validator continues to count imported and local memories together and
reject totals above one clearly; focused local tests preserve one imported or one
local memory as valid. Core 3 work uses only the explicit
`ValidationFeatures{MultiMemory: true}` path, which accepts valid indexed
`memory.size`/`memory.grow` and memargs while still rejecting unknown indexes.
That staged API is not wired to public compile/runtime admission. The Release 2
guard exercises both AST and byte-backed default validation and keeps its
rejection ahead of frontend support filtering.

For the Release 2 implicit-select operand rule, run the formerly failing
official site through both validator paths together with the local form matrix:

```sh
WAGO_SPECTEST_DIR="$PWD/tests/spec-v2" WAGO_SPEC_VERSION=2.0 \
  go test -count=1 \
  -run '^(TestRelease2ImplicitReferenceSelectValidationSite|TestValidateSelectForms)$' \
  ./src/core/compiler/wasm
```

The guard locks `select.wast` line 340. Opcode `0x1b` (implicit/untyped
`select`) accepts only numeric and vector operands; reference operands require
the typed `0x1c` form. Local tests preserve implicit numeric/vector values,
typed `funcref`/`externref`, all-bottom stack polymorphism, and rejection when a
known reference is paired with bottom.

For the Release 2 malformed data-count rules, run the five formerly accepted
binary sites together with local decoder edge cases:

```sh
WAGO_SPECTEST_DIR="$PWD/tests/spec-v2" WAGO_SPEC_VERSION=2.0 \
  go test -count=1 \
  -run '^(TestDecodeDataCountConsistency|TestDecodeDataInstructionsRequireDataCount|TestRelease2MalformedDataCountSites)$' \
  ./src/core/compiler/wasm
```

The official guard locks `binary.wast` lines 1185, 1195, 1205, and 1227 plus
`custom.wast` line 123 through both `DecodeModule` and
`DecodeModuleByteBacked`. A declared data count must equal the final data-section
length, including the zero/absent-section cases. A code section containing
`memory.init` or `data.drop` requires a data-count section even if validation
would later reject the instruction's index or types. This is a binary
well-formedness obligation: reject it during decode rather than relying on
`ValidateModule`. The byte-backed decoder records the requirement during its
existing instruction walk; do not materialize or rescan function bodies for
this check.

For the Release 2 reserved-zero memory immediate rule, run the ten formerly
accepted `binary.wast` sites together with the local AST/byte-backed matrix:

```sh
WAGO_SPECTEST_DIR="$PWD/tests/spec-v2" WAGO_SPEC_VERSION=2.0 \
  go test -count=1 \
  -run '^(TestDecodeInstructionImmediates|TestDecodeMemoryReservedZeroImmediate|TestRelease2MalformedMemoryReservedZeroSites)$' \
  ./src/core/compiler/wasm
```

The official guard locks `binary.wast` lines 857, 877, 897, 916, 935, 955,
974, 993, 1011, and 1029 through both public decode APIs. In WebAssembly 2.0,
`memory.size` and `memory.grow` do not carry an arbitrary ULEB memory index:
the reserved immediate must be exactly one literal `0x00` byte. Reject nonzero
bytes and non-minimal two- through five-byte LEB zero encodings during decode.
Keep the structured AST decoder, byte-backed instruction walk, and direct
validator decoder aligned, and preserve truncated-immediate offsets plus code-
section spans. The default/WebAssembly 2.0 path must still reject nonzero and
non-minimal encodings. The explicit Core 3 multi-memory validation path decodes a
canonical ULEB memory index instead; do not let that staged path weaken default
binary strictness or imply frontend/runtime execution support.

For the Release 2 memory32 memarg offset-width rule, run the twelve formerly
accepted sites together with the local AST, byte-backed, and direct-validator
matrix:

```sh
WAGO_SPECTEST_DIR="$PWD/tests/spec-v2" WAGO_SPEC_VERSION=2.0 \
  go test -count=1 \
  -run '^(TestDecodeMemoryOffsetWidthFollowsMemoryType|TestValidateByteBackedMemoryOffsetWidth|TestRelease2MalformedMemoryOffsetSites)$' \
  ./src/core/compiler/wasm
```

The official guard locks `binary.wast` lines 483, 540, 620, 639, 733, and 752
plus the duplicate `binary-leb128.wast` lines 405, 462, 731, 750, 844, and 863.
A memarg offset is a u32 LEB for memory32 and a u64 LEB for a sole effective
memory64 definition or import. With no memory or multiple memories, decode uses
the conservative u32 width; validation still reports an absent memory or wago's
strict unsupported multiple-memory shape separately. Preserve valid non-minimal
encodings that fit the selected width, while rejecting a sixth u32 byte or
nonzero unused bits in its fifth byte. Pass the width through the existing
structured, byte-backed, direct-validation, and frontend-supported body walks;
do not add another body scan, materialize instructions, or enlarge the reusable
reader solely to carry this context.

For the Release 2 aggregate local-count rule, run the two formerly accepted
malformed sites together with the local boundary matrix:

```sh
WAGO_SPECTEST_DIR="$PWD/tests/spec-v2" WAGO_SPEC_VERSION=2.0 \
  go test -count=1 \
  -run '^(TestDecodeLocalsRejectsAggregateCountOverflow|TestDecodeLocalsPreservesUint32BoundaryAndZeroRuns|TestRelease2MalformedLocalCountSites)$' \
  ./src/core/compiler/wasm
```

The official guard locks `binary.wast` lines 1082 and 1098 through both public
decode APIs. Each local run count is a valid u32, but the sum of the declared
locals must not exceed 2^32-1. Enforce this while decoding the compact run
vector, before validation, because `assert_malformed` is a binary
well-formedness obligation. Preserve zero-count runs and the exact 2^32-1
boundary without expanding one entry per local or using the aggregate as an
allocation hint. The malformed binary sum covers declared locals only;
function parameters remain part of the later validation local-index-space
check. Keep the AST oracle, byte-backed public APIs, and direct code-section
decoder aligned, including code-section diagnostic spans.

For the Release 2 shared-memory maximum rule, run the remaining formerly
accepted malformed site together with the imported/local and width matrix:

```sh
WAGO_SPECTEST_DIR="$PWD/tests/spec-v2" WAGO_SPEC_VERSION=2.0 \
  go test -count=1 \
  -run '^(TestDecodeRejectsSharedMemoryWithoutMaximum|TestDecodePreservesValidMemoryLimitForms|TestProgrammaticSharedMemoryWithoutMaxValidationRejects|TestRelease2MalformedSharedMemorySite)$' \
  ./src/core/compiler/wasm
```

The official guard locks `binary.wast` line 1563 through both public decode
APIs. Shared memories require an explicit maximum, so reject memory32 flag 2 and
memory64 flag 6 in the common memory-type decoder used by local definitions and
imports. Decode the minimum first so malformed u32/u64 LEB diagnostics retain
priority. Preserve shared-with-maximum flags 3/7, every unshared limits form,
`ErrInvalidLimits`, and import/memory section spans. Programmatically constructed
`MemType{Shared: true, Max: nil}` values remain a validation-time
`ErrInvalidSharedMemory`; binary malformedness must not depend on validation.

For the Release 2 core section-id namespace, run the final formerly accepted
malformed site together with the local AST/public decoder contract:

```sh
WAGO_SPECTEST_DIR="$PWD/tests/spec-v2" WAGO_SPEC_VERSION=2.0 \
  go test -count=1 \
  -run '^(TestDecodeRejectsReservedSectionID14|TestProgrammaticStringRefsAndStringConstValidation|TestRelease2MalformedSectionIDSite)$' \
  ./src/core/compiler/wasm
```

The official guard locks `binary.wast` line 48 through both public decode APIs.
WebAssembly 2.0 core section ids end at 13; id 14 must produce
`ErrInvalidSection` with the original section id and payload span. Wago's former
id-14 stringrefs proposal path never reached execution because the frontend
rejected non-empty `Module.StringRefs`, and no product/API caller opted into a
proposal-specific binary format. The default decoders therefore follow the
Release 2 core namespace rather than silently accepting that extension. Keep
the AST oracle and byte-backed decoder aligned. Programmatically constructed
`Module.StringRefs` plus `string.const` remain validator-covered as a separate
in-memory proposal boundary; restoring binary acceptance requires a deliberate,
explicit proposal/version API rather than changing the default core decoder.

For the first executable externref store and host-ABI slice, run the focused
local/official-source matrix:

```sh
go test -count=1 -run '^TestExternref' ./src/wago
```

The gate pins `ref_null.wast:2`, `ref_is_null.wast:5-6`,
`select.wast:35-36`, and `br_table.wast:1246-1249`. The local fixtures require
externref parameters/results, zero-initialized locals, block and `br_table`
results, typed `select`, `ref.null extern`, `ref.is_null`, reflection-free host
round trips, stable same-store object identity, feature gating, and rejection of
forged, stale, cross-runtime, and cross-private-store tokens before native
execution or host re-entry. The codec proof preserves structural externref types
without store identity. The official focused gates are:

```sh
go test -count=1 \
  -run '^TestRelease2Externref(Select|BrTable)Execution$' ./src/wago
```

They require `select.wast` to pass 2 modules / 118 assertions and
`br_table.wast` to pass 1 module / 149 assertions with no skips. The execution
harness now interns WABT `ref.extern N` values in the target instance's store and
checks result identity by resolving the returned token. Externref arguments no
longer count as gaps; the two remaining `reference-result` gaps are non-null
funcref results. Externref globals and tables remain explicitly outside this
bounded gate. Against red baseline `16a78af5`, the complete Release 2 execution
run improves from 1,542 passed / 58 skipped modules and 47,744 passed / 504
skipped assertions to 1,544 / 56 modules and 48,011 / 237 assertions. At that
slice boundary, gaps were compile-rejected=20, instantiate-rejected=36,
module-unavailable=235, absent-export=0, reference-argument=0, reference-result=2, and
reference-global=0.

For explicit host funcref ownership and the final non-null funcref result sites,
run:

```sh
go test -count=1 \
  -run '^(TestOwnedHostFuncref.*|TestHostFuncref.*|TestFuncrefHostReentryControlFrameIsDemandDriven|TestRelease2ExternrefTableExecution)$' \
  ./src/wago
```

The local matrix requires one exact `Runtime.NewHostFuncRef` signature/store
owner, stable identity across duplicate importing instances, global/table/public
round trips, callable same-store indirect dispatch, source retention after logical
close, importer/token/runtime close ordering, opaque callback parameters/results,
and rejection of raw unowned egress, cross-runtime owners/tokens, forged callback
results, and corrupted `refSlot` metadata. Only modules with a funcref table plus
public funcref ingress install the bounded synchronous control frame; fixed local
table-0 modules remain on the direct path. `table_get.wast` now passes one module
and all ten assertions because WABT's value-less funcref expectation requires any
nonzero opaque token rather than a descriptor/pointer identity.

For the bounded local nullable-funcref-global execution slice, run the focused
local/official-source matrix:

```sh
go test -count=1 \
  -run '^(TestRelease2NullableLocalFuncrefGlobals|TestLocalFuncrefGlobalRoundTripRetainsProducer|TestNullableLocalFuncrefGlobalsRespectFeatureAndOwnershipBoundaries|TestInstantiateRejectsUnsupportedReferenceGlobalMetadata|TestNullableLocalFuncrefGlobalsRemainOutOfSerializedState|TestRelease2NullableFuncrefGlobalSourceGuard)$' \
  ./src/wago
```

The binary fixture isolates the funcref declarations at `linking.wast:97-98`
because externref globals were a separate store-handle slice when this gate was
introduced. It proves local immutable/mutable 8-byte cells,
`ref.null func`, JIT `global.get`/`global.set`, typed null and non-null token
round trips, producer retention after logical close, feature gating, explicit-
owner rejection for unbound imports, forged-token rejection, and `.wago`/snapshot
fail-closed behavior. The source guard pins the exact official declarations.
Structural non-null initializers are covered separately by the complete
`ref_func.wast` execution gate above. Imported/shared ownership is covered by the
combined gate below.

For the bounded module-local externref-global slice, run:

```sh
go test -count=1 \
  -run '^(TestRelease2LocalExternrefGlobalExecution|TestNullableLocalExternrefGlobals|TestLocalExternrefGlobalsRespectFeatureStoreAndLifetimeBoundaries|TestLocalExternrefGlobalsRemainOutOfSerializedState|TestRelease2ExternrefGlobalSourceGuard|TestInvokeActionExecutesExternrefGlobalIdentity)$' \
  ./src/wago
```

The source guard pins `global.wast:20-21,26-27,34,198-199,223,229`,
`ref_null.wast:5`, and `linking.wast:99-100`. The local fixture proves immutable
and mutable null initialization, JIT get/set, same-store non-null identity,
`GlobalValue`/`SetGlobalValue`, feature gating, forged/cross-store rejection before
storage, runtime/private-store teardown, exact 8-byte cells, and `.wago`/snapshot
rejection. Imported/shared reference globals are covered by the combined gate
below. The focused official `global.wast` module passes 1 module / 58 assertions; the
complete `global.wast` and `ref_null.wast` files are green at 5 / 58 and 1 / 2.
The execution harness now routes supported reference-valued `get` actions through
typed global access and resolves externref fixture identity without weakening the
non-null funcref owner gap.

For imported/shared funcref and externref globals, host-created store-bound
externref globals, local exports/re-exports, and imported immutable `global.get`
initializers, run:

```sh
go test -count=1 \
  -run '^(TestStoreBoundExternrefGlobal|TestLocalReferenceGlobal|TestImportedReferenceGlobal|TestReferenceGlobal|TestRelease2ImportedReferenceGlobal)' \
  ./src/wago
```

The local matrix requires exact funcref/externref type and mutability, same-runtime
shared mutation, duplicate-alias deduplication, local export then re-import,
runtime-owned externref `GetValue`/`SetValue`, null/non-null identity, imported
immutable `global.get` copies, cross-runtime/private-store/forged rejection before
storage, producer and consumer close ordering, Runtime.Close root retention, and
bounded 40-byte `Global`, 776-byte `Instance`, 648-byte `Compiled`, and 88-byte
`referenceStore` layouts. Host global close rejects live importers. Codec v22
round-trips structural reference-global metadata; snapshots continue to reject
live reference-global state.

The source guard pins `linking.wast:96-129`, including all four exact compatible
imports and the incompatible type/mutability declarations. The WABT execution
guard replays lines 96-111 and requires two passed modules with no skips; the
execution harness does not yet replay `assert_unlinkable`, so the incompatible
sites remain locked by local exact-type tests. The full execution gate improves by
one module with no assertion-count change.

For host-created funcref globals initialized from an explicitly owned host token,
run:

```sh
go test -count=1 -run '^TestHostCreatedFuncRefGlobal' ./src/wago
```

The local gate pins `Runtime.NewFuncRefGlobal` with null and exact same-store
`FuncRef` initialization, existing typed `Global.GetValue`/`SetValue` accessors,
shared mutation, duplicate-alias deduplication, callable `HostFuncRef` identity,
producer retention, and importer/Runtime/owner/global close ordering. It rejects
forged and cross-runtime tokens and proves raw `HostFunc` descriptor egress stays
fail-closed. Codec v22 persists structural reference-global metadata only while
snapshots still reject live reference state, and
`Global`, `HostFuncRef`, `Compiled`, `Instance`, and `referenceStore` are 40,
120, 648, 776, and 88 bytes. This is a host API/lifetime gate; it does not change
the official Release 2 corpus counts.

For the bounded module-local externref-table slice, run:

```sh
go test -count=1 \
  -run '^(TestRelease2ExternrefTableExecution|TestLocalExternrefTablesExecuteAcrossHeterogeneousIndexes|TestExternrefOnlyTableUsesEightByteEntriesWithoutFuncrefArena|TestExternrefTableStructFootprintsRemainBounded|TestLocalExternrefTablesRespectFeatureStoreAndPersistenceBoundaries|TestRelease2ExternrefTableSourceGuard)$' \
  ./src/wago
```

The source guard pins `ref_is_null.wast:9-26`, `table_get.wast:1-36`,
`table_set.wast:1-49`, `table_size.wast:1-64`,
`table_grow.wast:1-38,53-80`, and `table_fill.wast:1-69`. The local matrix proves
null initialization, same-store non-null identity, exact table indexes and
8-byte strides, bounds traps, zero-length fill/grow behavior, bounded 1,024-entry
min-only growth, feature gating, forged/cross-store rejection before storage,
store teardown, and no funcref descriptor arena for externref-only tables.
Codec v22 round-trips externref-table structure while snapshots remain an
explicit live-table-state rejection. Imported/shared ownership and exact exports/re-exports are covered by
the next gate. The focused official roots lock
`ref_is_null` at 1 module / 13 assertions; `table_set`, `table_size`, and
`table_fill` at 1/18, 1/36, and 1/35; the first and min-only-growth
`table_grow` roots at 1/21 and 2 modules / 5 assertions including the file's
fixture module; and `table_get` at 1 module / 10 assertions, including both
non-null funcref results. The complete `table_grow.wast` file is green at
5 modules / 38 assertions.

For store-bound imported/shared externref tables and local exports/re-exports,
run:

```sh
go test -count=1 \
  -run '^(TestStoreBoundExternrefTable|TestLocalExternrefTableExport|TestImportedExternrefTablePersistence|TestRelease2ImportedExternrefTable)' \
  ./src/wago
```

The local matrix requires `Runtime.NewExternRefTable`, exact externref type and
8-byte stride, same-store aliases, get/set/size/grow/fill visibility, local export
then re-import/re-export, limit checks, cross-runtime and standalone/private-store
rejection, host-table close rejection with live importers, Runtime.Close root
retention through the final table close, codec-v22 structural round-trip plus
snapshot rejection, and the bounded 64-byte `Table`, 776-byte `Instance`, and
648-byte `Compiled` layouts.
The source/execution guard pins `linking.wast:291-309`; its exporter and compatible
importer execute as two modules, while the two incompatible-type assertions remain
covered by local exact-type tests because the execution harness does not replay
`assert_unlinkable` yet.

For typed externref elements, no-table drop state, table copy/init/drop, and the
final five compile-rejected Release 2 modules, run:

```sh
go test -count=1 \
  -run '^(TestTypedExternrefElementsExecuteAcrossModesAndIndexes|TestExternrefTableCopyPreservesIdentityOverlapAndBounds|TestActiveExternrefElementsPreserveDeclarationOrderOnFailedInstantiation|TestTypedElementMetadataStaysBoundedAndOutOfCodecV19|TestRelease2TypedElementCompileGapSourceGuard|TestRelease2TypedElementCompileGapExecution)$' \
  ./src/wago
```

The source and execution guards pin `bulk.wast:274/297`, `elem.wast:654-677`,
and `table.wast:8/9`. They require active/passive/declarative null externref
segments, nonzero and imported destinations, 8-byte `table.init`/`table.copy`,
`elem.drop`, overlap, zero-length and out-of-bounds behavior, declaration-order
failed-instantiation effects, no failed externref producer root, codec-v22
structural round-trip, snapshot rejection, and unchanged public/runtime layouts. The two `bulk.wast`
modules prove element drop state without a table. The two `table.wast` modules
prove that inert unexported huge spare capacities do not force an unusable arena
reservation when no grow/export surface can observe them. The complete files are
green at `bulk.wast` 13 modules / 104 assertions, `elem.wast` 29 / 37, and
`table.wast` 9 / 0.

Each Release 2 file replay uses one `Runtime`, one `spectest.table` (10/20), and
one `spectest.memory` (1/2). It also binds exact reflection-free no-op HostFuncs
for `print`, `print_i32`, `print_i64`, `print_f32`, `print_f64`,
`print_i32_f32`, and `print_f64_f64`. Memory/table mutation persists within one
file and never leaks to another file. All importing instances close before the
file-scoped owners. Imported-memory re-export returns the original `*Memory`, so
`imports.wast` growth/size actions observe one identity and producer lifetime.

For codec-v22 structural reference persistence and the snapshot live-state
boundary, run:

```sh
go test -count=1 -run '^TestCompiledCodecV21' ./src/wago
```

This gate covers the version-19 incompatibility boundary, exact reference
signatures/globals, multiple imported/local typed tables and exports, typed
active/passive/declarative elements, required feature bits, reused receivers,
truncation/malformed/live-bit rejection, loaded execution, and continued
snapshot rejection of reference globals/tables. Codec fuzz seeds include the
same structural fixture; marshal/unmarshal performance is tracked by
`Benchmark(Marshal|Unmarshal)Compiled(StructuralReferences|SmallScalar)`.

The CI-card renderer can also consume captured suite logs through
`SPEC_LOG_DIR`; this keeps report parsing testable without rerunning WABT. Run
its committed synthetic fixture with:

```sh
scripts/spec-card_test.sh
```

The files under `scripts/testdata/spec-card` are parser fixtures, not published
conformance counts. Real support claims must come from a fresh WABT-backed run.

Both harnesses print per-file and total module/assertion pass, fail, and skip
counts. The execution totals also print a fixed, bounded reason vector:
`compile-rejected`, `instantiate-rejected`, `module-unavailable`,
`absent-export`, `reference-argument`, `reference-result`, and
`reference-global`. The execution harness records up to 64 exact instantiate-
rejected file/line sites and retains bounded cause classification for regression
diagnostics. `TestRelease2InstantiateGapInventory` now requires that inventory
to be empty. The full Release 2 execution test fails if any module or assertion
is skipped, so a future unsupported module cannot become a green reasoned skip.
Unknown action/value shapes remain harness failures, not skips.

A missing/empty Release 2 checkout, a discovered file that disappears, or a
`wast2json` conversion failure is an error rather than a silent empty run. The
July 10, 2026 validation run is green at 1,600 passed / 0 failed / 0 skipped
modules and 2,880 passed / 0 failed / 1,077 non-validation-action skips, with no
accepted-invalid or accepted-malformed sites. The execution run is green at
1,600 passed / 0 failed / 0 skipped modules and 48,248 passed / 0 failed / 0
skipped assertions; every bounded gap reason is zero. `imports.wast` is fully
green at 54 / 34 modules/assertions, `data.wast` at 25 / 14, and `linking.wast`
at 21 / 90. `.wago` codec v23 persists structural reference globals, indexed typed
tables/exports/elements, exact declared table/memory-limit forms, and required-feature
bits while rejecting live runtime identity. The remaining product gates are also
closed locally: snapshot fail-closed admission,
all-table/reference inspection after compile/load, trapped-lease release, and
consolidated cross-link teardown. These product tests do not alter official corpus
counts. Host-created funcref globals remain covered by the local ownership/lifetime
gate. Do not weaken valid-module rejection, invalid-module acceptance,
missing-corpus failures, or the zero-skip execution gate.
