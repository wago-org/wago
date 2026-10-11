| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| audio-adpcm | compile | 8 | 24.9874 | 24.7765 | -1.11% | -1.26% to -0.25% | -0.84% |
| files-glob-match | compile | 8 | 28.4024 | 27.9801 | +0.28% | -1.20% to +0.69% | -1.49% |
| geo-point-in-polygon | compile | 8 | 38.1884 | 38.4104 | +0.05% | -1.08% to +1.08% | +0.58% |
| geo-polygon-clipping | exec | 10 | 30.8452 | 29.2585 | -6.11% | -6.70% to -3.17% | -5.14% |
| hardware-boolean-anf | compile | 8 | 70.6351 | 70.7896 | -0.50% | -1.59% to +0.74% | +0.22% |
| hardware-boolean-anf | exec | 10 | 68.0736 | 68.3813 | +0.22% | -2.09% to +1.50% | +0.45% |
| ml-kmeans | exec | 10 | 25.0712 | 23.4765 | -6.63% | -7.03% to -6.03% | -6.36% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
