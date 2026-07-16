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
strict null conversion constants, and executes the complete `gc/extern` family. Iteration 49 reuses the
checked compact-table owner for the exact official eqref product and executes the complete null/i31/object
identity matrix without adding native frame publication. Iteration 50 adds identity-preserving ordinary
reference casts for the two exact official leaders, with a dedicated cast-failure trap, canonical defined-type
matching, and the same bounded table/conversion ownership. Iteration 51 closes both official branch-cast
families with exact label-prefix/result refinement, nested control ordering, and identity-preserving selected
edges/fallthrough over the same bounded owners. Iteration 52 closes the official packed `array.fill` and
`array.copy` leaders through non-collecting bulk helpers with full preflight, overlap-safe copy, reference
barrier/Tiny remark proof, and one exact post-return mutable-global root reconciliation. Iteration 53 closes
numeric `array.init_data`; iteration 54 closes exact local-funcref `array.init_elem` through non-scanned
64-bit descriptor identities, complete preflight, two rooted arrays, and Tiny224 lifecycle proof. Iteration 55
pins the complete 170-command `gc/type-subtyping.wast` graph before admission. Its 45 valid leaders carry only
metadata or function identities and allocate no struct/array object. Iteration 56 closes the two valid recursive-
projection rejects and all fourteen invalid subtype/finality/storage/variance acceptances on both validator paths.
Iteration 57 admits the first six declaration graphs and two recursive-function-body leaders through a new exact
SHA-pinned no-object product, separate from iteration 37. Iteration 58 adds the next six immutable local `ref.func`-
global leaders under their own exact class and canonical descriptor-lifetime proof. Iteration 59 adds four single-result
function-only `ref.test` leaders. Iteration 60 adds three multi-result all-true leaders with ordered 2/4/8 i32 results.
Iteration 61 adds the final two function-only leaders, each returning zero, under a separate exact recursive-chain
class that preserves the false source-to-target direction across sibling declared-super edges. The same compile-only
local `ref.func` provenance and validator structural subtype relation fold every answer without routing descriptor
addresses through compact-GC classification. Iteration 62 adds the exact recursive runtime call/cast leader without
changing that category boundary: its local funcref table stores ordinary canonical descriptors, and generated subtype-
aware `call_indirect`/`ref.cast` checks compare those identities directly instead of invoking a compact-GC helper.
Iteration 63 adds the separate finality-sensitive leader: structurally identical open and final `() -> ()` descriptors
remain identity-distinct in both directions under the same finite checks. Iteration 64 adds a separate exact typed-table
leader: a fixed nullable `$t1` table stores local `$t1` and `$t2` identities under `$t2 <: $t1 <: $t0`, executes five
valid widening/exact indirect calls, and preserves two narrowing/unrelated signature traps. Iteration 65 adds the first
exact cross-instance subtype link pair with bounded descriptor ownership, duplicate-owner deduplication, rollback, and
both close orders. Iteration 66 adds only the separate finality-sensitive link provider and its two inverse unlinkable
consumers; open and final `() -> ()` identities remain incompatible in both directions and failed imports retain nothing.
All twenty-nine admitted leaders instantiate, or fail before consumer publication, without allocating a collector. The
finality provider owns 96 descriptor bytes, emits 157 code bytes, produces a 323-byte codec artifact, and measures
36.50–37.43 ns/op with zero allocations; each 38-byte consumer has a bounded 64-byte descriptor requirement and a
144-byte unlinked codec artifact. Sixteen leaders remain gated and 18 dependent commands remain blocked. General native
frame publication, object-valued mutable/reference globals, broader struct-defined linking ownership, public ownership,
and snapshots remain incomplete. These bounded products must not be presented as general executable WasmGC support.

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

### Iteration 49 exact `gc/ref_eq` product

The sole official leader is pinned at 197 bytes and SHA-256
`46b2bd3e4597ba5a871472aa14f5777df18b722b7f3283ba1fc946f4791a3adb`. Its nine-member type graph
contains two base structs, four declared struct subtypes, one i8 array, and two function types. The
module owns one twenty-entry `(ref null eq)` table, `init`, and a two-index `eq` function. Six separate
invalid binaries are pinned and continue to reject because `ref.eq` accepts only eqref operands, not
anyref, funcref, or externref in nullable or non-null form.

The product reuses `gcRefTestTableState` without a conversion owner. Null and i31 values occupy direct
compact words. Struct and array values occupy low-bit-zero compact handles paired with checked collector
`TableSlot`s. `init` creates two empty structs and two zero-length arrays; each returned handle is stored
and rooted before the next allocation can collect. Equality itself is the existing direct 64-bit value
comparison: nulls compare equal, equal i31 payloads compare equal, and objects compare only by stable
handle identity. Distinct objects remain unequal even when their dynamic type and contents agree. The
official file has no host/foreign-any operands, so this slice makes no claim about comparing public token
bits or foreign conversion identities.

An 80-byte Tiny heap repeats `init` 100 times with collect-every-allocation and retains exactly the four
current table objects after a full collection. The extra 16 bytes above the four-object 64-byte live set
allow one replacement allocation before its destination slot withdraws the old root. Forged object
handles and out-of-bounds writes reject before table mutation. Close withdraws all checked roots before
collector teardown. Codec v27 preserves type/table/code metadata but inherits no product, helper, table
root, or live compact identity; snapshots, guard mode, public family admission, and arm64 execution remain
closed.

