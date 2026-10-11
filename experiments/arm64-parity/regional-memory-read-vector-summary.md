| BLAKE3 input bytes | Phase | Off median µs | On median µs | Median paired delta |
|---|---|---:|---:|---:|
| 1024 | compile | 765.0020 | 760.2430 | -0.71% |
| 1024 | exec | 1.1625 | 1.1760 | +1.21% |
| 16384 | compile | 785.7250 | 769.8015 | -0.73% |
| 16384 | exec | 15.8385 | 15.9765 | +1.14% |
| 102400 | compile | 762.8510 | 769.2495 | +2.29% |
| 102400 | exec | 99.5235 | 99.8855 | +0.36% |

Six alternating process pairs per length, exact byte oracle before and after execution. Separate-process harness results are exploratory; no physical-core affinity is established. Negative means faster. Raw observations are retained.
