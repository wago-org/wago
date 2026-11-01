# Stage 6: optional mature evacuation design and implementation handoff

Design only, 2026-11-01. This is required remaining full-port work, not a proposal
to narrow the port to marking/sweeping. No implementation or passing runtime gate
is claimed. Owned directory: `docs/rfwgc-evidence/stage6-evacuation-design/`.
The owner's latest instruction supersedes the brief and older mirror exclusions:
all seven GC PRs belong in the one ready combined PR subject to integration and
qualification; no PR closes now. This worker neither publishes nor delegates.

Read local `/home/dev/Projects/wago/AGENTS.md` (no worktree AGENTS.md exists), both
`/tmp` launch briefs, the port log, stage4-mirror, stage5-mature-runtime,
stage5-mature-review and rollup-collector-assessment. The starting committed Wago
pin is **45c83820fbc10ee31eaaa8437e12464955b2fb9d**. Slipstream source pin is
**82d546679bab4887acfab056f6ad773a969b1e03**. Read-only Starshine comparison pin is
**e075c47e61b01ab9e4fd0ae4a7ca47b755e3fca1**; its current local HEAD was separately
observed at `1d6c0af47140299d8f3c01ed03060d279c43ee03`, not used to silently change
the comparison. [pins.json](pins.json) records exact blobs and hashes.
[Source excerpts](slipstream-source.txt) retain line-numbered primary evidence.
No sibling checkout was changed or fetched.

The native integrator is concurrently importing #1020. Uncommitted `parallel.go`
and changes in `collect.go` were inspected only to identify interfaces below;
the committed pin remains authoritative for absence claims. These observations
are not a review verdict on unfinished work. Reconcile symbol names against the
integrator's coherent commit before implementation. Preserve all shared dirt and
index entries; commit only this directory with `git commit --only` and force-add
ignored documentation.

## Exact landed behavior and mapping

All source symbols in this table are in Slipstream `gc/native.goose` at the pin;
Wago paths are relative to `src/core/runtime/gc/native` at the Wago pin.

| Source contract | Wago mapping and missing behavior |
| --- | --- |
| `NG_BLOCK=32768`, `NG_LINE=256`, `NG_LARGE=16384`; `NG_OLD=8388608`, `NG_LOS=402653184`, `NG_WINDOW=536870912` | `throughput_alloc.go:throughputHeap`, `config.go:normalizeConfig`, `collector.go:handleEntry`. Shared old/LOS Go backing, configurable pages and class rounding differ. Do not copy window sizes or forwarding offsets. |
| `ng_collect_owned` sets `N_PRESSURE=1` after failed destination/root/remembered admission; successful major root admission precedes `ng_evacuation`; pressure is captured for worker selection before evacuation consumes it | `CollectFull`/`CollectMinor` plus allocation retry paths need an explicit attempt-owned pressure latch. The current mature admission `cause/refused` is not this whole-collection diagnostic state. |
| `ng_evacuation` returns immediately without pressure; otherwise selects every occupied block with 1..32 of 128 lines occupied, before the current major trace. It totals **all existing starts**, including objects later found dead, and tracks largest size. `ng_admit(...,required=false)` runs before forwarding; no bytes or refusal clears every selection flag; attempted selection clears pressure | `mature_regions.go:matureRegion.live/large` records physical bytes, not sparse-line occupancy or selection. Need bounded selection, source exclusion, conservative pretrace demand, optional refusal, and an actual relocation driver. Existing optional-admission tests alone are not parity. |
| `ng_forward` in mode 2 only moves selected mature objects; all other objects, including young and LOS, are marked/enqueued in place. Selected objects claim destination, copy, set starts/marks/lines, publish forwarding, enqueue; `N_EVACUATED` is distinct from promotions | Wago stable `Ref`/handle identity allows tracing first and then copying only marked selected handles, without rewriting reference words. `CollectFull` currently marks/sweeps and leaves young in place, which must remain true. Need distinct evacuated payload/physical counts and same-generation stable identity. |
| `ng_admit` resets run count/claim, returns true for zero bytes, otherwise needs `bytes + reservationWorkers * NG_BLOCK`; each free-line run contributes only `size-max(16,largest)` when positive; selected blocks excluded; run count <=65536, protection before movement | `beginAllocTransaction`, `alloc`, `rollbackSuccessfulAlloc`, `restoreAllocTransaction`, `tryAllocRun` are serial exact-span building blocks. Region mode bypasses the ordinary equal-size run fast path. Full parallel run-credit/ownership parity remains required; the design below explicitly supplies it. |
| `ng_old_destination` claims an admitted run atomically, owns its cursor privately, and cannot run out after forwarding begins | #1020 admits exact plans on owner, then copies disjoint destinations. Keep coordinator allocator ownership and add bounded run admission/assignment. Do not allow helpers to call the shared free-tree allocator. |
| `ng_trace_workers`/`ng_sweep_workers` active counts are independent of conservative reservation count; `ng_dispatch`/`ng_join`/`ng_supervisor` seal and drain phases or poison | Wago channels/WaitGroup can implement equivalent ordering. Need explicit success/error outcome, immutable phase storage, active set, failure drain and no reuse while a participant can write. Claim epoch is not completion identity. |
| `ng_sweep_blocks` removes unmarked starts, clears evacuation flags, refreshes free-block bits, discards empty blocks; `ng_sweep` independently releases dead LOS extents | `sweepAll`, `releaseHandle`, `releaseMature`, `reclaimEmptyMature`: free exact physical spans once, retain Go backing for reuse, retire empty descriptors after debt reconciliation. Moving a live object must not call `releaseHandle` or fire reclaim callbacks. |
| `ng_attempt_begin`/`ng_refuse`, optional admission and destructive failure | Need collection-level result/first cause separate from optional-skip detail. Optional destination refusal is success-without-evacuation, not a required refusal or poison. A genuinely destructive failure is not retryable rollback. |

