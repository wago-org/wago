# Mature admission and failure requirements

## What the landed change actually does

At `ccc6512de`, `NG_WINDOW` changes from 268435456 to 536870912 bytes.
`NG_OLD` stays 8388608; `NG_LOS` becomes `NG_WINDOW - 134217728`.
Thus the mature partition grows from **120 MiB to 376 MiB**, while LOS remains
128 MiB and nursery sizing is unchanged. This is virtual reservation in a
demand-paged heap, not evidence that 512 MiB is resident or that Wago should
allocate an equally sized Go slice. Metadata reservation stays 64 MiB.

Starts/marks/pending maps, helper tickets, block records, descriptors, scans,
root/run/region/host records, LOS ownership/extents/tree, cards and summary maps,
type IDs and free-block/top-summary maps now have derived positions. `N_STARTS`
and the control prefix remain stable for generated nursery allocation. JIT
abstract/concrete type tests and single/range card emitters import the actual
`N_DESCRIPTORS`, `N_DIRTY`, `N_DIRTY_SUMMARY`, `N_DIRTY_TOP` constants instead of
old literals. Remembered-card traversal changes from eight top groups to
`NG_DIRTY_GROUPS` (16 at this pin). LOS's packed 15-bit page offsets and 16384-leaf
extent index stay sufficient because LOS itself does not grow.

The initial open guard rejects window/tag collisions, misaligned LOS and metadata
overrun. `bb0900efa` adds window/old alignment, positive mature extent, fixed LOS
size, stable starts-map address and monotonic metadata-offset checks. This is
not a new unbounded address format: offsets must stay below the forwarding bit.
Wago instead has even handle references, handle generations for checked tokens,
space-relative offsets and a versioned native view. Validate those actual limits.

## Reservation is still conservative

`ng_admit` resets run count/claim, accepts zero promotion bytes, then requires
`bytes + workers * NG_BLOCK`. It scans free-line runs outside evacuation blocks,
credits `size - max(16, largest)` only when positive, lazily protects blocks,
updates old high-water and caps admitted runs at 65536. The formula and per-run
slack are unchanged by capacity expansion. `bb0900efa` guards the addition
against overflow (its workers are bounded by source configuration); it is not a
general proof for arbitrary worker multiplication. Wago arithmetic needs its own
checked multiplication/addition under its own limits.

`ng_collect_owned` still separates reservation participants from active helpers.
Do not shrink destination credit just because dispatch trim/wake-join reduces
active participants. `ng_evacuation` now passes `required=false`: inability to
reserve optional evacuation destinations clears evacuation selection and does
not latch a required-refusal reason/result. Admission detail cells may still be
updated by this optional attempt. Neither a fallback nor an optional skip should
be reported as a failed required collection.

## Diagnostic attempt state

The diagnostic commit introduces owner-written attempt kind/result, first refusal,
admission need/credit/bytes/largest/reservation-workers, minor/major attempt counts
and refusal count. Attempt begin clears prior diagnostic details; first refusal
wins, cleanup cannot overwrite a completed result, and success does not overwrite
a refusal. Required pre-movement refusal can remain unpoisoned and retryable;
failed destructive tracing poisons and disables fast allocation. Root publication,
borrow/mode exclusion, old credit, run table, protection, LOS capacity and internal
trace failures have distinct source reasons. These numeric codes need not become
Wago's public API. Preserve Wago error contracts while making causal stage and
rollback outcome inspectable in focused tests.

## Wago mapping and decisions before implementation

Paths below are under `src/core/runtime/gc/native` at production `5b31c81ec`.

