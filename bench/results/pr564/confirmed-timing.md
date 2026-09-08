# Timing slowdowns after focused repeats

These are 12-pair, 300 ms requested-time results. See the main report for host
noise, multiple-comparison, and benchmark-unit limits. All first-pass data
remain in the comparison files.

| Mode | Benchmark | Main | Candidate | Change | Exact rank p |
|---|---|---:|---:|---:|---:|
| explicit | BenchmarkExecParallel/process/swar-pack-parse.parse4 | 8.04 ns | 13.85 ns | +72.18% | 0.00000296 |
| explicit | BenchmarkExecParallel/independent/fib_iter.fib | 14.36 ns | 16.89 ns | +17.69% | 7.40e-7 |
| explicit | BenchmarkExecParallel/process/xjb-mulhi.mulhi | 13.23 ns | 14.50 ns | +9.56% | 0.00106 |
| explicit | BenchmarkExecGlobalGet_wago | 75.67 ns | 80.09 ns | +5.85% | 0.00107 |
| explicit | BenchmarkExecParallel/independent/isa_simd_i64x2.shl | 157.05 ns | 163.60 ns | +4.17% | 0.0500 |
| signals | BenchmarkInstantiate/zstd | 9.283 us | 9.437 us | +1.66% | 0.0166 |
