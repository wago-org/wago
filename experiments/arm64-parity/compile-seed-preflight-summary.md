| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| audio-adpcm | compile | 4 | 25.8437 | 24.7781 | -3.63% | -5.24% to -1.18% | -4.12% |
| compiler-register-allocation | compile | 4 | 69.7280 | 70.1174 | +1.15% | -0.64% to +7.70% | +0.56% |
| files-glob-match | compile | 4 | 28.1679 | 28.2530 | -0.49% | -5.14% to +1.44% | +0.30% |
| geo-point-in-polygon | compile | 4 | 34.4494 | 34.7248 | +0.45% | -1.02% to +8.45% | +0.80% |
| graphics-reed-solomon | compile | 4 | 102.2357 | 105.5499 | +2.41% | -0.39% to +3.72% | +3.24% |
| video-dct | compile | 4 | 146.8709 | 147.4446 | +1.53% | +0.26% to +1.77% | +0.39% |

Negative deltas mean faster. These ratios pair separate processes in reversed source-build order, selecting loop-memory-base ON in both binaries; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
