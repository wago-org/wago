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

## Structured facts and late-barrier qualification

Issues #314 and #315 add bounded compiler facts and a guarded no-barrier bulk
path. Qualify them separately from collector throughput so compile-time wins do not
hide runtime or code-size regressions.

Correctness and allocation checks:

```sh
go test ./src/core/compiler/backend/railshot/shared
go test ./src/core/compiler/backend/railshot/amd64
go test ./src/core/runtime/gc/...
go test -tags wagodebug ./src/core/runtime/gc/...
go test -tags wago_gcstats ./src/core/runtime/gc/...
go test -race ./src/core/runtime/gc/...
```

The authoritative official Core 3 qualification additionally requires a Release 3
interpreter through `WAGO_SPEC_INTERPRETER`; an older WABT that rejects `rec`, packed
storage, or current reference syntax is an environment block, not a passing result.
Record the exact unavailable tool/version rather than silently dropping those gates.

Permanent microbenchmarks:

```sh
go test ./src/wago -run '^$' \
  -bench '^BenchmarkGC(RefCastInstruction|StructGetInstruction|RefCastNonFinalInstruction|StructGetNonFinalInstruction|InstructionLoopControl)$' \
  -benchmem -benchtime=500ms -count=10 -cpu=1
go test ./src/core/runtime/gc/native -run '^$' \
  -bench '^BenchmarkArrayBulk/(reference-fill|reference-fill-no-barrier)-(16|256|4096)$' \
  -benchmem -count=10
go test ./src/core/runtime/gc/native -run '^$' \
  -bench '^BenchmarkGCSubtypeInterval$' -benchmem -count=10
go test ./src/core/runtime/gc/native -run '^$' \
  -bench '^BenchmarkArrayDeferredReferenceBatch$' -benchmem -count=10 -cpu=1
go test ./src/core/runtime/gc/native -run '^$' \
  -bench '^BenchmarkGCBarrierStateMatrix$' -benchmem -count=10 -cpu=1
```

The focused instruction benchmarks perform one host invocation and execute `b.N`
guest-loop iterations. `BenchmarkGCRefCastInstruction` repeatedly casts a value
loaded from a mutable `eqref` global to one final struct type, which prevents exact
initializer facts from deleting the cast. `BenchmarkGCStructGetInstruction` reads
one mutable `i32` field from a retained final object. The matching `NonFinal`
benchmarks cast a proper subtype to an open supertype and access a subtype object
through that open struct declaration. Allocation occurs before the timed loop.
All GC results are dropped after the trap-capable instruction, and the
function returns its loop counter. `BenchmarkGCInstructionLoopControl` measures
the shared loop and counter-update floor. It is not an exact value to subtract because the two GC
instructions require different operand-loading work.

For a retained compiler/JIT result, also run the real Dew/Starshine workload and
record every `gc-barrier-*` state, `hostsync`/`gcnative` transition, generated GC
barrier/helper bytes, linked bytes, compile B/op and allocations, fresh and sustained
execution, and collector card/scanned-slot telemetry. The retired semantic-fact,
load-forwarding, and known-array-bounds paths are historical baselines rather than
alternate production policies. Barrier matrices must include nursery, remembered
old, unremembered old, large, and Tiny parents with null, i31, old, and young object
children. No barrier result is acceptable without forced collection and shadow-edge
verification after each write family.

A standalone generated workload can be retained outside the repository and measured
through:

```sh
WAGO_GC_OPT_WORKLOAD_WASM=/path/to/workload.wasm \
WAGO_GC_OPT_WORKLOAD_EXPORT=_start \
WAGO_GC_OPT_WORKLOAD_EXPECT=none \
go test ./src/wago -run '^$' -bench '^BenchmarkGCOptimizationWorkload$' \
  -benchmem -benchtime=100x -count=7 -cpu=1
```

Run interleaved processes with `WAGO_AMD64_NO_DEAD_GC_NEW=1` or
`WAGO_GC_SUBTYPE_INTERVALS=0` as relevant.
The benchmark requires a zero-argument export, uses deterministic no-op imports,
requires an exact comma-separated result vector (`none` for no results) on every
iteration, maintains a checksum only as a secondary anti-elision guard, and reports
linked, barrier, and helper bytes separately.

Initial candidate microbenchmarks on August 10, 2026 used linux/amd64, Ryzen 7
8845HS, Go 1.24.4, GOMAXPROCS=16, no affinity pinning, 200 ms benchtime. Five
fact-merge samples had a 4.642 ns/op median, 0 B/op, and 0 allocs/op. Three
null-reference fill samples measured median ordinary/no-barrier pairs of
37.43/33.80 ns at 16 elements, 53.96/51.39 ns at 256, and 158.2/155.9 ns at
4,096, all allocation-free.

The retired completion pass added bounded result-local load reuse and packed subtype
intervals. Its repeated `array.len` code-size fixture emitted 353 bytes with forwarding
versus 539 with the former `WAGO_AMD64_NO_GC_LOAD_FORWARDING=1` control. Three 200 ms subtype samples
are neutral at depth 1 (9.70/9.75 ns/op interval/parent median), improve depth 16
68.69→19.00 ns/op, and depth 256 1,113→18.12 ns/op, all allocation-free. A seven-
round `GOMAXPROCS=1`, 100-iteration MoonBit WasmGC JSON A/B measured facts at
188.752 µs/op median versus 191.401 µs/op disabled, both 208,779 B/op and 264
allocs/op. Code telemetry measured 294,341/294,181 linked bytes, 4,168/4,642 barrier
bytes, and 72,500/74,202 helper bytes for facts enabled/disabled. Timing is a modest,
host-noisy result; retain the deterministic code/work deltas and repeat on reviewer
hardware before claiming a broad speedup.

