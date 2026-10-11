| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| bio-kmer-count | compile | 12 | 27.1027 | 27.4792 | +1.39% | +0.99% to +1.61% | +1.39% |
| bio-kmer-count | exec | 12 | 34.9967 | 34.3048 | -0.49% | -4.30% to +1.08% | -1.98% |
| bio-lcs | compile | 12 | 42.8929 | 43.7249 | +2.16% | +1.85% to +2.68% | +1.94% |
| bio-lcs | exec | 12 | 59.7927 | 55.7570 | -7.22% | -7.90% to -6.81% | -6.75% |
| compression-rle | compile | 12 | 24.0674 | 24.3492 | +1.95% | -1.55% to +4.58% | +1.17% |
| compression-rle | exec | 12 | 17.3382 | 16.3672 | -6.46% | -9.68% to -0.09% | -5.60% |
| db-sort-merge-join | compile | 12 | 44.2030 | 44.9167 | +2.85% | +1.26% to +4.65% | +1.61% |
| db-sort-merge-join | exec | 12 | 48.1783 | 47.5441 | -0.31% | -1.91% to +0.56% | -1.32% |
| game-spatial-hash | compile | 12 | 30.7883 | 31.6430 | +3.09% | +2.25% to +3.74% | +2.78% |
| game-spatial-hash | exec | 12 | 35.8886 | 35.6067 | +0.16% | -1.44% to +2.21% | -0.79% |
| grid-pathfinding | compile | 12 | 55.4917 | 58.6343 | +5.84% | +1.96% to +8.93% | +5.66% |
| grid-pathfinding | exec | 12 | 37.0115 | 36.7069 | -0.78% | -1.78% to +0.64% | -0.82% |
| image-median | compile | 12 | 83.6344 | 83.4783 | -0.31% | -0.76% to +0.06% | -0.19% |
| image-median | exec | 12 | 274.0535 | 281.5542 | +1.69% | -2.40% to +7.70% | +2.74% |
| stats-bootstrap | compile | 12 | 37.6093 | 39.1177 | +1.68% | -0.66% to +6.16% | +4.01% |
| stats-bootstrap | exec | 12 | 20.8312 | 20.7461 | -0.43% | -0.66% to -0.07% | -0.41% |
| vision-dilation | compile | 12 | 44.6124 | 46.8581 | +4.62% | +3.80% to +6.96% | +5.03% |
| vision-dilation | exec | 12 | 42.2117 | 42.6984 | -0.15% | -1.45% to +1.95% | +1.15% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
