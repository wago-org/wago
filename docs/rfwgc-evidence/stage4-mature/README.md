# Stage 4 mature baseline qualification and implementation handoff

The current shared old/large span allocator passes the bounded correctness probes
below with Eden regions off and on. **This is a known baseline pass, not evidence
that mature regions exist.** No intentionally failing future-policy expectation
is installed as a green gate. Production remains owned by the native-reservation
worker; this worker changes only `rfwgc_mature_stage4*_test.go` and this directory.

## Provenance and scope

Tested immutable export: `94846b133a4c851ec5110d688453c3e9a2bf466f`, containing
production reference `5b31c81ec0c74695a90bc8d36754b92845b59f14`, plus exactly the
two new test files hashed in `results.json`. Export directory:
`/tmp/wago-stage4-mature-v19z7g`. Tests were copied from the assigned existing
worktree; this is an archive export, not another Git checkout or branch.
Concurrent production edits in the shared worktree are excluded from these
results. The test/evidence commit is named in the final handoff.

Read the resume brief, launch addendum, local AGENTS, stage3 status and
mirror/runtime/integration/review handoffs. Incorporated committed mirror
`9d6d3ff7b90fc243fd0d69556328902b38eb45cd`, especially its
[requirements](../stage4-mirror/mature-policy.md) and
[later-stage ownership contracts](../stage4-mirror/host-and-next-stages.md).
All nine source landings are assessed there. Source reads here used landed git
objects at `ccc6512de266916d2e6aabab09c50ba465019953` and
`bb0900efa2d5db8d037f3990ce587a4127a8c9db`, with master pinned to
`82d546679bab4887acfab056f6ad773a969b1e03`; no dirty sibling source was used.

The source change expands mature virtual capacity from 120 to 376 MiB, retaining
128 MiB LOS. It derives heap-indexed metadata and generated card offsets. It does
not imply a 512 MiB resident Go slice, a Wago default-capacity increase, or smaller
admission reservations. Source admission remains bytes plus reservation workers
times block size, with largest-object slack removed from each admitted run;
zero promoting bytes needs no old credit. Active dispatch participants remain
independent of reservation participants. C1/C2 remains excluded by the mirror's
ancestry checks; no tabled Wago PR was read or imported.

## Executed probes

Every scenario runs with NurseryRegionShift 0 and 6. Fixed Eden is 4096 bytes;
ordinary bounded heap is 16384 bytes. Explicit minor collection forces real
promotion rather than merely repeated Eden full sweeps. Verification and free
poisoning are enabled except the separate survivor-copy fixture (verification
still enabled). The churn case checks committed region occupancy before promotion.

| Test suffix | Observable contract |
| --- | --- |
| PromotionChurn | 20,001 replacement children promoted through an old parent's remembered edge; exact values/edge identities; two live nodes after each major; old bump at most 96 bytes and handle table at most four entries. Root drop reclaims the graph. |
| FragmentedPromotionAndZeroBytes | Sixteen promoted nodes, eight alternating interior holes; 64 no-young minor cycles consume no destination bytes; replacement nodes reuse the holes without bump growth. |
| SurvivorWithoutOldCredit | Entire 16 KiB old/large backing occupied; first-survival copying succeeds into a survivor semispace with zero promoted bytes and unchanged old high-water. |
| AdmissionRetry | Two 8192-byte large extents exhaust capacity; required small-object promotion fails without changing its entry, payload, bump or promoted-byte count. Dropping one large root and collecting permits retry. |
| LargeReclamation | Reuse one 8192-byte LOS extent 128 times while retaining an adjacent nonmoving extent; last-element payload preserved; exactly one reusable offset; root drop fully reclaims. |
| HighLargeCard | A 10,000-reference LOS array starts at offset 65536; late slot 9999 repeatedly retains a young child through minor/full collection. Stable LOS extent, exact values and full reclamation after root drop. Heap limit 256 KiB. Checked single-slot barrier, not generated range-barrier qualification. |
| LateRollback (wagodebug only) | Fail plan, destination and precommit after one successful hook visit. Three reusable slots below a live anchor ensure interior-span rollback; unchanged handles/Eden ledger/bump, Verify success, retry and independent extent oracle. |

The oracle takes fixture-known reachable handles and logical sizes, independently
rounds these fixture sizes to physical 16-byte extents, walks indexed and pending
free spans, sorts all intervals and requires exact nonoverlapping coverage of
[0,bump). It checks exact live count and bounded backing. It does not use mark
bits, production handleAllocSize, or production free-byte counters to derive the
expected answer. Fixtures deliberately avoid absorbed tails and larger rounded
size classes; those cases require further oracle inputs. These probes cannot
prove unknown graphs, native reservations, mature descriptors or parallelism.

## Concrete next implementation design

This is a proposal for the sole production owner, not implemented API.

1. Keep stable handle references and contiguous `throughput.mem` backing shared
   by spaceOld and spaceLarge. Layer bounded mature descriptors over Wago's
   configured page-aligned intervals, initially at ThroughputPageBytes granularity.
   Derive descriptor count with overflow-safe ceiling division from the configured
   limit. Do not copy Slipstream addresses or introduce a second payload owner.
   Keep the existing combined capacity contract until an explicit configuration
   change defines separate mature/LOS budgets; never silently reserve away LOS.
