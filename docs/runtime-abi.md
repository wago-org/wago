# Runtime ABI Notes

This document records runtime layout details that native code relies on. Keep it
in sync with `src/core/runtime/basedata_test.go`, `src/core/runtime/abi`, and
backend code that emits loads from `JobMemory` basedata.

## JobMemory basedata

`JobMemory` reserves basedata immediately before the linear-memory base. Offsets
are addressed as negative displacements from the linear-memory pointer used by
JIT code. Existing offsets must not move without re-deriving the runtime ABI and
updating the guard tests.

The globals pointer lives at basedata offset `112` (`abi.GlobalsPtrOffset`, used
by runtime layout and backend codegen). Backend `global.get`/`global.set` code
loads this pointer from basedata, indexes the per-instance global pointer table,
then loads or stores the pointed-to 8-byte global cell.

The trap-cell pointer and stack fence are per-invocation control fields, not
per-instance constants. Cross-instance direct and indirect calls temporarily
copy the active caller's values into the callee basedata so a callee trap unwinds
the whole native call tree. Consequently every later public entry—including the
prepared-call fast path—must restore its own trap-cell pointer and engine fence
before entering native code. Bind-once prepared calls are valid only while an
instance can never be used as a cross-instance callee.

The remaining pointer fields are modeled as `runtime.InstanceContext` and
captured when instantiation finishes. Every public native entry rebinds that
context and refreshes its invocation control fields before execution. The
current correctness-first execution lease serializes native execution
process-wide: one public root owns every basedata region its direct or indirect
cross-instance call graph may rebind. This avoids recursive per-memory lock
ordering and covers same-memory, different-memory, and cyclic call graphs.
Linear-memory size/growth caches remain backing-owned, while trap and stack
fields remain invocation-owned.

Direct imported calls load `{entry, homeLinMem, targetContext, callerContext}`
from the per-instance dispatch table. Indirect calls recover `targetContext` from
the canonical funcref descriptor referenced by the table entry. Both paths bind
the target pointer context before crossing instances and restore the caller
context after a normal return. A trap unwinds the native call tree before restore;
the next serialized public entry always rebinds its own captured context first.
Canonical funcref descriptors are 40 bytes: the 32-byte table payload plus an
8-byte owning-context pointer. Table entries remain 32 bytes. A function importer
retains each distinct producer instance until the importer's physical resource
release; logical close alone cannot release those roots when a table, global, or
public token still retains the importer's descriptor arena. Imported HostFuncRef,
reference-global, and table attachments follow the same physical-release rule.
When an imported funcref container retains the writer itself, that container
attachment is transferred to the retained root to avoid a self-owning importer
cycle, and is released exactly once.

## Synchronous host parking and active-callee routing

The trap allocation is 16 bytes. Bytes 0..3 hold the trap/pending code; bytes
8..15 hold the exact active host-control-frame pointer for a parked activation.
AMD64 and ARM64 host stubs save the current register state into the callee's
control frame, publish that frame pointer at `trap+8`, write
`hostCallPending`, and unwind to Go.

`Engine.CallWithHost` reads arguments and import indexes from the published
frame, not from the public root's frame. The frame address resolves to the
physically live callee instance, so dispatch uses that instance's import
namespace, HostFunc/HostFuncRef binding, HostModule, and result buffer. Every
synchronous frame receives its trampoline at instantiation, including callees
that have never been entered as public roots.

The process-wide native execution lease is released before arbitrary Go host
code runs. Nested Wasm entry may therefore acquire the lease without deadlock.
On every normal return or panic path the dispatcher reacquires the lease. It
rebinds the exact parked callee context when the execution epoch shows that a
nested or competing public entry ran; otherwise the already-installed context
is reused. Immediately before `resumeNative`, the runtime restores the parked
activation's trap pointer, stack fence, and architecture-specific trap re-entry
control. `HostExit`, traps, and propagated host panics therefore cannot leave a
lease held or resume against a context installed by nested entry.

Instances with actual host bindings use synchronous dispatch, including legacy
void HostFunc signatures, because such an instance may later execute as a
cross-instance callee. That parking capability propagates transitively through
canonical InstanceExport imports. Where the architecture host policy permits,
a host-free cross-instance-only consumer whose targets cannot park uses the
ordinary native entry path and allocates neither a control frame nor an async
replay log. Forced-synchronous architectures still allocate their control frame.
Modules importing a funcref table remain
synchronous because the table may be mutated to contain a host descriptor. The
old async log format remains an internal compatibility path but is not selected
for these compositions.

