# Reusable memory importer storage

Later update: [inline memory32 importer counts](memory32-importer-storage.md) remove the overflow table from memory32. The measurements below describe the earlier layout.

This follows the [correctness review measurements](correctness-review-followup.md), using `5afc2090f` as the production-code baseline. The earlier overflow threshold remains 63, and the packed memory representation is unchanged.

The overflow table now stores `uint32` counts directly in a typed map protected by one mutex. Updating a count reuses its map slot. This removes replacement `sync.Map` entries and integer boxing. Counts below 63 do not take the table lock; only a transition out of overflow deletes a table entry. `Memory` remains 16 bytes and `memoryState` remains 24 bytes on the tested 64-bit host. No guest load/store, compiler, global-write, or GC-reference-test path changed.

The table is created only when a count first overflows. First use and growth for additional distinct overflowing memories still allocate. Deleted entries release their memory-state keys. Empty map capacity is retained for reuse, so repeated threshold crossings do not repeatedly allocate storage. This trades some retained table capacity for less allocation traffic; it does not retain the closed memories themselves.

`TestMemoryImporterUpdatesReuseStorage` first failed with 1–4 allocations per round trip at counts 62, 63, 64, 254, 255, and 256. It now requires zero allocations after storage is warmed, checks the count values, and checks deletion of the state key. `TestMemoryImporterConcurrentStates` exercises independent states, repeated threshold crossings, and the full uint32 count range under concurrent access.

## Validation

The focused importer, memory type, footprint, conflicting-export, and host wait tests pass. The following commands also pass:

```sh
go test ./src/wago -run 'TestMemoryImporter|TestMemoryTypeFlags|TestMemoryOwnershipSidecar|TestConflictingMemoryReexports|TestSharedHostMemoryExportKeepsWaitCapability' -count=1
go test -race ./src/wago -run 'Memory|Shared|Global|Canonical' -count=1
go test ./...
go test -tags wago_runtime ./cli/...
```

The full and runtime-tagged suites use Go 1.22.12 and TinyGo 0.41.1, with the pinned WABT and spec interpreter from the previous report. Other measurements use Go 1.27.1 on Linux AMD64 (AMD Ryzen 7 8845HS).

## Measurements

Before/after timing uses eight alternating samples of 200 ms. Serial tests use one CPU pinned to CPU 2. The concurrency benchmark uses one or eight workers, pinned to CPUs 2–9, with a separate memory state per worker. Setup and table growth are excluded from the serial counter benchmarks.

Build the public API benchmark binary before and after the change with `go test -c -o <binary> ./src/wago`. Run it from `src/wago`:

```sh
taskset -c 2 <binary> -test.run='^$' -test.bench='^BenchmarkMemoryImporterCount$' -test.cpu=1 -test.benchmem -test.benchtime=200ms -test.count=1
taskset -c 2-9 <binary> -test.run='^$' -test.bench='^BenchmarkMemoryImporterParallel$' -test.cpu=1,8 -test.benchmem -test.benchtime=200ms -test.count=1
taskset -c 2 <binary> -test.run='^$' -test.bench='^BenchmarkRuntimeInstantiate(SharedMemoryImport|ImportedMemoryReexport)$' -test.cpu=1 -test.benchmem -test.benchtime=200ms -test.count=1
benchstat before.txt after.txt
```

A separate first-use probe created a fresh table, inserted preallocated state keys with count 63, and deleted all keys. These are allocated bytes for that table lifetime, including the temporary table object, not retained-heap measurements. The production table itself is global. The probe used three 100 ms samples:

| Distinct states | Old allocated bytes | New allocated bytes | Old allocations | New allocations |
|---|---:|---:|---:|---:|
| 1 | 256 | 208 | 3 | 3 |
| 8 | 827 | 208 | 11 | 3 |
| 64 | about 6779 | 4664 | 87 | 12 |

The global table object itself shrinks from 48 to 16 bytes in this Go version; these sizes do not include map backing storage.

## Paired results

