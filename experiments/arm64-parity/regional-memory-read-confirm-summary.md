| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| compiler-register-allocation | compile | 12 | 69.5479 | 69.2450 | -0.36% | -1.18% to +1.42% | -0.44% |
| compiler-register-allocation | exec | 12 | 17.0729 | 17.0910 | -0.11% | -0.43% to +0.17% | +0.11% |
| graphics-reed-solomon | compile | 12 | 100.4079 | 98.4179 | -1.69% | -2.38% to -1.05% | -1.98% |
| graphics-reed-solomon | exec | 12 | 123.0997 | 117.8219 | -4.42% | -5.04% to -3.54% | -4.29% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
