# Your 23 cases: full-suite samples

Before is freshly measured main `a07de0973191efab1d32677eff527952c7f9cdd2`. After is `3a84fa628ac16ac5502a7de986f2fe732007583d`. Negative changes are faster. All values are medians; p values are unadjusted two-sided rank tests, not proof of equivalence.

Six pairs, 100 ms requested time. Bounds labels recover the two otherwise duplicate benchmark names from the earlier report.

| Bounds | Benchmark | Main ns/op | After ns/op | Change | p | Samples each |
|---|---|---:|---:|---:|---:|---:|
| explicit | BenchmarkExec/coremark.coremark_run | 35177461 | 36646852 | +4.18% | 0.132 | 6 |
| signals | BenchmarkExecParallel/independent/isa_simd_i64x2.extend_high_s | 328.45 | 330.6 | +0.65% | 0.699 | 6 |
| signals | BenchmarkExecParallel/independent/isa_ctl.if_else | 1885.5 | 1915.5 | +1.59% | 1.000 | 6 |
| explicit | BenchmarkExecParallel/independent/isa_mem.store_i32_stride | 2084 | 1980.5 | -4.97% | 0.310 | 6 |
| signals | BenchmarkExecParallel/independent/float.run | 639.4 | 625.25 | -2.21% | 0.699 | 6 |
| signals | BenchmarkExecParallel/independent/json-as.deserializeN | 7139 | 7438 | +4.19% | 0.026 | 6 |
| signals | BenchmarkExecParallel/independent/isa_mem.store_i32_stride | 1746.5 | 1763 | +0.94% | 0.513 | 6 |
| signals | BenchmarkExecParallel/independent/isa_f64.sqrt | 25711.5 | 22553.5 | -12.28% | 0.394 | 6 |
| signals | BenchmarkCompileCompact/fib_rec | 17570.5 | 17709 | +0.79% | 0.699 | 6 |
| explicit | BenchmarkExecParallel/independent/isa_i64.rotl | 2425.5 | 2320.5 | -4.33% | 0.310 | 6 |
| signals | BenchmarkExecParallel/independent/isa_simd_i8x16.shr_s | 683.8 | 692.45 | +1.26% | 0.818 | 6 |
| explicit | BenchmarkExecParallel/process/float.run | 622.65 | 640.15 | +2.81% | 0.699 | 6 |
| signals | BenchmarkExecParallel/independent/isa_simd_i64x2.shl | 185.15 | 182.3 | -1.54% | 0.589 | 6 |
| signals | BenchmarkPluginExec/wasm3 | 24323044.5 | 24324543 | +0.01% | 0.937 | 6 |
| signals | BenchmarkExecParallel/independent/spectralnorm.run | 97512.5 | 103825 | +6.47% | 0.180 | 6 |
| signals | BenchmarkExecParallel/independent/matmul.run | 18454 | 18442 | -0.07% | 0.937 | 6 |
| signals | BenchmarkExecParallel/independent/nbody.step | 43907.5 | 43385 | -1.19% | 0.937 | 6 |
| explicit | BenchmarkExecParallel/process/isa_simd_f32x4.abs | 277.45 | 275.25 | -0.79% | 0.699 | 6 |
| signals | BenchmarkExecParallel/independent/arith.run | 235.65 | 229.8 | -2.48% | 0.485 | 6 |
| explicit | BenchmarkExecParallel/independent/isa_ctl.br_table | 3717.5 | 3492.5 | -6.05% | 0.485 | 6 |
| signals | BenchmarkExecParallel/independent/isa_ctl.br_table | 3387 | 3296 | -2.69% | 0.310 | 6 |
| signals | BenchmarkExecParallel/process/isa_bulk_mem.copy_fwd_64 | 418.3 | 368.45 | -11.92% | 0.071 | 6 |
| signals | BenchmarkPluginInstantiate/ruby | 2427480.5 | 2143690 | -11.69% | 0.065 | 6 |
