| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| language-register-vm | compile | 10 | 42.9498 | 42.4535 | -0.27% | -0.75% to +1.42% | -1.16% |
| language-register-vm | exec | 10 | 10.3750 | 10.3442 | -0.24% | -0.64% to +0.73% | -0.30% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
