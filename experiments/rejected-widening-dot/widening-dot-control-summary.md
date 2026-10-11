| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| compiler-register-allocation | compile | 6 | 68.5182 | 68.7467 | +0.28% | -0.53% to +0.94% | +0.33% |
| compiler-register-allocation | exec | 6 | 16.6602 | 16.5623 | -0.77% | -1.39% to -0.01% | -0.59% |
| graphics-reed-solomon | compile | 6 | 100.7203 | 100.6852 | +0.07% | -0.04% to +0.24% | -0.03% |
| graphics-reed-solomon | exec | 6 | 115.8913 | 115.5622 | -0.14% | -0.83% to +3.44% | -0.28% |
| vision-dilation | compile | 6 | 44.3636 | 44.6664 | +0.65% | +0.58% to +0.77% | +0.68% |
| vision-dilation | exec | 6 | 43.5545 | 42.7754 | -0.15% | -2.69% to +0.21% | -1.79% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