Strict schema-2 accounting is gap-free at 90 commands / 1 module / 81 assertions / 6 invalid / 0 gates /
0 blocked / 0 hidden failures. The product is 197 Wasm bytes / 1,846 linked code bytes / 2,334 codec bytes.
Fixed sidecars remain `gcRefTestTableState=200` and lazy `instancePluginState=144`; fixed layouts remain
`Compiled=712`, `Instance=792`, `compiledCodeCache=64`, `compiledMemoryDirectory=136`, and
`gc.Collector=640`. Five 500 ms samples of stable i31 equality measure 45.53–49.41 ns/op, all 0 B/op and
0 allocs/op. This proof adds no frame roots, public GC token ingress, conversion identity, canonical map,
or serialized live state.

### Iteration 50 exact `gc/ref_cast` products

The complete official `gc/ref_cast.wast` file contains two valid leaders and no invalid or malformed
commands. The 380-byte abstract leader owns one ten-entry anyref table and initializes null, i31, one
empty struct, one zero-length i8 array, one foreign-any conversion, and three typed nulls. Its forty
assertions distinguish `ref.as_non_null`'s `null reference` trap from ordinary nullable/non-null
`ref.cast` success and the dedicated `cast failure` trap. The 512-byte concrete leader allocates the
same eight declared-super/canonical struct values used by the concrete dynamic-test proof, then executes
all raw-super and canonical casts in two actions. Strict schema-2 accounting is gap-free at 47 commands /
2 modules / 40 assertions / 3 actions / 0 invalid / 0 malformed / 0 gates / 0 blocked / 0 hidden failures.

`Collector.RefCast` and `RefCastCanonical` reuse the validated dynamic classification walk but return the
original compact `gc.Ref` unchanged on success. A valid mismatch returns `gc.ErrCastFailure`; stale,
forged, closed, unknown-target, and wrong-owner cases retain their specific errors and are never collapsed
into a semantic cast mismatch. The mixed table owner extends that rule to its 64-bit internal domain:
foreign-any identities may cast only to `any`, successful casts return the exact original high-word
identity, and forged words fail ownership validation. Compact null/i31/object words, internal foreign-any
identities, public any/extern tokens, store extern tokens, funcref descriptors, and opaque public `GCRef`
tokens remain distinct categories.

The amd64 helper receives one copied reference word, signed heap target, and nullable bit. It performs no
allocation or collection. On success it returns the same word; on mismatch it raises runtime trap code 18,
`cast failure`. The abstract initializer still stores the struct before allocating the array and stores the
array before conversion, so no live local ref crosses a may-collect helper. A 48-byte Tiny heap repeats
initialization 100 times and retains exactly the two current 16-byte objects. The concrete initializer
stores each of eight allocations immediately; a 256-byte Tiny heap retains all eight, and all later cast
helpers are non-collecting. Cast results are dropped before any later helper. Neither product publishes a
native frame chain.

The abstract product reuses `gcRefTestTableState=200`, `gcExternConversionState=480`, and lazy
`instancePluginState=144`; the concrete product uses the same table/plugin sidecars without a conversion
owner. Fixed layouts remain `Compiled=712`, `Instance=792`, `compiledCodeCache=64`,
`compiledMemoryDirectory=136`, and `gc.Collector=640`. Product sizes are 380 Wasm / 4,445 linked code /
4,916 codec bytes for abstract and 512 / 8,684 / 9,263 for concrete. Five 500 ms samples of the stable
parked i31 cast measure 177.9–183.8 ns/op, all 0 B/op and 0 allocs/op. Codec v27 persists the type/table/code
metadata and trap-bearing native code but no exact product enum, canonical map, conversion identity,
checked root, compact table value, or collector state. Private reload therefore loses admission; snapshots,
guard mode, public GC admission, and arm64 execution remain fail-closed.

### Iteration 51 exact `gc/br_on_cast` and `gc/br_on_cast_fail` products

Both official files contain three valid leaders and six invalid modules. Complete schema-2 accounting is
gap-free for each file at 40 commands / 3 modules / 25 assertions / 6 invalid / 0 malformed / 0 gates /
0 blocked / 0 hidden failures. The abstract leaders each own one ten-entry anyref table and initialize null,
i31, a one-field i16 struct, a length-three i8 array filled with 5, and one foreign-any conversion. The
concrete leaders reuse the eight declared-super/canonical struct values and twenty checked table slots.
The third leader in each file has no actions and exists to prove source/target nullability and outer-label
result typing. All twelve invalid modules remain exact `type mismatch` obligations.

Validation now models a nullable target precisely: null takes the successful cast edge, so the failed edge is
non-null even when the declared source is nullable. A non-null target leaves null in the failed source type.
For `br_on_cast`, the refined target travels on the branch and the failed source falls through. For
`br_on_cast_fail`, the failed source travels on the branch and the refined target falls through. Label-prefix
operands are transferred independently of the appended reference, including the nested struct/array blocks.

