# Dedicated Eden core results

Qualified collector source `b893312c`; harness `f2606161`; one same-source binary with `wago_gc_compact_eden,wago_gc_compact_age2`. Go 1.27.1, Linux x86-64 Xeon Platinum 8573C. Runtime constructor chooses policy. Binary hash, process UTC/load/affinity and raw outputs retained. This is a core policy comparison, not default production runtime, native compiler, Wasm/V8 or TinyGo performance.

24/24 one-epoch audits and 120/120 timed processes passed (five rotating repetitions per 24 policy/workload cells), none missing. Each timed process measures eight epochs. All exact allocation deltas, root validity, retained payload IDs and dense reference edges checked successfully. The Go benchmark framework also runs a one-epoch calibration invocation on a separate fresh collector before its eight-epoch measurement; both appear in logs. Those calibration counters are not pooled with measured counters. Go allocations measured zero for every case.

The original five-shape run is retained in `initial-persistent-control`; it used a single persistent 344 KiB object and is NOT recurring-prefix evidence. The six-shape run below is entirely fresh, with rolling retention added and the persistent object explicitly relabeled. No samples are spliced.

## Equal-total-young primary comparison

Medians of five process samples. Throughput is logical objects/second; policy ratios use ns/epoch, lower is better. Each policy has 2 MiB total young and two 8 MiB old spaces.

| Workload | Promote ms | Age2 ms | Eden ms | Age2/promote | Eden/promote | Eden Mobjects/s |
|---|---:|---:|---:|---:|---:|---:|
| dead-heavy | 1.615 | 1.695 | 2.133 | 1.049× | 1.321× | 30.721 |
| persistent344KiB | 1.681 | 1.750 | 1.771 | 1.041× | 1.053× | 37.016 |
| array-bulk | 1.650 | 1.652 | 1.618 | 1.002× | 0.981× | 1.266 |
| rolling84x4096 | 1.871 | 2.017 | 2.133 | 1.078× | 1.140× | 0.960 |
| dense-persistent | 2.304 | 2.275 | 2.153 | 0.987× | 0.934× | 30.442 |
| old-young-mutation | 3.670 | 3.550 | 3.089 | 0.967× | 0.842× | 21.215 |

## Separate equal-fresh-capacity sensitivity

Eden 2 MiB + two 256 KiB survivors: total young 2.5 MiB (+25%), arena about 18.5 vs 18 MiB (+2.8%). Not an equal-memory competitor; never pool with the primary table.

| Workload | Same-fresh Eden ms | /promote | /equal-total Eden |
|---|---:|---:|---:|
| dead-heavy | 2.048 | 1.268× | 0.960× |
| persistent344KiB | 1.572 | 0.936× | 0.888× |
| array-bulk | 1.500 | 0.909× | 0.927× |
| rolling84x4096 | 1.829 | 0.978× | 0.857× |
| dense-persistent | 2.345 | 1.018× | 1.089× |
| old-young-mutation | 3.803 | 1.036× | 1.231× |

## Collection counts and process spread

Counts per measured eight-epoch process. Collection counts were invariant across the five repetitions of each cell. Failed Minor means capacity preflight returned OOM and the harness retried Full; it is not a lost allocation or test failure. Dense control explicitly Full-collects once per epoch.