The landed diagnostic change is `bb0900efa2d5db8d037f3990ce587a4127a8c9db`;
capacity expansion is `ccc6512de266916d2e6aabab09c50ba465019953`. The source pin's
major collection does **not** promote young objects, even under pressure. Source
selection is optional within the collector; there is no evidence here of a
separate user-facing source evacuation switch. Wago's explicit switch below is
a rollout adaptation, not a source feature claim. Parked type-cap C1/C2 is not
made landed by this design.

## Bounded representation and policy

Proposed names below are new APIs/internal types, not existing symbols.

Add `Config.MatureEvacuation bool`, default false, valid only for collecting
Throughput with nonzero `NurseryRegionShift`. Propagate it through runtime and CLI
configuration/schema using the existing region-policy plumbing. Reject Tiny and
collection-disabled combinations. `DisableMovingNursery` controls young movement;
it must neither secretly enable nor disable mature movement. For TinyGo retain
serial collector operation and qualify the option on that implementation before
claiming support; rejecting an unqualified target explicitly is preferable to
silently doing nothing, but target support remains an aggregate port gate.

Use a separate evacuation-block overlay of **32 KiB blocks and 256-byte lines**
over the shared throughput address space. This retains source density selection
without changing configured mature descriptor pages, native ABI or allocator
classes. A temporary two-word occupied-line bitmap per block can be rebuilt
from every old/LOS handle's **physical** extent before tracing, not payload bytes
or only marked handles. Overlay bounds are `ceil(heap.limit/32768)` (<=131072
for uint32 addressability), checked with uint64 arithmetic. The partial final
block has its actual limit; no synthetic capacity credit. Page descriptors remain
the authoritative live/reserved/large accounting.

LOS never moves: exclude every overlay block intersecting any `spaceLarge`
extent, including a young LOS object and cross-page/cross-block tails. Wago old
objects may cross block boundaries or be larger than Slipstream's small objects.
Build connected components of blocks joined by a crossing old physical extent;
a component is eligible only when every touched block is sparse (1..32 lines),
contains no LOS, and all extents are valid. Select the entire component or none.
This conservative grouping prevents partial-source reclamation and explains
exactly why some Wago layouts retain a sparse page. It does not make all old
objects nonmoving: include positive multi-block relocation fixtures where every
touched block qualifies. Capacity/density is based on physical extents including
class rounding and absorbed tails, not the Wasm type's reported payload size.

Bound a single attempt to 65536 candidate handles and 65536 destination runs,
with checked byte totals <=heap.limit; overlay metadata is bounded by heap.limit.
Handle traversal is bounded by existing handle-table length, no unbounded work
queue. If adding a component would exceed the candidate cap, leave it for a later
pressure attempt and advance a deterministic block cursor after this attempt.
Never truncate a component, silently drop an edge, or report an oversized
candidate as moved. A component that alone exceeds the cap stays in place with a
visible budget-skip reason. Optional bounded fallback is permitted; the normal
eligible under-cap graph must have a real successful-moving path.