The synchronous parked-Go transition restores callee-saved GPRs, but System V XMM
registers are caller-saved. Before any synchronous host or internal GC helper
call, amd64 copies all arguments to the 328-byte control frame and then spills
dirty pinned locals to their canonical frame slots. Float locals reload lazily
after resume. Codegen must not assume an XMM-pinned local survives parked Go
merely because RBX/RBP/R12-R15 do.

## Guarded host memory access

In guard-page mode, `memory.grow` raises the logical size before newly in-bounds
pages are necessarily committed; native loads/stores commit them lazily through
the fault handler. Host access uses `JobMemory.HostBytesChecked`, which mprotects
and extends the stable-base Go view through the current logical size first. This
is required for `Memory.Bytes`, typed host reads/writes, snapshot restore, and
active data initialization against an imported memory that grew before the new
instance was created. `CurrentBytes` remains limited to the original committed
Go slice and must not be used for that case.

On ARM64, the guard fault handler passes the faulting linear-memory base through
saved `X9` when it replaces the faulting PC with the native trap-exit landing
pad. The landing pad must not depend on the platform signal trampoline restoring
Wasm's pinned `X26`: Linux and Darwin replacement-PC returns can otherwise reach
the landing pad with an unusable `X26`, preventing recovery of the foreign-stack
save area and `enterNative` continuation.

ARM64 modules whose declared or imported memory minimum is zero use explicit
bounds checks and classic growable memory even when signals-based checks were
requested. This narrow fallback preserves exact zero-page semantics until the
ARM64 guard entry can safely place its control words immediately below a linMem
that starts on the first inaccessible linear page. One-page-and-larger ARM64
memories continue to use the guard-page path.

## Global storage convention

Each instantiated module owns an arena-backed globals pointer table:

- one 8-byte pointer-table entry per wasm global, in wasm global-index order;
- imported global entries come before locally defined global entries;
- duplicate imports of the same global key point at the same host-owned global
  cell, preserving mutable global object identity;
- locally defined globals point at instance-local 8-byte cells released with the
  instance arena on `Instance.Close`;
- `i32` and `f32` values occupy the low 32 bits of a cell; backend loads and
  stores use 32-bit accesses for these low halves;
- `i64` and `f64` values occupy all 64 bits of a cell; backend loads and stores
  use 64-bit accesses for the full cell.

The globals pointer table and every global cell handed to native code live in
stable off-heap memory. Native code must not receive Go heap pointers for
globals, and per-access `global.get`/`global.set` code must not allocate.

## WasmGC heap pointer stability

`gc.Ref` values are stable compact integers, but the current Throughput WasmGC
heap stores object payloads in Go byte slices. Growing that heap may allocate a
new backing slice and copy existing bytes. Generated native code therefore must
not cache raw pointers into WasmGC object payloads across helper calls,
allocations, safepoints, or any operation that can grow/collect the GC heap.

Until the Throughput allocator moves to chunked pages or pre-reserved backing
memory with stable native addresses, WasmGC object access from generated code is
helper-call-only: pass `gc.Ref` values and field/element indexes to runtime
helpers, then discard any transient Go-derived address before returning to
native code. Direct native loads/stores may be introduced only after the
allocator and runtime ABI explicitly guarantee payload address stability.

Helper calls that may allocate, collect, or run barriers must publish all
caller-known live refs, not just direct helper arguments. The `codegen.Emitter`
root protocol is all-or-nothing: `SpillLiveRefs` prepares storage,
`PublishRoots` either fully publishes it or returns an error with no roots live,
and successful publication must be followed by exactly one `UnpublishRoots` even
if the runtime helper fails.

Iterations 38-39 have a narrower executable helper ABI while general native root-map
publication remains incomplete. Exact linux/amd64 explicit-bounds numeric-struct products
park through the existing 328-byte synchronous control frame with dispatch bit 30 reserved
for internal GC helpers. The helpers receive compact `gc.Ref` values, type/field indexes,
and numeric bits in copied scalar slots; they never expose or retain a Go-slice payload
address in native code.

