# Dew Map/Set WasmGC performance investigation

Date: 2026-08-02

## Workload

The Dew repository's `tools/wago-map-bench` generator emits one import-free
WasmGC function containing deterministic Map and Set insertion and successful
lookup operations. The investigated workload contains 512 keys and compiles to
712,494 Wasm bytes.

On the Ryzen 7 8845HS host, the original local Wago runtime took about 10.73 ms
per complete workload while Node 26.3.0 took about 0.13-0.14 ms. Empty exported
function invocation was only about 87 ns in Wago, so public invocation overhead
was not the source of the gap.

`WAGO_EXPLAIN=1` showed the actual shape:

```text
Wasm bytes:             712,494
native code bytes:   14,603,409
hostsync sites:          72,603
ref.cast sites:          40,899
array.len sites:          8,180
struct.new sites:         2,050
array.new_default sites:  1,024
```

Node lowers and optimizes these GC operations inside native code. Wago currently
routes most casts, reference field/array operations, lengths, and allocations
through the synchronous parked-Go GC helper ABI. The workload therefore stresses
helper transitions rather than the public Invoke API.

## Profiles and fixes

A five-second CPU profile attributed 35.9% of the original runtime to
`gcHelperRoots`. Every allocating helper linearly scanned all compiled
safepoints to find a dense numeric ID. With thousands of allocation sites in one
generated function, root lookup was quadratic in static allocation-site count.
Compiler-produced safepoint IDs are dense, so lookup now uses direct `id - 1`
indexing with a binary-search fallback for valid sparse codec metadata.

Additional changes remove avoidable generic-host and descriptor work:

1. dense safepoint lookup is O(1), with repeated zero-allocation microbenchmarks around 1.3-2.1 ns/op;
2. final defined `ref.cast` targets compare canonical runtime type IDs directly instead of walking subtype descriptors;
3. internal GC helpers keep the native execution lease instead of running the arbitrary-host release/reacquire protocol;
4. non-allocating helpers no longer create and lock `gcPublicState` constructor/root scratch;
5. final struct/array helper checks use canonical type equality instead of subtype traversal; and
6. `struct.new` builds initializer root sets only when an initializer contains a live object reference, not merely because the layout contains nullable reference fields.

Sequential same-process measurements during the investigation were:

| Stage | Wago median | Incremental change |
| --- | ---: | ---: |
| Original | 10.728 ms | - |
| O(1) safepoint lookup | 7.420 ms | -30.8% |
| Final-cast equality fast path | 6.839 ms | -7.8% |
| Internal-helper lease retention | 6.025 ms | -11.9% |
| Allocate-only public-state locking | 5.766 ms | -4.3% |
| Final object-type equality | 5.368 ms | -6.9% |
| Skip roots for null-only initializers | 5.152 ms | -4.0% |

A longer nine-round rerun measured 5.694 ms for Wago and 0.142 ms for Node.
Host allocation fell from about 196,999 B to 98,563 B per workload. Timing varies
with GC and scheduler state, but the fixes consistently remove roughly half of
the original execution time and half of the measured host allocation.

A same-session plugin-complete stripped TinyGo release A/B measured
**1,736,036→1,737,268 bytes** (**+1,232 bytes, +0.071%**), keeping the runtime and
allocation improvement well below one tenth of one percent of release footprint.

The load-factor comparison was rerun after these changes. The maximum load 1.0
policy remained fastest: 5.283 ms, compared with 5.715 ms at 0.5, 6.587 ms at
0.75, and 6.569 ms at 0.875.

## Combined typed struct and array access

The first item from the combined-operation track is now implemented for
`struct.get`, `struct.set`, `array.get`, and `array.set`. Helper dispatch
previously resolved the compact handle with `ObjectType`, traversed the subtype
descriptors, and then resolved the same handle again in `StructGet`/`StructSet`
or `ArrayGet`/`ArraySet`. The collector now combines the dynamic type check,
container checks, bounds checks, and access from one resolved descriptor. Final
required types use canonical type-ID equality; open types retain complete
supertype traversal, and reference writes retain validation and barriers.

Pinned single-CPU medians on the same Ryzen host are:

| Operation | Separate checks | Combined typed access | Change |
| --- | ---: | ---: | ---: |
| collector struct get | 50.18 ns | 34.21 ns | -31.8% |
| collector struct set | 60.10 ns | 36.75 ns | -38.9% |
| collector array get | 56.48 ns | 41.51 ns | -26.5% |
| collector array set | 65.09 ns | 42.33 ns | -35.0% |
| native subtype struct set/get workload | 517.7 ns | 472.2 ns | -8.8% |
| native reference-array get workload | 525.9 ns | 508.5 ns | -3.3% |
| native v128-array set workload | 509.1 ns | 493.9 ns | -3.0% |

