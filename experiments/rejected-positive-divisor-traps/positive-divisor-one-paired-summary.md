| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| audio-adpcm | compile | 12 | 24.1385 | 26.8584 | +12.15% | +10.65% to +15.13% | +11.27% |
| audio-adpcm | exec | 12 | 38.5757 | 41.5408 | +7.70% | +6.58% to +8.96% | +7.69% |
| compiler-register-allocation | compile | 12 | 67.9106 | 67.9541 | +0.10% | -0.35% to +1.03% | +0.06% |
| compiler-register-allocation | exec | 12 | 16.9788 | 17.0599 | +0.35% | +0.17% to +1.12% | +0.48% |
| decimal-parse | compile | 6 | 1136.1555 | 1134.9590 | -0.74% | -1.57% to +0.22% | -0.11% |
| decimal-parse | exec | 6 | 0.6818 | 0.6844 | +0.21% | +0.11% to +0.47% | +0.38% |
| modular-arithmetic | compile | 6 | 2110.4085 | 2104.6062 | -0.00% | -1.28% to +0.73% | -0.27% |
| modular-arithmetic | exec | 6 | 11.8435 | 11.9268 | +0.65% | +0.42% to +0.80% | +0.70% |
| numeric-euclidean-gcd | compile | 12 | 21.1630 | 21.1187 | +0.26% | -1.40% to +1.19% | -0.21% |
| numeric-euclidean-gcd | exec | 12 | 123.5175 | 120.6997 | -2.07% | -5.11% to +6.30% | -2.28% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