The August 10 tertiary review added constant-index known-length array get/set,
checked dead dynamic arrays, and a complete barrier-parent benchmark. The paired
constant-index set/get fixture emitted 1,029 bytes versus 1,084 with the former
`WAGO_AMD64_NO_GC_KNOWN_BOUNDS=1` control. Request-changes qualification then replaced the
size-only dead-array preflight with allocation reservation so occupied bounded heaps
retain allocation/exhaustion parity. Updated constructor-family bytes are 128/142 for
default, 164/178 for numeric uniform, and 200/214 for data arrays
(enabled/disabled); reference element construction remains 214/214, and a nested
default wrapper is 264/312 after retaining the inner compact result across the outer
allocation. The earlier 64/82/100-byte and 128/312 nested figures are superseded.
Successful dropped pointer-free uniform/data and default-initialized constructors now
retain allocation, handle, collection, and safepoint state while omitting unreachable
payload population;
reference-valued uniform/element constructors retain their complete edge/card path,
and oversized cases still trap before allocation. Three `GOMAXPROCS=1`, 200 ms deferred-reference-batch samples measured
prevalidated/revalidated medians of 438.2/452.7 ns at 16 elements, 6,560/6,740 ns at
256, and 104,405/108,576 ns at 4,096, all allocation-free. The matching barrier-state
matrix medians were 25.50 ns nursery, 34.03 remembered old, 41.16 unremembered old
including metadata creation, 34.28 large, and 28.50 Tiny, also 0 B/op and 0 allocs/op.

The August 11 adversarial qualification adds executable switch-parity oracles for
exception joins, abstract `any`/`eq` narrowing, mutable-field backedges, dirty host
i32 loop bases, memory64 non-versioning, and native root-plan call/allocation counts.
These are correctness gates rather than new speed claims. Memory64 loops and
candidate native-root-plan functions intentionally retain one checked body. The
reference-intermediate dead-tree hardening deliberately changes the compiler proof
fixture from three reservations to two reservations plus one full constructor, while
keeping three allocation helper calls: only the unsafe omitted reference payload is
restored. Tiny heaps of 56 bytes (struct intermediate) and 64 bytes (array
intermediate) now reproduce exact enabled/disabled exhaustion parity.

This document defines the measurement contract for collector changes tracked by
issue #300. The opt-in recorder, public API, JSONL schema, phase semantics, and
footprint measurements are documented in
[`docs/gc-telemetry.md`](gc-telemetry.md). The matrix is intentionally broader than the current implementation:
it covers the Throughput and Tiny collectors that exist today and the collector
directions explicitly recorded in issues #302–#321.

Native benchmarks moved to `gc/native`; use the original `gc` path for
pre-boundary historical checkouts. The benchmark source is `src/core/runtime/gc/native/matrix_bench_test.go`. Benchmark
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

`FuzzThroughputAllocatorAgainstIntervalModel` caps each mutated byte stream at
256 bytes because its independent interval oracle verifies the complete heap
after every operation and is deliberately superlinear under adversarial
fragmentation. This favors fuzzing breadth and bounded worker latency; the
32-seed deterministic model test retains 20,000-byte sequence depth.

## Benchmark review protocol

This section is the canonical repository policy for GC, runtime, compiler, and
JIT performance changes. Workload-specific sections below provide fixtures and
historical results; they do not weaken this review protocol.

### Correctness is the first gate

Do not interpret performance results until the relevant unit tests, forced
collection, shadow verification, failure injection, race tests, fuzz/model tests,
codec or snapshot tests, and available cross-architecture checks pass. Every
timed fixture must retain an observable semantic result: an exact value or
checksum, live-object count and type, expected trap, or required side-effect
ordering. Dead work that the compiler or runtime may eliminate is not evidence.

### Measure complete lifecycle costs

An isolated pause is not sufficient evidence when work is deferred. Report both
the pause and the pause plus the first operation forced to consume its debt. For
example, pair full GC alone with full GC plus the next promotion, immediate minor
collection, or refill; pair deferred sweeping with the first allocation that
reconciles it. Moving equal or greater cost onto the next mutator operation is
not automatically a win.

### Keep cost domains separate

Report Wasm mutator execution, collector work, Go runtime or host allocation,
compiler work, JIT executable memory, managed WasmGC memory, metadata, and RSS or
OS effects separately. Go `B/op` is not a proxy for managed-GC efficiency.
Compiler/JIT changes should also report generated native bytes, shared-stub and
per-site bytes, linked executable delta, compile time, compiler `B/op` and
`allocs/op`, runtime speed, and helper/native transition counts. A small runtime
win does not automatically justify extreme generated-code growth.

### Use repeated interleaved A/B measurements

Build baseline and candidate from named commits with the same Go toolchain,
build flags, tags, and environment. Pin both to the same CPU when practical, set
`GOMAXPROCS` explicitly where appropriate, and alternate baseline/candidate order
rather than exhausting one binary first. Use repeated samples and report medians
and distributions or confidence intervals, not one run. Use `benchstat` or an
equivalent comparison when available.

Record at least CPU, OS/kernel, Go version, `GOMAXPROCS`, affinity/pinning, sample
count, benchtime or fixed iteration count, relevant tags/environment variables,
baseline SHA, and candidate SHA.

### Treat noisy hosts honestly

Record unrelated CPU load, thermal instability, virtualization noise, and
unavailable counters. Preserve large deterministic effects, but mark narrow
timing differences as inconclusive and repeat them on a cleaner reviewer or CI
host before making a strong claim. Do not manufacture precision or omit an
unfavorable/noisy row.

### Prefer deterministic work counters to tiny timing movement

When available, exact objects and reference slots scanned, dirty/useful cards,
bytes copied/promoted, collection counts, allocator search steps, helper
transitions, native-fast paths, generated/linked bytes, allocation counts, and
semantic checksums are stronger evidence than a 1–3% timing movement. Record both
when timing and semantic work differ; neither should be hidden.

### Predeclare workload-specific budgets

State important hot-path noise and regression thresholds before tuning. Existing
3% gates apply only to the dense-card and telemetry controls that declared them;
there is no universal 3% rule. Acceptance budgets must match the workload and
must not be invented after seeing the result.

### Preserve unfavorable and rejected experiments

Retain intermediate regressions, their causes, follow-up measurements, and the
reason alternatives were rejected. Current architectural evidence includes the
initial survivor-cycle slowdown before its fast path, the remaining 64-span AVL
churn cost, the rejected free-span search cache that improved fragmentation but
badly hurt fresh bump allocation, and oversized native-array admission removed
after regressions. Later fixes do not erase those results.

