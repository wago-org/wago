# Deterministic implementation qualification (not executed)

Every common fixture below must run under ordinary, `wagodebug`, and
`wago_regalloccheck`. Parameterize default region-off, region-on evacuation-off,
and region-on evacuation-on controls; moving assertions apply only to the last,
while identity/reachability/card assertions apply to all. Reject invalid policy
combinations rather than quietly skipping them. Parameterize configured workers
1/2/4/8; separately exercise GOMAXPROCS=1 serial fallback and sufficient processor
availability for actual helpers. Assert worker/task participation, not just config.

Use fixture coordinates and exact integer assertions, never wall time, sleeps or
probabilistic churn to force an outcome. Channel barriers arrange helper order;
test deadlines detect hangs only. Existing `wagodebug` failure hooks do not exist
in ordinary/regalloccheck: label those cases build-excluded and run natural
capacity/budget refusal in all modes. For phase faults, add test-controlled hooks
available in each test build if feasible without release hooks; otherwise report
the exact debug-only coverage. Never claim an omitted hook executed.

## Fixture matrix and independent oracles

| Proposed fixture | Construction and required assertions |
| --- | --- |
| `TestMatureEvacuationPolicy` | Validate opt-in combinations before backing allocation. Evacuation off leaves default behavior/metadata cost policy intact. No-pressure major does not move. Required admission refusal sets pressure; failed major root admission retains it; admitted optional attempt consumes it. A nursery growth event alone does not trigger it. |
| `TestMatureEvacuationSelection` | Build old physical objects covering exactly 0, 1, 32 and 33 distinct 256-byte lines within separate 32-KiB blocks. Count occupied lines independently from literal fixture intervals, including multiple objects sharing one line and absorbed tails. Only 1..32 eligible; LOS intersection excludes a block. Include a partial final block, a crossing old object joining two sparse blocks, and a crossing object whose second block is dense; select the complete eligible component or none. |
| `TestMatureEvacuationLiveGraph` | Sparse old cycle, duplicate roots, diamond/shared descendant, self edge, packed/numeric fields and reference array. Force pressure; assert at least one old offset changes outside all selected blocks, handle and checked-token generation do not change, references and exact payload hash remain equal, and each live identity moves once. Include unmarked selected objects in initial demand but never copy/publish them. Drop roots and require complete reclamation with no double callbacks. |
| `TestMatureEvacuationNoYoungMotion` | Old parent has young child, young cycle reaches an old object, young LOS roots an old graph. A forced major relocates selected old objects but leaves every young offset/age, nursery bump and census unchanged. Subsequent minor reaches children through preserved object/global/table cards. Zero young next-cycle copying counts on major. |
| `TestMatureEvacuationLOS` | LOS extent crossing three pages and two overlay blocks, plus live and dead old neighbors. Young and tenured LOS offsets and tail bytes are unchanged. Selected destinations never touch any live source/LOS interval. Root drop reclaims LOS exactly once; evacuation never increments LOS tenure or promotion counters. |
| `TestMatureEvacuationCardIdentity` | Remembered old ref array and struct with existing cardSlot, dirty late element 9999 and writes above offset 65536; relocate old parent, retain same handle-keyed card chain/membership, then minor-collect and read exact child values. Assert no stale lastCardBounds/native address cache. Add all seven PR root-transfer/admission/descriptor regressions after their integration. |
| `TestMatureEvacuationRunAdmission` | Use independently specified free intervals, source exclusions and object physical lengths. Check run count, per-run largest-object discount, checked demand and reserved slack, and exact plan nonoverlap. Fragmented aggregate bytes sufficient but usable credit insufficient must refuse. Exact threshold passes; one byte/run short refuses. Exercise run-table cap and arithmetic overflow with bounded synthetic planner inputs, not enormous allocations. |
| `TestMatureEvacuationZeroDemand` | Entire old/LOS budget occupied, live young objects below promotion age. Minor survivor-only copy succeeds with zero destination runs regardless of reservation participant count. In-place LOS tenure likewise needs zero old credit. Major with no eligible old candidate succeeds without slack admission; optional evacuation insufficiency cannot turn it into a required failure. |
| `TestMatureEvacuationReservationIndependence` | Hold heap/candidates/configured reservation participants fixed while forcing active copy/trace sets 1→8→2→1. Demand, credit and admitted destination ownership are identical; actual worker participation changes. Compare explicit cursor assignment with an independent serial run-packing model. Large-object exact-span class is separately tested and never passed through a too-small quantum proof. |
| `TestMatureEvacuationBoundedSelection` | Small test caps force candidate/run/metadata exhaustion and rotation without huge heaps. Whole-component over-cap remains in place with reason; no truncated graph. Successive pressure attempts consider later eligible blocks; no permanent always-first prefix. Descriptor/overlay/plan/run capacities remain within derived bounds after repeated success/refusal. |
| `TestMatureEvacuationRollback` | Snapshot source entries, object bytes, roots/cards, live descriptor counts, live interval ownership and pending debt after preliminary reconciliation. Fail metadata after several destinations, final admission, after first/last copy, and precommit. All dispatched peers drain; original payload/entries remain exact; no reserved bytes, live phantom destinations, extra free identity or poisoned source. Retry succeeds. Check both reused and bump runs, final backing growth/restoration and split-run tail handling. |
| `TestMatureEvacuationCommitBoundary` | No fallible hook/callback after publish starts. Capture committed source-debt records and require one per moved source. Inject subsequent reconciliation failure: new handles remain authoritative, exact pending spans retained, no rollback of published entries, next allocation/reconciliation succeeds, retirement epochs advance only after success. |
| `TestMatureEvacuationPhaseDrain` | Block last helper after another copied; coordinator cannot publish/restore backing/reuse plans/Close storage early. Abort a helper with private queued work, wake idle peers, drain once. Deliver stale/duplicate completion, wrap claim epoch and vary active sets: none completes a new phase incorrectly. Require real helper fault participation and fail-closed result on destructive invariant failure. |
| `TestMatureEvacuationOwnership` | Custom object-edge callbacks force serial trace but still preserve graph and optional serial evacuation. Live borrowed payload blocks caller-authorized collection according to existing domain API. Host reentry returns stable handles and freshly resolved payloads; retained checked tokens survive move, cross-domain tokens remain rejected. A raw borrowed slice is not used across collection. |
| `TestMatureEvacuationNativePublication` | Generated guest allocates/publishes native Eden batch, triggers mature move during collection, then reads via refreshed native handle/Spaces view; partially used batches and checked LOS retries preserve 1030 cancellation rules. Metadata/backing growth cannot leave a stale native view. Include cached artifact roundtrip on supported targets. |

