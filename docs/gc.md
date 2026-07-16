# Wasm GC runtime foundation

This document describes wago's native Wasm GC runtime direction. WasmGC is now
active mandatory WebAssembly 3.0 scope, tracked in [wasm3.md](wasm3.md), rather
than a non-goal. The current implementation is an initial foundation under
`src/core/runtime/gc`; it establishes reference encoding, object metadata, typed
descriptors, a byte-slice heap skeleton, exact scanning, roots, barriers, stress
knobs, and tests. Iteration 38 wires one exact linux/amd64 numeric-local helper product;
iteration 39 adds exact immutable GC-global roots, packed fields, and the numeric portion
of the official basic struct leader. Iteration 40 closes the final struct action through one
bounded store-owned public result token and pins the complete array family obligations.
Iterations 41-43 add a separate exact array helper/product boundary, all official array
leaders, immutable and passive-element checked roots, reference barriers, data/element-drop
lifecycle, and bounded public array results. Iteration 44 adds the complete collector-free
`gc/i31` family: direct immediate operations, exact globals, compact tables, and i31 casts.
Iteration 45 pins the complete official `gc/ref_test` obligation, corrects sibling-type dynamic-
test validation, and adds one exact collector-free null+i31 execution product. Iteration 46 adds
collector-owned dynamic type lookup, checked compact object-table roots, and executes the official
concrete subtype/canonicalization leader. Iteration 47 adds bounded extern conversion ownership,
separate anyref/funcref/externref table contracts, and executes the complete abstract leader, making
`gc/ref_test` gap-free. Iteration 48 extends that owner with distinct bounded public conversion tokens,
strict null conversion constants, and executes the complete `gc/extern` family. General native frame publication, object-valued mutable/reference globals,
broad public ownership, and snapshots remain incomplete. These bounded products must not be presented as general executable
WasmGC support.

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

Decoded function signatures stored in `wasm.Module` may contain recursive-local `TypeIdx{Rec: true}` values when the signature was decoded inside a recursive type group. Existing storage-aliasing helpers such as `TypeFunc` and `LocalFuncType` document that behavior. Metadata/codegen consumers that need flattened absolute module indexes must use `ResolvedTypeFunc` or `ResolvedLocalFuncType`, which return resolved copies without mutating module storage.

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

Each `Instance` normally owns its own `gc.Collector` when its executable product can create or retain heap objects. Collectors are never shared across instances: nursery state, old-space state, roots, remembered sets, cards, and collection statistics are per-instance runtime state. MVP/non-GC modules keep `Instance.gc == nil` to avoid allocating an unused heap.

Iteration 37 adds one deliberately narrower exception. Ten exact pinned `type-rec` products contain struct descriptors only because recursive struct definitions participate in function identity. Their functions, immutable globals, imports, and ordinary funcref tables carry only function descriptors; no struct/array value or GC opcode exists. A compile-only, non-serialized sidecar records that exact product proof and keeps `Instance.gc == nil` even though `gc.HasHeapObjectTypes` is true. Unknown binaries, arrays, mutable fields, additional state, public codec load, snapshots, guard mode, and arm64 remain closed. A codec-reloaded artifact does not inherit this live admission sidecar. This is metadata/function-identity execution, not WasmGC heap execution.

General GC roots are not wired to native frames yet. Iteration 38 adds a separate exact
numeric-local helper product with one allocation point and a proven empty live-ref set.
Iteration 39 adds two collector-owned immutable global slots, not frame roots: each slot is
installed before a later initializer allocation. Neither establishes general frame scanning.
Later slices must connect exact safepoint maps, runtime allocation calls with non-empty live
frame roots, mutable slot synchronization, and barrier emission to the instance-owned
collector.

## Native exception-root map contract

Iterations 34-35 define the bridge between amd64 exception frames and the collector/
lifecycle layers. `src/core/nativeabi` owns dependency-neutral `FunctionRootMap` and
`RootSlot` records; `src/core/runtime/gc` aliases and validates them at the collector
boundary. A map names one local function, the minimum post-prologue frame prefix containing
its roots, and strictly ordered 8-byte-aligned offsets relative to that function's post-
prologue RSP. Validation rejects out-of-range functions, duplicate or unordered maps/slots,
unknown ownership kinds, unaligned offsets, and slots outside the announced frame prefix
before a scanner may trust metadata. `catch_all_ref` maps derive each payload word's
ownership from every bounded tag that can reach the catch; a word that mixes scalar and
reference values or mixes `gc.Ref` and funcref identity is rejected because a non-tag-
discriminated scanner cannot interpret it safely.

The current staged EH frame reserves four seven-word handler records followed by four
three-word exception-root records. This is a fixed 320-byte EH region per EH function:
224 bytes of handler state plus 96 bytes of roots. Including the 16-byte native frame
header and no locals/spills, the minimum mapped frame prefix is 336 bytes. In each root,
word zero is native exception identity and payload words one and two retain the tag's two
bounded payload values. The amd64 map builder derives offsets from the same backend
constants used by lowering; the exact single-funcref fixture maps payload word one at
offset 248 and rejects a fifth reference catch before producing metadata.

Two root kinds are intentionally distinct:

- `RootGCRef` / `NativeRootGCRef` is a compact `gc.Ref`. A collector may scan it and a
  future representation may rewrite it through the root-map interface.
