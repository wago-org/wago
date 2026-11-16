# Sparse mark cleanup experiment

Control `4327a2eb` plus byte-identical `marks_test.go` from candidate `c591378b`; candidate implementation `a9caca77`, correctness/benchmark additions `d32d0ed5` and `c591378b`. Both collector binaries built with Go 1.27.1, no build tags. GOMAXPROCS=1, five alternating baseline/candidate process pairs, 300ms per graph. All 160 measurements completed; all report zero B/op and allocs/op. Both complete collector test binaries passed before timing.

Small arena: 256 KiB nursery / two 256 KiB old semispaces. Production arena: 2 MiB nursery / two 8 MiB old semispaces. Live shape is one reference array with N scalar children. Full moves/traces every iteration. Minor promotes once, then measures empty-young collections with old roots, **not sustained live graph collection**. The 4096-child production graph exercises sparse cleanup whereas its small-arena counterpart uses dense fallback. No threshold tuning follows from these samples.

| Shape | Baseline median ns | Candidate median ns | Candidate/base | Baseline samples ns | Candidate samples ns |
|---|---:|---:|---:|---|---|
| production/live0/fullfalse | 8492.0 | 35.2 | 0.004× | 8713.0, 8492.0, 7295.0, 7421.0, 10710.0 | 35.18, 31.88, 41.18, 31.23, 42.36 |
| production/live0/fulltrue | 116198.0 | 113463.0 | 0.976× | 136022.0, 116198.0, 105586.0, 102311.0, 139703.0 | 88430.0, 113463.0, 113989.0, 87255.0, 151570.0 |
| production/live1/fullfalse | 8091.0 | 41.2 | 0.005× | 8091.0, 8287.0, 7518.0, 7637.0, 11921.0 | 41.16, 38.15, 41.54, 39.98, 43.25 |
| production/live1/fulltrue | 117828.0 | 93013.0 | 0.789× | 117828.0, 112724.0, 101429.0, 123658.0, 204099.0 | 90606.0, 88328.0, 105382.0, 93013.0, 95263.0 |
| production/live4096/fullfalse | 8176.0 | 42.2 | 0.005× | 7783.0, 9396.0, 8176.0, 7938.0, 8500.0 | 42.24, 40.64, 39.84, 42.94, 45.96 |
| production/live4096/fulltrue | 314379.0 | 324123.0 | 1.031× | 273249.0, 343895.0, 314379.0, 284840.0, 338825.0 | 324123.0, 260674.0, 301890.0, 329898.0, 339945.0 |
| production/live64/fullfalse | 8303.0 | 39.7 | 0.005× | 8654.0, 8917.0, 8303.0, 7923.0, 8221.0 | 40.7, 38.31, 39.73, 39.12, 46.18 |
| production/live64/fulltrue | 121578.0 | 95447.0 | 0.785× | 121251.0, 126356.0, 124307.0, 119819.0, 121578.0 | 91469.0, 92211.0, 138570.0, 95447.0, 121940.0 |
| small/live0/fullfalse | 127.1 | 36.0 | 0.284× | 137.4, 122.1, 138.3, 122.2, 127.1 | 36.88, 35.15, 36.05, 40.15, 31.68 |
| small/live0/fulltrue | 2787.0 | 2866.0 | 1.028× | 3031.0, 2744.0, 3015.0, 2656.0, 2787.0 | 3044.0, 2903.0, 2866.0, 2743.0, 2518.0 |
| small/live1/fullfalse | 133.5 | 43.9 | 0.329× | 136.2, 123.5, 133.1, 133.5, 134.7 | 40.19, 44.63, 43.36, 43.88, 45.39 |
| small/live1/fulltrue | 2790.0 | 2890.0 | 1.036× | 2776.0, 2808.0, 2947.0, 2790.0, 2736.0 | 2620.0, 2890.0, 3048.0, 2552.0, 3655.0 |
| small/live4096/fullfalse | 127.1 | 38.1 | 0.300× | 147.0, 127.1, 151.0, 126.9, 123.4 | 44.59, 37.94, 67.3, 37.71, 38.14 |
| small/live4096/fulltrue | 206891.0 | 187200.0 | 0.905× | 199295.0, 214028.0, 226229.0, 179567.0, 206891.0 | 191937.0, 178799.0, 272957.0, 171978.0, 187200.0 |
| small/live64/fullfalse | 124.2 | 42.3 | 0.340× | 122.2, 124.2, 143.7, 132.1, 123.0 | 46.25, 42.28, 47.0, 38.91, 42.24 |
| small/live64/fulltrue | 5721.0 | 5540.0 | 0.968× | 5610.0, 5845.0, 6500.0, 5721.0, 5309.0 | 6088.0, 5540.0, 8823.0, 5189.0, 5486.0 |

Production 4096-live Full regresses by 3.1% at the median; small empty/one-live Full regress by 2.8%/3.6%. Keep these negatives. Sparse empty-young Minor improves dramatically because whole-bitmap clearing disappears, but this is not a corresponding application-speedup claim. Production small-live Full improves; mixed dense results warrant caution before default promotion. These microbenchmarks do not establish V8 parity.

## Fresh integrated struct profile

Separate untimed CPU profile uses `/workspace/shared/gc-integrated-full/baseline.test`, code93c7ec34/documentation4d524db8, same full-gate binary. Direct mode, elision/age2 off, 2MiB young/two8MiB old, 128warmup plus512calls. Receipt/checksum retained. No profiled timing is used in comparisons.

Native ExternalCode51.47%; retireNurseryHeaders21.45% cumulative; stride14.48%; eachRef10.19%; collect45.04%; memclrNoHeapPointers2.68% flat. Percentages overlap and must not be summed. This build has neither retire_startmap nor retire_adaptive enabled, leaving the integrated even-slot map optimization dormant. Native ExternalCode is not symbolically attributed by this Go profile.

## Next staged experiment

Keep sparse separate pending dense-regression decision. Compare identical baseline compiler pipeline headers vs adaptive retirement on struct and tree-survival first (three rotating rounds,100warmup,9×32calls); existing historical map/adaptive work means no new algorithm implementation is needed. Only compose surviving sparse changes with the winning retirement policy once. Then run all six artifacts/V8 and compilation footprint. Keep constructor-init work isolated rather than running an uncontrolled factorial.
