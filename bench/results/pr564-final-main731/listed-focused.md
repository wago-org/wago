# Your 23 cases: longer repeat

Before is freshly measured main `731e95ff2cda7309eaf6d956f1417066bf7f1b69`. After is `b4f2360f517641803249c7ed998f37d8b0ec82a2`. Negative changes are faster. All values are medians; p values are unadjusted two-sided rank tests, not proof of equivalence.

Twelve fresh alternating pairs, 500 ms requested time. These new samples are separate from the full-suite screen. All original listed cases were selected for repetition, regardless of their screen result.

| Bounds | Benchmark | Main ns/op | After ns/op | Change | p | Samples each |
|---|---|---:|---:|---:|---:|---:|
| explicit | BenchmarkExec/coremark.coremark_run | 33766523.5 | 33764954 | -0.00% | 0.378 | 12 |
| signals | BenchmarkExecParallel/independent/isa_simd_i64x2.extend_high_s | 290.05 | 288.8 | -0.43% | 0.767 | 12 |
| signals | BenchmarkExecParallel/independent/isa_ctl.if_else | 1660.5 | 1644.5 | -0.96% | 0.671 | 12 |
| explicit | BenchmarkExecParallel/independent/isa_mem.store_i32_stride | 1745.5 | 1737 | -0.49% | 0.514 | 12 |
| signals | BenchmarkExecParallel/independent/float.run | 616.9 | 632.55 | +2.54% | 0.291 | 12 |
| signals | BenchmarkExecParallel/independent/json-as.deserializeN | 7019 | 7031.5 | +0.18% | 0.291 | 12 |
| signals | BenchmarkExecParallel/independent/isa_mem.store_i32_stride | 1714.5 | 1713.5 | -0.06% | 0.921 | 12 |
| signals | BenchmarkExecParallel/independent/isa_f64.sqrt | 21823.5 | 21757 | -0.30% | 0.319 | 12 |
| signals | BenchmarkCompileCompact/fib_rec | 15033 | 14456 | -3.84% | 0.002 | 12 |
| explicit | BenchmarkExecParallel/independent/isa_i64.rotl | 2267.5 | 2263.5 | -0.18% | 0.619 | 12 |
| signals | BenchmarkExecParallel/independent/isa_simd_i8x16.shr_s | 675.2 | 680.2 | +0.74% | 0.401 | 12 |
| explicit | BenchmarkExecParallel/process/float.run | 633.9 | 628 | -0.93% | 0.977 | 12 |
| signals | BenchmarkExecParallel/independent/isa_simd_i64x2.shl | 178.55 | 179.4 | +0.48% | 0.799 | 12 |
| signals | BenchmarkPluginExec/wasm3 | 23167829.5 | 23501930.5 | +1.44% | 0.799 | 12 |
| signals | BenchmarkExecParallel/independent/spectralnorm.run | 96155.5 | 98573 | +2.51% | 0.178 | 12 |
| signals | BenchmarkExecParallel/independent/matmul.run | 17747.5 | 17915.5 | +0.95% | 0.630 | 12 |
| signals | BenchmarkExecParallel/independent/nbody.step | 41235.5 | 41075 | -0.39% | 0.291 | 12 |
| explicit | BenchmarkExecParallel/process/isa_simd_f32x4.abs | 270.45 | 273.95 | +1.29% | 0.640 | 12 |
| signals | BenchmarkExecParallel/independent/arith.run | 219.65 | 219.1 | -0.25% | 0.325 | 12 |
| explicit | BenchmarkExecParallel/independent/isa_ctl.br_table | 3018 | 2690.5 | -10.85% | 0.378 | 12 |
| signals | BenchmarkExecParallel/independent/isa_ctl.br_table | 3237.5 | 3252.5 | +0.46% | 0.989 | 12 |
| signals | BenchmarkExecParallel/process/isa_bulk_mem.copy_fwd_64 | 350.25 | 352.1 | +0.53% | 0.755 | 12 |
| signals | BenchmarkPluginInstantiate/ruby | 1867957.5 | 1648693 | -11.74% | <0.001 | 12 |