Allocation-local products pass the non-nil zero-sized `gc.EmptyRoots` set only when no prior
frame/local ref is live. Iteration 39 separately admits at most two immutable numeric GC
globals. Instantiation allocates one object, initializes it, installs a checked collector
`GlobalSlot`, and only then begins the next initializer. The native 8-byte global cell and
the collector slot contain the same compact stable handle; a fixed two-entry lazy instance
sidecar records their index mapping. Because these globals are immutable, no cell/slot write
coherence or barrier is required. Any mutable GC global must update both transactionally and
call the collector slot barrier, so it remains rejected.

Ordinary/packed `struct.get` and numeric `struct.set` do not collect. The official basic
numeric actions cross only exact internal callees whose reachable instructions cannot
allocate. Iteration 40 owns that product's sole exported non-null `ref.struct` result only
after native return: the compact 32-bit handle is checked against the declared result and
exact dynamic struct type, replaced with a random opaque store token whose upper 32 bits are
non-zero, and rooted in one reusable checked collector global slot. A second live token per
producer rejects. Release nulls the slot through the collector barrier contract; the token
retains the producer collector across logical instance close and supports both close orders.
Helper calls, token root mutation, and collector close serialize through one lazy per-instance
mutex.

This result-token root is not a native frame root and does not justify widening `gc.EmptyRoots`.
It exists only after the Wasm activation has returned, carries no cached payload pointer, and
is compile-only exact-product state absent from codec v27.

Iterations 41-42 give pointer-free arrays a separate compile-only helper/product path. Exact
`array.new`, `array.new_default`, bounded `array.new_fixed`, and i8 `array.new_data` producers
perform one allocation with a proven empty live-frame-ref set. Helper-side `array.get`, numeric
or packed `array.set`, and `array.len` discard every transient Go-derived address before native
resume. The fixed leader's one immutable global and the default leader's two immutable globals
install checked collector slots immediately after each object's scalar initialization; the first
default root is visible before the second allocation can collect. Numeric/packed array stores
need no barrier. Public results are tokenized only after native return and use the same one-live-
token root/lifetime policy with an exact dynamic array-type check.

`array.new_data` reads only the copied scalar source/length/type/segment arguments plus the
per-instance 16-byte passive descriptor. Parked Go checks the current descriptor length and
u64-widened source end before allocating, then copies retained immutable bytes into i8 payload
slots. `data.drop` mutates only the descriptor length. Thus non-empty post-drop and overflowing
ranges trap before collector state changes, while source zero/length zero remains valid; no
Go-slice pointer enters native state or survives helper return.

Iteration 43 adds one separate, bounded exception to the empty-root rule for the exact reference-
element leader. Its two passive i8-array values live in checked collector table slots, not Wasm
globals or native frames. `array.new_elem` copies at most two selected refs into one reusable
mutable `RootSet` stored in the per-instance segment sidecar, preflights the descriptor range,
allocates the outer array, rereads rewritten roots, performs reference stores with object/card
barriers, and invokes the post-write bulk barrier. `elem.drop` zeros descriptor length and nulls
both slots through the collector slot barrier. The descriptor is arena-owned and contains no Go
pointer; native code still receives only compact refs and scalar indexes. Codec v27 and snapshots
inherit no constructor, root, descriptor-lifecycle, or helper admission state.

This does not publish a native frame chain. The only live refs across allocation are the exact
passive segment pair already owned by collector slots; non-null ingress, mutable/reference globals,
hosts, general calls with live refs, snapshots, guard mode, and arm64 remain closed. These exact
struct/array proofs must not be treated as the general `codegen.Emitter` publication protocol.

Iteration 44 adds a separate non-allocating i31 ABI. Generated amd64 code keeps the existing
32-bit compact encoding in the low half of a native reference slot: zero is null and
`(low31 << 1) | 1` is an i31 immediate. Encoding and signed/unsigned extraction use direct 32-bit
shift/tag instructions; extraction traps before shifting a zero reference. Exact i31 casts test
the low tag bit and never dereference the value. No parked-Go helper, collector, safepoint, root,
barrier, or payload pointer participates.

Local i31 and pinned anyref tables use the same 8-byte scalar entry stride as externref storage,
but their values are not store-owned externref tokens. Element metadata stores tagged i31 bits in
the existing u32 payload field; active/passive copy, grow, fill, and init move only scalar words.
The exact imported-global table initializer is an 8-byte compile-only record naming table/global
indexes and is absent after codec reload. Literal/global-expression metadata and exact table/
element types persist in codec v27, but loaded artifacts have no staged execution admission and
fail before instantiation mutation.

