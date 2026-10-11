| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| 2k-performance | compile | 8 | 444.8982 | 443.7436 | +0.98% | -3.57% to +1.58% | -0.26% |
| compile-match | compile | 8 | 9272.3284 | 9276.6131 | +0.34% | -0.13% to +0.81% | +0.05% |
| compiler-register-allocation | compile | 8 | 68.1466 | 68.4123 | -0.26% | -0.89% to +2.25% | +0.39% |
| compiler-register-allocation | exec | 8 | 16.9131 | 16.7575 | -1.36% | -2.18% to -0.32% | -0.92% |
| complex-roundtrip | compile | 8 | 973.0354 | 969.3206 | -1.47% | -2.96% to +0.24% | -0.38% |
| complex-roundtrip | exec | 8 | 159.3836 | 159.7304 | +0.44% | -0.08% to +0.80% | +0.22% |
| image-median | compile | 8 | 83.9329 | 85.5679 | +0.66% | -0.05% to +2.25% | +1.95% |
| image-median | exec | 8 | 269.3890 | 255.9759 | -4.75% | -6.03% to -2.92% | -4.98% |
| ml-kmeans | compile | 8 | 78.9181 | 78.7148 | -0.54% | -1.97% to +0.22% | -0.26% |
| ml-kmeans | exec | 8 | 24.8152 | 24.8099 | +0.03% | -0.46% to +0.26% | -0.02% |
| ml-knn | compile | 8 | 70.5843 | 70.1295 | -0.18% | -1.11% to +0.09% | -0.64% |
| ml-knn | exec | 8 | 30.1757 | 30.5304 | +1.67% | +0.61% to +2.00% | +1.18% |
| modular-arithmetic | compile | 8 | 2253.6478 | 2248.5165 | -0.75% | -1.33% to +1.24% | -0.23% |
| modular-arithmetic | exec | 8 | 11.8331 | 11.9038 | +0.54% | +0.39% to +0.78% | +0.60% |
| search-horspool | compile | 8 | 80.0098 | 80.2050 | +0.67% | -0.56% to +3.86% | +0.24% |
| search-horspool | exec | 8 | 76.2984 | 75.4353 | -0.79% | -1.62% to -0.37% | -1.13% |
| search-rabin-karp | compile | 8 | 124.2911 | 125.6708 | +0.65% | +0.58% to +1.08% | +1.11% |
| search-rabin-karp | exec | 8 | 230.8794 | 234.9606 | +1.00% | -0.35% to +2.62% | +1.77% |
| stats-gini | compile | 8 | 80.3601 | 80.7482 | +0.35% | -0.06% to +1.93% | +0.48% |
| stats-gini | exec | 8 | 3.9530 | 3.9599 | +0.14% | -0.79% to +1.32% | +0.18% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