### Measure fresh and sustained execution

Batch, chunk, and cache changes can shift cost between startup and warmed loops.
Report fresh Runtime/first invocation and warmed repeated execution when relevant,
as well as single-operation, small-batch, and sustained-batch shapes. Do not hide
startup cost behind long-running throughput.

### Cover density, size, and lifetime distributions

GC evidence should cross representative survival ratios (0%, 1%, 10%, 50%, and
90%), pointer-free, sparse-reference, dense struct-reference, and dense
array-reference layouts, small/medium/large objects, one-minor and multi-minor
lifetimes, and long-lived objects. Card/barrier changes require both sparse and
dense mutation. Compiler/JIT changes must distinguish one static site executed
many times from many static sites executed sparsely.

### Use summaries without hiding cells

Geometric means are useful matrix summaries, but report important individual
cells. A geomean win must not hide a catastrophic high-survival or one-shot
regression, code-size explosion, pathological small-heap behavior, or p99/maximum
pause regression.

### Hardware counters are optional evidence, never invented evidence

When `perf` or an equivalent is available, capture relevant instructions, cycles,
branches and misses, L1/LLC and I-cache behavior, TLB misses, and page faults.
When unavailable, say so and leave the gate for reviewer/CI hardware. Do not infer
exact cache behavior from wall-clock time alone.

### Retain reproducible raw evidence

Record exact commands and preserve raw benchmark output long enough to reproduce
`benchstat` or comparison tables. Prose tables summarize evidence; they are not
the original evidence.

### Reproducible optimization-review recipe

Start with correctness on the candidate checkout. Tags are independent products;
run all applicable combinations before benchmarking:

```sh
go test ./src/core/runtime/gc/...
go test -tags wagodebug ./src/core/runtime/gc/...
go test -tags wago_gcstats ./src/core/runtime/gc/...
go test -tags 'wagodebug wago_gcstats' ./src/core/runtime/gc/...
go test -race ./src/core/runtime/gc/...
```

Use a detached temporary worktree for the baseline so the active working tree is
not rewritten. Confirm that both worktrees resolve the same `go` executable and
version:

```sh
BEFORE_SHA=<reviewed-baseline-sha>
CANDIDATE_SHA=$(git rev-parse HEAD)
BEFORE_DIR=$(mktemp -d /tmp/wago-bench-before.XXXXXX)
git worktree add --detach "$BEFORE_DIR" "$BEFORE_SHA"
trap 'git worktree remove --force "$BEFORE_DIR"; rm -rf "$BEFORE_DIR"' EXIT

command -v go
go version
(cd "$BEFORE_DIR" && command -v go && go version)
uname -a
```

Choose an exact benchmark regex after listing the package's current benchmark
names. A normal collector/allocator review can use the following real fixtures:

```sh
REGEX='^(BenchmarkGCCollectionMatrix|BenchmarkGCSparseRememberedArray|BenchmarkGCReferenceStoreBarrier|BenchmarkGCArrayReferenceStoreBarrier|BenchmarkGCArrayReferenceStoreBarrierNonHeadCard|BenchmarkThroughputFreshBump|BenchmarkThroughputCommonSpanReuse|BenchmarkThroughputLargeSpanMiss|BenchmarkThroughputLargeSpanChurn|BenchmarkThroughputRandomFragmentation|BenchmarkThroughputFullOldHeapPause|BenchmarkThroughputFullOldHeapAmortized|BenchmarkThroughputFullOldHeapMinorAmortized)($|/)'
```

Capture alternating single-sample runs into raw files. Set `CPU_PREFIX=()` on
portable hosts; Linux reviewers may use `CPU_PREFIX=(taskset -c 2)` after choosing
an otherwise idle physical core:

```sh
: > /tmp/gc-before.txt
: > /tmp/gc-after.txt
CPU_PREFIX=()
# Optional on Linux: CPU_PREFIX=(taskset -c 2)

for i in $(seq 1 10); do
  if [ $((i % 2)) -eq 1 ]; then
    (cd "$BEFORE_DIR" && GOMAXPROCS=1 "${CPU_PREFIX[@]}" go test ./src/core/runtime/gc/native \
      -run '^$' -bench "$REGEX" -benchmem -benchtime=200ms -count=1) \
      >> /tmp/gc-before.txt
    GOMAXPROCS=1 "${CPU_PREFIX[@]}" go test ./src/core/runtime/gc/native \
      -run '^$' -bench "$REGEX" -benchmem -benchtime=200ms -count=1 \
      >> /tmp/gc-after.txt
  else
    GOMAXPROCS=1 "${CPU_PREFIX[@]}" go test ./src/core/runtime/gc/native \
      -run '^$' -bench "$REGEX" -benchmem -benchtime=200ms -count=1 \
      >> /tmp/gc-after.txt
    (cd "$BEFORE_DIR" && GOMAXPROCS=1 "${CPU_PREFIX[@]}" go test ./src/core/runtime/gc/native \
      -run '^$' -bench "$REGEX" -benchmem -benchtime=200ms -count=1) \
      >> /tmp/gc-before.txt
  fi
done

benchstat /tmp/gc-before.txt /tmp/gc-after.txt
```

Narrow the regex or use fixed `-benchtime=Nx` when a complete matrix would exceed
the review budget. Keep baseline/candidate commands identical. Linux CPU affinity
and hardware counters improve timing evidence but are not required for ordinary
correctness validation on other supported hosts.

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

### Issue #307 native-GC ABI/resolver attribution

`BenchmarkGCResolverCodeSize` is the permanent compiler/JIT attribution fixture for
immutable-ABI hoisting, the module-owned noncollecting resolver, low-site crossover,
and bounded resolved-address reuse. Run it together with the process-level execution
fixture:

```sh
go test ./src/core/compiler/backend/railshot/amd64 -run '^$' \
  -bench '^BenchmarkGCResolverCodeSize$' -benchmem -count=10

GOMAXPROCS=1 taskset -c 0 go test ./src/wago -run '^$' \
  -bench '^BenchmarkGCNativeResolverReuse$' -benchmem -benchtime=500ms -count=10
GOMAXPROCS=1 WAGO_AMD64_NO_GC_RESOLVE_REUSE=1 taskset -c 0 go test ./src/wago \
  -run '^$' -bench '^BenchmarkGCNativeResolverReuse$' -benchmem -benchtime=500ms -count=10
GOMAXPROCS=1 WAGO_AMD64_NO_GC_RESOLVE_REUSE=1 WAGO_AMD64_NO_GC_SHARED_STUBS=1 \
  taskset -c 0 go test ./src/wago -run '^$' \
  -bench '^BenchmarkGCNativeResolverReuse$' -benchmem -benchtime=500ms -count=10
```

On August 10, 2026, Ryzen 7 8845HS / Go 1.24.4 linux/amd64 code-size
qualification measured a 229-byte shared resolver body. One candidate site remains
inline at 351 native bytes because an unconditional island would produce 453 bytes.
With reuse disabled, eight sites use the shared form at 821 versus 1,669 bytes
(-50.8%); 128 sites use 7,301 versus 24,349 bytes (-70.0%). With reuse enabled, a
single-function module starts inline and switches to the shared resolver only when
lowering reaches a second actual resolution. This avoids the former whole-function
second compilation. The eight-site straight-line fixture therefore compiles once as
one resolution plus seven certified reuses, retaining its byte-identical 451-byte
code with no shared island. On September 4, 2026, an eight-distinct-object fixture
compiled as one inline resolution plus seven shared calls in 1,076 bytes, versus 948
bytes for the former two-attempt result and 1,798 bytes fully inline. Twelve
interleaved native Ryzen 7 7800X3D pairs reduced compile time from 34.38 to 21.79
microseconds (-36.64%), allocation from 30.52 to 28.97 KiB/op (-5.09%), and
allocations from 87 to 75 (-13.79%). The +128-byte focused code-size trade removes
the full retry while remaining roughly 40% smaller than fully inline resolution.
Module telemetry records shared body bytes/call sites, and per-function telemetry
records emitted versus reused resolutions.

A post-audit set of ten CPU-0-pinned 500 ms execution samples measured medians of
317.0 ns/op for the default inline+reuse path, 341.85 ns/op with reuse disabled, and
340.6 ns/op with both reuse and shared resolution disabled; all cases were 0 B/op and
0 allocs/op. Thus the retained path was 7.3% faster than shared-without-reuse and 6.9%
faster than fully inline resolution on this repeated-access fixture. A same-command
stripped `wago_runtime` TinyGo build was 2,096,928 bytes at the baseline SHA and
2,106,824 bytes after the tertiary audit (+9,896, +0.472%); the fixed 64-byte
compiled-code cache and runtime collector/instance/view layouts do not grow. This
fixture proves the intended dense straight-line case; it does not claim every static
shared-stub site is dynamically hot.

A September 4 follow-up of twelve CPU-0-pinned 500 ms pairs after removing the
single-function retry measured 430.4 versus 435.8 ns/op for the same repeated-access
native execution fixture (`p=0.124`), with 0 B/op and 0 allocs/op in both builds.
The generated code for that path is byte-identical; the result is an execution
neutrality check, not a new runtime-speed claim.

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
card only increment the bounded duplicate-dirty counter. Removed and coalesced
ranges reuse intrusive tombstone slots before arena growth. A helper hit on a
non-head range swaps its interval with the stable head interval while leaving
links and backing addresses unchanged, allowing the next same-card generated
store to remain native.

Interleaved release barrier controls remain allocation-free. Nursery-parent
struct stores change from 25.57 to 25.14 ns/op; old-parent/old-child stores from
27.61 to 27.81 ns/op; newly exact old-struct/young-child same-card stores from
28.18 to 29.10 ns/op; existing old-array same-card stores from 36.82 to
35.51 ns/op; and persistent-root repeated dirties from 9.94 to 7.03 ns/op.
Generated code for the two-store old-array fixture changes from 1,864 to 1,856
bytes while barrier attribution remains 404 bytes. In a Sunday, August 9, 2026
follow-up, fourteen alternating 750 ms samples of
`BenchmarkGCArrayReferenceStoreBarrierNonHeadCard` changed from a 77.99 ns/op
median to 64.83 ns/op (**-16.88%**, bootstrap 95% interval **-20.75% to
-12.86%**), with 0 B/op and 0 allocs/op. The companion AMD64 integration test
proves that the first repeated non-head store takes one mutation helper and the
next store uses the moved native head card without another helper transition.

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

Before #319's first stage, Tiny's 65,536-reference-array fixture completed in six
steps but one step scanned the complete object. In the same 100-cycle benchmark
shape on the Ryzen 7 8845HS, the three-run baseline median was 175.4 us/cycle,
196.6 us step-p99, 244.9 us maximum, and 6 steps/cycle. The resumable scanner now
uses 261 steps/cycle with a deterministic maximum of 256 entries, 256 reference
slots, and 1,024 payload bytes in any marking step. Five 100-cycle samples measure
a 213.2 us median cycle (+21.5%), 1.34 us step-p99, and 26.1 us observed maximum;
the remaining maximum is host scheduling/timing-harness noise rather than extra
scan work. The direct-root `BenchmarkTinyStepReferenceArray` reports a 521 ns
median at 65,536 elements, 0 B/op, and 0 allocs/op, while
`BenchmarkTinyCompleteCycleReferenceArray` reports 147.8 us/cycle and 261 steps.
The same direct benchmark reports 256 maximum entries/slots for 256, 4,096, and
65,536 elements, proving the per-step object work no longer scales with object
size.

An adversarial follow-up A/B initially found that routing ordinary Throughput
complete scans through finite cursor bookkeeping regressed dense full collection.
The release path therefore retains its direct complete descriptor loop, while
Tiny and diagnostic scans use the cursor primitive. Identical 200-iteration,
five-sample controls put current medians versus `main` at +0.75%/-4.66% for the
50%/90% dense-struct cells and +1.02%/+1.43% for the matching dense-array cells;
allocations and bytes/op remain identical.