- `RootFuncRef` / `NativeRootFuncRef` is a canonical funcref descriptor identity, not a
  `gc.Ref`. The producer/reference lifecycle must retain the descriptor owner while the
  slot is live, and the collector must never reinterpret those bits as a heap handle.

Iteration 35 opens exactly one non-collector exception-payload product without weakening
those collector obligations. A local tag may carry one non-null indexed `() -> ()` funcref
created by one declarative local `ref.func`. The descriptor owner is the executing instance,
which remains live for the native call. Each reference catch zeros its fixed three-word root
before installing the handler; when the caught exn value is immediately dropped, generated
code clears all three words. The official catch/catch_ref/catch_all_ref results are matched
against canonical function identity rather than public token bits. Nullable payloads,
extern/GC refs, imported or otherwise foreign descriptors, wider tags, roots that escape
instead of being dropped, tails, hosts, snapshots, guard mode, public admission, and arm64
remain rejected. This local lifecycle proof does not publish a collector frame and must not
be described as general safepoint integration.

Iteration 36 opens two different non-collector products from `ref_null.wast`. They contain
only `ref.null`, immutable local null globals, `global.get`, and null results across
`any`/`none`, `func`/`nofunc`, `exn`/`noexn`, `extern`/`noextern`, and one indexed function
heap. The ABI uses one zero-valued 64-bit slot. `ValueTypeDescriptor` and codec v27 retain
the exact abstract/bottom/indexed heap identity, while the internal `ValAnyRef`, `ValExnRef`,
funcref, and externref categories keep result slots distinguishable. Every non-null
`ValAnyRef`/`ValExnRef` ingress, result, and literal-global bit pattern rejects. Both modules
have function-only type descriptors, so `gc.HasHeapObjectTypes` is false and instantiation
leaves `Instance.gc == nil`; no nursery, old space, roots, safepoints, barriers, object
allocation, or collector teardown is involved. The product is exact, linux/amd64 explicit-
bounds-only, and rejects imports, mutable or exported storage, additional instructions,
snapshots, public feature admission, guard mode, and arm64 execution. It must not be called
WasmGC heap execution merely because `any`/`none` are GC-family heap types.

Iteration 37 also strengthens function identity for these metadata-only products. The bounded
64-bit structural key now serializes every member of a non-singleton recursive group plus the
selected member position. Equivalent shifted groups agree; reordered, singleton, and externally
linked groups remain distinct. The three official ordinary-funcref `call_indirect` actions therefore
preserve exact success/mismatch behavior at 36.20-36.97 ns/op with 0 B/op and 0 allocs/op. This key
work does not scan, allocate, root, or barrier any struct value.

### Iteration 38 exact collector-backed numeric structs

Iteration 38 is the first slice in which generated Wasm code creates and accesses a real
collector-owned object. The boundary is deliberately exact and compile-only:

- linux/amd64 with explicit bounds only;
- one pinned synthetic numeric-local product with one mutable `i32` field;
- `struct.new_default`, numeric `struct.get`, and numeric `struct.set` only;
- no imports, GC globals/tables, host/cross-instance/tail calls, reference fields, arrays,
  exported GC result, snapshot state, guard mode, or arm64 execution;
- plus four exact official `gc/struct.wast` products for declarations/bindings, named
  numeric gets, and null get/set traps.

The amd64 lowering parks through the existing 328-byte synchronous control frame. Dispatch
bit 30 names internal GC helpers independently of ordinary imports and public host-funcref
dispatch. Go receives compact `gc.Ref`, type index, field index, and numeric value slots,
uses `Collector.NewStructDefaultWithRoots`, `StructGet`, or `StructSet`, and returns only a
compact ref or numeric bits. No Go-slice-derived object pointer enters generated code or
survives the helper call.

The admitted allocation point has an exact empty live-root proof: each invocation performs
at most one allocation, no prior `gc.Ref` is live, and no later may-collect helper runs while
the returned ref remains live. It nevertheless supplies non-nil `gc.EmptyRoots{}` so stress
collection never falls back to an implicit nil root set. The zero-sized root provider avoids
the former 24-byte interface/slice allocation; steady-state new/default/get and
new/default/set/get both report 0 B/op and 0 allocs/op. `struct.get` and numeric `struct.set`
do not collect. Numeric stores need no write barrier; reference-field binaries are rejected
at the exact product gate, while the collector's existing Tiny remark/barrier tests remain
green.

Throughput and Tiny stress modes execute 1,000 repeated allocations with at most one live
object after collection. A 16-byte Tiny heap deterministically returns `gc: tiny heap
exhausted` and recovers for the next invocation. Instance close closes the collector and
later allocations return `gc: collector closed`. The 65-byte get fixture emits 341 code
bytes and a 495-byte codec-v27 blob; the 106-byte mutation fixture emits 846 code bytes and
a 1,062-byte blob. Each object is 24 bytes including the 16-byte header. `Collector` is
640 bytes on the measured host; fixed `Compiled=712`, `Instance=792`, and
`compiledCodeCache=64` layouts remain unchanged.

The iteration-38 pinned `gc/struct.wast` schema-2 matrix was 36 commands / 4 modules /
2 assertions / 4 invalid / 1 source-only malformed / 2 gates / 17 blocked, with no hidden
failures. The remaining basic and packed leaders required GC constants, global roots,
packed semantics, and a public-result boundary.

### Iteration 39 exact GC-global roots and complete numeric struct actions

