| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| db-btree | compile | 8 | 68.6455 | 71.2666 | +3.99% | +3.58% to +4.04% | +3.82% |
| db-btree | exec | 8 | 10.3777 | 10.3693 | +1.69% | -0.34% to +7.04% | -0.08% |
| decimal-parse | compile | 4 | 1306.3525 | 1307.7289 | +0.11% | -0.73% to +0.58% | +0.11% |
| decimal-parse | exec | 8 | 0.6823 | 0.6854 | +0.48% | +0.19% to +0.64% | +0.46% |
| document-edit-distance | compile | 8 | 45.2822 | 46.8227 | +3.47% | +3.36% to +3.56% | +3.40% |
| document-edit-distance | exec | 8 | 76.8898 | 78.9337 | +2.56% | +2.36% to +2.90% | +2.66% |
| document-page-packing | compile | 8 | 47.0010 | 48.2035 | +2.58% | +2.10% to +2.82% | +2.56% |
| document-page-packing | exec | 8 | 30.5725 | 30.6692 | -0.06% | -0.26% to +0.64% | +0.32% |
| inflate | compile | 4 | 924.5299 | 929.5629 | +0.87% | -1.48% to +3.15% | +0.54% |
| parse-edit-write | compile | 4 | 7043.2664 | 7056.5982 | +0.29% | -0.27% to +0.51% | +0.19% |
| parse-edit-write | exec | 8 | 0.7766 | 0.7747 | -0.08% | -0.46% to +0.35% | -0.25% |
| serialization-protobuf | compile | 8 | 34.9887 | 36.0244 | +2.80% | +2.56% to +3.47% | +2.96% |
| serialization-protobuf | exec | 8 | 16.8156 | 16.7711 | +0.09% | -0.76% to +0.48% | -0.26% |

Negative deltas mean faster. Paired ratios retain the alternating same-thread comparisons; pooled medians and interquartile ranges are reported too. Raw JSONL retains every observation. OS-thread locking and QoS do not establish physical-core affinity.
