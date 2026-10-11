| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| compiler-register-allocation | compile | 8 | 67.0176 | 67.1592 | +0.09% | -0.06% to +0.40% | +0.21% |
| compiler-register-allocation | exec | 10 | 16.9891 | 16.9669 | -0.75% | -1.57% to +0.65% | -0.13% |
| vision-dilation | compile | 8 | 44.3931 | 44.7190 | +0.37% | +0.15% to +0.74% | +0.73% |
| vision-dilation | exec | 10 | 44.0469 | 43.8685 | +0.33% | -0.83% to +0.58% | -0.41% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