All measurements remain at 0 B/op and 0 allocs/op. Same-session
plugin-complete stripped TinyGo A/Bs measured **+1,584 bytes** for typed struct
access and **+2,432 bytes** further for typed array access, for a combined
**1,737,268→1,741,284 bytes** (**+4,016 bytes, +0.231%**). Internal GC helper
lowering also skips redundant module-global cell synchronization because these
helpers cannot observe or mutate numeric global cells and the module-pinned
registers survive the parked transition. A pinned-state helper workload improves
**364.2→353.6 ns/op** (**-2.9%**) with no additional release bytes. The
native-to-Go helper transition still dominates the end-to-end result, so shared
native access stubs and cast-plus-access fusion remain higher-leverage follow-ups.

### Direct `array.len` assessment

The existing native collector ABI can validate a compact handle, locate its
current object, and compare an object header with a statically known canonical
type. Plain `array.len`, however, has no type immediate and may receive any
array subtype. The ABI does not currently publish canonical type-kind metadata,
so native code cannot prove that the resolved object is an array before loading
`ObjHeader.Aux`; omitting that proof would incorrectly interpret a struct header
as an array length. No such unsafe fast path is retained.

The bounded options are to publish a versioned dense canonical type-kind table,
carry proven concrete array provenance into lowering, or route `array.len`
through a shared native stub that has equivalent metadata. The shared metadata/
stub route is preferable because it also supports reference field/array access
without duplicating a full handle check at thousands of static sites.

## Rejected experiment

Inlining a complete checked native final-type `ref.cast` reduced the workload to
about 4.77 ms, but expanded generated native code from 14.6 MB to 22.6 MB because
the complete handle, ABI, heap, and canonical-type check was duplicated at all
40,899 cast sites. That trade is not retained: it violates Wago's footprint goal
and increases instruction-cache pressure.

## Remaining gap

The remaining approximately 40x Node gap is primarily architectural:

- all 72,603 static GC helper sites remain synchronous native-to-Go transitions;
- `ref.cast` alone accounts for 40,899 transitions;
- reference `struct.get`/`struct.set` and `array.get`/`array.set` contribute roughly
  19,400 more transitions;
- `array.len` contributes 8,180 transitions;
- several helper paths resolve the same compact handle and descriptor more than
  once; and
- the 14.6 MB generated function has substantial instruction-cache cost.

The next measured options should be, in order:

1. shared native GC check/access stubs so checked casts and reference reads do not
   duplicate large inline sequences;
2. combined typed collector operations that resolve a handle once for final
   struct/array access;
3. reusable caller-owned root-pair scratch for non-null constructor operands;
4. direct native `array.len` and reference loads with the existing stable native
   collector ABI; and
5. producer-side typed scratch locals to reduce redundant casts in generated Dew
   code, while retaining Wago correctness for arbitrary producers.

Any native fast path must retain stale/forged-reference rejection, final/non-final
subtyping semantics, moving-collector root publication, Tiny/Throughput parity,
and codec-loaded metadata validation.

## Comprehensive Wago WasmGC optimization inventory

The following inventory is specifically for **Wago's runtime and native
compiler**. It assumes the input Wasm is unchanged. Producer optimizations such
as Dewdrop escape analysis, aggregate scalar replacement, ABI flattening,
payload-load CSE, and emitting a Wasm `loop` instead of recursion belong in the
producer compiler and are intentionally excluded here.

The priorities below favor correctness and low footprint first, then reduction
of native-to-Go transitions, then more ambitious optimizing-compiler work.
Every item is a measurement candidate until implemented and benchmarked.

### 1. Publish precise native frame roots at every GC safepoint

General generated WasmGC currently uses a bounded, collection-disabled
throughput heap because native frame roots are not available at every helper
safepoint. Generate compact stack maps describing:

- live reference registers;
- live reference stack slots and Wasm locals;
- outgoing reference arguments;
- constructor initializer roots; and
- any temporarily decoded collector handles that remain valid at the safepoint.

The collector slow path must locate the active native frame and map without a
linear search. Stack-map records should be compact, immutable after compilation,
and directly indexed by dense compiler-produced safepoint IDs. Correct root
publication is the prerequisite for collecting during arbitrary generated code,
for a nursery, and for allocation fast paths that fall back to collection.

### 2. Add a direct native bump-allocation fast path

Compile fixed-size final `struct.new` sites to a short native sequence:

1. load the allocation pointer and limit;
2. check that the fixed object size fits;
3. advance the allocation pointer;
4. initialize the object header and fields; and
5. return the compact reference without entering Go.

