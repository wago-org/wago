| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| bio-lcs | compile | 8 | 43.3671 | 44.8037 | +3.71% | +3.08% to +4.11% | +3.31% |
| bio-lcs | exec | 8 | 58.1632 | 54.9546 | -5.34% | -5.85% to -5.13% | -5.52% |
| document-edit-distance | compile | 8 | 46.0854 | 47.5380 | +3.10% | +2.36% to +4.11% | +3.15% |
| document-edit-distance | exec | 8 | 78.6124 | 81.7469 | +3.57% | +2.95% to +4.26% | +3.99% |
| grid-pathfinding | compile | 8 | 58.0116 | 59.8884 | +3.78% | +2.48% to +4.71% | +3.24% |
| grid-pathfinding | exec | 8 | 35.4461 | 35.3737 | -0.38% | -0.71% to +0.07% | -0.20% |
| serialization-protobuf | compile | 8 | 36.4733 | 36.8294 | +1.32% | -0.90% to +3.07% | +0.98% |
| serialization-protobuf | exec | 8 | 16.4609 | 16.5072 | +0.29% | -0.27% to +52.84% | +0.28% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
