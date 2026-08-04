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

### Reproducible workspace artifact

The August 2 workspace artifact is `dew-map-workload.wasm`, 712,494 bytes with
SHA-256 `552690e6159ffeaa8da5d265bc29af4407fb4663622ebc83dfeb87233add6984`.
Its source is `dew-map-workload.dew`, SHA-256
`5d4765f989fc8c1a1c02cedf6f999e29db20a85e2a7eaadcc29d8cf4735e0575`.
It exports import-free `main() -> ()` and reproduces the helper-heavy profile.

Pinned execution-only measurements compile and instantiate outside the timed
region. Wago uses a fresh 16 MiB Throughput collector instance per sample so old
objects and collection phase do not bias one invocation. Node 26.3.0 is reported
both after 100 same-instance warmups and with a fresh instance after module-code
warmup:

| Runtime mode | Median | Mean | Notes |
| --- | ---: | ---: | --- |
| Node hot same instance | 118.3 µs | 336.8 µs | 200 calls; p95 451.4 µs from GC |
| Node fresh instance | 492.3 µs | 685.0 µs | first call per instance |
| Wago at `a21b7007` | 4.466 ms | 4.464 ms | interleaved A/B; fresh collector |
| Wago at `802af7fc` | 3.679 ms | 3.701 ms | typed fusion and constructor scratch |
| Wago after shared AMD64 native paths | 1.877 ms | 1.895 ms | interleaved A/B; fresh collector |
| Wago after final-reference resolver and nursery stores | about 0.95 ms | about 0.98 ms | best controlled fresh runs |
| Wago after recursive dead constructor elimination | about 0.64 ms | about 0.67 ms | four interleaved A/B runs; fresh collector |
| Wago after structured exact-reference facts | about 0.57 ms | about 0.58 ms | four interleaved A/B runs; fresh collector |

The latest controlled Wago result is therefore about 4.8x the hot Node median and 1.2x
the fresh-instance Node median. The distinction matters: repeated Wago calls
also expose old-heap growth and major-collection policy, while Node's hot result
includes tiered optimized code.

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

### Fused final casts with typed reads

The compiler-side fusion now recognizes adjacent final defined-type
`ref.cast`/`ref.cast_null` followed by either:

- same-type `struct.get`, including scalar, reference, `v128`, and packed signed
  or unsigned fields; or
- `array.len` on the same concrete final array type.

AMD64 and ARM64 emit one non-allocating helper operation for each pair. The
struct helper resolves the compact handle once through `StructGetTyped`; the
array helper uses `ArrayLenTyped`. Both preserve exact canonical final-type
identity and sequence trap order: null through `ref.cast_null` reaches the access
and traps as null reference, while non-null cast of null, i31, or a mismatched
object traps as cast failure. Open types, differing struct access types, and
non-adjacent operations remain unchanged.

The workspace artifact contains **18,916** cast-plus-`struct.get` pairs and
**8,180** cast-plus-`array.len` pairs. Generalizing the struct fusion removes
8,692 duplicated direct scalar access sequences. It does not reduce helper-call
count for those scalar pairs, but reduces generated native code and hot local
reloads. Adding cast-plus-length fusion removes 8,180 parked transitions.

| Measurement | Before extension | After extension | Change |
| --- | ---: | ---: | ---: |
| generated native code | 13,499,220 B | 10,635,974 B | -21.2% |
| synchronous helper sites | 62,379 | 54,199 | -13.1% |
| artifact fresh-call median | 4.466 ms | 3.841 ms | -14.0% |
| isolated cast + `array.len` | 381.5 ns | 294.3 ns | -22.9% |
| collector typed length | 50.4 ns | 29.4 ns | -41.6% |

The isolated scalar cast/get benchmark remains effectively neutral because the
old sequence already used one cast helper followed by direct native scalar
access. Its artifact-level value is code footprint and instruction-cache
pressure: native code falls by about 2.21 MiB when all 18,916 struct pairs share
the fused helper form.