The public high-level ABI uses the distinct `ValI31Ref`/`I31Ref` category. It must never route
through `ValAnyRef` token ownership or `GCRef` release. The low-level untyped `Invoke` slot remains
signature-defined and carries the compact word, while `Call` exposes signed/unsigned i31 accessors.
The official slice has result-only i31 egress and no i31-reference parameters, host calls,
cross-instance reference transfer, snapshots, guard execution, or arm64 execution.

Iteration 45 reuses that compact word for one exact non-allocating `ref.test` product. Generated
amd64 code never dereferences the operand and never parks in Go. For target `i31`, `eq`, or `any`,
it isolates the low tag and combines it with the zero test only for nullable targets. For target
`struct`, `array`, or `none`, the admitted null+i31 domain can match only nullable null; tagged i31
values return false. A non-zero low-bit-zero word therefore cannot be mistaken for an immediate.
The exact product has no reference parameters, imports, globals, tables, object constructors, or
host/cross-instance boundary, so such an object-shaped word cannot enter the admitted activation.

This direct path is not general dynamic-type execution. Iteration 46 separately admits object
classification through the parked-Go boundary. Generated code passes only the compact `gc.Ref`,
the signed target heap/type ID, and nullability. Go resolves the exact collector descriptor, checks
struct/array kind, walks validated declared supers, and optionally compares through one immutable
collector-bound canonical representative map. No payload pointer, public `GCRef` token, or Go-slice
address enters native state.

One exact local compact struct table pairs each arena-owned eight-byte entry with a checked collector
`TableSlot`. A parked table-store helper preflights bounds and ref validity, updates the slot through
the collector barrier, and only then writes the native entry; rejected stores mutate neither. The
first object is rooted before the next allocation, repeated initialization overwrites through the
same fixed slots, and close nulls all slots before collector teardown. The synthetic table has two
slots; the official concrete leader uses twenty slots and retains eight live objects. The sidecar is
120 bytes and the lazy `instancePluginState` grows from 136 to 144 bytes; `Compiled=712`,
`Instance=792`, `compiledCodeCache=64`, `compiledMemoryDirectory=136`, and `gc.Collector=640`
remain unchanged.

The official concrete leader executes all subtype/canonicalization tests. Iteration 47 adds a
separate exact ABI for the 626-byte abstract leader. Its anyref table remains eight-byte words, but
only low-32-bit null/i31/object values may enter collector slots. A high-word foreign-any identity is
resolved by the bounded conversion sidecar and leaves its collector slot null. Funcref table entries
remain 32-byte lifecycle descriptors and are never scanned as `gc.Ref`. Externref table entries remain
eight-byte public store tokens or internal conversion identities and are likewise never scanned.

Parked `any.convert_extern` and `extern.convert_any` helpers receive one copied 64-bit word and return
one word. The fixed eight-entry owner distinguishes foreign-extern-to-any identities from data-to-
extern identities. Converted heap objects own checked collector table slots; i31 conversions own no
collector state. Parked table stores validate bounds and ownership before writing arena state, and
extern-table replacement withdraws the old conversion root before committing the new word. Repeated
initialization reuses entries rather than growing a process-global map. The sidecars are
`gcRefTestTableState=200` and `gcExternConversionState=352`; lazy `instancePluginState` remains 144
bytes and fixed runtime layouts do not grow.

Iteration 48 reuses the same one-word parked helper ABI for the exact 286-byte `gc/extern` product,
but adds a product-boundary translation on public invocation. Before native entry, an ordinary
store-owned extern token or a bounded public conversion token is resolved to its internal extern/any
word. After native return, an internal foreign/data word is translated to a stable high-word public
identity. Data conversions keep separate public-any and public-extern words; neither is an internal
extern word, compact `gc.Ref`, store extern token, funcref descriptor, or opaque public `GCRef` token.
The public argument/result buffers therefore never expose compact object handles.

