# Collector rollup: 1030 then 1019

Status: integrated pending final gates; final combined gates belong to the parent.
No INCLUDED disposition, PR closure, publication or performance claim is made.

The latest owner order supersedes the two launch briefs' historical exclusion
of tabled PRs. Source selection is the published common base
27abf4470e1137c18233d246e05dee6fb76ef266 to the exact heads below, not the older
port baseline or unrelated main ancestry. Existing mature production
14f1f7696344b7d24e9f968b02a0b6f2cf747651 and native region production
98939b4dd875efbcda6dd53ba769ad25dec2b4f1 remain the adaptation contracts.

## 1030

Source fba8e2b50ea0cbc31089789dd27dde628c55fe9d. Applied only its two
collector files from the actual PR diff. The fallback retains an independent
pending array's free handle and byte ownership; without such a reservation it
runs ordinary compaction, retaining handleless dead-charge high-water only when
needed. This includes the final published promotion/reclamation correction.
Region chunk cancellation, ownership checks, epochs, mature admission and
reclamation order are unchanged. No older tuning body replaced this behavior.

The original eight checked/unchecked, chunk/struct, 512/40960-byte cases and
no-array promoted reclamation regression are retained verbatim. Added policy
0/6/12 repeated partial publication, small/LOS/small, cancellation/refill,
region bounds, native LOS refusal with checked equivalent, payload/verification,
metadata invalidation and double Close coverage. Existing full collector and
focused guest suites supply dead charges, trap/reentry, promotion failure and
mature descriptor rollback controls.

## Validation method

`validate.py PHASE` archives a recorded HEAD, overlays only this owner's
collector directory, hashes every collector Go source, and runs correctness
suites on that stable export. Other workers' uncommitted files are excluded.
Each phase runs all collector packages, focused AMD64 backend, `^TestGC` guest,
and race-enabled native collector in ordinary, wagodebug and wago_regalloccheck.
Raw logs and commands/exits accompany the source manifest. Runner durations
are stripped; no benchmark or timing measurement is performed. Scratch archives
are removed after execution. Go 1.27.1 and matching GOROOT are explicit.

Two 1030 harness preflights are retained: the first hit mise trust before build;
the second inherited a mismatched GOROOT. These are setup failures, not tests.
No toolchain installation or repository trust mutation was needed.

Full-root, conformance, corpus, platform execution, combined rooting/TinyGo,
1020 and final consolidated reviews remain parent gates. Existing interval
register diagnostic failures are not selected or relabeled green here.

## Deferred prerequisites

1019 must be reconciled with future helper-written marks before enabling 1020:
all marks must be covered either by retained queue entries or a merged exclusive
high-water. Helper-local queues cannot disappear without publishing their maximum
handle; join/failure/drain paths must all preserve this bound before reset.
Serial queued, full, nursery and captured nursery paths are required controls.
No helpers are implemented by this cluster.

Owner R9 design requirement for 1020 (reported from Slipstream
`dcb45b563:hunt-findings.md`, H1-F1; independent audit belongs to parent): low-32-bit futex aliases can lose a 64-bit
ticket or STOP wake between full-width recheck and wait. Repeated full-width
checks alone are insufficient. The future protocol must close that window and
test preemption before syscall with publication/wake before wait, including STOP.
The parent owns the R9 audit and any resulting production adaptation.

Read-only #1022 applicability: landed commit
703b47d3e40b55837279363a2917c03d37a74ea4 adds the tinySweep state guard to
validateStoredRef's scan-handle rejection. Current code lacks it, while
tinySweepActive also holds during tinyRootsSweepBarrier marking. A live gray
partially scanned object can therefore be rejected as pending reclamation.
Its original regression is TestTinySweepBarrierPublishesPartiallyScannedArray.
This generic Tiny publication semantic is relevant when Tiny EH integration
reaches the same publication path; no claim is made that every EH fixture
triggers it. Recommend separate explicit prerequisite commit and gates after
parent coordination. No #1022 production change is included here. PR1003 remains
unassigned; PR1020 waits for both collector and rooting clusters.

Read-only sibling comparison: Slipstream 82d546679bab4887acfab056f6ad773a969b1e03
uses address-indexed native marking/admission, not Wago's Tiny shared scan cursor
or independent stable-handle reservation ledgers. Starshine
e075c47e61b01ab9e4fd0ae4a7ca47b755e3fca1 documents model/codec/validation surfaces,
not this native collector. Neither supplies an equivalent patch. No new sibling
defect is inferred and no sibling was modified.

