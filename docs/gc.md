# Wasm GC runtime foundation

This document describes wago's native Wasm GC runtime direction. The current implementation is an initial foundation under `src/core/runtime/gc`; it establishes reference encoding, object metadata, typed descriptors, a byte-slice heap skeleton, exact scanning, roots, barriers, stress knobs, and tests. It is intentionally not yet wired into amd64 WasmGC opcode code generation.

## Why a wago-native collector

wago remains no-cgo and pure Go. The runtime must not link Boehm, MMTk, jemalloc, mimalloc, TLSF, or other C/Rust GC libraries. A native design keeps deployment simple, keeps runtime invariants auditable, and lets the compiler provide exact safepoint maps for off-heap native execution.

Guest WasmGC objects are not represented as individual Go heap objects. Per-object Go allocation would make guest object layout depend on Go's allocator and GC, add pointer-heavy metadata, and prevent generated native code from using compact, predictable references. Guest object payloads live in byte arenas managed by the wago collector.

## Reference representation

`gc.Ref` is a 32-bit integer guest reference:

- `0` is null.
- low bit `1` is an `i31` immediate; bits 1..31 hold the low 31 bits.
- low bit `0` and non-zero is a heap object handle (`handle index << 1`).

Refs are values, not Go pointers. Generated code may keep them in registers, stack slots, globals, tables, and object payloads. Exact safepoint maps will describe which machine locations contain refs.

The handle indirection lets the nursery move/promote objects while keeping existing `Ref` values stable in this first implementation. Future collectors may refine handle tables, but guest code must continue to treat refs as compact integers.

## Type descriptors

`TypeDesc` records the runtime layout of WasmGC struct and array types. It contains:

- `TypeID` for module/runtime type identity.
- descriptor kind: struct or array.
- scalar storage kinds (`i8`, `i16`, `i32`, `i64`, `f32`, `f64`) and ref storage kinds.
- struct field offsets.
- array element size.
- alignment.
- exact `HasRefs` / pointer-free metadata.
- placeholders for supertype/finality integration.

Descriptors answer the questions needed by allocation, scanning, verification, and future `ref.test`/`ref.cast` integration without overengineering subtyping in the first pass.

## Frontend type lowering

`src/core/compiler/frontend.BuildGCTypeDescs` lowers decoded Wasm GC recursive type groups into `[]gc.TypeDesc`. Descriptor indexes match flattened `wasm.TypeIdx.Index` values exactly. Recursive groups are flattened in decoder/validator order: all subtypes from the first group, then all subtypes from the next group, and so on.

Function component types keep their indexes stable by lowering to `gc.KindFunc` sentinel descriptors. They are not GC heap-object layouts and must not be allocated as guest objects. Struct and array component types lower to concrete descriptors with exact field offsets, element sizes, `HasRefs` metadata, and `Super`/`HasSuper`/`Final` metadata for future cast work.

Ref fields and ref array elements are fixed-width compact `Ref` slots. Recursive and mutually recursive references do not recursively expand object layout; the slot size is independent of the referenced type. Nullable and non-null refs have the same storage size and scan behavior. Mutability affects validation and code generation, not GC reachability, so it is not represented in the runtime descriptor layout.

The first lowering rejects unsupported storage such as `v128` with clear errors. Numeric and packed numeric fields/arrays are pointer-free. Ref-typed fields/arrays are scanned exactly; scanner logic ignores `null` and `i31` values at runtime.

## Object layout

Every object starts with a 16-byte header:

```go
type ObjHeader struct {
    TypeID uint32
    Size   uint32
    Aux    uint32
    Flags  uint32
}
```

`TypeID` indexes the descriptor. `Size` is the total object size including the header. `Aux` currently stores array length and is reserved for forwarding metadata during copying. `Flags` stores generation/color/age/pointer-free/forwarding/large-object bits.

Payload begins at `PayloadOffset == HeaderSize`, currently 16 bytes. Object sizes are 8-byte aligned. Heap memory stores header fields in little-endian byte arenas, not as Go object pointers.

## Compiled metadata and instantiation

Frontend lowering produces immutable descriptor metadata during compile. `Compiled.GCTypeDescs` stores the descriptor slice so `.wago` blobs can instantiate without re-decoding the Wasm type section. The descriptor slice index matches flattened `wasm.TypeIdx.Index`, including function sentinels used only to preserve indexes.

Each `Instance` owns its own `gc.Collector` when descriptor metadata is present. Collectors are never shared across instances: nursery state, old-space state, roots, remembered sets, cards, and collection statistics are per-instance runtime state. MVP/non-GC modules keep `Instance.gc == nil` to avoid allocating an unused heap.