Pressure is a sticky request from failed required mature destination admission
and pre-movement root/remembered admission. Keep it through failed major root
admission; consume only when an enabled major has admitted roots and considered
evacuation (including empty selection/optional refusal), as in the source. For
deterministic tests use an internal force-pressure hook, not elapsed-time policy.
Do not confuse nursery-growth pressure or pause adaptation with this latch.

## Transaction and run admission

1. Stop mutator execution using the existing collector/domain ownership boundary;
   reconcile published native reservations before reading handles. Respect host
   view lifetime: `StructPayloadReadOnly` explicitly requires preventing collection
   and relocation while borrowed. Stable handles do not make borrowed byte slices
   stable. Root callbacks and custom object-edge sources stay on the coordinator.
2. Admit roots/required inputs, preserve retry card inputs, and reconcile old
   pending frees **before** taking the allocator rollback checkpoint. Preexisting
   dead debt reconciliation may persist on refusal; do not claim byte-for-byte
   allocator history rollback across that preliminary operation. Logical live
   graph and subsequent allocation ownership must be equivalent.
3. Snapshot candidate handles/entries/physical extents, selection overlay and
   immutable root/trace inputs. Compute conservative demand from all selected
   occupied objects before current marking, just as source starts are counted.
   Use `beginAllocTransaction` extended with an explicit `required` argument;
   its current unconditional `true` cannot open optional evacuation correctly.
   No nested admission. Keep required promotion and optional evacuation separate
   attempts; major evacuation does not need young destination reservations.
4. Reserve bounded destination **runs** from indexed free intervals and fresh
   backing, excluding selected source blocks. Do not merely reject an ordinary
   first-fit result after consuming it: implement a constrained interval planner,
   split at exclusion/block boundaries and select the next allowed interval.
   Check exclusion against the whole physical extent, including absorbed tails.
   Backing may grow on the owner before dispatch; it may relocate Go storage.
   Freeze the final backing slices before any helper starts. Wago LOS and old
   share one budget; no virtual extra old capacity is created.
5. For full conservative run parity choose destination quantum `B=32768`, and
   require every admitted run length <=B. Let `M=max(16,largest physical request)`
   and `D=sum(physical requests)`. Check multiplication then addition in uint64:
   `need=D+reservationWorkers*B`; credit sums `max(0,runBytes-M)`, checked too.
   All source-selected small objects that fit the bounded sparse policy fit B;
   if a representation-specific request exceeds B, skip its entire component
   with an explicit size reason rather than using this proof outside its domain.
   Cap run count at 65536. Required young run admission uses this same proof only
   for requests fitting B; LOS tenure needs no destination. Oversized requests
   need a separately proven exact-span class, never fictitious run credit.
6. Keep reservation participants fixed from collection admission policy, separate
   from actual trace/copy active participants. Reducing active helpers must not
   reduce demand. For region full majors use the admitted configured pool bound;
   minors may retain owner-only reservation below the chosen small-work gate,
   independently of phase scheduling. **D=0 succeeds without runs or worker
   slack**, even when the old heap is full; survivor-only copying and in-place
   LOS tenure must not fail optional/run admission.
7. Reserve the actual whole runs from the allocator, journal their original
   extents and charge them as reserved physical bytes in page descriptors. Owner
   partitions these runs into deterministic per-worker cursor streams and exact
   per-object destinations before copying. A checked serial simulation proves
   that each request fits and each destination is unique. This supplies source's
   run ownership and stranded-tail accounting while preserving #1020's immutable
   plans. Do not treat the source credit inequality alone as a proof for Wago's
   different physical sizes. If simulation fails, unwind before movement.
8. Preallocate plan storage, descriptor capacity, free-index rollback nodes,
   unused-run release records, and source pending-free debt capacity. Record
   actual destination class/physical allocation length. Subdivide runs with exact
   physical extents; do not invent an absorbed-tail flag on an interior object.
   Return unused suffixes via explicit spans, or absorb only a valid final 16-byte
   tail and account for it. Bound all metadata from runs/candidate count/page
   count and uint32 limits. Run metadata exhaustion is a real optional refusal.
   Host Go OOM is not promised recoverable; deterministic hooks test recoverable
   admission errors, not runtime-fatal allocation failure.
9. Mark the complete graph without rewriting handles or source bytes. Freeze the
   marked candidate subset after all trace participants drain. Release destinations
   reserved for dead candidates and unused run capacity through the run journal,
   not by also rolling back every subobject. Descriptor reservations then cover
   only live planned destinations. Required marking failure cancels the whole
   collection; optional destination refusal before marking instead proceeds with
   the ordinary full mark/sweep path. Preallocate release machinery so narrowing
   the plan cannot introduce an unhandled recoverable error after copying.

