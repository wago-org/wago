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
issue #300. The matrix is intentionally broader than the current implementation:
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

`BenchmarkGCSparseRememberedArray` crosses 4K and 256K-element old arrays with
two distant young writes or 1,024 distributed writes. It is deliberately hostile
to the current whole-object remembered scan. Future card implementations should
make the two-write case proportional to dirty regions without hiding dense-scan
costs.

`BenchmarkGCRootClassMatrix` crosses Throughput and Tiny with 1, 64, and 4,096
direct, global, or table roots. It isolates root enumeration that the collector
owns. Frame, token, foreign-instance, exception, and snapshot-temporary roots
remain runtime-integration fixtures because collapsing them into generic slots
would hide their actual traversal and synchronization costs.

`BenchmarkGCTinyStepMatrix` scans reference arrays with 16, 4K, and 64K slots.
Today one Tiny step may scan the whole object. A slot/byte-budgeted implementation
must keep p99 and maximum step time bounded while increasing `steps/op` with the
amount of scan work.

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
- #303/#315: 128/256/512-byte cards, sparse/dense writes, root cards, and all
  reference bulk operations;
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
taskset -c 2 go test ./src/core/runtime/gc \
  -run '^$' -bench '^BenchmarkGC' -benchmem -count=10 -timeout=30m \
  | tee gc-matrix.txt
```

Go's JSON event stream can be retained alongside ordinary benchmark text:

```sh
taskset -c 2 go test ./src/core/runtime/gc \
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