All warmed serial counter cases now report 0 B/op and 0 allocs/op. Serial updates are faster, and normal public import/re-export timings have no significant change. The eight-worker test at count 63 is about 10% slower (101.3 → 111.9 ns/op), because different heavily shared memories now use the same table lock. This contention is at importer attach/detach boundaries; guest memory accesses do not use the table. The other two eight-worker differences are not statistically significant.

### Counter updates

| Benchmark | Before ns/op | After ns/op | Before → after allocs/op | Before → after B/op | Timing change |
|---|---:|---:|---:|---:|---|
| `MemoryImporterCount/steady/62` | 10.71 | 3.713 | 0 → 0 | 0 → 0 | -65.35% (p=0.000 n=8) |
| `MemoryImporterCount/steady/63` | 42.71 | 16.43 | 1 → 0 | 48 → 0 | -61.54% (p=0.000 n=8) |
| `MemoryImporterCount/steady/64` | 42.79 | 16.44 | 1 → 0 | 48 → 0 | -61.59% (p=0.000 n=8) |
| `MemoryImporterCount/steady/254` | 42.86 | 16.42 | 1 → 0 | 48 → 0 | -61.70% (p=0.000 n=8) |
| `MemoryImporterCount/steady/255` | 42.77 | 16.43 | 1 → 0 | 48 → 0 | -61.60% (p=0.000 n=8) |
| `MemoryImporterCount/steady/256` | 48.34 | 16.43 | 2 → 0 | 52 → 0 | -66.01% (p=0.000 n=8) |
| `MemoryImporterCount/transition/62-63-62` | 58.4 | 35.77 | 1 → 0 | 48 → 0 | -38.75% (p=0.000 n=8) |
| `MemoryImporterCount/transition/63-64-63` | 92.76 | 35.91 | 2 → 0 | 96 → 0 | -61.28% (p=0.000 n=8) |
| `MemoryImporterCount/transition/254-255-254` | 92.99 | 35.84 | 2 → 0 | 96 → 0 | -61.46% (p=0.000 n=8) |
| `MemoryImporterCount/transition/255-256-255` | 99.32 | 35.85 | 3 → 0 | 100 → 0 | -63.90% (p=0.000 n=8) |

### Concurrent updates

| Benchmark | Before ns/op | After ns/op | Before → after allocs/op | Before → after B/op | Timing change |
|---|---:|---:|---:|---:|---|
| `MemoryImporterParallel/62` | 57.6 | 34.99 | 1 → 0 | 48 → 0 | -39.25% (p=0.000 n=8) |
| `MemoryImporterParallel/62-8` | 76.42 | 86.25 | 1 → 0 | 48 → 0 | ~ (p=0.105 n=8) |
| `MemoryImporterParallel/63` | 84.96 | 32.15 | 2 → 0 | 96 → 0 | -62.16% (p=0.000 n=8) |
| `MemoryImporterParallel/63-8` | 101.3 | 111.9 | 2 → 0 | 96 → 0 | +10.47% (p=0.010 n=8) |
| `MemoryImporterParallel/255` | 91.01 | 32.16 | 3 → 0 | 100 → 0 | -64.67% (p=0.000 n=8) |
| `MemoryImporterParallel/255-8` | 102.9 | 112.4 | 3 → 0 | 100 → 0 | ~ (p=0.442 n=8) |

### Normal public imports

| Benchmark | Before ns/op | After ns/op | Before → after allocs/op | Before → after B/op | Timing change |
|---|---:|---:|---:|---:|---|
| `RuntimeInstantiateSharedMemoryImport` | 1995 | 1994 | 13 → 13 | 2088 → 2088 | ~ (p=0.943 n=8) |
| `RuntimeInstantiateImportedMemoryReexport` | 1927 | 1937 | 12 → 12 | 2096 → 2096 | ~ (p=0.456 n=8) |

`just lint` passed before the commit, and `just docs` passed. Native platform results are available in [PR #670 checks](https://github.com/wago-org/wago/pull/670/checks).
