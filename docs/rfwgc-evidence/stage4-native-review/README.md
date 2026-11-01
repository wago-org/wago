# Independent intermediate native-region review

No new correctness finding verified in production range
`5b31c81ec0c74695a90bc8d36754b92845b59f14..98939b4dd875efbcda6dd53ba769ad25dec2b4f1`.
This is a bounded intermediate review, not approval of the full Snap port or a
replacement for the final three-Daybreak-plus-Claude fan. Two baseline diagnostic
failures remain explicitly red below. No production fix is proposed by this review.

## Source isolation and ownership

Read both launch briefs, local AGENTS, stage3-status, stage4-mirror (including
mature-policy and host-and-next-stages), stage4-native and stage4-mature handoffs.
All execution used a `git archive` export of exact production
`98939b4dd875efbcda6dd53ba769ad25dec2b4f1`, plus the three uniquely named
`native_review_stage4*` probes. The baseline diagnostic control used a complete
archive of `5b31c81ec`, not an overlay of one source file into newer production.
`provenance.json` records exact commits, archive hash and SHA256 of all six
production files changed in that range. These six files are also precisely the
production changes in 98939b4dd itself. Concurrent mature-worker changes were not
used. Archive directories and cross-compiled binaries are ignored local scratch;
no alternate checkout, branch switch or Git worktree was created.

Owned changes are this evidence directory and the three test files. Shared
stage2 corpus scripts, port log, conformance submodules and other workers' files
were preserved. The review commit is named in the final handoff; the tested source
snapshot above is distinct from the later shared-branch evidence commit.

## Audit conclusions

- **Reservation/publication:** `native_regions.go:7` admits bounded non-spanning
  chunks only in supported region throughput configurations. Allocation size is
  bounded before Align16; handle capacity is prepared before region byte ownership
  is committed. No collection occurs during preparation. `native_alloc.go`
  cancellation retains consumed identities, returns free unused identities and
  clears Count/advances epoch before reentry. Chunk ownership mismatch retains
  capacity rather than reclaiming another interval. Zero-use cancellation restores
  the pre-alignment bump; partial cancellation retains the initialized extent.
- **Mixed writers and bounds:** AMD64 `gc_direct.go` uses the chunk cursor/end
  only when direct capacity is zero. Structs check the actual large threshold;
  arrays now check that threshold on the chunk path as well. Bounds checks precede
  subtraction and payload writes. The publication tail contains no helper call,
  fallback or safepoint between initialization, cursor/metadata writes and handle
  cursor commit. Refill/helper paths cancel prior state before allocating. Large
  region arrays and dead batches remain checked. Baseline structs still advance
  the global bump even when sharing an array batch's handles, with conservative
  tail retention when that global bump has advanced beyond the chunk.
- **Metadata:** accepted nonempty AddTypes invalidates region reservations after
  validation and subtype construction, before refreshed view publication. Empty
  and rejected appends do not invalidate a usable reservation. Generated readers
  reload mutable handle/type backing; array state has epoch checks, struct state
  uses serialized Count/Cursor invalidation. Independent repeated failed/accepted
  appends verified these distinctions and zero-use alignment recovery.
- **Traps and roots:** `gc_array_globals.go:350` cancels the batch on error before
  reconciling global roots, without deleting published handles. Existing tests
  exercise escaping traps and nested host activation cleanup. New probes retain
  a pre-trap global and, separately, catch a nested host trap then allocate 1000
  objects before resuming the still-live outer caller frame. Neither global nor
  caller value was lost. Success after trap is tested on the same instance.
- **Platform/source contract:** ARM64 `gc_array_alloc.go` already checks threshold,
  bounded chunk capacity and Count/Cursor before mutation. It has no separate
  epoch load; like AMD64 structs it relies on serialized cancellation clearing
  Count. Thus the prior native handoff's broad statement that array paths check
  epochs should be read as AMD64-specific. ARM64 has no live native struct writer;
  dead writers remain blocked by zero direct capacity. Offsets, sizes and ABI 9
  are unchanged. Cross-compilation is recorded separately from execution.
- **Artifacts:** `api.go:3791` implements Load as Compile of Wasm bytes. Retired
  native-code loading and public artifact codecs are rejected/absent by executed
  tests. Internal same-build code caches retain the existing view ABI. There is
  no tested public path for loading older generated machine code into the new
  runtime. This is not a cross-version binary interoperability claim.

The review preserves Wago's stable handles and Go-owned backing. It introduces
no allocation-policy or resident-capacity change and imports neither parked
C1/C2 nor tabled Wago PR work.

## Independently devised adversaries

`native_review_stage4_test.go` places a live 24-byte checked object before a
16-aligned chunk, then performs 100 duplicate-ID rejections and valid descriptor
appends. It checks rejected reservations byte-for-byte, epoch change on success,
return of the eight-byte alignment gap, idempotent cancellation, exact payload
and final live count. It does not simulate native publication stores.