Iteration 39 installs a bounded root bridge for the two remaining official leaders without
claiming a general frame scanner or public GC value ABI:

- a compile-only initializer record retains at most four numeric field values for each of at
  most two exact immutable GC globals; codec v27 never serializes these live records;
- instantiation allocates each object with explicit `gc.EmptyRoots{}`, initializes numeric
  fields, then creates a checked collector `GlobalSlot` before any later initializer may
  allocate or collect;
- one fixed two-entry per-instance mapping records Wasm global index to collector slot index;
  the ordinary 8-byte global cell carries the same compact `gc.Ref` for native `global.get`;
- the globals are immutable and `gc.Ref` handles are stable, so this slice has no mutable
  cell/slot synchronization problem. Mutable globals remain rejected until stores update the
  cell and collector slot transactionally through `WriteBarrierSlot`;
- a failed second Tiny allocation closes the collector and rolls back the whole instance;
  no partially live root mapping escapes;
- exported packed globals remain visible in metadata, but `Global` and `GlobalValue` reject
  raw/non-null GC egress rather than exposing compact handles as host tokens.

The packed leader uses helper-side descriptor kinds to preserve exact i8/i16 truncation and
signed/unsigned extension. Its ten official actions execute under Throughput and Tiny. A
64-byte Tiny heap holds the two rooted globals; a 32-byte heap deterministically fails the
second allocation. Packed objects are 24 bytes including the 16-byte header (6-byte payload,
2-byte alignment).

The basic leader has two rooted globals plus one transient object per invocation. Its six
numeric/f32 actions execute because the exact internal callees contain only non-collecting
`struct.get`/numeric `struct.set`; no live ref crosses a may-collect point after allocation.
The exported `new` action still reaches and verifies the public non-null-anyref rejection,
then remains recorded as one blocked action. This is an exact finite call-graph proof, not a
general Wasm-to-Wasm safepoint rule. Basic objects are 32 bytes including the header
(12-byte payload, 4-byte alignment).

This work exposed an independent parked-Go ABI bug: synchronous helper/host calls restored
callee-saved GPRs but could leave pinned f32/f64 locals in caller-saved XMM registers. The
backend now copies arguments first, spills every dirty pinned local, and lazily reloads it
after resume. A dedicated non-GC synchronous-host regression repeats the float-local round
trip 100 times.

Final `gc/struct.wast` accounting is 36 commands / 6 modules / 18 assertions / 4 invalid /
1 source-only malformed / 0 module gates / 1 blocked public-result action, with zero hidden
failures. Packed get measures 196.7-200.0 ns/op and packed set/get 256.0-258.1 ns/op. Basic
get measures 211.5-237.7 ns/op and basic set/get 281.3-318.9 ns/op. Every sample reports
0 B/op and 0 allocs/op.

### Iteration 40 bounded public struct result ownership

Iteration 40 adds the first non-null public WasmGC value without exposing the compact
collector representation or widening general GC ingress:

- `GCRef` is an opaque eight-byte public token. Its zero value is null; non-null token bits
  are random store identity with a non-zero upper half, never a raw 32-bit `gc.Ref` handle.
- Only the exact staged basic `gc/struct` product may issue one live token per producer
  instance. The store records the producer, compact ref, exact dynamic defined struct type,
  and collector root slot. The result's declared `(ref null any)` supertype is checked against
  that exact dynamic type before issue.
- Each producer allocates at most one public-token root slot. Release overwrites that checked
  collector `GlobalSlot` with null and reuses the same slot for the next token, so 100 repeated
  issue/release/collect cycles do not grow collector root metadata. A second simultaneous
  token rejects explicitly.
- The token retains one producer resource root, keeping code, arena, and collector alive after
  logical `Instance.Close`. Token-before-producer and producer-before-token close orders both
  release exactly once. Stale, foreign-store, and cross-producer releases reject without
  modifying either owner.
- Collector helper operations, token root mutation, and collector close serialize through one
  lazy per-instance mutex. Tiny's slot barrier/remark contract remains active because token
  issue/release uses `NewCheckedGlobalSlot`/`SetGlobalSlot`; no unbarriered root mutation was
  added.
- Non-null function parameters, global ingress/egress, host boundaries, snapshots, codec load,
  guard execution, arrays, and arm64 remain closed. The compile-only exact product enum and
  live token/root state are not serialized by codec v27.

The complete official `gc/struct.wast` matrix is consequently gap-free at 36 commands / 6
modules / 19 assertions / 4 invalid / 1 source-only malformed / zero gates / zero blocked,
with zero hidden failures. Public token issue plus release measures 371.6-386.5 ns/op over
five 500 ms samples and remains 0 B/op / 0 allocs/op after one warmup token initializes the
bounded map/root slot.

At the iteration-40 boundary, complete `gc/array.wast` schema-2 accounting covered 61
commands with seven exact module gates, 41 blocked actions, and six invalid modules. The
leaders separate declaration/binding metadata, numeric `array.new`/`array.new_default`,
numeric `array.new_fixed`, packed `array.new_data` and data-drop lifecycle, reference
`array.new_elem` plus barriers/element-drop lifecycle, null get/set traps, array length/bounds,
and public array result ownership. Iteration 41 executes a strict subset below without
weakening the remaining classifications.

