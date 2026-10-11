| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| compiler-register-allocation | compile | 6 | 68.2895 | 67.6009 | -0.85% | -1.57% to -0.61% | -1.01% |
| compiler-register-allocation | exec | 8 | 16.7445 | 16.7446 | -0.23% | -0.33% to +0.17% | +0.00% |
| crypto-aes128 | compile | 6 | 178.0296 | 179.2929 | +0.76% | -0.10% to +0.95% | +0.71% |
| crypto-aes128 | exec | 8 | 37.8776 | 37.4730 | -0.17% | -1.58% to +1.02% | -1.07% |
| video-dct | compile | 6 | 149.4665 | 148.0793 | -2.30% | -4.28% to +0.32% | -0.93% |
| video-dct | exec | 8 | 40.2085 | 40.2044 | +0.07% | -0.44% to +0.87% | -0.01% |
| vision-components | compile | 6 | 57.9939 | 58.4634 | +0.37% | -1.06% to +1.56% | +0.81% |
| vision-components | exec | 8 | 36.6709 | 36.7055 | +0.20% | -0.27% to +0.48% | +0.09% |
| vision-dilation | compile | 6 | 45.1627 | 45.3020 | +0.31% | -0.02% to +1.03% | +0.31% |
| vision-dilation | exec | 8 | 41.0889 | 40.1504 | -2.39% | -2.96% to -1.55% | -2.28% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
