# Standalone compact-header collector prototype

This is an isolated research substrate derived from `experiments/directgc`.
Nothing in Wago runtime, compiler, Throughput or Tiny uses it. There is no V8
performance or object-size claim, and no runtime-admission receipt yet.

## Layout

Objects retain **16-byte alignment and stride rounding** in this first stage.
Structs store only a four-byte type ID. Their payload begins at the type header
rounded to the descriptor's actual alignment. Arrays additionally store exact
length in the next four bytes; their payload begins at offset eight, or sixteen
for SIMD elements. Immutable descriptors derive payload extent, stride and
reference fields. Generation comes from occupied regions, not header flags.

A one-i32 struct with four-byte alignment occupies a 16-byte stride here, versus
32 bytes in the existing direct substrate. This is a representation fact, not a
speed measurement. Eight-byte object alignment is deliberately deferred: it
requires finer mark granularity and address-dependent SIMD payload layout.
The existing queue capacity can remain sufficient under the promote-all proof
in `COMPACT8_DESIGN.md`. Empty objects still occupy sixteen bytes.

`Header` returns a computed **value** containing the true compact layout.
`Payload` borrows only actual logical payload bytes with a bounded capacity.
There is deliberately no mutable `ObjectBytes` API pretending a native sixteen-
byte header exists. Borrowing is trusted: never alter typed reference validity,
remember raw writes into old reference objects, and discard slices before any
allocation/collection. Prefer `SetRef`, `ArraySetRef` and `WriteBytes`.

## Collector contract

All operations are serialized. Refs are compact offsets, not ownership- or
lifetime-tagged handles. The caller supplies every live mutable root. The
collector validates pointer maps, not Wasm subtype/nullability semantics.

Minor collection promotes all nursery survivors. Full collection moves live
objects into the other old semispace. Capacity and references are completely
preflighted before heap/root mutation. After copying an intact object, its source
type word becomes the forwarding offset. Source start/range membership plus the
preflight mark bit identify forwarding; destination ranges are disjoint from
original live ranges, so duplicate root pointers cannot double-forward.

No source header is interpreted after its type word is overwritten. Source
retirement and exact deferred allocation counting use start-map membership.
Destinations retain original type and array length. Dirty old-parent overflow
uses a start-map walk. All dirty/queue state is retained on preflight failure.

The bounded dense work queue remains in this stage. Replacing it with hierarchical
pending bitmaps is a separate experiment, not an implicit claimed saving.

## Memory accounting and verification

`MemoryUsage` reports backing capacities, descriptor/reference-map storage and
collector object size. It excludes Go allocator size classes, temporary setup
copies and runtime bookkeeping; it is not RSS. `FixedBytes` reports arena plus
fixed tracing backing only. Arena capacity includes two old semispaces and its
alignment slack. Queue/start/dirty/mark backing is not hidden.

Tests cover mixed layouts, SIMD payload alignment, packed/empty arrays, cycles,
sharing and repeated root aliases, i31 values, old-to-young writes, oversized old
allocation, randomized graph identity, OOM/invalid-root byte invariance, exact
accounting, bounds, descriptor isolation and allocation-free full collection.
Package tests passed with Go 1.27.1 on AMD64: normal 0.014s, race 1.103s.
The randomized oracle covers 40 graphs and four collections per graph; the
allocation-free assertion reports zero Go allocations for full collection.
These are correctness runs, not comparative performance measurements.

For the test configuration of 128-byte Eden and two 512-byte old spaces, measured
backing/accounted components total 2,191 bytes: arena including alignment slack
1,183; starts 146; dirty 146; marks 16; queue 292; remembered queue 128; collector
struct 224; descriptor backing 48; reference-offset backing 8. Fixed backing is
1,911 bytes. This excludes Go allocator rounding/bookkeeping and is not RSS.
All existing directgc backing geometry remains unchanged in the new package:
compact headers reduce per-object occupancy, not these fixed arena budgets.

## Helper-only Wago integration experiment

Build with `wago_gc_compact_helpers` and explicitly select
`GCConfig.ExperimentalCompactOffsets`. The compiler issues a separate private
compact-helper receipt; ordinary handle artifacts and direct native ABI artifacts
are not interchangeable. Combining compact and direct native compiler tags is
rejected. The existing owned-Runtime, no-import/table/EH/extern admission remains.
This is an AMD64 experiment, not a TinyGo execution mode.

The native collector wrapper selects a backend concrete type at build time;
there is no new collector interface or pointer field. Compact payloads are the
real logical backing slices. Header access returns computed values, never an
expanded mutable native-header buffer. Native DirectView is unavailable. Generic
public StructSize/ArraySize utilities still describe the original native layout;
collector-bound allocation preflights use the selected backend's geometry.

Initial qualification covers exact retained-object wasm.fyi fixtures, mutable
globals and exported tokens through relocation, initializer scratch roots,
reference barriers, capacity failure, packed numeric payload borrows/copy/data
initialization, and default native collector tests. This is not a complete
conformance or performance qualification, and no V8 comparison is claimed.

The compact-only targeted suite also checks reference-array fill/copy barriers,
SIMD data construction/alignment through relocation, and graph cloning with
declared subtypes, i31, nullable references and rejected wrong-type/non-null
violations. Dedicated build-selected guards reject compact construction under
TinyGo-target builds. Default native tests retain the 68-byte Config and
3160-byte Collector footprint assertions; Compiled remains 808 bytes on AMD64.
Legacy tests specifically requiring direct native ABI or handle-native code
are not a compact-mode conformance suite and were not reported as passing.
