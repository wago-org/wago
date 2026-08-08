# WasmGC measurement matrix

## Compiler type-metadata matrix

Compiler-side WasmGC metadata is measured separately from collector behavior.
`src/core/compiler/wasm/type_rep_bench_test.go` covers construction, semantic
equality, decode, validation, and cold/warm structural identity at 10, 100,
1,000, and 10,000 types and at 1, 4, 16, and 64 fields per type where the
operation permits. Fixtures include numeric, vector, packed, mutable, abstract,
indexed, recursive, exact, nullable, and shorthand reference forms. Fixture
construction is outside timed decode, validation, equality, and identity loops.

Record a baseline before changing the representation, then repeat the exact
command and compare the raw files:

```sh
go test ./src/core/compiler/wasm -run '^$' \
  -bench 'BenchmarkGCType' -benchmem -benchtime=100ms -count=10

cd bench
go test -run '^$' \
  -bench 'Benchmark(Decode|Validate|Compile)$' -benchmem -count=10
```

`TestCompilerTypeRepresentationLayout` is the deterministic layout contract.
It records both size and Go-pointer containment for compiler leaf metadata and
the separately owned runtime collector descriptors. Keep the two result sets
separate: denser compiler values do not by themselves prove a collector heap or
pause-time improvement.

This document defines the measurement contract for collector changes tracked by
issue #300. The opt-in recorder, public API, JSONL schema, phase semantics, and
footprint measurements are documented in
[`docs/gc-telemetry.md`](gc-telemetry.md). The matrix is intentionally broader than the current implementation:
it covers the Throughput and Tiny collectors that exist today and the collector
directions explicitly recorded in issues #302–#321.

The benchmark source is `src/core/runtime/gc/matrix_bench_test.go`. Benchmark
names and custom metric names are stable machine-facing identifiers. Change them
only when the represented workload or unit changes.

## Measurement principles

Every result must keep mutator, collector, compiler, and operating-system costs
distinct. A lower pause is not a win if it comes from substantially more mutator
barrier work, CPU, memory, or generated code.

Report at least:

- mutator wall time and throughput;
- collection-cycle and individual-step p50, p90, p95, p99, and maximum latency;
- total collector CPU and phase time;
- objects, payload bytes, and reference slots visited, copied, promoted, swept,
  or evacuated;
- managed live, committed, free, and reserved bytes;
- metadata, Go heap, executable JIT, and peak RSS bytes as separate domains;
- allocation rate, survival by age, full/minor frequency, and time to exhaustion;
- linked bytes, JIT bytes, compile time, B/op, and allocs/op where code generation
  participates; and
- a semantic checksum or expected trap for every timed fixture.

Percentiles are derived from a bounded histogram with 16 linear sub-buckets per
power-of-two nanosecond interval. Relative bucket width is at most 6.25%, and
measurement memory stays fixed even for long runs. A percentile is the upper
bound of its bucket; raw `ns/op` remains available for comparison tools.

Deterministic counters (objects, slots, bytes, roots, cards, search steps, and
semantic checksums) may be compared exactly. Time, CPU counters, page faults,
RSS, and cache/TLB behavior are host-specific and require repeated interleaved
A/B runs on an otherwise idle machine.

## Telemetry acceptance thresholds

Issue #300 uses these gates for the measurement layer itself:

- an ordinary build must add zero collector allocations and no timing, map, or
  interface-dispatch work to allocation, barrier, mark, scan, minor/full, or Tiny
  hot paths;
- the median of an interleaved disabled-build collector control must remain
  within 3% of the uninstrumented parent, with no B/op or allocs/op delta;
- the generated text size of each touched release hot-path function must not
  increase; a stripped minimal collector executable may grow by at most 8 KiB
  including file alignment, `Config` by at most 8 bytes, and `Collector` by at
  most 8 bytes;
- after warmup, an attached recorder must add zero Go allocations per collection
  and retain bounded state independent of cycle count; and
- hardware-counter runs must show no statistically significant disabled-build
  instruction or cache-miss increase over ten or more interleaved samples. Use a
  3% instruction threshold; treat cache misses as inconclusive unless confidence
  intervals separate because low-count cache events are noisy.

The release binary meets the deterministic gates below. The August 8, 2026 host
had no `perf` executable, so the hardware-counter command remains a required CI
or reviewer-host check rather than a fabricated local result.

