# Stage 5 mature/LOS descriptor runtime

This slice implements bounded mature/LOS physical occupancy, destination
reservation/commit/cancel, and descriptor-driven empty-page reclamation. It does
not close the full Snap port, aggregate correctness, performance, or final
four-reviewer gates. The source/evidence commit is identified in the worker
handoff; `source-sha256.txt` identifies the exact owned source tested.

Existing worktree and branch: `/home/dev/.t3/worktrees/wago/experiment-rfwgc-port`,
`experiment/rfwgc-port`. Production base is
`98939b4dd875efbcda6dd53ba769ad25dec2b4f1`. Concurrent documentation and TinyGo
prerequisite work may advance the branch independently. No branch/worktree
creation, subdelegation, PR, push, benchmark, or timing experiment occurred.
Unowned corpus scripts, shared port log, conformance submodules, reviewer tests,
and TinyGo/toolchain/native-entry files were preserved.

## Policy and implementation

`NurseryRegionShift != 0` now explicitly selects both bounded Eden and mature/LOS
policy. Zero retains the default allocator. Existing region validation still
requires a fixed collecting Throughput nursery; there is no new default activation,
heap-limit increase, separate LOS budget, or resident 512 MiB allocation.
`ThroughputHeapBytes` remains the combined old/large capacity and `ThroughputPageBytes`
defines mature descriptor width, independently of the Eden region width.

The allocator holds one optional pointer (eight bytes on the tested amd64 host).
Only region mode allocates its private state. Descriptor storage starts empty,
grows geometrically as actual placements require it, and is capped at
`ceil(heapLimit/pageBytes)` using uint64 arithmetic. Each descriptor is 32 bytes
on this host. Cancelled metadata growth can remain as bounded reusable capacity;
rollback does not promise byte-identical metadata capacity. Payload backing retains
its existing lazy growth and Go ownership. Descriptor storage owns no payload.

Descriptors distinguish published physical bytes, published LOS bytes, reserved
physical bytes, reserved LOS bytes, retirement candidates and retirement epochs.
Payload lengths remain in handles; backing length remains separately inspectable.
Every object contributes the exact intersection of its physical allocation with
every covered page, including class rounding and absorbed tails. Large objects
remain contiguous, nonmoving physical extents across boundaries; age/tenure changes
do not allocate or double-count them.

Existing promotion planning now opens a mature admission transaction. Actual
spans are claimed from the shared allocator and descriptor capacity is prepared
before any source copy or handle publication. All successful destinations stay
reserved until the entire plan passes precommit. Commit converts reservations to
published occupancy in the collector-owned, non-reentrant publication phase.
Abort unwinds destinations in reverse, including both reused spans and bump spans,
restores backing/bump and requires zero outstanding descriptor reservations.
Checked large allocations use the same transaction before publication, including
ordinary builds. Failed required admission records its first cause; a new attempt
clears it. Optional admission records detail without latching required refusal.
There is no optional evacuation driver yet.

The exact-span serial allocator is the admission proof: every destination's actual
physical extent is held before copying. This is not a transplant or weakening of
Slipstream's parallel run-credit formula. No helper count, worker slack, virtual
window offsets, or dispatch heuristic is invented here. The region path currently
uses individual destination claims rather than the baseline equal-size run fast
path. Parallel run admission and its conservative reservation arithmetic remain
explicit future parity work.

Full tracing releases dead handles' exact physical contributions into the existing
pending-free ledger. Empty descriptor candidates trigger ledger reconciliation and
tail trimming; retirement epochs advance only after reconciliation succeeds.
This makes reclamation policy observable: the paired default control leaves its
dead span pending, while region mode immediately retires the empty pages and
rewinds the bump. Pages are never separately inserted into the free index, so
there is no double free of cross-page objects. Mixed pages retain their live
extents; their holes remain eligible through the shared allocator. Backing capacity
is retained for reuse, not returned to the OS or zeroed across live neighbors.
A reconciliation failure retains authoritative pending debt and retirement
candidates for retry, after Eden/card cleanup has completed.

Native Eden cancellation, handle layout, `NativeCollectorView` and `Spaces` remain
unchanged. Collection and checked allocation retain the prior reservation
invalidation boundaries. Mature metadata never escapes to generated code. Heap
backing publication still uses the existing refresh paths. Only internal Collector
size/offset footprint assertions change for the new optional pointer; no native
ABI field or artifact offset was added. Native LOS batches remain disabled in
region mode. No helper pool was added.

## Qualification

`validate.sh` executes collector packages, selected AMD64 GC backend tests and
`^TestGC` guest tests in ordinary, wagodebug and wago_regalloccheck, with bounded
Go test timeouts. It does not run benchmarks. Go test's printed durations are
incidental runner output, not performance samples. Additional focused final
stage5 logs cover all new tests on the final owned source. `results.json` records
command results and test/skip counts; raw logs are adjacent.

New assertions cover:

- Real reserved versus published occupancy and reverse cancellation of reused
  destinations; class rounding and a separately exercised absorbed 16-byte tail.
- Fragmented capacity with enough aggregate free bytes but no usable large extent;
  required versus optional refusal and successful retry clearing prior cause.
- A non-page-aligned 8208-byte configured heap, bounded final descriptor, exact
  exhaustion and uint32-oversize refusal without mutated occupancy.
