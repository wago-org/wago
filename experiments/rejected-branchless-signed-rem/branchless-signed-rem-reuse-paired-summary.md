| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| complex-roundtrip | compile | 8 | 974.8813 | 939.0395 | -2.45% | -8.08% to +3.76% | -3.68% |
| complex-roundtrip | exec | 8 | 159.4824 | 159.4528 | -0.23% | -1.23% to +0.30% | -0.02% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
