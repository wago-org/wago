| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| bio-lcs | compile | 8 | 41.8613 | 41.9979 | +0.35% | -1.20% to +1.41% | +0.33% |
| compiler-register-allocation | compile | 8 | 66.8651 | 66.7944 | +0.56% | -0.06% to +1.27% | -0.11% |
| geo-point-in-polygon | compile | 8 | 34.2209 | 34.5724 | +1.27% | +0.29% to +2.19% | +1.03% |
| grid-pathfinding | compile | 8 | 56.0798 | 55.7249 | -0.58% | -1.43% to -0.06% | -0.63% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
