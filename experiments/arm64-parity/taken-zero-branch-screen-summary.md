| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| audio-adpcm | compile | 4 | 24.5036 | 24.6794 | +1.30% | +0.33% to +2.22% | +0.72% |
| audio-adpcm | exec | 4 | 30.1542 | 30.1595 | +0.22% | -1.59% to +2.35% | +0.02% |
| compiler-register-allocation | compile | 4 | 68.7315 | 68.4381 | +0.18% | -0.81% to +0.37% | -0.43% |
| compiler-register-allocation | exec | 4 | 17.1172 | 17.2698 | +1.37% | +0.19% to +1.84% | +0.89% |
| files-glob-match | compile | 4 | 26.4232 | 26.6014 | -0.34% | -0.54% to +0.99% | +0.67% |
| files-glob-match | exec | 4 | 131.3184 | 131.4626 | -0.36% | -0.57% to +0.49% | +0.11% |
| hardware-boolean-anf | compile | 8 | 68.0486 | 67.9817 | +0.51% | -0.32% to +0.74% | -0.10% |
| hardware-boolean-anf | exec | 8 | 75.9389 | 67.9869 | -9.98% | -12.03% to -9.48% | -10.47% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
