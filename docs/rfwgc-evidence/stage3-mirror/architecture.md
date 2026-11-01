# Staged architecture: real regions under Wago handles

This is an implementation proposal, not a claim that the named new structures
already exist. All existing Wago symbols below were read at initial head
`a3c08365f531eb3c9003e3acf3e0a822c12c31f4`. Subsequent source work by other
workers requires rechecking offsets and assumptions before implementing.

## What counts as the full port

An enabled allocation must place observable struct/array payload in managed
region storage and publish a real stable reference. Collection must use region
metadata to trace, retain, evacuate or reclaim that storage. Reference-bearing
graphs, cycles, dynamically sized arrays, escape to roots, and repeated
collections must run through it. Scalar replacement and handleless checked
charges may coexist, but cannot be the only enabled paths or the evidence for
region capture. A diagnostic `region` classification without owned backing and
reclamation is not sufficient.

Slipstream's `ng_region` specifically registers persistent root-slot ranges
with ownership/card metadata; it is not a function-lifetime payload arena.
Its ABI2 object storage separately comprises Eden, survivor semispaces, mature
blocks/lines and nonmoving large extents. Port both responsibilities deliberately.
Do not equate a root region, payload region, compiler lifetime proof, or virtual
address reservation with one another.

Wago can preserve its compact references while adopting region-first storage
and collection. It must not silently reinterpret `Ref` as a Slipstream heap
offset. `Ref` is zero/null, odd/i31, or nonzero-even `handle << 1`.
Moving payload changes a handle entry, not guest references. Host/public checked
identities additionally have owner/generation rules; raw guest references are
not themselves generational tokens. Collection must discover every live raw
reference before recycling its handle.

## Concrete map and intended change boundaries

| Responsibility | Existing implementation anchors | Port shape |
| --- | --- | --- |
| Collector ownership/configuration | `gc/native/collector.go`: `Collector`, `Config`, `NewCollector`, `Close`; `wago/instantiate.go`: `prepareCollector` | Explicit immutable opt-in region policy, same domain ownership and limits. Keep Go owners for all backing reachable for the complete native lease. Decide reset/close semantics for metadata and payload independently. |
| Payload allocation | `gc/native/alloc.go`: `alloc`, `newHandle`, `bytes`; `throughput_alloc.go`: `throughputHeap.alloc`, `tryAllocRun`, `growBacking` | Introduce bounded region descriptors and allocation runs with explicit free/reserved/published occupancy. Start within existing throughput backing so handles still resolve through space+offset. Treat a later noncontiguous region directory as a separate ABI change. |
| Native ABI | `native_view.go`: `NativeABIVersion=9`, `NativeCollectorView`, `NativeSpaceView`, `ValidateNativeABI`, `refreshNativeView`; `handleEntry` stride 16 | If metadata only is added privately, preserve existing layout. If spaces/handle interpretation/generated offsets change, version the ABI and update every consumer/artifact validator together. Existing `Spaces[space]` cannot address arbitrary independent region buffers without redesign. |
| Native reservations | `native_alloc.go`: `PrepareNativeStructAllocation`, `PrepareNativeDeadStructAllocation`, `PrepareNativeArrayAllocation`, `CancelNativeAllocationBatch`, `discardNativeStructHandles`, `discardNativeLargeArrayAllocation`; `native_alloc_batch_amd64.go`; `wago/gc_native_alloc_amd64.go` | Region-backed batches must reserve bytes and handle capacity before native entry; publish only initialized objects; invalidate epochs at collection/cancel/relocation. Preserve dead-charge accounting and large-array physical extents. |
| Collection | `collect.go`: `CollectMinor`, `CollectFull`, `promoteNursery`, `commitPromotionPlans`, `sweepAll`, `releaseHandle`; `throughput_alloc.go`: `beginAllocTransaction`, `restoreAllocTransaction` | Build a serial region candidate/occupancy pass and destination admission before handle updates. Bulk reclaim only wholly dead regions; mixed regions retain exact object liveness. Promotion and optional old evacuation must preserve stable handles. |
| Roots and barriers | `roots.go`: `RootSet`, `RootGroups`, `ClassifiedRoots`, initializer root scratch, global/table slots; `Collector.remembered`, `objectCards`, `slotCards` | Persistent root regions are borrowed descriptors of already-owned slots, not new owners. Preserve exact initialized constructor operand roots and old/large-to-young edges; dirty-card clearing must be recoverable before commit. |
| JIT/root integration | `wago/gc_collect.go`: `prepareGCCollectionRootsLocked`, `collectGCWithJoint`; `gc_frame_roots_runtime_linux_amd64.go`; compiler `shared/gc_frame_roots.go`: `GCFrameRootPlan` | Keep exact frame maps at every collecting slow path, including callers/exception paths; no raw payload address may survive a collecting helper. Roots remain stable handle words while the resolver reloads backing. |
| Host and domain lifetime | `reference_store.go`: `lockGCInvocationContext`, `suspendGCInvocation`, `gcInvocationSuspension.resume`, `rangeGCDomainPersistentRoots`, `rangeGCDomainSuspendedRoots`, `acquireGCCollector`, `issueGCRefMode`, `stageGCHostResult` | Collect the complete shared domain under existing leases. A parked host activation remains rooted during reentry and callbacks; closing one instance must not free a region reachable from another instance or retained public token. |
| Compiler proof layer | `amd64/gc_lifetime_consumer.go`, `gc_dead_new.go`, `gc_struct.go` | Existing sealed scalar replay remains an optional consumer. Do not require a proof to allocate ordinary escaping WasmGC objects in region storage. Frame-lifetime allocation is a separate future proof contract, not the runtime-region implementation. |

