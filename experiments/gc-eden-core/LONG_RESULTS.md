# 128-epoch affinity-controlled Eden confirmation

Same exact binary SHA-256 `1a407c2e8bb1bad859e6c7750f3843baac51fcb55acbe51d4affea011793066a` as the eight-epoch run: qualified collector `b893312c`, harness `f2606161`, Go 1.27.1, Eden+age2 tags. No runtime.GC or source changes. Protocol changes are 128 measured epochs and taskset affinity to allowed CPU 0. Our own local jobs were paused; this was **not an exclusive or proven-stable host**. External contention/frequency effects cannot be excluded. Per-process UTC, load average, allowed/selected affinity and CPU model remain in raw evidence. Do not pool absolute timing samples with the previous unpinned eight-epoch run.

24/24 one-epoch audits and 120/120 timed processes passed, none missing. Five rotating fresh-process repetitions per cell; analyze five process-level ns/epoch samples, not 640 independent epochs. All allocation deltas, root/payload/edge checks and memory budgets passed. Zero measured Go allocations. The benchmark framework one-epoch calibration uses a separate fresh collector and is excluded from the exact measured counts below.

## Equal-total-young primary results

All three policies have 2 MiB young backing and two 8 MiB old spaces. Values are median ms/epoch across five processes.

| Workload | Promote | Age2 | Eden | Age2/promote | Eden/promote |
|---|---:|---:|---:|---:|---:|
| dead-heavy | 1.661 | 1.749 | 1.885 | 1.053× | 1.135× |
| persistent344KiB | 1.627 | 1.897 | 1.810 | 1.166× | 1.113× |
| array-bulk | 1.544 | 1.840 | 1.691 | 1.192× | 1.095× |
| rolling84x4096 | 1.994 | 2.318 | 1.993 | 1.162× | 0.999× |
| dense-persistent | 2.019 | 2.154 | 2.291 | 1.067× | 1.135× |
| old-young-mutation | 3.597 | 3.744 | 3.714 | 1.041× | 1.033× |

## Separate equal-fresh-capacity sensitivity

This policy adds two 256 KiB survivors to 2 MiB fresh Eden: 2.5 MiB total young.

| Workload | Same-fresh Eden ms | /promote |
|---|---:|---:|
| dead-heavy | 1.726 | 1.039× |
| persistent344KiB | 1.804 | 1.109× |
| array-bulk | 1.728 | 1.119× |
| rolling84x4096 | 2.012 | 1.009× |
| dense-persistent | 2.254 | 1.116× |
| old-young-mutation | 3.936 | 1.094× |

## Exact counts and process samples

Exact counts are parsed from the final 128-epoch receipt, not reconstructed from rounded benchmark metrics. Counts are invariant across five repeats. Full counts for dense-persistent are explicit workload controls; failed Minor is the preflight OOM that triggers Full retry.

