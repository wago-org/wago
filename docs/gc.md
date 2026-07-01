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

## Collector lifetime

`Collector.Close` is idempotent and releases heap backing storage plus root/card/mark metadata so an instance shutdown does not retain guest refs. After close, operations that need a live heap return `gc: collector closed`: allocation, collection, verification, object access/mutation, promotion, and checked root-slot creation/access/mutation. `Step` follows the same rule for both profiles; on Throughput it routes through `CollectMinor`, and on Tiny it rejects the closed collector before advancing incremental state.

`Stats` is intentionally safe after close for shutdown diagnostics. Allocation and collection counters are not incremented by rejected closed operations; `LiveObjects` is recomputed from the released handle table and therefore reports zero after close. Unchecked nullable slot readers (`GlobalSlot` and `TableSlot`) cannot return an error, so after close they return `null`; callers that need to distinguish close from an empty slot must use the checked accessors before shutdown.

## Heap profiles and architecture

`gc.Config.Profile` selects one of the supported allocator/runtime presets:

- `ProfileThroughput` is the zero-value/default profile. It pairs the
  `AllocatorPagedSizeClass` allocator with the `RuntimeGenerational` scaffold.
- `ProfileTiny` pairs the `AllocatorTinyFixedBlock` allocator with the
  `RuntimeIncrementalMarkSweep` runtime.

Allocator choice and GC runtime choice are separate concepts internally. Today
only those two preset combinations are supported; unsupported cross-products are
rejected at collector construction instead of being exposed as production-ready.
Public APIs re-export the GC configuration as `wago.GCConfig` with profile
constants `wago.GCProfileThroughput` and `wago.GCProfileTiny`, so callers do not
need to import internal runtime packages to choose a profile.

The throughput/default target architecture is GenImmix-shaped:

- bump-allocated nursery for young objects;
- reusable old generation allocation;
- reusable non-moving large-object allocation;
- exact root maps;
- typed object descriptors;
- remembered sets and card marking.

The current throughput implementation keeps the nursery simple and routes old
and large allocations through a reusable paged size-class allocator. Objects at
or above `LargeObjectBytes`, or any object larger than an empty nursery, are
allocated in non-moving large space so stress configurations cannot overrun or
permanently reject nursery-impossible allocations. Small and medium promoted
objects are rounded into supported size classes and returned to per-class free
lists on full collection. `ThroughputClassLimit` must be one of those size
classes (`32` through `32768` bytes today); unsupported limits are rejected at
collector construction rather than silently changing allocation policy. Larger
objects use a coalescing free-span list.

Throughput heap growth is intentionally checked before touching the backing
slice. Bump offsets, allocation ends, and page-rounded reservation lengths are
computed wider than `uint32`; configurations or object sizes that would wrap the
32-bit guest offset space or exceed a representable Go slice reservation must
return a clear allocation/configuration error instead of installing a handle that
points beyond the allocated byte arena.

This is more memory-intensive than Tiny
and intentionally carries more metadata so allocation and reuse are faster. Full
Immix line/block marking remains future work; the current allocator is
production-shaped but not the final old-space collector.

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
active Tiny mark and remark phases. Pointerful objects allocated during active
Tiny marking are born gray so array/ref initialization cannot publish an unscanned black object
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

## Allocator/GC codegen dependency contract

WasmGC opcode code generation must not depend on a concrete allocator or
collector implementation. Both the existing direct wasm-to-amd64 backend and the
new IR-to-amd64 backend should lower heap operations through the same small
semantic interface, with backend-specific emitters adapting that interface to the
current register/stack representation.

The contract has two layers:

1. a target-neutral heap/GC ABI that describes allocation, field/element access,
   barriers, and safepoints; and
2. a backend emitter implemented by each code generator to materialize loads,
   stores, helper calls, traps, spills, and root publication.

This keeps allocator-only and GC-backed policies injectable while avoiding an
IR-shaped API that the direct backend cannot use.

### Package boundary

