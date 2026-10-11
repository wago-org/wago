| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| audio-adpcm | compile | 4 | 24.1070 | 25.1962 | +5.78% | +1.65% to +10.21% | +4.52% |
| audio-adpcm | exec | 4 | 30.5234 | 30.3314 | -1.64% | -2.56% to +0.11% | -0.63% |
| compiler-register-allocation | compile | 4 | 68.6966 | 70.3102 | +1.75% | +0.78% to +2.96% | +2.35% |
| compiler-register-allocation | exec | 4 | 16.6271 | 16.6436 | -0.28% | -1.35% to +1.19% | +0.10% |
| files-glob-match | compile | 4 | 27.0505 | 27.9473 | +3.31% | -19.30% to +4.85% | +3.32% |
| files-glob-match | exec | 4 | 129.0390 | 128.4055 | -1.35% | -10.37% to -0.55% | -0.49% |
| hardware-boolean-anf | compile | 4 | 67.6139 | 68.3876 | +0.66% | -0.95% to +2.35% | +1.14% |
| hardware-boolean-anf | exec | 4 | 73.0690 | 65.1079 | -10.94% | -11.33% to -10.44% | -10.90% |

Negative deltas mean faster. Ratios pair separate processes in reversed source-build order, selecting loop-memory-base ON in both binaries; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
