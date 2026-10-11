| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| audio-adpcm | compile | 8 | 24.0382 | 24.5992 | +2.74% | +1.81% to +3.08% | +2.33% |
| audio-adpcm | exec | 8 | 37.6506 | 38.2746 | +1.58% | -0.95% to +2.74% | +1.66% |
| audio-autocorrelation | compile | 8 | 36.6261 | 37.5142 | +2.42% | +2.12% to +2.75% | +2.42% |
| audio-autocorrelation | exec | 8 | 43.1921 | 43.3546 | +0.68% | -0.23% to +1.69% | +0.38% |
| audio-biquad | compile | 8 | 21.8846 | 22.1654 | +1.40% | -0.75% to +2.21% | +1.28% |
| audio-biquad | exec | 8 | 23.4831 | 23.4533 | +0.11% | -1.40% to +0.90% | -0.13% |
| bio-global-alignment | compile | 8 | 44.5357 | 45.9025 | +2.43% | +1.95% to +3.19% | +3.07% |
| bio-global-alignment | exec | 8 | 66.0022 | 64.9967 | -1.63% | -2.51% to -1.02% | -1.52% |
| bio-kmer-count | compile | 8 | 27.6021 | 28.4463 | +2.79% | +2.17% to +3.40% | +3.06% |
| bio-kmer-count | exec | 8 | 35.8360 | 35.9661 | +0.28% | -0.52% to +1.21% | +0.36% |
| compiler-register-allocation | compile | 8 | 69.1644 | 70.6679 | +2.88% | +1.58% to +3.71% | +2.17% |
| compiler-register-allocation | exec | 8 | 17.1019 | 17.0349 | -0.17% | -1.82% to +0.31% | -0.39% |
| geo-point-in-polygon | compile | 8 | 34.0902 | 35.8192 | +4.89% | +4.22% to +5.72% | +5.07% |
| geo-point-in-polygon | exec | 8 | 21.7026 | 21.7545 | -0.03% | -1.86% to +0.88% | +0.24% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