Paths under `gc/native` above are relative to `src/core/runtime`; paths under
`wago` are relative to `src`; backend paths are under
`src/core/compiler/backend/railshot`.

## R0 — settle contracts and maintain the oracle

Pin compiler options, runtime config, native ABI and source mirror cursor in
fixtures. Distinguish payload bytes, layout bytes, allocator-rounded bytes,
reserved capacity, live/published handles, region occupancy and retained backing.
Expose only bounded diagnostic counters needed to assert the new path, without
making diagnostics necessary for correctness. Preserve Off/default behavior;
explicit On must prove it enters region allocation on eligible runtime configs.
Keep Tiny and disabled-collection paths separately identified.

Exit: a fixture manifest identifies enabled region-producing cases, fallback
cases, and unchanged zero-GC controls. Existing Stage 2 corpus has zero GC
constructors, so add a small real WasmGC graph corpus with independent outputs.
No performance gate is satisfied by synthetic scalar counts or source numbers.

## R1 — region allocation with stable handles, before native fast paths

Use Go-owned aligned throughput backing first. A region descriptor records a
bounded offset interval, allocation state, live/reserved accounting and the
object/handle membership required to reclaim it. Region granularity is policy,
not a copied Slipstream 32 KiB constant. Large objects get dedicated spans or
explicit nonmoving regions with exact extent ownership. Enforce overflow-safe
alignment and `Config` heap limits before state mutation. Decide whether
existing size classes coexist with region runs and account rounding consistently.

Allocate through checked constructors with complete initializer roots. Stage
metadata/bytes/handle acquisition, initialize header and payload including
observable defaults/padding, then publish membership and reference. Failure
before publication returns unused reservations without touching live entries.
Backings may grow only under collector exclusion, and refreshed views must be
published before native execution resumes. A reserved-but-unpublished handle
must never appear as a traceable object or leak through cancellation.

Exit: checked real structs/arrays, self/cross references and large objects
allocate in region-owned storage; checked/native view resolution agrees; repeated
cancel/retry reaches a bounded reservation/handle plateau. Do not yet claim
native allocation or region-first reclamation is implemented.

## R2 — serial region-first collection, with transactional admission

Under the existing domain stop/exclusion protocol: cancel native batches; admit
all exact roots; capture/retain remembered edges; determine young-region work
and old/large occupancy; reserve all required destinations and metadata; then
commit movement and handle updates. Existing `promoteNursery` already builds
plans and rollback state: extend that contract rather than creating a second
partial-commit escape hatch. A precommit failure must preserve payload identities,
root reachability, card evidence and allocator retryability. If an implementation
introduces unavoidable postcommit failure, define explicit unusable/teardown-only
state and test it; never resume a partially moved heap.

Minor collection follows the complete young reachable closure plus old/large
remembered edges. Survivor age and promotion age are independent of region
reuse epochs. Large young objects remain traced even when nonmoving. Full
collection follows the whole domain graph; old candidate regions can be swept
or evacuated only after destination admission excludes source regions. Empty
regions are released as units; mixed regions cannot be cleared just because a
subset of their objects is dead. Preserve exact card coverage when an object
crosses region/card boundaries and when promotion creates new young edges.

