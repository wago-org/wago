# Wago macOS main / PR #802 comparison

Main: `da456bc1c00c89e42cbfec54d4d93300d8601d85`. PR: `ba13d00926a70976c5be5f6375a244e0e631fe5f`.

Native arm64; 3 ABBA blocks, 6 fresh processes per revision/cohort.

Each process contributes one sample per workload. All samples are retained. See comparison-*.text for benchstat uncertainty and significance; summary.csv adds seeded 10,000-resample independent-process median-ratio bootstrap 95% intervals. Intervals overlapping zero are inconclusive, not evidence of equivalence.

## Key timing rows

| Workload | Main ns/op | PR ns/op | Change | Bootstrap 95% |
|---|---:|---:|---:|---|
| BenchmarkSharingZeroLocals/256 | 8175.00 | 8741.50 | +6.93% | [-2.96%, +7.88%] |
| BenchmarkExec/tiny.add | 10.43 | 10.22 | -2.06% | [-4.17%, +0.88%] |
| BenchmarkSharingExec/join | 11.46 | 11.48 | +0.22% | [-1.51%, +3.12%] |

## Aggregate timings (benchstat)

```
│ ../pr802-final-evidence/mac-final-20261004/reanalyzed/results/main-aggregates.txt │ ../pr802-final-evidence/mac-final-20261004/reanalyzed/results/pr-aggregates.txt │
                                  │                                      sec/op                                       │                          sec/op                           vs base               │
Aggregate/AllNative                                                                                      35.97µ ±  6%                                                32.42µ ± 1%   -9.87% (p=0.002 n=6)
Aggregate/MigratedSyntheticNative                                                                        34.31µ ±  3%                                                28.68µ ± 2%  -16.39% (p=0.002 n=6)
Aggregate/AllFull                                                                                        54.59µ ± 19%                                                51.77µ ± 4%   -5.17% (p=0.004 n=6)
Aggregate/AllExec                                                                                        238.8n ±  2%                                                232.6n ± 1%   -2.59% (p=0.002 n=6)
Aggregate/MigratedSyntheticExec                                                                          39.93n ±  2%                                                38.17n ± 1%   -4.39% (p=0.002 n=6)
geomean                                                                                                  3.644µ                                                      3.359µ        -7.82%
```

## Memory (bytes, median [min, max])

