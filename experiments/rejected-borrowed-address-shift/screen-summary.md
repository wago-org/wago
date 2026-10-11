| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| compiler-register-allocation | compile | 6 | 69.3054 | 68.9048 | -0.36% | -0.84% to +0.18% | -0.58% |
| compiler-register-allocation | exec | 6 | 16.8147 | 16.7654 | +0.17% | -0.64% to +0.18% | -0.29% |
| crypto-aes128 | compile | 6 | 179.2852 | 181.1133 | +1.26% | +0.22% to +2.32% | +1.02% |
| crypto-aes128 | exec | 6 | 38.7386 | 38.6995 | +0.62% | -2.24% to +2.95% | -0.10% |
| video-dct | compile | 6 | 150.1649 | 148.9875 | -0.65% | -1.50% to +0.41% | -0.78% |
| video-dct | exec | 6 | 42.4429 | 41.7663 | -0.51% | -2.16% to +0.17% | -1.59% |
| vision-components | compile | 6 | 58.1664 | 58.3369 | +0.07% | -0.39% to +0.61% | +0.29% |
| vision-components | exec | 6 | 39.3074 | 39.6400 | +1.35% | -0.36% to +2.08% | +0.85% |
| vision-dilation | compile | 6 | 45.4570 | 45.4603 | -0.16% | -0.78% to +1.27% | +0.01% |
| vision-dilation | exec | 6 | 42.6658 | 43.0584 | +0.73% | +0.62% to +1.38% | +0.92% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