The initial reference-only fusion cost 3,152 plugin-complete TinyGo release
bytes. General struct and array-length fusion add 3,312 more bytes in the paired
build used here. The bounded fusion is still a bytecode peephole, not a stored
instruction IR.

### Reusable constructor roots

This artifact performs 2,050 `struct.new` operations with live object-reference
initializers. Composing frame roots and initializer roots through a temporary
interface object caused approximately one Go allocation per constructor. A
caller-owned `gc.InitializerRootScratch`, retained in the bounded
`gcPublicState`, now publishes the same exact mutable roots without per-site heap
traffic.

On a fresh artifact call, host allocation changes from **1,023,008 B and 2,142
allocations** to **924,512 B and 90 allocations**. Across 30 calls on one 256 MiB
collector, warmed medians improve by about **3.3%** while allocations fall from
about **2,062 to 10 per call**. The bounded cost is 72 bytes in `gcPublicState`
(3,768→3,840 bytes) and 3,696 stripped TinyGo release bytes. Exact moving-GC
rewrites remain covered by the existing atomic-constructor collection test.

### Shared AMD64 native final-operation stubs

The next pass keeps the complete handle, collector-ABI, heap-space, extent, and
canonical-type proof out of line once per generated function. Static sites pass
compact operands and immediates to shared native stubs while preserving only the
R9-R11 pins actually reserved by that function. These stubs do not allocate or
cross into Go, so they need no safepoint root publication.

AMD64 now uses shared native paths for:

- 8,180 final cast-plus-`array.len` pairs;
- 13,803 remaining final defined `ref.cast` operations; and
- 5,112 final reference-array reads.

The 8,692 scalar cast/`struct.get` pairs are deliberately split again: the cast
uses the shared native stub and the existing checked scalar access remains
inline. This grows the artifact relative to the all-fused helper form, but removes
8,692 additional Go transitions and remains smaller than the `802af7fc` artifact.
ARM64 retains the exact helper paths pending native execution measurements.

| Measurement | `802af7fc` | Shared native paths | Change |
| --- | ---: | ---: | ---: |
| generated native code | 10,635,974 B | 8,960,336 B | -15.8% |
| synchronous Go helper sites | 54,199 | 18,412 | -66.0% |
| shared native stub calls | 0 | 35,787 | new |
| fresh artifact median | 3.679 ms | 1.877 ms | -49.0% |
| isolated final cast | 319.3 ns | 225.4 ns | -29.4% |
| isolated reference-array get | 489.4 ns | 433.1 ns | -11.5% |
| scalar cast + `struct.get` | about 379 ns | about 288 ns | -24.0% |

The generated artifact is now 33.6% smaller than the original 13,499,220-byte
post-reference-fusion baseline. The complete plugin-enabled stripped TinyGo
binary grows **1,751,444→1,759,092 bytes** for the native paths, then 208 bytes
further for the retained old-space growth policy below.

A native fused final cast/reference-field-load stub was also measured. Its
isolated result was effectively neutral and the complete artifact regressed from
about 3.52 ms to 3.83 ms despite lower code size, so that path was reverted. The
optimized Go helper remains preferable for the 10,224 reference-field pairs.

### Specialized helper reads, constructors, and bounded reclamation

A further helper pass retains the Go transition for reference-field reads but
makes the transition materially cheaper:

- internal GC dispatch now bypasses the ordinary host closure's duplicate
  dispatch-bit branch and indirect call;
- `StructGetFinalRef` validates one canonical final descriptor by pointer,
  resolves one compact handle, and directly loads the four-byte reference;
- fully initialized struct constructors use unchecked field stores only after
  the existing complete shape, kind, ownership, nullability, and size preflight;
- fully initialized struct payloads no longer clear bytes that are immediately
  overwritten; layout padding is neither observable nor scanned; and
- allocation-triggered promotion exhaustion performs one cold full collection
  and retries the transactional minor promotion. Explicit `CollectMinor` keeps
  its existing fail-closed transactional behavior.

