# Your 23 cases: full-suite samples

Before is freshly measured main `731e95ff2cda7309eaf6d956f1417066bf7f1b69`. After is `b4f2360f517641803249c7ed998f37d8b0ec82a2`. Negative changes are faster. All values are medians; p values are unadjusted two-sided rank tests, not proof of equivalence.

Six pairs, 100 ms requested time. Bounds labels recover the two otherwise duplicate benchmark names from the earlier report.

| Bounds | Benchmark | Main ns/op | After ns/op | Change | p | Samples each |
|---|---|---:|---:|---:|---:|---:|
| explicit | BenchmarkExec/coremark.coremark_run | 32760005.5 | 32669483.5 | -0.28% | 0.394 | 6 |
| signals | BenchmarkExecParallel/independent/isa_simd_i64x2.extend_high_s | 283.65 | 291.45 | +2.75% | 0.290 | 6 |
| signals | BenchmarkExecParallel/independent/isa_ctl.if_else | 1702.5 | 1687 | -0.91% | 0.818 | 6 |
| explicit | BenchmarkExecParallel/independent/isa_mem.store_i32_stride | 1665.5 | 1676 | +0.63% | 0.818 | 6 |
| signals | BenchmarkExecParallel/independent/float.run | 573.45 | 587.15 | +2.39% | 0.132 | 6 |
| signals | BenchmarkExecParallel/independent/json-as.deserializeN | 5979.5 | 6319 | +5.68% | 0.041 | 6 |
| signals | BenchmarkExecParallel/independent/isa_mem.store_i32_stride | 1541 | 1535.5 | -0.36% | 0.660 | 6 |
| signals | BenchmarkExecParallel/independent/isa_f64.sqrt | 21697.5 | 21664 | -0.15% | 0.699 | 6 |
| signals | BenchmarkCompileCompact/fib_rec | 15282 | 14490.5 | -5.18% | 0.009 | 6 |
| explicit | BenchmarkExecParallel/independent/isa_i64.rotl | 1978 | 1999 | +1.06% | 0.394 | 6 |
| signals | BenchmarkExecParallel/independent/isa_simd_i8x16.shr_s | 577.65 | 599.7 | +3.82% | 0.394 | 6 |
| explicit | BenchmarkExecParallel/process/float.run | 567.15 | 558.8 | -1.47% | 0.589 | 6 |
| signals | BenchmarkExecParallel/independent/isa_simd_i64x2.shl | 158.5 | 156.85 | -1.04% | 0.310 | 6 |
| signals | BenchmarkPluginExec/wasm3 | 22680709.5 | 22550154.5 | -0.58% | 0.937 | 6 |
| signals | BenchmarkExecParallel/independent/spectralnorm.run | 88430.5 | 85655 | -3.14% | 0.093 | 6 |
| signals | BenchmarkExecParallel/independent/matmul.run | 16967.5 | 16064.5 | -5.32% | 0.394 | 6 |
| signals | BenchmarkExecParallel/independent/nbody.step | 37010.5 | 35991.5 | -2.75% | 0.818 | 6 |
| explicit | BenchmarkExecParallel/process/isa_simd_f32x4.abs | 236.75 | 226.95 | -4.14% | 0.180 | 6 |
| signals | BenchmarkExecParallel/independent/arith.run | 205.75 | 200.4 | -2.60% | 0.132 | 6 |
| explicit | BenchmarkExecParallel/independent/isa_ctl.br_table | 2869.5 | 2810 | -2.07% | 0.589 | 6 |
| signals | BenchmarkExecParallel/independent/isa_ctl.br_table | 2898.5 | 2782 | -4.02% | 0.589 | 6 |
| signals | BenchmarkExecParallel/process/isa_bulk_mem.copy_fwd_64 | 395.4 | 335.4 | -15.17% | 0.818 | 6 |
| signals | BenchmarkPluginInstantiate/ruby | 1828200 | 1681074.5 | -8.05% | 0.002 | 6 |