The next #319 stage replaces Tiny's handle-wide color reset with a compact mark
epoch. Five one-second `GOMAXPROCS=1` samples of
`BenchmarkTinyCycleStartHandleReset` measured the following medians on the same
Ryzen 7 8845HS host:

| Live handle entries | Handle-reset baseline | Epoch start | Change |
|---:|---:|---:|---:|
| 256 | 237.5 ns | 4.341 ns | -98.17% |
| 4,096 | 3.698 us | 4.223 ns | -99.89% |
| 65,536 | 60.959 us | 4.510 ns | -99.99% |

Every case remains 0 B/op and 0 allocs/op, and the epoch path is independent of
handle count. Ten CPU-affined, single-P, interleaved 300 ms controls at 4,096
reference-array elements measured a 428.05 ns baseline versus 435.25 ns current
median for an individual bounded Step (+1.68%), while complete cycles changed
from 9.0085 us to 9.0355 us (+0.30%). Median paired deltas were -0.13% and +0.19%
respectively. Per-handle mark metadata remains one byte and the compact Tiny
scan/collector structures retain their previous sizes. The final stripped
linux/amd64 size card reports no aligned-file change for runtime-standard,
runtime-minimal -4.0 KiB, and runtime-minimal-tiny +0.1 KiB versus `main`; all
four release profiles remain within budget.

Adversarial follow-up found two hot-path hazards in the initial epoch factoring.
First, shared handle growth performed Tiny encoding even for Throughput. A
CPU-affined, pre-grown 100,000-handle control measured 4.939 ns on `main`, 6.745
ns before the correction (+36.6%), and 4.879 ns after it (-1.2%), all at 0 B/op
and 0 allocs/op. Second, inlining white-child shading into the active Tiny barrier
inflated the marked-child fast path. The retained barrier keeps white-child work
in one cold noinline helper. Ten CPU-affined interleaved samples of
`BenchmarkTinyIncrementalBarrierMarkedChild` measure 3.3055 ns on `main` and
3.347 ns current (+1.26%; paired median +2.34%), again with no allocations.
The final #319 barrier comparison isolates the minimum marked-edge work at
2.45-2.56 ns for retained incremental update and 1.98-2.03 ns for an SATB
old-edge test, both allocation-free. SATB was not retained: it would require an
additional pre-store payload load on every scalar overwrite, old-edge traversal
for every bulk overwrite, and more barrier/product code while transient roots
already use an atomic bounded snapshot. The retained incremental-update policy
instead removes the actual latency hazard by pausing sweep and queueing ordinary
bounded mark work rather than synchronously draining a graph.

The persistent-root cursor stage adds `BenchmarkTinyStepPersistentRoots`. Five
samples on the same host measured 520-526 ns for one 256-slot step at 256 and
65,536 total slots; the 4,096-slot control was 520-550 ns. Every case reports
0 B/op and 0 allocs/op, demonstrating that step work scales with the fixed chunk
rather than total persistent-root count. `BenchmarkTinyStepSweepBlack` measures
one 64-handle survivor sweep at 188 ns for 64 handles, 191-193 ns for 4,096
handles, and 174-177 ns for 65,536 handles, all with 0 B/op and 0 allocs/op.
Sweep work is capped at 64 handles and 256 blocks; oversized debug-poison spans
resume separately. `BenchmarkTinyAllocationDebtStep` measures one debt-purchased
rootless cycle-start step at 16.4-17.1 ns, 0 B/op, and 0 allocs/op. Ordinary
pacing buys one step per 1,024 allocated physical bytes and near-exhaustion work
is capped at 32 steps per allocation. The `wago_tiny_nonincremental` policy
build has no aligned-file change in the final stripped Go runtime-minimal
binary and reduces the stripped TinyGo runtime-minimal-tiny binary by 3,408
bytes in identical local builds; it makes every `Step` a complete synchronous cycle and is therefore a
footprint option, not a bounded-latency configuration. Transient/native roots remain one atomic direct walk with a
hard 1,024-reference limit because frame slots may
change when the mutator resumes; callback-only root sets fail closed in Tiny.

The final release size card for the complete #319 branch remains within every
budget: runtime-standard is +36.0 KiB, runtime-minimal +36.0 KiB, and
runtime-minimal-tiny +14.1 KiB versus `main`; the corresponding free budget is
358 KiB, 352 KiB, and 184 KiB.

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
index after reconciliation, regardless of its original size class. Subtree
maximums provide the lowest-address first fit among currently reconciled/indexed
spans, plus exact `largest-free`, free-byte, and span-count summaries without
rescanning. Deferred spans are reclamation debt, not allocation candidates: an
indexed higher-address fit is selected before a lower-address pending fit. Only
an indexed miss reconciles pending spans, one LIFO debt item at a time, and
retries the indexed lowest-address search after each item. Adjacent class and
large spans coalesce, top spans rewind the bump, and a later class allocation can
consume a span produced by a large object or vice versa. Full collection queues
dead old-space spans in a bounded reusable pending arena, moving balancing and
coalescing work out of the full-GC pause without changing poisoning or heap
limits. Promotion planning stable-sorts multiple survivors by destination size
class before reserving old-space spans.

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

### Exact native-root width matrix

Issue #304 is covered by `BenchmarkGCFrameLocalLivenessRootCounts` and
`BenchmarkGCNativeFrameRootMetadataWidths` at 64, 65, 128, 256, and 1,024
tracked roots. Run:

```sh
go test ./src/wago -run '^$' \
  -bench '^(BenchmarkGCFrameLocalLiveness$|BenchmarkGCFrameLocalLivenessRootCounts$|BenchmarkGCNativeFrameRootMetadataWidths$|BenchmarkGCNativeFrameRootEnumerationWidths$|BenchmarkCompiledGCFrameRootsSafepointByIDDense$)' \
  -benchmem -count=5
```

On August 8, 2026, the Ryzen 7 8845HS median results were:

