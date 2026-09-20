# Correctness review: performance and validation

Later update: [inline memory32 importer counts](memory32-importer-storage.md) remove the overflow table from memory32. The measurements below describe the earlier layout.

The [follow-up report](correctness-review-followup.md) adds conflict-export and EVEX destination fixes, stronger tests, the importer overflow allocation benchmark, and updated validation.

Baseline: `4e3345d2d` (`origin/main` when work began). Linux AMD64, AMD Ryzen 7 8845HS, Go 1.27.1. Each timing has eight samples, a 200 ms benchmark duration, `GOMAXPROCS=1`, and CPU affinity to CPU 2. Baseline and candidate binaries ran sequentially, with their order reversed on alternate repetitions. Tables show medians and the `benchstat` comparison; `~` means no statistically significant difference.

The native compilation and corpus measurements preceded the final API and parser tuning. Native code generation did not change after that capture. The GC, encoder, semver, and direct API measurements were repeated after their respective changes.

No measured benchmark added allocations per operation. The successful reference-test and global-set paths remain at zero allocations. `Memory` remains 16 bytes and its optional state remains 24 bytes. The packed memory type flags reduce the inline importer count from 254 to 62; 63 or more importers now use the existing overflow side table. Thus highly shared memories can incur side-table allocation and lookup costs earlier. The boundary tests cover both entry to and exit from that table, with the full memory64 maximum and all type flags preserved.

The retained timing costs buy required checks: outdated GC canonical maps must return an error, memory imports must check declared sharing, and expression encoding must preserve its memory argument. These measurements are local results, not a guarantee for other CPUs or workloads.

## Native compilation

| Benchmark | Baseline | Fixed | Change |
|---|---:|---:|---|
| `RailshotCompileSmallScalar` | 6.937 µs | 6.877 µs | ~ (p=0.878 n=8) |
| `RailshotCompileMediumControl` | 8.119 µs | 8.085 µs | ~ (p=0.798 n=8) |
| `RailshotCompileALUHeavy` | 71.18 µs | 71.18 µs | ~ (p=0.798 n=8) |

## Corpus compilation and execution

| Benchmark | Baseline | Fixed | Change |
|---|---:|---:|---|
| `Compile/tiny` | 7.514 µs | 7.436 µs | ~ (p=0.328 n=8) |
| `Compile/fib_rec` | 17.37 µs | 17.57 µs | ~ (p=0.442 n=8) |
| `Compile/memory` | 12.74 µs | 12.80 µs | ~ (p=0.721 n=8) |
| `Compile/dispatch` | 18.28 µs | 18.49 µs | ~ (p=0.645 n=8) |
| `Compile/fannkuch` | 92.19 µs | 91.77 µs | ~ (p=0.382 n=8) |
| `Compile/matmul` | 55.45 µs | 55.53 µs | ~ (p=0.959 n=8) |
| `Compile/blake3` | 856.5 µs | 826.8 µs | -3.47% (p=0.001 n=8) |
| `Compile/xxhash` | 94.06 µs | 93.76 µs | ~ (p=0.382 n=8) |
| `Compile/embench-crc32` | 18.30 µs | 18.11 µs | ~ (p=0.083 n=8) |
| `Exec/tiny.add` | 20.42 ns | 20.47 ns | ~ (p=0.739 n=8) |
| `Exec/fib_rec.fib` | 850.8 µs | 855.0 µs | ~ (p=0.382 n=8) |
| `Exec/memory.sum` | 127.1 ns | 126.4 ns | ~ (p=0.267 n=8) |
| `Exec/dispatch.apply` | 21.97 ns | 21.98 ns | ~ (p=0.591 n=8) |
| `Exec/fannkuch.run` | 1.059 ms | 1.057 ms | ~ (p=1.000 n=8) |
| `Exec/matmul.run` | 120.3 µs | 119.8 µs | ~ (p=0.505 n=8) |
| `Exec/blake3.blake3_hash` | 270.0 µs | 273.0 µs | ~ (p=0.328 n=8) |
| `Exec/blake3.blake3_keyed_hash` | 273.3 µs | 271.5 µs | ~ (p=0.505 n=8) |
| `Exec/blake3.blake3_derive_key` | 282.2 µs | 281.4 µs | ~ (p=0.328 n=8) |
| `Exec/xxhash.xxhash_run` | 135.7 µs | 136.0 µs | ~ (p=0.382 n=8) |

## Collector reference tests

| Benchmark | Baseline | Fixed | Change |
|---|---:|---:|---|
| `CollectorRefTestDefined` | 27.57 ns | 27.57 ns | ~ (p=0.777 n=8) |
| `CollectorRefTestCanonical` | 24.58 ns | 25.18 ns | +2.46% (p=0.000 n=8) |

## Expression encoding and validation

| Benchmark | Baseline | Fixed | Change |
|---|---:|---:|---|
| `EncodeExprFastTables/simple` | 1.714 µs | 1.710 µs | ~ (p=0.625 n=8) |
| `EncodeExprFastTables/memory` | 2.194 µs | 2.214 µs | +0.91% (p=0.026 n=8) |
| `DecodeValidate` | 68.94 µs | 69.51 µs | ~ (p=0.083 n=8) |