| Source invariant | Wago responsibility and decision |
| --- | --- |
| Admit destinations before movement | `collect.go` builds promotion plans, injects destination/commit failures, then calls `commitPromotionPlans`. Extend that transaction to mature region descriptors/occupancy. Keep handle entries and live source payload unchanged until every destination and root requirement is admitted. |
| Undo allocator preparation | `throughput_alloc.go` snapshots backing and bump in `beginAllocTransaction`; callers free successful reused spans in reverse order before `restoreAllocTransaction`. A snapshot alone does not restore the free-span index. Include new region reservation state, pending frees and metadata in an explicit rollback design. |
| Physical allocation differs from payload | `handleAllocSize` includes class rounding, 16-byte alignment and absorbed tails. Admission/reclamation must count physical spans, not only `handleEntry.size`. Distinguish live bytes, reserved bytes, backing capacity and metadata. |
| Derived layout and high cards | Preserve `NativeCollectorView`, `Spaces`, handle layout and `refreshNativeView` consistency. Backing movement must republish all native consumers before entry; a noncontiguous directory requires a deliberate ABI/artifact change. Test high offsets/card groups and metadata growth, rather than transplanting source constants. |
| Mature/LOS separation | Keep `spaceOld` and `spaceLarge` semantics, nonmoving large-object tenure and exact old/large-to-young barriers. Region accounting must not silently consume a separate LOS budget or misclassify young large objects as Eden. |
| Capacity policy | Select bounded mature region backing/admission using Wago configuration and address limits. Source's larger reserved window explains its fix, but is not an optimal Wago capacity recommendation. Do not use a larger default to conceal a leak or rollback failure. |
| Optional evacuation | Ordinary collection can retain a live mature region when optional relocation cannot reserve. Keep required promotion failure distinct. No partial handle publication and no false success after destructive failure. |

## Concrete qualification derived from source tests

These are requirements for the later worker, not executed results in this report.
Run new Wago fixtures with default-policy oracle and region policy where valid,
in ordinary, wagodebug and wago_regalloccheck; failure injection uses its supported
mode with explicit accounting for modes lacking hooks.

1. Source `expanded_old_space(workers)` now runs with one and four workers. It
   admits a 32768-object chain of 8192-byte nodes (256 MiB), verifies destination
   beyond the old 256 MiB window, writes a young reference into a high mature
   card, collects minor then major, walks exact node values and drops the root
   for reclamation. Wago must cross its chosen metadata/card/region boundaries,
   keep graph values and stable handles through promotion, and fully reclaim
   after root drop. Include an explicit large-capacity case when the policy
   supports it; do not pretend a tiny fixture qualifies source-size capacity.
2. Source `old_credit_refusal` first creates a survivor containing 42, injects
   protection failure, then removes all free mature-block credit. Both refusals
   preserve root word/payload, root count, no forwarding and no poison. Restoring
   credit permits promotion and clears prior refusal. Wago equivalents must
   preserve handle entry, payload, region ledger, free spans, roots/cards and
   ability to allocate/collect after retry; separately inject late destination
   and precommit failures after earlier destinations have succeeded.
3. Test true insufficient capacity, fragmentation with inadequate usable credit,
   metadata/run-table exhaustion and backing allocation failure independently.
   Optional mature evacuation refusal must leave required collection successful
   and diagnostic state coherent. Assert no premature publication or lost dirty
   edges; never relax admission just to make the graph pass.
4. Source `los-range-cards` fills a 10000-reference array with an object containing
   42 and reads element 9999 after churn. Wago needs generated range barriers and
   late-array/high-card coverage in LOS as well as single-slot mature writes.
5. Source root-admission rollback now checks every derived dirty top group.
   Wago should fail root admission after consuming some dirty inputs, verify
   restoration across high groups, retry, and retain all young descendants.
6. Source allocation-failure CLI test expects numeric stage/reason/attempt/result
   fields and unpoisoned oversize refusal. Wago should assert its existing
   error identity plus causal diagnostic state, no mutated live graph, and
   successful later use; no requirement to copy stderr strings or source codes.

These fixtures supplement the existing Stage 3 Eden reuse and promotion probes.
They do not convert those earlier tests into mature-policy qualification.