The fixed measured layouts are `Compiled=712`, `Instance=792`, `compiledCodeCache=64`,
`instancePluginState=128`, `referenceStore=96`, `gcPublicState=24`, `gcRefTokenEntry=40`,
`GCRef=8`, `Value=16`, and `gc.Collector=640` bytes. Relative to iteration 39, the runtime
store and lazy plugin state each grow by one pointer; ordinary instances still allocate
neither lazy public GC state nor a collector.

### Iteration 41 exact pointer-free arrays

Iteration 41 gives arrays their own compile-only product enum, helper bit, and metadata-only
sidecar; it does not reuse `stagedGCStructProduct` or `collectorFreeStructuralMetadata`.
The admitted surface is deliberately finite:

- a 146-byte synthetic local product executes `array.new_default`, plain numeric
  `array.get`, mutable numeric `array.set`, and `array.len`;
- the official declaration and recursive-binding leaders instantiate without a collector
  through their own exact hash-pinned no-object proof;
- the official null leader executes `array.get`/`array.set` null traps; and
- the 268-byte numeric-fixed leader executes all seven actions, including bounds traps and
  two public non-null array results.

Allocation helpers still park through the 328-byte synchronous control frame and pass only
compact refs, numeric values, type indexes, and lengths. Each producer performs exactly one
allocation with no prior live frame ref and supplies non-nil `gc.EmptyRoots{}`. Access,
length, and numeric stores do not collect; numeric stores require no object barrier. Native
code never receives or retains a Go-slice-derived payload pointer.

The fixed leader's sole immutable `(ref $vec)` global is initialized from two exact f32
constants. Instantiation allocates the 24-byte array, writes both numeric elements, and
installs one checked collector `GlobalSlot` before any invocation. The global cell and slot
carry the same stable compact handle. Codec v27 serializes neither the array product enum,
helper admission, initializer bits, root mapping, nor live collector state.

Public `new` results reuse the existing one-live-token policy without exposing raw handles.
The store checks that the dynamic object is exactly the declared array type, retains the
producer/collector, and roots it through the same reusable checked slot contract. This is
result-only ownership: non-null array parameters, globals, hosts, cross-instance values,
multiple simultaneous results, and snapshots remain closed.

At the iteration-41 boundary, strict `gc/array.wast` accounting was 61 commands / 4 modules /
9 assertions / 6 invalid / 3 exact gates / 32 blocked commands, with zero hidden failures. The
remaining leaders were
numeric-default (including two globals and `array.new`), packed-data/drop lifecycle, and
reference-element/element-drop/barrier lifecycle. Fixed set/get measures 379.4-381.5 ns/op;
fixed result issue/release measures 462.8-488.2 ns/op; all samples are 0 B/op and
0 allocs/op. The fixed product is 268 Wasm bytes / 2,113 linked code bytes / 2,712 codec
bytes; the synthetic product is 146 / 1,247 / 1,527 bytes. Both fixed and synthetic i32/f32
length-two arrays occupy 24 bytes including the 16-byte header. Fixed layouts remain
`Compiled=712`, `Instance=792`, `compiledCodeCache=64`, `gcArrayGlobalInit=48`,
`compiledMemoryDirectory=120`, and `gc.Collector=640` bytes.

### Iteration 42 numeric-default roots and packed data

Iteration 42 closes two more exact official leaders without introducing a general reference-
array path:

- the 250-byte numeric-default leader records one uniform f32 initializer and one default f32
  initializer in two bounded 48-byte compile-only records. Each three-element array is 32
  bytes. Instantiation installs the first checked global slot before allocating the second,
  so Throughput/Tiny collection sees the earlier immutable root; a 64-byte Tiny heap fits the
  pair exactly, while a 32-byte heap fails deterministically on the second allocation and
  rolls the instance back;
- native `array.new` passes one numeric fill value plus length through the parked helper. It
  allocates once with `gc.EmptyRoots{}` and performs only non-collecting scalar stores. The
  complete get/set/len and bounds action set executes, and both non-null public results use
  the unchanged exact dynamic-array/one-live-token contract; and
- the 351-byte packed-data leader admits only i8 `array.new_data`. Before allocation, the
  helper reads the per-instance passive descriptor's current length, widens source plus length
  to u64, rejects overflow/out-of-range with a linear-memory trap, and verifies the retained
  immutable bytes. Only then does it allocate one 24-byte array and copy scalar bytes. Native
  code carries no Go pointer. `data.drop` sets the descriptor length to zero; non-empty reads
  trap without allocation, while source zero/length zero still creates the required empty
  array. Signed/unsigned packed loads, truncating scalar stores, bounds traps, Tiny exhaustion,
  trap atomicity, and codec sidecar loss are covered.

These are still empty-live-frame-ref allocations. The two default arrays are collector global
roots only after their individual initialization, and packed passive bytes are not GC roots.
No object or bulk reference barrier is needed for f32/i8 storage. Public result tokens are
created only after native return, reuse one checked slot, and never authorize non-null ingress,
multiple live results, globals/hosts, cross-instance values, or snapshot persistence.

Strict `gc/array.wast` accounting is now 61 commands / 6 modules / 29 assertions / 6 invalid /
1 exact gate / 12 blocked commands, with zero hidden failures. Only the reference-element
leader remains: its element segment contains two allocated i8 arrays, `array.new_elem` must
keep those refs live while allocating/copying the outer array, mutable reference stores need
object/card barriers, and `elem.drop` must withdraw segment roots coherently.

