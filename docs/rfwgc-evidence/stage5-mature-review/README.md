# Intermediate independent mature/LOS correctness review

No verified correctness defect found in the reviewed slice. This is an intermediate
review of the serial mature/LOS implementation, not the final four-reviewer gate,
full Snap parity approval, or approval of later rollup changes.

## Source versus evidence

Tested source is **14f1f7696344b7d24e9f968b02a0b6f2cf747651**, reviewed against
**98939b4dd875efbcda6dd53ba769ad25dec2b4f1**. Testing used an immutable `git archive`
export at `/tmp/wago-mature-review-stage5-14f1`, plus only this review's two
`mature_review_stage5*_test.go` files. All 3,919 pinned `src` blobs were checked
against their Git blob IDs after the probes were installed. `provenance.json`
records the pin; `SHA256SUMS` records probes and evidence. The evidence commit is
the commit introducing this directory (reported separately in the handoff); it
is deliberately **not** the tested runtime source. Concurrent commits can be its
ancestors without being qualified by this review.

Read local AGENTS, `/tmp/brief-wago-rfwgc-port-resume.txt`, the launch addendum,
stage5-mature-runtime README, stage4-mirror/mature-policy, and stage4-mature
handoff. The newest owner instruction supersedes the brief's old tabled-PR
exclusion: compatible 997/1003/1019/1020/1026/1027/1030 plus the foreign-stack fix
belong in the combined port. None of those integrations is qualified by this pin.
The existing worktree's dirty files and concurrent TinyGo changes were not edited
or used in the tests. No new branch/worktree, delegation, production edit, PR,
push, benchmark, or timing experiment was performed.

## Independently constructed adversaries

- `TestMatureReviewStage5ExtentTransactions`: 600 deterministic transactions on a
  65,552-byte shared heap. A model using literal fixture class sizes and known
  occupied intervals predicts first-fit placement, class rounding and absorbed
  tails. Mixed old/LOS requests include 65, 129, 3,000, 4,080, 4,097 and 8,208
  bytes; transactions mix reuse and bump allocation, cancellation and exhaustion.
  After each transaction, an independent interval oracle requires exact coverage
  of `[0,bump)` by known live extents plus indexed/pending holes, with neither
  overlaps nor gaps. It independently intersects known physical extents with
  pages and checks live/LOS counts, zero reservations and bounded metadata.
  It does not derive physical size from `handleAllocSize` or descriptor counters.
- `TestMatureReviewStage5MixedNativeCheckedCycles`: 80 complete retirement/reuse
  cycles. Publish one native nursery object through the actual state protocol,
  retain it only through a checked old parent's remembered field, then make a
  checked 8,208-byte LOS allocation. Check batch invalidation/epoch change,
  full/minor/full/minor preservation of both payloads, nonmoving LOS offsets,
  exact descriptor/extent accounting, full root-drop reclamation and no duplicate
  retirement. This simulates native publication, not generated machine code;
  the separate guest suite exercises generated code.
- `TestMatureReviewStage5MixedDestinationFailure` (wagodebug): six destinations,
  two reused slots below a live anchor and four bump slots. Inject destination
  and precommit failure after 0, 1, 2, 4 and 5 successful hook visits. Check exact
  source handle, nursery byte and Eden metadata preservation, complete reverse
  rollback via the extent oracle, required refusal, successful retry, all six
  payloads and cleared refusal state.
- `TestMatureReviewStage5PartialRetirementRetry` (wagodebug): five cross-page LOS
  extents, three dead with two live separators. Fail reconciliation after the
  first successful insertion, require two remaining debt records and no epoch
  publication, then reconcile by allocating into a reclaimed hole before the
  next full collection. Verify exact remaining extents and final reclamation.

These augment, rather than rename, the pinned stage4/5 tests. The collector suite
also reruns metadata failure after 128 destinations, late high-card/global-root
rollback, optional/required refusal, partial final page, zero-promotion credit,
20,001-cycle churn, native invalidation and ABI/footprint controls.

## Audit conclusions at the source pin

Paths below are relative to `src/core/runtime/gc/native`; line references refer
to the tested source, not a later worktree head.