The exact product has one ten-entry anyref table. Its checked slots scan only compact object words;
foreign-any words leave roots null, and null/i31 remain non-owning. `init` stores the new struct before
allocating the zero-length array, then stores the array before any later may-collect helper. The public
result identities are created only after conversion ownership exists and are reused from the fixed
entry. Forged public words fail translation before `serArgs` is committed to native execution.
`gcExternConversionEntry` grows to 56 bytes and the fixed eight-entry `gcExternConversionState` to
480 bytes; `gcRefTestTableState=200` and lazy `instancePluginState=144` remain unchanged. Codec v27
serializes none of these words or ownership records.

This remains an exact no-frame-publication proof. Every allocation is stored in a checked anyref slot
or converted/rooted before another may-collect helper. Arbitrary live local/operand refs, mutable GC
globals, reference fields, hosts, snapshots, signal bounds, public admission, and arm64 remain closed.
Codec v27 serializes no `ref.test` product, canonical map, conversion identity, checked root, local
funcref ownership, or live discriminator state.

Iteration 49 adds no new ABI word category. The exact `gc/ref_eq` product uses the same eight-byte compact
eqref table entries and checked collector slots as the prior object-table products, but attaches no extern
conversion owner and creates no public tokens. `table.get` yields the compact null/i31/object word, and the
existing `ref.eq` lowering compares the two 64-bit words directly. This is semantic value equality for null
and i31 plus handle identity for objects; it is not comparison of opaque public `GCRef`, public any/extern,
store extern, or internal foreign/conversion token bits.

The twenty-slot table roots each newly allocated struct/array before the next may-collect helper. Repeated
initialization on an 80-byte Tiny heap retains four 16-byte objects and one bounded replacement allocation;
no live local ref crosses allocation. `gcRefTestTableState` remains 200 bytes and lazy
`instancePluginState` remains 144 bytes. Codec v27, snapshots, guard mode, public admission, and arm64
inherit no equality product, checked roots, or live compact identities. This remains an exact scheduling
proof, not general native frame publication.

Iteration 50 adds one non-allocating parked reference-cast helper. Its copied ABI is three input words —
the original 64-bit internal reference word, signed heap target, and nullable flag — plus one output word.
Successful casts return the input word byte-for-byte. A valid dynamic mismatch raises trap code 18
(`cast failure`); `ref.as_non_null` remains trap code 16 (`null reference`). Stale/forged/closed compact
objects and forged foreign-any words remain helper errors, not semantic cast traps. The helper consumes
collector classification and optional canonical representatives but does not allocate, collect, mutate a
root, or expose a payload pointer.

The abstract official product reuses one ten-slot checked anyref table and the fixed eight-entry extern
conversion owner. Null/i31/object compact words and the high-word foreign-any identity retain their
separate ownership categories through casts. The concrete product reuses the twenty-slot object table and
collector-bound canonical representative map. In both products every allocation is stored before another
may-collect helper; all subsequent casts are non-collecting and their results are immediately dropped.
Consequently no live local ref crosses allocation and no native frame chain is published. Sidecars remain
`gcRefTestTableState=200`, `gcExternConversionState=480` for the abstract product, and lazy
`instancePluginState=144`; fixed runtime layouts do not grow. Codec v27 serializes no cast admission,
canonical map, conversion identity, checked root, compact table identity, or live collector state.

Iteration 51 reuses the classification half of that ABI for branch casts but changes the native control
contract. Generated code keeps the original 64-bit reference word as the top logical operand and passes a
copied word, signed target heap, and nullable bit through the parked `ref.test` helper. The one-word i32
result is consumed only as a branch condition. `br_on_cast` branches when it is non-zero;
`br_on_cast_fail` branches when it is zero. Both the branch payload and fallthrough retain the original
reference word byte-for-byte, while validation assigns the refined target or failed-source type to the
appropriate path. A nullable target makes the failed source non-null because null necessarily matches.

This is not implemented as `ref.cast` plus `br`: a mismatch never raises trap code 18, label-prefix values
move independently of the appended reference, and nested block result slots use the ordinary canonical
branch merge. The helper is non-allocating and non-collecting. The original reference is live in a canonical
native stack slot across parked Go, but no collector frame is published because the helper cannot collect,
mutate roots, or retain the word. Forged/stale/closed/wrong-owner values remain helper errors rather than
branch false values.

