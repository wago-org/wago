| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| audio-adpcm | compile | 8 | 23.1283 | 25.5347 | +10.19% | +9.56% to +12.10% | +10.40% |
| audio-adpcm | exec | 8 | 38.4642 | 37.9683 | -0.73% | -1.32% to +1.27% | -1.29% |
| compiler-register-allocation | compile | 8 | 65.4361 | 64.9639 | -0.72% | -1.78% to -0.06% | -0.72% |
| compiler-register-allocation | exec | 8 | 17.0865 | 17.0510 | -0.17% | -1.24% to -0.01% | -0.21% |
| decimal-parse | compile | 8 | 1118.4364 | 1129.8643 | +1.25% | -0.08% to +2.14% | +1.02% |
| decimal-parse | exec | 8 | 0.6800 | 0.6698 | +0.03% | -0.89% to +0.50% | -1.50% |
| modular-arithmetic | compile | 8 | 2052.4987 | 2056.1862 | +0.29% | -0.88% to +1.50% | +0.18% |
| modular-arithmetic | exec | 8 | 11.9058 | 11.8456 | -0.24% | -1.40% to +0.34% | -0.51% |
| numeric-euclidean-gcd | compile | 8 | 20.4740 | 22.5033 | +7.95% | +7.33% to +9.62% | +9.91% |
| numeric-euclidean-gcd | exec | 8 | 111.0813 | 114.9756 | +3.08% | +1.05% to +11.83% | +3.51% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
