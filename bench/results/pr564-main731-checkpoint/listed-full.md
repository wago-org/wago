# Your 23 cases: full-suite samples

Before is freshly measured main `731e95ff2cda7309eaf6d956f1417066bf7f1b69`. After is `1d04b458fa512265e2b93711f5c355435a29f33e`. Negative changes are faster. All values are medians; p values are unadjusted two-sided rank tests, not proof of equivalence.

Six pairs, 100 ms requested time. Bounds labels recover the two otherwise duplicate benchmark names from the earlier report.

| Bounds | Benchmark | Main ns/op | After ns/op | Change | p | Samples each |
|---|---|---:|---:|---:|---:|---:|
| explicit | BenchmarkExec/coremark.coremark_run | 33202479.5 | 32654234 | -1.65% | 0.240 | 6 |
| signals | BenchmarkExecParallel/independent/isa_simd_i64x2.extend_high_s | 287.65 | 296.6 | +3.11% | 0.394 | 6 |
| signals | BenchmarkExecParallel/independent/isa_ctl.if_else | 1669 | 1607 | -3.71% | 0.310 | 6 |
| explicit | BenchmarkExecParallel/independent/isa_mem.store_i32_stride | 1718.5 | 1748.5 | +1.75% | 0.589 | 6 |
| signals | BenchmarkExecParallel/independent/float.run | 587.15 | 557.95 | -4.97% | 0.180 | 6 |
| signals | BenchmarkExecParallel/independent/json-as.deserializeN | 6157 | 6104.5 | -0.85% | 0.699 | 6 |
| signals | BenchmarkExecParallel/independent/isa_mem.store_i32_stride | 1516.5 | 1541 | +1.62% | 0.221 | 6 |
| signals | BenchmarkExecParallel/independent/isa_f64.sqrt | 21937.5 | 22146 | +0.95% | 0.589 | 6 |
| signals | BenchmarkCompileCompact/fib_rec | 15653.5 | 14894 | -4.85% | 0.132 | 6 |
| explicit | BenchmarkExecParallel/independent/isa_i64.rotl | 1981.5 | 2008.5 | +1.36% | 0.515 | 6 |
| signals | BenchmarkExecParallel/independent/isa_simd_i8x16.shr_s | 573.15 | 575.5 | +0.41% | 0.818 | 6 |
| explicit | BenchmarkExecParallel/process/float.run | 577.4 | 566.75 | -1.84% | 0.394 | 6 |
| signals | BenchmarkExecParallel/independent/isa_simd_i64x2.shl | 157.6 | 157.05 | -0.35% | 0.974 | 6 |
| signals | BenchmarkPluginExec/wasm3 | 23405007.5 | 22472598 | -3.98% | 0.818 | 6 |
| signals | BenchmarkExecParallel/independent/spectralnorm.run | 84908 | 91699.5 | +8.00% | 0.180 | 6 |
| signals | BenchmarkExecParallel/independent/matmul.run | 15833 | 15474 | -2.27% | 0.937 | 6 |
| signals | BenchmarkExecParallel/independent/nbody.step | 36310.5 | 36875.5 | +1.56% | 1.000 | 6 |
| explicit | BenchmarkExecParallel/process/isa_simd_f32x4.abs | 233.35 | 229.8 | -1.52% | 0.667 | 6 |
| signals | BenchmarkExecParallel/independent/arith.run | 202.45 | 199.7 | -1.36% | 0.017 | 6 |
| explicit | BenchmarkExecParallel/independent/isa_ctl.br_table | 2937 | 2938 | +0.03% | 0.699 | 6 |
| signals | BenchmarkExecParallel/independent/isa_ctl.br_table | 2742.5 | 2903 | +5.85% | 0.132 | 6 |
| signals | BenchmarkExecParallel/process/isa_bulk_mem.copy_fwd_64 | 335.75 | 363.35 | +8.22% | 1.000 | 6 |
| signals | BenchmarkPluginInstantiate/ruby | 2009120.5 | 1871014.5 | -6.87% | 0.065 | 6 |
