| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| audio-adpcm | compile | 12 | 23.8781 | 25.7923 | +9.26% | +8.60% to +10.49% | +8.02% |
| audio-adpcm | exec | 12 | 37.6421 | 39.2339 | +5.27% | +4.04% to +5.64% | +4.23% |
| compiler-register-allocation | compile | 12 | 65.6680 | 65.8272 | +0.01% | -0.48% to +0.47% | +0.24% |
| compiler-register-allocation | exec | 12 | 16.9322 | 16.9589 | +0.27% | -0.90% to +0.55% | +0.16% |
| decimal-parse | compile | 6 | 1239.7273 | 1253.9451 | +0.96% | +0.84% to +2.22% | +1.15% |
| decimal-parse | exec | 6 | 0.6825 | 0.6825 | -0.10% | -0.15% to +0.14% | +0.01% |
| modular-arithmetic | compile | 6 | 2187.8120 | 2160.4338 | -1.80% | -2.11% to -1.62% | -1.25% |
| modular-arithmetic | exec | 6 | 11.9144 | 11.9200 | -0.37% | -0.60% to +0.24% | +0.05% |
| numeric-euclidean-gcd | compile | 12 | 22.0691 | 21.8129 | +1.22% | -3.37% to +3.15% | -1.16% |
| numeric-euclidean-gcd | exec | 12 | 121.1282 | 116.5567 | -3.25% | -7.15% to +3.02% | -3.77% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
