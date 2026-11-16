# Matched single entry-clear removal results

Candidate `fadea926` (production change `49cded96`) versus base `35faf6c9`. Only four entry bitmap clears are removed; unchanged unconditional deferred clears remain. No sparse cleanup, new policy, cache or constructor change. Prior correctness qualification is recorded separately on the candidate; this report does not replace that matrix or hide its known baseline admission exclusions.

Both isolated builds used identical direct mark-cleanup benchmark source from `c591378b`, identical existing full-tracing graphs and identical compact traffic harness. Harness SHA-256 files and four binary hashes retained. Direct binaries have no feature tags; compact binaries both enable Eden+age2 and compare each runtime policy against itself. Go1.27.1, Linux x86-64 Xeon8573C, GOMAXPROCS1, same allowedCPU0 pin. Own local jobs paused, not proven-exclusive physical host. Per-process UTC/load/affinity retained.

38/38 audit processes and190/190 timed processes passed, none missing: direct two audit processes with21graphs and10timed processes; compact36audits and180timed processes. Each direct timed subbenchmark uses500ms; compact uses128fixedepochs. Five alternating/reversed revision processes per cell; compact cases/policies rotate. Graph/counter audits passed; exact compact allocation/minor/full/failed-minor counts match revisions in every case, as do all memory budgets. All measurements report zero Go bytes/op and allocations/op.

Do not infer broad application performance from empty collections: direct Minor promotes its graph once, then measures empty-young collection with old roots. Full graphs retain/retrace their live objects each iteration. Compact workloads include allocations, barriers and automatic collection; dense control forces one Full per epoch. No runtime/Wasm/V8 comparison is made.

## Direct process-level medians

ns/op. Five process repetitions per graph; subbenchmarks within a shared process are correlated, not independent cross-workload experiments. Ratios below1 favor candidate.

| Graph | Control ns | Candidate ns | Ratio | Median paired ratio | Five paired ratios |
|---|---:|---:|---:|---:|---|
| BenchmarkMarkCleanupGraphs/small/live0/fullfalse | 118.7 | 65.5 | 0.552× | 0.559× | 0.559, 0.550, 0.559, 0.450, 0.566 |
| BenchmarkMarkCleanupGraphs/small/live0/fulltrue | 2885.0 | 2556.0 | 0.886× | 0.910× | 0.910, 1.022, 0.969, 0.814, 0.886 |
| BenchmarkMarkCleanupGraphs/small/live1/fullfalse | 122.0 | 73.0 | 0.598× | 0.596× | 0.568, 0.597, 0.596, 0.601, 0.586 |
| BenchmarkMarkCleanupGraphs/small/live1/fulltrue | 2656.0 | 2603.0 | 0.980× | 0.991× | 1.007, 0.971, 0.986, 0.991, 1.020 |
| BenchmarkMarkCleanupGraphs/small/live64/fullfalse | 123.7 | 71.4 | 0.578× | 0.589× | 0.589, 0.608, 0.577, 0.595, 0.575 |
| BenchmarkMarkCleanupGraphs/small/live64/fulltrue | 5286.0 | 5302.0 | 1.003× | 1.004× | 1.004, 1.021, 0.985, 0.999, 1.028 |
| BenchmarkMarkCleanupGraphs/small/live4096/fullfalse | 122.7 | 71.9 | 0.586× | 0.585× | 0.572, 0.585, 0.590, 0.459, 0.609 |
| BenchmarkMarkCleanupGraphs/small/live4096/fulltrue | 172544.0 | 174032.0 | 1.009× | 1.018× | 1.022, 0.994, 1.018, 0.868, 1.043 |
| BenchmarkMarkCleanupGraphs/production/live0/fullfalse | 7139.0 | 3620.0 | 0.507× | 0.504× | 0.497, 0.338, 0.546, 0.514, 0.504 |
| BenchmarkMarkCleanupGraphs/production/live0/fulltrue | 97381.0 | 93306.0 | 0.958× | 0.958× | 0.913, 0.804, 0.958, 0.961, 0.964 |
| BenchmarkMarkCleanupGraphs/production/live1/fullfalse | 7314.0 | 3668.0 | 0.502× | 0.488× | 0.470, 0.445, 0.506, 0.578, 0.488 |
| BenchmarkMarkCleanupGraphs/production/live1/fulltrue | 97432.0 | 95838.0 | 0.984× | 0.967× | 0.986, 0.920, 0.967, 1.155, 0.940 |
| BenchmarkMarkCleanupGraphs/production/live64/fullfalse | 7114.0 | 3577.0 | 0.503× | 0.505× | 0.499, 0.466, 0.513, 0.506, 0.505 |
| BenchmarkMarkCleanupGraphs/production/live64/fulltrue | 100731.0 | 97614.0 | 0.969× | 0.951× | 0.975, 0.918, 0.972, 0.951, 0.938 |
| BenchmarkMarkCleanupGraphs/production/live4096/fullfalse | 7217.0 | 3622.0 | 0.502× | 0.505× | 0.500, 0.519, 0.502, 0.505, 0.556 |
| BenchmarkMarkCleanupGraphs/production/live4096/fulltrue | 276071.0 | 288751.0 | 1.046× | 1.036× | 1.008, 1.036, 1.009, 1.079, 1.130 |
| BenchmarkFullTracingShapes/scalarStruct | 13037.0 | 13119.0 | 1.006× | 0.991× | 0.972, 0.983, 1.051, 1.043, 0.991 |
| BenchmarkFullTracingShapes/scalarArray | 13567.0 | 13337.0 | 0.983× | 0.996× | 0.975, 0.999, 0.996, 1.000, 0.950 |
| BenchmarkFullTracingShapes/refStruct | 18498.0 | 18874.0 | 1.020× | 1.019× | 1.021, 1.006, 1.019, 1.032, 0.981 |
| BenchmarkFullTracingShapes/refArray | 107382.0 | 109392.0 | 1.019× | 1.019× | 0.980, 1.005, 1.024, 1.258, 1.019 |
| BenchmarkFullTracingShapes/denseRefArray | 143035.0 | 144252.0 | 1.009× | 0.983× | 0.979, 0.891, 1.034, 1.245, 0.983 |