The numeric-default product is 250 Wasm bytes / 1,937 linked code bytes / 2,551 codec bytes;
the packed-data product is 351 / 2,863 / 3,585 bytes. Three-element f32 and i8 arrays occupy
32 and 24 bytes respectively, including the 16-byte header. Packed `get_u` measures
311.6-315.9 ns/op across five 500 ms samples, all 0 B/op and 0 allocs/op. Fixed layouts remain
`Compiled=712`, `Instance=792`, `compiledCodeCache=64`, `gcArrayGlobalInit=48`,
`compiledMemoryDirectory=120`, and `gc.Collector=640` bytes. Codec v27 is unchanged and reload
inherits no helper/product/global-root/data-lifecycle/token admission.

### Iteration 43 passive reference elements and complete `gc/array`

Iteration 43 closes the reference-element leader without generalizing native frame scanning.
The exact 396-byte/SHA-pinned product has one passive segment containing two allocated i8
arrays. Compilation retains a separate 96-byte non-serialized constructor record; instantiation
allocates the 24-byte inner objects in order and installs each in a checked collector table slot
before the next allocation. A 48-byte Tiny heap fits the pair exactly, while a 24-byte heap fails
on the second allocation and withdraws the first root. The per-instance 56-byte segment state is
separate from immutable-global mappings and carries only compact refs, slot indexes, one
arena-owned 16-byte descriptor, and a reusable two-root allocation record.

`array.new_elem` checks current descriptor length and widened u32 source+length before allocation.
For non-empty copies it publishes the selected segment values through the fixed mutable
`gcArrayElementRoots`; the collector rereads the first root after allocation before initializing
non-null reference arrays. The helper then writes every selected ref with `ArraySet` (object and
card barriers) and invokes the post-write bulk barrier. Nullable `$nvec` and mutable-any `$avec`
widening are checked against the exact dynamic inner-array type. Zero-length construction remains
valid after drop even for non-defaultable destination element types. No Go pointer enters native
state, and the reusable root record removes per-call root-interface allocations.

`elem.drop` parks through the same serialized helper boundary, zeros descriptor length, and nulls
both collector table slots through slot barriers. Non-empty and overflowing post-drop construction
trap before collector allocation. Reference get/set, nested packed reads, length, bounds traps,
public type-1 array tokens, one-live-token enforcement, producer-before-token close order,
Throughput/Tiny mark/remark stress, codec-sidecar loss, snapshot rejection, guard rejection, and
arm64 compile-only gates are covered. Complete `gc/array.wast` accounting is now 61 commands /
7 modules / 41 assertions / 6 invalid / zero gates / zero blocked commands.

The product is 396 Wasm bytes / 3,507 linked code bytes / 4,478 codec bytes. Both inner arrays and
the two-element outer reference array occupy 24 bytes including the header. Fixed layouts remain
`Compiled=712`, `Instance=792`, and `gc.Collector=640`; at that iteration
`compiledMemoryDirectory` was 128 bytes and the lazy `instancePluginState` was 136 bytes. Five
500 ms nested-get samples measured 6.309-11.634 us/op, 0 allocs/op, and 4-8 amortized B/op from
collector backing growth.

### Iteration 44 collector-free i31 immediates

Iteration 44 completes the official `gc/i31.wast` file without allocating or scanning heap
objects. All seven exact binaries are pinned by size, SHA-256, decoded type/state graph, opcode
inventory, and ordered action stream. Strict schema-2 accounting is gap-free at 80 commands /
7 modules / 65 assertions / zero invalid or malformed commands / zero gates / zero blocked
commands.

The internal value remains the existing 32-bit `gc.Ref` word: `(low31 << 1) | 1`. amd64 lowers
`ref.i31` to a 32-bit shift/tag pair, `i31.get_u` to a logical right shift, and `i31.get_s` to an
arithmetic right shift. Both gets trap on zero before decoding. Exact `ref.cast i31ref` checks
nullability and the low-bit tag; the admitted anyref products can contain only null or tagged
i31 immediates, never low-bit-zero object handles.

Literal i31 globals persist their tagged bits directly. One imported immutable i32 global is
re-evaluated through a validated `global.get; ref.i31` program at instantiation. A separate
8-byte compile-only table-initializer record names the sole imported global for the exact
three-entry i31 table product; codec v27 deliberately does not serialize that live admission
sidecar. i31 and the pinned anyref table use 8-byte entries and execute size/get/grow/fill/copy/
init through the existing bounded table descriptor and passive-element lifecycle. No collector,
root slot, remembered set, card, barrier, or heap arena is created, even under Throughput/Tiny
stress configuration.

The public product category is `ValI31Ref`/`I31Ref`, not `ValAnyRef`/`GCRef`. High-level `Call`
returns typed signed/unsigned accessors; low-level `Invoke` retains the signature-defined compact
slot. A raw tagged immediate cannot be released as an opaque `GCRef` token. Public feature
admission remains disabled, and no official product accepts an i31 reference parameter or host
boundary.

Measured Wasm/code/codec sizes are 252/1,086/1,558 bytes for the core leader; 259/1,455/1,901
for the i31 table; 96/206/360 for the imported-global table initializer; 88/154/309 for the
imported-global global initializer; 131/414/635 for anyref globals; and 262/1,503/1,954 for the
anyref table. `gcI31TableInitializer` is 8 bytes. Adding its pointer grows the lazy compiled
memory directory from 128 to 136 bytes; fixed `Compiled=712`, `Instance=792`,
`compiledCodeCache=64`, and `gc.Collector=640` remain unchanged. Five 500 ms samples measured
core `get_u` at 34.63-35.18 ns/op and anyref-table get/cast at 35.01-35.78 ns/op, all 0 B/op and
0 allocs/op.