2. Each descriptor records published physical occupancy, reserved bytes and an
   epoch/state. Objects crossing descriptor boundaries contribute their actual
   intersection to each descriptor; alternatively constrain future run placement
   only through an explicit admission policy. Large objects keep exact owned
   physical spans and nonmoving tenure, with membership across every covered
   descriptor. A boundary must never truncate an extent or clear live neighbors.
3. Add private prepare/commit/abort reservation operations integrated with
   `promoteNursery` and `promoteMarkedNurseryImmediate`. Prepare admits all spans
   and descriptor metadata before copying/publication. Journal reserved descriptor
   deltas and reused-span allocations. Abort unwinds spans in reverse, restores
   bump/backing and descriptor reservations, retaining source handles/payloads and
   root/card evidence. Existing pending-free reconciliation may validly alter
   free-index shape or trim dead trailing capacity before transaction start;
   require semantic capacity preservation rather than byte-identical free trees.
4. Commit publishes handle destinations and region membership as one collector-
   owned operation, clears reservations, and refreshes native views before entry.
   The collector/domain lease owns backing and descriptors; native batches own
   only epoch-bounded reservations. No independent region pointer escapes through
   a handle. Changing Spaces resolution or discontiguous backing would require a
   separate ABI/artifact transition across all consumers.
5. Full tracing derives live physical occupancy from exact live handles. Empty
   mature descriptors become reclaim candidates; mixed descriptors retain exact
   live extents. Coordinate bulk retirement with the shared AVL/pending-free
   ledger so each span is returned once. Initially retain mixed regions. Optional
   evacuation can come later: exclude source descriptors from destination credit,
   prepare every target before publication, and retain sources successfully if
   optional admission fails. A required promotion refusal must remain distinct.
6. Add inspectable private diagnostics for required/optional admission, first
   cause and retry outcome while preserving public errors. Count payload, physical
   live bytes, reserved bytes, backing capacity and metadata separately. Metadata
   exhaustion and backing allocation failure must be precommit errors. Do not
   lower conservative reservation demand when fewer helpers are active; any
   future parallel reservation arithmetic needs checked multiplication/addition.

Eligible next work is descriptor accounting plus transactional mature/LOS
admission and empty-region retirement under the sole production owner. The
passing tests provide regression controls; new tests must additionally assert
real mature descriptor occupancy/reservation cleanup and descriptor-driven
reclamation before that implementation is declared qualified.

## Remaining mirror requirements and gates

Still required: chosen mature-region boundary/high-group graphs and explicit
large-capacity qualification; true fragmentation with insufficient usable credit;
metadata/run-table and backing failures; absorbed-tail/class-rounding oracle;
optional evacuation refusal; consumed high-card/root-admission rollback; generated
LOS range barriers; required-refusal diagnostics; native partial batches and
host result/borrow/exit ownership. The 64 KiB offset probe does not qualify the
source's 256 MiB live chain or its 16 derived top-summary groups. The mature and
LOS metadata policy does not exist at the tested head, so no fake enabled-policy
pass or speculative failing test claims these gates are complete.

The parent reports full-root suites passed, with a separate TinyGo abort
investigation keeping the aggregate gate open. This worker did not duplicate the
immutable d546a1af full-suite run. Final three-Daybreak-plus-Claude review remains
mandatory later; no delegation, review fan, branch switch, PR or push occurred.

## Validation and interpretation

Linux amd64, Go 1.27.1, GOMAXPROCS=4. Run from the immutable export, with GOROOT
explicitly set to `/home/dev/.local/share/mise/installs/go/1.27.1` and that install's
`bin/go` executable:

```
go test -p=4 ./src/core/runtime/gc/native -run '^TestRFWGCMatureStage4' -count=1 -v -timeout=5m
go test -p=4 -tags=wagodebug ./src/core/runtime/gc/native -run '^TestRFWGCMatureStage4' -count=1 -v -timeout=5m
go test -p=4 -tags=wago_regalloccheck ./src/core/runtime/gc/native -run '^TestRFWGCMatureStage4' -count=1 -v -timeout=5m
```

Final logs and counts are adjacent. Early harness corrections: direct binary plus
explicit GOROOT bypassed untrusted export mise config and inherited 1.27.3 GOROOT;
LiveObjects uses uint32; reference-pair logical size is 24 versus its 32-byte
physical slot; rollback needs a live tail anchor to rule out valid pretransaction
bump trimming. These were test assumptions, not verified production defects.

Read-only sibling cross-check: Slipstream's landed admission/diagnostic tests
preserve roots/payload and unpoisoned retry after protection/old-credit refusal.
Starshine `e075c47e61b01ab9e4fd0ae4a7ca47b755e3fca1`, read through
`docs/wiki/wasm-gc-core-boundary.md`, provides model/codec/validation/authoring
surfaces, not a corresponding native collector. No verified defect is triaged.

Tests/docs only cannot affect shipped runtime or compiler performance: test files
are excluded from production builds and evidence is not runtime input. No benchmark
or timing experiment was run; Go test durations are incidental runner output.
Future production changes still require an independent measurer and global lease
after tests drain. Proposed later workloads: bounded-live promotion churn,
fragmented mixed mature occupancy, zero-promotion survivors, required refusal/retry,
LOS late/range writes, native refill/cancel, plus no-GC controls.