Put shared codegen contracts in an internal compiler package such as
`src/core/compiler/codegen`. That package may depend on `compiler/wasm` and
`compiler/ir` only for metadata types where unavoidable, but the heap interface
itself must not require `ir.ValueID`. Direct codegen has no IR values; it has
operand-stack entries, locals, pinned registers, and spill slots. IR codegen may
wrap `ir.ValueID` behind the same opaque value type.

A minimal shared backend object can be:

```go
type Object struct {
    Code  []byte
    Entry []int
}

type Backend[M any] interface {
    Name() string
    CompileModule(m M, opts Options) (*Object, error)
}

type Options struct {
    Runtime RuntimeABI
    Heap    HeapABI
}
```

The direct backend can instantiate this as `Backend[*wasm.Module]`; the IR
backend can instantiate it as `Backend[*ir.Module]`. Both can keep their current
public compatibility wrappers while internally constructing `codegen.Options`.
The direct amd64 adapter is `backend/amd64.DirectBackend`, which forwards shared
`codegen.Options` into the existing direct compiler options without selecting a
concrete allocator or collector inside the backend.

### Backend-neutral values

Heap/GC lowering works with opaque backend values, not IR ids or amd64 registers:

```go
type Value struct {
    // Opaque backend-owned handle. A backend may encode a register, spill slot,
    // local, call slot, IR value, or temporary. Heap policies must treat this as
    // an opaque token and pass it back through Emitter methods.
    Opaque any
    Type   wasm.ValType
}
```

The value is intentionally opaque by contract. Policies ask the emitter to load,
store, spill, pass, or bind values; the backend decides whether that means using
a register, stack slot, frame offset, or materialized constant.

### Heap/GC ABI

The heap ABI is semantic and per-module/per-function:

```go
type HeapABI interface {
    Name() string
    RefLayout() RefLayout
    BeginModule(ModuleInfo) (ModuleHeapABI, error)
}

type ModuleHeapABI interface {
    BeginFunc(FuncInfo) (FuncHeapABI, error)
}

type FuncHeapABI interface {
    AllocObject(Emitter, AllocObjectRequest) (Value, error)
    AllocArray(Emitter, AllocArrayRequest) (Value, error)

    LoadField(Emitter, FieldLoadRequest) (Value, error)
    StoreField(Emitter, FieldStoreRequest) error
    LoadArrayElem(Emitter, ArrayLoadRequest) (Value, error)
    StoreArrayElem(Emitter, ArrayStoreRequest) error
    ArrayLen(Emitter, ArrayLenRequest) (Value, error)

    WriteBarrier(Emitter, WriteBarrierRequest) error
    BulkWriteBarrier(Emitter, BulkWriteBarrierRequest) error
    Safepoint(Emitter, SafepointRequest) error

    EndFunc(Emitter) error
}
```

Representative request structs:

```go
type AllocObjectRequest struct {
    TypeID     uint32
    Fields     []Value
    ResultType wasm.ValType
    LiveRefs   []Value // caller-known refs live across this may-allocate helper
}

type AllocArrayRequest struct {
    TypeID     uint32
    Length     Value
    Init       Value
    ResultType wasm.ValType
    LiveRefs   []Value // caller-known refs live across this may-allocate helper
}

type FieldStoreRequest struct {
    Object   Value
    Value    Value
    TypeID   uint32
    Field    uint32
    LiveRefs []Value // caller-known refs live across this helper safepoint
}

type ArrayStoreRequest struct {
    Array    Value
    Index    Value
    Value    Value
    TypeID   uint32
    LiveRefs []Value // caller-known refs live across this helper safepoint
}

type WriteBarrierRequest struct {
    Parent   Value // object ref when storing into an object/array
    Child    Value // stored ref; null/i31 filtering may be inline or helper-side
    Kind     BarrierKind
    LiveRefs []Value // caller-known refs live across this helper safepoint
}

type SafepointRequest struct {
    LiveRefs []Value
    Reason   SafepointReason
}
```

Allocator-only and collector-backed policies both implement `HeapABI`:

- an allocator-only/no-GC policy may make barriers and safepoints no-ops and
  route allocation to deterministic helpers; and
- Tiny, Throughput, and future collectors may use the same allocation requests
  while emitting profile-specific barriers, root publication, and helper calls.