| Roots | Mask words/site | Liveness ns/op | B/op | allocs/op | Root-map bytes/site | ID lookup ns/op | Enumeration ns/op |
|---:|---:|---:|---:|---:|---:|---:|---:|
| 64 | 1 | 6,929 | 12,088 | 8 | 264 | 1.39 | 143.1 |
| 65 | 2 | 7,841 | 13,512 | 9 | 268 | 1.39 | 147.1 |
| 128 | 2 | 15,587 | 27,336 | 9 | 520 | 1.39 | 326.5 |
| 256 | 4 | 37,130 | 64,360 | 9 | 1,032 | 1.39 | 573.3 |
| 1,024 | 16 | 294,723 | 445,269 | 11 | 4,104 | 1.40 | 2,271 |

The existing 16K-instruction one-root construction benchmark moved from a
1,355,953 ns/op median and 4,399,246 B/op at `fb102621` to 1,312,611 ns/op
and 3,580,039 B/op: -3.20% time, -18.6% temporary bytes, and the same five
allocations. Dense 4,096-site ID lookup remains direct-indexed at a 1.66 ns/op
median with zero allocations. Root-map lookup is independent of root width;
root enumeration remains linear in the exact live offset count.

The compact CFG node falls from 40 to 32 bytes. `GCFrameRootPlan` grows from 184
to 208 bytes per candidate function for the shared extra-word arena, and the
compile-only module plan grows from 24 to 40 bytes for its diagnostic. Runtime
fixed layouts do not grow: `Compiled` remains 784 bytes on this branch,
`compiledCodeCache` remains 64 bytes, and `validateMemo` remains 40 bytes. The
production `cli/wago` binary changed from 12,159,008 to 12,154,808 bytes
unstripped and from 8,290,596 to 8,286,500 bytes stripped: reductions of 4,200
and 4,096 bytes respectively. The 64-root generated fixture remains exactly 730
native bytes with SHA-256
`34a31f2aced3b860a0b07b56644406f5a5dd11f6c39bf16dcecc3f20cf939c97`
on both `fb102621` and this change; wider-map support is code-neutral for the
existing one-word case.

### Survivor aging and adaptive tenuring

Run the retained lifetime and policy A/B matrix with:

```sh
go test ./src/core/runtime/gc/native -run '^$' \
  -bench '^(BenchmarkThroughputObjectLifetimes|BenchmarkThroughputMixedLifetimeGraph|BenchmarkThroughputMixedLifetimeFullGCPressure|BenchmarkThroughputZeroSurvivalPolicy)$' \
  -benchmem -count=5
```

The retained design uses two bounded copy semispaces rather than region-local
survivor sets. At the default 64-KiB Eden, each semispace is 32 KiB: 64 KiB of
additional fixed backing, no per-object allocation, no region bitmap/run
metadata, and no handle growth because age occupies existing high class bits.
Region-local survivor sets would avoid reserving the inactive copy space but
would duplicate the region/start/free-run machinery planned for #313; they were
rejected for this stage rather than introducing a second transient allocator.

Ryzen 7 8845HS medians on August 8, 2026:

| Young lifetime | ns/op | young copied B/op | promoted B/op | full GCs/op |
|---:|---:|---:|---:|---:|
| 0 minors | 181.8 | 0 | 0 | 0 |
| 1 minor | 416.0 | 32 | 0 | 0 |
| 2 minors | 677.8 | 64 | 32 | 0 |
| 3 minors | 741.7 | 64 | 32 | 0 |
| 5 minors | 1,066 | 64 | 32 | 0 |

The controlled one-minor mixed-lifetime A/B moves from a 363.2 ns/op median
with immediate promotion to 294.1 ns/op with survivor aging (-19.0%). Promoted
bytes fall from 32 to zero per operation; both policies copy 32 bytes and report
four fixture allocations per operation. Ten-run zero-survival medians are 77.24
ns/op for immediate promotion and 77.93 ns/op for survivor aging (+0.89%), both
at 0 B/op and 0 allocs/op. Thus the transient bump/cleanup path remains within
the 1% noise gate while medium-lived objects avoid old space entirely. Under a
4-KiB old-space pressure fixture, immediate promotion performs one full GC per
128 operations (`0.007812 full-GCs/op`) at a 328.4 ns/op median; survivor aging
performs zero full GCs, zero promotion, and a 292.1 ns/op median.

The matrix also reports the current adaptive threshold and cumulative
`YoungBytesCopied`/`PromotedBytes` through `Stats`. `wago_gcstats` adds age
histograms plus separate pointer-free age populations. Tests cover occupancy and
old-pressure threshold increases, pause/occupancy decreases, survivor-capacity
fallback, in-place large-object aging, card persistence, full collection,
poisoning, and exact failure rollback. Relative to `db149b36`, `cli/wago` changes
from 12,154,808 to 12,154,816 bytes unstripped (+8) and remains 8,286,500 bytes
stripped. The existing 64-root generated fixture remains exactly 730 bytes with
the same SHA-256, so survivor policy and ABI-v5 Eden bounds add no native bytes
to that allocation fixture.

## Integrated major-GC comparison against main

An adversarial branch review on August 8, 2026 compared `main` at `1e03d71f`
with the integrated GC branch. Test binaries were built once, pinned to CPU 4
with `GOMAXPROCS=1`, and run in alternating order on the Ryzen 7 8845HS host.
The collection matrix used 20 samples per binary and 3,000 fixed iterations per
case; medians below do not mix allocation/setup time into the pause timer.

Across repeated 20-case runs, the candidate's Throughput full-collection
geometric mean ranges from -1.26% to -4.41%; Tiny ranges from -0.83% to +0.52%.
Go allocation counts and bytes/op remain identical in every paired case.
Representative medians from the least noisy fixed-iteration run are:

| Layout | Survival | `main` | candidate | Change |
| --- | ---: | ---: | ---: | ---: |
| dense array refs | 0% | 1,311 ns | 1,146 ns | -12.62% |
| dense array refs | 10% | 2,055 ns | 1,914 ns | -6.86% |
| dense array refs | 90% | 7,349 ns | 7,258 ns | -1.24% |
| pointer-free | 0% | 920 ns | 878 ns | -4.62% |
| pointer-free | 50% | 1,660 ns | 1,698 ns | +2.29% |
| pointer-free | 90% | 2,231 ns | 2,343 ns | +5.04% |
| sparse struct refs | 1% | 1,075 ns | 991 ns | -7.80% |
| sparse struct refs | 90% | 3,739 ns | 3,704 ns | -0.95% |

