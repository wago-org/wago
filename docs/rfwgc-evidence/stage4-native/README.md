# Stage 4 native Eden reservations

This slice enables opt-in region-bounded native live structs and small arrays.
It does not close Stage 3, implement mature policy, or approve performance.
Branch: `experiment/rfwgc-port`, existing worktree
`/home/dev/.t3/worktrees/wago/experiment-rfwgc-port`. Started at
`94846b133a4c851ec5110d688453c3e9a2bf466f` (production `5b31c81ec`).
The source/test commit is named in the handoff; `source-sha256.txt` pins tested
owned files without a self-referential commit hash.

## Landed mirror decision

Read the launch brief/addendum, local AGENTS, Stage 3 status and mirror/runtime/
integration/review handoffs. Read landed Slipstream objects at
`82d546679bab4887acfab056f6ad773a969b1e03`, including the actual implementation
`ccc6512de266916d2e6aabab09c50ba465019953`, diagnostic changes
`bb0900efa2d5db8d037f3990ce587a4127a8c9db`, and current `ng_admit`,
`ng_collect_owned`, `ng_allocate`. Incorporated Stage 4 mirror commit
`9d6d3ff7b90fc243fd0d69556328902b38eb45cd`, `mature-policy.md` and
`host-and-next-stages.md`, plus mature interface handoff
`4eeaea8fc28a3715fe67dbf7304fc6ba2642ea60`.

The source expands demand-paged mature capacity 120 -> 376 MiB, keeps LOS at
128 MiB and derives metadata offsets. Admission still reserves promotion bytes
plus reservation-worker block slack, independently of active workers, before
movement. Wago adapts bounded ownership, initialization-before-publication and
transactional refusal; it does not copy source offsets or increase capacity.
C1/C2 implementation/report remain non-ancestors and excluded. No tabled Wago PR
was imported. Read-only Starshine audit at
`e075c47e61b01ab9e4fd0ae4a7ca47b755e3fca1:docs/wiki/wasm-gc-core-boundary.md`
confirms model/codec/validation/authoring scope, not a corresponding native heap.
No pre-existing production defect is claimed or triaged in this implementation.
The new array threshold guard is part of qualifying this newly enabled writer.

## Ownership and publication contract

- `native_regions.go` reserves up to 4096 bytes within one region and 32 free
  handles. Region identity is derived from ChunkStart / region width; ChunkEnd
  bounds the exclusive interval. The region bump covers the entire reservation
  until cancellation. The complete handle batch is prepared before byte ownership
  is committed. Refill cannot collect; refusal returns to the existing rooted
  helper, which cancels prior state before allocation/collection and then refills.
- The existing native state and NativeCollectorView ABI/layout/version remain
  unchanged. `NurseryAllocBytes` remains zero in region mode, preventing every
  direct global-bump writer from allocating. `NurseryObjectMaxBytes` advertises
  the actual threshold for live chunk constructors. AMD64 structs select the
  chunk only when direct capacity is zero; baseline still allocates structs
  above array chunks using the historical global bump. Array chunk admission
  additionally respects the large-object threshold in region mode.
- Generated constructors validate operands and bounds before writing. They
  initialize header/payload before setting live handle metadata; cursor advances
  within that safepoint-free publication sequence. The collector/domain remains
  serialized. Array paths check epochs; struct Count and Cursor are the existing
  serialized cancellation proof. Cancellation clears Count and advances epoch
  before another native entry can consume recycled identities.
- Cancellation returns only unused identities and the exact unused region tail;
  an ownership mismatch retains bytes rather than reclaiming another interval.
  Zero-use cancellation restores pre-alignment region bump; partial cancellation
  retains the initialized extent. Global Eden high-water may conservatively
  include gaps, but never authorizes region allocation. Full collection rebuilds
  occupancy from exact live handles; minor collection retains transactional
  promotion/rollback behavior. No region directory or persistent ABI field added.
- Collection, refill, checked allocating fallbacks, close, and successful AddTypes
  invalidate reservations. Fixed region mode already excludes nursery growth.
  Checked large allocation cancels small reservations before possible backing
  growth. Aborted invocation reconciliation cancels region state before returning
  a trap/error, preserving published objects in the exact ledger.
- Large-array native reservations remain disabled for regions. Objects too large
  for a region, unsupported layouts, dead/handleless batches, CollectEveryAlloc,
  Tiny and collection-disabled configurations retain checked fallback. Both native
  dead-batch preparation and Go batched dead-charge admission retain their original
  region exclusion. Individual checked charges still allocate/count exactly.

ARM64 array writers already consume bounded chunks and check the large threshold;
ARM64 has no live native struct writer. Its dead writers still see zero direct
capacity. Those consumers were source-inspected, not executed on ARM64. Cached
artifact/platform gates remain open; no new ABI offsets require migration.

## Qualification

`validate.sh` runs all `src/core/runtime/gc/...` tests, focused AMD64 backend tests
(`GC|Collector|NativeAlloc|Eden|Region`) and guest `^TestGC` in ordinary,
wagodebug and wago_regalloccheck, with GOMAXPROCS=4, -p=4, -count=1 and bounded
Go test timeouts. It is not the separate full-root/conformance suite.
`validate-extra.sh` runs the final new native guest tests in every mode and
repeats backend selection checks with wago_codegenstats added, because those
existing tests require compiler diagnostics. Logs and machine counts are adjacent.
Go test's printed durations are incidental runner output, not performance samples.

New tests assert:

