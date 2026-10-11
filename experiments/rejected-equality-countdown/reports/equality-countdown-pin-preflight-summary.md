| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| bio-lcs | compile | 8 | 41.9541 | 43.5808 | +3.55% | +2.81% to +3.99% | +3.88% |
| files-merkle-tree | compile | 8 | 55.3636 | 56.8912 | +2.89% | +1.99% to +3.25% | +2.76% |
| game-spatial-hash | compile | 8 | 29.8291 | 30.5680 | +2.21% | +1.70% to +2.71% | +2.48% |
| geo-point-in-polygon | compile | 8 | 32.7633 | 32.6599 | -0.36% | -0.65% to -0.20% | -0.32% |
| grid-pathfinding | compile | 8 | 55.7326 | 56.8109 | +2.15% | +1.72% to +2.40% | +1.93% |
| vision-dilation | compile | 8 | 45.6322 | 46.4187 | +1.55% | +1.15% to +2.42% | +1.72% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
