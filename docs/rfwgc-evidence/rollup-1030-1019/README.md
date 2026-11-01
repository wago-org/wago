# Collector rollup: 1030 then 1019

Status: integration in progress; final combined gates belong to the parent.
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

Owner R9 design requirement for 1020: low-32-bit futex aliases can lose a 64-bit
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
