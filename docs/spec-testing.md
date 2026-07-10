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
reasoned skips rather than being treated as support. After the implicit-reference-
select fix, the July 9, 2026 validation run is 1,600 passed / 0 failed / 0 skipped
valid modules. Invalid/malformed assertions
are 2,849 passed / 31 failed / 1,077 skipped and still keep the complete
validation command red. The newly passing assertion is `select.wast:340`;
remaining failures are 24 cases in `binary.wast`, six in
`binary-leb128.wast`, and `custom.wast:123`. The execution run remains 1,423
passed / 177 skipped modules and 46,384 passed / 0 failed / 1,864 skipped
assertions, with gaps compile-rejected=97,
instantiate-rejected=80, module-unavailable=1,773, absent-export=0,
reference-argument=36, reference-result=55, and reference-global=0.
WebAssembly 2.0 completion requires every feature-related reason count to reach
zero; do not weaken valid-module rejection, invalid-module acceptance, or
missing-corpus failures into silent skips.