The allocation-per-cycle matrix initially measured the 90%-survival
pointer-free case at +5.04%, but repeated matrix runs were unstable. The
permanent `BenchmarkThroughputFullLivePointerFree` removes allocation and
cleanup cache noise and repeatedly collects the same live heap. At 90, 900, and
9,000 live objects, candidate medians improve by 10.73%, 8.54%, and 11.32%
respectively. That stable scaling result supersedes the isolated matrix outlier.
Tiny remains neutral in aggregate; individual low-microsecond cases should
still be interpreted with their interleaved confidence interval rather than one
percentage.

The permanent `BenchmarkThroughputFullOldHeap*` family separately exposes old
space and deferred free-span debt. With 15 alternating samples and 20 fixed
iterations, collect-only pauses improve substantially:

| Old objects | Survival | `main` | candidate | Change |
| ---: | ---: | ---: | ---: | ---: |
| 1,000 | 0% | 19,904 ns | 11,871 ns | -40.36% |
| 1,000 | 50% | 23,780 ns | 16,915 ns | -28.87% |
| 10,000 | 0% | 201,980 ns | 110,467 ns | -45.31% |
| 10,000 | 50% | 229,024 ns | 170,303 ns | -25.64% |

A second optimization pass removes the previously measured refill regressions.
Monotonic pending-free runs coalesce before entering the AVL index, already
ordered promotion plans skip stable sorting, and equal-sized warmed promotion
runs reserve one first-fit-preserving contiguous destination. The run allocator
falls back when the first fit is too small or backing must grow, preserving
fragmented behavior, geometric growth, and failure injection.

| Amortized operation | Objects | `main` | candidate | Change |
| --- | ---: | ---: | ---: | ---: |
| full + ForcePromote refill | 1,000 | 105,726 ns | 96,259 ns | -8.95% |
| full + ForcePromote refill | 10,000 | 1,073,041 ns | 959,771 ns | -10.56% |
| full + Eden + immediate minor promotion | 1,000 | 106,107 ns | 103,045 ns | -2.89% |
| full + Eden + immediate minor promotion | 10,000 | 1,136,193 ns | 1,086,762 ns | -4.35% |

The isolated `BenchmarkForcePromoteTransactional` likewise changes from 93.30
to 91.65 ns (-1.77%). Fresh bump allocation remains neutral (-0.13%), randomized
fragmentation stays within -1.50% to +2.17%, and 1,024/16,384-span churn changes
by -0.92%/-0.67%. The optimization therefore makes the formerly bad contiguous
refill cases net positive without giving back #312's fragmented-heap wins.

The adversarial run initially found a +36.22% Throughput full-collection
geometric-mean regression and +6.55% Tiny regression. Three release-path issues
caused it: card removal did work for handles with no card, full collection made
a second complete handle pass to recompute young bumps, and disabled Tiny
telemetry retained a runtime-owned defer path. The reviewed implementation now
returns immediately for cardless handles, compacts young membership and bump
state together, and compile-gates Tiny step timing. The matrix above is after
those corrections. They do not change the measured `cli/wago` file sizes:
12,154,816 bytes unstripped and 8,286,500 bytes stripped.

## Native array allocation and nursery chunks (#311)

On August 9, 2026, AMD64 native-array A/B runs used one test binary with
`WAGO_AMD64_NO_GC_NATIVE_ALLOC=1`, `GOMAXPROCS=1`, CPU affinity, alternating
order, 15 samples, and fixed iteration counts. The host was also running unrelated
CPU-heavy work, so absolute times and narrow differences are not release claims;
the retained policy is based on large repeated deltas and reports the noisy rows
rather than dropping them.

Generic public calls perform a mandatory collection at their next boundary. A
batch-depth matrix showed that reserving 32 handles and 4 KiB after only two or
four constructors regressed those short calls. Generic execution now remains
helper-only through eight slow constructors and refills after the ninth; products
without mandatory boundary collection refill immediately because their batch
survives later invocations. A 33-constructor generic operation therefore executes
nine rooted helpers followed by 24 native allocations. All rows remain 0 B/op and
0 allocs/op, and telemetry still records all 33 semantic allocations.

| Element | Length | helper-only | native/chunk | Change |
| --- | ---: | ---: | ---: | ---: |
| i32 | 0 | 4,157 ns | 2,283 ns | -45.08% |
| i32 | 1 | 3,958 ns | 2,219 ns | -43.94% |
| i32 | 4 | 3,874 ns | 2,236 ns | -42.28% |
| i32 | 32 | 3,995 ns | 2,298 ns | -42.48% |
| i32 | 256 | 4,402 ns | 4,475 ns | +1.66% |
| i32 | 4,096 | 23,111 ns | 22,518 ns | -2.57% |
| nullable anyref | 0 | 3,754 ns | 2,060 ns | -45.13% |
| nullable anyref | 1 | 3,939 ns | 2,017 ns | -48.79% |
| nullable anyref | 4 | 3,867 ns | 2,052 ns | -46.94% |
| nullable anyref | 32 | 3,973 ns | 2,250 ns | -43.37% |
| nullable anyref | 256 | 4,418 ns | 4,415 ns | -0.07% |
| nullable anyref | 4,096 | 23,043 ns | 22,844 ns | -0.86% |

The measured generic batch-depth discriminator was:

| Constructors per invocation | Change |
| ---: | ---: |
| 1 | +15.46% (95% interval includes zero) |
| 2 | -0.71% |
| 4 | -7.20% |
| 8 | +2.55% |
| 16 | -7.02% (95% interval includes zero) |
| 33 | -42.93% |

A separate 20-sample fresh-runtime run measured 5,949 ns helper-only versus
5,196 ns admitted (-12.65%, interval includes zero), with 90 B/op and 6 allocs/op
on both sides. The staged fixed numeric product, whose batch persists across
public calls, improves about 31-33% for construct-plus-length and 50-53% for
construct/set/get. The existing native struct sequence remains byte-identical at
4,303 generated bytes except for sixteen ABI-version immediates; structs retain
the direct global nursery bump and do not pay array chunk-cursor checks.