| Measurement | Before this pass | After | Change |
| --- | ---: | ---: | ---: |
| collector final reference-field read | about 44 ns | about 5.1 ns | -88% |
| fused cast/reference-get invocation | about 349 ns | about 313 ns | -10% |
| fresh Dew median, interleaved A/B | 2.068 ms | 1.772 ms | -14.3% |
| 16 MiB sustained run | exhausts before 100 calls | 500 calls complete | bounded recovery |
| 500-call sustained median | unavailable | 2.063 ms | new |
| 500-call host allocation | unavailable | 146 KiB/call | new |

The full-collection retry is entered only after the normal promotion path returns
the typed throughput-exhaustion sentinel. The successful allocation path retains
the original direct `CollectMinor` call and therefore showed no repeatable fresh
regression. If the live graph itself exceeds the configured old-space limit, the
retry still fails with the original bounded exhaustion error.

The complete plugin-enabled stripped TinyGo binary grows
**1,759,300→1,761,156 bytes** for this pass, **+1,856 bytes/+0.105%**.

### Native final-reference resolution and nursery-safe writes

A smaller native resolver now replaces the remaining 10,224 final
cast-plus-reference-`struct.get` transitions. It repeats the complete collector
ABI, compact-handle, heap-space, object-extent, and exact canonical final-type
proof, returns the resolved header pointer, and performs the immediate
constant-offset four-byte field load before any safepoint. A pinned-local
regression test covers the out-of-line resolver. The focused final cast/get
benchmark is approximately 230-237 ns/op at 0 B/op and 0 allocs/op.

Reference writes use a narrower optimization than the previously rejected
unconditional native barrier. Final `eqref`/`anyref` struct fields and arrays now
probe one shared checked stub and store directly only when the parent is in the
Throughput nursery. Nursery parents need neither old-to-young remembered-set
publication nor Tiny incremental marking. Old/large/Tiny parents, invalid
handles, and unsupported storage take the unchanged Go helper, retaining exact
validation and barriers. The stub validates the child compact reference before
mutation. Conditional lowering also restores pinned locals only on the cold Go
fallback; the hot native edge skips both helper transition and local spill/reload.
A dedicated Throughput/Tiny test caught and fixed both conditional-local-state
merging and four-byte array-reference store width.

Constructor helpers now validate descriptor kinds once, pass raw helper ABI words
directly into a prevalidated collector constructor, and expose those mutable raw
reference words through reusable exact root scratch if allocation collects. This
removes the intermediate `[]gc.Value` conversion/store walk. Nursery allocation
refreshes only native handle pointer/count/generation metadata; collection and
large-space growth still refresh the complete space view. A focused allocation
A/B measured about 55.6 ns versus 57.0 ns for full-view refresh.

| Measurement | Before this pass | Current | Change |
| --- | ---: | ---: | ---: |
| generated native code | 8,960,336 B | 7,479,004 B | -16.5% |
| final reference-read Go transitions | 10,224 | 0 | -100% |
| static synchronous fallback sites | 18,412 | 8,188 | -55.5% |
| fresh Dew median, stable pinned runs | about 1.33 ms | about 0.95 ms | about -29% |
| 500-call sustained median | about 1.60-1.66 ms | about 1.28 ms | about -21% |
| fresh host allocation | 924,512 B/op | 924,536 B/op | effectively unchanged |
| fresh host allocations | 90/op | 90/op | unchanged |

The remaining 8,188 static helper sites are 4,098 constructors plus 4,090 exact
write fallbacks. The write calls remain in generated code because old/Tiny
parents still require them, but nursery-heavy Dew execution usually takes the
native edge. The plugin-complete stripped TinyGo candidate is 1,774,188 bytes,
up 13,032 bytes (+0.740%) from the 1,761,156-byte pre-pass release, and executes
the artifact successfully.

### Recursive dead constructor trees

V8/Binaryen research identified 1,024 Dew suffixes where a fixed array initializes
an outer struct and the outer result is immediately dropped. AMD64 now removes
both allocations with bounded postfix lookahead rather than making them faster.
Direct constructor/drop pairs are removed at the constructor. Inner constructors
are replaced only when push-only intervening leaves cannot consume or expose the
reference and the following outer `struct.new` is itself immediately dropped.

