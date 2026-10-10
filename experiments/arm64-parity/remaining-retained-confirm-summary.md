| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| audio-adpcm | exec | 8 | 40.2045 | 39.2797 | -0.69% | -3.44% to +2.11% | -2.30% |
| language-register-vm | exec | 8 | 11.2575 | 10.7642 | -4.28% | -5.22% to -3.07% | -4.38% |
| ml-knn | exec | 8 | 30.5124 | 30.5250 | +0.02% | -0.56% to +0.96% | +0.04% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