| Workload | Policy | Successful Minor | Full | Failed Minor | Five ns/epoch samples |
|---|---|---:|---:|---:|---|
| dead-heavy | promote | 8 | 0 | 0 | 1477226, 1615383, 2644794, 1497026, 1837078 |
| dead-heavy | age2 | 16 | 0 | 0 | 1837525, 1574132, 1449384, 1695223, 1817867 |
| dead-heavy | eden | 10 | 0 | 0 | 2344665, 2286231, 1632606, 1379119, 2133311 |
| dead-heavy | eden-same-fresh | 8 | 0 | 0 | 2048191, 2170546, 1666468, 2432338, 1595684 |
| persistent344KiB | promote | 8 | 0 | 0 | 1194135, 1238080, 2008863, 1765560, 1680599 |
| persistent344KiB | age2 | 16 | 0 | 0 | 1510208, 3969100, 1413375, 1862773, 1749951 |
| persistent344KiB | eden | 10 | 0 | 0 | 1404561, 4522388, 1839400, 1770505, 1366311 |
| persistent344KiB | eden-same-fresh | 8 | 0 | 0 | 1458170, 1572276, 2150718, 1471920, 1679356 |
| array-bulk | promote | 32 | 0 | 0 | 2353640, 1559000, 1649527, 1483234, 1684103 |
| array-bulk | age2 | 64 | 0 | 0 | 3103762, 2326090, 1652451, 1395401, 1528501 |
| array-bulk | eden | 42 | 0 | 0 | 4302423, 1617976, 1586400, 1520063, 2821204 |
| array-bulk | eden-same-fresh | 32 | 0 | 0 | 5261382, 1544065, 1246681, 1223283, 1499916 |
| rolling84x4096 | promote | 30 | 1 | 1 | 1635158, 1870634, 2083919, 1839497, 3398937 |
| rolling84x4096 | age2 | 94 | 0 | 0 | 1832590, 2016708, 1812931, 2093732, 2851664 |
| rolling84x4096 | eden | 42 | 0 | 0 | 16262276, 1529709, 2133289, 2186494, 1763889 |
| rolling84x4096 | eden-same-fresh | 31 | 0 | 0 | 4204632, 1583178, 1829198, 2020573, 1673646 |
| dense-persistent | promote | 8 | 8 | 0 | 6045526, 2304028, 2577025, 2027294, 1609146 |
| dense-persistent | age2 | 16 | 8 | 0 | 3209910, 1771035, 2275127, 2272086, 2312862 |
| dense-persistent | eden | 8 | 8 | 0 | 1686769, 1644916, 2286784, 2190692, 2152838 |
| dense-persistent | eden-same-fresh | 8 | 8 | 0 | 2345476, 2023668, 1918207, 3383311, 3170074 |
| old-young-mutation | promote | 8 | 0 | 0 | 3324989, 3075082, 4543198, 3669787, 6843265 |
| old-young-mutation | age2 | 16 | 0 | 0 | 8241133, 3540475, 3549537, 3779955, 3341579 |
| old-young-mutation | eden | 10 | 0 | 0 | 7274098, 2998158, 3300129, 2996291, 3089220 |
| old-young-mutation | eden-same-fresh | 8 | 0 | 0 | 8735032, 3803405, 3487895, 4702945, 3249598 |

## Memory budgets

| Policy | Total young | Maximum fresh | Survivor cap | FixedBytes | Accounted total | Collector struct |
|---|---:|---:|---:|---:|---:|---:|
| promote | 2097152 | 2097152 | 0 | 28463151 | 28463575 | 280 |
| age2 | 2097152 | 1048576 | 524288 | 28463151 | 28463575 | 280 |
| eden | 2097152 | 1572864 | 262144 | 28463151 | 28463575 | 280 |
| eden-same-fresh | 2621440 | 2097152 | 262144 | 29253679 | 29254103 | 280 |

Collector struct is 280 bytes in all policies, including the common enabled feature-state fields. Accounted total includes descriptors/collector backing, not allocator size-class overhead or RSS. The constructor cap means different things: age2 caps retained content within shared semispaces; Eden caps each dedicated survivor space.

## Interpretation

Rolling retention is the intended architectural control: an old 84-slot root points to the latest 84 scalar arrays of 4096-byte total stride (344064 bytes young live); payload IDs are verified. This approximates recurring retention, not exact reproduction of a Wasm artifact. Across eight epochs promote-all runs 30 successful minors plus one failed minor and one Full; age2 runs 94 minors; equal-total Eden runs 42 minors; equal-fresh Eden runs 31. All survivor policies avoid Full in this short run. No copied/promoted-byte counters were collected, so do not infer exact traffic volumes.

Despite reducing age2 collection frequency, equal-total Eden is 1.140× promote-all in rolling throughput and 1.321× in dead-heavy in this run. Mutation improves to 0.842× and dense to 0.934×; bulk and persistent controls are mixed. Equal-fresh Eden is 0.978× promote-all for rolling but consumes more memory; that small difference is not a robust gain claim. The original control run and fresh run also show variation, so treat these short millisecond-scale samples as exploratory. Repeat longer matched runs with host telemetry before any promotion. No overall architectural throughput win or V8 parity is established.

### Additional-memory breakdown

Measured `FixedBytes` rises from **28,463,151 to 29,253,679 bytes** (+790,528 bytes), and accounted total from **28,463,575 to 29,254,103 bytes** (+790,528 bytes, about 2.78%). This is more than the raw additional 524,288-byte arena. The constructor's capacity formulas account for the difference as +524,288 arena, +65,536 start map, +65,536 dirty map, +4,096 mark bitmap and +131,072 queue bytes. This component decomposition is derived from the inspected allocation formulas; the aggregate totals are measured harness metrics. Remembered storage, descriptors and the common 280-byte collector structure are unchanged. The approximately 2.8% arena increase alone must not be used as a substitute for total-memory accounting.