Non-trapping valent trees disappear without code. If any initializer still carries
a deferred trap, the compiler flushes bottom-to-top before removing the operands,
preserving Wasm evaluation and trap order. The frontend's dense allocation-site
safepoint ID is retained as an unpublishable retired entry so every later native
safepoint keeps its exact identity. `WAGO_AMD64_NO_DEAD_GC_NEW=1` restores all
constructor helpers for A/B.

| Measurement | Disabled | Enabled | Change |
| --- | ---: | ---: | ---: |
| eliminated constructor sites | 0 | 2,048 | +2,048 |
| static synchronous helper sites | 8,188 | 6,140 | -25.0% |
| generated native code | 7,479,004 B | 7,104,256 B | -5.0% |
| fresh median, stable range | 0.93-1.04 ms | 0.62-0.67 ms | about -34% |
| fresh host allocation | 924,536 B/op | 524,512 B/op | -43.3% |
| fresh host allocations | 90/op | 68/op | -24.4% |
| sustained median | about 1.28 ms | 0.92-1.03 ms | about -24% |

The plugin-complete stripped TinyGo candidate is **1,776,700 bytes**, +2,512
bytes (+0.142%) over the preceding 1,774,188-byte build. It executes Dew
successfully. The speed/host-allocation gain clearly justifies this small product
cost.

### Structured exact-reference facts

AMD64 now carries one compact exact-non-null canonical type per GC reference local
inside conservative straight-line structured regions. Exact constructors and
successful non-null final casts establish facts; local copies preserve them;
local writes replace or clear them. Blocks, loops, branches, `if`/`else`, exception
regions, and returns clear the table rather than requiring SSA/phi state. The fact
contains no raw pointer and remains valid across collection; resolved heap pointers
are still reloaded and revalidated by access stubs.

Adjacent final cast/access fusions run before cast elimination, preserving the
profitable 8,180 array-length and 10,224 struct-get fused sites. Facts remove only
a standalone cast already proved by a constructor or prior successful cast.
`WAGO_AMD64_NO_GC_REF_FACTS=1` restores all casts for A/B.

| Measurement | Facts disabled | Facts enabled | Change |
| --- | ---: | ---: | ---: |
| proven casts removed | 0 | 7,158 | +7,158 |
| shared native GC calls | 50,101 | 42,943 | -14.3% |
| flushes | 91,011 | 83,853 | -7.9% |
| generated native code | 7,104,256 B | 6,957,024 B | -2.1% |
| fresh median | 0.60-0.61 ms | 0.57-0.58 ms | about -5-6% |
| sustained median | 0.90-0.93 ms | 0.80-0.86 ms | about -8-12% |
| host bytes/allocations | 524,512 B / 68 | 524,512 B / 68 | unchanged |

The plugin-complete stripped TinyGo candidate is **1,777,788 bytes**, +1,088
bytes (+0.061%) over the dead-constructor build. It executes Dew successfully.

A diagnostic `wago_gcstats` build enables executed-helper instrumentation through
`Instance.SetGCHelperStatsTracking(true)` plus `Instance.GCHelperStats`; production
builds compile the hook away. Before old-struct specialization, one fresh Dew
invocation executed **1,724** of the 6,140 emitted synchronous helper sites:
**1,038 allocation helpers** and **686 mutation fallbacks**, all with old parents.
The counters additionally split struct/array mutations and old-to-young remembered
state. Those baseline counts remained exact over 100 and 500 repeated invocations.
Tracking is disabled by default.

The shared AMD64 final-reference struct-store stub now admits a Throughput old/large
parent when its validated child is non-young or the parent is already remembered.
A nursery child behind an unremembered parent still takes the helper that appends
remembered metadata; Tiny remains helper-only. This removes **426 mutation helper
transitions per call**, leaving **1,298 total = 1,038 allocation + 260 mutation**.
The 260 mutations after that step were 258 array stores and two unremembered
old-to-young struct stores. Of the array calls, 254 already had remembered membership
and a valid object card. Collector ABI v2 now publishes that card backing, allowing
the shared array-store stub to widen an existing interval in place without appending
metadata. Cardless/unremembered and Tiny stores still fall back. This removes those
254 transitions and leaves **1,044 total = 1,038 allocation + 6 mutation**. Counts
stay exact through 500 repeated calls.