Branch to one shared out-of-line slow path for exhaustion, collection, oversized
objects, or initialization cases requiring more general handling. Static sites
already know the final type, size, alignment, reference bitmap, and field
layout; those should be encoded as immediates or constant-pool entries instead
of looked up dynamically.

Array allocation should receive separate fixed-length and dynamic-length paths.
The dynamic path must retain overflow, implementation-limit, and bounds checks.

### 3. Add shared native GC check and access stubs

Use compact out-of-line native stubs for common checked operations instead of a
native-to-Go transition or a complete inline helper expansion at every site:

- final and non-final `ref.cast`/`ref.test`;
- typed `struct.get` and `struct.set`;
- typed `array.get` and `array.set`;
- `array.len`; and
- compact-reference validation and resolution.

The rejected fully inline final-cast experiment demonstrated runtime value but
expanded native code from 14.6 MB to 22.6 MB. Shared stubs should retain most of
the transition reduction while preserving Wago's low-footprint goal and
instruction-cache behavior.

Stubs must use the internal native ABI, preserve the execution lease, produce
normal Wasm traps, and keep slow/error paths auditable.

### 4. Fuse cast and typed access operations

Recognize native-compiler sequences such as:

```wat
local.get $value
ref.cast (ref $Entry)
struct.get $Entry 3
```

and lower them to one checked typed operation that:

1. validates the compact reference;
2. resolves the object address once;
3. checks the final type or required subtype once;
4. performs the field or array access; and
5. returns the value without publishing an intermediate reference result.

Apply the same fusion to cast-plus-`struct.set`, cast-plus-`array.len`,
cast-plus-`array.get`, and cast-plus-`array.set`. Final types should use canonical
type-ID equality; non-final types must retain complete subtype semantics.

### 5. Reuse decoded handles across adjacent non-allocating operations

Several helper paths resolve the same compact handle and type descriptor more
than once. Within a proven non-allocating, non-safepoint region, retain a decoded
object address and descriptor across adjacent accesses.

The cache must be invalidated across allocation, collection, arbitrary calls,
host transitions, or any operation that may move the object. If the collector
can move objects at a boundary, raw addresses may not survive that boundary
unless they are pinned or updated through a published root.

### 6. Lower `array.len` directly in native code

`array.len` should normally require only reference validation, optional final
array-type verification, and one length load. Emit that sequence directly or
through a very small shared native stub. It should not acquire allocating helper
state or transition to Go.

The Map/Set workload contains 8,180 static `array.len` sites, making this a
focused, low-complexity candidate. Preserve null, forged/stale-reference, and
wrong-heap-type trap behavior.

### 7. Lower reference field and array accesses directly

Provide native fast paths for reference-valued `struct.get`, `struct.set`,
`array.get`, and `array.set`.

Reads must validate the container, perform required type and bounds checks, and
preserve null and immediate-reference representations. Writes must additionally
execute the collector's write barrier. Scalar and reference paths should share
object validation where practical without forcing scalar accesses through a
reference-heavy ABI.

### 8. Reuse caller-owned constructor root scratch

For constructors initialized with non-null collector references, publish the
initializer values in fixed native frame slots described by the allocation
safepoint map. Let the allocation slow path scan those slots directly instead of
allocating or composing per-constructor root containers.

Retain the existing null-only fast path: null references and immediate `i31`
values do not require collector-object roots. Benchmark one-reference,
two-reference, wide-reference, and mixed scalar/reference constructors.

### 9. Specialize allocation sites by concrete final type

Attach immutable allocation metadata to each static final-type site:

- canonical type ID and descriptor;
- object size and alignment;
- reference-field bitmap;
- initialization layout;
- collector size class; and
- slow-path entry.

This removes repeated descriptor lookup and shape classification from hot
allocation paths. Equivalent layouts may share slow paths and metadata records
when doing so does not weaken nominal type checks.

### 10. Reduce generated native-code size

The measured 712 KB Wasm module generated approximately 14.6 MB of native code.
Reduce repeated code without reintroducing Go transitions through:

- shared GC checks, traps, and allocation slow paths;
- compact helper-call sequences;
- per-function or per-module constant pools;
- common cast/access continuations;
- deduplicated type-check sequences;
- smaller safepoint records and references; and
- avoiding repeated internal-ABI setup for adjacent GC operations.

Track native bytes, instruction-cache events where available, compile time, and
runtime together. A speedup that reproduces the 22.6 MB inline-cast expansion is
not automatically acceptable.

### 11. Coalesce non-allocating helper boundaries