## Collector-level matrix

`BenchmarkGCCollectionMatrix` crosses:

- collector: Throughput minor, Throughput full, and Tiny full;
- survival: 0%, 1%, 10%, 50%, and 90%; and
- layout: pointer-free, sparse-reference struct, dense-reference struct, and
  dense-reference array.

Allocation and cleanup occur outside the benchmark timer. Each operation checks
the exact live-object count and every surviving object's type before cleanup.
Reference-bearing fixtures store non-null self edges in every traced slot and
validate them after collection; they therefore exercise traversal rather than
only iterating null slots.
This matrix is the primary discriminator for tracing density, promotion cost,
zero-survivor cleanup, and future age-based tenuring.

`BenchmarkGCSparseRememberedArray` crosses 4K and 256K-element old arrays,
128/256/512-byte cards, and either two distant writes or writes to every slot.
Both densities allocate exactly two nursery objects per operation, so the
comparison changes dirty coverage without changing dynamic allocation rate or
survivor count. Deterministic `card-slots-scanned/op` proves sparse work is
bounded by dirty cards while the dense case still reports every reference slot.

`BenchmarkGCDirtyPersistentRoots` holds one nursery global dirty while scaling
the collector-owned global directory through 1, 64, and 4,096 slots. With
`wago_gcstats`, every case must report exactly one dirty card and one global-root
visit per minor cycle.

`BenchmarkGCRootClassMatrix` crosses Throughput and Tiny with 1, 64, and 4,096
native-frame, global, table, public-token, foreign-instance, and snapshot-
temporary roots. Runtime frame walkers preserve those classes through an
allocation-free classified direct-root interface; public and cross-instance
collector slots retain their exact class across telemetry resets.

`BenchmarkGCTinyStepMatrix` scans reference arrays with 16, 4K, and 64K slots.
Today one Tiny step may scan the whole object. A slot/byte-budgeted implementation
must keep p99 and maximum step time bounded while increasing `steps/op` with the
amount of scan work.

Run the collection matrix with `-tags wago_gcstats` to add cumulative collector
phase, root, trace, promotion, and card metrics to ordinary Go benchmark output.
`BenchmarkCollectorTelemetryOverhead` keeps the empty disabled/enabled control
visible. `BenchmarkGCStaticSiteCompilation` compares one and 4,096 static GC
allocation sites at the backend layer; `BenchmarkGCStaticSiteExecution` executes
4,096 allocations through one hot site or one allocation through each of 4,096
sparse sites at the product layer. Every case retains semantic checks.

On the August 8, 2026 Ryzen 7 8845HS host, ten pinned one-iteration compilation
samples put the one-site fixture at a 0.063 ms median (0.043-0.190 ms), about
41,208 B/op and 80 allocs/op, producing 146 native bytes with 106 bytes attributed
to allocation. The 4,096-site fixture had a 19.77 ms median (19.17-19.87 ms),
4,956,936 B/op and 28,778 allocs/op, producing 589,826 native bytes with 434,176
allocation-attributed bytes. Ten pinned 10-operation execution samples put 4,096
allocations through one hot site at a 0.979 ms median and one allocation through
each sparse site at 1.091 ms; both had 4,096 Go allocation paths, zero minor
cycles, 533,396 B/op, and 8 allocs/op. The execution difference is below a
pre-declared optimization threshold and is baseline evidence, not a reason to
special-case static sites.

### Issue #300 baseline report

The following pinned linux/amd64 results use Go 1.24.4, Linux 6.12, one Ryzen 7
8845HS core, `wago_gcstats`, ten process samples, and 500 collection cycles per
sample. Values are medians across the ten bounded-histogram summaries; maximum is
the median of each sample's exact maximum. Fixture allocation and semantic
validation are outside the pause timer.

