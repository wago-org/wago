# Memory64 inline importer capacity

This follow-up compares original main `4e3345d2d` and reviewed PR head `26173c7fc` with the restored eight-bit memory64 count. It retains the exact shared/unshared export checks and the memory32 count layout.

## Representation

Two bits can be recovered without a new field or a new side table:

- Address form and declared limits become known together in every production constructor/export path. One flag records both. An absent maximum is still a known external type, and does not permit an address-form change.
- The maximum field stores `maximum + 1` when a maximum is present, and zero when it is absent. The largest legal memory64 maximum, `2^48` pages, and its encoded value both fit in 49 bits. Memory32 uses the same scheme in 17 bits.

| State | Encoded maximum | Importers | Unused | Flags |
|---|---:|---:|---:|---:|
| Memory32 | 17 | 32 | 8 | 7 |
| Memory64 | 49 | 8 | 0 | 7 |

`Memory` remains 16 bytes and `memoryState` remains 24 bytes on the tested 64-bit platform. The existing per-memory lifecycle mutex remains in use. No guest load/store checks or runtime fields were added.

The three sharing properties remain separate: backing wait capability, whether the Wasm type is known, and the declared shared type. A host-created shared backing can have an unshared Wasm export. Its `JobMemory` uses the same mapping constructor as ordinary host memory, so mapping properties cannot recover that distinction.

A 47-bit maximum plus a cold large-maximum table was considered. Encoding maximum presence and merging the two equivalent known flags preserves the complete legal maximum range in place, so the large-maximum table is unnecessary. **No maximum, including the largest legal value, requires cold storage.** Only memory64 importer counts of 255 or more use the existing overflow table. First table use or growth can allocate; warmed updates do not.

## Correctness

The red phase failed `TestMemory64ImporterInlineCapacity` at count 63. The green phase retains the original memory-type and wait-capability tests and adds coverage for:

- counts through 254 without overflow storage, and transitions through 255, 256, and 1024;
- zero maximum versus absent maximum; values below, at, and above the proposed 47-bit sentinel; and the largest legal memory64 maximum;
- exact import limits after count updates;
- conflicting address and sharing types rejected without state changes;
- both declared sharing types and consistent re-exports.

Existing tests still check both shared/unshared export orders, memory32 count/maximum packing, object sizes, concurrent importer states, and first-export count preservation.

## Measurement

Go 1.27.1, Linux AMD64, AMD Ryzen 7 8845HS. All three revisions use the same expanded benchmark source. Ten 200 ms samples per revision alternate revision order. Serial runs use CPU 2 and `GOMAXPROCS=1`; parallel runs use CPUs 2–9 and `GOMAXPROCS=8`. Parallel ns/op measures aggregate throughput across independent memory objects. Values below are benchstat medians. A printed `p=0.000` in the raw results means `p<0.001`.

The `63 → 64 → 63` memory64 transition changes from 20.94 ns on main and 36.015 ns at the reviewed head to 7.409 ns. All candidate counter cases measure **0 B/op and 0 allocs/op after setup**. Memory32 retains its previous improvement. Memory64 counts 255 and above retain the removal of repeated allocations.

The cold overflow table still has one mutex. Eight-worker `254 → 255 → 254` is 88.795 ns versus 78.705 ns on main (+12.82%, p<0.001), while removing main’s one allocation per operation. The other measured parallel overflow counts do not show a statistically significant timing change versus main in this run. This remaining cold-path contention is separate from the restored inline range.

## Serial results