## Measurement handoff

READY TO MEASURE only by an independent measurer holding the global lease, after
parent correctness coordination. Branch: experiment/rfwgc-port. Production
commits are reported in the handoff and discoverable as the two integration
commits introducing this directory and its 1019 update; each phase's manifest
pins actual tested sources without self-referential commit hashes.

Sensitive paths: native_alloc.go cancellation fallback; mark.go full/nursery
reset and drains; barrier.go remembered/card cleanup; survivor.go completed
minor cleanup. Suggested workloads: partially consumed native batches with
independent small/large arrays and promotion; cancel/refill and host trap churn;
sparse high-identity full marking after large historical handle capacity;
queued-root interruption followed by minor/full; dense/sparse survivor promotion;
repeated high-card old-to-young and global/table updates; failed promotion/LOS
admission and successful retry. Pair default policy with region shifts 6/12,
retained/all-dead/low-survival controls and native/checked allocation paths.
No measurements were taken by this implementation worker.

1030 result: all twelve commands passed. Ordinary/regalloccheck collector runs
passed 428 top-level tests; wagodebug passed 458 with three existing dead-charge
skips. Backend and guest profile/diagnostic skips remain listed in summary.json;
they are not green coverage claims. All three race runs passed. The archive base
and collector hashes in 1030/source.json identify the tested snapshot; subsequent
shared-branch rooting/TinyGo commits are not implied by these results.

## 1019

Source b6cb0e829011ab08d98d64eaefbc0085c34a0284, including published queued-mark
repair 4cbaa605ab5084b0a24070709ae6baf8fdf46bff. Applied only the actual PR's
collector hunks; upstream ARCHITECTURE/CHANGELOG cleanup is not imported.
markHighWater covers drained full, nursery and captured nursery marks; reset
also accounts for undrained queues, including old handles discarded by a nursery
reset after root enumeration panic. Close resets the bound. The field occupies
existing padding; native view/handle ABI is unchanged.

Complete evacuation clears remembered owners' cardSlot heads once, then resets
shared scalar card lists, slot bitmap bits, fallback/cache state and native card
view. General clearCardMetadata still walks owners. finishMinorCardMetadata
uses this fast cleanup only when no young handle remains; no cleanup was moved
across promotion failure or descriptor admission. CollectFull, mature retirement,
transactional publication and region chunk cancellation are unchanged.

All four published tests remain, parameterized across policy 0/6/12. Additional
slot/direct panic-root variants come from verified local
0bef34b13d51a2d941ed4b2167fb5a8b58e47dff, also parameterized across all three
policies. They assert queued parent/unmarked child before the intervening minor
reset, then retain the graph through full collection. The high-identity reuse
and all serial/captured drain cases remain, as do repeated global/table card
reuse across 63/64 and 4096 boundaries.

New wagodebug controls cover destination/precommit refusal across policy 0/6/12:
exact source handles/bytes/remembered owners survive refusal, cards and persistent
roots remain usable, successful retry promotes children and clears cards. Region
6/12 large admission tests inject metadata, backing and handle-publication
failure with an outstanding small reservation, require zero descriptor debt and
restored backing, then retry into the original extent. Existing stage5 full
collector tests still cover late multiple-destination rollback, high LOS cards,
metadata-growth failure, retirement failure/retry and zero-promotion credit.

The #1022 recommendation above is source inspection, not an executed reproduction
or an instruction to expand this cluster. Its one-condition production guard and
original test should be integrated only under the parent's separate assignment.
No #1003 extent guard, #1020 helper machinery, R9 fix, unrelated main commit,
foreign-stack edit or rooting-worker source is part of these collector commits.

Concurrent commits may appear in their ancestry. 1030 validation exported
ca30843801956bae67d5cd1219f66d191518c125; 1019 validation exported
9bfe98360e6ff5e1ab2f6605efc216d2c441fb37, each with only its recorded collector
overlay. Thus the latter includes committed rooting prerequisites in its archive,
but neither run claims to validate later dirty or committed integration work.

1019 result: all twelve commands passed. Ordinary/regalloccheck collector runs
passed 433 top-level tests; wagodebug passed 465 with the same three existing
dead-charge skips. All three race runs passed. Backend/guest existing skips are
listed in 1019/summary.json. The increased backend/guest counts versus 1030 also
reflect committed rooting prerequisites in the later archive; they are not
attributed to collector changes. Collector source hashes were rechecked after
execution with no changes. No new production defect was found during these
bounded integration gates. Combined final gates and final dispositions remain
pending with the parent.
