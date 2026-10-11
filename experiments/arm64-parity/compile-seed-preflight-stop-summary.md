| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| audio-adpcm | compile | 4 | 24.5409 | 24.3946 | -1.00% | -2.04% to +0.19% | -0.60% |
| compiler-register-allocation | compile | 4 | 68.4017 | 69.4094 | +1.01% | -0.13% to +2.19% | +1.47% |
| files-glob-match | compile | 4 | 27.0343 | 27.2021 | +0.26% | -0.21% to +0.98% | +0.62% |
| geo-point-in-polygon | compile | 4 | 34.4996 | 34.0631 | -2.30% | -2.73% to -1.45% | -1.27% |
| graphics-reed-solomon | compile | 4 | 101.6293 | 102.4243 | +0.77% | +0.22% to +1.67% | +0.78% |
| video-dct | compile | 4 | 147.3308 | 146.9788 | -1.11% | -3.19% to +0.71% | -0.24% |

Negative deltas mean faster. These ratios pair separate processes in reversed source-build order, selecting loop-memory-base ON in both binaries; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
