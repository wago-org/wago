# Timing increases in the longer repeat

Before is freshly measured main `a07de0973191efab1d32677eff527952c7f9cdd2`. After is `3a84fa628ac16ac5502a7de986f2fe732007583d`. Negative changes are faster. All values are medians; p values are unadjusted two-sided rank tests, not proof of equivalence.

Positive changes with p < 0.05. These are unadjusted warnings for further diagnosis, not automatically proven causes.

| Bounds | Benchmark | Main ns/op | After ns/op | Change | p | Samples each |
|---|---|---:|---:|---:|---:|---:|
| signals | BenchmarkExecParallel/process/isa_f64.min | 31852 | 32134.5 | +0.89% | 0.014 | 12 |
| signals | BenchmarkInstantiate/sha256 | 7407.5 | 7466 | +0.79% | 0.015 | 12 |
| signals | BenchmarkValidate/tiny | 672.65 | 678.3 | +0.84% | 0.009 | 12 |
| signals | BenchmarkExecParallel/independent/isa_simd_i32x4.extadd_pairwise_u | 692.05 | 707.3 | +2.20% | 0.002 | 12 |
| signals | BenchmarkExecParallel/process/blake-as-simd.hashN | 514736 | 517219.5 | +0.48% | 0.039 | 12 |
| explicit | BenchmarkExecParallel/independent/isa_simd_i32x4.trunc_sat_f32x4_s | 1083.5 | 1094 | +0.97% | 0.011 | 12 |