amd64 decodes the flags, depth, source heap, and target heap directly. It keeps the original 64-bit reference
word on the logical operand stack and passes only a copied word to the existing parked `ref.test`
classification helper. The helper does not allocate, collect, mutate roots, or expose payload pointers. Its
i32 result selects the edge; both paths retain the original compact object/i31/null or internal foreign-any
identity byte-for-byte. Branch mismatch is ordinary control flow and never raises trap code 18. Forged,
stale, closed, or wrong-owner words remain helper errors rather than false matches.

The abstract initializer uses a new exact one-field numeric allocation helper: allocation receives
`gc.EmptyRoots{}`, initializes the packed i16 field before returning, and the returned object is stored in its
checked table slot before the later array allocation. The array is likewise stored before conversion or any
later helper. A 72-byte Tiny heap repeats initialization 100 times and retains exactly the two current 24-byte
objects while permitting one replacement allocation. The concrete products use Tiny256 and retain exactly
eight table-rooted objects. The nullability-only products instantiate with a collector for their defined
struct descriptor but create no table state or object.

Measured product sizes are 385 Wasm / 3,663 linked code / 4,226 codec bytes for abstract `br_on_cast`,
403 / 3,663 / 4,242 for abstract `br_on_cast_fail`, 772 / 11,409 / 11,989 for concrete `br_on_cast`,
876 / 14,367 / 14,948 for concrete `br_on_cast_fail`, 111 / 948 / 1,237 for the `br_on_cast` nullability
leader, and 103 / 862 / 1,094 for the fail variant. Five 500 ms stable i31 branch samples measure
124.2–127.0 ns/op, all 0 B/op and 0 allocs/op. Sidecars remain `gcRefTestTableState=200`,
`gcExternConversionState=480`, and lazy `instancePluginState=144`; fixed layouts remain `Compiled=712`,
`Instance=792`, `compiledCodeCache=64`, `compiledMemoryDirectory=136`, and `gc.Collector=640`.

Codec v27 persists the type/table/code/control metadata but no exact branch product, helper admission,
canonical representatives, conversion identity, checked roots, compact/internal table values, or collector
state. Private reload therefore loses admission. Snapshots, guard mode, public GC admission, and arm64
execution remain fail-closed. No native frame chain is published: the only live reference across the parked
classification call is the original branch operand, and that helper is proven non-collecting; every allocation
is stored before the next may-collect helper.

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

### Iteration 52 exact bulk array fill/copy products

The official `gc/array_fill.wast` and `gc/array_copy.wast` files are gap-free under staged admission.
Combined schema-2 accounting is 54 commands / 2 modules / 43 assertions / 7 invalid / 0 malformed /
0 gates / 0 blocked / 0 hidden failures. The 183-byte fill leader and 402-byte copy leader each own one
immutable length-12 i8 array and one mutable length-12 i8 array. Validation now rejects immutable fill/copy
destinations and requires packed storage equality or ordinary source-to-destination value subtyping before
execution.

`Collector.ArrayFill` and `Collector.ArrayCopy` are non-allocating and non-collecting. Both resolve and
validate the complete object/range/value contract before the first write. Fill preserves packed truncation.
Copy validates both arrays and all reference payloads before mutation, then uses forward or backward
iteration for exact memmove overlap without allocating a temporary slice. Reference writes use the existing
object and element-card barriers per store and invoke the post-write bulk barrier only after the destination
range contains the new references. Throughput tests prove remembered/card publication; a Tiny test advances
a rooted parent to remark/black, fills an otherwise unrooted child, and proves the child survives the cycle.
The official products contain only packed i8 arrays, so the reference tests establish the helper contract
without widening those exact product hashes.

The two globals are created and installed in checked collector slots transactionally. `array.fill` mutates
only the rooted object payload. The copy overlap functions allocate one replacement array with explicit
empty roots, perform only the non-collecting copy helper while that local is live, and execute `global.set`
as the final native operation. After successful return, only the exact bulk-copy product synchronizes the
bounded two-entry global mapping from the compact native cells into checked collector slots. No later
may-collect helper can observe the new cell before reconciliation. Tiny96 repeats 100 alternating overlap
replacements and a full collection retains exactly the immutable array plus the current mutable array;
trapping copies leave the cell and slot unchanged.

The fill product measures 183 Wasm / 834 linked code / 1,220 codec bytes. The copy product measures
402 / 2,331 / 2,863 bytes. Five 500 ms fill samples measure 170.2–173.1 ns/op, all 0 B/op and
0 allocs/op. Fixed layouts remain `Compiled=712`, `Instance=792`, `compiledCodeCache=64`,
`compiledMemoryDirectory=136`, `gcArrayGlobalInit=48`, lazy `instancePluginState=144`, and
`gc.Collector=640`. Codec v27 persists type/global/data/code metadata but no bulk product/helper admission,
mutable cell/slot synchronization rule, compact live value, or collector state. Private reload loses staged
admission; snapshots, guard mode, public GC admission, and arm64 execution remain fail-closed. This exact
post-return rule does not authorize a mutable GC global followed by another allocation, host/cross-instance
transfer, or arbitrary non-null ingress.

### Iteration 53 exact numeric array data initialization

The official `gc/array_init_data.wast` file is gap-free under staged admission at 48 commands / 2 modules /
42 assertions / 2 invalid / 0 malformed / 0 gates / 0 blocked / 0 hidden failures. The companion
`gc/array_init_elem.wast` file is pinned and strictly validated but remains one explicit product gate with
19 blocked commands and three expected invalid modules. Combined schema-2 accounting is therefore
72 commands / 2 passed modules / 42 passed assertions / 5 invalid / 1 gate / 19 blocked, with no hidden
compile, instantiate, invoke, or result failures.

