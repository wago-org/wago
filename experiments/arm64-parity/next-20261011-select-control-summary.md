| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| compiler-register-allocation | exec | 10 | 17.4839 | 17.4422 | -0.58% | -2.89% to +1.68% | -0.24% |
| hardware-prime-implicants | exec | 10 | 47.9478 | 47.6035 | +1.57% | +0.30% to +2.87% | -0.72% |
| vision-components | exec | 10 | 38.2521 | 38.0487 | +0.79% | -1.09% to +2.10% | -0.53% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
