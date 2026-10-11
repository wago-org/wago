| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| audio-adpcm | compile | 8 | 24.8222 | 27.2920 | +9.81% | +7.70% to +16.31% | +9.95% |
| audio-adpcm | exec | 8 | 38.6087 | 39.3190 | +0.65% | -0.52% to +1.79% | +1.84% |
| compiler-register-allocation | compile | 8 | 68.4867 | 68.5142 | -0.68% | -38.08% to +0.15% | +0.04% |
| compiler-register-allocation | exec | 8 | 16.8807 | 16.8220 | -0.35% | -0.60% to -0.12% | -0.35% |
| decimal-parse | compile | 8 | 1231.8747 | 1213.2926 | -0.11% | -2.58% to +0.38% | -1.51% |
| decimal-parse | exec | 8 | 0.6850 | 0.6831 | -0.02% | -0.40% to +0.14% | -0.28% |
| modular-arithmetic | compile | 8 | 2199.5469 | 2241.8352 | +0.85% | -0.33% to +12.28% | +1.92% |
| modular-arithmetic | exec | 8 | 11.7888 | 11.8205 | +0.10% | -0.07% to +0.46% | +0.27% |
| numeric-euclidean-gcd | compile | 8 | 21.4651 | 21.6767 | +0.35% | -0.01% to +1.16% | +0.99% |
| numeric-euclidean-gcd | exec | 8 | 120.6783 | 111.5100 | -3.24% | -17.09% to +2.97% | -7.60% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
