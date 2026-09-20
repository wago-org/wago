# Inline memory32 importer counts

This follows the [reusable overflow table measurements](memory-importer-storage.md), with `223b2496e` as the baseline.

Memory32 needs only 17 bits for its declared maximum of 65,536 pages. Its existing 64-bit state word now holds that maximum, the full 32-bit importer count, and all nine flags. The six remaining bits are unused. Memory64 retains its 49-bit exact maximum, six-bit inline count, and existing overflow table at count 63.

| State word | Maximum | Importer count | Unused | Flags |
|---|---:|---:|---:|---:|
| Memory32 | 17 bits | 32 bits | 6 bits | 9 bits |
| Memory64 | 49 bits | 6 bits | 0 bits | 9 bits |

Memory32 never inserts an importer count into the global table, including on first use or at the maximum uint32 count. This removes both its table storage and contention between unrelated memories. `Memory` remains 16 bytes and `memoryState` remains 24 bytes on the tested 64-bit host. No additional runtime fields, wrappers, or guest execution checks were added.

This is not a fully lock-free memory lifecycle. Each memory still uses its existing mutex to serialize attach, detach, close, and ownership changes. Memory64 overflow still uses the shared table mutex and can allocate on first use or table growth. Repeated memory64 updates retain the previous zero-allocation behavior after warmup. A new concurrent table would add storage and lifetime-management complexity; this change instead removes the need for that table on the memory32 path.

The address-type flag selects the layout. A first export that establishes memory64 repacks any existing count before recording its declaration. Later conflicting address or shared types are still rejected before those changes. The full uint32 count limit remains enforced by attach.

## Tests

`TestMemory32ImporterCountNeedsNoOverflowStorage` failed before the change because counts of 63 or more entered the global table. It passes with the new layout. It checks maxima 0, 1, 65,535, and 65,536, counts through uint32 maximum, all memory32 flags, and count preservation across maximum updates.

`TestMemoryFirstExportPreservesImporterCount` checks first exports of both address forms with existing counts through uint32 maximum. Allocation tests cover both layouts, and concurrent tests mix memory32 and memory64 states. Existing tests cover memory64's exact maximum, shared/unshared export conflicts, host wait capability, count overflow rejection, and object sizes.

## Validation and performance

All of these local commands passed:

```sh
go test ./src/wago -run 'TestMemory32Importer|TestMemoryFirstExport|TestMemoryImporter|TestMemoryTypeFlags|TestMemoryOwnershipSidecar|TestMemoryStatePreserves|TestConflictingMemoryReexports|TestSharedHostMemoryExportKeepsWaitCapability' -count=1
go test -race ./src/wago -run 'Memory|Shared|Global|Canonical' -count=1
go test ./...
go test -tags wago_runtime ./cli/...
GOMAXPROCS=2 just test fuzz 5s
just test corpus quick
just lint
```