| Collector case (100 objects) | ns/op | p50 ns | p90 ns | p95 ns | p99 ns | max ns |
|---|---:|---:|---:|---:|---:|---:|
| Throughput minor, pointer-free, 0% survival | 2,613 | 2,431 | 2,687 | 3,327 | 4,863 | 26,139 |
| Throughput minor, pointer-free, 90% survival | 18,362 | 18,431 | 19,455 | 22,527 | 26,623 | 51,617 |
| Throughput minor, dense array, 0% survival | 2,647 | 2,559 | 2,815 | 3,071 | 4,351 | 8,096 |
| Throughput minor, dense array, 90% survival | 22,718 | 22,527 | 24,575 | 27,647 | 32,767 | 46,367 |
| Throughput full, dense array, 90% survival | 15,375 | 14,847 | 16,383 | 18,431 | 22,527 | 40,396 |
| Tiny full, dense array, 90% survival | 17,949 | 17,407 | 18,431 | 22,527 | 24,575 | 51,227 |

Deterministic telemetry matched each fixture exactly. The 90%-survival dense
minor visited 90 objects and 1,440 reference slots and promoted 7,200 bytes per
operation. The corresponding Throughput and Tiny full cases visited the same 90
objects and 1,440 slots with zero promotion. Phase medians for the dense minor
were about 7.88 us reference scanning and 7.96 us promotion/copy; the Throughput
dense full spent about 8.23 us reference scanning and 0.46 us sweeping. These
counters, not small host-time differences, are the primary A/B invariant.

Ten pinned 20-operation giant-array samples produced:

| 256K-element old array | young allocations/op | scanned slots/op | median ns/op | p50 ns | p90 ns | p95 ns | p99 ns |
|---|---:|---:|---:|---:|---:|---:|---:|
| Two distant writes | 2 | 262,144 | 627,322 | 622,591 | 753,663 | 776,499 | 796,076 |
| Every slot written | 2 | 262,144 | 834,228 | 819,199 | 917,503 | 950,271 | 1,033,180 |

### Issue #303 card-driven minor collection

Issue #303 replaces the baseline whole-object input with exact transient roots,
dirty persistent slots, and linked fixed-card ranges. Ten pinned 20-operation
samples on the same August 8, 2026 Ryzen 7 8845HS host, with the measured
128-byte default, produced:

| 256K-element old array | young allocations/op | scanned slots/op | median ns/op | p50 ns | p90 ns | p95 ns | p99 ns |
|---|---:|---:|---:|---:|---:|---:|---:|
| Two distant writes | 2 | 64 | 2,475 | 2,431 | 2,559 | 2,661 | 2,806 |
| Every slot written | 2 | 262,144 | 858,957 | 873,216 | 873,216 | 873,216 | 873,216 |

The sparse fixture visits 4,096 times fewer slots and its median pause is about
253 times lower than the #300 baseline. The dense fixture continues to visit all
262,144 slots and is 2.96% slower than baseline, inside the pre-declared 3%
dense-work budget rather than hiding the scan. Five pinned 10-operation samples
selected the default card size:

| Card bytes | sparse scanned slots/op | sparse median ns/op | dense scanned slots/op | dense median ns/op |
|---:|---:|---:|---:|---:|
| 128 | 64 | 2,696 | 262,144 | 867,680 |
| 256 | 128 | 2,813 | 262,144 | 869,981 |
| 512 | 256 | 3,015 | 262,144 | 872,647 |

The implementation stores one 16-byte linked range per disjoint dirty region,
not one metadata entry per card, so selecting 128 bytes does not multiply dense
range metadata. Adjacent writes coalesce; repeated writes to an already covered
card only increment the bounded duplicate-dirty counter.

Interleaved release barrier controls remain allocation-free. Nursery-parent
struct stores change from 25.57 to 25.14 ns/op; old-parent/old-child stores from
27.61 to 27.81 ns/op; newly exact old-struct/young-child same-card stores from
28.18 to 29.10 ns/op; existing old-array same-card stores from 36.82 to
35.51 ns/op; and persistent-root repeated dirties from 9.94 to 7.03 ns/op.
Generated code for the two-store old-array fixture changes from 1,864 to 1,856
bytes while barrier attribution remains 404 bytes.

On linux/amd64, `Config` remains 64 bytes, `handleEntry` remains 20 bytes,
`objectCard` grows from 12 to 16 bytes, and `Collector` grows from 1,008 to 1,064
bytes. A minimal executable that performs an old-array store and minor
collection changes from 2,536,460 to 2,561,068 unstripped bytes (+24,608) and
from 1,659,064 to 1,675,448 stripped bytes (+16,384, including file alignment).
After warmup, the direct card scan and repeated barrier paths remain zero-allocation.

