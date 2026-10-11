| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| compiler-register-allocation | compile | 8 | 67.0454 | 67.0164 | +0.09% | -0.61% to +0.54% | -0.04% |
| compiler-register-allocation | exec | 10 | 16.8568 | 16.8154 | -0.67% | -1.08% to +0.25% | -0.25% |
| vision-dilation | compile | 8 | 44.5906 | 44.6039 | +0.26% | -0.61% to +0.74% | +0.03% |
| vision-dilation | exec | 10 | 43.7198 | 42.0999 | -2.02% | -2.97% to -0.89% | -3.71% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