GC roots are not wired to native frames yet, and no WasmGC opcode/codegen support is enabled by this metadata plumbing. Later PRs will connect exact safepoint maps, runtime allocation calls, and barrier emission to the instance-owned collector.

## Heap profiles and architecture

`gc.Config.Profile` selects one of the supported allocator/runtime presets:

- `ProfileThroughput` is the zero-value/default profile. It pairs the
  `AllocatorPagedSizeClass` allocator with the `RuntimeGenerational` scaffold.
- `ProfileTiny` pairs the `AllocatorTinyFixedBlock` allocator with the
  `RuntimeIncrementalMarkSweep` runtime.

Allocator choice and GC runtime choice are separate concepts internally. Today
only those two preset combinations are supported; unsupported cross-products are
rejected at collector construction instead of being exposed as production-ready.
The older `PolicyDefault`/`PolicyTiny` names remain compatibility aliases for the
profile presets.

The throughput/default target architecture is GenImmix-shaped:

- bump-allocated nursery for young objects;
- reusable old generation allocation;
- reusable non-moving large-object allocation;
- exact root maps;
- typed object descriptors;
- remembered sets and card marking.

The current throughput implementation keeps the nursery simple and routes old
and large allocations through a reusable paged size-class allocator. Small and
medium promoted objects are rounded into size classes and returned to per-class
free lists on full collection. Larger objects use a coalescing free-span list.
This is more memory-intensive than Tiny and intentionally carries more metadata
so allocation and reuse are faster. Full Immix line/block marking remains future
work; the current allocator is production-shaped but not the final old-space
collector.

## Tiny constrained heap policy

`ProfileTiny` selects a constrained hardware profile inspired by the allocation
shape of `umm_malloc` and the incremental tri-color state machine of `ugc`, but
implemented natively in Go: wago does not link C code, enable cgo, or vendor
either project.

Tiny is intentionally fixed-size and non-moving:

- `TinyHeapBytes` is the maximum guest-object heap size. The heap never grows
  automatically.
- `TinyBlockBytes` is the power-of-two allocator quantum, at least the object
  alignment. The allocator manages variable-size objects as contiguous block
  spans.
- `TinyStepBudget`, `TinyStepEveryAlloc`, and `TinyCollectEveryAlloc` control
  allocation-time incremental/full collection stress behavior.
- `PoisonFreed` and `VerifyAfterCollect` apply to Tiny as debug knobs.

The allocator is a compact first-fit fixed-block allocator over one byte slice.
Free span metadata lives in scalar side-table slices indexed by block number;
there is no Go object per guest object and no pointer-heavy free list. Freeing a
span returns it to an address-sorted free list and coalesces adjacent spans.
Allocated object bytes are stable for the lifetime of the object, so existing
`Ref` handles continue to identify non-moving objects by handle-table entry.
Allocation failure is deterministic: if a requested span cannot be found, the
collector completes a Tiny collection cycle using the supplied roots, retries
once, and then returns `gc: tiny heap exhausted` without growing the heap.
Allocation-triggered collection requires explicit roots; if roots are absent the
allocator returns a clear error rather than collecting with an incomplete root
set.

Tiny collection is an incremental tri-color mark/sweep collector with states
`idle -> mark -> remark -> sweep -> idle`. Marking grays exact roots from the
supplied `RootSet`, globals, and tables, then scans guest objects by `TypeDesc`.
Before sweep, Tiny re-scans roots so stack/frame/local root stores that do not
run object barriers are still observed. Sweep walks handle indexes and frees
white Tiny objects back to the fixed-block allocator. `CollectFull` completes one
whole Tiny cycle. `CollectMinor` is specified as the same full Tiny cycle because
Tiny is non-generational.

Exact scanning is shared with the default policy: pointer-free objects are not
recursively scanned, struct ref fields are visited only at descriptor offsets,
ref arrays scan elements, numeric fields and arrays are ignored even when their
bits look like refs, and `null`/`i31` values are ignored. Global and table slots
are part of the root set for both full and incremental Tiny collection.

Tiny write barriers preserve the incremental no-black-to-white invariant.
Object stores use a conservative hybrid barrier: when a black parent receives a
white child during Tiny marking, the child is grayed (forward barrier) and the
parent is re-grayed (backward barrier). Handles already gray are not pushed to
the gray stack again. Slot stores for globals/tables gray the stored child during
an active Tiny mark. Pointerful objects allocated during active Tiny marking are
born gray so array/ref initialization cannot publish an unscanned black object
with white children. This keeps `struct.set`, ref-array stores, `global.set`, and
`table.set` safe without introducing C-style intrusive lists or pointer headers.

