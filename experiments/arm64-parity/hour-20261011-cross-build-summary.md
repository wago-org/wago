| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| audio-adpcm | compile | 4 | 24.0281 | 24.5087 | +2.44% | +1.30% to +3.04% | +2.00% |
| audio-adpcm | exec | 4 | 36.9520 | 29.6349 | -19.28% | -19.67% to -18.64% | -19.80% |
| compiler-register-allocation | compile | 4 | 66.8736 | 67.2737 | +0.54% | +0.36% to +0.96% | +0.60% |
| compiler-register-allocation | exec | 4 | 16.7578 | 16.7261 | -0.21% | -1.46% to -0.17% | -0.19% |
| files-glob-match | compile | 4 | 26.4356 | 26.9427 | +2.08% | +1.76% to +2.32% | +1.92% |
| files-glob-match | exec | 4 | 133.0083 | 129.1976 | -3.42% | -4.63% to -2.38% | -2.86% |
| geo-point-in-polygon | compile | 4 | 33.3307 | 34.1436 | +2.44% | +1.60% to +3.22% | +2.44% |
| geo-point-in-polygon | exec | 4 | 21.4845 | 21.3600 | -0.34% | -0.62% to -0.11% | -0.58% |
| graphics-reed-solomon | compile | 4 | 101.6872 | 101.5608 | -1.11% | -3.17% to +0.14% | -0.12% |
| graphics-reed-solomon | exec | 4 | 119.7116 | 115.0616 | -3.83% | -4.20% to -3.72% | -3.88% |
| image-median | compile | 4 | 82.8141 | 82.1887 | -0.96% | -1.65% to -0.19% | -0.76% |
| image-median | exec | 4 | 312.5930 | 284.5273 | -8.85% | -10.99% to -6.72% | -8.98% |
| video-dct | compile | 4 | 135.2065 | 135.6685 | +0.23% | -0.55% to +1.65% | +0.34% |
| video-dct | exec | 4 | 41.4964 | 39.7039 | -4.28% | -4.63% to -4.20% | -4.32% |
| vision-components | compile | 4 | 55.8179 | 56.7803 | +1.01% | +0.56% to +1.83% | +1.72% |
| vision-components | exec | 4 | 37.1094 | 36.3904 | -1.78% | -2.18% to -1.27% | -1.94% |

Negative deltas mean faster. These paired ratios use separate processes in reversed source-build order; pooled medians and interquartile ranges are reported too. The comparison selects loop-memory-base ON in both binaries. Each raw process also retains its unused OFF state. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
