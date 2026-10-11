| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| audio-adpcm | compile | 12 | 24.3072 | 24.1833 | -0.11% | -1.01% to +0.92% | -0.51% |
| audio-adpcm | exec | 12 | 36.9705 | 29.7724 | -19.48% | -19.74% to -19.16% | -19.47% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
