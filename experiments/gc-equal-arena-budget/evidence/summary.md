# Equal-total arena policy sensitivity

18/18 uniform 512-call audits and 54/54 timed receipts passed. Each timed process used 100 warmups and 9 batches of 32 calls; three rotating fresh-process rounds per cell. One binary, unchanged production policy; no V8 run. All timed rows performed real collections.

Production source: `2a907dab`; test-only geometry source: `d473d430`; final runner: `8291d882`. Binary SHA256: `5510b16655f8ca84ec46b7ea8257c62d4c3b95006cca110680f38832a6b54023`. Exact seven tags are in raw provenance. Qualification `TestGCBridgeGeometry` passed.

All splits have 18 MiB usable arena plus 16-byte guard and derived/independently core-tested FixedBytes 28,463,151. Metadata includes arena alignment, start/dirty tables, mark bitmap, traversal queue and remembered queue. Excludes descriptors/Go headers/runtime roots. Equal reserved capacity is not equal committed RSS.

| Case | Young/each-old MiB | median process ms | ratio of medians | within-round ratios | timed Minor/Full | process HWM KiB |
|---|---|---:|---:|---|---|---|
|struct-allocation-access|young2-old8|4.9766|1.000|1.000, 1.000, 1.000|1275/23|39792, 40524, 39648|
|struct-allocation-access|young4-old7|5.3473|1.074|0.589, 1.087, 1.140|635/13|39712, 39476, 39596|
|struct-allocation-access|young6-old6|4.4544|0.895|0.570, 0.901, 0.895|422/10|40228, 40332, 39656|
|array-allocation-access|young2-old8|9.6915|1.000|1.000, 1.000, 1.000|2901/125|40384, 39804, 40376|
|array-allocation-access|young4-old7|8.4791|0.875|0.908, 0.808, 0.793|1441/71|39960, 40452, 39856|
|array-allocation-access|young6-old6|8.5030|0.877|0.910, 0.848, 0.805|953/55|40132, 39536, 39672|
|array-fill|young2-old8|11.3775|1.000|1.000, 1.000, 1.000|2901/125|40420, 40384, 40544|
|array-fill|young4-old7|10.0216|0.881|0.881, 0.775, 0.917|1441/71|39840, 39924, 39792|
|array-fill|young6-old6|9.3511|0.822|0.822, 0.764, 0.870|953/55|39496, 39456, 40264|
|array-copy-checksum|young2-old8|0.2240|1.000|1.000, 1.000, 1.000|3/0|18828, 19020, 18792|
|array-copy-checksum|young4-old7|0.2423|1.082|1.001, 0.959, 1.129|1/0|21372, 21148, 21100|
|array-copy-checksum|young6-old6|0.3274|1.462|1.044, 0.977, 1.536|1/0|23432, 23480, 23460|
|array-loop-copy-checksum|young2-old8|0.5671|1.000|1.000, 1.000, 1.000|3/0|18912, 18868, 19068|
|array-loop-copy-checksum|young4-old7|0.6034|1.064|0.779, 1.228, 1.298|1/0|21372, 21300, 21224|
|array-loop-copy-checksum|young6-old6|0.5557|0.980|0.834, 0.980, 1.356|1/0|23612, 23592, 23732|
|tree-survival|young2-old8|1.9883|1.000|1.000, 1.000, 1.000|293/3|39808, 40288, 39920|
|tree-survival|young4-old7|1.7498|0.880|0.935, 0.915, 0.880|146/2|39644, 39588, 39716|
|tree-survival|young6-old6|1.7111|0.861|0.957, 0.968, 0.803|97/2|39520, 39604, 39596|

## Interpretation

Larger nurseries consistently reduced Minor/Full frequency on these graphs. Array allocation, fill and tree were faster in every paired round at both larger splits. This is actionable policy sensitivity, not a general collector speedup or proof of the V8 target. Six-MiB nursery struct was also faster in every round, but the first baseline process was a conspicuous outlier; four-MiB struct was slower in two of three rounds. Copy/loopcopy results are mixed and variable; six-MiB copy ratio-of-medians 1.462 is not a stable per-round 46% regression (ratios 1.044, 0.977, 1.536). Do not select a universal default from these results.

Copy workloads show committed-memory tradeoff despite identical reserved bytes: process high-water RSS rises roughly 18,800–19,100 KiB to 23,400–23,700 KiB. Larger nursery touches more pages before collection. Reducing each old space 8→7→6 MiB also reduces maximum old live-set capacity; these six small-live-set graphs do not bound that risk. Validate a retained-old graph near 6–8 MiB before any default change.

All rows preserve exact checksums/artifact hashes and allocation counts; counters deterministic across rounds. Copied/promoted byte zeros remain uninstrumented, not evidence of no work. Process-level replications, not 27 pooled batch samples, underpin comparisons. Own local workers paused; processes unpinned; external host contention/cache/scheduling drift cannot be excluded.

## Preserved failed attempts

1. Initial runner iterated entire corpus manifest. After 9 intended audits passed, non-heap i31-roundtrip failed `collector selection mismatch`. Scope corrected to six intended GC workloads. This was not a heap semantic failure.
2. Complete corrected audit restarted with 128 calls. After 10 passed rows, young 4/old 7 copy failed `probe completed without any real collection; increase warmup`. Uniform 512-call audits then restarted from scratch. Final timed protocol unchanged; no rows spliced and no guards relaxed.

## Next validation

Repeat adjacent reversed-order pairs for array allocation/fill/tree and disputed copy/struct, using all splits and sufficiently long uniform batches. Add a controlled old-live-set pressure audit to quantify capacity loss. Any V8 comparison requires a fresh contemporaneous unchanged-artifact gate and still cannot claim matched V8 heap policy. No production promotion recommended yet.
