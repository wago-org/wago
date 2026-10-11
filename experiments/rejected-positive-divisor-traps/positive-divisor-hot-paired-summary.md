| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| audio-adpcm | compile | 8 | 23.2971 | 24.6833 | +6.97% | +6.50% to +7.50% | +5.95% |
| audio-adpcm | exec | 8 | 36.0727 | 36.4361 | -0.05% | -0.46% to +1.52% | +1.01% |
| compiler-register-allocation | compile | 8 | 67.4020 | 66.8585 | -0.62% | -1.16% to -0.38% | -0.81% |
| compiler-register-allocation | exec | 8 | 16.5590 | 16.5893 | +0.25% | -0.15% to +0.52% | +0.18% |
| decimal-parse | compile | 6 | 1226.6231 | 1225.5588 | -0.77% | -1.93% to +0.55% | -0.09% |
| decimal-parse | exec | 6 | 0.6758 | 0.6786 | +0.22% | +0.14% to +0.67% | +0.42% |
| modular-arithmetic | compile | 6 | 2222.8136 | 2223.7174 | +0.66% | -0.64% to +0.92% | +0.04% |
| modular-arithmetic | exec | 6 | 11.7327 | 11.7044 | -0.28% | -0.40% to -0.04% | -0.24% |
| numeric-euclidean-gcd | compile | 8 | 20.9106 | 21.1563 | +1.53% | +0.39% to +2.82% | +1.17% |
| numeric-euclidean-gcd | exec | 8 | 111.5228 | 114.1537 | +1.23% | -7.18% to +13.90% | +2.36% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
