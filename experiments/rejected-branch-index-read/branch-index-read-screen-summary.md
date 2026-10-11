| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| audio-adpcm | compile | 8 | 24.5884 | 24.5622 | -0.05% | -0.22% to +0.47% | -0.11% |
| audio-adpcm | exec | 8 | 30.2676 | 30.2194 | +0.16% | -1.87% to +2.68% | -0.16% |
| compiler-register-allocation | compile | 8 | 69.0758 | 69.3641 | +0.42% | -0.25% to +1.01% | +0.42% |
| compiler-register-allocation | exec | 8 | 17.1074 | 17.2938 | +0.15% | -0.69% to +1.34% | +1.09% |
| language-register-vm | compile | 8 | 41.4956 | 41.2923 | -0.09% | -1.29% to +0.04% | -0.49% |
| language-register-vm | exec | 8 | 10.3331 | 10.6677 | +2.60% | +1.50% to +3.56% | +3.24% |
| vision-dilation | compile | 8 | 45.5104 | 45.4815 | +0.46% | -0.26% to +3.07% | -0.06% |
| vision-dilation | exec | 8 | 43.3764 | 43.0666 | -1.57% | -2.31% to +0.84% | -0.71% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