Validation consumes the array reference, destination index, source index, and element count for both init
instructions. `array.init_data` requires a mutable numeric or vector-storage array and a valid passive data
segment. `array.init_elem` requires a mutable reference array, a valid passive element segment, and source
reference subtyping into the destination element type. Malformed operand order, immutable destinations,
numeric/reference mismatches, bad segment indices, and incompatible element refs fail before lowering. The
remaining element product is not admitted merely because its validator is complete.

`Collector.ArrayInitData` is a non-allocating and non-collecting primitive. It resolves the exact destination
descriptor, checks `dstStart + length` in element units, multiplies by the fixed element width with u64
arithmetic, and checks `srcStart + byteLength` against the retained passive bytes before the first write.
Only after both ranges pass does it decode one-, two-, four-, or eight-byte little-endian values and store
them through the existing typed array setter. Packed i8/i16 storage returns canonical i32 values and preserves
truncation. A short i16/i32/i64 tail therefore traps without changing even the first destination element.
Zero length is accepted exactly at the destination/source end and rejected when either start is beyond its
end.

The parked amd64 helper copies six scalar words: destination compact ref, destination index, source byte
index, element count, exact array type index, and exact data-segment index. It verifies the product/type,
consults the live passive-data descriptor so `data.drop` is authoritative, bounds retained bytes by that
descriptor length, and invokes the collector primitive. The helper allocates no Go or collector object,
performs no collection, retains no ref or payload pointer, and needs no native frame publication. This proof
is exact to numeric data initialization; it does not authorize `array.init_elem`, whose reference stores
require segment-root ownership plus object/card/post-bulk and Tiny remark obligations.

The 335-byte leader owns three immutable arrays: two length-12 i8 arrays and one length-6 i16 array. The
compile-only global-root directory is deliberately capped at three entries only for the hash-pinned
init-data product; each object is installed into a checked collector slot before the next allocation. Tiny96
fits the three arrays exactly and repeats 100 i8/i16 initializations before full collection retains all three.
After `data.drop`, zero-length initialization succeeds and nonzero initialization traps. The 435-byte leader
allocates one temporary length-one i32 or i64 array per action. Tiny24 repeats 100 alternating width actions:
the allocation may collect before the new ref exists, while the subsequent init helper cannot collect, so no
live local ref crosses a safepoint. A trapping short source unwinds normally and the next action recovers.

The products measure 335 Wasm / 1,567 linked code / 2,140 codec bytes and 435 / 4,606 / 5,055 bytes.
Five 500 ms stable i8 samples measure 175.4–177.5 ns/op, all 0 B/op and 0 allocs/op. `Compiled=712`,
`Instance=792`, `compiledCodeCache=64`, `compiledMemoryDirectory=136`, `gcArrayGlobalInit=48`, and
`gc.Collector=640` remain unchanged. Enlarging the fixed global-root mapping from two to three entries grows
the lazy `instancePluginState` from 144 to 152 bytes; instances without plugin state still pay nothing.
Codec v27 persists no init product/helper/root admission or live passive descriptor state. Private reload,
snapshots, signal-backed bounds, public GC admission, and arm64 execution remain fail-closed.

### Iteration 54 exact funcref array element initialization

Iteration 54 closes the sole official `gc/array_init_elem.wast` leader without treating function
references as compact collector handles. The 268-byte/SHA-pinned module has two immutable global
arrays of length 12: one non-null indexed-function array initialized from local `ref.func`, and one
mutable nullable `funcref` array initialized to null. Both arrays are ordinary collector objects and
are installed in checked global slots before execution. Their payload values, however, are canonical
64-bit local function-descriptor identities. For this exact product the two runtime array descriptors
use non-scanned i64 storage, preserving the native funcref ABI and preventing the collector from
misinterpreting descriptor pointers as `gc.Ref` values.

Helper ID 30 copies six words in operand order: destination compact array ref, destination index,
passive source index, element count, destination type index, and element-segment index. Before the
first write it checks the exact product/type/segment, destination range, current descriptor length,
every selected passive descriptor identity, local descriptor-arena ownership, and structural source-
to-destination reference subtyping. A fixed twelve-word Go-stack buffer holds the preflighted
identities; no cache, map, heap allocation, collector allocation, or native-frame publication is
introduced. `Collector.ArrayInitWords` then stores the complete range only after preflight.

Collector object/card/post-bulk barriers are deliberately not emitted for these payloads: they are
function lifecycle identities owned by the executing instance, not guest heap references. The
instance itself owns the local descriptor arena for the entire activation, and this exact module has
no imports, cross-instance descriptors, host refs, mutable function storage outside the destination,
or escaping funcref result. A future product containing compact GC refs must use the ordinary
object/card/post-bulk contract instead; this exception must not be generalized by ABI category alone.