Only statically sized objects through 256 object bytes receive native code. The
256- and 4,096-element rows are unchanged helper-only shapes and expose only
measurement/code-layout noise. Earlier approximately 1-KiB and 16-KiB admission
regressed 69-70% and 60-100% because a chunk exhausted before its handle batch.
Dynamic lengths, `array.new_data`, `array.new_elem`, non-final arrays, and defined
heap-reference arrays remain exact helpers.

For 33 static length-4 sites, native admission adds 941 generated bytes: one
578-byte shared stub plus bounded call-site setup. A rejected 256-element module
emits byte-identical helper code. Small final reference tests validate every
initializer before publication and cover exact roots, collection, traps,
malformed metadata, and chunk cancellation. The ordinary collector remains
1,120 bytes, the native view 168 bytes, and the shared allocation ticket 160
bytes. Reproducible `-trimpath -buildvcs=false` CLI builds measure 12,129,774
bytes unstripped and 8,266,020 bytes stripped. That is -184 unstripped bytes and
no stripped change versus pre-#311 `ee257b06`; versus branch-base `main` at
`1e03d71f`, the integrated branch is +352 unstripped bytes and unchanged stripped.
A `wago_gcstats` build is eight additional unstripped bytes and has the same
stripped size.

## PR #371 regression follow-up

A post-PR adversarial pass on August 9, 2026 addressed the two repeatable bad
measurements rather than accepting them as policy costs. Pure survivor and
large-young minor cycles now skip old-space pending-free synchronization,
promotion sorting, and allocator transaction setup when no destination can
enter old space. Homogeneous-age cycles also skip impossible old-to-young
reconciliation scans, and internal survivor alignment lookup no longer repeats
public reference validation. Throughput free-span prefix consumption and
one-neighbor coalescing update the existing AVL node and its `maxSize` path in
place instead of removing, reinserting, and rebalancing the same span.

The follow-up used separate current-PR and candidate test binaries, alternating
order, `GOMAXPROCS=1`, CPU affinity, fixed iteration counts, and 15 samples per
side. The host still had unrelated CPU-heavy jobs, so individual matrix-cell
confidence intervals remain wide; geometric means and allocator churn effects
are the useful discriminators.

Relative to PR head `ba37eb0f`, the 20-cell Throughput minor matrix improves
2.27% geometrically overall, 5.20% across nonzero-survival cells, and 10.38%
for the 50% and 90% survival cells. A same-run `main`-to-candidate comparison
measures **+0.09%** geometrically for Throughput minor collection, replacing the
previous +9.16% aggregate. The corresponding Throughput-full matrix is -6.39%.
The Tiny point estimate was -7.92%, but these changes do not alter Tiny's
collector algorithm, so that result is treated as host/code-layout noise rather
than a claimed improvement.

The indexed allocator changes are independently large and stable:

| benchmark | current PR | follow-up | follow-up delta | `main` -> follow-up |
| --- | ---: | ---: | ---: | ---: |
| 64-span churn | 398.8 ns | 178.1 ns | **-55.34%** | **+70.44%** |
| 1,024-span churn | 651.6 ns | 269.8 ns | **-58.59%** | **-76.93%** |
| 16,384-span churn | 896.7 ns | 372.8 ns | **-58.43%** | **-98.20%** |

The 64-span case is therefore substantially repaired but remains a reported
70% regression versus `main` in this run, down from the previous 316% result.
Fresh bump allocation (+2.39%), one-span reuse (+3.08%), and impossible misses
were within the paired noise intervals. A path-caching experiment reduced churn
a further 10-18% but regressed fresh bump allocation by about 62% because its
fixed search-path scratch enlarged the common stack frame; it was rejected.

The latest main-to-candidate major lifecycle samples remain favorable: isolated
old-heap pauses range from -6.14% to -37.14%; full GC plus `ForcePromote` refill
is -1.55% at 1,000 objects and -10.78% at 10,000; full GC plus immediate-minor
refill is -3.14% and -8.58%. A separate 25-pair, 500-iteration live-heap run
measures -11.72%, -12.59%, and -16.38% at 90, 900, and 9,000 objects; all three
bootstrap intervals still include zero, so the point estimates are retained as
noise-qualified rather than presented as guaranteed wins.

The follow-up does not change `Config`, `Collector`, `handleEntry`, native-view,
or allocation-ticket footprints. Reproducible CLI sizes remain 12,129,774 bytes
unstripped and 8,266,020 bytes with linker stripping.

Future issue work must extend the matrix as follows:

- #302: no-survivor and low-survivor cleanup with high handle counts;
- #315: extend the completed 128/256/512-byte card matrix to any new reference
  bulk operations or region-local card representations;
- #308: reserved/committed bytes, backing growth, bytes copied, and page faults;
- #309: metadata bytes/object, handle-resolution instructions, and cache misses;
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
go test ./src/core/runtime/gc/native -run '^$'
go test -tags wago_gcstats ./src/core/runtime/gc/native -run '^$'
```

Ordinary `TestGC*WorkloadSmoke` tests execute one correctness cycle through
every benchmark family, including the largest sparse array and Tiny scan, without
invoking Go's adaptive benchmark runner.

To validate timing plumbing cheaply, select exact sub-benchmarks and force a
fixed iteration count. For example:

```sh
go test ./src/core/runtime/gc/native -run '^$' \
  -bench '^BenchmarkGCCollectionMatrix/throughput-minor/dense-array-refs/survival=50$' \
  -benchtime=2x
```

For a controlled run, disable frequency scaling/turbo when practical, pin the
process to one physical CPU, record the CPU/kernel/Go revision, and run multiple
interleaved samples:

```sh
taskset -c 2 go test -tags wago_gcstats ./src/core/runtime/gc/native \
  -run '^$' -bench '^BenchmarkGC' -benchmem -count=10 -timeout=30m \
  | tee gc-matrix.txt
```

Go's JSON event stream can be retained alongside ordinary benchmark text:

```sh
taskset -c 2 go test -tags wago_gcstats ./src/core/runtime/gc/native \
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
taskset -c 2 go test ./src/core/runtime/gc/native \
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
