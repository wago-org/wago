| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| modular-arithmetic | compile | 8 | 2028.9592 | 2028.1914 | +0.20% | -0.62% to +0.69% | -0.04% |
| modular-arithmetic | exec | 8 | 11.6461 | 11.6902 | +0.70% | -0.35% to +1.14% | +0.38% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