The abstract branch products add one exact two-input/one-output allocation helper for their single packed
i16 struct field. It receives only the scalar field and type index, allocates with `gc.EmptyRoots{}`,
initializes field zero before returning, and exposes no payload pointer. Native code stores that returned
reference into the checked anyref table before the later `array.new` helper may collect. The array result is
also stored before conversion. Tiny72 therefore retains two 24-byte objects plus one bounded replacement
allocation. Concrete products reuse Tiny256, twenty checked slots, and the immutable canonical map.
Nullability-only products allocate no object or table state.

No fixed ABI layout grows: `Compiled=712`, `Instance=792`, `compiledCodeCache=64`,
`compiledMemoryDirectory=136`, `gc.Collector=640`, `gcRefTestTableState=200`,
`gcExternConversionState=480`, and lazy `instancePluginState=144`. Codec v27 serializes neither the six
exact branch-product enums nor helper admission, roots, canonical maps, conversions, or live identities.
Snapshots, signal bounds, public admission, and arm64 execution remain closed.

Iteration 52 adds two non-collecting bulk-array helper calls. `array.fill` copies five scalar words into the
parked frame: destination compact ref, destination index, value bits, length, and exact type index.
`array.copy` copies seven: destination ref/index, source ref/index, length, destination type, and source type.
Both helpers preflight every null/type/range/value obligation before the first write and never allocate,
collect, retain operands, or expose payload pointers. The original object refs may therefore remain ordinary
canonical native operands across parked Go without publishing a collector frame. This rule is specific to
these helpers and does not authorize `array.init_*` or any bulk helper that may allocate or collect.

Reference fill/copy stores execute object and element-card barriers per destination element and invoke the
post-write bulk barrier after the complete range is visible. Same-array copy selects forward/backward
iteration for memmove semantics and allocates no temporary buffer. The official hash-pinned products use
packed i8 arrays, but collector tests separately prove nullable/non-null storage compatibility, rejected-copy
atomicity, Throughput remembered/card publication, and Tiny remark preservation.

The exact copy product also contains a mutable GC array global. Its overlap functions allocate one array,
run only the non-collecting copy helper while that local is live, and perform `global.set` as the final native
operation. After successful return, `Instance.invoke` and `invokeLocalContext` call a reconciliation routine
that is gated to `stagedGCArrayProductBulkCopy`: it reads at most two compact global cells, validates that the
high word is zero, and updates the corresponding checked collector slots under the existing GC mutex. No
later may-collect helper runs before this synchronization. A trap before the final `global.set` leaves both
cell and slot unchanged. This post-return rule must not be generalized to a mutable GC global whose new value
can cross another allocation, host/cross-instance call, tail transfer, or snapshot boundary.

No fixed ABI layout grows: `Compiled=712`, `Instance=792`, `compiledCodeCache=64`,
`compiledMemoryDirectory=136`, `gcArrayGlobalInit=48`, lazy `instancePluginState=144`, and
`gc.Collector=640`. Codec v27 serializes neither the bulk product/helper bits nor the mutable cell/slot
coherence rule or live refs; guard mode, public admission, snapshots, and arm64 execution remain closed.

Iteration 53 adds helper ID 29 for exact numeric `array.init_data`. The parked call copies six words in
operand order: destination compact ref, destination element index, passive source byte index, element count,
exact destination type index, and exact data-segment index. The helper first rechecks compact ownership and
exact type. It then reads the live 12-byte passive-data descriptor, uses its current length as the authoritative
post-`data.drop` bound, caps that length by the retained compile-time bytes, and calls the collector's width-
aware initializer. Destination element bounds and the complete source byte interval are checked with u64
arithmetic before the first write; one-, two-, four-, and eight-byte values are decoded little-endian.

Helper 29 allocates neither Go nor collector memory, cannot collect, retains no native ref after return, and
never exposes an arena pointer. A local destination ref may therefore remain an ordinary canonical native
operand across this exact parked call without a frame root. This is safe for the 435-byte transient product
because its only may-collect operation is the array allocation before the local exists; no later helper can
move or reclaim it before return. The rule does not cover `array.init_elem`: that path must first prove passive
reference-segment ownership, root publication, subtype-compatible stores, object/card/post-bulk barriers, and
Tiny remark behavior.

