# Untimed compact cohort observations

Underlying runtime `6237226a`; core diagnostic `38c4fb7a` plus qualification `f5b7960b` (local cherry-picks 49f1eae9/b4170f4f). Probe source a7c7f42b. Build tags `wago_gc_compact_range_loop,wago_gc_compact_refstruct,wago_gc_compact_cohort`: stride/shared checks OFF. Explicit 2 MiB nursery and two 8 MiB old semispaces (18 MiB arena plus reserved null region and metadata). Diagnostic traversal is deliberately extra work: no speed or pause measurements are reported.

All six exact artifacts pass hashes, every exported checksum, full 133-call allocation counts, actual collection and equality of native/core successful collection counters. Each fresh process executes 128 warmups + 5 following calls. Initial/after-warmup/final snapshots are retained in raw JSON. No forced collection changes the natural graph.

## Successful minor collections (all 133 calls)

Physical live young bytes are promoted by this promote-all collector. Ratios below are totals over successful collection events, not unique lifetime cohorts. Failed attempts are excluded.

| Case | Minor/full successes | Minor/full failures | Young allocated bytes | Young live/promoted bytes | Live fraction |
|---|---:|---:|---:|---:|---:|
| struct-allocation-access | 330/3 | 3/0 | 689,475,872 | 27,105,248 | 3.9313% |
| array-allocation-access | 1340/57 | 57/0 | 2,808,390,480 | 459,356,320 | 16.3566% |
| array-fill | 1340/57 | 57/0 | 2,808,390,480 | 459,356,320 | 16.3566% |
| array-copy-checksum | 1/0 | 0/0 | 2,097,072 | 144 | 0.0069% |
| array-loop-copy-checksum | 1/0 | 0/0 | 2,097,072 | 144 | 0.0069% |
| tree-survival | 68/0 | 0/0 | 142,606,336 | 3,253,856 | 2.2817% |

## Successful full collections

| Case | Full events | Sum old occupancy bytes | Sum traced old live bytes | Sum young live bytes |
|---|---:|---:|---:|---:|
| struct-allocation-access | 3 | 25,072,416 | 0 | 146,256 |
| array-allocation-access | 57 | 466,496,320 | 0 | 12,645,280 |
| array-fill | 57 | 466,496,320 | 0 | 12,645,280 |
| array-copy-checksum | 0 | 0 | 0 | 0 |
| array-loop-copy-checksum | 0 | 0 | 0 | 0 |
| tree-survival | 0 | 0 | 0 | 0 |

Struct and array/fill show zero reachable old bytes at each observed successful full collection, despite substantial old occupancy. This supports investigating bounded survivor space rather than immediately promoting every live young object, but does NOT establish the age at which objects die, the required survivor budget, or a speedup. A shadow lifetime trace or separately qualified survivor experiment is needed. The root arrays may be replaced between workload calls; no broader application generalization is justified.

Tree and copy cases have no successful full collection in this 133-call observation, so their old survival is **unobserved**, not zero. Oversized allocation objects/bytes and oversized old live objects/bytes are zero in every workload. Minor failures are recorded separately (3 struct, 57 each array/fill); full failures are zero. The diagnostic does not record failure reason or collection trigger, so do not infer one from counter alignment.

The minor totals count only completed collections: remaining uncollected nursery bytes and nursery work handled by full collections are not included. Full and minor old occupancy totals repeatedly sample the heap, not unique allocated bytes. Native generic copied/promoted/scan counters remain uninstrumented; use the dedicated cohort fields for measured physical live/promoted bytes. No V8 comparison or target claim follows.
