| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| compile-match | compile | 8 | 9424.7093 | 9413.8472 | -0.18% | -0.69% to +0.50% | -0.12% |
| compile-match | exec | 8 | 8.1516 | 8.1844 | +0.57% | -0.19% to +0.87% | +0.40% |
| compiler-register-allocation | compile | 12 | 67.2989 | 67.1612 | -0.39% | -0.59% to -0.15% | -0.20% |
| compiler-register-allocation | exec | 12 | 17.0428 | 17.0514 | +0.08% | -0.16% to +0.60% | +0.05% |
| complex-roundtrip | compile | 8 | 935.1664 | 957.7309 | +1.99% | -2.19% to +48.26% | +2.41% |
| complex-roundtrip | exec | 8 | 157.6018 | 157.9800 | +0.34% | -0.08% to +0.51% | +0.24% |
| image-median | compile | 12 | 81.1579 | 80.9764 | -0.34% | -0.97% to +0.67% | -0.22% |
| image-median | exec | 12 | 317.8172 | 301.1780 | -5.98% | -8.34% to -2.54% | -5.24% |
| ml-knn | compile | 12 | 67.6502 | 68.8646 | +1.22% | +0.20% to +2.64% | +1.80% |
| ml-knn | exec | 12 | 30.7282 | 30.7137 | -1.09% | -2.17% to +0.55% | -0.05% |
| modular-arithmetic | compile | 8 | 2229.7375 | 2218.9495 | -1.70% | -2.38% to +0.60% | -0.48% |
| modular-arithmetic | exec | 8 | 11.8808 | 11.8699 | -0.20% | -0.34% to +0.14% | -0.09% |
| render | compile | 8 | 231.0261 | 233.5055 | +0.84% | -0.05% to +1.88% | +1.07% |
| render | exec | 8 | 248.2343 | 248.2532 | +0.05% | -0.38% to +0.87% | +0.01% |
| search-rabin-karp | compile | 12 | 127.2708 | 128.3913 | +0.67% | -0.50% to +2.80% | +0.88% |
| search-rabin-karp | exec | 12 | 230.6746 | 230.2732 | +0.01% | -0.48% to +0.45% | -0.17% |
| step | compile | 8 | 85.7862 | 85.4833 | +0.42% | -0.56% to +8.73% | -0.35% |
| step | exec | 8 | 136.1717 | 136.4946 | +0.06% | -0.37% to +1.40% | +0.24% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
