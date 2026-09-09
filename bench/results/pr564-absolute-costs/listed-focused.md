# Your 23 cases: longer repeat

Before is freshly measured main `a07de0973191efab1d32677eff527952c7f9cdd2`. After is `3a84fa628ac16ac5502a7de986f2fe732007583d`. Negative changes are faster. All values are medians; p values are unadjusted two-sided rank tests, not proof of equivalence.

Twelve fresh alternating pairs, 500 ms requested time. These new samples are separate from the full-suite screen. All original listed cases were selected for repetition, regardless of their screen result.

| Bounds | Benchmark | Main ns/op | After ns/op | Change | p | Samples each |
|---|---|---:|---:|---:|---:|---:|
| explicit | BenchmarkExec/coremark.coremark_run | 32827641 | 32740149 | -0.27% | 0.347 | 12 |
| signals | BenchmarkExecParallel/independent/isa_simd_i64x2.extend_high_s | 305.9 | 302.45 | -1.13% | 0.630 | 12 |
| signals | BenchmarkExecParallel/independent/isa_ctl.if_else | 1802 | 1789 | -0.72% | 0.242 | 12 |
| explicit | BenchmarkExecParallel/independent/isa_mem.store_i32_stride | 1739 | 1748.5 | +0.55% | 0.989 | 12 |
| signals | BenchmarkExecParallel/independent/float.run | 646.35 | 637.55 | -1.36% | 0.155 | 12 |
| signals | BenchmarkExecParallel/independent/json-as.deserializeN | 6059.5 | 6009.5 | -0.83% | 0.887 | 12 |
| signals | BenchmarkExecParallel/independent/isa_mem.store_i32_stride | 1700 | 1680.5 | -1.15% | 0.325 | 12 |
| signals | BenchmarkExecParallel/independent/isa_f64.sqrt | 21749 | 21288.5 | -2.12% | 0.514 | 12 |
| signals | BenchmarkCompileCompact/fib_rec | 14385.5 | 14283.5 | -0.71% | 0.378 | 12 |
| explicit | BenchmarkExecParallel/independent/isa_i64.rotl | 2223.5 | 2216 | -0.34% | 0.899 | 12 |
| signals | BenchmarkExecParallel/independent/isa_simd_i8x16.shr_s | 696 | 689.1 | -0.99% | 0.755 | 12 |
| explicit | BenchmarkExecParallel/process/float.run | 549 | 546.05 | -0.54% | 0.854 | 12 |
| signals | BenchmarkExecParallel/independent/isa_simd_i64x2.shl | 160.5 | 161.25 | +0.47% | 0.944 | 12 |
| signals | BenchmarkPluginExec/wasm3 | 23017639.5 | 23370330.5 | +1.53% | 0.932 | 12 |
| signals | BenchmarkExecParallel/independent/spectralnorm.run | 83797.5 | 83628.5 | -0.20% | 0.932 | 12 |
| signals | BenchmarkExecParallel/independent/matmul.run | 14975 | 14946 | -0.19% | 0.810 | 12 |
| signals | BenchmarkExecParallel/independent/nbody.step | 35191 | 35336.5 | +0.41% | 0.319 | 12 |
| explicit | BenchmarkExecParallel/process/isa_simd_f32x4.abs | 227.85 | 227.25 | -0.26% | 0.417 | 12 |
| signals | BenchmarkExecParallel/independent/arith.run | 226.45 | 225.55 | -0.40% | 0.977 | 12 |
| explicit | BenchmarkExecParallel/independent/isa_ctl.br_table | 3012.5 | 3078.5 | +2.19% | 0.514 | 12 |
| signals | BenchmarkExecParallel/independent/isa_ctl.br_table | 3154.5 | 3201.5 | +1.49% | 0.410 | 12 |
| signals | BenchmarkExecParallel/process/isa_bulk_mem.copy_fwd_64 | 329.65 | 327.65 | -0.61% | 0.640 | 12 |
| signals | BenchmarkPluginInstantiate/ruby | 1906399.5 | 1695779 | -11.05% | 0.002 | 12 |
