# Timing increases in the longer repeat

Before is freshly measured main `731e95ff2cda7309eaf6d956f1417066bf7f1b69`. After is `b4f2360f517641803249c7ed998f37d8b0ec82a2`. Negative changes are faster. All values are medians; p values are unadjusted two-sided rank tests, not proof of equivalence.

Positive changes with p < 0.05. These are unadjusted warnings for further diagnosis, not automatically proven causes.

| Bounds | Benchmark | Main ns/op | After ns/op | Change | p | Samples each |
|---|---|---:|---:|---:|---:|---:|
| signals | BenchmarkExec/isa_f32.min | 33202 | 33933 | +2.20% | <0.001 | 12 |
| signals | BenchmarkExec/isa_simd_i16x8.extend_high_s | 1986.5 | 2069 | +4.15% | <0.001 | 12 |
| signals | BenchmarkExecGlobalGet_wago | 92.925 | 96.155 | +3.48% | <0.001 | 12 |
| signals | BenchmarkExecMemoryLoad_wago | 93.1 | 96.46 | +3.61% | <0.001 | 12 |
| signals | BenchmarkExecParallel/independent/isa_simd_i32x4.extadd_pairwise_u | 703.55 | 728.2 | +3.50% | 0.008 | 12 |
| signals | BenchmarkMemSumBounds/guard | 240.3 | 245.95 | +2.35% | <0.001 | 12 |
| signals | BenchmarkValidateWorkers/tiny/p4 | 3291.5 | 3338.5 | +1.43% | 0.005 | 12 |
| explicit | BenchmarkExec/tiny.add | 8.1115 | 8.53 | +5.16% | <0.001 | 12 |
| explicit | BenchmarkExec/swar-pack-parse.pack | 8.221 | 8.498 | +3.37% | <0.001 | 12 |
| explicit | BenchmarkExec/swar-pack-parse.parse4 | 8.0785 | 8.559 | +5.95% | <0.001 | 12 |
| explicit | BenchmarkExec/branches.classify | 7.9445 | 8.2635 | +4.02% | <0.001 | 12 |
