# WasmGC telemetry and A/B reporting

Issue #300 adds one bounded, opt-in measurement surface for collector cycles,
dynamic paths, managed memory, generated GC code, and product-level A/B reports.
The instrumentation is diagnostic rather than collector policy: it must not alter
reachability, allocation order, trap behavior, heap limits, or emitted native
bytes.

## Build and runtime opt-in

Collector timing and counters are compiled only with `wago_gcstats`:

```sh
go test -tags wago_gcstats ./src/core/runtime/gc/...
go test -tags wago_gcstats ./src/wago
```

The repository coverage report also runs the collector package with this tag and
merges that profile with the ordinary, guard-page, and specification profiles.
This keeps diagnostic-only telemetry paths visible without adding timing or
counter machinery to release builds.

Then attach one recorder to one checked collector. In this example, `gc` is
`github.com/wago-org/wago/src/core/runtime/gc`:

```go
telemetry := new(gc.Telemetry)
collector, err := gc.NewCollector(gc.Config{
    Telemetry: telemetry,
}, types)
```

The public runtime aliases the same API through `wago.GCTelemetry` and
`wago.GCConfig`; `wago.GCTelemetryAvailable`,
`wago.NewGCBenchmarkTelemetryReport`, and `wago.CaptureGCMemoryDomains` expose
build/report helpers through the root facade. `Instance.GCTelemetrySnapshot` and
`Instance.ResetGCTelemetry` serialize access with the shared Runtime collector
domain. A consumer joining an existing domain may omit the recorder. Supplying a
different non-nil recorder for an existing domain rejects instead of silently
splitting one collector's counters.

An ordinary build deliberately ignores `Config.Telemetry`.
`TelemetrySnapshot` returns `false`, `ResetTelemetry` returns `false`, and JSON
report emission returns an error naming the required build tag. This keeps the
normal collector on its uninstrumented implementation and prevents timing code,
histograms, `encoding/json`, and RSS sampling from entering release hot paths.

## Cycle data

`TelemetrySnapshot` contains cumulative `Minor` and `Full` records. Each record
includes:

- completed and failed cycle counts;
- total collector wall time;
- bounded p50, p90, p95, p99, and maximum pause estimates;
- phase wall time;
- root counts and root-enumeration time by ownership class;
- visited objects, payload bytes, reference slots, and descriptor/array scan entries;
- object scans begun, resumed, and completed, plus Tiny's maximum object-range,
  entry, reference-slot, and payload-byte work observed in one `Step`;
- swept objects and payload bytes;
- Eden/survivor occupancy, copying, promotion, age histograms, and separate
  pointer-free age populations; and
- dirty/useful/duplicate cards, scanned slots, whole-object scans, scans avoided,
  and cleared cards.

The pause histogram uses 16 linear sub-buckets per power-of-two nanosecond
interval. It retains 1,009 counters regardless of run length. Reported
percentiles are bucket upper bounds; `MaxNS` is the exact observed maximum.

Reference-slot scan time is measured inside tracing and then subtracted from the
enclosing tracing/marking phase. Phase values are therefore additive rather than
double-counting object scans. Verification and shadow tracing are suspended from
cycle timing and work counters.

For Throughput minor cycles, `DirtyObjectCards` counts fixed cards covered by
linked dirty byte ranges rather than dirty objects; `DirtyRootCards` counts
stable global/table slot bits. `ScannedSlots` is the exact number of reference
slots visited through card ranges. `UsefulObjectCards` counts cards that actually
contained a nursery edge, and `WholeObjectScansAvoided` counts dirty objects for
which clean payload cards were skipped. Metadata-growth fallbacks are explicit:
a missing object range performs one whole-object scan, while root-card growth
failure enumerates every persistent slot. These fallback scans remain visible in
whole-object/root counters rather than silently losing reachability.

The Throughput full collector queues dead old spans and reconstructs indexed free
space lazily. Consequently `FreeSpaceReconstructionNS` and
`FragmentationRecoveryNS` may legitimately be zero for the full pause; the
resulting pending bytes, free spans, largest span, fragmentation, backing growth,
and copied backing bytes remain visible in heap/path data. Do not move lazy work
back into the pause merely to make a phase counter nonzero.