- 1000 unused reservation cancellations reuse bounded handles and return region
  bytes; unpublished identities stay spaceFree. Partial publication/cancellation
  retains an object, refill advances regions, and collection clears the epoch.
- Successful type metadata growth invalidates the reservation; unsupported sizes,
  large arrays and native dead batches refuse. 100 checked dead allocations keep
  exact charges. In wagodebug, failed promotion commit and checked publication
  cancel reservations, retain live entry/region state, Verify and retry successfully.
- Real native typed structs initialize references to a persistent child; 200
  success/trap cycles return 42, observe consumed reservation cursors, remain
  within region bounds, and clear Count/change epochs after traps.
- Real native arrays and mixed structs/arrays run 100 cycles each with exact
  allocation counts. Partial cancellation does not double-charge. A checked large
  array cancels the small batch. A small-array chunk cannot admit an array at the
  configured large-object threshold.
- Three-level native host reentry runs default and region policies, observes
  actual native publication, keeps outer frame values through 1000 inner
  allocations, handles success/trap/success, and drains host activation depth.
- Existing region caller-root tests exercise native allocation under callee
  pressure; existing Stage 3 integration, native ABI/layout, cancellation,
  promotion and mature regression probes remain controls in the selected suites.

The mature probes were already present when this worker's collector suite ran;
these runs qualify them against the native changes, separately from their
worker's immutable baseline report. Initial new fixture corrections: a reuse
call may end on a checked refill, so native execution is witnessed across the
run rather than demanded at every final cursor. No production admission was
relaxed for that fixture. Reentry was added after the initial ordinary broad
suite; the final focused reruns qualify the complete new guest file in all modes.

## Remaining paths and measurement handoff

Mature/LOS descriptors and transactional reservation/reclamation are next under
`native/throughput_alloc.go`, `collect.go`, `collector.go`, and future mature
metadata, guided by stage4-mature. Native large-array region admission remains
checked in `native_alloc.go`; no speculative mature policy or helper pool exists
here. Platform execution, cached-artifact compatibility, deeper host result/
borrow/exit coverage, full corpus runtime/compile/oracle reconciliation, and
parallel parity remain full-port work. The separate TinyGo debug interruption
investigation does not enable regions and is not resolved by this slice.
Stage 3 measurement and the final three-Daybreak-plus-Claude review remain open.

READY TO MEASURE after tests drain and an independent measurer acquires the global
lease. Performance-sensitive paths: native region reserve/cancel and metadata
invalidation; AMD64 struct/array allocation guards and commit; trap reconciliation.
Use baseline policy off/on controls and native/helper oracle pairs: small live
structs, reference structs with caller roots, dynamic small arrays, mixed
struct/array/large-fallback churn, partial cancellation/refill, low/high live-region
occupancy and repeated reuse, dead checked charges, nested host/trap recovery,
and the existing mature promotion controls. Compare compile costs on identical
module bytes/options and include no-GC/scalar controls; baseline native emitters
also contain new policy-selection branches. This worker ran no benchmarks,
timings, measurements or review fan, and made no PR/push or branch/worktree change.
Shared corpus scripts, port log and spec submodules were preserved.

## Final results and unrelated diagnostic failure

All nine primary commands exited zero. Final native guest reruns passed four
new top-level tests / nine tests including subtests in each mode, with no skips.
GC-specific diagnostic backend reruns also exited zero in all modes. Collector
passes (top-level) are 414 ordinary, 437 debug, 414 regalloccheck; mature controls
passed in these runs. Debug collector skips three pre-existing fast dead-charge
checks because failure injection selects checked fallback. Broad guest suites
retain pre-existing diagnostics/profile/dead-lowering skips. Diagnostic backend
runs retain two source-profile subtest skips; debug also skips two lifetime replay
tests by its existing consumer-profile policy. These are explicitly scoped
results, not a zero-skip full-port gate. `results.json` records all counts.

The first diagnostic selector also included `Region`, selecting unrelated
interval-register tests. `TestIntervalRegionDynamicReuse` and
`TestIntervalRegionControlBoundaries/if-else` fail their expected activation
counters under wago_codegenstats, in all three modes. They compile scalar modules,
not GC constructors. Restoring only `gc_direct.go` from starting head 94846b133
through a Go read-only overlay reproduces both exact failures in ordinary
wago_codegenstats (`baseline-interval-diagnostics.txt`); this isolates the only
changed backend file. No interval-register source/test was changed. Broad failed
logs are retained as `*-broad-backend-diagnostics.txt`; the final GC diagnostic
selector is `GC|Collector|NativeAlloc|Eden`, as recorded in validate-extra.sh.
This is an unrelated baseline diagnostic limitation for the parent to carry,
not a newly verified GC runtime defect or a claim that the full suite is green.

Baseline reproduction: obtain
`git show 94846b133:src/core/compiler/backend/railshot/amd64/gc_direct.go` into a
scratch file, map this worktree's absolute gc_direct.go path to that file in a
Go overlay JSON `Replace` object, and run:

```
GOMAXPROCS=4 go test -p=4 -tags=wago_codegenstats -overlay=<overlay.json> ./src/core/compiler/backend/railshot/amd64 -run '^TestIntervalRegion(DynamicReuse|ControlBoundaries)$' -count=1 -v -timeout=5m
```

The sibling source audit above provides the relevant boundary: Slipstream's
native admission and Starshine's model/validation do not implement Wago's Go
interval-register diagnostic counters. No sibling runtime bug is inferred from
this test expectation failure, and no unrelated fix is folded into the port.
