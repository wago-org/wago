# Final timing diagnostics

Before is freshly measured main `a07de0973191efab1d32677eff527952c7f9cdd2`. After is `3a84fa628ac16ac5502a7de986f2fe732007583d`. Negative changes are faster. All values are medians; p values are unadjusted two-sided rank tests, not proof of equivalence.

Twenty-four fresh alternating main/prior/after triples; one second requested time. Selection: positive p < 0.05 in the twelve-pair repeat, or a user-listed case still above +5%, or any increase in a user-listed case with main time at least 1 ms. Prior is diagnostic only; it does not replace main. Earlier warning samples remain in confirmed-timing-regressions.md.

| Bounds | Benchmark | Main ns/op | After ns/op | Change | p | Samples each |
|---|---|---:|---:|---:|---:|---:|
| explicit | BenchmarkExecParallel/independent/isa_simd_i32x4.trunc_sat_f32x4_s | 1156 | 1165.5 | +0.82% | 0.010 | 24 |
| signals | BenchmarkPluginExec/wasm3 | 23402883.5 | 22854796.5 | -2.34% | 0.136 | 24 |
| signals | BenchmarkExecParallel/process/blake-as-simd.hashN | 532476.5 | 531210 | -0.24% | 0.455 | 24 |
| signals | BenchmarkExecParallel/process/isa_f64.min | 33359 | 33235.5 | -0.37% | 0.341 | 24 |
| signals | BenchmarkInstantiate/sha256 | 7669 | 7742 | +0.95% | 0.238 | 24 |
| signals | BenchmarkExecParallel/independent/isa_simd_i32x4.extadd_pairwise_u | 707 | 719.15 | +1.72% | <0.001 | 24 |
| signals | BenchmarkValidate/tiny | 682.3 | 693.8 | +1.69% | 0.018 | 24 |
