# Wasm GC runtime foundation

This document describes wago's native Wasm GC runtime direction. WasmGC is now
active mandatory WebAssembly 3.0 scope, tracked in [wasm3.md](wasm3.md), rather
than a non-goal. The current implementation is an initial foundation under
`src/core/runtime/gc/native`; it establishes reference encoding, object metadata, typed
descriptors, a byte-slice heap skeleton, exact scanning, roots, barriers, stress
knobs, and tests.

The decision-grade collector measurement contract, canonical
[benchmark review protocol](gc-benchmarks.md#benchmark-review-protocol), and
roadmap-aligned matrix are documented in [gc-benchmarks.md](gc-benchmarks.md).

## CLI heap sizing

`wago run` uses the bounded Throughput collector defaults unless the invocation
sets explicit capacities. Large compiler and build workloads can select the
root instance's old/large-object heap and Eden nursery directly:

```sh
wago run --gc-heap 2GiB --gc-nursery 64MiB compiler.wasm
```

Both flags accept a positive byte count or a binary `KiB`, `MiB`, or `GiB`
suffix. Values must fit the collector's 32-bit address space. Omitting the flags
preserves the existing defaults. Collection remains enabled; heap sizing is not
a substitute for exact native root admission.

## Current generated-payload boundary

The mandatory pinned Core 3 corpus is complete, but that result is narrower than
unrestricted compiler-generated WasmGC. The current linux/amd64 explicit- and
signal-backed paths admit struct and array helpers from validated opcode/type semantics
rather than requiring an exact binary hash or export spelling. It supports
multi-field and reference-bearing `struct.new`, reference `struct.set`, numeric,
`v128`, and reference `array.new`/`array.new_fixed`, object-building global constant
expressions, indexed `ref.null`, opaque non-scanned 64-bit function/extern
reference fields, and declared-subtype struct/array access. Pointer-free
`Array<v128>` values preserve both 64-bit ABI slots through default, uniform, and
fixed construction, get/set/fill/copy, and data-segment initialization. Fixed
constructors that exceed the 64-slot control frame pass one pointer to contiguous
off-heap native spill slots, removing the former 31-element vector limit without
putting a Go pointer in native state. Vector struct fields use the same two-slot
constructor/get/set protocol. Constructor
operands are mutable temporary roots across allocation.

This path compiles, links, and completes the start function of the 3,225,249-byte
MoonBit Starshine CLI payload with SHA-256
`3a92309ca48f80594c88ea6c3508982d6fc34953c018ce31786382e08a18d046`.
The smoke is environment-gated through `WAGO_STARSHINE_SMOKE_WASM`, or via
`make test-starshine STARSHINE_WASM=/path/to/cmd.wasm`, so the external payload
is not vendored into this repository.

Exact native frame-root publication is still incomplete for general generated
modules. Those modules therefore force `ProfileThroughput` and keep collection
disabled while a native invocation is active: objects remain stable, no
incomplete frame-root set is scanned, and one invocation that exceeds
`ThroughputHeapBytes` returns `gc: collection-disabled heap exhausted`.

The bounded linux/amd64 and Linux/Darwin arm64 products collect within admitted
native invocations. They cover local and same-domain call graphs, host re-entry,
module-local and shared GC globals/tables, direct and discarded-frame tail calls,
exact GC-reference parameters/results, local start functions, EH payload records,
and variable-size exact collector-reference root vectors in one frame. Imported starts and
unknown host/cross-runtime ownership remain fail-closed. Local-only or proven
same-domain `call_ref`/`return_call_ref` and direct/tail `call_indirect` retain
their existing ownership restrictions.

Compile admission builds exact structured-CFG local liveness for every reachable
allocation and native call. Functions through 64 tracked roots retain one-word
masks; 65-128 roots use two words; wider functions use one bounded flat word
arena rather than per-node heap bitsets. A non-collecting function may omit a
frame plan independently, so an unrelated wide leaf no longer disables an
otherwise exact module. The backends add hidden operand spills and fixed EH
roots, assign dense safepoint IDs, record call return PCs, and publish final frame
sizes. Parked synchronous control records expose actual off-heap qwords as
mutable collector roots, including caller-frame rewrites after moving collection.

The native walker follows validated return-PC maps through bounded direct local
call graphs and recursion. Throughput collect-every-allocation/forced-major verification and
Tiny collect/step-every-allocation preserve references in every recursive caller
while the deepest frame performs 1,000 allocations. Dead locals are omitted,
hidden operand roots survive control merges, and malformed IDs, offsets,
call-sites, and adapter returns fail closed. Codec version 2 persists and revalidates
safepoint, spill, callsite, frame-size, local-start, and adapter-return metadata;
repeated offset vectors share immutable storage after compilation and reload.
It never persists collector handles, liveness work arenas, or live frames.
`Compiled.GCNativeRootAdmission` reports exactness, map counts, maximum roots,
metadata bytes, and an actionable fail-closed reason. Five 500 ms samples of
`BenchmarkGCSingleNativeFrameRoots` measured 432.5-443.5 ns/op, 0 B/op, and
0 allocs/op on the Ryzen 7 8845HS host for boundary collection plus two parked
allocating-helper transitions.

For the table/element-free subset whose persistent GC references are module-local
globals, instantiation registers every such global as a checked collector slot.
`Invoke` synchronizes and scans those slots at boundaries, and every allocating
helper synchronizes mutable global cells before collection. Both boundary and
in-invocation collection are zero-allocation after warm-up and reuse object spans
and free-list metadata instead of growing metadata per cycle.
Exact staged official products retain their existing collectors, roots, barriers,
and stress coverage.

The synchronous helper boundary keeps its original 64-slot inline control frame
and derives a checked wider capacity for large struct constructors. Counts remain
u16 on the parked-call wire; modules above 64 append off-heap argument/result
areas, and capacities above 65,535 reject. The 403-field regression therefore
uses 404 slots and adds 6,472 off-heap control bytes only to that instance. Its
ordinary 64-slot frame size, generated small-call path, and `Compiled` size stay
unchanged. Runtime presents the appended 6,464 argument/result bytes directly
to the helper, so wide transitions add no Go allocation or copy scratch. Codec
reload derives the same capacity from immutable GC descriptors and never
persists live contents; the extension address is derived from the off-heap frame
base, so native state retains no Go pointer. Only wide instances acquire a
length-registry entry; the ordinary per-instance heap footprint is unchanged.
Fully initialized reference
constructors continue to classify every initializer word through the reusable
typed root scratch before collection.

A lazy
per-instance `gcPublicState` includes one mutex-protected 63-value constructor
scratch, 64 reusable host-result roots, 64 reusable host-ingress roots, a bounded
direct native-frame/root-chain adapter, one reusable foreign-clone handoff root,
and the generic-global root mapping plus cross-code-owner identity (3,768 bytes
total on amd64); each live result token adds
one 48-byte `gcRefTokenEntry` map value plus map overhead. The fixed state avoids
per-constructor, per-root-publication, and
per-boundary-collection Go allocations. On August 1, 2026, warmed same-domain
`GCRef` ingress measured 0 Go allocations per call after its first checked root
slot was created. Five benchmark samples of issue/use/release measured 574.1-584.8
ns/op for the basic struct product and 688.8-697.3 ns/op for the fixed numeric array
product, both at 0 B/op and 0 allocs/op. On July 31, 2026, five 500 ms samples of `BenchmarkGCArrayV128Set` measured 439.5-476.8 ns/op
with 0 B/op and 0 allocs/op on the Ryzen 7 8845HS host.

AMD64 scalar struct/array get/set operations and defined struct/array type checks
bypass the parked helper through native collector ABI version 1. Its 184-byte
collector-owned stable view preserves the complete handle/space/generation/object-card
and allocation-state prefix, then publishes the immutable packed subtype-interval
pointer and count. Collection and relocating/large-space allocation republish the
complete view; ordinary helper nursery allocation updates only handle metadata and
generation, card append/remove/clear republishes card metadata, and canonical-domain
type append republishes the interval backing. One instance-owned view publishes the
immutable local-to-canonical type map at basedata offset 280.

The Go/native structure layout is validated when a collector is created. Codec version 2
records the required native-GC ABI for generic struct/array execution and rejects a
missing or mismatched requirement while loading. Instantiation then validates the
immutable instance version, collector identity, local-type map pointer/count,
collector version, and handle stride before basedata publication. Production AMD64
operations do not reload those immutable guards per access; `-tags wagodebug` retains
a coarse Go-to-native entry assertion. Dynamic semantic checks are unchanged:
generated code reloads mutable handle/backing pointers and counts, then checks compact
handle tag/range/liveness, space and backing extents, object extent, canonical type,
array bounds, ownership/barrier state, and trap order before touching payload.
Non-final defined `ref.cast` and `ref.test` reload the interval table and apply
`required.pre <= actual.pre && actual.post <= required.post`; exact casts retain
canonical ID equality. Neither path retains a raw object pointer across a call,
allocation, helper, or safepoint.
Direct scalar/reference array paths zero-extend dynamic Wasm i32 indexes before
native-width scaling; logical bounds failures use the builtin trap category, while
physical object-extent hardening remains a cast-failure trap.

At modules with two or more candidate direct scalar sites, AMD64 may emit one
229-byte module-owned noncollecting compact-handle resolver leaf and patch local
`CALL rel32` sites to it. One-site modules keep inline resolution because the measured
crossover is unfavorable. When reuse is enabled for a single-function module, the
compiler first lowers inline and emits the island only if at least two actual handle
resolutions remain; repeated accesses collapsed to one resolution do not pay the fixed
island, while distinct objects still share it. The leaf cannot allocate, collect,
enter Go, publish roots, or become a safepoint; local trap stubs retain exact source
attribution. A separate one-entry derived-address certificate may reuse a successful resolution only across
an unchanged compact local and a mechanically safepoint-free straight-line region.
Calls, helper/host transitions, allocations, control/EH/tail edges, local writes,
mutable-fact invalidation, and all unknown opcodes clear it before lowering. Controls:
`WAGO_AMD64_NO_GC_SHARED_STUBS=1` and `WAGO_AMD64_NO_GC_RESOLVE_REUSE=1` restore
inline/no-reuse differential paths.

### GC roots, barriers, and resolved addresses

The former AMD64 structured-reference fact experiment was retired after broad
qualification found neutral execution and materially worse compile resources. GC
references now carry only the root bit required by exact frame planning. Reference
stores use the conservative barrier unless an independent native lowering proves a
no-barrier case. A separate one-entry resolved-address certificate may reuse a native
payload address only within a mechanically safepoint-free straight-line region; calls,
allocations, control edges, local replacement, and unknown effects invalidate it.

Dynamic subtype checks use the collector's validated descriptor forest. Each
canonical type owns one packed DFS `[pre,post]` interval; a four-parent shallow walk
keeps ordinary one-level hierarchies neutral, while deeper tests use constant-time
interval containment. Canonical representative remapping retains parent traversal.
The interval replaces the former `typeIndex` table byte-for-byte, keeping permanent
per-type memory and the 1,120-byte collector layout unchanged.

A former reference-fact experiment forwarded repeated dynamic `array.len` and
immutable `struct.get` results and used constructor-known lengths to specialize
constant-index array accesses. It was retired after broad measurement found neutral
execution and materially worse compile resources. The general one-entry resolved-
address cache remains and is invalidated at calls, allocations, merges, loop edges,
and unknown effects. The loop-versioning experiment was
removed after its broad execution benefit failed to justify duplicated lowering,
native code, and compile-resource cost; all memory32 and memory64 loops now retain
their ordinary per-access checks unless straight-line bounds facts or guard pages
provide the existing explicit certificate.

Dead allocation remains bounded and postfix. Direct struct/fixed-array drops can
remove complete nested `struct.new`/`array.new_fixed` trees only while every reserved
intermediate has a pointer-free or default-zero payload. Pointer-free uniform,
default-initialized, and pointer-free data-array drops call allocation-reservation
helpers after complete type, physical-size, and passive-segment-range preflight. The
helper preserves current bounded-heap exhaustion, collection, handle/allocation state,
and a real frame-root safepoint, but omits population of the unreachable zeroed
payload. Nested reservation helpers return the real compact allocation result rather
than a null placeholder, so payload-safe inner objects remain rooted across enclosing
allocations. A `struct.new` or `array.new_fixed` intermediate initialized with
references retains the full constructor path: replacing it with a zero payload would
hide transitive children from a collection during the next enclosing allocation.
Immediate top-level drops may still reserve such an object because no later allocation
observes it as a parent. Reference-valued uniform and element-segment constructors
also retain the full path because suppressing edges/cards could change later
minor-collection retention and capacity. Tiny-heap differential tests cover both
reference-struct and reference-array intermediates. This is intentionally less
aggressive than the earlier size-only preflight, which was not equivalent when prior
live allocations occupied the bounded heap.

Reference stores select an explicit late state: `NoBarrier`, `YoungParent`,
`KnownOldChild`, `ExistingCard`, `CardMark`, or `SlowBarrier`. Null and i31 children
need no object barrier. Generation fields and the two generation-named states remain
reserved but cannot currently select a compile-time no-barrier path: relocation
validity, concurrent-marking obligations, and remembered-set obligations must be
proved independently first. Object-reference stores continue through the checked
native struct/array stubs, which decide
nursery, existing-card, card-mark, and slow-helper cases from current collector
metadata. This keeps card growth, foreign/stale refs, malformed metadata, unknown
subtypes, and every required Tiny shade on the shared cold path.

Reference `array.fill` uses the ordinary checked helper and retains its exact
post-write destination range barrier. The guarded `Collector.ArrayFillNoBarrier`
compatibility helper remains available to runtime callers that can prove a null or
i31 child; it performs the same complete range, type, and value preflight and rejects
an object child before the first write. Reference fill/copy/init operations retain
overlap-safe copy and trap atomicity. Throughput `array.init_elem`
preflights the complete retained segment, then performs type-compatible prevalidated
stores with a deferred barrier and publishes one exact destination range after all
writes. No collection can occur between preflight and publication; explicit
hardening modes repeat ownership validation to detect contract misuse. Tiny keeps
immediate per-edge shading; bulk ranges are handled in 64-element chunks and each
between-chunk drain uses the same 256-entry/256-reference/1,024-byte resumable
object budget as a marking `Step`. The Tiny path validates the complete destination range
with widened arithmetic before deriving any element index. This bounds collector
scan work performed between chunks, but does not make the complete bulk mutation
call itself incremental. Numeric arrays and non-collector function-identity payloads
remain barrier-free.

Diagnostic `wago_gcstats` snapshots include separate dynamic checked-path counts for
`NoBarrier`, `YoungParent`, `KnownOldChild`, `ExistingCard`, `CardMark`, and
`SlowBarrier`. Native fast decisions remain represented by static JIT counters and
helper-transition deltas rather than adding diagnostic memory writes to release hot
paths.

Shared AMD64 stubs additionally cover final casts, final cast-plus-array-length,
final reference-array reads, and final cast-plus-reference-struct reads. Final
mutable `eqref`/`anyref` arrays perform a checked direct store in Throughput nursery
space and may also admit old/large parents when remembered membership and a validated
object-card slot already exist. The native path can widen that stable inclusive card
interval in place, but never appends or relocates card metadata. Final mutable
`eqref`/`anyref` struct fields admit Throughput old/large parents when the validated
child is not in nursery or the parent handle's stable remembered bit is already set.
A nursery child behind an unremembered old/large parent, a cardless old/large array,
every Tiny parent, and malformed metadata retain the unchanged helper with the full
remembered-set/card or incremental barrier. Conditional
lowering preserves hot pinned registers and emits local reloads only on the fallback
edge. Non-final declarations retain helper lowering. The former exact-reference-
fact specialization for open `struct.get` was retired with its default-off alternate
compiler path. Unknown dynamic subtypes, `v128`, bulk operations, and barrier states that require
metadata growth retain helper lowering. Current scalar
end-to-end measurements are 227.9–229.4 ns/op for struct set/get, 218.2–219.9 ns/op
for struct get, and 265.2–265.6 ns/op for array set/get; final cast/reference-struct
get measures about 230–237 ns/op, all at 0 B/op and 0 allocs/op.

Actual synchronous helper transitions can be measured without changing the default
hot path by building with `-tags wago_gcstats`. That diagnostic product exposes
`Instance.SetGCHelperStatsTracking(true)` and `Instance.GCHelperStats()`; production builds
compile the helper hook away. One collector domain may be selected process-wide at
a time; selecting another replaces it, and callers disable tracking after measurement
to release the diagnostic reference. The counters separate total, allocation, struct/array mutation, reference-mutation,
parent-space, and old-to-young remembered-state calls. Before the old-struct fast
path, one fresh Dew invocation after dead-constructor elimination and exact-reference
propagation mapped static `hostsync=6,140` to 1,724 executed transitions: 1,038
allocating constructors and 686 mutation fallbacks, all with old parents. The checked
old/large struct path removes 426 of those transitions. ABI v2 existing-card array
reconciliation removes another 254, leaving 1,044 total: the same 1,038 allocations
plus six mutations (four cardless/unremembered array stores and two unremembered
old-to-young struct stores). These per-call counts remain exact through 100 and 500
repeated invocations. Allocation-family counters further split the remaining 1,038
calls into 1,026 fully initialized `struct.new` transitions and 12
`array.new_default` transitions, with zero struct-default or other-array calls.
Native bump allocation therefore targets the initialized struct path rather than
being justified only by static constructor sites. The retained AMD64 path reserves
32 unpublished handle identities without reserving nursery bytes. Its generated
stub validates the batch epoch, free handle, canonical final type, aligned physical
extent, and every nullable/non-null compact-reference initializer before writing
header/fields, advancing the real nursery bump, publishing the handle, and updating
semantic allocation count. Collection and Close cancel and recycle unused identities.
Tiny, collection-disabled, collect-every-allocation, unsupported layouts, malformed
metadata, and nursery exhaustion retain the rooted helper. Dew falls to 50 executed
helpers per call: 32 initialized structs, 12 default arrays, and six metadata-growing
mutations; counts remain exact through 500 calls. The differential control is
`WAGO_AMD64_NO_GC_NATIVE_ALLOC=1`.

Fully initialized helper-side `struct.new` prevalidates field kinds, ownership,
subtyping, and nullability once, then passes raw helper ABI words directly to the
collector. A reusable word-root scratch exposes object-reference words as mutable
roots across a collecting allocation, after which the collector stores the raw
numeric/reference/vector words without constructing an intermediate `[]Value`.
The generic public constructors keep their complete independent validation.

A pinned single-CPU benchmark pass on July 16, 2026 (Ryzen 7 8845HS, Go 1.24.4)
measured Starshine at 1.027 s for the pre-link compile/classification stage,
0.602 s for an isolated cold link/JIT from a fresh unlinked product, 1.522 s for
end-to-end compile+link,
and 31.7 ms for linked instantiate/start. The cold Starshine link/JIT allocated
166.2 MB in 565,697 allocations; this and the 74.8 MB/448,851-allocation compile
front half are explicit optimization targets rather than footprint claims.

Codec version 2 persists generic helper admission, the required native-GC ABI version,
vector layout, and bounded native root maps; compact handles remain process-local.

Numeric host imports may re-enter the same instance: codec version 2 callsites carry
stack adjustments, a bounded eight-entry activation stack preserves control
state, nested invocations borrow separate foreign stacks with the active Runtime's
configured 512 KiB-through-1 GiB capacity, and suspended outer frames remain roots
during boundary and helper collection. The default remains 4 MiB. A bounded
same-Runtime cross-instance product additionally canonicalizes recursive structural
types across generic-GC modules and gives compatible reordered/additional local type
graphs one collector. Exact GC-reference parameters/results retain
identity, and the native walker switches code/root-map ownership when a return PC
enters another live instance. Imported/exported mutable and immutable GC globals
share their actual off-heap cells: every live domain instance contributes those
cells directly during collection, while checked slots retain barrier/card state.
Codec reload, failed-domain rollback, producer-before-consumer close, Throughput,
Tiny, amd64, and ARM64 execution are covered. Multiple heterogeneous
imported/exported collector-reference tables share direct indexed descriptor roots,
table.grow state, attachment rollback, codec reload, and producer-first close. Exact
GC-reference function imports coexist with those shared globals and tables under
Throughput/Tiny collection and retain foreign-frame roots after producer-first close.
Host reference transfer, incompatible collector configurations/descriptors, and
mixed unproved host/cross-instance import graphs reject transactionally.

Generic struct/array helpers and exact frame roots execute under linux/amd64
guard-page bounds checks. Linux and Darwin arm64 explicit-bounds builds lower
struct, array, i31, conversion, cast/test, and branch-cast operations through the
same synchronous helper ABI. Abstract `eq`/`i31`/`struct`/`array` null globals use
ordinary zero-valued constant expressions. Indexed function-identity lowering is
selected only for function heap targets; GC struct casts continue through collector
supertype metadata, including final concrete closure structs cast to non-final bases.
Arm64 publishes liveness-exact locals and hidden
spills from parked SP. Codec version 2 callsites carry caller frame size, return PC,
stack adjustment, and exact roots; saved-LR walking spans direct/recursive calls,
suspended direct-host activations (including sync-thunk records), and same-domain
foreign instances. Mutable local GC globals synchronize checked collector slots,
one private collector-reference table is scanned directly, and indirect/reference
calls, function subtype checks, proper tails, and fixed EH payload records publish
exact maps. AMD64 compact finalization does not apply `local-slot-order` to a
function with exact GC frame-root metadata. The final local homes therefore stay
identical to the offsets recorded by every safepoint and caller map. Wrapper calls
stage any operand stack whose later spilled source sits
below its canonical destination. This preserves leading arguments for wide
reference signatures even below the 64-slot wide-stack threshold. It also stages
before materialization when GP pressure would consume the scratch-register
reserve or XMM pressure would exhaust the vector register file, so a deferred
argument cannot create a new overlapping spill. The Dewdrop
reproducer used one nullable reference followed by 17 non-null references and
previously propagated the leading null through the first five non-null argument
slots. Dynamic host and same-domain foreign direct tails discard the current
frame, so no dead caller roots are retained. A direct immutable-root visitor keeps
warmed Throughput/Tiny recursive collection allocation-free. ARM64 polymorphic
local `call_indirect` and same-domain foreign `call_ref` publish every possible
internal, wrapper, and cross-instance return PC; foreign wrapper maps account for
the 64-byte saved caller-invariant area. Descriptor-resolved `return_call_ref` uses bounded discarded-frame transfers for
local internal entries, host wrappers, and retained same-Runtime cross-instance
wrappers, including descriptors loaded from mutable/imported funcref tables. The
native context carries stable numeric collector-domain identity separately from
linear-memory ownership; GC-bearing typed tails compare those identities before
frame discard. Ordinary host or foreign-domain GC transfer stays fail-closed rather
than scanning approximate roots. An explicit `Runtime.NewGCHostFuncRef` is the bounded
exception: first import binds the owner to one exact canonical Runtime collector domain;
parked direct/indirect/reference calls convert object arguments to temporary opaque
`GCRef` tokens, validate result tokens structurally, and hold per-activation checked
argument/result roots until the native lease is reacquired. Proper host tails discard
the caller frame only after the same numeric domain check and reuse the wrapper result
pointer without retaining the caller.

Foreign Runtime transfer now has one explicit non-sharing model:
`target.CloneGCRefFrom(source, ref)` captures the retained source root into stable
object IDs, maps every object to a structurally equivalent target-local type, then
allocates and populates the target graph in two rooted passes. Cycles and sharing are
preserved inside the clone, but the result intentionally receives new target identity.
Each call is bounded to 1,024 objects, 65,536 values, and 1 MiB of payload. Nulls,
i31 immediates, numeric/vector payloads, and collector references transfer; non-null
funcref/externref payloads reject because their stores remain independent. A reusable
checked handoff root closes the gap between reconstruction and public-token issuance,
and failure clears that root and performs a target collection. Direct compact-handle
sharing and GC-bearing cross-Runtime calls remain rejected.

Iteration 38 wires one exact linux/amd64 numeric-local helper product;
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
Iteration 67 adds only the M3 struct-defined provider/consumer pair. Its immutable recursive struct field and empty
companion structs participate in function identity, while instances retain ordinary canonical descriptors and allocate no
collector or GC object. The provider and consumer each own 64 descriptor bytes; the one compatible import retains one
producer transactionally and both close orders release exactly once. Their wasm/code/codec sizes are 70/77/313 and
51/0/236 bytes, and empty `g` measures 38.46–51.80 ns/op with zero allocations. Iteration 68 adds only the M4
struct-projection provider/consumer pair. Three exact two-member recursive groups and five ordered immutable fields affect
function identity without producing a struct value; the provider and consumer again own 64 descriptor bytes, retain one
producer transactionally, and release it in both close orders. Their wasm/code/codec sizes are 104/77/482 and 85/0/405
bytes, and empty `g` measures 37.05–39.08 ns/op with zero allocations. Iteration 69 adds only the M5
provider/expected-unlinkable pair. Complete recursive-group comparison preserves selected-member position and distinguishes
bound references from equivalent-looking external references, so the provider's second group cannot flatten into the
consumer's self-recursive requirement. The provider owns 64 descriptor bytes; the attempted consumer has the same bounded
requirement but rejects before retention or publication. Their wasm/code/codec sizes are 82/77/403 and 51/0/236 bytes,
and provider `g` measures 36.78–37.82 ns/op with zero allocations. Iteration 70 adds only the M6 provider/consumer pair.
Its two independent self-recursive function/struct groups remain distinct while `g <: f1` links exactly. Both products own
64 descriptor bytes, retain one producer transactionally across both close orders, and keep `Instance.gc` nil. Their
wasm/code/codec sizes are 82/77/403 and 63/0/326 bytes, and provider `g` measures 37.44–42.95 ns/op with zero allocations.
Iteration 71 adds only the M7 provider/two-import consumer pair. Its fourth-group `h` extends the provider projection and
satisfies both consumer root and projected views; provider/consumer arenas are 64/96 bytes, duplicate imports retain one
producer, and both instances keep `Instance.gc` nil. Their wasm/code/codec sizes are 114/77/561 and 102/0/502 bytes, and
provider `h` measures 36.65–38.72 ns/op with zero allocations. All thirty-eight admitted leaders instantiate, or fail
before consumer publication, without allocating a collector. Seven leaders remain gated and 12 dependent commands remain
blocked. General native frame publication, object-valued mutable/reference globals, broader linking ownership, public
ownership, and snapshots remain incomplete. These bounded products must not be presented as general executable WasmGC support.

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
- scalar storage kinds (`i8`, `i16`, `i32`, `i64`, `f32`, `f64`), pointer-free 16-byte `v128`, and ref storage kinds.
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

Array elements and struct fields lower `v128` as 16-byte-aligned, pointer-free storage. Their helpers consume and produce two ABI slots. Scalar, packed, and vector numeric storage is pointer-free. Ref-typed fields/arrays are scanned exactly; scanner logic ignores `null` and `i31` values at runtime.

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

Payload begins at `PayloadOffset == HeaderSize`, currently 16 bytes. Logical object sizes remain at least 8-byte aligned; nursery, Throughput, and compatible Tiny allocations additionally align objects to the descriptor requirement through 16 bytes. Heap backing slices are explicitly aligned rather than relying on Go allocator alignment. Tiny configurations with 8-byte blocks reject descriptor tables containing 16-byte-aligned storage. Heap memory stores header fields in little-endian byte arenas, not as Go object pointers.

## Compiled metadata and instantiation

Frontend lowering produces immutable descriptor metadata during compile. The same
flattened type-section traversal also builds a compiler-only table containing each
type's recursive-group bounds, struct field offsets/sizes/initializer slots,
array-element layout, alignment, object size, and collector-reference classification.
Railshot indexes that table directly while compiling GC instructions instead of
walking recursive groups and preceding fields at every use. The table points at the
immutable decoded subtype declarations and is discarded after code generation; it is
not retained by `Compiled` or serialized. `Compiled.GCTypeDescs` stores the runtime
descriptor slice so `.wago` blobs can instantiate without re-decoding the Wasm type
section. The descriptor slice index matches flattened `wasm.TypeIdx.Index`, including
function sentinels used only to preserve indexes. Codec version 2 retains the appended
`StorageV128` kind, the 16-byte layout contract, and validated native
safepoint/callsite root maps; older codec versions are rejected.

Each `Instance` normally owns its own `gc.Collector` when its executable product can create or retain heap objects. Collectors are never shared across instances: nursery state, old-space state, roots, remembered sets, cards, and collection statistics are per-instance runtime state. MVP/non-GC modules keep `Instance.gc == nil` to avoid allocating an unused heap.

Iteration 37 adds one deliberately narrower exception. Ten exact pinned `type-rec` products contain struct descriptors only because recursive struct definitions participate in function identity. Their functions, immutable globals, imports, and ordinary funcref tables carry only function descriptors; no struct/array value or GC opcode exists. A compile-only, non-serialized sidecar records that exact product proof and keeps `Instance.gc == nil` even though `gc.HasHeapObjectTypes` is true. Unknown binaries, arrays, mutable fields, additional state, public codec load, snapshots, guard mode, and arm64 remain closed. A codec-reloaded artifact does not inherit this live admission sidecar. This is metadata/function-identity execution, not WasmGC heap execution.

Iteration 38 added a separate exact numeric-local helper product with one allocation point
and a proven empty live-ref set. Iteration 39 added two collector-owned immutable global
slots, not frame roots: each slot is installed before a later initializer allocation. The native-frame publication slice now records function-relative safepoint IDs, exact
structured-CFG local liveness, hidden operand spills, and direct self-call return-PC maps for
linux/amd64 local functions. Codec version 2 persists and revalidates that metadata, including
caller stack adjustments, and the runtime walks cross-function, recursive, and suspended host
activations through mutable off-heap slots. Mutable module-local global slots synchronize before
allocation. Private local `call_indirect` and tail-indirect calls now participate in exact frame walking,
and one private mutable collector-reference table is scanned as actual off-heap root slots.
Local-only `call_ref`/tail-ref and EH fixed GC-payload roots now participate in
collection. Cross-instance collector ownership and broader persistence products
remain later slices.

## Native exception-root map contract

Iterations 34-35 define the bridge between amd64 exception frames and the collector/
lifecycle layers. `src/core/nativeabi` owns dependency-neutral `FunctionRootMap` and
`RootSlot` records; `src/core/runtime/gc/native` aliases and validates them at the collector
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

### Iteration 67 M3 struct-defined linking pair

The source-lines-566–572 pair is pinned independently from iterations 65–66. The command-line-442 provider is 70 bytes
with SHA-256 `ac63802e3827e33389d92ff8a8bd25b6231f1dde96bab5cb77a0e1d094f80e6f`; the command-line-450
consumer is 51 bytes with SHA-256 `5f090989edc62437b56b36c69a316cdcfddec4a63d451bd9443ad59da75af0a3`.
Both contain two two-member recursive groups. The first group is an open `() -> ()` function paired with a final struct
whose sole immutable field is a non-null reference to that function; the second is an open function subtype paired with
a final empty struct. Exact AST and byte-backed validation prove the complete graphs and the compatible provider `g` to
consumer `M3.g` relation before classification and pin checks.

Struct definitions remain metadata-only. No struct/array instruction or value executes, no compact `gc.Ref`, checked
collector root, barrier, card, remembered set, or collector object appears, and both instances keep `Instance.gc == nil`.
The provider and consumer each own a 64-byte arena containing null plus one ordinary 32-byte function descriptor. The
consumer copies the provider's nonzero code pointer and canonical identity word, retains the one distinct producer exactly
once, rolls that owner back on failed instantiation, and releases it correctly in both provider-first and consumer-first
close orders. Hosts, iterations 65–66, widened namespaces/fields, and structurally similar unpinned products reject.

Provider/consumer wasm/code/codec sizes are 70/77/313 and 51/0/236 bytes. Empty `g` measures 38.46–51.80 ns/op,
0 B/op, and 0 allocs/op across three samples. Unlinked codec v27 preserves exact type/function/import/export metadata but
no admission marker; a live linked consumer cannot serialize. Private/public reload, snapshots, signal-backed bounds,
host and cross-product substitution, arm64, unsupported platforms, and public GC admission remain fail-closed. Strict
accounting is now 170 commands / 31 passed modules / 23 passed assertions / 14 gates / 17 blocked commands / 24 invalid /
5 executed expected unlinkables / 3 blocked unlinkables / zero validator gaps, unexpected compile/link failures, or hidden
failures. The source-lines-578–588 M4 provider/consumer pair is next and must remain separate from source-line 598 and the
source-line-605 unlinkable.

### Iteration 68 M4 struct-projection linking pair

The source-lines-578–588 pair is pinned independently from the M3 classifier. The command-line-460 provider is 104 bytes
with SHA-256 `8de41fdb1e1b4ef57639e5a6344eed6c13bfb5ada5ea56433bb221f403c56d8e`; the command-line-470
consumer is 85 bytes with SHA-256 `a5d3e6060f52fa0becf68e6e4dd06623df6ecf7bf22bfe5430b484f2adbdf0a2`.
Both contain three complete two-member recursive groups. The first two groups each pair an open `() -> ()` function with
an open struct whose sole immutable field is a non-null reference to that group's function. The provider's final function
extends the second root function and its companion struct extends the second root struct with ordered fields
`f1,f2,f1,f2,g2`; the consumer's final function extends the first root function and its companion struct extends the first
root struct with `f1,f1,f2,f2,g1`. Exact AST and byte-backed validation prove every group/member, flat or recursive super
edge, field order, empty provider body/export, `M4.g` import, registration order, and the compatible cross-module relation.

These struct definitions remain metadata-only. No struct/array instruction or value executes, no compact `gc.Ref`, checked
collector root, barrier, card, remembered set, or collector object appears, and both instances keep `Instance.gc == nil`.
The provider and consumer each own a 64-byte arena containing null plus one ordinary 32-byte function descriptor. Linking
copies the provider's nonzero code pointer and canonical identity word, retains the one distinct producer exactly once,
rolls that owner back on failed instantiation, and releases it correctly in both provider-first and consumer-first close
orders. Hosts, M3 and earlier link products, widened namespaces/fields, and structurally similar unpinned products reject.

Provider/consumer wasm/code/codec sizes are 104/77/482 and 85/0/405 bytes. Empty `g` measures 37.05–39.08 ns/op,
0 B/op, and 0 allocs/op across five samples. Unlinked codec v27 preserves exact type/function/import/export metadata but
no admission marker; a live linked consumer cannot serialize. Private/public reload, snapshots, signal-backed bounds,
host and cross-product substitution, arm64, unsupported platforms, and public GC admission remain fail-closed. Strict
accounting is now 170 commands / 33 passed modules / 23 passed assertions / 12 gates / 16 blocked commands / 24 invalid /
5 executed expected unlinkables / 3 blocked unlinkables / zero validator gaps, unexpected compile/link failures, or hidden
failures. The source-lines-598–605 M5 provider/unlinkable pair is next and must remain separate from source line 614.

### Iteration 69 M5 bound/external recursive-group mismatch

The source-lines-598–605 pair is pinned independently from M3/M4. The command-line-479 provider is 82 bytes with SHA-256
`0494d7c95b50e151ac8e0f9eb8a1c935a016db45b1969378ed95d40369fda062`; the command-line-487 expected-unlinkable
consumer is 51 bytes with SHA-256 `bb598cc89f2d73720190e6c7e115bec104013bf8ebead4c417d17e701598c7a1`.
The provider contains three complete two-member groups. The first pairs root `f1` with a final struct holding a bound
non-null reference to `f1`; the second pairs root `f2` with a final struct whose field refers externally to `f1`; the final
pair is `g2 <: f2` plus an empty final struct. The consumer contains the M3-like two-group requirement: root `f1` and its
bound self-referential struct, then `g1 <: f1` and an empty final struct. Exact AST and byte-backed validation prove every
member, field, super edge, empty provider body/export, `M5.g` import, and registration order.

Cross-module descriptor equivalence now compares each complete recursive group at the selected member position and checks
whether every defined reference is bound to the current group or external to it before following structural equivalence.
The provider's external `f1` reference therefore cannot satisfy the consumer's bound self-reference. Instantiation rejects
with signature mismatch before owner retention or consumer publication. No live consumer binding exists, and no close-order
retention claim is made. The provider owns a 64-byte arena containing null plus one ordinary canonical descriptor; the
attempted consumer's finite requirement is also 64 bytes. Both products contain no struct/array opcode or value, compact
`gc.Ref`, collector root, barrier, card, remembered set, or object, and `Instance.gc` remains nil.

Provider/consumer wasm/code/codec sizes are 82/77/403 and 51/0/236 bytes. Empty provider `g` measures 36.78–37.82 ns/op,
0 B/op, and 0 allocs/op across five samples. Failed linking leaves the original unlinked consumer serializable and retains
zero provider owners. Private codec reload inherits no product marker, public reload rejects unknown GC bits, snapshots
reject WasmGC reference products, and signal-backed bounds, host and cross-product substitution, arm64, unsupported
platforms, and public GC admission remain fail-closed. Strict accounting is now 170 commands / 34 passed modules /
23 passed assertions / 11 gates / 14 blocked commands / 24 invalid / 6 executed expected unlinkables / 2 blocked
unlinkables / zero validator gaps, unexpected compile/link failures, or hidden failures. The source-lines-614–621 M6
provider/consumer pair is next and must remain separate from source line 628.

### Iteration 70 M6 independent struct-defined linking pair

The source-lines-614–621 pair is pinned independently from M5. The command-line-496 provider is 82 bytes with SHA-256
`7c8af0765c2e2d43a07e7a6a75a85d396531827c1b2cb4402a24277308781dff`; the command-line-504 consumer is 63 bytes
with SHA-256 `a593d0db0e5f173aaac2d6007b84a4b268d7ad2047a4e8cd8fe3a275ef9b0820`. Both contain three two-member
recursive groups. The first two groups pair open `() -> ()` functions with final structs whose immutable non-null fields
refer to their own recursive function member. The last group contains `g <: f1` and an empty final struct. Exact AST and
byte-backed validation prove all members, both independent self references, the flat super edge, empty provider body/export,
consumer `M6.g` import as type `f1`, registration order, and the compatible provider-source to consumer-required relation.

Struct definitions remain metadata-only. No struct/array instruction or value executes, no compact `gc.Ref`, checked
collector root, barrier, card, remembered set, or collector object appears, and both instances keep `Instance.gc == nil`.
Provider and consumer each own a 64-byte arena containing null plus one ordinary 32-byte function descriptor. The consumer
copies the provider's nonzero code pointer and canonical identity, retains one distinct producer transactionally, rolls
that owner back before publication on invalid export, and releases it under both provider-first and consumer-first close
orders. Hosts, M5 and earlier providers, widened namespaces/self-reference forms, and unpinned structural lookalikes reject.

Provider/consumer wasm/code/codec sizes are 82/77/403 and 63/0/326 bytes. Empty `g` measures 37.44–42.95 ns/op,
0 B/op, and 0 allocs/op. Unlinked codec v27 preserves exact recursive metadata but no admission marker; linked
serialization, private/public reload admission, snapshots, signal-backed bounds, host/cross-product substitution, arm64,
unsupported platforms, and public GC admission remain fail-closed. Strict accounting is now 170 commands / 36 passed
modules / 23 passed assertions / 9 gates / 13 blocked commands / 24 invalid / 6 executed expected unlinkables / 2 blocked
unlinkables / zero validator gaps, unexpected compile/link failures, or hidden failures. The source-lines-628–639 M7 pair
is next and must remain separate from source line 652.

### Iteration 71 M7 extended recursive projection linking

The source-lines-628–639 pair is pinned independently from M6. The command-line-515 provider is 114 bytes with SHA-256
`4b62738d11270b2ac5b43e5de7c8105ddac449d9f23de09ce44160996f302d62`; the command-line-526 consumer is 102 bytes
with SHA-256 `5992b926a2f2ed28c4e6d5149d97ff075c0e6fdbcc9fb8ec8194fa96271405db`. Both contain four two-member
recursive groups. Their first two groups are open self-referential function/struct pairs. The provider's projected third
group extends the second root pair with fields `f1,f2,f1,f2,g2`; the consumer extends the first pair with
`f1,f1,f2,f2,g1`. Each fourth-group `h` extends its own projected function and carries an empty final companion.
Exact AST and byte-backed validation prove every member, super edge, field order, provider body/export, both `M7.h` imports,
registration order, and the compatible provider `h` source to consumer `f1` plus `g1` requirements.

Struct definitions remain metadata-only. No struct/array instruction or value executes, no compact `gc.Ref`, checked
collector root, barrier, card, remembered set, or collector object appears, and both instances keep `Instance.gc == nil`.
The provider owns a 64-byte arena; the two-import consumer owns 96 bytes. Both imported descriptors retain the provider's
nonzero code pointer and canonical identity, while owner retention deduplicates to one producer. Invalid exports roll back
before publication, and both provider-first and consumer-first close orders release exactly. Hosts, M6 and earlier products,
widened projections/import order, and unpinned lookalikes reject.

Provider/consumer wasm/code/codec sizes are 114/77/561 and 102/0/502 bytes. Empty `h` measures 36.65–38.72 ns/op,
0 B/op, and 0 allocs/op. Unlinked codec v27 preserves exact recursive metadata but no admission marker; linked
serialization, private/public reload admission, snapshots, signal-backed bounds, host/cross-product substitution, arm64,
unsupported platforms, and public GC admission remain fail-closed. Strict accounting is now 170 commands / 38 passed
modules / 23 passed assertions / 7 gates / 12 blocked commands / 24 invalid / 6 executed expected unlinkables / 2 blocked
unlinkables / zero validator gaps, unexpected compile/link failures, or hidden failures. The source-lines-652–659 M8 pair
is next and must remain separate from source line 668.

## Collector lifetime

`Collector.Close` is idempotent and releases heap backing storage plus root/card/mark metadata so an instance shutdown does not retain guest refs. After close, operations that need a live heap return `gc: collector closed`: allocation, collection, verification, object access/mutation, promotion, and checked root-slot creation/access/mutation. `Step` follows the same rule for both profiles; on Throughput it routes through `CollectMinor`, and on Tiny it rejects the closed collector before advancing incremental state.

`Stats` is intentionally safe after close for shutdown diagnostics. Allocation and collection counters are not incremented by rejected closed operations; `LiveObjects` is recomputed from the released handle table and therefore reports zero after close. Unchecked nullable slot readers (`GlobalSlot` and `TableSlot`) cannot return an error, so after close they return `null`; callers that need to distinguish close from an empty slot must use the checked accessors before shutdown.

## Heap profiles and architecture

`gc.Config.Profile` selects one of the supported allocator/runtime presets:

- `ProfileThroughput` is the zero-value/default profile. It pairs the
  `AllocatorPagedSizeClass` allocator with the `RuntimeGenerational` scaffold.
- `ProfileTiny` pairs the `AllocatorTinyFixedBlock` allocator with the
  `RuntimeIncrementalMarkSweep` runtime. Builds with
  `wago_tiny_nonincremental` retain the same API/profile and fixed heap but make
  `Step` complete one synchronous mark/sweep cycle, allowing the linker to drop
  incremental root/object/sweep policy code.

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

The Throughput young generation is Eden followed by two bounded survivor
semispaces in one aligned backing. `NurseryBytes` remains the hard Eden allocation
limit; `SurvivorBytes` selects each semispace and defaults to half of Eden.
Generated allocation uses the separately published Eden limit, while ordinary
handle resolution sees the complete backing. `DisableMovingNursery` remains the
A/B and compatibility control for immediate first-survival promotion.

Minor collection traces only exact transient roots, dirty persistent-root slots,
dirty old/large cards, and the resulting live young graph. First survivors copy
contiguously into the inactive semispace, preserving compact handle identity while
changing only the handle offset. The next successful minor swaps semispaces.
Two age bits use previously unused high `handleEntry.class` bits, retaining the
20-byte handle layout. Large young objects use the same age state but remain in
large backing; they become old in place rather than copying their payload.

The initial tenuring threshold is two survivals and remains bounded from one
through three. Survivor occupancy, old-space pressure, recent full collections,
and promoted-versus-copied bytes update it deterministically. A nonzero
`MinorPauseTargetMicros` additionally opts policy adaptation into one minor-cycle
clock sample; zero performs no release-build clock read. Pointer-free and
reference-bearing age populations remain separately visible through
`wago_gcstats` telemetry.

Movement planning covers survivor copies, old-space destinations, and in-place
large aging before any handle is published. Destination and commit failure points
run before inactive survivor bytes are written; old-space allocations roll back
in reverse order. Native handle batches are invalidated before tracing. Useful
cards and dirty roots remain authoritative across survivor movement, while
card-range pruning examines only recorded ranges rather than complete old
objects. Metadata clears directly once no young object remains.

Objects promoted into old space are rounded into supported size classes.
`ThroughputClassLimit` must be zero for the default or exactly one built-in class
(`32` through `32768` bytes); unsupported values reject rather than round. The
fixed 64-bit layouts are 20 bytes for `handleEntry`, 72 bytes for `Config`, and
1,120 bytes for `Collector` in the ordinary build. The 16-byte increase is the
native allocation state for contiguous handle runs and nursery chunks; ABI version 1
now includes the 16-byte subtype-interval pointer/count suffix in the collector view.

Allocation-triggered minor collection treats old-space promotion exhaustion as a
cold reclamation signal. Because promotion planning has published no move, the
allocator may run one full collection with the same exact roots and retry the
minor promotion transactionally. Direct callers of `CollectMinor` retain the
original fail-closed error and unmoved-survivor contract. If the live graph still
cannot fit, the retry returns the same bounded throughput-exhaustion error. A
16 MiB Dew Map/Set instance that previously exhausted before 100 repeated calls
completes 500 calls under this policy.

The old/large backing grows exactly below 16 pages, then reserves at most 1.5x
the current committed backing through a noinline cold helper, capped by
`ThroughputHeapBytes`. This avoids repeated page-step copies without adding a
hot fresh-allocation branch; unused reserved capacity appears only after the
backing is already hot.

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
  allocation-time incremental/full collection stress behavior. `TinyStepBudget`
  retains that exact step-count meaning.
- `TinyPacingStepLimit` bounds ordinary allocation-debt work before one
  allocation; zero selects one and values above 32 are rejected.
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
run object barriers are still observed. Transient roots are captured atomically
at each safepoint through allocation-free direct visitors. The old 1,024-root
semantic ceiling is removed; arbitrary callback-only root sets still require a bounded direct-enumeration interface.
Collector-owned globals and tables resume through a stable index cursor at
most 256 slots per `Step`, including both initial mark and remark. Sweep walks handle indexes and frees
white Tiny objects back to the fixed-block allocator. Remark snapshots a finite
handle-table endpoint, so allocations appended during sweep are protected without
extending the active cycle. One sweep `Step` visits at most 64 handles and accounts
at most 256 allocation blocks; one oversized span is handled alone. With `PoisonFreed`, clearing is capped at 4,096 bytes and resumes
through a stable handle/byte cursor before the span is released, so a large
configured block or object cannot make one step scale with its size. `CollectFull` completes one whole Tiny cycle.
`CollectMinor` is specified as the same full Tiny cycle because Tiny is
non-generational.

Tiny clears logical mark state in O(1). Each handle retains one byte whose low
seven bits identify its mark epoch and whose high bit distinguishes current-epoch
gray from black. Advancing the epoch makes every older state logically white,
so cycle start does not walk the handle table and sweep does not rewrite black
survivors. Idle and pointer-free sweep-protected allocations are black in the
current epoch; pointerful mark/remark/sweep allocations are born gray. Sweep-time
initialized constructors therefore queue their complete payload for bounded tracing
before reclamation resumes. A synchronous `CollectFull`
restart advances again to a third epoch, distinct from both current marks and the
preceding white population. Normal completion and restart therefore remain safe
across the 128-value wrap without increasing per-handle metadata.

Object marking is resumable. Tiny retains at most one compact active cursor:
one stable handle plus the next struct descriptor entry or array element index.
Each resume reacquires the descriptor and object bytes from the handle; no raw
payload pointer survives a `Step`. The active object remains gray and is absent
from the gray stack until its final outgoing reference is visited, at which point
it becomes black. Newly discovered children retain the previous descriptor/array
visitation order and are processed after the active object completes.

One marking `Step` is limited to at most 64 object-range setups, 256 descriptor
or array entries, 256 reference slots, and 1,024 accounted payload bytes. A large
dense reference array therefore advances by at most 256 elements per marking
step. A large sparse-reference struct is also split because every descriptor
entry, including numeric fields, consumes entry and storage-byte budget. These are
internal object-tracing limits; `TinyStepBudget` keeps its existing meaning as the
number of `Step` calls performed after an allocation when `TinyStepEveryAlloc` is
enabled.

Tiny's resumable path and diagnostic complete scans use one cursor/range
primitive. The ordinary Throughput/full release path deliberately retains its
specialized direct complete loop: identical-configuration A/B measurements found
that cursor-budget bookkeeping regressed dense full scans, while the direct loop
remains within the established 3% control. Both paths preserve the same descriptor
and element order. Pointer-free objects are not recursively scanned, struct ref
fields are loaded only at descriptor offsets, ref arrays scan elements, numeric
bits are never interpreted as refs, and `null`/`i31` values are ignored. Global
and table slots are part of the root set for both full and incremental Tiny
collection. Appended persistent slots remain ahead of the cursor, while stores
behind it retain the ordinary Tiny insertion barrier; the complete remark pass
therefore observes root movement without retaining mutable slot interfaces or
raw object pointers across `Step` calls. On the Ryzen 7 8845HS host, a 256-slot
persistent-root step measures about 520-526 ns with 0 B/op and 0 allocs/op,
independent of whether the collector owns 256, 4,096, or 65,536 slots.

Tiny write barriers preserve the incremental no-black-to-white invariant.
Object stores retain the existing conservative hybrid policy for black parents:
the white child is grayed (forward barrier) and the parent is re-grayed (backward
barrier). A store into a gray parent now also shades a white child, because a
partially scanned parent's cursor may already be past the mutated slot. This
conservatively covers writes both before and after the cursor without adding
cursor-position checks to the mutator path. Handles already gray are not pushed
to the gray stack again. Slot and object stores during sweep pause the sweep
cursor, enqueue the child, and resume sweep only after ordinary bounded marking
steps complete; barriers no longer trace a complete graph synchronously. Checked
stores reject a white pointerful graph whose earlier descendants may already
have been reclaimed. Pointerful objects allocated during active Tiny marking are born gray so
array/ref initialization cannot publish an unscanned black object with white
children.

Tiny manages WasmGC heap objects only. It is separate from Wasm linear memory
allocation. Iterations 38-39 connect exact numeric-struct parked-Go helper products,
including rooted immutable globals and packed fields; broader WasmGC opcode lowering and
backend integration remain incomplete.
It is also not a replacement for the future GenImmix/default policy; it is a
bounded, predictable option for constrained targets where a fixed maximum heap,
stable object addresses, compact metadata, and deterministic allocation failure
are more important than moving/generational throughput.

Known Tiny limitations in this foundation:

- transient roots use a hard 1,024-reference direct-visitor bound rather than a
  resumable cursor because native frame roots may change when the mutator resumes;
- allocation may consume existing or already swept free spans without draining
  the remaining cycle. Ordinary allocations accrue physical-span debt and buy one
  collector `Step` per 1,024 bytes, capped by `TinyPacingStepLimit` before each
  allocation. An allocation miss adapts to near exhaustion with at most eight
  times that configured work and an absolute 32-step ceiling, then fails
  explicitly rather than synchronously draining an arbitrary cycle. A reference graph published through an external `WriteBarrierRoot`
  during sweep must remain in the exact supplied roots until publication; the
  current one-pass sweep cannot resurrect descendants already reclaimed from an
  omitted graph. Checked collector global/table setters fail before mutating their
  slot when asked to publish a white pointerful graph during sweep, while
  pointer-free objects remain safe for immediate marking;
- Tiny bulk barriers validate ranges with widened arithmetic and chunk mutator
  publication. The complete bulk mutation remains proportional to its requested
  range, but collector graph tracing between chunks and after sweep publication
  uses the fixed Step work vector;
- collection is incremental by explicit `Step` calls or allocation-time stress
  knobs, not concurrent;
- handle-table entries remain the stable ref indirection; and
- the default product remains incremental; `wago_tiny_nonincremental` is an
  explicit smallest-policy build and intentionally gives up bounded pauses.

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

Global and table root-slot constructors accept only nullable stored refs: `null`, `i31`, or a live object ref owned by the same collector. Checked constructors (`NewCheckedGlobalSlot` and `NewCheckedTableSlot`) are the safe API for production decode/instantiation: they return errors and do not append a slot when they see a forged, stale, or cross-collector ref. The exported convenience constructors (`NewGlobalSlot` and `NewTableSlot`) are trusted/test setup wrappers that delegate to the same validation and panic on invalid initial refs, so there is no root-slot creation path that silently installs invalid metadata. A slot created with a nursery ref is dirtied immediately. Checked setters validate later stores and retain a dirty bit conservatively until collection instead of paying a map lookup or vector removal when the slot is overwritten with an old, null, or `i31` value.

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

Generated code must call object barriers for `struct.set` and ref array stores, slot barriers for `global.set` and `table.set`, and bulk barriers for array initialization/copy/fill paths that write refs. Fresh old/large objects are not exempt: constructors reconcile the completed payload once before publication so direct nursery children receive complete card coverage. `SlotFrame` barriers remain unsupported because exact frame roots are supplied directly at safepoints rather than retained as persistent slot cards. `BulkWriteBarrier` remains a post-write contract: generated helpers publish the complete destination range before invoking `BulkWriteBarrier`/`PostBulkWriteBarrier`. The Throughput barrier does not rescan the destination values; it conservatively dirties the exact byte range, and collection determines which cards are useful.

Remembered/card metadata is deliberately conservative and has bounded membership cost. Each handle stores a remembered bit, so repeated old-object dirties are O(1) without a membership map. The dense remembered vector is now the dirty-object index for minor collection; members normally own fixed-card ranges. Any object-card or persistent-root metadata failure arms one shared cold fallback that scans every remembered object and persistent root until evacuation clears the young generation. This also preserves an edge whose first card addition failed when a later write successfully creates a different exact card. Sweeping clears ownership before handle reuse. Ordinary old-object stores never rescan the complete parent merely because the new value is old or null.

Throughput cards are authoritative minor-GC inputs. The default is a measured 128-byte payload card. One 16-byte `objectCard` stores a handle, an inclusive payload-byte range aligned to card boundaries, and a one-based link to another disjoint range for the same object. Dense adjacent writes coalesce into one range; two distant writes remain separate and do not widen across the clean middle of a large array. A helper access to a non-head range swaps only its interval into the stable head slot, preserving links and native backing while allowing subsequent Go and generated stores to use the constant-time head-card check. Removed or coalesced ranges become an intrusive free list in tombstoned card slots and are reused before the arena grows, so continuously surviving young workloads retain only the peak card-slot high-water mark rather than accumulating stranded metadata. Minor collection walks exact transient roots, dirty persistent slots, and these linked card ranges, then traces the nursery graph. Struct scanning checks exact field offsets; array scanning converts each range to exact element bounds. A complete dirty range uses the same one-pass descriptor walk as dense tracing rather than dispatching once per card.

Persistent global/table slots use stable-index bitmaps plus one compact dirty-slot vector, replacing the lazy Go map. Minor collection visits only vector members; full and Tiny collection continue to enumerate every persistent slot. Bits are cleared by walking the dirty vector rather than zeroing a root-sized table. A metadata-growth injection arms the shared cold fallback for both persistent roots and remembered objects. Verification independently reconstructs every old-to-nursery field/element and nursery persistent root, proving that its exact byte/slot is carded or that the explicit fallback is active.

Successful minor evacuation clears all remembered/card metadata only when no young object remains. While survivors remain, collection retains ranges and persistent slots that still contain young edges and prunes obsolete membership by scanning recorded cards rather than complete old objects. A full Throughput collection does not move surviving young objects and preserves their useful metadata. Object freeing unlinks all owned ranges before handle reuse. The Native collector ABI is version 6: `objectCard` remains 16 bytes, `handleEntry` remains 20 bytes, the 168-byte view retains the Eden limit and adds the nursery-object maximum, `Config` is 72 bytes, and the ordinary linux/amd64 `Collector` is 1,120 bytes.

On the August 8, 2026 Ryzen 7 8845HS host, interleaved release benchmarks remain allocation-free. Nursery-parent struct stores change from 25.57 to 25.14 ns/op, old-parent/old-child stores from 27.61 to 27.81 ns/op, and newly exact old-struct/young-child same-card stores from 28.18 to 29.10 ns/op. Existing old-array same-card stores improve from 36.82 to 35.51 ns/op, and repeated dirty persistent-root stores improve from 9.94 to 7.03 ns/op. Generated old-array fixture code shrinks from 1,864 to 1,856 bytes; barrier-attributed bytes remain 404. All cases remain 0 B/op and 0 allocs/op. An August 9 follow-up benchmark alternated fourteen 750 ms samples of repeated writes to a non-head card: interval promotion reduces the median from 77.99 to 64.83 ns/op (**-16.88%**, bootstrap 95% interval **-20.75% to -12.86%**) with zero allocations. Relative to `81e36324`, the stripped standard runtime is unchanged, the stripped minimal runtime adds one 4-KiB file-alignment page, and the TinyGo minimal runtime adds 320 bytes; `Collector` remains 1,120 bytes.

## Reference storage classes

Collector references (`StorageRef`/`StorageRefNull`) are compact traced handles and are the only storage kinds that participate in object scanning, rooting, remembered sets, or write barriers. Function and external references use separate non-null/nullable opaque 64-bit token classes; they receive semantic nullability and copy validation but are never interpreted as collector handles. Numeric and packed storage is a fourth, non-reference class.

Array copy accepts exact storage kinds plus the three non-null-to-nullable widenings: collector ref, function ref, and external ref. Nullable-to-non-null and cross-class copies reject before mutation. Once descriptors, ranges, and storage compatibility pass, production Throughput copy trusts the valid source array invariant and performs direct memmove-compatible payload copy; per-element collector-ref integrity checks remain enabled under `VerifyAfterCollect` or `StressBarriers`. `array.init_data` accepts only numeric/packed storage and rejects every collector or opaque reference destination before reading source bytes or changing payload state.

On linux/amd64 (Go 1.24.4, Ryzen 7 8845HS), removing redundant production validation reduces 4,096-element compact-ref copy from 37.1–38.2 µs to 213–218 ns, nullable widening from 37.5–38.2 µs to 200–201 ns, and same-array overlap from 36.6–40.4 µs to 157–159 ns. The 256-element forms fall from roughly 2.3–2.5 µs to 45–52 ns. All remain 0 B/op and 0 allocs/op.

Array constructors preflight every value and root before allocation, then initialize the compact payload in bulk. Uniform constructors use one doubling fill; fixed constructors use unchecked width-specialized stores after complete validation. Collector-reference arrays perform one post-construction barrier reconciliation rather than one barrier/card operation per element. Tiny scans the final range once to maintain its incremental mark invariant; Throughput records at most one card interval and one remembered membership. Internal value-root sets are traversed directly during mark and verification, avoiding one escaping `RootSlot` interface value per fixed-array element. Allocation-free direct root enumerators return whether the sink accepted the complete walk, allowing composite groups and constructor scratch sets to propagate early termination without allocating an adapter. Classified direct enumerators use the same completion contract, so constant-expression temporary stacks do not continue after an attributed frame or element-root sink stops.

On AMD64, ABI version 1 exposes the retained 32-handle native struct batch as one
generic struct/array allocation ticket. A helper reserves a contiguous handle run
when available. Arrays additionally reserve one bounded 4-KiB Eden chunk and
advance its private cursor; structs retain the measured direct collector-bump path
so chunk support adds no checks to established struct code. Statically sized final
arrays up to 256 object bytes admit native `array.new`,
`array.new_fixed`, and defaultable `array.new_default` for numeric, packed,
vector, and nullable abstract `any`/`eq` reference elements. All reference
initializers are checked before any payload or handle publication. The final
write publishes the handle space byte, then advances semantic allocation count.

The 256-byte admission ceiling is measured policy, not a semantic limit.
Dynamic lengths and larger candidates initially regressed because a 4-KiB chunk
exhausted before the fixed handle batch, forcing repeated exact cancellation of
unused identities. Those shapes, large-object classifications,
non-final/defined-heap references, `array.new_data`, and `array.new_elem` stay on
the rooted helper path. Every Go allocation cancels the ticket first; collection,
traps, malformed metadata, epoch changes, and `Close` recycle unused handles and
rewind only an exclusively owned top chunk. Generic public calls keep their first
eight constructors helper-only and refill after the ninth because the next public
boundary collects and cancels the batch; products without that mandatory boundary
refill immediately. Post-invocation GC-global reconciliation covers exact mapped
products because successful native batches may cross several constructors without
a helper synchronization point, while modules without collector-reference globals
return before touching reconciliation state.

On the same machine, 4,096-element uniform i32 construction improves from 25.6–25.7 µs to 96 ns and uniform compact-ref construction from 27.4–27.5 µs to 263 ns. Fixed compact-ref construction improves from 96.5–97.4 µs, 43,851–43,858 B/op, and 1,371 allocs/op to 25.4–25.9 µs, 172 B/op, and 6 allocs/op. The remaining allocations are collection/constructor control metadata, not per-element growth.

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
- `TinyPacingStepLimit`
- `TinyCollectEveryAlloc`
- `TinyStepEveryAlloc`
- `ThroughputHeapBytes`
- `ThroughputPageBytes`
- `ThroughputClassLimit`

Tests exercise tiny nurseries, collect-every-alloc, exact scanning, cycles, roots, minor/full collection, and barrier metadata. Environment variables can be layered on later if needed; the first pass keeps the knobs explicit and testable.

The GC package's randomized graph stress tests also run an independent shadow
tracer before full collection. The oracle walks root slots, globals, tables, and
descriptor layouts without calling the production mark or object-scan helpers,
then checks both missing retention and unnecessary retention. Promotion-failure
tests snapshot nursery, handle, card, mark, and old-space allocator state and
require byte-for-byte-equivalent observable state after rollback.

### Hardened GC stress build

Build tag `wagodebug` enables deterministic failure injection and a reproducible
mixed minor/full choice at `CollectEveryAlloc` safepoints. Release builds compile
the hooks to constant no-ops and retain no injector state. The hardening suite is
run with:

```sh
go test -tags wagodebug ./src/core/runtime/gc/...
go test -race -tags wagodebug ./src/core/runtime/gc/...
```

The injection matrix covers promotion planning, destination allocation, commit
preflight, handle publication, object-card growth, slot-card growth, and backing
growth. Promotion failures at every survivor index must restore handles, object
bytes, nursery allocation state, marks, remembered/card metadata, and the
throughput allocator. Armed failures are scoped to one collector (or its
throughput heap), so concurrent collectors cannot consume each other's test
faults. Native handle-batch tests bracket both collection and
`Close` and require reservation cancellation plus epoch advancement.

Randomized Throughput and Tiny operation fuzzers feed every full collection
through the independent shadow tracer. Descriptor fuzzing covers every storage
kind, packed fields, alignment, invalid layouts, and bounded size arithmetic;
operation fuzzing covers sparse/dense reference graphs, giant rejected arrays,
globals, tables, old-to-young writes, promotion exhaustion, incremental Tiny
phases, and pointer-free ref-looking payloads. Product tests separately retain
the public-token, cross-instance, host re-entry, exception, snapshot, subtype,
bulk-operation, trap-order, and collection-disabled fail-closed gates.

The scheduled `Regression stress` workflow supplies the independently runnable
CI shard. Its `gc-hardening` matrix runs three repetitions on native Linux
amd64 and arm64 in explicit-bounds and `wago_guardpage` builds, including the
collector suite and real native frame-root/host-transition products. This is the
architecture execution gate; cross-compilation alone is not treated as arm64
coverage.

Focused amd64 comparison against `main` on a Ryzen 7 7800X3D found no new
allocations: `ForcePromote` measured 113.4 ns/op at baseline and 114.5 ns/op with
transactional failure restoration (+1.0%, 0 B/op), while the retained promotion
plan remained 24 bytes per survivor. Nursery constructor and remembered-array
write means stayed within 2.2% of baseline. `BenchmarkForcePromoteTransactional`
and the `plan-B` metric on `BenchmarkMinorPromotionScratch` retain these gates
for future comparisons.

## Current limitations

- The mandatory Core 3 WasmGC corpus is complete on linux/amd64 explicit and
  signal-backed bounds. Recursive/subtyped graphs, structs, arrays, i31, extern
  conversion, equality, casts/tests/branches, bulk/data/element initialization,
  linking, non-flat exports, and invalid/unlinkable cases execute or reject
  exactly. Linux/Darwin arm64 explicit bounds is also complete under the platform
  qualification described in `docs/wasm3.md`.
- Exact collection is admitted only where every local, spill, callsite, suspended
  host activation, EH payload, persistent global/table slot, and foreign frame has
  proven ownership and mutable root storage. ARM64 polymorphic local indirect calls
  and same-domain foreign `call_ref` now satisfy that proof for non-tail calls.
  Descriptor-resolved local, host-wrapper, and retained same-Runtime foreign
  `return_call_ref` targets use bounded discarded-frame transfers, including mutable
  and imported funcref-table loads. GC-bearing tails require exact collector-domain
  identity; host and foreign-domain GC transfer remains fail-closed instead of
  scanning an approximate root set.
- Imported/exported mutable and immutable GC globals participate in exact
  canonical Runtime collector domains. Module-local flattened type indexes translate
  through immutable per-instance maps and are never cross-module identity. Actual alias cells are domain
  roots and checked slots preserve barrier/card state. Multiple heterogeneous
  imported/exported GC tables participate with direct indexed alias roots,
  growth/close coverage, and attachment rollback.
- Generic struct/array results may be retained as opaque `GCRef` tokens through a
  64-slot inline fast path plus reusable dynamic overflow storage. Each token has
  an independent checked root and retains exact Runtime/store
  and producer ownership after producer close, rejects stale/cross-producer release,
  reuses released slots without reusing token identity, and must be released explicitly.
  Multi-result translation rolls back every token issued by the failing result boundary,
  so a capacity or validation error cannot strand an unreachable retained object.
  Non-null tokens re-enter only the exact collector domain after structural subtype
  validation. Up to 64 reusable checked argument slots keep staged
  values rooted until return, including concurrent release after staging; shared-domain
  collector mutation is serialized independently of parked host transitions. Untyped
  `uint64` values are never accepted as compact collector handles.
- Minor collection copies first survivors through stable handles and promotes at
  a bounded adaptive age. The Throughput old generation reuses freed spans but
  does not yet implement full Immix line/block marking or selective evacuation.
- The Throughput heap uses growable Go byte slices, so generated code must not
  cache raw payload pointers. Direct checked JIT object access will require a
  measured stable access contract with helper slow paths retained.

These boundaries keep the runtime exact, bounded, typed, and no-cgo while the
remaining ownership and direct-access work is completed.

## Iteration 72 M8 duplicate recursive function linking

The source-lines-652–659 provider/consumer pair is pinned at 100 bytes (`cee53b6e420932faec8bf166a6ae79cfab88f7ca890cd39851d3f70b932471aa`) and 92 bytes (`30ff7e3befab7405c63554526ecf2e61fc9ecf2b65e175414db07aa774e6a540`). Each has two complete two-member recursive function groups. Provider `f11` and `f12` satisfy exact and shifted duplicate consumer views in the ordered four-import sequence. Provider/consumer descriptor arenas are 96/160 bytes; all four imports retain one producer transactionally; both close orders release exactly; hosts and other products reject; `Instance.gc` stays nil. Wasm/code/codec sizes are 100/253/531 and 92/0/315 bytes. Codec reload, snapshots, guard mode, public admission, arm64, and unsupported platforms remain fail-closed. Accounting is 170 commands / 40 passed modules / 23 passed assertions / 5 gates / 11 blocked commands / 24 invalid / 6 executed plus 2 blocked unlinkables / zero validator gaps, unexpected failures, or hidden failures. Source line 668 is next.

## Iteration 73 complete type-subtyping accounting

M9, M10, M11, and the non-flat export finish the official type-subtyping file. M9 provider/consumer wasm/code/codec sizes are 136/253/725 and 164/0/589 bytes with 96/288-byte descriptor arenas and one deduplicated producer owner. M10 sizes are 74/77/304 and 43/0/148; M11 sizes are 87/77/384 and 56/0/228; both consumers reject incompatible imports before retention. The 183-byte non-flat module has 753 code bytes and a 1,173-byte codec and executes all six f32-zero exports allocation-free with `Instance.gc` nil. Accounting is 170/45/29/24/8 with zero gates, blocked commands, validator gaps, unexpected failures, or hidden failures.

## Iteration 74 Core 3 GC integration

Explicit `CoreFeaturesV3` admission now integrates the complete GC surface with
typed references, exceptions, multi-memory, memory64, and table64. Exact staged
classifiers remain as audit and footprint proofs, but unrelated valid Core 3
products are no longer rejected merely for missing a pinned identity. Generic
`array.new_data` and `array.new_elem` helpers validate segment liveness, source
ranges, destination element type, and allocation bounds before publishing a
collector reference. Reference argument/result conversion uses store-owned
extern/any tokens and releases transient ownership after matching. The pinned
full suite passes 2,226 modules and 58,038 assertions with zero gaps.

## Checked Go references

The parent `src/core/runtime/gc` package now supplies collector-bound, generation-
checked references for Go callers. Its opaque Ref is separate from the compact
native ABI described above. See [GC reference boundaries](gc-reference-boundaries.md)
for ownership, rooting, and migration rules.
