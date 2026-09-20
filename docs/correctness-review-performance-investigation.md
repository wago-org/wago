# Correctness review performance investigation

This investigation compares original main `4e3345d2d12afbce82485b5c875a5e45265c9a57`, reviewed PR head `26173c7fccf5d6f86f6978c58f7999d022999085`, and the follow-up changes on PR #670. No correctness check was removed. Results are Linux AMD64 measurements, not performance claims for other platforms.

## Final comparison

Final production revision: `5fda2109069801bdd54ea1714f8db5b8ec1a9915`. These are ten alternating 500 ms samples per revision, using the frozen benchmark source from the original bisection. The permanent expanded importer benchmark was measured separately with the same expanded source on all three revisions. No production file was changed for measurement.

| Benchmark | Main | Reviewed head | Final | Final vs main | p-value |
|---|---:|---:|---:|---:|---:|
| Memory64 63 → 64 → 63 | 20.94 ns | 35.72 ns | 7.45 ns | -64.42% | <.001 |
| CPU SIMD flag scanner | 171.65 ns | 208.65 ns | 209.40 ns | +21.99% | <.001 |
| Host-capable loop, host0/n8 | 158.40 ns | 175.30 ns | 157.35 ns | -0.66% | .002 |
| Prepared admission, 2000 functions | 14.944 µs | 15.732 µs | 15.686 µs | +4.97% | <.001 |
| Null externref write | 36.85 ns | 38.10 ns | 37.80 ns | +2.56% | .003 |

All five cases retain their allocation counts: zero B/op and zero allocs/op except prepared admission, which remains 6656 B/op and four allocs/op. The final null-write improvement versus reviewed head is not significant in this comparison (p=.075); the stack-size reduction is established by assembly, not by a claimed timing win.

The final host-capable result is back at main speed, but no host-call fast path was added. Its Go boundary and guest instructions remain identical to main after relocation normalization. Treat the timing recovery as placement-sensitive, not a portable host-call optimization. The scanner (+22%) and prepared admission (+5%) remain measured regressions. Their exact hardware placement mechanism is unresolved; unchanged source, instruction sequences, and inlining do not justify a speculative rewrite or padding workaround. The small nullability cost also remains; the cached-flag experiment below did not establish a further gain.

| Additional case | Main | Reviewed head | Final | Final vs main | Allocation count, all three |
|---|---:|---:|---:|---:|---:|
| Actual callbacks, host1/n8 | 1.875 µs | 1.865 µs | 1.863 µs | No significant change, p=.093 | 8 (512 B/op) |
| Railshot parallel control fixture, one CPU | 64.829 µs | 68.123 µs | 66.909 µs | +3.21%, p<.001 | 25 (23137 B/op median) |
| CompileSmallScalar | 14.131 µs | 13.957 µs | 13.660 µs | No significant change, p=.165 | 81 (15856 B/op) |
| CompileValueTypeInterning/distinct128 | 208.643 µs | 209.535 µs | 208.425 µs | No significant change, p=.280 | 724 (226329 B/op) |

The control fixture's remaining 3.21% compile cost is reported, not attributed to the global pointer change. It is a different case from the much larger compiler outlier in the earlier sweep. No compiler allocation count increased in these measurements.

[Final raw samples](benchmarks/correctness-review/final.csv) and [boundary confirmation samples](benchmarks/correctness-review/boundaries.csv) retain the full revision, sample number, ns/op, B/op, and allocs/op. CSV case names map to the table rows in order: `memory64`, `scanner`, `host`, `prepared`, `global`; additional names are `callback`, `compiler`, `small_compile`, and `global_compile`. Boundary rows use the first five names; `host-repeat` identifies the twenty-sample follow-up.

## Method and first transitions

All timings use Go 1.27.1 on an AMD Ryzen 7 8845HS, fixed CPU affinity, and the same benchmark case source. Serial runs use CPU 2 and `GOMAXPROCS=1`. Parallel memory tests use CPUs 2–9 and eight workers. Revision order alternates. The initial automated search used five 300 ms pairs, followed by a scan of earlier commits because some results were not monotonic. The candidate boundary matrix used ten 500 ms samples of main, parent, candidate, successor, and reviewed head. The host boundary received a further twenty 500 ms samples. Benchstat compares distributions; table values are medians. A printed p-value of zero means p<0.001.