On success update occupancy, free handles/checked generations, clear only dead
starts/cards, and return unused destination reservations. Track reserved versus
used extents explicitly. Zero-promotion cycles reserve no old destinations.
A full collection must permit allocation retry after old-space reclamation;
failed admission must not permanently consume capacity. Region reuse must clear
old membership/marks/cards without erasing adjacent live storage. Do not infer
safe page discard from a survivor byte census; use actual owned used extents.

Exit: region topology/occupancy changes demonstrably drive collection;
reference-bearing graphs survive; dead regions and unused reservations recycle;
steady-live-heap repeated collections do not monotonically exhaust reservations.
All collection results match the baseline oracle in every supported mode.

## R3 — native allocation and view publication

Connect region runs to existing native preparation helpers and AMD64 generated
allocation. Every path must either complete a noncollecting reservation-backed
allocation or branch to a root-publishing helper before collection. No safepoint
may observe a half-initialized object. Refill, cancellation, trap/unwind, heap
growth, type registration and collection invalidate relevant native epochs.
Reload handles/space bases/counts from the stable collector view after mutation;
never cache a relocatable backing pointer across a helper. Audit native reads,
stores, casts, barriers and array operations as well as constructors.

Handle entry/view offsets are compiler contracts, including ARM64 readers.
If ABI layout changes, update version validation and cached artifact rejection;
unsupported emitters/configurations must select a correct checked path rather
than consume a mismatched view. Keep Go backing owners alive through native
execution even though view addresses are uintptr values.

Exit: real native-generated allocations, ref stores, old-to-young barriers and
accesses after movement pass mixed register-pressure and caller-frame tests.
Stage 2 handleless charges remain charged and cancel safely across the new
collector cycle; their success count is not a region payload allocation count.

## R4 — host reentry, shared domains, and fallbacks

Preserve existing root/domain locking order. Admission must enumerate suspended
callers, callback arguments/results, persistent global/table slots, retained
public references, and all linked instances sharing the collector. Alias imports
must not create conflicting root ownership. Root registration/growth/teardown
must occur under the same leases as collection; root slots cannot be invalidated
by another instance's close while an invocation is suspended.

Tiny stays the fixed nonmoving incremental collector unless explicitly ported
and separately qualified. `DisableCollection` is an allocation policy, not proof
that roots are absent: preserve bounded allocation/exhaustion, and do not
accidentally invoke region reclamation on a path whose exact frame roots are
unsupported. Keep public explicit collection behavior distinct from automatic
collection policy. Stress/failure-injection builds may deliberately use checked
fallback; tests must assert that fallback rather than demand a fast-path count.
Borrowed guest storage retains its collection exclusion contract.

Exit: nested callbacks, multiple same-domain instances, cross-domain rejection,
close while suspended, traps/EH/tails and retained tokens qualify through public
runtime entrypoints in all three modes. No parent gate can infer this from the
five native-unit smoke tests retained in this directory.

## R5 — optional parallel collection only after R1–R4

If parallelism is included in the full-port scope, adopt the landed source
active-set/ticket invariants and bounded work queue overflow discipline, using
Wago-compatible atomics/ownership. Budget workers globally, with owner-only
execution when no helpers are admitted. Separate reserved capacity from actual
participants; do not let scheduling alone change admission outcomes. No helper
may outlive the backing it can touch. Join all dispatched work before recycling
regions, publishing a collection result or resuming the mutator. Queue exhaustion
must retain all edges using a bounded fallback, not drop work.

Parallelism is not necessary to establish real serial region allocation and
region-first collection. If the parent treats full Snap behavioral parity as
including parallelism, this gate cannot be marked complete by a defer decision.
Do not present the staging decision here as permission to omit required scope.

## Final acceptance and measurements

Every implemented slice needs ordinary, wagodebug and wago_regalloccheck tests,
new-path assertions, failure injection, and baseline differential evidence.
Before the eventual PR, close conformance/toolchain gaps and the owner's four-
reviewer consolidated gate with zero unresolved findings. Source performance
studies do not satisfy Wago gates. After correctness, the independent measurer
under the global lease must compare runtime/compile costs with matched controls,
real GC graphs, low/high survival, promotion/large-object churn, host reentry,
shared domains, Tiny fallback and no-GC corpus cases. Report allocation bytes,
reservation high-water, retained backing and live objects separately from timing.
