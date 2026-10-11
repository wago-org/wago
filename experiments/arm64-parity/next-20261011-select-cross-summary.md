| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| audio-adpcm | compile | 4 | 25.1126 | 24.8728 | +1.12% | +0.01% to +1.45% | -0.96% |
| audio-adpcm | exec | 4 | 29.9284 | 30.4486 | +1.51% | +1.28% to +2.19% | +1.74% |
| compiler-register-allocation | compile | 4 | 68.9462 | 71.4067 | +3.12% | -0.95% to +3.86% | +3.57% |
| compiler-register-allocation | exec | 4 | 17.2163 | 17.1903 | -0.34% | -1.54% to +0.89% | -0.15% |
| files-glob-match | compile | 4 | 27.1244 | 27.7125 | +3.04% | +2.53% to +3.50% | +2.17% |
| files-glob-match | exec | 4 | 131.3339 | 131.6376 | +0.24% | -3.47% to +3.77% | +0.23% |
| geo-point-in-polygon | compile | 4 | 34.7431 | 38.5900 | +7.37% | +6.97% to +123.41% | +11.07% |
| geo-point-in-polygon | exec | 4 | 22.1379 | 20.8391 | -7.03% | -27.33% to -5.41% | -5.87% |
| geo-polygon-clipping | compile | 4 | 71.0148 | 73.4168 | +1.44% | -1.05% to +2.90% | +3.38% |
| geo-polygon-clipping | exec | 4 | 31.1692 | 28.9673 | -7.41% | -7.94% to -6.05% | -7.06% |
| hardware-boolean-anf | compile | 4 | 66.6873 | 68.5561 | +1.27% | -3.55% to +2.93% | +2.80% |
| hardware-boolean-anf | exec | 4 | 73.7204 | 67.6201 | -7.72% | -9.87% to -4.22% | -8.27% |
| ml-kmeans | compile | 4 | 79.3296 | 80.1368 | +0.06% | -1.58% to +2.28% | +1.02% |
| ml-kmeans | exec | 4 | 25.3655 | 23.7623 | -6.63% | -7.83% to -3.09% | -6.32% |

Negative deltas mean faster. Ratios pair separate processes in reversed source-build order, selecting loop-memory-base ON in both binaries; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