| Measurement | Before old-struct path | After | Change |
| --- | ---: | ---: | ---: |
| executed helpers/call | 1,724 | 1,298 | -24.7% |
| mutation helpers/call | 686 | 260 | -62.1% |
| generated native code | 6,957,024 B | 6,957,095 B | +71 B |
| median of six fresh medians | 0.846 ms | 0.755 ms | -10.8% |
| median of six sustained medians | 0.880 ms | 0.839 ms | -4.7% |
| host bytes/allocations | 524,512 B / 68 | 524,512 B / 68 | unchanged |

The existing-card array step adds only 226 generated bytes
(6,957,095→6,957,321). Against the old-struct build, six further interleaved rounds
move the median of fresh medians about 0.671→0.660 ms and sustained medians about
0.763→0.725 ms, with host allocation unchanged. Only four array stores and two
struct stores still execute mutation helpers; all six create previously absent
remembered/card metadata. The remaining 1,038 allocation transitions split exactly
into **1,026 fully initialized `struct.new` calls** and **12
`array.new_default` calls**, with no struct-default or other-array helper executions.
The split remains identical per call through 500 repetitions.

A one-ticket native nursery-allocation prototype was rejected. The helper path
reserved one unpublished handle/extent for one later constructor, reducing total
helpers 1,044→736 and initialized-struct helpers 1,026→718. Generated code grew
6,957,321→6,992,108 bytes, however, while fresh host allocation improved only
524,512→522,528 B/op with the same 68 allocations. Six interleaved fresh rounds
were effectively neutral/noisy (median-of-medians about 0.690→0.683 ms), and
repeated execution trapped after collection because ticket lifecycle invalidation
was not yet exact. No part of that prototype is retained.

The retained replacement reserves a transactional batch of 32 handle identities
without consuming nursery bytes. Native collector ABI v3 exposes that fixed batch,
an explicit collection epoch, the real nursery bump, and the semantic allocation
counter after preserving the complete v2 prefix. The shared AMD64 constructor stub
validates the epoch, free handle, canonical final type, aligned extent, and every
compact-reference initializer before initializing bytes and publishing the handle.
Collection and Close recycle every unused identity; helper fallback handles nursery
exhaustion, Tiny, unsupported layouts, collect-every-allocation, malformed metadata,
and all collection/growth work.

| Measurement | Helper-only | Batched native allocation | Change |
| --- | ---: | ---: | ---: |
| executed helpers/call | 1,044 | 50 | -95.2% |
| initialized-struct helpers/call | 1,026 | 32 | -96.9% |
| array + mutation helpers/call | 18 | 18 | unchanged |
| generated native code | 6,957,321 B | 6,976,191 B | +18,870 B (+0.27%) |
| median of six fresh medians | 0.658 ms | 0.527 ms | -20.0% |
| median of six sustained medians | 0.692 ms | 0.573 ms | -17.3% |
| fresh host bytes/allocations | 524,512 B / 68 | 524,576 B / 69 | +64 B / +1 |

The helper split remains exact through 500 calls. Sustained execution remains
0 B/op and 0 allocs/op. The plugin-complete stripped TinyGo candidate is
**1,787,076 bytes**, +8,688 bytes (+0.49%) over the 1,778,388-byte card-ABI build.
The generated-code and release-size increases are retained because they buy a large,
repeatable execution win while staying at or below 0.5% of each measured product.
`WAGO_AMD64_NO_GC_NATIVE_ALLOC=1` restores the helper-only differential baseline.
A 64-handle follow-up reduced initialized-struct refills 32→17 per call but was
neutral sustained, made four-round fresh median-of-medians about 10% slower, added
800 B/op and two host allocations, and grew fixed collector state by 128 bytes.
The retained 32-handle batch is deliberately the smaller speed/footprint point.

The individual wall-clock rounds remain frequency-sensitive; the table uses
interleaved median-of-medians rather than selecting one favorable run.

