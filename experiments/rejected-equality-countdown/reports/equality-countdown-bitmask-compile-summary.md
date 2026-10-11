| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| bio-kmer-count | compile | 8 | 27.6740 | 27.6483 | +0.08% | -12.05% to +1.40% | -0.09% |
| bio-lcs | compile | 8 | 42.9061 | 44.1248 | +4.00% | +2.56% to +5.62% | +2.84% |
| compression-rle | compile | 8 | 24.2209 | 24.9928 | +1.71% | +1.24% to +3.04% | +3.19% |
| db-sort-merge-join | compile | 8 | 41.9444 | 43.3874 | +2.94% | +2.66% to +3.73% | +3.44% |
| files-merkle-tree | compile | 8 | 54.1241 | 56.4939 | +4.21% | +3.90% to +4.44% | +4.38% |
| geo-point-in-polygon | compile | 8 | 34.4841 | 34.8112 | +0.33% | -0.28% to +1.05% | +0.95% |
| grid-pathfinding | compile | 8 | 54.7829 | 57.4444 | +5.09% | +4.67% to +5.51% | +4.86% |
| image-median | compile | 8 | 82.8214 | 83.4956 | +1.06% | -0.34% to +2.16% | +0.81% |
| vision-dilation | compile | 8 | 44.3549 | 46.0535 | +4.02% | +3.60% to +4.51% | +3.83% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