### Iteration 45 bounded dynamic reference tests

Iteration 45 adds strict schema-2 accounting for both official `gc/ref_test.wast` binaries. The
inventory pins canonical source and command lines, byte sizes, SHA-256 identities, decoded type/
state graphs, every opcode, and all ordered actions. It contains 73 commands: two valid module
leaders are explicitly gated and their 69 actions remain blocked, with zero invalid/malformed
commands and zero hidden failures. The 626-byte abstract leader combines null, i31, struct, array,
funcref, externref, three tables, allocation, conversion, and mutation. The 976-byte concrete
leader creates eight struct dynamic types and performs 84 subtype/canonicalization tests. Neither
is admitted merely because a smaller primitive now executes.

Validation now treats ordinary `ref.test` compatibility by top-level reference hierarchy. Defined
struct siblings, and struct/array members of the data hierarchy, are legal dynamic tests even when
neither type is a subtype of the other; the runtime answer may be false. Data, function, extern,
exception, and string hierarchies remain disjoint, so cases such as funcref versus i31 still reject
strictly. Descriptor-form tests retain their narrower descriptor compatibility rule.

Execution is intentionally smaller than either official leader. One 255-byte/SHA-pinned product
contains only `ref.null`, `ref.i31`, and `ref.test`, has numeric signatures, no tables/globals/
imports/objects, and reaches amd64 only through the existing private i31 product gate. Nullable
null tests return true, non-null null tests return false, tagged i31 values match `i31`, `eq`, and
`any`, and do not match `struct` or `array`. The lowering checks the low tag; a low-bit-zero non-null
word is never accepted as an i31 immediate. Because the product has no reference parameters,
imports, storage, or object constructors, no host/raw non-null ingress can manufacture such a word
on the admitted path.

Throughput and Tiny instantiation both keep `Instance.gc == nil`. No helper transition, root,
barrier, card, remembered set, descriptor sidecar, or heap allocation is added. Codec v27 remains
unchanged: the exact type/code metadata persists, but private reload inherits no product bit and
fails required-feature admission before mutation. Public compilation, snapshots, signal bounds,
and arm64 execution remain fail-closed. The product measures 255 Wasm bytes / 996 linked code
bytes / 1,292 codec bytes. Five 500 ms samples measured the two-test i31 function at
36.58-37.34 ns/op, 0 B/op, and 0 allocs/op. Fixed `Compiled=712`, `Instance=792`,
`compiledCodeCache=64`, `compiledMemoryDirectory=136`, and `gc.Collector=640` layouts remain
unchanged.

### Iteration 46 rooted object tables and concrete dynamic tests

Iteration 46 separates collector type semantics from product admission. `Collector.RefTest`
classifies null, tagged i31 immediates, struct objects, array objects, and defined targets without
consulting public token state. Defined tests walk only the validated declared-super chain. Invalid
heap targets and closed, stale, or forged object refs return errors instead of becoming false
matches. `TypeCanonicalization` is a collector-bound immutable representative map constructed once
at instantiation; `RefTestCanonical` compares each visited declared type through that map, preserving
ordinary super traversal while allowing the official duplicate structural types to share canonical
identity. The raw and canonical paths measure 31.97–34.81 ns/op and 25.57–26.19 ns/op respectively,
all at 0 B/op and 0 allocs/op.

A separate 168-byte/SHA-pinned product proves the table lifecycle before official admission. Its
single two-entry `(ref null struct)` table uses compact eight-byte native entries paired with two
checked collector `TableSlot`s. Native `table.set` parks before mutation; the helper validates the
index and compact ref, updates the collector slot through `SetTableSlot`, then writes the arena-owned
native entry. Rejected forged or out-of-bounds writes leave both representations unchanged. The first
allocation is stored and rooted before the next allocation, repeated `init` calls overwrite through
slot barriers, and instance close nulls every slot before closing the collector. A 24-byte Tiny heap
commits the first Wasm table store and then fails the second allocation deterministically, preserving
normal Wasm trap-side-effect ordering. The product is 168 Wasm / 1,462 linked code / 1,832 codec
bytes; `gcRefTestTableState=120` and lazy `instancePluginState=144` bytes. Its parked defined test
measures 146.9–148.5 ns/op, 0 B/op, and 0 allocs/op.

The same fixed sidecar admits the official 976-byte concrete leader. Twenty checked slots are created
at instantiation; each exported call allocates and stores the same eight dynamic struct values into
slots 0–4 and 10–12 before running its tests. Throughput and Tiny repeatedly execute both exports,
and a full collection retains exactly those eight rooted objects. The immutable nine-entry canonical
map covers the eight struct definitions plus the function sentinel; `$t1/$t1'` and `$t2/$t2'` share
representatives while all other declared identities remain distinct. All 84 reached `ref.test`
instructions satisfy subtype and canonicalization behavior. The official product is 976 Wasm /
16,981 linked code / 17,563 codec bytes. Codec v27 persists descriptors/code but not the exact product,
canonical map, checked slots, collector, or helper admission; loaded artifacts fail required-feature
validation. Snapshots, guard mode, public admission, and arm64 execution remain fail-closed.

