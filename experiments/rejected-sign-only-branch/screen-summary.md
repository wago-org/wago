| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| compiler-register-allocation | compile | 6 | 68.3269 | 67.8054 | -0.35% | -0.72% to -0.12% | -0.76% |
| compiler-register-allocation | exec | 8 | 16.7999 | 16.6040 | -2.05% | -2.82% to +4.67% | -1.17% |
| geo-point-in-polygon | compile | 6 | 35.4481 | 35.7526 | +0.77% | +0.34% to +1.23% | +0.86% |
| geo-point-in-polygon | exec | 8 | 21.2217 | 21.3755 | +0.67% | +0.50% to +0.87% | +0.73% |
| video-dct | compile | 6 | 145.7546 | 146.2732 | +0.39% | -0.01% to +0.81% | +0.36% |
| video-dct | exec | 8 | 41.1848 | 42.1059 | +0.34% | -0.74% to +3.31% | +2.24% |
| vision-components | compile | 6 | 57.3039 | 57.5041 | +0.11% | -0.28% to +0.37% | +0.35% |
| vision-components | exec | 8 | 40.3708 | 40.0077 | -0.59% | -1.33% to +0.51% | -0.90% |
| vision-dilation | compile | 6 | 45.7942 | 45.6100 | +0.00% | -0.41% to +0.62% | -0.40% |
| vision-dilation | exec | 8 | 42.4921 | 42.9633 | +1.20% | -0.22% to +11.96% | +1.11% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