The exact-span planner remains the serial oracle and a legitimate implementation
stage. It alone does **not** close the requested run-reservation gate. The final
combined port must include and qualify the bounded run planner/credit/ownership
above (or an explicitly reviewed equivalent with all the same invariants), not
label the gate unnecessary because helpers only copy. Helpers need not acquire
runs dynamically: deterministic owner assignment is a justified Go representation
adaptation with explicit reserved slack and nonoverlap proof.

## Copy, publication, rollback and retirement

Introduce `matureMove {handle, before, destination, sourcePhysical}`; preserve
`before` separately from mutable handle storage. Move entries retain size,
remembered bit and cardSlot; destination supplies offset/class/space. Generation,
`Ref`, external token identity, major-block handle membership, type/header and
object reference words remain unchanged. An old-to-old move is not a young
promotion: do not call `promoteHandleTo` (it reads nursery bytes), do not increment
promotion/young census, and do not call `releaseHandle` (which reclaims identity,
cards and ownership). Major young spans and ages remain untouched.

Copy helpers read stable source slices and write only their admitted, nonoverlapping
destination ranges. No helper source poisoning, allocator/descriptors/cards,
callbacks, native refresh, handle publication, backing append or metadata growth.
All source and destination intervals are checked disjoint; source block exclusion
also prevents a destination occupying another selected source's padding/hole.
Copy exactly object bytes; initialize destination padding deterministically when
required by existing poison/verification policy. LOS offsets/payloads stay fixed.

All workers join with explicit success/error before any publication. Test failures
before dispatch, after partial destination copying, and immediately before commit.
Copied but unpublished destination bytes are disposable. On recoverable failure,
join/drain every dispatched peer first, undo run reservations once, restore bump
and backing slice, cancel reserved descriptor contributions, reset ephemeral
marks/claims/selection and restore consumed root/card inputs; leave original
handles and source bytes intact. Refresh native view after rollback. Metadata
capacity may remain within its bound, but no reservation/live occupancy may leak.
A bool saying 'parallel path used' is not an adequate error result.

At the commit barrier every recoverable hook and capacity check has passed. Owner
converts remaining reserved destination occupancy to live, publishes new entries,
records each old physical source extent once as pending free and subtracts its
live descriptor contributions **after** admission closes. `releaseMature` switches
between reserved/live by `admission`; calling it on a live source during admission
would debit the wrong ledger. Use a pre-reserved, nonfallible debt append and keep
source extents, not new entries, for that release. No callbacks or fallible
allocator insertion in the handle publication loop. Commit is logically atomic
under the stopped-mutator lease, not a claim that several Go writes are hardware
atomic. A panic/invariant violation after this barrier is fail-closed/poisoned;
never claim rollback of already published handles.

Then sweep dead handles normally, preserving moved live marks. Retain handle-keyed
object cards and remembered membership; invalidate cached address-derived bounds
where necessary (`lastCardBounds`) and refresh native views before guest reentry.
Keep `finishFullCardMetadata`'s young-survivor condition: moved old objects may
still point to young objects. No blanket card clear merely because evacuation
succeeded. `reclaimEmptyMature` reconciles source/dead pending spans and advances
empty-descriptor epochs once; pages themselves are never separately freed.
Reconciliation failure after publication is a committed-collection maintenance
failure with authoritative debt retained for retry, not optional-copy rollback.
This distinction must be visible in diagnostics and deterministic tests.

Track collection attempt kind/result/first required refusal separately from
optional evacuation outcome (`disabled/no-pressure/no-candidates/budget/credit/
metadata/copy-abort/moved`) and admitted demand/run/worker counts. A later optional
skip cannot erase an earlier required error. A successful later attempt resets
old required cause. Counters advance only for committed moves; no timings added.

## Contract for the #1020 integrator

The observed in-progress `parallel.go` supplies `parallelCollector.run`,
`traceWorker.copyPromotions`, `copyPromotionParallel`, `drainParallel`,
`sweepParallel`, `closeParallel`. Its current copy returns bool and poisons young
sources in the helper; current promotion callers commit descriptors before copy.
That can rely on a no-failure copy phase today, but must change before adding
recoverable copy failure/mature movement. This is an interaction requirement,
not a finding against an uncommitted intermediate diff.

- Integrator owns production. Reserve a generic prepared-copy interface with
  explicit source/destination ranges and a result `(used, err)` or equivalent;
  mature copy cannot assume nursery source. Move poison to successful owner commit.