Where direct native lowering is not yet implemented, allow a short sequence of
non-allocating GC operations to execute through one internal helper boundary.
Do not repeatedly acquire state, resolve the same object, and leave helper mode
for each cast, length, and field read.

This is an intermediate strategy; direct native operations or shared native
stubs remain the preferred end state. Coalescing must not move traps across
observable operations or hide an allocation/safepoint boundary.

### 12. Add a nursery or generation-oriented allocation path

After precise native roots and write barriers are complete, evaluate a nursery
for short-lived WasmGC objects:

- bump-allocate young objects;
- collect the nursery independently when possible;
- promote survivors under a measured policy; and
- track old-to-young references with a bounded remembered set or card table.

Compare this with the existing Tiny and Throughput collectors. The design must
retain predictable memory use and must not require unbounded metadata, hidden
host allocations, or goroutine-heavy background work.

### 13. Add an optional optimizing native tier

Longer term, Wago's Wasm-to-native compiler could optimize unchanged producer
Wasm with:

- redundant `ref.cast` and `ref.test` elimination from validated type facts;
- immutable field-load common-subexpression elimination;
- local aggregate escape analysis and scalar replacement;
- dead allocation elimination when identity and traps are unobservable;
- tail-call or tail-recursion optimization where Wasm semantics permit it; and
- loop-invariant type, handle, and bounds-check hoisting.

These are Wago optimizations only when Wago performs them after decoding and
validation. Similar transformations in Dewdrop remain producer optimizations.
An optimizing tier is lower priority than removing helper transitions because it
adds compiler complexity and code-size risk.

## Optimization safety requirements

Every fast path must preserve:

- stale and forged compact-reference rejection;
- nullability and null traps;
- final and non-final subtype semantics;
- struct mutability and array bounds rules;
- reference write barriers;
- moving-collector root publication;
- Tiny and Throughput collector parity unless a mode explicitly documents a
  narrower feature set;
- codec-loaded module and type metadata validation;
- ordinary Wasm trap ordering and visibility;
- host-call re-entry and execution-lease rules; and
- bounded metadata and predictable memory use.

A fast path must fall back explicitly when one of its static assumptions is not
proved. Unsupported or unsafe cases must not silently use weaker checks.

## Benchmark matrix

The large Dew Map/Set workload remains valuable, but smaller import-free WasmGC
modules should isolate each cost center.

### Dead allocation

Run a loop containing `struct.new` followed by `drop`. This measures allocator,
safepoint, and collection overhead with a minimal live set.

### Retained linked chain

Allocate a recursive node whose reference field points to the previous head and
retain the newest head in a local. This measures allocation with an increasing
live set and verifies native-frame root publication during movement and
collection.

### Final struct access

Repeatedly execute scalar and reference `struct.get`/`struct.set` against one
final object. Separate access cost from allocation cost.

### Cast plus access

Repeatedly execute final and non-final `ref.cast` followed by field or array
access. Compare independent helpers, shared native stubs, and fused operations.

### Array length and access

Measure `array.len`, scalar/reference `array.get`, and scalar/reference
`array.set` independently, including bounds traps and reference write barriers.

### Reference-bearing construction

Measure constructors with zero, one, two, and many live reference initializers.
This isolates caller-owned root scratch and null-only fast paths.

### Static-site count versus dynamic repetition

Compare:

- one allocation/cast/access site executed many times; and
- thousands of static sites each executed a small number of times.

This separates dynamic helper cost from code-size, safepoint-indexing, metadata,
and instruction-cache costs.

### Collector matrix

For every allocation benchmark, report Tiny and Throughput results, and report
collection-enabled results once precise roots permit them. Track at least:

- median and tail execution time;
- generated native-code bytes;
- compile and instantiate time;
- Go host allocation;
- WasmGC heap bytes allocated;
- collection count and pause time;
- peak live and committed heap;
- helper or native-stub transition counts; and
- result checksum or expected trap.

## Recommended execution order

1. Add isolated allocation, access, cast, and root-publication benchmarks.
2. Complete precise native frame-root maps and collection-capable safepoints.
3. Implement direct native `array.len` as the smallest access fast path.
4. Add shared final-cast and typed-access native stubs.
5. Fuse cast-plus-access and resolve compact handles once.
6. Add direct reference loads/stores with correct write barriers.
7. Add caller-owned constructor root scratch.
8. Add fixed-size final-struct bump allocation with one shared slow path.
9. Reduce repeated native sequences and remeasure code size.
10. Evaluate a nursery only after roots, barriers, and allocation slow paths are
    complete and tested.
11. Consider an optimizing native tier only after the architectural
    native-to-Go transition gap is substantially reduced.
