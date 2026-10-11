| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| compiler-register-allocation | compile | 8 | 68.9742 | 69.2858 | +0.39% | -0.33% to +0.72% | +0.45% |
| compiler-register-allocation | exec | 10 | 17.0079 | 16.9152 | -0.14% | -0.37% to -0.00% | -0.54% |
| vision-components | compile | 8 | 56.8087 | 57.4048 | -0.17% | -0.98% to +1.58% | +1.05% |
| vision-components | exec | 10 | 36.9864 | 37.3948 | +1.34% | +0.80% to +2.24% | +1.10% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