### Iteration 47 mixed table ownership and extern conversion

The exact 626-byte abstract leader uses a finite ownership split instead of a universal reference
word. Its ten-entry anyref table pairs eight-byte arena words with checked collector slots; i31 and
null values remain immediate/non-owning, heap objects occupy slots, and opaque foreign-any words
leave the corresponding collector slot null. Its funcref table uses existing 32-byte native local
descriptors and never enters the collector. Its externref table stores either validated public store
tokens or internal conversion identities and never reuses public `GCRef` bits.

`gcExternConversionState` is fixed at eight entries. `any.convert_extern` maps a valid foreign public
externref to a stable opaque internal any word and reverses internal i31/object conversions.
`extern.convert_any` maps a foreign-any word back to its original public token or creates a stable
opaque internal extern word for i31/object data. Converted objects receive checked table roots.
Extern-table replacement withdraws the final old conversion root and reuses the fixed entry; forged,
foreign-store, stale, closed, and capacity-exhausted operations reject explicitly. Null remains zero.
Stable object round trips measure 19.70–21.04 ns/op, 0 B/op, and 0 allocs/op.

A 96-byte Tiny heap executes 100 repeated `init` calls. The anyref struct and zero-length array plus
the converted struct remain as exactly three live objects after full collection. The state remains
at three conversion records: one foreign extern, one i31, and one current object. Parked anyref and
externref stores preflight bounds/ownership before mutation, so forged and out-of-bounds writes leave
both native words and roots unchanged. Instance close releases private-store membership, then resource
teardown nulls all checked roots, closes conversion ownership, zeros all three tables, and closes the
collector. The product measures 626 Wasm / 7,416 linked code / 8,087 codec bytes; `gcRefTestTableState=200`, `gcExternConversionState=352`, and lazy
`instancePluginState=144` bytes. Foreign-any `ref.test` measures 171.7–172.5 ns/op, 0 B/op, and
0 allocs/op.

Strict `gc/ref_test.wast` accounting is gap-free at 73 commands / 2 passed modules / 68 passed
assertions / 0 invalid / 0 malformed / 0 gates / 0 blocked / 0 hidden failures. Codec v27 persists
none of the conversion entries, roots, local descriptor ownership, helper state, or exact admission.
Public compile/load, snapshots, guard mode, and arm64 remain fail-closed. No native frame chain is
published: every allocated object is stored or converted before the next may-collect helper.

### Iteration 48 exact `gc/extern` product

The sole official leader is pinned at 286 bytes and SHA-256
`5ad921ebe511ca9e23c137aef6883113684896f15b8a9726d5d77524d562f823`. Its two immutable globals
are `extern.convert_any (ref.null any)` and `any.convert_extern (ref.null extern)`. Validation accepts
those conversion instructions in constant expressions only behind the staged GC constant-expression
feature; the exact evaluator verifies the source heap, conversion opcode, result type, `end`, and no
trailing bytes before folding null to zero. Default/public validation remains closed.

The product has one ten-entry anyref table. Checked collector slots own only low-word object refs;
null and i31 are immediate/non-owning, while a high-word foreign-any identity leaves its collector
slot null. `init` allocates an empty struct and a zero-length i8 array and stores each returned ref
before the next may-collect helper. A 48-byte Tiny heap repeats the action 100 times and full
collection retains exactly those two table objects.

The fixed conversion owner remains eight entries but each data entry now carries four distinct
identities around its compact ref: an internal data-to-extern word, a bounded public any token, a
bounded public extern token, and the compact null/i31/object word itself. Foreign entries carry an
ordinary store extern token plus a distinct internal foreign-any word. Public data tokens are random
high-word identities owned only by this exact instance conversion state; they are neither store
extern tokens nor opaque `GCRef` object tokens. Stable public ingress maps them back to internal words
before native execution, and egress maps internal words back to the stable public identity. Forged,
foreign-store, stale, full, and closed cases reject before native mutation.

All official host/null/i31/struct/array conversions execute. Strict accounting is 19 commands /
1 module / 16 assertions / 0 gates / 0 blocked / 0 hidden failures. The product is 286 Wasm bytes /
2,102 linked code bytes / 2,712 codec bytes. Sidecars are `gcRefTestTableState=200`,
`gcExternConversionEntry=56`, `gcExternConversionState=480`, and lazy `instancePluginState=144`.
Raw stable conversion measures 20.96–21.19 ns/op; the staged public round trip measures
144.2–147.8 ns/op; all samples report 0 B/op and 0 allocs/op. Codec v27, snapshots, guard mode,
public family admission, and arm64 inherit no product, conversion identity, root, or helper state.
No native frame is published, so this proof does not authorize arbitrary live local refs or broader
host/cross-instance GC ownership.

Before broader live `gc.Ref` payloads or funcref lifetimes can be admitted, codegen/runtime
must still prove all of the following as one coherent product:

1. publish and withdraw the active native frame chain at exact collector safepoints;
2. copy `gc.Ref` payloads with the required allocation/store barriers and Tiny remark
   semantics, including catch, rethrow, tail-frame discard, trap recovery, and teardown;
3. retain/release foreign funcref producers across cross-instance exception lifetime,
   including close order and rollback;
