| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| audio-adpcm | compile | 6 | 23.1684 | 23.0567 | +0.86% | -1.09% to +4.20% | -0.48% |
| compiler-register-allocation | compile | 6 | 65.4430 | 66.5268 | +1.43% | +0.57% to +2.27% | +1.66% |
| files-glob-match | compile | 6 | 25.7581 | 26.8211 | +2.47% | +0.25% to +5.13% | +4.13% |
| hardware-boolean-anf | compile | 6 | 64.7009 | 65.1384 | +1.46% | -2.73% to +2.74% | +0.68% |

Negative deltas mean faster. Ratios pair separate processes in reversed source-build order, selecting loop-memory-base ON in both binaries; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
