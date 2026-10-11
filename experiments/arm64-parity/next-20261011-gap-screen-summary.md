| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| audio-fir | compile | 6 | 76.6510 | 76.7856 | +0.12% | -0.05% to +1.00% | +0.18% |
| audio-fir | exec | 6 | 23.7282 | 23.6181 | +0.43% | -1.71% to +1.02% | -0.46% |
| files-path-normalize | compile | 6 | 76.8767 | 78.5065 | +2.85% | -2.21% to +9.85% | +2.12% |
| files-path-normalize | exec | 6 | 14.2055 | 14.4049 | +0.98% | -0.16% to +1.42% | +1.40% |
| grid-pathfinding | compile | 6 | 59.1619 | 58.1617 | -1.83% | -3.80% to -0.72% | -1.69% |
| grid-pathfinding | exec | 6 | 38.8301 | 38.4970 | -0.17% | -1.96% to +0.75% | -0.86% |
| hardware-boolean-anf | compile | 6 | 66.3462 | 66.4310 | +0.04% | -0.20% to +0.24% | +0.13% |
| hardware-boolean-anf | exec | 6 | 73.3955 | 73.2936 | -0.10% | -1.39% to +0.36% | -0.14% |
| hardware-prime-implicants | compile | 6 | 50.8188 | 49.6496 | +0.06% | -4.76% to +2.33% | -2.30% |
| hardware-prime-implicants | exec | 6 | 48.3901 | 48.2192 | -0.32% | -0.92% to +0.24% | -0.35% |
| ml-convolution | compile | 6 | 38.1554 | 38.4065 | +0.26% | -2.72% to +0.69% | +0.66% |
| ml-convolution | exec | 6 | 24.1136 | 24.0330 | -0.36% | -0.85% to +0.32% | -0.33% |
| ml-inference | compile | 6 | 35.7107 | 35.9457 | +0.50% | -0.01% to +0.94% | +0.66% |
| ml-inference | exec | 6 | 5.8608 | 5.8348 | -0.47% | -0.79% to -0.43% | -0.44% |
| ml-kmeans | compile | 6 | 78.9228 | 79.2261 | +0.12% | -0.06% to +0.48% | +0.38% |
| ml-kmeans | exec | 6 | 24.9235 | 24.9232 | -0.00% | -0.63% to +19.89% | -0.00% |
| search-aho-corasick | compile | 6 | 197.7774 | 197.6590 | +0.18% | -0.23% to +0.46% | -0.06% |
| search-aho-corasick | exec | 6 | 49.5234 | 48.6326 | -2.26% | -6.07% to +0.32% | -1.80% |
| serialization-protobuf | compile | 6 | 39.3305 | 35.6055 | +1.08% | +0.75% to +1.32% | -9.47% |
| serialization-protobuf | exec | 6 | 16.9026 | 16.9445 | -0.07% | -1.34% to +2.90% | +0.25% |
| stats-bootstrap | compile | 6 | 38.0225 | 37.9791 | +0.05% | -0.47% to +0.26% | -0.11% |
| stats-bootstrap | exec | 6 | 21.0373 | 21.0526 | -0.15% | -0.44% to +0.11% | +0.07% |
| stats-linear-regression | compile | 6 | 29.0540 | 29.0004 | +0.34% | -0.04% to +0.51% | -0.18% |
| stats-linear-regression | exec | 6 | 1.7855 | 1.7840 | +0.18% | -0.40% to +0.56% | -0.08% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