- A nonmoving 8208-byte LOS object starting at offset 32, intersecting three pages;
  exact per-page occupancy, intact tail payload, a neighboring object's death,
  root-drop retirement, actual ledger/bump reclamation and no double retirement.
- 20,001 promotions through a remembered parent with exact child payloads, bounded
  handle/backing occupancy, zero reservations between collections and final
  descriptor-driven reclamation.
- Survivor copying with the entire shared old/LOS budget occupied: zero promoting
  bytes requires no destination credit. Default mode allocates no mature metadata.
- Debug metadata growth failure after 128 already-reserved destinations, exact
  source handles/payload/Eden preservation, full reservation unwind and retry.
- Debug late destination/precommit failures, LOS metadata/backing/publication
  failures and failed empty-page reconciliation followed by retry.
- Debug consumed remembered-card and persistent-root retry, including array slot
  9999 above offset 65536. Wago references in that array are four bytes; the
  initial fixture's eight-byte assumption was corrected without changing policy.

All previous stage4 baseline tests remain controls, including their 20,001-cycle
promotion, fragmentation, refusal/retry, zero-credit and high-card probes.
`Verify` additionally rebuilds expected physical descriptor occupancy from exact
handles and rejects stale reservations or mismatches. New tests assert policy
state and actual reclamation independently of that verifier.

Initial broad runs exposed fixed-size footprint assertions after adding metadata
state. The final design uses a lazy pointer instead of embedding the state, and
updates the explicit footprint expectations. Initial failing collector logs are
retained as `initial-*-footprint.txt`; these were implementation qualification
failures, not claimed baseline defects. Concurrent independent native-review test
files were present during broad runs; their hashes are recorded separately and
none is owned or committed by this slice.

Final results: all nine primary commands and all three diagnostic backend
commands exited zero. Collector top-level pass counts are 423 ordinary, 451
debug, and 423 regalloccheck. The final new stage5 tests pass 8/13/8 respectively,
with no skips. Diagnostic backend runs use `wago_codegenstats` through
`validate-diagnostics.sh` so diagnostic-dependent GC tests actually execute.
Broad suites retain their documented profile/diagnostic/dead-charge skips;
counts are in `results.json`. This is scoped amd64 qualification, not a zero-skip
full-root gate or TinyGo qualification.

## Source mirror and remaining parity

Read the launch briefs, local AGENTS, stage3 status and stage4 native/mature/mirror
handoffs. Read-only sibling audit used Slipstream's landed
`82d546679bab4887acfab056f6ad773a969b1e03:gc/native.goose`, including `ng_admit`,
and Starshine `e075c47e61b01ab9e4fd0ae4a7ca47b755e3fca1`'s
`docs/wiki/wasm-gc-core-boundary.md`. Slipstream oldspace `ccc6512de` increases the
virtual mature partition from 120 to 376 MiB with 128 MiB LOS; `bb0900efa` preserves
`bytes + workers * block` and largest-object slack per admitted run, accepts zero
promotion bytes, and distinguishes optional refusal. This slice does not change
those source facts, infer a leak fix, or transplant virtual addresses. Starshine
provides model/validation/authoring surfaces, not a corresponding native collector.
No pre-existing collector defect is triaged. Parked type-cap C1/C2 remains
excluded. The owner subsequently authorized tabled Wago PRs 997, 1003, 1019,
1020, 1026, 1027 and 1030 in the combined port, plus the foreign-stack fix.
The parent will assess/integrate those serially after this commit; none was
cherry-picked or assessed by this worker. Potential integration surfaces are
`collect.go` marking/cleanup order (1019), serial descriptor admission and
publication (1020), and `alloc.go` large-allocation retry/cancellation boundaries
(1030). The existing cancellation calls remain unchanged; these are potential
overlaps, not findings against unread PR diffs.

Still open: optional mature evacuation, parallel helper/run reservation parity
(including checked conservative worker-slack arithmetic and helper failure/drain),
source-sized 256 MiB live-chain qualification, source-equivalent top-group/root
admission coverage, generated LOS range-barrier qualification, recoverable host
allocator exhaustion beyond deterministic injection, cached artifacts and non-amd64
execution, deeper host/exit ownership, and full corpus/oracle reconciliation.
The serial exact-span policy has no separate source run table to exhaust; its
lazy descriptor growth and existing backing hooks are qualified here. Go process
OOM remains a host runtime failure, not a newly promised recoverable public error.

The TinyGo foreign-stack conservative-scanner prerequisite remains independently
owned. The owner reports its pre-existing stackTop/SP mismatch reproduces on
0.42 and 0.43 (`b6a36decb` evidence). This slice makes no trampoline, native-entry,
or toolchain edits and does not close that aggregate blocker. Final three-Daybreak
plus one-Claude review has not occurred and is not waived.

READY TO MEASURE only after correctness workers drain and an independent measurer
acquires the global lease. Sensitive paths: old/LOS allocation and free, promotion
planning/commit/rollback, page metadata growth, full-collection retirement and
verification; baseline nil guards and internal collector footprint also change.
Proposed workloads: default/region and no-GC controls, bounded-live promotion
churn, high/low survival, zero-promotion survivors, fragmented mixed-page heaps,
required refusal/retry, LOS cross-boundary and high/range writes, metadata growth,
native partial reservation/refill/cancel and host reentry. Include compile costs
on identical module/options. Implementation did not measure itself.
