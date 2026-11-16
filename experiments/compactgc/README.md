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
would change minimum-object queue capacity and introduce mixed SIMD alignment
questions. Empty objects still occupy sixteen bytes.

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
Tests have been authored but not run until an explicit shared-CPU window opens.