At 4,096 roots, Throughput median full-pause p99 ranged from about 51 us for
public-token/snapshot roots and 57 us for foreign-instance roots to 410 us for
collector globals and 377 us for collector tables. Native-frame p99 was about
70 us. Exact classified counts were 4,096 per Throughput cycle; Tiny reports
8,192 visits because it enumerates roots during both initial mark and remark.

Tiny's 65,536-reference-array step fixture still completes in six steps, but one
step scans the complete object. Across ten pinned 100-cycle samples, median step
p50 was 383 ns, p90/p95 360 us, p99 377 us, and exact maximum 422 us. This is the
baseline that #319's slot/byte-budgeted scanner must flatten while increasing
`steps/op`.

For disabled-build overhead, twenty pinned interleaved 10,000-operation runs of
the zero-survivor Throughput minor control measured an 811.95 ns parent median
and an 814.30 ns current median (+0.29%), with identical 40 B/op and 2 allocs/op
from fixture construction. The collector's direct `AllocsPerRun` control remains
0. Release builds emit the same text byte sizes as the parent for `alloc`,
`CollectFull`, `CollectMinor`, `markRoots`, `markNurseryRoots`, `scanObjectRefs`,
`addObjectCardRange`, and `tinyAlloc`; no telemetry call, clock read, map, or
interface dispatch remains in those functions. Hardware instruction/cache
counters were not collected because `perf` was unavailable on this host; use the
command below before making a hardware-counter claim.

`BenchmarkThroughputFreshBump`, `BenchmarkThroughputCommonSpanReuse`,
`BenchmarkThroughputLargeSpanMiss`, `BenchmarkThroughputLargeSpanChurn`, and
`BenchmarkThroughputRandomFragmentation` cover issue #312's allocator tradeoff.
Every freed old/large span now enters one reusable arena-backed AVL address
index, regardless of its original size class. Subtree maximums provide a
leftmost first-fit query, exact `largest-free`, free-byte, and span-count
summaries without rescanning. Adjacent class and large spans coalesce, top spans
rewind the bump, and a later class allocation can consume a span produced by a
large object or vice versa. Full collection queues dead old-space spans in a
bounded reusable pending arena; allocation incrementally indexes pending spans
only until it finds a fit, moving balancing/coalescing work out of the full-GC
pause without changing poisoning or heap limits. Promotion planning stable-sorts
multiple survivors by destination size class before reserving old-space spans.

`BenchmarkThroughputLargeSpanMiss` constructs 64, 1,024, and 16,384 fragmented
spans that are all too small. The augmented root rejects every impossible fit in
one search step. `BenchmarkThroughputLargeSpanChurn` repeatedly consumes and
returns the unique fitting span at the same fragmentation levels; always run it
with the miss benchmark so constant-time rejection cannot hide insertion or
coalescing work. `BenchmarkThroughputRandomFragmentation` warms a mixed-size
heap before timing and reports free spans, largest free bytes, and reusable node
metadata bytes.

On the Ryzen 7 8845HS host with Go 1.24.4 on linux/amd64, five 100 ms samples
changed the 16,384-span cold impossible miss from roughly 9.85-16.27 us to
1.68-1.96 ns with exactly one search step and zero allocations. The 16,384-span
largest-span churn changed from 12.56-14.31 us to 0.49-0.51 us; the 1,024-span
case changed from 0.84-1.04 us to 0.35-0.38 us. The intentionally exposed
64-span crossover changes from about 70-77 ns to 210-225 ns, and one-span common
reuse changes from about 19 ns to 29 ns. Fresh bump allocation remains about
7.7-8.1 ns and allocation-free. Use the randomized/end-to-end workloads rather
than the small fragmented crossover alone when deciding the tradeoff.

On 64-bit builds `throughputSpanNode` is 28 bytes, `throughputHeap` changes from
144 to 120 bytes, and `Collector` changes from 1,024 to 1,000 bytes on the
current main layout. A 16,384-span fixture retains 458,780 node-arena bytes; the
arena is reused after warmup and the timed churn remains 0 B/op and 0 allocs/op.
Handle, object, native ABI, and serialized layouts are unchanged. A minimal
linux/amd64 executable that constructs the Throughput collector and allocates a
1,024-element `i32` array changes from 2,492,486 to 2,518,202 unstripped bytes
(+25,716) and from 1,630,392 to 1,646,776 stripped bytes (+16,384). Do not use the
manager-only `cli/wago` binary for this comparison because its dependency graph
dead-strips the collector.

