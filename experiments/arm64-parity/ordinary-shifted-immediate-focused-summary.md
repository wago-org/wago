| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| audio-adpcm | compile | 6 | 22.8586 | 23.1495 | +1.71% | +0.64% to +2.32% | +1.27% |
| audio-adpcm | exec | 6 | 36.4281 | 38.4300 | +5.45% | +5.00% to +6.16% | +5.50% |
| audio-fir | compile | 6 | 74.7911 | 74.4647 | -0.22% | -0.64% to -0.09% | -0.44% |
| audio-fir | exec | 6 | 23.7956 | 23.9089 | +0.54% | +0.00% to +0.76% | +0.48% |
| compile-match | compile | 6 | 8979.4805 | 8985.1914 | +0.15% | -0.28% to +1.77% | +0.06% |
| compile-match | exec | 6 | 7.9987 | 8.0262 | +0.56% | +0.22% to +1.62% | +0.34% |
| compiler-constant-fold | compile | 6 | 35.0390 | 35.0323 | -0.19% | -0.65% to +0.43% | -0.02% |
| compiler-constant-fold | exec | 6 | 1.7810 | 1.7716 | -1.02% | -1.46% to +0.02% | -0.53% |
| compiler-register-allocation | compile | 6 | 65.3685 | 65.4324 | +0.59% | +0.17% to +1.47% | +0.10% |
| compiler-register-allocation | exec | 6 | 16.6772 | 16.5944 | -0.54% | -0.84% to +0.21% | -0.50% |
| compression-lzw | compile | 6 | 25.1557 | 25.0047 | -0.10% | -0.49% to +1.06% | -0.60% |
| compression-lzw | exec | 6 | 341.3636 | 337.8515 | -0.13% | -3.87% to +0.74% | -1.03% |
| files-glob-match | compile | 6 | 26.8553 | 27.0705 | +1.14% | +0.78% to +1.58% | +0.80% |
| files-glob-match | exec | 6 | 131.5680 | 127.9513 | -2.68% | -3.08% to -1.56% | -2.75% |
| files-merkle-tree | compile | 6 | 53.2128 | 53.2302 | +0.04% | -0.11% to +0.38% | +0.03% |
| files-merkle-tree | exec | 6 | 6.8893 | 6.8587 | -0.20% | -0.37% to +0.04% | -0.44% |
| geo-polygon-clipping | compile | 6 | 71.2865 | 71.9197 | +1.46% | -0.27% to +1.73% | +0.89% |
| geo-polygon-clipping | exec | 6 | 30.7294 | 30.5326 | -0.46% | -0.98% to -0.06% | -0.64% |
| grid-pathfinding | compile | 6 | 54.8939 | 54.8589 | +0.14% | -0.01% to +0.43% | -0.06% |
| grid-pathfinding | exec | 6 | 36.7974 | 35.9569 | +0.17% | -4.68% to +1.30% | -2.28% |
| modular-arithmetic | compile | 6 | 2186.6152 | 2194.8486 | +0.21% | -1.04% to +0.42% | +0.38% |
| modular-arithmetic | exec | 6 | 11.6318 | 11.6590 | +0.23% | -0.07% to +0.33% | +0.23% |
| search-bk-tree | compile | 6 | 49.8020 | 49.4278 | -0.25% | -1.35% to +1.13% | -0.75% |
| search-bk-tree | exec | 6 | 157.5868 | 158.7729 | +0.74% | +0.31% to +8.71% | +0.75% |
| video-dct | compile | 6 | 143.2558 | 144.0104 | +0.55% | +0.19% to +0.96% | +0.53% |
| video-dct | exec | 6 | 41.7290 | 41.5963 | -0.46% | -0.58% to +0.17% | -0.32% |
| vision-components | compile | 6 | 55.7550 | 54.4263 | -2.19% | -2.67% to -1.71% | -2.38% |
| vision-components | exec | 6 | 37.1611 | 36.4586 | -1.90% | -2.43% to -1.74% | -1.89% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
