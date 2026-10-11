| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| compiler-register-allocation | compile | 4 | 69.7314 | 69.9319 | +0.51% | -0.76% to +1.97% | +0.29% |
| compiler-register-allocation | exec | 4 | 16.9455 | 17.2404 | +1.72% | +1.08% to +2.32% | +1.74% |
| files-glob-match | compile | 4 | 27.4485 | 28.1688 | +3.14% | +1.25% to +4.23% | +2.62% |
| files-glob-match | exec | 4 | 131.9954 | 131.7670 | -0.57% | -1.63% to +0.48% | -0.17% |
| hardware-boolean-anf | compile | 4 | 67.3847 | 68.8522 | +2.69% | +1.61% to +3.10% | +2.18% |
| hardware-boolean-anf | exec | 4 | 75.7104 | 68.0844 | -10.45% | -11.04% to -9.77% | -10.07% |
| language-register-vm | compile | 4 | 43.0318 | 43.2496 | +0.12% | -20.19% to +0.46% | +0.51% |
| language-register-vm | exec | 4 | 11.1370 | 10.3205 | -7.26% | -7.95% to -6.81% | -7.33% |

Negative deltas mean faster. Ratios pair separate processes in reversed source-build order, selecting loop-memory-base ON in both binaries; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
