| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| audio-adpcm | compile | 12 | 23.7390 | 25.3551 | +7.11% | +6.32% to +8.36% | +6.81% |
| audio-adpcm | exec | 12 | 39.2867 | 38.6657 | -0.80% | -3.08% to +1.24% | -1.58% |
| compiler-register-allocation | compile | 12 | 68.0100 | 67.7800 | -0.24% | -0.78% to -0.08% | -0.34% |
| compiler-register-allocation | exec | 12 | 16.8939 | 16.8713 | +0.15% | -1.02% to +1.03% | -0.13% |
| decimal-parse | compile | 6 | 1315.2657 | 1307.2427 | +0.54% | -0.63% to +1.35% | -0.61% |
| decimal-parse | exec | 6 | 0.7228 | 0.6999 | -3.12% | -41.18% to -0.73% | -3.17% |
| modular-arithmetic | compile | 6 | 2175.0675 | 2182.4105 | +0.24% | -0.12% to +0.84% | +0.34% |
| modular-arithmetic | exec | 6 | 11.8809 | 11.8639 | -0.11% | -0.84% to +0.40% | -0.14% |
| numeric-euclidean-gcd | compile | 12 | 21.3556 | 21.6945 | +1.54% | +1.31% to +1.79% | +1.59% |
| numeric-euclidean-gcd | exec | 12 | 116.5739 | 125.9549 | +5.64% | -0.64% to +17.81% | +8.05% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