Unsupported allocator/runtime cross-products remain rejected at collector/config
normalization. Codegen sees only the normalized `HeapABI` selected for the
compiled instance.

### Emitter ABI

Each backend implements the emitter surface using its own codegen state:

```go
type Emitter interface {
    ConstI32(uint32) Value
    ConstI64(uint64) Value

    Load(Address, wasm.ValType) (Value, error)
    Store(Address, Value, wasm.ValType) error

    CallRuntime(RuntimeFunc, []Value, []wasm.ValType) ([]Value, error)
    Trap(TrapCode) error

    SpillLiveRefs([]Value) (PublishedRoots, error)
    PublishRoots(PublishedRoots) error
    UnpublishRoots(PublishedRoots) error
}
```

The direct amd64 backend can adapt `Value` to its `ventry`/local/spill/register
state. The IR backend can adapt `Value` to `ir.ValueID`, frame slots, and
backend temporaries. Neither backend should expose its allocator, register
allocator, or block-lowering internals to heap policies.

### Runtime helper first policy

Until `docs/runtime-abi.md` explicitly guarantees stable native addresses for
WasmGC object payloads, generated code must use runtime helpers for object field
and array element access. Heap policies may emit inline ref tests, null checks,
`i31` packing/unpacking, length bounds checks, and simple barrier fast paths, but
they must not cache Go-slice-derived object payload pointers across helper
calls, allocations, safepoints, or collection points.

The first codegen integration should therefore lower WasmGC heap operations to
helper calls with exact roots:

1. collect all live refs required across the helper call;
2. pass caller-known live refs in the request `LiveRefs` field while leaving the
   direct helper operands in their semantic fields (`Fields`, `Init`, `Object`,
   `Array`, `Parent`, `Child`, and similar);
3. spill/publish the union of direct ref operands and `LiveRefs` through the
   emitter root protocol;
4. call the runtime helper with compact `gc.Ref` values and descriptor/type
   indexes;
5. unpublish roots after returned refs are stored in backend-owned locations;
6. emit the selected barrier for ref stores.

`LiveRefs` is an additive safepoint set, not a replacement for direct operands.
Backends lowering an allocating or may-collect helper must include every other
reference value that remains live after the call, even if that value is not an
argument to the helper. `HelperHeap` filters non-ref values before publishing and
keeps direct operands before caller-provided refs so root ordering stays
predictable for tests and backend adapters. It does not deduplicate roots because
`Value` is intentionally opaque and may not be safely comparable across
backends.

Later allocator profiles that provide stable chunked or pre-reserved payload
storage may add inline load/store fast paths behind the same `HeapABI` without
changing wasm or IR lowering.

### Lowering usage

Direct wasm-to-amd64 lowering should translate GC opcodes directly into heap ABI
requests:

```go
fields := g.popValues(fieldTypes)
ref, err := g.heap.AllocObject(g, codegen.AllocObjectRequest{
    TypeID: typeID,
    Fields: fields,
    ResultType: resultType,
})
if err != nil {
    return err
}
g.pushValue(ref)
```

IR-to-amd64 lowering should translate IR GC ops into the same requests and bind
the returned value to the instruction result:

```go
args := g.values(valueIDs(g.f, in.Args))
ref, err := g.heap.AllocObject(g, codegen.AllocObjectRequest{
    TypeID: uint32(in.Aux),
    Fields: args,
    ResultType: g.resultType(in),
})
if err != nil {
    return err
}
g.bindResult(singleResult(g.f, in), ref)
```

The IR op set may still contain semantic ops such as `OpStructNew`,
`OpStructGet`, `OpStructSet`, `OpArrayNew`, `OpArrayGet`, `OpArraySet`,
`OpRefTest`, and `OpRefCast`. The important constraint is that backend heap
lowering does not expose those IR ids to allocator/GC implementations.

### Safepoint and barrier placement

Generated code must insert safepoints at every helper call that can allocate or
collect, at wasm-to-wasm calls once GC refs can be live across them, at host
calls, and at future loop checkpoints if long-running native loops need
cooperative runtime polling. The safepoint request must describe exactly the ref
values live across the call; non-ref values must not be published or scanned.

