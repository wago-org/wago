| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| audio-adpcm | compile | 8 | 23.4877 | 23.8784 | +2.80% | +0.61% to +3.89% | +1.66% |
| audio-adpcm | exec | 8 | 36.8426 | 36.8737 | +0.16% | -0.08% to +0.51% | +0.08% |
| audio-autocorrelation | compile | 8 | 35.1832 | 36.2040 | +2.72% | +0.29% to +3.03% | +2.90% |
| audio-autocorrelation | exec | 8 | 42.5449 | 42.3276 | +0.03% | -0.87% to +0.43% | -0.51% |
| bio-global-alignment | compile | 8 | 44.0492 | 45.4988 | +2.95% | +1.64% to +3.28% | +3.29% |
| bio-global-alignment | exec | 8 | 62.7003 | 61.9955 | -1.49% | -2.07% to -0.68% | -1.12% |
| bio-kmer-count | compile | 8 | 26.4826 | 26.9375 | +2.27% | +1.48% to +2.83% | +1.72% |
| bio-kmer-count | exec | 8 | 35.2404 | 34.0567 | -4.69% | -5.31% to -3.46% | -3.36% |
| compiler-register-allocation | compile | 8 | 66.2623 | 66.9431 | +0.83% | +0.51% to +2.63% | +1.03% |
| compiler-register-allocation | exec | 8 | 16.8475 | 16.8037 | -0.29% | -0.57% to +0.01% | -0.26% |
| geo-point-in-polygon | compile | 8 | 32.6385 | 33.4980 | +0.42% | -0.04% to +3.63% | +2.63% |
| geo-point-in-polygon | exec | 8 | 21.3361 | 21.3581 | +0.03% | -0.32% to +0.59% | +0.10% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