Full tests, runtime-tagged tests, and corpus tests use Go 1.22.12 and TinyGo 0.41.1. Other tests and benchmarks use Go 1.27.1. The pinned WABT 1.0.41 is on PATH. Full and corpus tests use the pinned spec interpreter at revision `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. Tests use `GOFLAGS=-buildvcs=false` in the working checkout.

Timing compares eight alternating 200 ms samples on Linux AMD64 (AMD Ryzen 7 8845HS), with serial tests pinned to CPU 2 and parallel tests to CPUs 2–9. Both binaries contain the same benchmark source; the baseline has the previous production implementation. Build each with `go test -c -o <binary> ./src/wago`, then run from `src/wago`:

```sh
taskset -c 2 <binary> -test.run='^$' -test.bench='^BenchmarkMemory(64)?ImporterCount$' -test.cpu=1 -test.benchmem -test.benchtime=200ms -test.count=1
taskset -c 2-9 <binary> -test.run='^$' -test.bench='^BenchmarkMemory(64)?ImporterParallel$' -test.cpu=1,8 -test.benchmem -test.benchtime=200ms -test.count=1
taskset -c 2 <binary> -test.run='^$' -test.bench='^BenchmarkRuntimeInstantiate(SharedMemoryImport|ImportedMemoryReexport)$' -test.cpu=1 -test.benchmem -test.benchtime=200ms -test.count=1
benchstat before.txt after.txt
```

All measured counter cases remain at **0 B/op and 0 allocs/op** after setup, for both layouts. Memory32 additionally needs no table allocation on first use. Memory64 still allocates table storage on first use and growth.

The memory32 serial overflow cases improve by about 77%, and round trips improve by about 80%. With eight workers and independent states, the count-63 round trip improves from 112.5 to 1.536 ns/op. Parallel ns/op measures aggregate throughput, not individual operation latency.

Small regressions remain: memory32's already-inline count-62 case rises by 0.079 ns, and memory64's count-62 case by 0.204 ns. Three memory64 steady overflow cases rise by about 0.06–0.16 ns. Memory64 round-trip and eight-worker changes are not statistically significant. These results do not justify a more complex layout or another shared data structure.

## Paired results


### Serial counts

| Benchmark | Before | After | Timing change |
|---|---:|---:|---|
| `MemoryImporterCount/steady/62` | 3.633ns | 3.712ns | +2.16% (p=0.000 n=8) |
| `MemoryImporterCount/steady/63` | 16.360ns | 3.707ns | -77.34% (p=0.000 n=8) |
| `MemoryImporterCount/steady/64` | 16.360ns | 3.730ns | -77.20% (p=0.000 n=8) |
| `MemoryImporterCount/steady/254` | 16.335ns | 3.712ns | -77.27% (p=0.000 n=8) |
| `MemoryImporterCount/steady/255` | 16.350ns | 3.721ns | -77.24% (p=0.000 n=8) |
| `MemoryImporterCount/steady/256` | 16.395ns | 3.706ns | -77.40% (p=0.000 n=8) |
| `MemoryImporterCount/transition/62-63-62` | 36.190ns | 7.081ns | -80.43% (p=0.000 n=8) |
| `MemoryImporterCount/transition/63-64-63` | 35.805ns | 7.040ns | -80.34% (p=0.000 n=8) |
| `MemoryImporterCount/transition/254-255-254` | 35.970ns | 7.035ns | -80.44% (p=0.000 n=8) |
| `MemoryImporterCount/transition/255-256-255` | 35.890ns | 7.025ns | -80.43% (p=0.000 n=8) |
| `Memory64ImporterCount/steady/62` | 3.644ns | 3.848ns | +5.57% (p=0.000 n=8) |
| `Memory64ImporterCount/steady/63` | 16.34ns | 16.46ns | +0.73% (p=0.046 n=8) |
| `Memory64ImporterCount/steady/64` | 16.34ns | 16.43ns | ~ (p=0.265 n=8) |
| `Memory64ImporterCount/steady/254` | 16.34ns | 16.40ns | +0.37% (p=0.014 n=8) |
| `Memory64ImporterCount/steady/255` | 16.35ns | 16.43ns | ~ (p=0.111 n=8) |
| `Memory64ImporterCount/steady/256` | 16.34ns | 16.49ns | +0.95% (p=0.028 n=8) |
| `Memory64ImporterCount/transition/62-63-62` | 36.80ns | 36.44ns | ~ (p=0.902 n=8) |
| `Memory64ImporterCount/transition/63-64-63` | 35.93ns | 35.91ns | ~ (p=0.488 n=8) |
| `Memory64ImporterCount/transition/254-255-254` | 35.91ns | 36.05ns | ~ (p=0.855 n=8) |
| `Memory64ImporterCount/transition/255-256-255` | 35.95ns | 35.98ns | ~ (p=0.557 n=8) |

### Independent concurrent states

| Benchmark | Before | After | Timing change |
|---|---:|---:|---|
| `MemoryImporterParallel/62` | 34.605ns | 5.797ns | -83.25% (p=0.000 n=8) |
| `MemoryImporterParallel/62-8` | 88.280ns | 1.534ns | -98.26% (p=0.000 n=8) |
| `MemoryImporterParallel/63` | 32.085ns | 5.769ns | -82.02% (p=0.000 n=8) |
| `MemoryImporterParallel/63-8` | 112.500ns | 1.536ns | -98.63% (p=0.000 n=8) |
| `MemoryImporterParallel/255` | 31.940ns | 5.827ns | -81.75% (p=0.000 n=8) |
| `MemoryImporterParallel/255-8` | 113.000ns | 1.535ns | -98.64% (p=0.000 n=8) |
| `Memory64ImporterParallel/62` | 35.28ns | 35.57ns | ~ (p=0.442 n=8) |
| `Memory64ImporterParallel/62-8` | 88.83ns | 89.39ns | ~ (p=0.901 n=8) |
| `Memory64ImporterParallel/63` | 32.00ns | 31.81ns | ~ (p=0.857 n=8) |
| `Memory64ImporterParallel/63-8` | 112.3ns | 113.6ns | ~ (p=0.663 n=8) |
| `Memory64ImporterParallel/255` | 32.17ns | 31.75ns | -1.31% (p=0.010 n=8) |
| `Memory64ImporterParallel/255-8` | 113.2ns | 113.0ns | ~ (p=0.699 n=8) |

### Normal public imports

| Benchmark | Before | After | Timing change |
|---|---:|---:|---|
| `RuntimeInstantiateSharedMemoryImport` | 1.990µs | 1.987µs | ~ (p=0.223 n=8) |
| `RuntimeInstantiateImportedMemoryReexport` | 1.914µs | 1.903µs | ~ (p=0.266 n=8) |

Normal shared imports remain at 2,088 B/op and 13 allocs/op; imported-memory re-exports remain at 2,096 B/op and 12 allocs/op. Neither timing change is statistically significant.

Compiler code, guest load/store code, and shift lowering did not change. Compiler/guest timings and native frame/spill measurements were not repeated for this count-storage change; the earlier measurements remain in the [correctness follow-up report](correctness-review-followup.md). This report makes no global zero-allocation claim.

`just docs` also passed. Native platform checks will run on the PR after this commit is pushed.