- Promotion and mature evacuation both use prepare -> immutable phase -> join ->
  descriptor/handle commit. Keep minor dense/sparse/LOS publication semantics and
  the 1019 mark-high-water fix; mature moves do not modify the young ledger.
- Phase-owned plans/slices/claim storage survive until every dispatched participant
  acknowledges exactly once. Worker completion is distinct from claim epoch.
  Use full-width phase identity or a non-reusable Go phase object; stale/duplicate
  notifications cannot decrement a new phase's pending count. Do not recreate
  Slipstream's truncated futex-generation problem (R9 H1-F1).
- Select active participants per trace/copy/sweep independently of fixed reservation
  participants. Fault injection that names a helper forces real participation.
  Queue overflow retains work; failure/cancellation reconciles private stacks and
  outstanding work, wakes waiting peers and drains them before phase reuse.
- Recoverable injected abort before publication returns an error with intact source.
  Unexpected destructive state poisons and disables admission. Close drains an
  active phase and destroys the pool only after all writes cease. Recovery must
  not blindly `Done` while peers wait forever on abandoned outstanding work.
- Sweep helpers discover dead handles only; coordinator updates source/destination
  ownership, free trees, descriptor retirement, cards and callbacks once. Custom
  object-edge callbacks stay serial. Tiny/TinyGo helper exclusions remain explicit.
- Preserve 1003 extent validation, 1030 independent reservation cancellation and
  LOS transactions, 1019 queued/captured mark reset, and 997/1026/1027 rooting/
  admission semantics after their integration. Do not use evacuation to waive any
  of the seven PR assessments or foreign-stack qualification.

## Read-only sibling cross-check and triage

Slipstream primary source above establishes the missing optional relocation and
run contracts; its direct offsets/forwarding differ from stable handles. Starshine
`docs/wiki/wasm-gc-core-boundary.md` and `src/lib/types.mbt` (`CompType`,
`TypeMetadata`, `Instruction`) describe model/codec/validator/authoring layers,
not a native relocating heap, mature descriptor ledger or helper pool. Its local
custom type descriptors are not Wago physical-space descriptors. Use its core
binary/model surface for cyclic struct/array/reference fixtures when text syntax
is narrower; parsing alone is not runtime reachability validation. No analogous
native evacuation implementation or sibling fix is identified in those surfaces.
Both comparisons were made before classifying the Wago gap as REQUIRED PORT WORK.
No newly reproduced runtime bug or severity claim is made by this design audit.

## Implementation order and completion gates

1. Wait for coherent #1020/rooting integration commits; repin current source and
   check contracts above. Add opt-in validation, attempt state and bounded immutable
   candidate overlay; test selection and disabled/default controls.
2. Implement excluded-interval/run planner and transaction journal with descriptor
   reserve/cancel and zero-demand behavior. Qualify exact physical interval coverage
   before wiring copying. Preserve the exact-span serial oracle.
3. Add serial old-to-old copy and nonfallible publication/debt handoff, then integrate
   it after full root admission/mark and before sweep. Exercise optional fallback,
   nonmoving LOS, cards/borrows/native view refresh and committed retirement retry.
4. Extend #1020 prepared copy and phase result/drain contracts; reuse serial plans
   with 1/2/4/8 participants. Add active-set/run-credit tests and nonvacuous faults.
5. Run [deterministic qualification](tests.md), final full three-mode suites, race
   suites where supported, guest/native/TinyGo/foreign-stack, artifacts/platforms,
   corpus/oracle, source-sized capacity and high-card gates. Reconcile all seven
   PR dispositions on the combined commit. Keep any failing/unrun gate open.
6. Independent leased measurement follows correctness drain; then the mandated
   three-Daybreak-plus-Claude review fan/consolidation, zero unresolved findings,
   final-head rechecks and the one ready PR. This worker performs none of those
   later publication/review actions and closes no existing PR.

This evidence-only commit changes no production/test/build input and cannot affect
runtime or compiler performance. No test, benchmark, timing or lease was taken.
Validation is source/blob pinning, document consistency and owned-path whitespace/
hash checks. Its SHA is in the handoff. Runtime gates listed here are future work,
not inherited passes from stage5. Later implementation measurement handoff must
name its own commit, branch, allocation/collection/card/native-view/phase paths and
workloads: disabled/default and no-GC controls, sparse/dense old graphs, optional
refusal/retry, survivors with zero promotion, fragmented mixed old/LOS, high-card
writes, metadata/run limits, helper active-set transitions and compilation controls.
No performance prediction is made.
