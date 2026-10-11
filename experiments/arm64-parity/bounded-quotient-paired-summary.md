| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| audio-adpcm | compile | 12 | 23.3721 | 23.3252 | +0.33% | -0.33% to +0.77% | -0.20% |
| audio-adpcm | exec | 12 | 36.5204 | 29.0669 | -20.79% | -21.11% to -19.98% | -20.41% |
| compiler-register-allocation | compile | 12 | 65.6517 | 65.7919 | -0.04% | -0.42% to +0.56% | +0.21% |
| compiler-register-allocation | exec | 12 | 16.8386 | 16.7512 | -0.50% | -0.92% to +0.30% | -0.52% |
| numeric-euclidean-gcd | compile | 12 | 20.7150 | 20.7186 | +0.12% | -0.08% to +0.36% | +0.02% |
| numeric-euclidean-gcd | exec | 12 | 105.9221 | 102.5110 | -7.87% | -14.05% to +4.58% | -3.22% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
