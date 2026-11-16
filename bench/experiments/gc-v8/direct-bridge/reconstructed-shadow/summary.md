# Fresh reconstructed shadow workload observations

Runtime 8bbb5885, probe c1730dfd. This is a NEW RUN on Intel Xeon Platinum 8573C, not restored historical raw evidence or byte-identical shadow implementation. New schema/tag `wago_gc_compact_shadow` is separate from cohort. Explicit 2 MiB nursery and two 8 MiB old semispaces; compiler range_loop+refstruct, cohort+shadow enabled, SSE/stride/shared OFF. No age2 runtime selection. No elapsed-time measurements.

All six artifacts passed SHA/checksum checks, exact 133-call allocation totals, actual GC, and native/cohort/shadow successful-collection reconciliation. Each fresh process ran 128 warmups + 5 calls. Historical artifact hashes remain identical; host and binary provenance are recorded separately.

| Case | Minor/full successes | Covered/invalid/uncovered | Prior promoted bytes/objects | Exact-root reachable bytes/objects | Shadow scratch bytes |
|---|---:|---:|---:|---:|---:|
| struct-allocation-access | 330/3 | 326/0/4 | 26,711,136/1,335,622 | 0/0 | 4,866,060 |
| array-allocation-access | 1340/57 | 1282/0/58 | 437,865,600/5,211,792 | 0/0 | 4,866,060 |
| array-fill | 1340/57 | 1282/0/58 | 437,865,600/5,211,792 | 0/0 | 4,866,060 |
| array-copy-checksum | 1/0 | 0/0/1 | 0/0 | 0/0 | 0 |
| array-loop-copy-checksum | 1/0 | 0/0/1 | 0/0 | 0/0 | 0 |
| tree-survival | 68/0 | 67/0/1 | 3,216,976/201,061 | 0/0 | 4,866,060 |

All covered valid previous-promotion cohorts in struct, array/fill and tree have zero exact-root reachable objects at the next successful minor observation. Copies have no covered follow-up intervals, so next-minor survival is unobserved, not zero. This supports testing bounded survivor retention for these specific workloads, not predicting the required budget or execution gain. Changing collection frequency can change the cohorts.

Reconstructed coverage semantics explicitly invalidate the prior range on **every full attempt, including failed full attempts**. Failed minors retain the previous range and do not commit partial observations. Valid totals exclude invalid/uncovered cycles. This differs from simply assuming historical coverage semantics or treating a failed/full-interrupted interval as zero survival. Here full failures are zero, minor failures 3 for struct/57 for array+fill, and all observed shadow intervals are valid. Triggers/failure reasons remain unavailable and are not inferred.

The extra exact-root traversal and lazy 4,866,060-byte scratch allocation invalidate timing comparisons. Scratch is zero in copies because no follow-up trace occurs. No diagnostic fields enter ordinary timing binaries. Ordinary cohort/live snapshots remain in raw data alongside shadow snapshots. Neither V8 parity nor age2 implementation correctness/performance is established by this observation.