| Benchmark | Main ns/op | Reviewed head ns/op | Candidate ns/op | Main B/op | Candidate B/op | Main allocs/op | Candidate allocs/op |
|---|---:|---:|---:|---:|---:|---:|---:|
| `MemoryImporterCount/steady/62` | 10.43 | 3.706 | 3.668 | 0 | 0 | 0 | 0 |
| `MemoryImporterCount/steady/63` | 10.47 | 3.684 | 3.689 | 0 | 0 | 0 | 0 |
| `MemoryImporterCount/steady/64` | 10.44 | 3.69 | 3.68 | 0 | 0 | 0 | 0 |
| `MemoryImporterCount/steady/126` | 10.48 | 3.683 | 3.704 | 0 | 0 | 0 | 0 |
| `MemoryImporterCount/steady/127` | 10.48 | 3.706 | 3.677 | 0 | 0 | 0 | 0 |
| `MemoryImporterCount/steady/128` | 10.47 | 3.7 | 3.691 | 0 | 0 | 0 | 0 |
| `MemoryImporterCount/steady/254` | 10.47 | 3.69 | 3.679 | 0 | 0 | 0 | 0 |
| `MemoryImporterCount/steady/255` | 42.44 | 3.678 | 3.696 | 48 | 0 | 1 | 0 |
| `MemoryImporterCount/steady/256` | 47.68 | 3.682 | 3.679 | 52 | 0 | 2 | 0 |
| `MemoryImporterCount/steady/1024` | 47.9 | 3.692 | 3.687 | 52 | 0 | 2 | 0 |
| `MemoryImporterCount/transition/62-63-62` | 21.11 | 7.005 | 6.779 | 0 | 0 | 0 | 0 |
| `MemoryImporterCount/transition/63-64-63` | 20.88 | 6.975 | 6.834 | 0 | 0 | 0 | 0 |
| `MemoryImporterCount/transition/126-127-126` | 20.91 | 6.984 | 6.774 | 0 | 0 | 0 | 0 |
| `MemoryImporterCount/transition/127-128-127` | 20.91 | 7.031 | 6.796 | 0 | 0 | 0 | 0 |
| `MemoryImporterCount/transition/254-255-254` | 57.53 | 7.016 | 6.783 | 48 | 0 | 1 | 0 |
| `MemoryImporterCount/transition/255-256-255` | 97.39 | 6.972 | 6.823 | 100 | 0 | 3 | 0 |
| `MemoryImporterCount/transition/1023-1024-1023` | 102.6 | 6.992 | 6.771 | 104 | 0 | 4 | 0 |
| `Memory64ImporterCount/steady/62` | 10.51 | 3.825 | 3.865 | 0 | 0 | 0 | 0 |
| `Memory64ImporterCount/steady/63` | 10.45 | 16.37 | 3.862 | 0 | 0 | 0 | 0 |
| `Memory64ImporterCount/steady/64` | 10.46 | 16.3 | 3.88 | 0 | 0 | 0 | 0 |
| `Memory64ImporterCount/steady/126` | 10.44 | 16.37 | 3.854 | 0 | 0 | 0 | 0 |
| `Memory64ImporterCount/steady/127` | 10.52 | 16.32 | 3.865 | 0 | 0 | 0 | 0 |
| `Memory64ImporterCount/steady/128` | 10.44 | 16.34 | 3.862 | 0 | 0 | 0 | 0 |
| `Memory64ImporterCount/steady/254` | 10.44 | 16.3 | 3.873 | 0 | 0 | 0 | 0 |
| `Memory64ImporterCount/steady/255` | 42.3 | 16.41 | 16.36 | 48 | 0 | 1 | 0 |
| `Memory64ImporterCount/steady/256` | 48.05 | 16.3 | 16.39 | 52 | 0 | 2 | 0 |
| `Memory64ImporterCount/steady/1024` | 47.96 | 16.32 | 16.43 | 52 | 0 | 2 | 0 |
| `Memory64ImporterCount/transition/62-63-62` | 20.9 | 35.82 | 7.472 | 0 | 0 | 0 | 0 |
| `Memory64ImporterCount/transition/63-64-63` | 20.94 | 36.02 | 7.409 | 0 | 0 | 0 | 0 |
| `Memory64ImporterCount/transition/126-127-126` | 20.97 | 35.72 | 7.439 | 0 | 0 | 0 | 0 |
| `Memory64ImporterCount/transition/127-128-127` | 20.9 | 35.67 | 7.417 | 0 | 0 | 0 | 0 |
| `Memory64ImporterCount/transition/254-255-254` | 57.21 | 35.6 | 36.79 | 48 | 0 | 1 | 0 |
| `Memory64ImporterCount/transition/255-256-255` | 97.87 | 35.89 | 35.93 | 100 | 0 | 3 | 0 |
| `Memory64ImporterCount/transition/1023-1024-1023` | 102.1 | 35.77 | 36.21 | 104 | 0 | 4 | 0 |

## Parallel results