`elem.drop` zeros the live passive descriptor length. Non-zero post-drop initialization traps before
mutation, while zero length remains valid at the exact source/destination ends. Throughput and
Tiny224 repeat 100 initialize-and-call cycles, survive a full collection with exactly the two rooted
arrays, preserve local call identity after collection, and prove source-range trap atomicity. Strict
combined array-init accounting is now gap-free at 72 commands / 3 modules / 61 assertions / 5 invalid /
0 gates / 0 blocked / 0 hidden failures. The element product measures 268 Wasm / 1,683 linked code /
2,229 codec bytes; five 500 ms samples measure 213.4–219.2 ns/op, 0 B/op, and 0 allocs/op.

No fixed layout grows: `Compiled=712`, `Instance=792`, `compiledCodeCache=64`,
`compiledMemoryDirectory=136`, `gcArrayGlobalInit=48`, lazy `instancePluginState=152`, and
`gc.Collector=640` bytes remain unchanged. Codec v27 serializes structural types, globals, elements,
and code but no exact product, i64 descriptor reinterpretation, live function identity, helper bit,
checked roots, or dropped descriptor state. Private reload, snapshots, guard mode, public GC
admission, and arm64 execution remain fail-closed.

### Iteration 56 strict recursive component subtyping

The type-subtyping validator now checks every declared function, struct, and array super edge before any
collector or product decision. Function parameters are contravariant and results covariant; struct subtypes
retain the complete super prefix; immutable fields are covariant; mutable fields are invariant with unchanged
mutability; and packed storage must match exactly. Recursive equivalence also distinguishes group-bound
references from absolute references to prior groups while allowing super chains to target an equivalent
recursive projection.

Exact official AST and byte-backed tests cover the two formerly rejected valid leaders and fourteen formerly
accepted invalid modules. This is validation-only work: all 45 valid products and 48 dependents stay gated, no
collector is created, no roots or barriers are added, and codec v27, snapshots, guard mode, public admission,
and arm64 inherit no executable state.

### Iteration 57 exact no-object type-subtyping products

A separate `stagedGCTypeSubtypingProduct` marker admits the first eight official leaders without reusing
iteration 37's generic structural marker. Six modules contain only declared array/struct/function super graphs.
Two contain three recursive local functions each, but no exports or state; their bodies are restricted to
`local.get` and direct `call`. The classifier rejects imports, globals, tables, elements, memories, data, tags,
start, exports, descriptor metadata, heap-object types in the function-body class, and any other opcode before
the SHA-256 pin is checked.

The declaration products retain exact `gc.TypeDesc` metadata but instantiate without a collector because no
object can be created or observed. The recursive-function products have function sentinels only and likewise
leave `Instance.gc` nil. No helper, root, barrier, frame publication, public token, conversion owner, or mutable
cell/slot coherence rule is added. The six declarations emit no code; the two function products emit 632 and
592 linked bytes. Codec v27 preserves their type graph/code but not the marker, so reload cannot inherit the
nil-collector exception. Strict accounting becomes 8 passed modules / 37 gates / 48 blocked dependents / 24
invalid / 8 unlinkable obligations / zero validator gaps or hidden failures.

### Iteration 58 immutable local function-identity globals

The next six official leaders use only immutable local `ref.func` globals. Their exact product class rejects imports,
exports, tables, elements, memories, data, tags, start, mutable storage, arbitrary function bodies, and non-local
initializers. It accepts one or two local functions and one, two, four, or eight globals only after checking every
initializer's function type as a subtype of its declared non-null indexed storage type.

Instantiation creates the existing canonical function-descriptor arena but no collector. One-function products own
64 bytes and two-function products own 96 bytes, including the null descriptor entry. Each global cell points into
the same instance arena and each selected descriptor's identity slot points to itself. These words are function
lifecycle identities, not compact `gc.Ref` handles, collector roots, opaque public tokens, or foreign descriptors.
The globals are not exported and the functions are either empty or exactly `unreachable; end`, so no identity
crosses a public, host, cross-instance, table, snapshot, or mutable-storage boundary.

The six products are 94/77/498, 134/77/656, 84/77/419, 150/77/754, 112/253/597, and 172/253/851
Wasm/code/codec bytes. Codec v27 preserves exact descriptors and `ref.func` initializer metadata but loses the
compile-only class, so private reload fails before descriptor/global mutation. Strict accounting becomes 14 passed
modules / 31 gates / 48 blocked dependents / 24 invalid / 8 unlinkable obligations / zero validator gaps or hidden
failures. No helper, root, barrier, collector sidecar, frame publication, basedata offset, fixed descriptor layout,
or public ABI changes.

### Iteration 59 single-result function `ref.test`

The next four official leaders are pinned at generated command lines 248, 263, 275, and 286. Their Wasm sizes are
122, 162, 122, and 112 bytes, and their expected single `i32` results are exactly `1, 1, 0, 1`. A distinct
`stagedGCTypeSubtypingRefTestSingle` class requires exactly two local functions, no imports/globals/tables/memories/
data/tags/start, one declarative function element containing only function 0, one export `run` naming function 1,
an empty function 0, and a function 1 body exactly `ref.func 0; ref.test <indexed function type>; end`. AST and
direct byte-backed validation both remain authoritative; hashes are checked only after the complete product shape.

The amd64 backend records compile-only local `ref.func` provenance in stack values. `ref.test` consumes that provenance
and emits the validator's structural/declaration-supertype answer as a constant i32. No descriptor is dereferenced,
no parked-Go helper is called, and no function descriptor pointer is interpreted as a compact `gc.Ref`. Instantiation
uses the existing null-plus-two 96-byte canonical descriptor arena and leaves `Instance.gc == nil`; 1,000 repeated
`run` calls measure 0 allocations per invocation. Every product emits 178 native code bytes, while codec-v27 sizes
are 647, 805, 647, and 568 bytes.