| Workload | Policy | Objects | Minor | Full | Failed Minor | Five process ns/epoch samples |
|---|---|---:|---:|---:|---:|---|
| dead-heavy | promote | 8388736 | 128 | 0 | 0 | 1735024, 1660865, 1796261, 1612102, 1644366 |
| dead-heavy | age2 | 8388736 | 256 | 0 | 0 | 1780490, 1783828, 1749287, 1663870, 1739292 |
| dead-heavy | eden | 8388736 | 170 | 0 | 0 | 2036922, 1933480, 1884778, 1763554, 1681051 |
| dead-heavy | eden-same-fresh | 8388736 | 128 | 0 | 0 | 1689026, 1726251, 1851456, 1710408, 1844983 |
| persistent344KiB | promote | 8388736 | 128 | 0 | 0 | 1606092, 1633263, 1627068, 1684366, 1586788 |
| persistent344KiB | age2 | 8388736 | 256 | 0 | 0 | 1706989, 2048021, 1731458, 2063821, 1897310 |
| persistent344KiB | eden | 8388736 | 170 | 0 | 0 | 2190673, 1810274, 1952784, 1629963, 1739247 |
| persistent344KiB | eden-same-fresh | 8388736 | 128 | 0 | 0 | 1780020, 1701073, 2322558, 1803899, 1824359 |
| array-bulk | promote | 262144 | 514 | 0 | 0 | 1544104, 1747528, 1479864, 1627894, 1512854 |
| array-bulk | age2 | 262144 | 1028 | 0 | 0 | 1583064, 1648595, 1907166, 1840311, 1903242 |
| array-bulk | eden | 262144 | 686 | 0 | 0 | 1551573, 1600923, 1744799, 1691198, 1725507 |
| array-bulk | eden-same-fresh | 262144 | 514 | 0 | 0 | 1965712, 1728123, 1823445, 1565281, 1671162 |
| rolling84x4096 | promote | 262144 | 490 | 21 | 21 | 1927737, 2248247, 1940962, 2184684, 1994411 |
| rolling84x4096 | age2 | 262144 | 1523 | 0 | 0 | 2317552, 2218567, 2233941, 2399401, 2349070 |
| rolling84x4096 | eden | 262144 | 676 | 6 | 6 | 1972124, 2279906, 2301543, 1963091, 1992984 |
| rolling84x4096 | eden-same-fresh | 262144 | 506 | 5 | 5 | 1897659, 2427700, 2011584, 2201070, 1971890 |
| dense-persistent | promote | 8388736 | 128 | 128 | 0 | 1859923, 2019064, 2489143, 2377701, 1992041 |
| dense-persistent | age2 | 8388736 | 256 | 128 | 0 | 2088521, 2248848, 2154477, 3214481, 1990511 |
| dense-persistent | eden | 8388736 | 128 | 128 | 0 | 2266438, 2310242, 2461767, 2276198, 2290964 |
| dense-persistent | eden-same-fresh | 8388736 | 128 | 128 | 0 | 1918152, 2256518, 2253596, 2387216, 2024436 |
| old-young-mutation | promote | 8388736 | 128 | 0 | 0 | 3505898, 3597200, 4247651, 3742169, 3518474 |
| old-young-mutation | age2 | 8388736 | 258 | 0 | 0 | 4215362, 3603728, 3744350, 4111083, 3552216 |
| old-young-mutation | eden | 8388736 | 170 | 0 | 0 | 3715767, 3714261, 3631541, 4196757, 3504994 |
| old-young-mutation | eden-same-fresh | 8388736 | 128 | 0 | 0 | 3935564, 3976516, 4962189, 3770669, 3442641 |

## Interpretation

Recurring rolling retention shows the architectural tradeoff clearly. Across 128 epochs: promote-all has 490 successful Minor and 21 Full collections; age2 has 1523 Minor and zero Full; equal-total Eden has 676 Minor and six Full; equal-fresh Eden has 506 Minor and five Full. Both Eden geometries therefore DO eventually need Full collections on this workload; the earlier eight-epoch zero-Full observation was a short-run result, not a lasting property. No copied/promoted-byte counters were measured, so collection counts must not be presented as exact byte traffic.

Equal-total Eden matches promote-all rolling median within 0.1% (not a demonstrated gain) and reduces age2 collection frequency, but is 9.5–13.5% slower in dead-heavy, persistent-large-object, bulk and dense controls and 3.3% slower in old-young mutation in this run. Equal-fresh Eden has no rolling median gain (1.009× promote) despite extra memory. It also has no across-workload throughput win. These mixed/negative longer-run results do not support promotion or a general architectural speedup. Timing spread remains visible in the five samples; do not infer statistical significance or stable-host causality from a median alone.

FixedBytes remains 28,463,151 bytes for equal-total policies and 29,253,679 for equal-fresh; accounted total remains 28,463,575 versus 29,254,103 (+790,528 bytes, approximately 2.78%). This includes additional arena plus start/dirty maps, mark bitmap and queue, not merely the +524,288-byte arena. Common collector structure is 280 bytes in all four policies. Default production binary memory/performance is not measured.

This is core-only synthetic allocation/graph traffic, not the Wasm runtime or V8 suite. All original short-run evidence stays intact; the longer run supersedes neither its raw observations nor correctness results, but materially narrows the performance interpretation.