4. scan only live records, never stale slots belonging to discarded try scopes;
5. add tag-discriminated maps or reject catch-all products whose payload positions do not
   have one uniform ownership kind;
6. serialize or explicitly reject every snapshot/codec/live-handler context; and
7. prove bounded no-cgo behavior under stress collection and concurrent consumers.

Until those obligations are executable and tested, the maps remain descriptive collector
metadata outside the narrow local lifecycle product. WasmGC opcodes, GC-managed EH
payloads, exported exception references, and public/guard/arm64 admission remain fail-
closed.

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
lists on full collection. `ThroughputClassLimit` must be zero for the default or
exactly one of those size classes (`32` through `32768` bytes today);
unsupported below-minimum, above-maximum, or between-class values such as `4097`
are rejected at collector construction rather than rounded. Larger objects use a
coalescing free-span list.

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
allocation. Iterations 38-39 connect exact numeric-struct parked-Go helper products,
including rooted immutable globals and packed fields; broader WasmGC opcode lowering and
backend integration remain incomplete.
It is also not a replacement for the future GenImmix/default policy; it is a
bounded, predictable option for constrained targets where a fixed maximum heap,
stable object addresses, compact metadata, and deterministic allocation failure
are more important than moving/generational throughput.

Known Tiny limitations in this foundation:

- first-fit allocation is simple and deterministic but not fragmentation-optimal;
- collection is incremental by explicit `Step` calls or allocation-time stress
  knobs, not concurrent;
- handle-table entries remain the stable ref indirection;
- only exact numeric struct products are wired: allocation-local empty-root shapes plus
  two immutable collector-rooted globals. General frame publication, mutable root
  synchronization, and reference barriers remain unwired.

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

The general codegen integration should therefore lower WasmGC heap operations to
helper calls with exact roots. Iterations 38-39's hash-pinned numeric products reuse the
parked-Go control frame plus exact immutable collector global slots as narrower bootstraps;
they do not replace this
backend-neutral contract:

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
backends. The emitter root protocol is ordered: `SpillLiveRefs` prepares root
storage without publishing it; `PublishRoots` must be all-or-nothing, and a
publish error means no roots are live and `UnpublishRoots` is skipped; after a
successful publish, `HelperHeap` calls `UnpublishRoots` exactly once even when
the runtime helper fails. If both the runtime helper and unpublish fail, the
runtime-helper error is returned; if only unpublish fails, the unpublish error is
returned.

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

Global and table root-slot constructors accept only nullable stored refs: `null`, `i31`, or a live object ref owned by the same collector. Checked constructors (`NewCheckedGlobalSlot` and `NewCheckedTableSlot`) are the safe API for production decode/instantiation: they return errors and do not append a slot when they see a forged, stale, or cross-collector ref. The exported convenience constructors (`NewGlobalSlot` and `NewTableSlot`) are trusted/test setup wrappers that delegate to the same validation and panic on invalid initial refs, so there is no root-slot creation path that silently installs invalid metadata. Checked setters validate later slot stores and prune stale nursery slot cards when a slot is overwritten with a non-nursery value.

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

Generated code must call object barriers for `struct.set` and ref array stores, slot barriers for `global.set` and `table.set`, and bulk barriers for array initialization/copy/fill paths that write refs. `SlotFrame` barriers remain unsupported until exact frame-root maps exist; frame roots must be supplied through explicit root publication/safepoint machinery instead of recorded as slot cards. `BulkWriteBarrier` is a post-write barrier: generated helpers must store or copy refs into the destination array range before invoking `BulkWriteBarrier`/`PostBulkWriteBarrier`. A pre-write bulk barrier cannot observe newly written nursery refs and is not a safe substitute.

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

- WasmGC opcode validation is not complete. Exact staged numeric structs plus complete official
  `gc/array.wast` and `gc/i31.wast` execution are wired to amd64: bounded array constructors and
  access, immutable/passive roots and barriers, plus collector-free i31 encode/get/cast,
  global/initializer, and compact-table lifecycle. General casts/tests, array fill/copy/init,
  extern conversion, reference struct fields, and broader GC constant expressions remain.
- The parked-Go runtime-call ABI is proven for exact empty-frame-root numeric/packed
  allocations, non-collecting numeric access/mutation, ordered immutable collector-rooted
  globals, per-instance passive data descriptors, and one result-token root installed only
  after the native call returns. General allocation with live frame refs, passive-element GC
  roots, field/element object+bulk barriers, and traps still need the backend-neutral root-
  publication ABI before broader generated code can use objects.
- Exact native safepoint maps are not connected to compiled frames yet.
- Minor collection currently promotes marked nursery survivors through handles
  rather than implementing a final copying nursery/root-update path.
- The Throughput old/large allocator reuses freed memory, but full Immix
  line/block marking, compaction, and more advanced fragmentation control remain
  future work.
- The Throughput heap currently uses growable Go byte slices, so native code
  must not cache raw heap payload pointers; see `docs/runtime-abi.md`.
- Tiny and Throughput profiles are connected to the exact staged numeric-struct and
  pointer-free array helper paths, including immutable rooted globals, packed struct fields,
  packed data/drop lifecycle, bounded public-token rooting, stress collection, and
  deterministic Tiny exhaustion. Reference arrays and broader WasmGC opcode/backend lowering
  remain closed.

These limitations are intentional for this commit series: the runtime foundation
is small, exact, typed, and no-cgo, giving later codegen work stable contracts.
