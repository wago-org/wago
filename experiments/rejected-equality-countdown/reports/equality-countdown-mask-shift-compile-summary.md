| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| bio-lcs | compile | 8 | 41.4373 | 42.6292 | +2.75% | +1.99% to +3.04% | +2.88% |
| compression-rle | compile | 8 | 23.5300 | 24.2032 | +2.93% | +2.55% to +3.79% | +2.86% |
| files-merkle-tree | compile | 8 | 53.8776 | 55.8807 | +3.85% | +2.98% to +4.98% | +3.72% |
| geo-point-in-polygon | compile | 8 | 33.0946 | 33.5585 | +0.46% | -0.60% to +1.18% | +1.40% |
| grid-pathfinding | compile | 8 | 57.2744 | 60.1194 | +4.01% | +3.27% to +5.46% | +4.97% |
| vision-dilation | compile | 8 | 44.5194 | 46.1726 | +4.40% | +3.76% to +4.82% | +3.71% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