Codec reload preserves types, functions, elements, exports, and code but not the compile-only product class; private
reload fails required-feature admission and public load rejects unknown GC feature bits. Public compile, snapshots,
signal-backed guard mode, and non-linux/amd64 execution remain fail-closed. No helper, root, barrier, collector
sidecar, basedata offset, descriptor entry, fixed runtime layout, or public ABI changes. Strict accounting becomes
18 passed modules / 4 passed assertions / 27 gates / 44 blocked dependents / 24 invalid / 8 unlinkable obligations /
zero validator gaps or hidden failures.

### Iteration 60 multi-result function `ref.test`

The next three official leaders are pinned at generated command lines 302, 315, and 338. Their Wasm sizes are 178,
144, and 204 bytes, and their ordered results are respectively two, four, and eight i32 ones. A distinct
`stagedGCTypeSubtypingRefTestMulti` class accepts only two or three local functions, no imports/globals/tables/memories/
data/tags/start, one declarative element per tested source function, one final `run` export, no locals, empty or exact
`unreachable; end` source bodies as pinned, and a runner containing only `ref.func; ref.test` pairs followed by `end`.
Every tested function index and indexed function target is checked before the SHA-256 pin.

No runtime reference category or helper was added. The existing compile-only provenance emits each structural subtype
answer as an i32 constant. The two-result runner uses the existing integer register-result path; the four- and eight-
result runners use the ordinary canonical result slots and result buffer. Invocation preserves exact source order and
repeats 1,000 times with zero allocations. Descriptor arenas are 96, 128, and 128 bytes; linked code is 215, 448, and
560 bytes; codec-v27 blobs are 922, 785, and 1,095 bytes. Every instance keeps `Instance.gc == nil`.

Codec reload retains structural/function/element/export/code metadata but no admission class. Public compile/load,
snapshots, signal-backed guard mode, arm64, and unsupported platforms remain fail-closed. No helper ID, root, barrier,
collector sidecar, basedata offset, descriptor entry, fixed layout, or public ABI changes. Strict accounting is now
21 passed modules / 7 passed assertions / 24 gates / 41 blocked dependents / 24 invalid / 8 unlinkable obligations /
zero validator gaps or hidden failures. The two finality/direction-false function-only leaders remain a separate slice.

### Iteration 61 final direction-false function `ref.test`

The final two function-only leaders are pinned at generated command lines 359 and 371. Their Wasm sizes and SHA-256
identities are 104 bytes / `2841d098dfca125ccd9c577cf55762744c8a3911a1986f857be48ebc0d51f735` and
117 bytes / `b0797a1825d04be467e336f7f236637184aab41a13de20ff7a06eb1bb7885613`. Each module has one
empty source function, one declarative element naming only function 0, one `run` export naming function 1, no locals
or state, and a runner exactly `ref.func 0; ref.test <indexed function type>; end`. Both return one i32 zero.

A distinct `stagedGCTypeSubtypingRefTestDirectionFalse` class recognizes only the exact recursive graph. There are
two or three two-member open-function groups before the final runner type. In group zero, member one names member zero
as a recursive super; in every later group, member one names the preceding group's first member as an absolute super.
The source function uses the first member of the last group and is tested against the first member of the preceding
group. The classifier independently proves that source-to-target subtyping is false: the first member does not inherit
its sibling's declared-super edge. Final/open flags, group/member order, absolute-versus-recursive indexes, source and
target type indexes, and the false result are checked before the SHA pin.

The backend reuses the existing compile-only local-function provenance and emits a constant zero through the ordinary
one-result RAX path. It never reads the 96-byte canonical descriptor arena, invokes a runtime classifier, or treats a
function descriptor as a compact `gc.Ref`. Each product emits 178 native bytes, produces a 469- or 549-byte codec-v27
artifact, leaves `Instance.gc == nil`, and repeats 1,000 public invocations with zero allocations. Codec reload retains
metadata/code but no product marker; public compile/load, snapshots, signal-backed guard mode, arm64, and unsupported
platforms remain fail-closed. `compiledCodeCache` remains 64 bytes and no helper, root, barrier, sidecar, basedata,
descriptor, frame, result, or public ABI layout changes.

Strict accounting is now 23 passed modules / 9 passed assertions / 22 gates / 39 blocked dependents / 24 invalid /
8 unlinkable obligations / zero validator gaps or hidden failures. All nine function-only `ref.test` leaders are now
closed; runtime call/cast/table, linking, and non-flat exported-function products remain separate obligations.

### Iteration 62 recursive runtime function identity

The 412-byte/SHA-256-pinned source-line-229 product is the first `gc/type-subtyping` leader that executes dynamic
function identity rather than folding a local `ref.func`. It owns three open function types in the chain `$t2 <: $t1 <:
$t0`, three local functions returning null at those exact types, one fixed three-entry funcref table initialized with the
three canonical local descriptors, one `run` export, and six trap exports.