The independent model should maintain sorted owned physical intervals and compare
live spans + reserved runs + indexed free spans + pending free debt + unused
unallocated tail against heap bounds. Check no overlap/gaps below the bump, not
only aggregate page counts (two overlapping spans can give plausible totals).
Use known class sizes and fixture tails, not `handleAllocSize` itself, to derive
expected values. Independently intersect those intervals with configured pages
for descriptors and with 256-byte lines for selection. Compare oracle state at
prepare, partial rollback, successful commit and retirement. `Verify` supplements
this model; it is not the sole oracle.

Rollback is an ownership journal: after releasing an unused/dead subrange, remove
that subrange from the held-run record before any later abort. Abort releases only
still-held spans, never both the original run and its already-returned pieces.
The immutable source snapshot remains intact until commit; current handle entries
cannot substitute for it when retiring old locations.

## Execution recipe for the implementation owner

Run from a clean immutable archive of the implementation commit after concurrent
workers commit, not this dirty worktree. Record source SHA, exact commands, exit
codes, modes, tests/skips and actual workers. Use the repository's pinned Go
version and existing stage5 validate scripts as controls, without invoking any
`Benchmark` functions. Typical package checks from that archive:

```sh
go test -count=1 -timeout=10m ./src/core/runtime/gc/...
go test -count=1 -timeout=10m -tags=wagodebug ./src/core/runtime/gc/...
go test -count=1 -timeout=10m -tags=wago_regalloccheck ./src/core/runtime/gc/...
go test -race -count=1 -timeout=10m ./src/core/runtime/gc/...
go test -race -count=1 -timeout=10m -tags=wagodebug ./src/core/runtime/gc/...
go test -race -count=1 -timeout=10m -tags=wago_regalloccheck ./src/core/runtime/gc/...
```

These commands are proposed, not executed results, and a compiler/platform that
cannot run a race variant must be reported explicitly. Repeat the stage5 backend
and guest recipes with new evacuation guest selectors, including diagnostic tags
where counter assertions require them. Then execute the full repository matrix
on the final combined head; focused native tests do not replace it. Record actual
TinyGo guest tags/toolchain, since host build tags do not propagate automatically.

Remaining aggregate gates include source-sized 256 MiB live-chain capacity (with
sufficient configured bounded backing), high-card/root-admission and generated LOS
range-barrier coverage, real native/host borrow/exit boundaries, TinyGo foreign
stack and cancellation, non-amd64 execution, artifact compatibility, conformance
and corpus census/oracle reconciliation, independent measurement after workers
drain, and final four-reviewer consolidation/rechecks. None is closed by design
text, earlier stage5 snapshot passes or an optional admission unit test.
