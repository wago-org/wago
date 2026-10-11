| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| bio-kmer-count | compile | 8 | 27.5302 | 27.6037 | +0.47% | -0.09% to +0.88% | +0.27% |
| bio-kmer-count | exec | 8 | 32.5410 | 32.4986 | -0.03% | -0.75% to +0.41% | -0.13% |
| bio-lcs | compile | 8 | 42.7196 | 44.0612 | +3.07% | +2.61% to +3.88% | +3.14% |
| bio-lcs | exec | 8 | 58.1346 | 54.6897 | -5.99% | -6.36% to -5.82% | -5.93% |
| compression-rle | compile | 8 | 24.2697 | 24.3815 | +0.50% | -0.56% to +1.82% | +0.46% |
| compression-rle | exec | 8 | 16.1254 | 16.4282 | +1.94% | -0.26% to +2.57% | +1.88% |
| db-sort-merge-join | compile | 8 | 42.0752 | 43.1645 | +1.90% | +1.45% to +2.68% | +2.59% |
| db-sort-merge-join | exec | 8 | 45.0483 | 44.9016 | -0.24% | -0.59% to +0.10% | -0.33% |
| game-spatial-hash | compile | 8 | 30.8035 | 31.7742 | +3.26% | +2.86% to +3.74% | +3.15% |
| game-spatial-hash | exec | 8 | 31.9851 | 31.7857 | -0.35% | -0.74% to -0.20% | -0.62% |
| grid-pathfinding | compile | 8 | 57.8162 | 59.1322 | +2.25% | +1.54% to +4.10% | +2.28% |
| grid-pathfinding | exec | 8 | 39.7943 | 38.7610 | -3.17% | -3.67% to -2.35% | -2.60% |
| image-median | compile | 8 | 83.1834 | 83.6361 | +0.51% | -0.49% to +1.45% | +0.54% |
| image-median | exec | 8 | 288.9726 | 291.9633 | +0.24% | -4.82% to +4.89% | +1.03% |
| vision-dilation | compile | 8 | 45.7304 | 46.8889 | +2.87% | +1.83% to +3.20% | +2.53% |
| vision-dilation | exec | 8 | 41.3105 | 41.0845 | -0.32% | -1.49% to +0.41% | -0.55% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
