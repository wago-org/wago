# Stage 3 runtime slice: region-partitioned Eden

Pre-implementation assessment, 2026-11-01. Wago base a3c08365f531eb3c9003e3acf3e0a822c12c31f4.
Slipstream fetched twice at launch/implementation boundary: origin/master
506b08d3d3fa3c8e8b1ce9ac0cb4f30a2463cdc3. All source reads use git show origin/master,
not its dirty checkout.

Landed gc/native.goose ng_allocate uses bounded bump Eden, separate LOS;
ng_collect_owned marks young in place on majors and evacuates on minors.
ng_admit reserves old destinations before evacuation; its old-space reservation
fix is NOT yet visible at this pin. Do not copy the parked fix. Type-cap
01303964b is also not landed. cc48b98ea chooses trace/sweep worker counts from
work while retaining reservation policy; cf05aaf81 dispatches only participating
helpers and joins ticketed completion. Defer both synchronization translations:
this Wago collector remains serialized and has no helper pool. ng_region owns
persistent root-slot ranges with unique ownership/cards: it is NOT a heap-region
allocation API. Wago already supplies exact persistent/frame/domain roots and
stable handles; keep these and its card barriers rather than copying ABI2 offsets.

Port shape: explicit NurseryRegionShift (log2 bytes) opt-in partitions existing Go-owned Eden
into bounded, non-spanning allocation regions. At allocation exhaustion, trace
and sweep through the existing full root universe first, reset wholly empty Eden
regions, retry allocation, then use the existing transactional minor evacuation
if live regions still block admission. Full collection retains payload addresses
and stable handles; minors retain the existing survivor/promotion contracts.
Large objects (including objects larger than a region) use existing LOS. This is
an incremental Wago adaptation, not a claim that Slipstream has this exact Eden
policy, nor the full Snap port. No static lifetime evidence authorizes reclamation.

Initial admission rejects Tiny, collection-disabled and adaptive nursery/pause
combinations; use explicit fixed Eden. Disable native allocation reservations and
handleless batches for this mode until those writers understand region bounds.
Keep native resolution/access, exact roots, all barriers, default/off allocator,
and compiler analysis unchanged. Region metadata is bounded by configuration.

Remaining: optimized region-aware native reservations; old/large region policy and source old-space mirror once
landed; independently measured runtime/compile and full campaign qualification.
Compiler-directed frame lifetimes remain a separate future proof contract.
No performance claims or source-to-Wago performance extrapolation.

The later mirror handoff 69a6d9ec5 was read and reconciled in qualification.md.
Active helper counts must never substitute for admission reservation counts.