Tiny `CollectFull`/`CollectMinor` records the complete incremental cycle. A cycle
driven directly through repeated `Step` calls is also recorded once, from its
initial root mark through final sweep. The recorder suspends between separate
`Step` calls, so cycle and phase time sum collector work rather than arbitrary
mutator delays. Partial ranges contribute reference-slot and scan-entry work
exactly once. The established `PayloadBytesVisited` field still records one
complete aligned logical payload when an object scan completes, including
pointer-free payloads; `MaxStepPayloadBytes` separately reports actual bounded
per-step scan accounting. `ObjectScansBegun`, `ObjectScansResumed`, and
`ObjectScansCompleted` expose cursor lifecycle, while the four `MaxStep*` fields
show the largest bounded object-tracing vector consumed by one marking `Step`.
Root enumeration and sweeping remain separately attributable phases and are not
claimed to be covered by the object-scan maximum. Tiny's epoch advance performs
no per-handle color-reset work, so root-enumeration time measures roots rather
than an implicit handle-table clearing pass. The additive `Tiny` policy record
reports current allocation debt, pacing limit, incremental/nonincremental build,
root/sweep hard limits, poison-byte limit, and whether sweep is suspended for
queued barrier work; release builds add no counter writes for this snapshot-only
state.

## Root classes

Unclassified `RootSet` arguments are reported as native-frame roots. Runtime or
test integrations using the trusted native ABI can use `native.ClassifiedRoots`
or `native.RootGroups` (`native` imports `src/core/runtime/gc/native`) to report:

- native frames;
- collector globals;
- collector tables;
- public tokens;
- foreign instances; and
- snapshot temporaries.

Collector-owned global and table slots are classified automatically. Throughput
minor collection reports only dirty collector slots; full and Tiny collection
report every persistent slot. Runtime frame walkers use an allocation-free
classified direct-root interface for transient native frames, Runtime-domain
globals, and tables that are not owned by the collector slot directory. Public-
token, host-boundary, foreign-instance, and constant-expression/snapshot
temporary slots retain their exact class, including across telemetry reset.
Counts are visits, not deduplicated object identities; Tiny's initial mark and
remark each enumerate roots and therefore each contribute a visit.

## Dynamic paths

`PathTelemetry` separates:

- generated native-fast allocations;
- successful Go allocation paths;
- build-tagged synchronous Go-helper transitions;
- conditional medium/refill paths;
- card backing growth;
- handle refills;
- nursery exhaustion;
- minor/full collections; and
- Throughput backing growth and bytes copied.

Generated native allocations increment the collector's authoritative allocation
counter. Native-fast count is derived from that total minus successful Go
allocation paths, so generated code needs no diagnostic counter store on its hot
path. Handle-batch refills are conditional-medium paths. Build-tagged
`wago.GCHelperStats.TelemetryPaths` contributes synchronous helper transitions to
a report without guessing successful native conditional paths from fallback
counts.

## Managed and host memory

`ManagedHeapTelemetry` reports current live objects/payload, physical allocated,
committed, configured-reserved, reusable free, largest-free, free-span,
fragmentation, metadata, and occupancy-histogram values. Throughput summaries use
the exact augmented free-span index plus pending spans and backing tails. Tiny
summaries use compact span boundaries and bins.

`CaptureMemoryDomains` keeps these domains separate:

1. Go compiler heap, supplied by the product benchmark at compile boundaries;
2. current Go runtime heap from `runtime.MemStats`;
3. WasmGC committed managed bytes;
4. executable JIT bytes supplied by the compiler/runtime fixture; and
5. peak RSS from `getrusage` on Linux/Darwin, or a product-supplied platform
   measurement elsewhere.

Compiler and runtime heap attribution requires lifecycle deltas. A single
process-wide Go heap sample cannot infer which retained bytes came from module
compilation.

## Native-code attribution

Fresh compilation can enable code-neutral attribution independently of collector
cycle telemetry:

```go
cfg := wago.NewRuntimeConfig().WithGCCodeTelemetry(true)
compiled, err := wago.Compile(cfg, wasm)
code, ok := compiled.GCNativeCodeTelemetry()
```

The compiler reports total native bytes plus overlapping diagnostic attribution
for:

- allocation;
- handle resolution;
- type/cast checks;
- null checks;
- bounds checks;
- barriers;
- spill/reload traffic;
- synchronous helper calls;
- shared GC stubs;
- trap stubs; and
- exact root-map metadata.

Categories may overlap when one instruction sequence proves multiple properties;
`TotalBytes` is the authoritative non-overlapping total. Stats collection is
covered by byte-for-byte code-neutral tests. Attribution is not serialized in a
`.wago` artifact because it is reproducible compile diagnostics, not executable
semantics.

## Machine-readable reports

`BenchmarkTelemetryReport` schema version 1 combines a structured workload
configuration, collector, memory, native code, linked bytes, compile/mutator
time, B/op, allocs/op, operations, and a semantic checksum or expected trap.
`WriteJSON` emits one newline-terminated JSON object suitable for JSONL A/B
files.

A report producer should:

1. reset telemetry immediately before the measured lifecycle;
2. keep fixture construction and semantic validation outside the pause timer;
3. capture compiler and runtime heap boundaries separately;
4. record linked and executable bytes from their authoritative owners;
5. preserve the raw JSONL and ordinary benchmark output; and
6. reject a timed fixture whose semantic checksum or expected trap is absent.

Go benchmark output remains machine-readable through `go test -json` and stable
custom metrics. The collector matrix reports phase time, visited work, promotion,
and card scanning when run with `wago_gcstats`.

## Benchmark layers

- `BenchmarkGCCollectionMatrix`: Throughput minor/full and Tiny full across 0%,
  1%, 10%, 50%, and 90% survival and four pointer-density layouts.
- `BenchmarkGCSparseRememberedArray`: 128/256/512-byte cards, two distant writes
  versus every-slot writes in large old reference arrays at the same two-object
  allocation rate.
- `BenchmarkGCDirtyPersistentRoots`: one dirty collector global with 1, 64, or
  4,096 total slots; minor telemetry must report one visit in every case.
- `BenchmarkGCRootClassMatrix`: all six required ownership classes at 1, 64, and
  4,096 roots.
- `BenchmarkGCTinyStepMatrix`: bounded-step baseline for large reference arrays.
- `BenchmarkCollectorTelemetryOverhead`: empty-cycle disabled/enabled diagnostic
  cost.
- `BenchmarkGCStaticSiteCompilation`: one versus 4,096 static GC allocation sites,
  including compile B/op, allocations, native bytes, and GC allocation bytes.
- `BenchmarkGCStaticSiteExecution`: 4,096 executions through one hot site versus
  one execution through each of 4,096 sparse sites.

Run collector telemetry and executed helper tracking together when dynamic path
attribution is required:

```sh
go test -tags wago_gcstats ./src/core/runtime/gc/native \
  -run '^$' -bench '^BenchmarkGC' -benchmem -count=10

go test -tags wago_gcstats ./src/wago \
  -run '^$' -bench '^BenchmarkGCStaticSiteExecution$' -benchmem -count=10
```

The canonical performance-change evidence policy, complete-lifecycle rules,
workload gates, raw-evidence requirements, CPU pinning, hardware counters, and
repeated interleaved A/B recipe are in the
[benchmark review protocol](gc-benchmarks.md#benchmark-review-protocol).

## Current footprint and overhead

Measured August 8, 2026 on linux/amd64 with Go 1.24.4:

- `Config`: 60 to 64 bytes;
- `Collector`: 1,000 to 1,008 bytes;
- ordinary-build inert `Telemetry`: 184 bytes when a caller explicitly allocates
  one, but `NewCollector` drops the pointer;
- `wago_gcstats` `Telemetry`: 18,496 bytes plus an optional one-byte-per-
  classified-persistent-slot vector, fixed after slot construction;
- `TelemetrySnapshot`: 1,864 bytes, copied only on explicit snapshot;
- twenty pinned interleaved 10,000-operation zero-survivor minor controls had an
  811.95 ns parent median and 814.30 ns current median (+0.29%), with identical
  fixture allocation metrics; direct collection remains 0 B/op and 0 allocs/op;
- release `alloc`, barrier, mark, scan, minor/full, and Tiny hot-path text sizes
  are byte-for-byte the same length as the parent and contain no telemetry calls;
  and
- an instrumented empty minor cycle measured 1.09-1.23 us/op with 0 B/op and 0
  allocs/op, versus 22.7-26.8 ns/op for the disabled empty-cycle control in the
  telemetry build.

For the minimal Throughput collector executable used by the GC footprint matrix,
ordinary builds changed from 2,518,194 to 2,524,860 unstripped bytes (+6,666) and
from 1,646,776 to 1,650,872 stripped bytes (+4,096 file alignment). Building the
same source with `wago_gcstats` produced 2,734,166 unstripped and 1,790,136
stripped bytes. Diagnostic timing and JSON support therefore remain explicitly
outside release builds. Hardware instruction/cache counters were not sampled on
this host because `perf` was unavailable; the documented reviewer command is
still required before claiming hardware-counter neutrality.

## Checked API migration

The checked `gc` package exposes `Telemetry`, `TelemetryAvailable`, and collector
`TelemetrySnapshot`/`ResetTelemetry` methods. Closed collectors return unavailable.
It retains opaque owner/generation-checked references; telemetry does not expose
raw references or a native collector. Native root classification helpers belong
to the trusted `gc/native` integration and must not be used as checked Go ingress.
Checked root snapshots report the ordinary unclassified root category.

Checked collectors retain at most one idle scratch record with at most 256 entries
in each roots/native-values/constructor-values vector. Nested root callbacks use
separate records. Error, panic, and close paths clear references; outlier buffers
are discarded. Collector and root access still requires external synchronization.
