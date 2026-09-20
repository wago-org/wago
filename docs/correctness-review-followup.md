# Correctness review follow-up

The later [importer storage change](memory-importer-storage.md) removes the repeated overflow allocations measured below. These tables retain the earlier results for comparison.

This follow-up to [PR #670](https://github.com/wago-org/wago/pull/670) compares the previous PR head, `f6d7e56ed`, with the fixes through `e67ea054f`. The importer benchmark also compares original main, `4e3345d2d`. The [original report](correctness-review-performance.md) records the earlier fixes and measurements against main.

## Changes

- Reject a conflicting shared/unshared memory export under the existing state lock, before changing state. Both export orders are tested with both consumers instantiated first. Rejection preserves the state, backing, and first export.
- Gate the threaded provider regression and the new conflicting-export fixture on `CoreFeatureThreads`. Packing, overflow, and host wait-capability tests remain active without the threads backend.
- Encode destination registers 8–15 with inverted EVEX.R (P0 bit 7). GNU assembler 2.44 supplied the exact expected bytes for 36 register cases and two memory cases. One older memory-form golden value needed the same correction. The managed register range stays below 16.
- Add all five i64 shifts/rotates at counts 0, 31, 32, 63, 64, and 127, with positive and negative operands. Add 40 nested i32/i64 division/remainder/shift combinations, each run with those six counts and two divisors. The tests require actual spills and reloads and check execution against Go results.
- Add negative verifier tests for every requested data-operation index, operand type, missing effect, and extra effect. Add unknown-poison tests for dead stack-polymorphic state and rejected instruction, branch, and return uses. The verifier is unchanged.
- Add `BenchmarkMemoryImporterCount` at counts 62, 63, 64, 254, 255, and 256, with four increment/decrement transitions. Setup and cleanup are excluded; each measured operation includes the state lock and count reads.
- Document canonical-map invalidation after `AddTypes` grows the collector type set, and the expression encoder's explicit memory indices and fail-fast u32 offset limit.

No production compiler, register-allocation, guest load/store, global-write, or GC-test path changed in this follow-up. The two production changes are one locked export-boundary check and one EVEX prefix bit. The memory object and state fields are unchanged (16 and 24 bytes on the tested 64-bit host).

## Red/green checks

`TestConflictingMemoryReexports` failed in both export orders before the export check and passed afterward. `TestEVEXRegisterBoundaries` failed for high destinations before the prefix correction and passed afterward. GNU `as --64` plus `objcopy -O binary --only-section=.text` independently confirmed the byte expectations. The previous CI run failed both Windows jobs at the unguarded threaded fixture; the follow-up gates that fixture explicitly, without treating compile errors as skips.

The added shift and verifier coverage passes with the existing fixes; no further compiler or verifier change was needed. The existing 3,000-expression generated regression suite also passes.

## Local validation

All of these completed successfully:

```sh
just lint
go test ./src/core/encoder/amd64 ./src/wago -run 'TestEVEX|TestReviewSharedProvider|TestConflictingMemory|TestMemoryTypeFlags|TestSharedHostMemoryExport' -count=1
go test ./src/core/compiler/backend/railshot/amd64 -run 'TestI64ShiftCountBoundaries|TestNestedShiftDivisionPressure' -count=1 -v
go test ./src/core/compiler/ir -run 'TestVerifyRejectsMalformedDataOperations|TestVerifyUnknownPoisonBoundaries|TestBuildUnreachableSelectUnknownType' -count=1 -v
go test ./src/core/compiler/ir ./src/core/compiler/backend/railshot/amd64 -count=1
go test ./src/core/runtime/gc/native ./src/core/compiler/wasm -count=1
go test ./...
go test -tags wago_runtime ./cli/...
go test -race ./src/core/compiler/wasm ./src/core/compiler/ir ./src/core/runtime/gc/native ./src/core/semver ./cli/manager/internal/plugin/build
go test -race ./src/wago -run 'Memory|Shared|Global|Canonical'
GOMAXPROCS=2 just test fuzz 5s
just test corpus quick
```

`just lint` passed before every commit. Its standard staticcheck step still reports existing findings; its runtime-tagged checks pass.

The complete Go suite and runtime-tagged CLI suite passed without exclusions with CI's Go 1.22.12 and TinyGo 0.41.1. The default local Go 1.27.1/TinyGo 0.42.0 pair still fails the two standalone TinyGo tests with duplicate `tinygo_task_exit` symbols, as seen on unchanged main in the original review. The supported pair resolves that local toolchain issue without a source change. Other focused, race, fuzz, and corpus checks used Go 1.27.1.

Full-suite environment (clear the inherited `GOROOT` when changing Go versions):

```sh
unset GOROOT
export PATH=/home/jtenner/.local/share/mise/installs/go/1.22.12/bin:/tmp/wago-gc-tinygo/tinygo/bin:/home/jtenner/Projects/wago/.tools/wabt-1.0.41-linux-x64/bin:$PATH
export GOFLAGS=-buildvcs=false
export WAGO_SPEC_INTERPRETER=/home/jtenner/Projects/wago/.tools/spec-interpreter-9d36019973201a19f9c9ebb0f10828b2fe2374aa/wasm
export WAGO_SPEC_INTERPRETER_REVISION=9d36019973201a19f9c9ebb0f10828b2fe2374aa
go test ./...
go test -tags wago_runtime ./cli/...
```

## Shift frame and spill measurements

All 40 stress cases have identical code bytes, native frame bytes, spill counts, and reload counts before and after this follow-up. Against original main, the tracked-value fix changes spill handling but does not enlarge the native frames:

| Cases | Revision | Code bytes (range) | Frame bytes | Spills | Reloads |
|---|---|---:|---:|---:|---:|
| i32 | Original main | 1066–1598 | 40 | 23 | 0 |
| i32 | PR before and after follow-up | 940–1460 | 40 | 12–35 | 12 |
| i64 | Original main | 1143–1869 | 56 | 23 | 0 |
| i64 | PR before and after follow-up | 1076–1802 | 56 | 12–35 | 12 |

These are compiler instrumentation counts, not hardware performance counters. For the original-main measurement only, the new assertion requiring nonzero reloads was replaced with a log so the old compiler could execute the same expressions; all returned the expected values. The original reported wrong-result regressions remain separate tests.

## Benchmark method

Linux AMD64, AMD Ryzen 7 8845HS, Go 1.27.1. Eight samples of 200 ms per workload, `GOMAXPROCS=1`, pinned to CPU 2. Before/after order is reversed on alternate repetitions; the three-way importer comparison also reverses order. Timings below are medians. No other local validation ran during timing.

Build each package with `go test -c -o <binary> <package>`, then run:

```sh
GOMAXPROCS=1 taskset -c 2 <binary> -test.run='^$' -test.bench='<pattern>' -test.benchmem -test.benchtime=200ms -test.count=1
benchstat before.txt after.txt
```

The package/pattern set is the same as the original report. The new importer pattern is `^BenchmarkMemoryImporterCount$` in `src/wago`; copy that benchmark into each baseline checkout. Corpus runs use `-wago.corpus=tiny,fib_rec,memory,dispatch,fannkuch,matmul,blake3,xxhash,embench-crc32` from `bench/suite`.

Native Linux AMD64/ARM64, Windows AMD64/ARM64, and Darwin AMD64/ARM64 execution is checked by the [PR CI matrix](https://github.com/wago-org/wago/pull/670/checks). These hosted checks are distinct from local Linux AMD64 validation.

## Paired benchmark results

`~` means no significant change in the benchstat comparison. Allocation counts are the same before and after this follow-up for every workload below. No native compiler or guest execution timing increase was significant. Small measured increases remain in simple expression encoding (+0.70%, about 12 ns) and null externref global writes (+0.89%, about 0.34 ns); neither production path changed in this follow-up. Compiler bytes/op varied slightly, including +0.15% for `fib_rec`, with unchanged allocation counts. No runtime change was made to recover these small differences.

### Native compiler

| Benchmark | Before | After | Time change | allocs/op (both) | B/op before → after |
|---|---:|---:|---|---:|---:|
| `RailshotCompileSmallScalar` | 6.864 µs | 6.857 µs | ~ (p=0.291 n=8) | 22 | 7924.5 → 7923.5 |
| `RailshotCompileMediumControl` | 8.217 µs | 8.096 µs | ~ (p=0.083 n=8) | 21 | 9524.5 → 9514.5 |
| `RailshotCompileALUHeavy` | 70.83 µs | 70.64 µs | ~ (p=0.328 n=8) | 16 | 113352 |

### Corpus compiler and guest execution

| Benchmark | Before | After | Time change | allocs/op (both) | B/op before → after |
|---|---:|---:|---|---:|---:|
| `Compile/tiny` | 7.23 µs | 7.178 µs | ~ (p=0.161 n=8) | 22 | 8053 → 8054.5 |
| `Compile/fib_rec` | 17.3 µs | 17.3 µs | ~ (p=0.574 n=8) | 50 | 26368.5 → 26408.5 |
| `Compile/memory` | 12.52 µs | 12.54 µs | ~ (p=0.505 n=8) | 29 | 10144 |
| `Compile/dispatch` | 17.83 µs | 17.88 µs | ~ (p=0.398 n=8) | 43 | 25080 → 25055.5 |
| `Compile/fannkuch` | 91.01 µs | 91.38 µs | ~ (p=0.065 n=8) | 64 | 66705 → 66738.5 |
| `Compile/matmul` | 54.7 µs | 54.88 µs | ~ (p=0.574 n=8) | 62 | 61632 |
| `Compile/blake3` | 849.8 µs | 842.3 µs | ~ (p=0.798 n=8) | 106 | 158472 |
| `Compile/xxhash` | 93.42 µs | 93.49 µs | ~ (p=0.328 n=8) | 49 | 64544 |
| `Compile/embench-crc32` | 17.91 µs | 17.78 µs | ~ (p=0.328 n=8) | 43 | 15840 |
| `Exec/tiny.add` | 20.26 ns | 20.21 ns | ~ (p=0.745 n=8) | 0 | 0 |
| `Exec/fib_rec.fib` | 848.1 µs | 847.8 µs | ~ (p=0.382 n=8) | 0 | 0 |
| `Exec/memory.sum` | 126.8 ns | 125.4 ns | ~ (p=0.266 n=8) | 0 | 0 |
| `Exec/dispatch.apply` | 21.68 ns | 21.62 ns | ~ (p=0.262 n=8) | 0 | 0 |
| `Exec/fannkuch.run` | 1.053 ms | 1.052 ms | ~ (p=0.574 n=8) | 0 | 0 |
| `Exec/matmul.run` | 119.6 µs | 119.5 µs | ~ (p=0.878 n=8) | 0 | 0 |
| `Exec/blake3.blake3_hash` | 269.2 µs | 270.9 µs | ~ (p=0.161 n=8) | 0 | 0 |
| `Exec/blake3.blake3_keyed_hash` | 271.2 µs | 269.6 µs | ~ (p=0.161 n=8) | 0 | 0 |
| `Exec/blake3.blake3_derive_key` | 278.7 µs | 283.1 µs | ~ (p=0.234 n=8) | 0 | 0 |
| `Exec/xxhash.xxhash_run` | 135.1 µs | 134.9 µs | ~ (p=0.234 n=8) | 0 | 0 |

### Collector reference tests

| Benchmark | Before | After | Time change | allocs/op (both) | B/op before → after |
|---|---:|---:|---|---:|---:|
| `CollectorRefTestDefined` | 27.44 ns | 27.4 ns | ~ (p=0.366 n=8) | 0 | 0 |
| `CollectorRefTestCanonical` | 25.12 ns | 25.11 ns | ~ (p=0.592 n=8) | 0 | 0 |

### Expression encoding and validation

| Benchmark | Before | After | Time change | allocs/op (both) | B/op before → after |
|---|---:|---:|---|---:|---:|
| `EncodeExprFastTables/simple` | 1.707 µs | 1.719 µs | +0.70% (p=0.011 n=8) | 7 | 1016 |
| `EncodeExprFastTables/memory` | 2.204 µs | 2.216 µs | ~ (p=0.170 n=8) | 8 | 1912 |
| `DecodeValidate` | 69.46 µs | 69.59 µs | ~ (p=0.279 n=8) | 350 | 47187 → 47186.5 |

### Version constraints

| Benchmark | Before | After | Time change | allocs/op (both) | B/op before → after |
|---|---:|---:|---|---:|---:|
| `ConstraintBounds/^1.2.3/parse` | 222.6 ns | 219.4 ns | -1.44% (p=0.001 n=8) | 5 | 408 |
| `ConstraintBounds/^1.2.3/check` | 28.73 ns | 28.78 ns | ~ (p=0.426 n=8) | 0 | 0 |
| `ConstraintBounds/~1.2.3/parse` | 224.7 ns | 220.9 ns | -1.69% (p=0.005 n=8) | 5 | 408 |
| `ConstraintBounds/~1.2.3/check` | 28.84 ns | 28.84 ns | ~ (p=0.629 n=8) | 0 | 0 |
| `ConstraintBounds/1.2.x/parse` | 209.2 ns | 205.5 ns | -1.77% (p=0.002 n=8) | 5 | 408 |
| `ConstraintBounds/1.2.x/check` | 28.85 ns | 28.86 ns | ~ (p=0.701 n=8) | 0 | 0 |
| `ConstraintBounds/1.2.3_-_2.3/parse` | 295.9 ns | 292.2 ns | ~ (p=0.052 n=8) | 7 | 424 |
| `ConstraintBounds/1.2.3_-_2.3/check` | 28.67 ns | 28.7 ns | ~ (p=0.262 n=8) | 0 | 0 |
| `ConstraintBounds/>=1.2.3_<2.0.0/parse` | 357.9 ns | 355.1 ns | -0.80% (p=0.037 n=8) | 8 | 552 |
| `ConstraintBounds/>=1.2.3_<2.0.0/check` | 28.67 ns | 28.67 ns | ~ (p=0.941 n=8) | 0 | 0 |

### Global writes and memory imports

| Benchmark | Before | After | Time change | allocs/op (both) | B/op before → after |
|---|---:|---:|---|---:|---:|
| `RuntimeInstantiateSharedMemoryImport` | 1.956 µs | 1.978 µs | ~ (p=0.053 n=8) | 13 | 2088 |
| `RuntimeInstantiateImportedMemoryReexport` | 1.879 µs | 1.9 µs | ~ (p=0.088 n=8) | 12 | 2096 |
| `SetGlobalValue/i32` | 37.99 ns | 38.28 ns | ~ (p=0.065 n=8) | 0 | 0 |
| `SetGlobalValue/null_funcref` | 37.54 ns | 37.5 ns | ~ (p=0.459 n=8) | 0 | 0 |
| `SetGlobalValue/null_externref` | 37.78 ns | 38.12 ns | +0.89% (p=0.016 n=8) | 0 | 0 |

## Importer overflow allocation costs

A transition is one complete increment/decrement pair, not one store. The previous PR head and this follow-up have identical allocation counts and bytes/op: the overflow implementation is unchanged. Original main kept counts through 254 inline; the PR keeps counts through 62 inline.

| Benchmark | Main time | PR time | Main B/op | PR B/op | Main allocs/op | PR allocs/op |
|---|---:|---:|---:|---:|---:|---:|
| `steady/62` | 10.46 ns | 10.58 ns | 0 | 0 | 0 | 0 |
| `steady/63` | 10.46 ns | 42.57 ns | 0 | 48 | 0 | 1 |
| `steady/64` | 10.47 ns | 42.36 ns | 0 | 48 | 0 | 1 |
| `steady/254` | 10.47 ns | 42.53 ns | 0 | 48 | 0 | 1 |
| `steady/255` | 42.45 ns | 42.58 ns | 48 | 48 | 1 | 1 |
| `steady/256` | 47.86 ns | 48.01 ns | 52 | 52 | 2 | 2 |
| `transition/62-63-62` | 20.96 ns | 57.83 ns | 0 | 48 | 0 | 1 |
| `transition/63-64-63` | 21.06 ns | 93.23 ns | 0 | 96 | 0 | 2 |
| `transition/254-255-254` | 57.46 ns | 92.81 ns | 48 | 96 | 1 | 2 |
| `transition/255-256-255` | 97.78 ns | 98.01 ns | 100 | 100 | 3 | 3 |

The earlier overflow boundary does add allocations relative to main. These results must not be read as a global zero-allocation claim. Successful canonical reference tests and normal global writes remain at 0 allocs/op in the measured set. Normal import and re-export allocations remain 13 and 12 allocs/op.

The overflow path was reviewed for a small allocation reduction. Skipping an unchanged count could improve the steady duplicate-store benchmark, but would not remove allocation from actual importer increments/decrements. Retaining entries below the boundary would retain memory states longer; replacing immutable map values with mutable counters changes the storage and synchronization contract. This PR leaves the existing overflow path intact.