| Case | First measured bad commit | Parent → candidate | Confidence and scope |
|---|---|---|---|
| Memory64 63 → 64 → 63 | `f9e7e91dd0332247c8355d70d6e66ac0cd493687` | 20.725 → 91.995 ns | High, p<0.001. Inline capacity fell to 63. Later commits removed repeated allocation and reduced this to about 35.5 ns. |
| CPU SIMD flag scanner | `0f9dd731dfccf63b716296e768a2f97a4053a6d2` | 170.2 → 207.95 ns | High for the measured transition, p<0.001. Non-monotonic; scanner source and normalized instructions are unchanged. |
| Host-capable loop, host0/n8 | `0f9dd731dfccf63b716296e768a2f97a4053a6d2` | 158.2 → 166.2 ns | High for the first transition, p<0.001 with 20 samples. The successor returns to 159.1 ns; reviewed head is 175.7 ns. The first commit does not explain the entire final 11% difference. |
| Prepared admission, 2000 functions | `e19356943fb9406142a15c9b2d9cefe49d9cd56b` | 14.879 → 15.616 µs | High for the measured transition, p<0.001. The three measured graph functions have unchanged source and normalized instructions. |
| Null externref write | `c04b30c976c3091ef0ab3790117a330954bc56f3` | 37.165 → 42.305 ns | High, p<0.001. This adds the required exact nullability check. Later PR changes already recover part of the cost. |

“First measured bad” is not proof of added work in that commit. The scanner and host results recover and regress again across later commits. The source and assembly checks below distinguish a correctness-check cost from a binary-placement effect.

## Memory representation

[The memory capacity report](memory64-importer-capacity.md) contains the full serial and parallel count matrix, red/green tests, allocation counts, and bit layout. The follow-up restores an eight-bit memory64 importer count by combining flags that always become known together and encoding maximum presence as zero versus maximum+1. All legal maxima remain inline. The three distinct shared-memory properties remain separate. `Memory` is 16 bytes and `memoryState` is 24 bytes.

## Scanner and prepared admission

`simdCPUFlagsSupported` scans CPU flag text; it does not scan Wasm instructions. Standard Go AMD64 builds use CPUID/XGETBV. The text path is used by TinyGo AMD64 behind `sync.Once`. The Go microbenchmark does not establish the performance of TinyGo-generated code.

The scanner has 169 instructions and 597 bytes on both revisions after relocation normalization. Constants and relative branch destinations are preserved in the comparison. There is no added helper, bounds check, load, or branch, and no loss of inlining: its inlining cost remains 220. The main function address is 0 modulo 64; the reviewed address is 32 modulo 64. Across the sampled history, the slower results track the latter placement, including reversals without scanner source changes. This is strong evidence of placement sensitivity, but does not identify a single hardware front-end mechanism.

The prepared benchmark constructs the compiler's bounded admission graph; it is not a lookup on every host call. `resolveBoundedPreparedEntries`, `boundedPreparedTableSummary`, and `immutablePreparedTableTargets` remain identical after relocation normalization: respectively 309/78/371 instructions and 1542/242/1470 bytes. Their inlining costs remain 539/95/545. Allocation count remains four per graph construction. Function placement changes. The first bad commit also removes code in Wasm validation, but does not change these graph functions. No new cache, map, or lookup fast path is justified by this evidence.

## Host boundary

The representative case is `HostRoundtripLoop/mem0/parallelfalse/host0/n8`. It enters a module that can call the host, but this parameter choice performs zero callbacks. It must not be described as the measured cost of a host dispatch.

The exact guest fixture emits identical 475-byte, 122-instruction native code on main, reviewed head, and final revision. All three revisions have 18 push/pop instructions and the same two call instructions. Public entry offset is zero; internal entry offset is 21; native frame size is 40 bytes. Identical bytes mean no added guest branches, loads, stores, spills, reloads, or register saves. The fixture contains no shift instructions. Go-side `Invoke` (28 instructions) and `invokeEntry` (683 instructions) are also identical after relocation normalization.

A smaller external benchmark binary using the same guest fixture reverses the short-loop result: 170.3 → 155.8 ns (-8.51%). Actual host1/n8 is unchanged there, while host1/n1024 increases 2.57%. This further supports binary-placement sensitivity rather than a fixed extra host-boundary operation. No safety check or register preservation was removed to change these numbers.

## Global write change

Commit `5fda2109069801bdd54ea1714f8db5b8ec1a9915` takes a pointer to the immutable execution snapshot's global descriptor instead of copying the 88-byte descriptor. The invocation lease and native-state lock are unchanged. All exact-type/nullability checks remain in their original form. The generated Go frame falls from 704 to 632 bytes; the descriptor-copy loads/stores disappear. There is no added field, heap allocation, metadata preparation, or compiler work.

An isolated alternative precomputed `rejectsNull` during metadata snapshot construction, using a byte in existing descriptor padding. It passed the global and snapshot tests and kept the descriptor size, but did not show a significant improvement over the pointer-only change in ten alternating 500 ms samples. Null externref was 37.03 versus 36.93 ns (p=.469); i32 and null funcref also showed no significant gain. The extra cached flag and preparation logic were therefore not retained.

The new test covers nullable/non-nullable externref and funcref through both direct and exported owner-backed paths. A rejected null write must retain the prior non-null value. Nullable cases also pass through public artifact serialization/loading. The existing non-null GC-reference test remains. These tests passed before the pointer change and after it: this is a performance change with unchanged required behavior.

## Scope of the broader sweep