## Compact same-policy medians

ns/epoch,128epochs per process. This is candidate/control within each policy, not comparing policies to declare a new collector winner.

| Graph/policy | Control ns | Candidate ns | Ratio | Median paired ratio | Five paired ratios |
|---|---:|---:|---:|---:|---|
| BenchmarkEdenTraffic/promote/dead-heavy | 1382444.0 | 1387436.0 | 1.004× | 1.004× | 1.004, 1.035, 1.040, 0.979, 0.702 |
| BenchmarkEdenTraffic/age2/dead-heavy | 1507741.0 | 1522881.0 | 1.010× | 1.000× | 0.966, 1.101, 1.022, 0.969, 1.000 |
| BenchmarkEdenTraffic/eden/dead-heavy | 1469969.0 | 1505043.0 | 1.024× | 1.004× | 1.004, 0.827, 1.024, 1.030, 0.998 |
| BenchmarkEdenTraffic/promote/persistent344KiB | 1380428.0 | 1349524.0 | 0.978× | 0.979× | 0.990, 0.972, 0.966, 0.994, 0.979 |
| BenchmarkEdenTraffic/age2/persistent344KiB | 1498068.0 | 1503862.0 | 1.004× | 0.987× | 0.987, 0.962, 0.963, 1.032, 1.021 |
| BenchmarkEdenTraffic/eden/persistent344KiB | 1475435.0 | 1466304.0 | 0.994× | 0.996× | 1.032, 0.996, 0.956, 0.962, 1.011 |
| BenchmarkEdenTraffic/promote/array-bulk | 1321979.0 | 1301159.0 | 0.984× | 0.977× | 0.979, 0.949, 0.935, 1.003, 0.977 |
| BenchmarkEdenTraffic/age2/array-bulk | 1418829.0 | 1363978.0 | 0.961× | 0.952× | 1.023, 0.959, 0.952, 0.932, 0.946 |
| BenchmarkEdenTraffic/eden/array-bulk | 1324558.0 | 1282728.0 | 0.968× | 0.974× | 0.936, 0.988, 0.974, 0.947, 1.025 |
| BenchmarkEdenTraffic/promote/rolling84x4096 | 1573944.0 | 1604853.0 | 1.020× | 1.020× | 0.964, 1.041, 1.002, 1.020, 1.026 |
| BenchmarkEdenTraffic/age2/rolling84x4096 | 1860306.0 | 1820436.0 | 0.979× | 0.967× | 1.010, 0.967, 0.952, 1.021, 0.940 |
| BenchmarkEdenTraffic/eden/rolling84x4096 | 1643646.0 | 1636064.0 | 0.995× | 0.988× | 0.985, 0.988, 1.013, 0.979, 0.996 |
| BenchmarkEdenTraffic/promote/dense-persistent | 1651694.0 | 1656224.0 | 1.003× | 0.993× | 0.951, 0.993, 0.987, 0.998, 1.058 |
| BenchmarkEdenTraffic/age2/dense-persistent | 1804763.0 | 1800418.0 | 0.998× | 1.004× | 1.004, 0.894, 1.349, 0.973, 1.084 |
| BenchmarkEdenTraffic/eden/dense-persistent | 1768698.0 | 1795599.0 | 1.015× | 0.983× | 1.015, 0.824, 0.958, 1.037, 0.983 |
| BenchmarkEdenTraffic/promote/old-young-mutation | 3022338.0 | 3015097.0 | 0.998× | 0.998× | 0.994, 0.900, 0.998, 1.027, 1.016 |
| BenchmarkEdenTraffic/age2/old-young-mutation | 3206562.0 | 3267234.0 | 1.019× | 1.003× | 1.058, 0.995, 0.988, 1.003, 1.038 |
| BenchmarkEdenTraffic/eden/old-young-mutation | 3183484.0 | 3188564.0 | 1.002× | 0.993× | 1.036, 0.854, 1.005, 0.993, 0.886 |