Tiny manages WasmGC heap objects only. It is separate from Wasm linear memory
allocation and does not implement WasmGC opcode lowering or backend integration.
It is also not a replacement for the future GenImmix/default policy; it is a
bounded, predictable option for constrained targets where a fixed maximum heap,
stable object addresses, compact metadata, and deterministic allocation failure
are more important than moving/generational throughput.

Known Tiny limitations in this foundation:

- first-fit allocation is simple and deterministic but not fragmentation-optimal;
- collection is incremental by explicit `Step` calls or allocation-time stress
  knobs, not concurrent;
- handle-table entries remain the stable ref indirection;
- no WasmGC opcode/backend lowering is wired to the policy yet.

## Roots and safepoints

`RootSlot` is a mutable ref slot:

```go
type RootSlot interface {
    GetRef() Ref
    SetRef(Ref)
}
```

`RootSet` ranges over root slots. Tests use simple root slots, globals, and tables. Future codegen should expose frame/safepoint roots through a lower-allocation equivalent generated from exact stack maps.

Safepoint contract for generated code:

1. At a GC safepoint, every live guest ref in registers/spills/frames must be described exactly.
2. Non-ref machine values must not be scanned conservatively.
3. Runtime calls that may allocate must either publish roots or use an ABI where the collector can find all ref arguments/results.
4. Allocation-triggered collection requires an explicit root set/root provider; helper allocation paths must not collect with implicit nil roots.
5. If the nursery moves objects or the handle representation changes, root slots must be mutable so the collector can update them.

## Write barrier contract

The barrier surface is present now:

- `WriteBarrierObject(parent, child)`
- `WriteBarrierSlot(slotKind, index, child)`
- `CardMarkArray(array, elementIndex)`
- `BulkWriteBarrier(dst, start, length)`

The barriers have two responsibilities:

1. Generational remembered-set/card marking: old-to-young object edges and root/table/global slots containing young refs must be discoverable by minor collection.
2. Incremental tri-color marking: future concurrent/incremental marking must preserve the no-black-to-white invariant.

Generated code must call object barriers for `struct.set` and ref array stores, slot barriers for `global.set`, `table.set`, and frame/root publications as needed, and bulk barriers for array initialization/copy/fill paths that write refs.

## Exact scanning

Scanning is descriptor-driven:

- pointer-free objects are not recursively scanned;
- struct refs are scanned only at exact ref field offsets;
- ref arrays scan elements;
- numeric arrays do not scan elements;
- null and `i31` refs are ignored.

Verification checks that live refs point to valid handles, object type IDs exist, descriptor-derived sizes match headers, array lengths match sizes, remembered-set entries are valid, and roots do not point to reclaimed objects.

## Stress and debug knobs

`Config` includes:

- `CollectEveryAlloc`
- `TinyNurseryBytes`
- `ForceMajorEveryMinor`
- `VerifyAfterCollect`
- `PoisonFreed`
- `StressBarriers`
- `DisableMovingNursery`
- `Profile`, including `ProfileThroughput` and `ProfileTiny`
- `Allocator` / `Runtime` normalized profile choices
- compatibility `Policy`, including `PolicyTiny`
- `TinyHeapBytes`
- `TinyBlockBytes`
- `TinyStepBudget`
- `TinyCollectEveryAlloc`
- `TinyStepEveryAlloc`
- `ThroughputHeapBytes`
- `ThroughputPageBytes`
- `ThroughputClassLimit`

Tests exercise tiny nurseries, collect-every-alloc, exact scanning, cycles, roots, minor/full collection, and barrier metadata. Environment variables can be layered on later if needed; the first pass keeps the knobs explicit and testable.

## Current limitations

- Wasm type sections are not lowered into `TypeDesc` yet.
- WasmGC validation and opcodes are not connected to this package yet.
- amd64 codegen does not emit allocation calls or barriers yet.
- Minor collection currently promotes marked nursery survivors through handles rather than implementing a final copying nursery/root-update path.
- Old generation is a byte-slice mark/sweep skeleton, not full Immix block/line allocation yet.
- Large-object reclamation is represented in metadata but arena compaction/reuse is not implemented.
- Tiny policy is available for fixed-size non-moving heaps, but WasmGC opcode/backend lowering is not connected to it yet.

These limitations are intentional for the first commit series: the runtime foundation is small, exact, typed, and no-cgo, giving later codegen work stable contracts.