| Area | Conclusion and evidence |
| --- | --- |
| Lazy bounds | `mature_regions.go:82-99` uses uint64 end/ceiling arithmetic, geometric growth capped by configured pages, and no descriptor allocation until placement. Default `alloc` bypasses at line 120; nil guards cover release, commit, cancel, retirement and verification. Existing partial-page and oversize probes pass. Host Go OOM is not recoverable admission. |
| Exact physical accounting | `throughput_alloc.go:175` reconstructs class-rounded bytes and the absorbed 16-byte tail. `mature_regions.go:72` charges every page intersection. The new independent extent model supplements the production verifier, which otherwise shares that size helper. Page sums alone would not detect all same-page overlaps; the allocator interval proof is essential. |
| Shared capacity | Old and LOS remain in one allocator and one limit. Descriptor capacity adds metadata, not payload credit or a separate LOS allowance. LOS tenure does not reallocate or charge again. |
| Prepare/commit | Both promotion planners reserve actual extents, run all recoverable destination/precommit hooks, then commit descriptors at `collect.go:523/706` before non-reentrant source copy/handle publication. No recoverable error path intervenes in that publication phase. Checked LOS uses a transaction even outside debug builds. |
| Reverse cancellation | `throughput_alloc.go:209` distinguishes spans below the transaction bump from bump allocations. Reverse frees restore reused spans; the snapshot restores bump/backing. The new mixed failure matrix checks both kinds in one transaction. Metadata capacity may remain after abort, within its cap. |
| Refusal state | `mature_regions.go:24-44` clears prior cause at admission begin, preserves first cause and prevents optional admission from latching required refusal. Existing optional/required tests and new retry probes pass. These fields describe the latest **mature admission**, not every collection: plan/pre-reconciliation failures and paths that need no admission do not necessarily create/reset that diagnostic attempt. This is narrower than Slipstream whole-collection diagnostics, not evidence of whole-attempt parity. |
| Full retirement | `collect.go:36` runs mature reconciliation after Eden/card cleanup. `mature_regions.go:182-203` commits epochs only after all pending debt is reconciled. It never frees a page independently of object spans. New partial-reconciliation/reallocation probes preserve neighbors and demonstrate retry without lost debt/double free. |
| Native lifetime/ABI | Descriptors are Go-owned and private; no descriptor address enters handles, Spaces or NativeCollectorView. Old/large still share contiguous Go-owned backing. Allocation restores the previous backing slice on abort; collection defers view refresh, successful checked LOS refreshes it, and ForcePromote refreshes after backing length change. Existing native ABI/offset tests pass. Native LOS batches remain disabled in region mode. |

## Sibling cross-check and parity boundaries

Read-only source audit used Slipstream
`82d546679bab4887acfab056f6ad773a969b1e03:gc/native.goose`, specifically
`ng_attempt_begin`/`ng_refuse` (253-273), `ng_admit` (978 onward), and
`ng_evacuation` (1146 onward). Its admission still requires bytes plus reservation
workers times block size, discounts each run by largest-object slack, and treats
zero bytes and optional refusal separately. Wago's serial exact-span reservations
are a different valid ownership proof; these tests do not qualify parallel run
credit arithmetic, helper failure/drain or source-equivalent attempt diagnostics.

Starshine `e075c47e61b01ab9e4fd0ae4a7ca47b755e3fca1`, read through
`docs/wiki/wasm-gc-core-boundary.md` in the local starshine-master-base repository,
provides model/codec/validation/authoring surfaces rather than this native mature
allocator. No equivalent descriptor/transaction implementation was identified
there. There is no verified finding needing severity/trigger/repro/fix triage.

**Optional mature evacuation is absent.** Testing an optional admission flag is
not testing an evacuation driver, source-region exclusion, successful relocation,
or safe optional fallback of a live mature graph. Keep that port gate open.
Also unqualified here: 256 MiB source-sized live-chain capacity, generated LOS
range barriers as a dedicated adversary, source top-group admission parity,
non-amd64 execution, cached artifacts, TinyGo/foreign-stack changes, external
host borrow/exit lifetimes, later authorized PR integration and full corpus
reconciliation. Final three-Daybreak plus one-Claude review remains later.

## Validation and performance scope

`validate.sh` runs probes, all collector packages, selected AMD64 backend tests
and `^TestGC` guest tests in ordinary, wagodebug and wago_regalloccheck with Go
1.27.1, GOMAXPROCS=4. `exits.txt`, `results.json` and the twelve raw logs record
outcomes, top-level pass counts and skips. Failure injection is supported only
in wagodebug; the two debug probes are build-excluded in other modes, not passed
there. Existing diagnostic/profile/dead-charge skips remain explicit in results.
This is not a zero-skip full-root test gate.

Owned changes are tests and documentation only. Test files are excluded from
shipped builds and these documents are not runtime input, so this commit cannot
affect shipped runtime performance. No measurement lease was used and no
performance claim is made. Go test's incidental durations are runner output,
not benchmark samples.
