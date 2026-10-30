# Stage2 measured comparison

Each timing cell: median [min, max]; sample CV. Time unit ns/op; lower is faster. Percentages compare medians.

| Suite / fixture / option | n baseline/stage2 | Baseline ns/op [min,max]; CV | Stage2 ns/op [min,max]; CV | Stage2 vs baseline | Host B/op baseline → stage2 | Host allocs/op baseline → stage2 |
|---|---:|---|---|---:|---:|---:|
| compile / no-gc/off | 6/6 | 41797.500 [41497.000, 41928.000]; 0.37% | 42441.000 [41898.000, 42767.000]; 0.82% | +1.54% | 33168 → 33168 | 55 → 55 |
| compile / no-gc/on | 6/6 | 42650.500 [42561.000, 43026.000]; 0.39% | 44286.000 [43735.000, 44716.000]; 0.88% | +3.83% | 38064 → 38064 | 57 → 57 |
| compile / nonqualifying/off | 6/6 | 126547.500 [126133.000, 129745.000]; 1.08% | 128257.000 [127646.000, 129597.000]; 0.51% | +1.35% | 119312 → 119312 | 275 → 275 |
| compile / nonqualifying/on | 6/6 | 132045.000 [130931.000, 133253.000]; 0.62% | 142825.500 [141950.000, 143976.000]; 0.51% | +8.16% | 124982 → 125750 | 309 → 341 |
| compile / qualifying/off | 6/6 | 128385.000 [127317.000, 129972.000]; 0.70% | 129865.500 [129401.000, 136185.000]; 2.01% | +1.15% | 131072 → 131072 | 274 → 274 |
| compile / qualifying/on | 6/6 | 160469.000 [159543.000, 169893.000]; 2.47% | 352295.500 [351209.000, 354125.000]; 0.31% | +119.54% | 147496 → 299472 | 692 → 2463 |
| runtime / throughput/no-gc/on=false | 6/6 | 397.950 [397.200, 409.800]; 1.22% | 410.850 [409.200, 415.300]; 0.65% | +3.24% | 0 → 0 | 0 → 0 |
| runtime / throughput/no-gc/on=true | 6/6 | 398.900 [397.800, 407.100]; 0.88% | 419.050 [412.200, 427.000]; 1.28% | +5.05% | 0 → 0 | 0 → 0 |
| runtime / throughput/nonqualifying/on=false | 6/6 | 278.400 [276.900, 279.500]; 0.30% | 289.050 [286.600, 304.700]; 2.31% | +3.83% | 0 → 0 | 0 → 0 |
| runtime / throughput/nonqualifying/on=true | 6/6 | 278.550 [277.100, 281.800]; 0.57% | 289.650 [288.000, 296.500]; 1.12% | +3.98% | 0 → 0 | 0 → 0 |
| runtime / throughput/qualifying/on=false | 6/6 | 279.000 [277.100, 280.200]; 0.46% | 287.800 [287.000, 289.300]; 0.33% | +3.15% | 0 → 0 | 0 → 0 |
| runtime / throughput/qualifying/on=true | 6/6 | 279.200 [276.400, 286.800]; 1.62% | 276.700 [275.000, 285.900]; 1.48% | -0.90% | 0 → 0 | 0 → 0 |

Within-revision On vs Off median runtime/compile costs:

| Suite / fixture | Baseline On vs Off | Stage2 On vs Off |
|---|---:|---:|
| compile / no-gc | +2.04% | +4.35% |
| compile / nonqualifying | +4.34% | +11.36% |
| compile / qualifying | +24.99% | +171.28% |
| runtime / throughput/no-gc | +0.24% | +2.00% |
| runtime / throughput/nonqualifying | +0.05% | +0.21% |
| runtime / throughput/qualifying | +0.07% | -3.86% |

Guest accounting (separate fresh instance, exactly 1024 invocations; NOT timed). All repeated observations must agree. Totals below are exact counters reconstructed from per-op values when representable; raw Go output rounds the fractions.

| Revision / runtime cell | Charges | Handleless | Materialized / published | Layout B | Observed nursery advance B | Retained nursery B | Rounded heap request B (calculated) |
|---|---:|---:|---:|---:|---:|---:|---:|
| baseline / throughput/no-gc/on=false | 0 | 0 | 0 / 0 | 0 | 0 | 0 | 0 |
| stage2 / throughput/no-gc/on=false | 0 | 0 | 0 / 0 | 0 | 0 | 0 | 0 |
| baseline / throughput/no-gc/on=true | 0 | 0 | 0 / 0 | 0 | 0 | 0 | 0 |
| stage2 / throughput/no-gc/on=true | 0 | 0 | 0 / 0 | 0 | 0 | 0 | 0 |
| baseline / throughput/nonqualifying/on=false | 1024 | 0 | 1024 / 1024 | 24576 | 24576 | 24576 | 32768 |
| stage2 / throughput/nonqualifying/on=false | 1024 | 0 | 1024 / 1024 | 24576 | 24576 | 24576 | 32768 |
| baseline / throughput/nonqualifying/on=true | 1024 | 0 | 1024 / 1024 | 24576 | 24576 | 24576 | 32768 |
| stage2 / throughput/nonqualifying/on=true | 1024 | 0 | 1024 / 1024 | 24576 | 24576 | 24576 | 32768 |
| baseline / throughput/qualifying/on=false | 1024 | 0 | 1024 / 1024 | 24576 | 24576 | 24576 | 32768 |
| stage2 / throughput/qualifying/on=false | 1024 | 0 | 1024 / 1024 | 24576 | 24576 | 24576 | 32768 |
| baseline / throughput/qualifying/on=true | 1024 | 0 | 1024 / 1024 | 24576 | 24576 | 24576 | 32768 |
| stage2 / throughput/qualifying/on=true | 1024 | 1023 | 1 / 1 | 24576 | 24576 | 24576 | 32768 |
