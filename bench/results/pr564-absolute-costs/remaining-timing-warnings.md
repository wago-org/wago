# Timing warnings in final diagnostics

Before is freshly measured main `a07de0973191efab1d32677eff527952c7f9cdd2`. After is `3a84fa628ac16ac5502a7de986f2fe732007583d`. Negative changes are faster. All values are medians; p values are unadjusted two-sided rank tests, not proof of equivalence.

3 positive changes have p < 0.05 in the final diagnostics. This threshold is unadjusted and is not a proof of cause or equivalence. The earlier twelve-pair warnings remain in confirmed-timing-regressions.md; all final samples remain in timing-diagnostics.md.

| Bounds | Benchmark | Main ns/op | After ns/op | Change | p | Samples each |
|---|---|---:|---:|---:|---:|---:|
| explicit | BenchmarkExecParallel/independent/isa_simd_i32x4.trunc_sat_f32x4_s | 1156 | 1165.5 | +0.82% | 0.010 | 24 |
| signals | BenchmarkExecParallel/independent/isa_simd_i32x4.extadd_pairwise_u | 707 | 719.15 | +1.72% | <0.001 | 24 |
| signals | BenchmarkValidate/tiny | 682.3 | 693.8 | +1.69% | 0.018 | 24 |