`call_indirect` retains its ordinary table bounds and null checks. For this exact product only, the signature check loads
the table entry's canonical identity and compares it against the finite set of local descriptors whose declared function
type is a subtype of the requested type. `ref.cast` applies the same validated relation directly to the descriptor pointer
returned by `table.get`, preserving the original word on success and raising only trap code 18 on mismatch. Descriptor
addresses never enter `gc.Ref`, `Collector.RefCast`, checked collector slots, public GC tokens, or extern conversion
ownership.

The successful action proves calls and casts for `t0<-{t0,t1,t2}`, `t1<-{t1,t2}`, and `t2<-t2`. The six failure
actions prove indirect-signature and cast rejection for the inverse directions `t1<-t0`, `t2<-t0`, and `t2<-t1`.
Trap recovery is followed by a fresh successful run, and 1,000 successful repetitions allocate zero bytes. The instance
owns 352 canonical descriptor bytes plus a 104-byte immutable table image, emits 4,938 native bytes, produces a
5,433-byte codec-v27 artifact, and keeps `Instance.gc == nil`. Five 500 ms samples measure 50.78–51.50 ns/op, 0 B/op,
and 0 allocs/op.

Codec reload retains the structural types, table, element, exports, and code but not the compile-only product marker.
Public compile/load, snapshots, signal-backed bounds, arm64, and unsupported platforms remain fail-closed.

### Iteration 63 finality-sensitive runtime function identity

The 185-byte/SHA-256-pinned source-line-290 product owns one open and one final `() -> ()` type with no declared super
edge, two empty local functions at those exact identities, and a fixed two-entry funcref table initialized with both
canonical descriptors. Four exports isolate final-to-open and open-to-final mismatches separately for indirect signatures
and indexed-function casts.

The classifier requires the validated dynamic relation to be identity-only before enabling iteration 62's finite local
descriptor checks. `call_indirect` keeps ordinary bounds, null, code-pointer, and call ABI behavior; `table.get` returns
the canonical descriptor identity; and `ref.cast` compares that identity directly and preserves it byte-for-byte only on
success. No descriptor word enters `gc.Ref`, `Collector.RefCast`, checked collector roots, public GC tokens, or extern
conversion ownership. The product contains no imports, mutable table operation, host/cross-instance descriptor, or public
reference egress.

All four official exports trap exactly: the two calls report `wrong signature` and the two casts report `cast failure`.
A subsequent successful invocation of local function zero proves recovery, and 1,000 repetitions allocate zero bytes.
The instance owns 224 descriptor bytes and a 72-byte table image, emits 1,257 native bytes, produces a 1,555-byte
codec-v27 artifact, and keeps `Instance.gc == nil`. Five 500 ms local-recovery samples measure 37.71–38.02 ns/op,
0 B/op, and 0 allocs/op. Codec reload retains exact open/final metadata and code but no product admission; public load,
snapshots, guard mode, arm64, and unsupported platforms remain fail-closed. Linking products and the non-flat export
remain separate obligations.

### Iteration 64 exact typed-table function identity

The 186-byte/SHA-256-pinned source-line-319 product defines open `$t0`, `$t1 <: $t0`, and `$t2 <: $t1` `() -> ()`
function types plus one unrelated final runner type. Its fixed `table 2 2 (ref null $t1)` is initialized by a typed active
element with local function 0 at exact `$t1` and local function 1 at subtype `$t2`. The classifier proves both source-to-
storage assignments and rejects `$t0` as too wide before consulting the binary pin.

`run` requests `$t0` from both entries, `$t1` from both entries, and `$t2` from the second entry; all five calls succeed.
`fail1` requests `$t2` from the first `$t1` entry, and `fail2` requests the unrelated final runner type from that entry;
both trap for indirect signature mismatch. The call path retains ordinary bounds, null, code-pointer, and call behavior,
then compares the table entry's canonical local identity against the finite validated subtype set for the requested type.
No descriptor enters compact `gc.Ref`, collector helpers, checked GC roots, public GC tokens, or extern conversion state.

The instance owns 192 descriptor bytes (null plus five local descriptors) and a 72-byte immutable table image. Both table
entries have nonzero code pointers and point back to their exact canonical local descriptors. Post-trap `run` recovery and
1,000 successful repetitions are allocation-free. The product emits 1,431 native bytes, produces a 1,790-byte codec-v27
artifact, keeps `Instance.gc == nil`, and measures 49.16–52.61 ns/op across five 500 ms samples at 0 B/op and 0 allocs/op.
Codec reload retains exact type/function/table/element/export/code metadata but no admission marker; public compile/load,
snapshots, signal-backed bounds, arm64, and unsupported platforms remain fail-closed. Mutable/imported/exported/host or
cross-instance typed tables, linking providers/consumers, and the non-flat export require separate ownership/ABI proofs.

### Iteration 65 first cross-instance subtype linking cluster

The source-lines-486–530 product uses a 103-byte provider over `$t2 <: $t1 <: $t0`, an 86-byte consumer with six
exact/duplicate/widening imports, and three 51-byte narrowing consumers. Exact AST and byte-backed validation, SHA-256
pins, provider bodies/exports, and import order precede separate provider/consumer classification. Cross-instance subtype
matching is enabled only for that pinned pair. The provider owns 128 descriptor bytes; the successful consumer owns 224.
Duplicate imports retain one provider, a later mismatch rolls earlier retention back, both close orders release once, and
all narrowing attempts retain nothing. Provider/consumer wasm/code/codec sizes are 103/369/623 and 86/0/300 bytes; the
null-result export measures 67.56–76.86 ns/op, 0 B/op, and 0 allocs/op.

