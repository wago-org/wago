| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| compiler-register-allocation | compile | 8 | 68.3757 | 67.8972 | +0.06% | -0.30% to +0.39% | -0.70% |
| compiler-register-allocation | exec | 8 | 16.9538 | 16.8327 | -0.54% | -1.86% to +0.41% | -0.71% |
| files-glob-match | compile | 8 | 26.8285 | 26.8784 | +0.01% | -0.63% to +0.33% | +0.19% |
| files-glob-match | exec | 8 | 132.6238 | 133.3399 | +0.09% | -0.37% to +0.74% | +0.54% |
| hardware-boolean-anf | compile | 8 | 66.4137 | 66.1163 | -0.54% | -1.11% to -0.16% | -0.45% |
| hardware-boolean-anf | exec | 8 | 75.2153 | 75.3553 | -1.50% | -18.24% to +1.19% | +0.19% |
| language-register-vm | compile | 8 | 42.0460 | 42.1013 | -0.14% | -4.83% to +0.15% | +0.13% |
| language-register-vm | exec | 8 | 11.2831 | 10.4995 | -5.48% | -6.21% to -5.14% | -6.95% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