The current collector still marks by stable handle and therefore has no
independently sweepable page ownership from which to build correct lazy
page-mark, card, or object-start tables. This change deliberately does not add
unused per-page side metadata: deferred span indexing captures the measurable
pause win now, while page-local mark/card/object-start state remains part of a
future region/Immix generation design rather than speculative Throughput-heap
overhead.

`BenchmarkTinyAllocatorCommonSpanReuse`, `BenchmarkTinyAllocatorFragmentedFit`,
and `BenchmarkTinyAllocatorFragmentedMiss` isolate the compact Tiny span
allocator tracked by #318. Always run the common reuse case with the fragmented
cases: bounded misses must not hide work shifted into successful allocation or
free/coalescing. `CommonSpanReuse` also reports `metadata-bytes`, which counts
the boundary-tag, allocation-start, bin-head, and bin-occupancy backing storage;
the eight intrusive link bytes inside each free span remain part of the managed
heap rather than a separately retained metadata allocation.

On linux/amd64 with the default 64 KiB heap, allocator backing metadata changes
from 65,536 bytes (4,096 16-byte `tinyBlock` records) to 9,180 bytes, an 86.0%
reduction. A 2,048-span impossible fragmented allocation changes from about
2.29 us and 16 B/op to about 4.95 ns and 0 B/op. The stricter metadata and
coalescing work makes the isolated allocate/free/full-coalesce microbenchmark
about 39.5 ns instead of 9.8 ns; use end-to-end allocation workloads when
deciding whether that explicit footprint/fragmentation tradeoff is acceptable.
The minimal Tiny collector constructor executable changes from 2,473,626 to
2,479,440 unstripped bytes (+5,814) and from 1,642,658 to 1,646,754 stripped
bytes (+4,096, including file alignment). Directly retained Tiny initialization
text includes 1,049 bytes for `newTinyHeap`, 533 bytes for `insertFree`, and a
122-byte increase in `newTinyCollector`.

## Required companion layers

Collector microbenchmarks cannot observe every cost. Keep the following as
separate layers instead of folding unrelated setup into collector pause timing.

| Layer | Required dimensions |
|---|---|
| Collector | Survival, topology, pointer density, roots, cards, promotion/copy, sweep, fragmentation, Tiny steps |
| Runtime integration | Frames, globals, tables, public tokens, foreign instances, snapshot temporaries, exceptions, re-entry, cross-instance calls |
| JIT/compiler | One hot static site versus thousands of sparse sites; native-fast/medium/helper paths; barriers; spills; stubs; trap code; root-map bytes |
| Product | Compile/load/instantiate time, Go compiler/runtime heaps, managed heap, executable mappings, linked binary, snapshots, RSS |

Future issue work must extend the matrix as follows:

- #302: no-survivor and low-survivor cleanup with high handle counts;
- #315: extend the completed 128/256/512-byte card matrix to any new reference
  bulk operations or region-local card representations;
- #304: 64, 65, 128, 256, and 1,024 exact roots plus metadata bytes/safepoint;
- #308: reserved/committed bytes, backing growth, bytes copied, and page faults;
- #309: metadata bytes/object, handle-resolution instructions, and cache misses;
- #310: object lifetimes of zero through five minors, adaptive thresholds,
  survivor occupancy, copied/promoted bytes, and full-GC frequency;
- #311: numeric/reference arrays at lengths 0, 1, 4, 32, 256, and 4,096 plus
  handle/chunk refill and cancellation paths;
- #313: occupancy histograms, mark bandwidth, recyclable regions, and selective
  evacuation build on #312's randomized fragmentation and indexed large spans;
- #318/#319: Tiny metadata, size-bin search, giant objects, root churn, mutation
  during every phase, allocation debt, cycle latency, and minimum mutator
  utilization; and
- #321: one, two, four, and available-core collectors only after its prerequisites
  land, including synchronization and total CPU rather than wall time alone.

## Running the matrix

Do not run the complete matrix casually on a busy development machine. First
compile without executing benchmarks:

```sh
go test ./src/core/runtime/gc -run '^$'
go test -tags wago_gcstats ./src/core/runtime/gc -run '^$'
```