The 335-byte product initializes three immutable array globals transactionally. Its exact compile-time
product gate raises the global-root directory cap from two to three mappings; no other product receives that
admission. The fixed mapping array in lazy `instancePluginState` therefore grows by one 8-byte entry, from
144 to 152 bytes. `Compiled=712`, `Instance=792`, `compiledCodeCache=64`,
`compiledMemoryDirectory=136`, `gcArrayGlobalInit=48`, and `gc.Collector=640` do not grow. The helper does
not use iteration 52's post-return mutable-cell reconciliation: all three globals are immutable and rooted at
instantiation. Codec v27 serializes neither helper/product admission nor checked roots or passive dropped
state; codec reload, snapshots, signal bounds, public admission, and arm64 execution remain closed.

Iteration 54 adds helper ID 30 for the exact local-funcref `array.init_elem` product. The parked
call copies six scalar words: destination compact array ref, destination index, passive source index,
element count, exact destination type index, and exact element-segment index. Go reads the current
16-byte passive descriptor through the existing basedata pointer, checks both complete ranges, then
preflights every selected 32-byte passive funcref entry. Each non-null identity must point into the
instance's bounded canonical descriptor arena and structurally subtype the destination array element
type before any payload write occurs.

The two exact destination array descriptors use eight-byte non-scanned payload slots. Their stored
words are canonical local funcref descriptor identities, not compact `gc.Ref` handles, public tokens,
or collector roots. Consequently helper 30 performs no collector object/card/post-bulk barrier. The
array objects themselves remain checked global roots, while the executing instance owns the local
function descriptor arena for the whole activation. This is an exact local-only lifecycle proof; a
foreign, imported, host, extern, or compact-GC reference array must not reuse it without separate
producer retention and the applicable collector barriers.

A fixed twelve-word stack buffer holds all preflighted identities. The helper allocates neither Go nor
collector memory, cannot collect, retains no pointer after return, and never exposes a GC payload
address to native code. `elem.drop` reuses helper ID 26 to zero only the passive descriptor length;
zero-length post-drop initialization succeeds and non-zero initialization traps before mutation. The
268-byte product uses two 112-byte rooted arrays and measures 213.4–219.2 ns/op with 0 B/op and
0 allocs/op. No fixed runtime, basedata, descriptor, plugin-sidecar, or codec-v27 layout grows.

Iteration 55 adds accounting only for `gc/type-subtyping.wast`. The 45 valid leaders contain no
struct/array constructor, access, storage, result, import, export, or snapshot state; their runtime values are
function identities where values exist at all. No helper ID, basedata slot, descriptor representation, root,
barrier, collector sidecar, native frame, or codec field is added.

Iteration 56 closes the accounting's two valid and fourteen invalid validator gaps by enforcing recursive
projection identity plus function/struct/array component subtyping on both AST and byte-backed paths. This is
still validation-only: every leader remains product-gated, no native instruction or runtime lookup is added, and
no fixed ABI, helper, sidecar, codec, snapshot, root, barrier, collector, guard, public, or arm64 state changes.
The inventory therefore remains insufficient as an ABI admission argument until exact collector-free product
ownership is closed separately.

Iteration 57 closes that ownership proof only for the first eight leaders. Six declaration modules emit no native
code. Two recursive-function modules use the existing typed-reference slot/call ABI but have no exports and contain
only `local.get` plus direct `call`; no helper or host/native boundary is introduced. A separate compile-only product
enum authorizes nil-collector instantiation and is absent after codec reload. The marker consumes no new fixed space:
GC helper admission is derived from existing exact product enums, preserving `compiledCodeCache=64` bytes. No
basedata offset, control frame, descriptor entry, helper ID, root, barrier, collector sidecar, snapshot field, or
public/guard/arm64 ABI changes.

## Global coherence invariant

The global cell is the sole host- and cross-instance-visible storage for a
global. Backend code currently reads or writes the cell on every `global.get`
and `global.set`, so the cell is always authoritative.

A future backend may cache global values in registers across straight-line code.
Such caching must preserve this invariant:

- spill cached values back to the cell at function return and around calls
  (host imports and wasm-to-wasm calls), and reload after, so callers and later
  `Instance.Global`/`SetGlobal` reads observe writes;
- never assume exclusive ownership of an imported global's cell — its identity
  may be shared with the host and with other instances importing the same
  `*Global`, so the cell must remain the shared source of truth.

Synchronous host callbacks and cross-instance calls may observe globals during
one public invocation. Generated code must therefore spill caller-cached globals
before either boundary and reload afterward. Non-exported, non-imported globals
need the same call-boundary discipline for host re-entry; exported and imported
globals additionally remain coherent at public return.