## Exact compact counts per128epochs

Both revisions match each row in all five repetitions. Memory accounting remains28,463,575bytes with280-byte collector structures; no memory saving follows from deleting a clear instruction.

| Policy/workload | Allocations | Minor | Full | Failed Minor |
|---|---:|---:|---:|---:|
| promote/dead-heavy | 8388736 | 128 | 0 | 0 |
| age2/dead-heavy | 8388736 | 256 | 0 | 0 |
| eden/dead-heavy | 8388736 | 170 | 0 | 0 |
| promote/persistent344KiB | 8388736 | 128 | 0 | 0 |
| age2/persistent344KiB | 8388736 | 256 | 0 | 0 |
| eden/persistent344KiB | 8388736 | 170 | 0 | 0 |
| promote/array-bulk | 262144 | 514 | 0 | 0 |
| age2/array-bulk | 262144 | 1028 | 0 | 0 |
| eden/array-bulk | 262144 | 686 | 0 | 0 |
| promote/rolling84x4096 | 262144 | 490 | 21 | 21 |
| age2/rolling84x4096 | 262144 | 1523 | 0 | 0 |
| eden/rolling84x4096 | 262144 | 676 | 6 | 6 |
| promote/dense-persistent | 8388736 | 128 | 128 | 0 |
| age2/dense-persistent | 8388736 | 256 | 128 | 0 |
| eden/dense-persistent | 8388736 | 128 | 128 | 0 |
| promote/old-young-mutation | 8388736 | 128 | 0 | 0 |
| age2/old-young-mutation | 8388736 | 258 | 0 | 0 |
| eden/old-young-mutation | 8388736 | 170 | 0 | 0 |

## Verdict

The expected narrow benefit appears in empty-young direct Minor: production-size medians are about0.50× control after removing one of two full bitmap clears. This is not a live-object tracing or application throughput win. Production4096-live Full instead regresses4.6% by ratio of medians; every paired round is slower (1.008,1.036,1.009,1.079,1.130×), median paired ratio1.036×. Preserve this dense negative rather than promote from empty-collection improvements. Smaller direct Full/tracing graphs are mixed.

Compact allocation-driven workloads show mostly small mixed effects; age2 bulk improves to0.961× while promote rolling regresses to1.020×. Several pair directions change, and occasional control movement is large. This does not establish a broad throughput gain. The code-level removal is valid under the qualified mark ownership invariant, but semantics alone do not prove performance improvement; code-layout/cache effects and host noise cannot be excluded. Investigate or repeat the dense regression before any source promotion. No automatic promotion, default-runtime regression-free claim or V8 target claim follows.