Ref stores must emit the selected barrier after the store becomes visible, or use
a helper that performs the store and barrier atomically with respect to the
collector. Required store sites include:

- `struct.set` for ref fields;
- ref array element stores and bulk ref array initialization/copy/fill paths;
- `global.set` for ref globals;
- `table.set` and element-table writes once reference tables are supported; and
- any root publication path required by helper-call ABIs.

### Tests required with codegen integration

The GC PR that introduces this contract should include at least interface-level
or backend smoke coverage for:

- allocator-only/no-op barrier policy and real GC policy both satisfying the same
  `HeapABI`;
- direct amd64 and IR amd64 emitters compiling the same helper-call-shaped heap
  request in tests, even if one route remains opt-in;
- allocation helper calls preserving live ref arguments/results through exact
  roots;
- ref store paths invoking object or slot barriers exactly once;
- unsupported inline access being rejected while Throughput payload addresses are
  not stable; and
- clear errors when codegen requests a heap operation unsupported by the selected
  policy.

## Roots and safepoints

`RootSlot` is a mutable ref slot:

```go
type RootSlot interface {
    GetRef() Ref
    SetRef(Ref)
}
```

`RootSet` ranges over root slots. Tests use simple root slots, globals, and tables. Future codegen should expose frame/safepoint roots through a lower-allocation equivalent generated from exact stack maps.

Global and table root-slot constructors accept only nullable stored refs: `null`, `i31`, or a live object ref owned by the same collector. Checked constructors (`NewCheckedGlobalSlot` and `NewCheckedTableSlot`) return errors and do not append a slot when decode/instantiation sees a forged, stale, or cross-collector ref. The legacy convenience constructors delegate to the same validation and panic on invalid initial refs, so there is no root-slot creation path that silently installs invalid metadata.

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

Generated code must call object barriers for `struct.set` and ref array stores, slot barriers for `global.set`, `table.set`, and frame/root publications as needed, and bulk barriers for array initialization/copy/fill paths that write refs. `BulkWriteBarrier` is a post-write barrier: generated helpers must store or copy refs into the destination array range before invoking `BulkWriteBarrier`/`PostBulkWriteBarrier`. A pre-write bulk barrier cannot observe newly written nursery refs and is not a safe substitute.

Remembered-set and card metadata is deliberately conservative, but bounded for repeated writes to the same location. The throughput collector deduplicates remembered handles, object cards, and global/table slot cards. Checked global/table slot setters prune the slot card when a slot is overwritten with `null`, an `i31`, or a non-nursery object ref, because such slots no longer need to be revisited by future minor/card scanning. Object cards are kept as dirty-location metadata for still-live containers even if the exact young edge is later overwritten; they are deduplicated and are removed when the owning object is freed. Verification enforces that remaining cards point at valid live object handles or in-range global/table slots, but it does not require every conservative card to still contain a young edge.

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
- `StressNurseryBytes`
- `ForceMajorEveryMinor`
- `VerifyAfterCollect`
- `PoisonFreed`
- `StressBarriers`
- `DisableMovingNursery`
- `Profile`, including `ProfileThroughput` and `ProfileTiny`
- `Allocator` / `Runtime` normalized profile choices
- public profile aliases in package `wago`
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

- WasmGC opcode validation is not complete and the opcode/backend lowering is
  not wired to this runtime yet.
- The runtime-call ABI for allocation, field/element access, barriers, and
  traps still needs to be finalized before generated code can use WasmGC
  objects.
- Exact native safepoint maps are not connected to compiled frames yet.
- Minor collection currently promotes marked nursery survivors through handles
  rather than implementing a final copying nursery/root-update path.
- The Throughput old/large allocator reuses freed memory, but full Immix
  line/block marking, compaction, and more advanced fragmentation control remain
  future work.
- The Throughput heap currently uses growable Go byte slices, so native code
  must not cache raw heap payload pointers; see `docs/runtime-abi.md`.
- Tiny and Throughput profiles are available at the runtime/API level, but
  WasmGC opcode/backend lowering is not connected to either profile yet.

These limitations are intentional for this commit series: the runtime foundation
is small, exact, typed, and no-cgo, giving later codegen work stable contracts.
