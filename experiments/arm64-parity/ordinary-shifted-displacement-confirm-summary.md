| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| compression-lzw | compile | 10 | 26.3609 | 26.3803 | -0.24% | -1.32% to +0.82% | +0.07% |
| compression-lzw | exec | 10 | 338.4887 | 339.3301 | +0.19% | -0.02% to +0.29% | +0.25% |
| document-edit-distance | compile | 10 | 44.7939 | 44.7604 | -0.23% | -0.78% to -0.01% | -0.07% |
| document-edit-distance | exec | 10 | 77.1468 | 72.3044 | -5.97% | -6.34% to -5.85% | -6.28% |
| files-glob-match | compile | 10 | 27.2037 | 27.0603 | -0.67% | -1.60% to +0.01% | -0.53% |
| files-glob-match | exec | 10 | 133.4328 | 130.0190 | -2.99% | -3.37% to -2.16% | -2.56% |
| stencil-wave-equation | compile | 10 | 64.8842 | 64.3640 | -0.52% | -0.95% to -0.46% | -0.80% |
| stencil-wave-equation | exec | 10 | 65.1549 | 62.5711 | -3.91% | -4.10% to -3.33% | -3.97% |
| triangle-raster | compile | 10 | 37.5990 | 37.5093 | -0.56% | -1.13% to +0.64% | -0.24% |
| triangle-raster | exec | 10 | 144.2408 | 132.1323 | -8.37% | -8.84% to -7.45% | -8.39% |
| video-optical-flow | compile | 10 | 79.2275 | 79.6930 | -0.40% | -0.68% to +2.16% | +0.59% |
| video-optical-flow | exec | 10 | 19.3419 | 18.5448 | -4.37% | -4.54% to -3.28% | -4.12% |
| vision-components | compile | 10 | 57.7440 | 58.4667 | +0.18% | -0.39% to +1.01% | +1.25% |
| vision-components | exec | 10 | 37.2452 | 36.5551 | -1.85% | -2.61% to -0.42% | -1.85% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