| Scenario / phase / metric | Main | PR | Absolute change | Change |
|---|---:|---:|---:|---:|
| memory-native-auto / process / peak_rss_bytes | 1.73097e+07 [1.71704e+07, 1.76783e+07] | 1.68755e+07 [1.65151e+07, 1.73834e+07] | -434176 | -2.51% |
| memory-native-auto / released_2 / code_bytes | 0 [0, 0] | 0 [0, 0] | +0 | n/a (zero baseline) |
| memory-native-auto / released_2 / heap_alloc | 595664 [593624, 598720] | 597852 [594016, 601560] | +2188 | +0.37% |
| memory-native-auto / released_2 / heap_inuse | 1.3353e+06 [1.21242e+06, 1.36806e+06] | 1.3312e+06 [1.20422e+06, 1.3353e+06] | -4096 | -0.31% |
| memory-native-auto / released_2 / mapped_bytes | 0 [0, 0] | 0 [0, 0] | +0 | n/a (zero baseline) |
| memory-native-auto / released_2 / rss_bytes | 1.65888e+07 [1.64659e+07, 1.69738e+07] | 1.61546e+07 [1.57942e+07, 1.66625e+07] | -434176 | -2.62% |
| memory-native-auto / retained_0 / code_bytes | 212912 [212912, 212912] | 235968 [235968, 235968] | +23056 | +10.83% |
| memory-native-auto / retained_0 / heap_alloc | 667048 [666536, 671888] | 669180 [666528, 672120] | +2132 | +0.32% |
| memory-native-auto / retained_0 / heap_inuse | 1.42131e+06 [1.29434e+06, 1.44998e+06] | 1.40902e+06 [1.27795e+06, 1.4336e+06] | -12288 | -0.86% |
| memory-native-auto / retained_0 / mapped_bytes | 851968 [851968, 851968] | 851968 [851968, 851968] | +0 | +0.00% |
| memory-native-auto / retained_0 / rss_bytes | 1.71377e+07 [1.681e+07, 1.75636e+07] | 1.63594e+07 [1.6171e+07, 1.7236e+07] | -778240 | -4.54% |
| memory-native-one / process / peak_rss_bytes | 1.59498e+07 [1.58106e+07, 1.61055e+07] | 1.58024e+07 [1.5532e+07, 1.59744e+07] | -147456 | -0.92% |
| memory-native-one / released_2 / code_bytes | 0 [0, 0] | 0 [0, 0] | +0 | n/a (zero baseline) |
| memory-native-one / released_2 / heap_alloc | 326976 [326656, 327136] | 327000 [326520, 327320] | +24 | +0.01% |
| memory-native-one / released_2 / heap_inuse | 901120 [901120, 901120] | 901120 [901120, 901120] | +0 | +0.00% |
| memory-native-one / released_2 / mapped_bytes | 0 [0, 0] | 0 [0, 0] | +0 | n/a (zero baseline) |
| memory-native-one / released_2 / rss_bytes | 1.52289e+07 [1.50897e+07, 1.53846e+07] | 1.50815e+07 [1.48111e+07, 1.52535e+07] | -147456 | -0.97% |
| memory-native-one / retained_0 / code_bytes | 212912 [212912, 212912] | 235968 [235968, 235968] | +23056 | +10.83% |
| memory-native-one / retained_0 / heap_alloc | 402384 [402064, 402544] | 402392 [401912, 402712] | +8 | +0.00% |
| memory-native-one / retained_0 / heap_inuse | 1.00762e+06 [1.00762e+06, 1.00762e+06] | 999424 [999424, 999424] | -8192 | -0.81% |
| memory-native-one / retained_0 / mapped_bytes | 851968 [851968, 851968] | 851968 [851968, 851968] | +0 | +0.00% |
| memory-native-one / retained_0 / rss_bytes | 1.59089e+07 [1.57942e+07, 1.60563e+07] | 1.56795e+07 [1.54337e+07, 1.58925e+07] | -229376 | -1.44% |
| memory-public-auto / process / peak_rss_bytes | 1.99721e+07 [1.97427e+07, 2.00704e+07] | 1.94478e+07 [1.9202e+07, 2.01851e+07] | -524288 | -2.63% |
| memory-public-auto / released_2 / code_bytes | 0 [0, 0] | 0 [0, 0] | +0 | n/a (zero baseline) |
| memory-public-auto / released_2 / heap_alloc | 1.75043e+06 [1.74184e+06, 1.75437e+06] | 1.80325e+06 [1.78698e+06, 1.81822e+06] | +52816 | +3.02% |
| memory-public-auto / released_2 / heap_inuse | 2.74842e+06 [2.68698e+06, 2.81805e+06] | 2.89178e+06 [2.87539e+06, 2.90816e+06] | +143360 | +5.22% |
| memory-public-auto / released_2 / rss_bytes | 1.97509e+07 [1.95625e+07, 1.98738e+07] | 1.93004e+07 [1.9071e+07, 2.0054e+07] | -450560 | -2.28% |
| memory-public-auto / retained_0 / code_bytes | 212912 [212912, 212912] | 235968 [235968, 235968] | +23056 | +10.83% |
| memory-public-auto / retained_0 / heap_alloc | 1.73807e+06 [1.72943e+06, 1.74551e+06] | 1.78799e+06 [1.77839e+06, 1.80194e+06] | +49924 | +2.87% |
| memory-public-auto / retained_0 / heap_inuse | 2.74022e+06 [2.64602e+06, 2.89178e+06] | 2.89178e+06 [2.84262e+06, 2.94912e+06] | +151552 | +5.53% |
| memory-public-auto / retained_0 / rss_bytes | 1.94314e+07 [1.92348e+07, 1.96936e+07] | 1.91283e+07 [1.8858e+07, 1.97263e+07] | -303104 | -1.56% |
| memory-public-auto / runtime_released / code_bytes | 0 [0, 0] | 0 [0, 0] | +0 | n/a (zero baseline) |
| memory-public-auto / runtime_released / heap_alloc | 521584 [513000, 525592] | 520160 [503368, 534600] | -1424 | -0.27% |
| memory-public-auto / runtime_released / heap_inuse | 1.26566e+06 [1.14688e+06, 1.30253e+06] | 1.28614e+06 [1.26157e+06, 1.30253e+06] | +20480 | +1.62% |
| memory-public-auto / runtime_released / rss_bytes | 1.99311e+07 [1.97263e+07, 2.0054e+07] | 1.94314e+07 [1.91857e+07, 2.01687e+07] | -499712 | -2.51% |
| memory-public-one / process / peak_rss_bytes | 1.73179e+07 [1.71213e+07, 1.73834e+07] | 1.68673e+07 [1.67772e+07, 1.71868e+07] | -450560 | -2.60% |
| memory-public-one / released_2 / code_bytes | 0 [0, 0] | 0 [0, 0] | +0 | n/a (zero baseline) |
| memory-public-one / released_2 / heap_alloc | 1.54474e+06 [1.54458e+06, 1.54506e+06] | 1.54503e+06 [1.54479e+06, 1.54543e+06] | +296 | +0.02% |
| memory-public-one / released_2 / heap_inuse | 2.26099e+06 [2.23642e+06, 2.26918e+06] | 2.26099e+06 [2.24461e+06, 2.27738e+06] | +0 | +0.00% |
| memory-public-one / released_2 / rss_bytes | 1.7154e+07 [1.69574e+07, 1.72196e+07] | 1.67363e+07 [1.66461e+07, 1.7023e+07] | -417792 | -2.44% |
| memory-public-one / retained_0 / code_bytes | 212912 [212912, 212912] | 235968 [235968, 235968] | +23056 | +10.83% |
| memory-public-one / retained_0 / heap_alloc | 1.54909e+06 [1.54894e+06, 1.54942e+06] | 1.54935e+06 [1.54913e+06, 1.54974e+06] | +264 | +0.02% |
| memory-public-one / retained_0 / heap_inuse | 2.28966e+06 [2.27738e+06, 2.33472e+06] | 2.31424e+06 [2.23642e+06, 2.36749e+06] | +24576 | +1.07% |
| memory-public-one / retained_0 / rss_bytes | 1.7023e+07 [1.67936e+07, 1.71213e+07] | 1.66216e+07 [1.65478e+07, 1.69411e+07] | -401408 | -2.36% |
| memory-public-one / runtime_released / code_bytes | 0 [0, 0] | 0 [0, 0] | +0 | n/a (zero baseline) |
| memory-public-one / runtime_released / heap_alloc | 173528 [173368, 173848] | 173792 [173552, 174192] | +264 | +0.15% |
| memory-public-one / runtime_released / heap_inuse | 745472 [745472, 745472] | 745472 [745472, 745472] | +0 | +0.00% |
| memory-public-one / runtime_released / rss_bytes | 1.73015e+07 [1.71049e+07, 1.7367e+07] | 1.68509e+07 [1.67608e+07, 1.7154e+07] | -450560 | -2.60% |
| memory-runtime-auto / process / peak_rss_bytes | 1.95953e+07 [1.93495e+07, 1.9841e+07] | 1.94806e+07 [1.88416e+07, 1.96772e+07] | -114688 | -0.59% |
| memory-runtime-auto / released_2 / code_bytes | 0 [0, 0] | 0 [0, 0] | +0 | n/a (zero baseline) |
| memory-runtime-auto / released_2 / heap_alloc | 1.41265e+06 [1.40082e+06, 1.42627e+06] | 1.41246e+06 [1.39578e+06, 1.4233e+06] | -192 | -0.01% |
| memory-runtime-auto / released_2 / heap_inuse | 2.28557e+06 [2.1463e+06, 2.31834e+06] | 2.31424e+06 [2.22822e+06, 2.42483e+06] | +28672 | +1.25% |
| memory-runtime-auto / released_2 / rss_bytes | 1.71049e+07 [1.68591e+07, 1.73507e+07] | 1.69984e+07 [1.63512e+07, 1.71868e+07] | -106496 | -0.62% |
| memory-runtime-auto / retained_0 / code_bytes | 212912 [212912, 212912] | 235968 [235968, 235968] | +23056 | +10.83% |
| memory-runtime-auto / retained_0 / heap_alloc | 1.49018e+06 [1.48491e+06, 1.49645e+06] | 1.48568e+06 [1.47186e+06, 1.49986e+06] | -4496 | -0.30% |
| memory-runtime-auto / retained_0 / heap_inuse | 2.34291e+06 [2.31834e+06, 2.47398e+06] | 2.4576e+06 [2.39206e+06, 2.62144e+06] | +114688 | +4.90% |
| memory-runtime-auto / retained_0 / rss_bytes | 1.93413e+07 [1.91365e+07, 1.96116e+07] | 1.90628e+07 [1.85795e+07, 1.94642e+07] | -278528 | -1.44% |
| memory-runtime-auto / runtime_released / code_bytes | 0 [0, 0] | 0 [0, 0] | +0 | n/a (zero baseline) |
| memory-runtime-auto / runtime_released / heap_alloc | 481416 [469432, 494976] | 481096 [464400, 492088] | -320 | -0.07% |
| memory-runtime-auto / runtime_released / heap_inuse | 1.16736e+06 [1.11411e+06, 1.22061e+06] | 1.15507e+06 [1.08954e+06, 1.16326e+06] | -12288 | -1.05% |
| memory-runtime-auto / runtime_released / rss_bytes | 1.71295e+07 [1.68919e+07, 1.73834e+07] | 1.70394e+07 [1.64004e+07, 1.72196e+07] | -90112 | -0.53% |
| memory-runtime-one / process / peak_rss_bytes | 1.69083e+07 [1.68591e+07, 1.7236e+07] | 1.68264e+07 [1.66625e+07, 1.7023e+07] | -81920 | -0.48% |
| memory-runtime-one / released_2 / code_bytes | 0 [0, 0] | 0 [0, 0] | +0 | n/a (zero baseline) |
| memory-runtime-one / released_2 / heap_alloc | 1.06246e+06 [1.06222e+06, 1.06758e+06] | 1.06373e+06 [1.06349e+06, 1.06477e+06] | +1272 | +0.12% |
| memory-runtime-one / released_2 / heap_inuse | 1.82272e+06 [1.80224e+06, 1.82682e+06] | 1.78586e+06 [1.78586e+06, 1.79405e+06] | -36864 | -2.02% |
| memory-runtime-one / released_2 / rss_bytes | 1.44179e+07 [1.43688e+07, 1.47456e+07] | 1.4336e+07 [1.41722e+07, 1.45326e+07] | -81920 | -0.57% |
| memory-runtime-one / retained_0 / code_bytes | 212912 [212912, 212912] | 235968 [235968, 235968] | +23056 | +10.83% |
| memory-runtime-one / retained_0 / heap_alloc | 1.15395e+06 [1.15371e+06, 1.15903e+06] | 1.15522e+06 [1.15498e+06, 1.15626e+06] | +1272 | +0.11% |
| memory-runtime-one / retained_0 / heap_inuse | 1.8391e+06 [1.81862e+06, 1.85139e+06] | 1.8473e+06 [1.83501e+06, 1.85139e+06] | +8192 | +0.45% |
| memory-runtime-one / retained_0 / rss_bytes | 1.6769e+07 [1.67281e+07, 1.70885e+07] | 1.66789e+07 [1.65151e+07, 1.68919e+07] | -90112 | -0.54% |
| memory-runtime-one / runtime_released / code_bytes | 0 [0, 0] | 0 [0, 0] | +0 | n/a (zero baseline) |
| memory-runtime-one / runtime_released / heap_alloc | 133096 [132856, 138224] | 134368 [134128, 135408] | +1272 | +0.96% |
| memory-runtime-one / runtime_released / heap_inuse | 675840 [663552, 679936] | 663552 [655360, 663552] | -12288 | -1.82% |
| memory-runtime-one / runtime_released / rss_bytes | 1.44343e+07 [1.43852e+07, 1.4762e+07] | 1.43688e+07 [1.42049e+07, 1.45654e+07] | -65536 | -0.45% |

