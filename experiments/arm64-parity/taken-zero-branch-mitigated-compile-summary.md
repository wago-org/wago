| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| audio-adpcm | compile | 8 | 23.8530 | 23.9622 | +0.03% | -0.67% to +0.62% | +0.46% |
| compile-match | compile | 8 | 9046.9766 | 9033.5482 | -0.17% | -0.47% to +0.43% | -0.15% |
| compiler-register-allocation | compile | 8 | 67.6089 | 67.4301 | -0.05% | -0.42% to +0.27% | -0.26% |
| complex-roundtrip | compile | 8 | 893.6364 | 903.4967 | +1.36% | +0.49% to +2.27% | +1.10% |
| decimal-parse | compile | 8 | 1187.4111 | 1196.7769 | +0.70% | -0.67% to +1.49% | +0.79% |
| files-glob-match | compile | 8 | 26.7526 | 26.8227 | +0.31% | -0.30% to +0.56% | +0.26% |
| hardware-boolean-anf | compile | 8 | 66.4065 | 66.6010 | +0.08% | -0.14% to +1.04% | +0.29% |
| modular-arithmetic | compile | 8 | 2243.4610 | 2268.4990 | +1.05% | -0.59% to +1.83% | +1.12% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