### Iteration 66 finality-sensitive linking cluster

The source-lines-540–556 product remains separate even though both function signatures are `() -> ()`. The 70-byte
provider, SHA-256 `dcf54459e9f39087c697c9d9edc0955aabc02eb28e40b65c84291cbe194a9562`, exports open `f1`
and final `f2`. The two 38-byte consumers are pinned as
`ea960ddec4f24c952d26ee7a567309a41c5895cf84690ca120d4577bb4c26e08` (`f1` required as final) and
`7fc43bbbff42ca923db1604d0339cadd21458f5671ea7962d031786e93517996` (`f2` required as open).
Both AST and byte-backed validation prove the identity-only relation before classification and pin checks.

The provider owns a 96-byte canonical descriptor arena. Each consumer has one imported function and a bounded 64-byte
descriptor requirement, but both official imports fail before a consumer instance, descriptor copy, or producer owner is
published. Provider retention therefore remains exactly zero after either attempt. Hosts and the iteration-65 provider
also reject before attachment. No compatible consumer exists in this cluster, so cross-instance close-order sharing is
not claimed; ordinary provider close remains single-owner and exact. Descriptor words remain ordinary instance-owned
64-bit function identities, never compact `gc.Ref`s, roots, public GC tokens, or collector objects.

Provider wasm/code/codec size is 70/157/323 bytes; each consumer is 38/0/144 bytes. The final export measures
36.50–37.43 ns/op across five samples, 0 B/op, and 0 allocs/op. Unlinked codec v27 preserves open/final metadata but no
admission marker. Live linked serialization, private/public reload admission, snapshots, signal-backed bounds, host
substitution, cross-product substitution, arm64, unsupported platforms, and public GC admission remain fail-closed.
Strict accounting is 170 commands / 29 passed modules / 23 passed assertions / 16 gates / 18 blocked commands /
24 invalid / 5 executed expected unlinkables / 3 blocked unlinkables / zero validator gaps, unexpected compile/link
failures, or hidden failures. The source-lines-566–572 M3 struct-defined pair is the next separate exact obligation.

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

- WasmGC opcode/product execution is not complete. Exact staged `gc/struct`, `gc/array`, `gc/i31`,
  `gc/ref_test`, `gc/extern`, `gc/ref_eq`, `gc/ref_cast`, `gc/br_on_cast`, and
  `gc/br_on_cast_fail` families are wired to amd64, including bounded array constructors/access/barriers,
  collector-free i31 operations, dynamic tests/casts, identity-preserving cast branches, extern conversion,
  and compact null/i31/object equality. Array fill/copy plus both `array.init_data` and the exact local-
  funcref `array.init_elem` product are staged and gap-free. Complete `gc/type-subtyping` accounting is pinned
  at 170 commands; iterations 57-66 execute twenty-nine collector-free leaders, including six immutable local `ref.func`
  globals, nine function-only tests, recursive call/cast/finality products, the exact typed-table call product, the first
  cross-instance subtype pair, and the separate finality-link provider. The first pair keeps ordinary canonical descriptor
  identity in bounded 128/224-byte arenas with deduplicated retention, rollback, both close orders, and three narrowing
  unlinkables. The 70-byte finality provider owns 96 descriptor bytes; two 38-byte inverse consumers have bounded 64-byte
  requirements, unlink in both directions, and retain nothing. Repeated provider invocations allocate zero bytes, leaving
  16 exact gates and 18 blocked commands with zero validator gaps, unexpected compile/link failures, or hidden failures.
  Live linked codec state, snapshots, hosts, guard mode, arm64, the later struct-defined linking clusters, reference struct
  fields, non-local/reference-owning array products, broader GC constant expressions, and non-flat exports remain.
- The parked-Go runtime-call ABI is proven for exact empty-frame-root numeric/packed
  allocations, non-collecting numeric access/mutation/data initialization, exact local-funcref element
  initialization, ordered immutable collector-rooted globals, per-instance passive descriptors, and one
  result-token root installed only after the native call returns. General allocation with live frame refs,
  passive-element compact-GC roots, reference field/element object+bulk barriers, and traps still need the
  backend-neutral root-publication ABI before broader generated code can use objects.
- Exact native safepoint maps are not connected to compiled frames yet.
- Minor collection currently promotes marked nursery survivors through handles
  rather than implementing a final copying nursery/root-update path.
- The Throughput old/large allocator reuses freed memory, but full Immix
  line/block marking, compaction, and more advanced fragmentation control remain
  future work.
- The Throughput heap currently uses growable Go byte slices, so native code
  must not cache raw heap payload pointers; see `docs/runtime-abi.md`.
- Tiny and Throughput profiles are connected to exact staged struct/array/table helper paths,
  including immutable and passive roots, reference array barriers, packed data/drop lifecycle,
  bounded public/conversion ownership, dynamic tests, and compact equality under stress collection
  and deterministic Tiny exhaustion. Broader WasmGC opcode/backend lowering remains closed.

These limitations are intentional for this commit series: the runtime foundation
is small, exact, typed, and no-cgo, giving later codegen work stable contracts.
