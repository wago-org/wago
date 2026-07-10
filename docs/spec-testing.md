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

The Release 2 source is the specification repository because that tagged tree
contains the official release's complete core tests, including the nested
`test/core/simd` files. The old `WebAssembly/testsuite` checkout is a preserved
pre-reference-types baseline and its proposal directories are not a substitute
for a tagged Release 2 core corpus.

Initialize only the suite needed for a focused run:

```sh
git submodule update --init tests/spec       # WebAssembly 1.0
git submodule update --init tests/spec-v2    # WebAssembly 2.0
```

## Commands

Install WABT so `wast2json` is on `PATH`, then run:

```sh
make spec1
make spec2
```

`make spec2` sets `WAGO_SPECTEST_DIR` to the `tests/spec-v2` checkout and
`WAGO_SPEC_VERSION=2.0`; the execution harness resolves the tagged repository's
`test/core` layout. CI provisions WABT, initializes only `tests/spec-v2`, and
runs this same target in the `WebAssembly 2.0 spec` job.

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
out-of-bounds function index.

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
memory, and two local memories. Multi-memory is not part of WebAssembly 2.0 and
is a documented wago non-goal, so validation counts imported and local memories
together and rejects totals above one clearly; focused local tests preserve one
imported or one local memory as valid. The guard exercises both AST and byte-
backed validation and keeps this rejection ahead of frontend support filtering.

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
section spans. Multi-memory remains outside the target; do not generalize this
field to a nonzero index.

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
`reference-global`. Reference-valued `get` assertions are counted as
`reference-global` rather than the broader result category. Unknown action/value
shapes are harness failures, not skips.

A missing/empty Release 2 checkout, a discovered file that disappears, or a
`wast2json` conversion failure is an error rather than a silent empty run.
During closeout, known unsupported modules and reference-valued assertions remain
reasoned skips rather than being treated as support. After the shared-memory
maximum fix, the July 10, 2026 validation run is 1,600 passed / 0 failed / 0
skipped valid modules. Invalid/malformed assertions are 2,879 passed / 1 failed /
1,077 skipped and still keep the complete validation command red. The newly
passing assertion is `binary.wast` line 1563; the sole remaining failure is
`binary.wast` line 48. The execution run remains 1,423 passed / 177 skipped
modules and 46,384 passed / 0 failed / 1,864 skipped assertions, with gaps
compile-rejected=97, instantiate-rejected=80, module-unavailable=1,773,
absent-export=0, reference-argument=36, reference-result=55, and
reference-global=0.
WebAssembly 2.0 completion requires every feature-related reason count to reach
zero; do not weaken valid-module rejection, invalid-module acceptance, or
missing-corpus failures into silent skips.