| Benchmark | Main ns/op | Reviewed head ns/op | Candidate ns/op | Main B/op | Candidate B/op | Main allocs/op | Candidate allocs/op |
|---|---:|---:|---:|---:|---:|---:|---:|
| `MemoryImporterParallel/62-8` | 5.316 | 1.534 | 1.479 | 0 | 0 | 0 | 0 |
| `MemoryImporterParallel/63-8` | 5.354 | 1.534 | 1.484 | 0 | 0 | 0 | 0 |
| `MemoryImporterParallel/64-8` | 5.353 | 1.535 | 1.481 | 0 | 0 | 0 | 0 |
| `MemoryImporterParallel/126-8` | 5.372 | 1.534 | 1.48 | 0 | 0 | 0 | 0 |
| `MemoryImporterParallel/127-8` | 5.36 | 1.534 | 1.48 | 0 | 0 | 0 | 0 |
| `MemoryImporterParallel/128-8` | 5.359 | 1.538 | 1.48 | 0 | 0 | 0 | 0 |
| `MemoryImporterParallel/254-8` | 77.64 | 1.534 | 1.482 | 48 | 0 | 1 | 0 |
| `MemoryImporterParallel/255-8` | 99.92 | 1.534 | 1.48 | 100 | 0 | 3 | 0 |
| `MemoryImporterParallel/256-8` | 112.7 | 1.534 | 1.478 | 104 | 0 | 4 | 0 |
| `MemoryImporterParallel/1024-8` | 106 | 1.532 | 1.483 | 104 | 0 | 4 | 0 |
| `Memory64ImporterParallel/62-8` | 5.382 | 90.3 | 1.653 | 0 | 0 | 0 | 0 |
| `Memory64ImporterParallel/63-8` | 5.37 | 112.1 | 1.653 | 0 | 0 | 0 | 0 |
| `Memory64ImporterParallel/64-8` | 5.372 | 109.8 | 1.65 | 0 | 0 | 0 | 0 |
| `Memory64ImporterParallel/126-8` | 5.365 | 109.4 | 1.65 | 0 | 0 | 0 | 0 |
| `Memory64ImporterParallel/127-8` | 5.367 | 110 | 1.654 | 0 | 0 | 0 | 0 |
| `Memory64ImporterParallel/128-8` | 5.364 | 109.2 | 1.651 | 0 | 0 | 0 | 0 |
| `Memory64ImporterParallel/254-8` | 78.7 | 109.8 | 88.8 | 48 | 0 | 1 | 0 |
| `Memory64ImporterParallel/255-8` | 102.1 | 108.6 | 111.7 | 100 | 0 | 3 | 0 |
| `Memory64ImporterParallel/256-8` | 107.7 | 109.3 | 110.2 | 104 | 0 | 4 | 0 |
| `Memory64ImporterParallel/1024-8` | 109.3 | 107.9 | 110.5 | 104 | 0 | 4 | 0 |

## Reproduction

Build each checkout with `go test -c -o <binary> ./src/wago`, with identical benchmark files, then alternate these commands between revisions ten times:

```sh
taskset -c 2 <binary> -test.run='^$' -test.bench='^BenchmarkMemory(64)?ImporterCount$' -test.benchmem -test.benchtime=200ms -test.cpu=1
taskset -c 2-9 <binary> -test.run='^$' -test.bench='^BenchmarkMemory(64)?ImporterParallel$' -test.benchmem -test.benchtime=200ms -test.cpu=8
benchstat main.txt before.txt candidate.txt
```

Set `GOMAXPROCS` to the matching CPU count. Raw data and scripts are retained in `/tmp/wago-perf-investigation`.

## Validation

These commands passed:

```sh
go test ./src/wago -run 'Memory|Shared' -count=1
go test ./...
go test -race ./src/wago ./src/core/runtime/gc/native -run 'Memory|Shared|Global|Canonical|TypeGrowth' -count=1
go test -tags wago_runtime ./cli/...
GOMAXPROCS=2 just test fuzz 5s
just test corpus quick
just lint
just docs
```

Full tests, runtime-tagged CLI tests, and the corpus use Go 1.22.12 and TinyGo 0.41.1. Other checks use Go 1.27.1. WABT 1.0.41 is first on PATH, `GOFLAGS=-buildvcs=false`, and the spec interpreter is pinned to `9d36019973201a19f9c9ebb0f10828b2fe2374aa`. The first broad memory run picked WABT 1.0.42 and failed fixture-version checks; the pinned rerun passed. The full test command includes the AMD64 encoder, Railshot, and GC packages.
