| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| bio-lcs | compile | 12 | 41.6524 | 42.9572 | +2.66% | +1.56% to +4.52% | +3.13% |
| bio-lcs | exec | 12 | 60.0276 | 56.7112 | -5.71% | -6.32% to -5.25% | -5.52% |
| geo-point-in-polygon | compile | 12 | 32.3377 | 32.2745 | +0.09% | -0.42% to +0.97% | -0.20% |
| geo-point-in-polygon | exec | 12 | 21.0828 | 21.0254 | +0.11% | -0.48% to +0.32% | -0.27% |
| grid-pathfinding | compile | 12 | 54.4422 | 55.7744 | +2.46% | +2.32% to +2.94% | +2.45% |
| grid-pathfinding | exec | 12 | 35.4955 | 35.4202 | -0.19% | -0.69% to +0.09% | -0.21% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