## Shared-path proof

| Revision | Functions | Migrated functions | Body bytes | Migrated body bytes |
|---|---:|---:|---:|---:|
| main | 1380 | 0 | 42481 | 0 |
| pr | 1380 | 1332 | 42481 | 23445 |

## Method and limitations

- Native compile: decode/validation outside timing; backend analysis, code generation and explicit Close inside. Decoded input is reused using the existing harness. Full compile includes decode, validation, analysis, generation and Close.
- Execution: compile/instantiate outside timing; verified fixtures/semantic oracles and fixed inputs. Synthetic calls verify results every iteration. Existing corpus checks outputs before timing; its call/error-check boundary is unchanged. JSON-AS retains mutable guest state between calls; it is not a strict reset-state execution comparison.
- No compiled-output cache is used by these compile paths. Fixed-work memory tests compile/release 270 modules, then run three retain/release batches of 36 modules / 4128 functions. Runtime runner also retains 36 verified instances. GC is outside latency timing. No forced memory release or finalizer reliance.
- Native mapping capacity is reported separately from rounded payload, generated code and Go allocation totals. Current RSS uses macOS ps (KiB converted to bytes). Darwin getrusage peak RSS is bytes. RSS sampling and Go test startup are included only in separate memory diagnostics; they can affect those observations.
- Scratch peaks sum worker high-water counters: an accounting envelope, not simultaneous process RSS. Frame/spill sums are function storage totals, not simultaneously live native stack. Admission nanos are instrumented diagnostics, not production latency.
- One-worker results use GOMAXPROCS=1. Normal-policy worker/memory results use the recorded machine logical CPU count (or --auto-procs) and adaptive worker count 0. Production builds have no diagnostic/check tags; checked and stats binaries run separately.
- Compiler/corpus hashes and binary hashes are in the manifests. Benchmark code and fixture directories are copied from the frozen PR into main, identically hashed. The caller checkout is untouched. No mid-run rebase or ref update.
- Default checks: synthetic/selected corpus semantic correctness, native backend tests, scalar/trap tests, all also with allocation checks. Full repository tests require --full-tests. External-tool/conformance skips remain in logs; missing spec submodules are not automatically fetched. This validates only this Mac architecture.
- macOS has no taskset equivalent here. CPU/core placement, power and thermal noise remain possible. No power policy is changed; caffeinate inhibits idle sleep when available. --quick is a smoke run with too few processes for conclusions.

## Potential timing regressions

Rows above +5% median are listed for investigation; statistical uncertainty still applies. Use aggregate +2% and important-workload +5% as investigation thresholds, not acceptance criteria.

- BenchmarkCompileFullWorkers/many_funcs/p1-16: +9.25%, 95% [+4.38%, +32.36%].
- BenchmarkCompile/tiny: +7.01%, 95% [+2.50%, +11.87%].
- BenchmarkSharingZeroLocals/256: +6.93%, 95% [-2.96%, +7.88%].
- BenchmarkSharingNative/fallback: +5.82%, 95% [+3.48%, +10.02%].