Compiler-native count-only load facts now report 4,092 fused accesses with a prior
exact array type, 3,067 with a prior exact struct type, and 1,022 repeated immutable
`array.len` operations on the same unchanged local. Conservative same-field get/get
and set/get counts are both zero. A one-entry length cache and smaller exact-known
resolver stubs were prototyped, measured, and reverted: the cache grew generated code
and was usually slower sustained, while the smaller stubs saved 64,919 generated bytes
but were neutral-to-slightly slower. The counters remain to guide future dynamic
instrumentation; neither runtime transformation is retained.

Two additional experiments were rejected:

- specialized abstract-anyref struct/array stores preserved exact barriers but
  improved a focused set/get benchmark by only about 1% and were neutral or
  slower on the complete artifact; and
- splitting final cast/get dispatch into another Go function regressed the
  focused helper path by about 2%, so the switch case remains local.

## Rejected experiment

Inlining a complete checked native final-type `ref.cast` reduced the workload to
about 4.77 ms, but expanded generated native code from 14.6 MB to 22.6 MB because
the complete handle, ABI, heap, and canonical-type check was duplicated at all
40,899 cast sites. That trade is not retained: it violates Wago's footprint goal
and increases instruction-cache pressure.

## Remaining gap

The remaining hot Node gap is primarily architectural. The optimized artifact's
6,140 synchronous helper sites are now distributed exactly as follows:

| Helper family | Static sites | Share |
| --- | ---: | ---: |
| old/Tiny fallback `array.set` | 2,046 | 33.3% |
| old/Tiny fallback reference `struct.set` | 2,044 | 33.3% |
| live `struct.new` | 1,026 | 16.7% |
| `array.new_default` | 1,024 | 16.7% |

The V8/Cranelift/Binaryen follow-up in
[`wasmgc-v8-cranelift-research-2026-08.md`](wasmgc-v8-cranelift-research-2026-08.md)
changes the order of the next measured options. A Binaryen type-flow oracle
removed 1,024 recursively dead fixed-array-plus-struct constructor trees,
reduced static synchronous helper sites 8,188→6,140, and improved fresh Wago
execution about 0.94→0.63 ms while reducing host allocation 924,536→524,512
B/op. Full Heap2Local by itself was neutral-to-slower and tripled the frame, so
broad scalar replacement is not the lesson.

The next measured options should be, in order:

1. propagate compact structured exact/non-null local facts in count-only mode and
   use them to remove proven repeated cast/type work;
2. count repeated array lengths, same-field load/store forwarding, and short-lived
   same-reference resolver reuse;
3. add a bounded immutable-length or field-value forwarding window where counters
   prove dynamic leverage;
4. add native bump allocation plus one shared collection/handle slow path for the
   2,050 constructor sites that remain live;
5. publish a bounded native remembered-set/card barrier so old/large parent writes
   can avoid the remaining write fallbacks while preserving Tiny incremental
   semantics and exact mutation;
6. mature the retained full-retry path into compacting or segmented old space and
   quantify worst-case pause behavior for larger live graphs; and
7. qualify the profitable shared native read/cast/write paths on ARM64.

The old-space backing experiment was retained after moving it behind a genuinely
cold helper. Growth remains exact while the backing is below 16 pages (1 MiB with
the default 64 KiB page), then reserves at most 1.5x the current backing, capped
by the configured heap limit. On the 30-call artifact run this reduces host
allocation from about **10.1 MiB to 1.12 MiB per call** (**-88.9%**) and improves
median time **2.784→1.987 ms** (**-28.6%**), while interleaved fresh-call medians
remain effectively unchanged. The trade is up to 50% unused capacity only after
the old backing is already hot and at least 1 MiB.

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

Final same-type cast-plus-`struct.get` and cast-plus-`array.len` are implemented
as bounded bytecode peepholes on AMD64 and ARM64. Cast-plus-set and indexed array
forms remain candidates because their extra operands usually occur between the
cast and access. Final types use canonical type-ID equality; any future non-final
fusion must retain complete subtype semantics.

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