Ordinary `TestGC*WorkloadSmoke` tests execute one correctness cycle through
every benchmark family, including the largest sparse array and Tiny scan, without
invoking Go's adaptive benchmark runner.

To validate timing plumbing cheaply, select exact sub-benchmarks and force a
fixed iteration count. For example:

```sh
go test ./src/core/runtime/gc -run '^$' \
  -bench '^BenchmarkGCCollectionMatrix/throughput-minor/dense-array-refs/survival=50$' \
  -benchtime=2x
```

For a controlled run, disable frequency scaling/turbo when practical, pin the
process to one physical CPU, record the CPU/kernel/Go revision, and run multiple
interleaved samples:

```sh
taskset -c 2 go test -tags wago_gcstats ./src/core/runtime/gc \
  -run '^$' -bench '^BenchmarkGC' -benchmem -count=10 -timeout=30m \
  | tee gc-matrix.txt
```

Go's JSON event stream can be retained alongside ordinary benchmark text:

```sh
taskset -c 2 go test -tags wago_gcstats ./src/core/runtime/gc \
  -run '^$' -bench '^BenchmarkGC' -benchmem -count=10 -timeout=30m -json \
  > gc-matrix.json
```

Compare ordinary output with `benchstat`. Preserve raw files; summaries alone
discard run order and outliers.

On Linux, collect hardware counters in separate runs because `perf` changes the
measurement environment:

```sh
perf stat -r 10 -e \
instructions,cycles,branches,branch-misses,L1-dcache-loads,L1-dcache-load-misses,\
LLC-loads,LLC-load-misses,iTLB-loads,iTLB-load-misses,dTLB-loads,dTLB-load-misses,\
minor-faults,major-faults \
taskset -c 2 go test ./src/core/runtime/gc \
  -run '^$' -bench '^BenchmarkGCCollectionMatrix$' -benchtime=1x
```

Use native hardware for architecture claims. QEMU results are correctness data,
not ARM64 performance data.

## Descriptor lowering allocation

`BenchmarkGCTypeLowering` isolates construction of runtime descriptors from
decoded Wasm GC types. Compiler lowering fills `FieldDesc` storage directly via
`gc.StructDescBuilder`; it does not first allocate a temporary
`[]gc.StorageKind`. This matters only when Go would heap-allocate the temporary:
on a Ryzen 7 7800X3D with Go 1.26.5, the 10-type/64-field case changed from
13 to 10 allocs/op (-23.1%) and from 8,224 to 8,032 B/op (-2.3%), with time
changing from 3.603 to 3.551 us/op (-1.4%). Across the four benchmark shapes,
time was neutral (-0.02% geomean); smaller field arrays already remained on the
Go stack.

## Research basis

The matrix follows primary and official sources rather than one aggregate
throughput score:

- Blackburn and McKinley's Immix work measures collection efficiency, space
  efficiency, mutator locality, fragmentation, line/block occupancy, and
  opportunistic evacuation: <https://doi.org/10.1145/1375581.1375586>.
- Bacon, Cheng, and Rajan's Metronome work treats pause bounds, pointer density,
  object size, fragmentation, heap overhead, and minimum mutator utilization as
  coupled quantities: <https://research.ibm.com/publications/controlling-fragmentation-and-space-consumption-in-the-metronome-a-real-time-garbage-collector-for-java>.
- Zhao, Blackburn, and McKinley's LXR evaluation reports tail latency together
  with throughput and the costs of remembered sets, copying, barriers, and
  fragmentation: <https://arxiv.org/abs/2210.17175>.
- Cai et al.'s distilled-cost methodology separates collector cost from the
  application's intrinsic allocation/reclamation work:
  <https://arxiv.org/abs/2112.07880>.
- Go's official runtime metrics separate GC CPU classes, pause distributions,
  live/goal heap bytes, and scannable heap/stack/global bytes:
  <https://pkg.go.dev/runtime/metrics>.
- G1's official tuning documentation exposes root scanning, remembered-set
  merging, heap-root scanning, object copying, survivor aging, and region/card
  behavior as distinct phases:
  <https://docs.oracle.com/en/java/javase/18/gctuning/garbage-first-g1-garbage-collector1.html>.

These sources are guidance for what to measure, not targets for copying JVM heap
policy into a small no-cgo Wasm runtime.
