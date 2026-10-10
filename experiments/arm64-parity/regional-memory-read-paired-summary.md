| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| compiler-register-allocation | compile | 8 | 66.6374 | 66.5565 | -0.27% | -0.80% to -0.05% | -0.12% |
| compiler-register-allocation | exec | 8 | 17.3483 | 17.1291 | -1.15% | -2.39% to +0.39% | -1.26% |
| decimal-parse | compile | 8 | 1191.3234 | 1192.3574 | -0.31% | -0.76% to +0.65% | +0.09% |
| decimal-parse | exec | 8 | 0.6783 | 0.6720 | -0.96% | -1.34% to -0.47% | -0.93% |
| graphics-reed-solomon | compile | 8 | 103.9609 | 102.5736 | -1.56% | -2.36% to -0.66% | -1.33% |
| graphics-reed-solomon | exec | 8 | 123.8443 | 118.6017 | -4.21% | -4.31% to -3.73% | -4.23% |
| modular-arithmetic | compile | 8 | 2191.1369 | 2219.8861 | +0.62% | -1.66% to +1.61% | +1.31% |
| modular-arithmetic | exec | 8 | 11.8162 | 11.9055 | +1.15% | +0.61% to +22.60% | +0.76% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
