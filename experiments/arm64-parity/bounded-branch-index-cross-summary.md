| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| compiler-register-allocation | compile | 4 | 69.0393 | 69.2115 | -0.43% | -0.54% to +0.13% | +0.25% |
| compiler-register-allocation | exec | 4 | 17.1267 | 17.1555 | +0.21% | -0.20% to +0.66% | +0.17% |
| files-glob-match | compile | 4 | 27.7931 | 27.6429 | -0.86% | -3.36% to +2.05% | -0.54% |
| files-glob-match | exec | 4 | 131.2588 | 130.7031 | -0.76% | -1.25% to -0.25% | -0.42% |
| hardware-boolean-anf | compile | 4 | 67.5464 | 67.4086 | -0.99% | -2.56% to +0.25% | -0.20% |
| hardware-boolean-anf | exec | 4 | 73.7449 | 74.9613 | +0.91% | -1.79% to +3.74% | +1.65% |
| language-register-vm | compile | 4 | 41.9364 | 42.4769 | +1.60% | +1.34% to +1.98% | +1.29% |
| language-register-vm | exec | 4 | 11.1263 | 10.6103 | -4.19% | -5.68% to -2.90% | -4.64% |

Negative deltas mean faster. Ratios pair separate processes in reversed source-build order, selecting loop-memory-base ON in both binaries; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