The preceding sweep screened 1770 matched cases and confirmed 276 selected cases. The initial compiler slowdown did not reproduce: the single-CPU large compiler case was 31.8525 → 31.8377 ms, p=.853, with 78 allocations on both revisions; the eight-CPU case was 11.50 → 11.53 ms, p=.280, with 147 allocations. Small repeatable corpus compile differences remained (json-as +2.21%, matmul +1.84%). Fixed-iteration validation showed 352 decoder allocations on both revisions; a preliminary one-allocation difference was benchmark calibration, not a new allocation.

The bulk table copy0 benchmark hung in isolated runs on both revisions and was excluded. The V128 indirect-host-call benchmark is unsupported on both. The cancellation-latency benchmark timed out on main with one worker; eight-worker runs passed on both without a regression. These exclusions are not treated as successful measurements.

## Overflow contention experiment

An isolated eight-table prototype used an address hash to choose one of eight mutex-protected typed maps. This did not change `Memory` or `memoryState`, but grew static table metadata from 16 to 128 bytes and required additional lazy map storage. It retained zero allocations for warmed updates.

Ten alternating 300 ms samples found serial steady overflow access 12–14% slower (about 16.4 → 18.5 ns). The 254 → 255 → 254 transition grew 8%, and 255 → 256 → 255 grew 16%. Parallel medians improved, but dispersion was large: 254 improved 88.46 → 55.16 ns (p<0.001), 255 improved 110.10 → 65.89 ns (p=.022), and 256/1024 did not establish a significant gain. The address-based distribution and adjacent locks make this sensitive to object placement. A fresh local-table allocation probe measured one key at 208 versus 320 B/op (three allocations each), and eight keys at 208 versus 1664 B/op (three versus seventeen allocations). This probe includes the local table wrapper; production uses a static wrapper. The additional maps still have a real cold memory cost. The prototype was not retained. More shards, cache-line padding, or another indirection would spend more memory to address a cold path.

The selected layout removes the overflow lock entirely for memory64 counts below 255 and for all memory32 counts. Memory64 counts of 255 or more still share the overflow mutex. This is a remaining tradeoff, not a claim of lock-free lifecycle management or zero cold allocation.

## Validation and reproduction

The memory-capacity change and the direct global descriptor change passed these commands:

```sh
go test ./src/wago -run 'Memory|Shared' -count=1
go test ./src/wago -run 'TestGlobalNullabilityDirectAndExported|TestReviewNonNullGCGlobalSetNull' -count=1
go test ./src/wago -run 'Global' -count=1
go test ./...
go test -race ./src/wago ./src/core/runtime/gc/native -run 'Memory|Shared|Global|Canonical|TypeGrowth' -count=1
go test -tags wago_runtime ./cli/...
GOMAXPROCS=2 just test fuzz 5s
just test corpus quick
just lint
just docs
git diff --check
```

Full tests, runtime-tagged CLI tests, and corpus tests use Go 1.22.12 and TinyGo 0.41.1. Other checks and all benchmarks use Go 1.27.1. WABT 1.0.41 is first on PATH. The environment sets `GOFLAGS=-buildvcs=false`, `WAGO_SPEC_INTERPRETER_REVISION=9d36019973201a19f9c9ebb0f10828b2fe2374aa`, and `WAGO_SPEC_INTERPRETER` to that pinned interpreter's `wasm` executable. This matches the repository fixture requirements. `go test ./...` includes AMD64 encoder byte tests, Railshot nested-expression tests, IR verifier tests, GC tests, and the memory/global suites.

For benchmark reproduction, build each revision with `go test -c -o <binary> <package>`. Alternate the following command between revisions ten times, selecting the exact benchmark with anchored slash-separated patterns:

```sh
GOMAXPROCS=1 taskset -c 2 <binary> -test.run='^$' -test.bench='<pattern>' -test.benchmem -test.benchtime=500ms -test.cpu=1
benchstat main.txt before.txt final.txt
go test -c -gcflags=-m=2 -o diagnostics.test ./src/wago
go tool objdump -s '<function>' diagnostics.test
```

The case names are `Memory64ImporterCount/transition/63-64-63`, `SIMDCPUFlags/scanner`, `HostRoundtripLoop/mem0/parallelfalse/host0/n8`, and `SetGlobalValue/null_externref` in `./src/wago`; `ResolveBoundedPreparedEntriesTableFanout/functions=2000` is in `./src/core/compiler/backend/railshot/amd64`. Prefix names with `Benchmark` for `-test.bench`. The scripts, raw samples, disassembly, and validation logs are retained in `/tmp/wago-perf-investigation`; the earlier broad sweep is in `/tmp/wago-main-sweep`.

The [CI run for the final code revision](https://github.com/wago-org/wago/actions/runs/35535066592) passed the native Linux AMD64/ARM64, Windows AMD64/ARM64, and Darwin AMD64/ARM64 jobs. The [regression-stress run](https://github.com/wago-org/wago/actions/runs/35535066613) also passed its AMD64/ARM64 explicit and guard-page GC jobs. Platform correctness checks are separate from the Linux AMD64 timing claims above.