## Version constraints

| Benchmark | Baseline | Fixed | Change |
|---|---:|---:|---|
| `ConstraintBounds/^1.2.3/parse` | 218.9 ns | 219.9 ns | ~ (p=0.105 n=8) |
| `ConstraintBounds/^1.2.3/check` | 29.05 ns | 28.78 ns | -0.95% (p=0.048 n=8) |
| `ConstraintBounds/~1.2.3/parse` | 222.5 ns | 221.0 ns | ~ (p=0.442 n=8) |
| `ConstraintBounds/~1.2.3/check` | 29.17 ns | 28.86 ns | ~ (p=0.062 n=8) |
| `ConstraintBounds/1.2.x/parse` | 205.0 ns | 206.3 ns | ~ (p=0.168 n=8) |
| `ConstraintBounds/1.2.x/check` | 29.25 ns | 28.88 ns | -1.25% (p=0.010 n=8) |
| `ConstraintBounds/1.2.3_-_2.3/parse` | 291.7 ns | 291.6 ns | ~ (p=0.878 n=8) |
| `ConstraintBounds/1.2.3_-_2.3/check` | 29.02 ns | 28.73 ns | -0.98% (p=0.028 n=8) |
| `ConstraintBounds/>=1.2.3_<2.0.0/parse` | 356.1 ns | 355.4 ns | ~ (p=0.778 n=8) |
| `ConstraintBounds/>=1.2.3_<2.0.0/check` | 29.10 ns | 28.66 ns | -1.53% (p=0.019 n=8) |

## Global writes and memory imports

| Benchmark | Baseline | Fixed | Change |
|---|---:|---:|---|
| `RuntimeInstantiateSharedMemoryImport` | 1.993 µs | 2.007 µs | ~ (p=0.205 n=8) |
| `RuntimeInstantiateImportedMemoryReexport` | 1.946 µs | 1.910 µs | -1.85% (p=0.004 n=8) |
| `SetGlobalValue/i32` | 37.45 ns | 38.53 ns | +2.88% (p=0.000 n=8) |
| `SetGlobalValue/null_funcref` | 42.15 ns | 38.24 ns | -9.28% (p=0.000 n=8) |
| `SetGlobalValue/null_externref` | 37.76 ns | 38.27 ns | +1.36% (p=0.002 n=8) |

## Reproduction

Build each benchmark package at the baseline and candidate revisions with `go test -c -o <binary> <package>`. Copy the new `global_set_bench_test.go` into the baseline checkout to measure the identical public API workload. Run each binary eight times in alternating order:

```sh
GOMAXPROCS=1 taskset -c 2 <binary> -test.run='^$' -test.bench='<pattern>' -test.benchmem -test.benchtime=200ms -test.count=1
benchstat baseline.txt fixed.txt
```

Packages and patterns:

- `src/core/compiler/backend/railshot/amd64`: `^BenchmarkRailshotCompile(SmallScalar|MediumControl|ALUHeavy)$`
- `src/core/runtime/gc/native`: `^BenchmarkCollectorRefTest(Canonical|Defined)$`
- `src/core/compiler/wasm`: `^Benchmark(EncodeExprFastTables|DecodeValidate)$`
- `src/core/semver`: `^BenchmarkConstraintBounds$`
- `src/wago`: `^Benchmark(SetGlobalValue|RuntimeInstantiateSharedMemoryImport|RuntimeInstantiateImportedMemoryReexport)$`
- `bench/suite`: `^Benchmark(Compile|Exec)$`, run from `bench/suite` with `-wago.corpus=tiny,fib_rec,memory,dispatch,fannkuch,matmul,blake3,xxhash,embench-crc32`.

## Validation

Each of the 12 report issues first failed its regression test, then passed after the fix. The shift wrong-result and register-exhaustion cases share one fix. Additional red/green checks cover AST result widening, standalone IR verification, and host memory wait support after export.

- `just lint` passed before every commit. The recipe still reports its pre-existing standard staticcheck findings; runtime-tagged staticcheck passes. The new unused validator helper was removed.
- The full Go suite passes with only `TestBuildTinyGoEmbedsArtifactWithoutCompiler` and `TestBuildTinyGoStripsByDefault` excluded. Both fail with `ld.lld: duplicate symbol: tinygo_task_exit`; the latter was rerun on the unchanged baseline and failed identically.
- Runtime-tagged CLI tests pass.
- Race tests pass for Wasm validation, research IR, native GC, semver, plugin builds, and the focused public API regressions.
- The Linux ARM64 and Windows AMD64 public API test binaries compile. These are compile checks, not execution tests.
- Quick corpus pipeline and semantic execution tests pass.
- Staged Release 3 checks used WABT 1.0.41 and the interpreter at `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. `GOFLAGS=-buildvcs=false` avoids the local TinyGo worktree VCS-stamping failure.
