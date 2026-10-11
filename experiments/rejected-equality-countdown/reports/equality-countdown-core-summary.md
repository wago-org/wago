| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| compile-match | compile | 4 | 9558.6333 | 9300.9124 | -2.67% | -4.51% to +5.65% | -2.70% |
| compile-match | exec | 8 | 8.0120 | 8.1271 | +1.42% | -0.05% to +2.14% | +1.44% |
| decimal-parse | compile | 4 | 1314.6464 | 1279.8137 | -1.50% | -3.54% to -0.10% | -2.65% |
| decimal-parse | exec | 8 | 0.6864 | 0.6818 | -0.74% | -1.32% to +0.18% | -0.68% |
| decompress | compile | 4 | 4153.7778 | 4129.3292 | -5.39% | -19.67% to +1.98% | -0.59% |
| encode | compile | 4 | 169.2132 | 168.9547 | -0.95% | -2.03% to +0.07% | -0.15% |
| hash | compile | 4 | 792.7530 | 779.9451 | -0.96% | -1.52% to -0.22% | -1.62% |
| inflate | compile | 4 | 842.9116 | 851.6564 | -0.27% | -1.30% to +1.03% | +1.04% |
| parse-edit-write | compile | 4 | 9661.9513 | 9511.8819 | -0.86% | -11.31% to +16.78% | -1.55% |
| parse-edit-write | exec | 8 | 0.7635 | 0.7589 | -0.41% | -0.72% to +0.28% | -0.60% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
