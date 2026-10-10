| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| compile-match | compile | 12 | 8807.0322 | 8855.3939 | +0.37% | +0.25% to +0.69% | +0.55% |
| compiler-register-allocation | compile | 12 | 66.3724 | 67.2466 | +1.99% | +1.11% to +2.33% | +1.32% |
| compiler-register-allocation | exec | 12 | 17.0825 | 17.0556 | +0.60% | -0.35% to +0.95% | -0.16% |
| decimal-parse | compile | 12 | 1113.3583 | 1121.1790 | +0.81% | +0.31% to +1.74% | +0.70% |
| files-glob-match | compile | 12 | 26.1878 | 26.6104 | +1.13% | +0.63% to +1.95% | +1.61% |
| files-glob-match | exec | 12 | 134.8599 | 134.4415 | -0.45% | -0.93% to +0.29% | -0.31% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
