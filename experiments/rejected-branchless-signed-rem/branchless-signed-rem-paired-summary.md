| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| complex-roundtrip | compile | 8 | 933.8275 | 906.7262 | +0.32% | -2.02% to +1.20% | -2.90% |
| complex-roundtrip | exec | 8 | 160.8489 | 161.6220 | +0.14% | -0.34% to +1.34% | +0.48% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
