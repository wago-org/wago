| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| audio-adpcm | compile | 8 | 23.6370 | 25.1184 | +6.82% | +5.99% to +7.63% | +6.27% |
| audio-adpcm | exec | 8 | 40.5581 | 40.0267 | -2.79% | -4.28% to +0.53% | -1.31% |
| compiler-register-allocation | compile | 8 | 67.5225 | 68.0528 | +0.47% | -0.23% to +1.32% | +0.79% |
| compiler-register-allocation | exec | 8 | 16.8353 | 16.9355 | +0.60% | -0.63% to +1.46% | +0.60% |
| decimal-parse | compile | 8 | 1305.5091 | 1282.7727 | -0.84% | -5.29% to -0.30% | -1.74% |
| decimal-parse | exec | 8 | 0.6877 | 0.6889 | +1.59% | -2.43% to +54.94% | +0.18% |
| modular-arithmetic | compile | 8 | 2212.4936 | 2220.9258 | -0.09% | -1.32% to +2.46% | +0.38% |
| modular-arithmetic | exec | 8 | 11.7096 | 11.8461 | +1.19% | +0.65% to +2.12% | +1.16% |
| numeric-euclidean-gcd | compile | 8 | 21.3094 | 21.5458 | -0.25% | -0.70% to +1.46% | +1.11% |
| numeric-euclidean-gcd | exec | 8 | 117.3567 | 115.2677 | +0.24% | -9.26% to +7.30% | -1.78% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
