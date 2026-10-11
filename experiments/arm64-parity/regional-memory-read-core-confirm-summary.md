| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| complex-roundtrip | compile | 12 | 912.5383 | 908.2192 | +0.55% | -1.10% to +7.58% | -0.47% |
| complex-roundtrip | exec | 12 | 157.2445 | 156.8567 | +0.03% | -0.34% to +0.26% | -0.25% |
| decimal-parse | compile | 12 | 1223.4510 | 1242.5558 | +1.12% | -0.14% to +1.84% | +1.56% |
| decimal-parse | exec | 12 | 0.6851 | 0.6633 | -3.06% | -3.62% to -2.65% | -3.18% |
| hash | compile | 12 | 778.7415 | 779.0909 | -0.23% | -1.21% to +0.49% | +0.04% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