`native_review_stage4_amd64_test.go` builds a fresh module alternating 40 arrays
and structs, with both globals live. Native off/on and region shifts 0/6/10/12
form eight controls. Three rounds cover lengths 0, 1, 9, 15, 16, 17, 27, 28,
63, 64, 65, 1024, UINT32_MAX, then successful reuse. The threshold is 128 bytes,
so 27/28 straddle aligned large-object classification. Successful results must
be exactly length+39 and charge exactly 81 allocations. Failed huge allocation
must preserve the preceding global value 55 through explicit collection. Native
publication is witnessed in the 4096-byte-region case. These assertions use
known guest semantics, not the allocator's extent calculations as the oracle.

`native_review_stage4_reentry_amd64_test.go` catches an inner host trap and resumes
allocation while its outer guest caller still owns a live struct. Twenty cycles
in each of baseline/64-byte/1024-byte-region policies preserve the caller value,
clear trapped region reservations, drain host depth and force collection. It
reuses an existing module fixture but changes the adversary to a swallowed trap
rather than an error escaping every activation.

## Executed results

| Gate | Ordinary | wagodebug | wago_regalloccheck |
| --- | --- | --- | --- |
| Full collector package subtree | PASS | PASS | PASS |
| Focused region/native guest selector plus mixed probe | PASS | PASS | PASS |
| Final three independent probes and artifact rejection tests | PASS | PASS | PASS |
| Broad diagnostic backend selector | 2 baseline failures | 2 baseline failures | 2 baseline failures |
| Full-baseline diagnostic control | Same 2 failures | Same 2 failures | Same 2 failures |

Linux ARM64 backend and wago test binaries both cross-compile (ordinary mode);
neither was executed. Collector runs retain three existing debug dead-charge
skips; the broad backend keeps its existing policy/profile skips. Exact names
are in results.json. The focused guest runs and final probes/artifact checks
have no skips. Counts include subtests and are not independent experiments.

## Retained baseline diagnostics

Broad `wago_codegenstats` backend commands deliberately retain `Region` in the
selector. Both snapshot and complete baseline fail in all three modes:

- `interval_region_test.go:140`, `TestIntervalRegionDynamicReuse`: activation
  counter `interval-region = 0`, expected 1.
- `interval_region_test.go:214`,
  `TestIntervalRegionControlBoundaries/if-else`: Candidates/Activations remain 0
  despite the expected control-region activation. The `call` subtest passes.

These compile scalar fixtures, not GC constructors. The baseline is the entire
pre-native production snapshot, so their baseline classification does not depend
on selectively restoring gc_direct.go. Raw failing logs and nonzero exit codes
are retained. No production repair, test deletion, expected-failure wrapper or
selector narrowing converts these broad commands to green. They remain a parent
qualification issue; they are not new native-port regressions established here.

Read-only sibling cross-check used Slipstream
`82d546679bab4887acfab056f6ad773a969b1e03` (`gc/native.goose` ng_admit,
ng_collect_owned, ng_allocate) and Starshine
`e075c47e61b01ab9e4fd0ae4a7ca47b755e3fca1`
(`docs/wiki/wasm-gc-core-boundary.md`). Slipstream has heap publication/admission
rather than Wago's Go interval-register diagnostics; Starshine's documented
surface is model/codec/validation/authoring. Searches for these diagnostic names
in their relevant source trees found no equivalent. No sibling bug is inferred,
and no new verified runtime finding requires a severity/fix triage entry.

## Reproduction and limits

On a fresh evidence directory run `bash prepare.sh` to export both pinned
revisions and copy the three probes. Existing exports are deliberately not
overwritten. Run `bash validate.sh`, then
`bash validate-extra.sh`. Use the explicit Go 1.27.1 path in those scripts.
`exits.txt`, `extra-exits.txt`, `results.json` and raw logs retain the actual scope.
One fixture correction is retained in `*-probe-fixture-preflight.txt`: the
new caught-trap test initially required FullCollections > 0. Baseline policy
correctly used minor collections; the final assertion requires the sum of minor
and full collections > 0. All payload/trap assertions already passed. No
production behavior was changed for this correction.

Printed Go test durations are incidental correctness-run output, not timing
samples. No benchmark or performance measurement was taken.

This review does not qualify ARM64 execution, full-root/conformance/corpus gates,
TinyGo interruption diagnosis, concurrent mature implementation, parallel parity,
or final review closure. The caught-trap fixture covers live caller roots, not
all borrowed host views, retained results, or process-exit paths. Debug failure
hooks are covered by the existing collector suite, not new injected native
instruction failures (there is no safepoint within publication).

Tests and evidence cannot affect shipped runtime/compiler performance: test
files are excluded from production builds and docs are not executable inputs.
Production 98939b4dd remains subject to independent measurement under the global
lease after correctness workers drain. No PR, push, delegation or Claude review
was performed by this worker.
