| Workload | Phase | Pairs | Off median µs | On median µs | Median paired delta | Paired IQR | Pooled median delta |
|---|---|---:|---:|---:|---:|---:|---:|
| wago/drwav/pcm-decode-seek | compile | 4 | 1769.1410 | 1764.2565 | -0.85% | -1.25% to -0.24% | -0.28% |
| wago/lz4/compress | compile | 4 | 143.4090 | 143.5620 | +0.19% | +0.11% to +0.41% | +0.11% |
| wago/monocypher/aead-blake2b | compile | 4 | 763.0335 | 760.9145 | -0.29% | -0.55% to +0.05% | -0.28% |
| wago/nanosvg/parse-structure | compile | 4 | 3214.6610 | 3182.8220 | -0.99% | -2.12% to -0.23% | -0.99% |
| wago/pcre2/compile-match | compile | 4 | 9218.5715 | 9317.5555 | +1.00% | +0.23% to +1.75% | +1.07% |
| wago/sha256/hashN | compile | 4 | 87.5890 | 87.3305 | +0.15% | -0.16% to +0.20% | -0.30% |
| wago/utf8proc/normalize-casefold | compile | 4 | 780.6940 | 779.8725 | -0.11% | -1.01% to +0.73% | -0.11% |
| wago/zlib/inflate | compile | 4 | 825.2005 | 821.9780 | -0.02% | -0.91% to +0.64% | -0.39% |

Negative deltas mean faster. Each ratio pairs separate-process off/on observations in reversed order, using the frozen narrowed candidate and four100ms pairs. Compilation includes release; this phase does not execute the guest. The original exact contracts were verified in the signal qualification and execution screen. Process reversal does not establish physical-core affinity or remove GC/scheduling variability. Raw JSONL and each process output retain timing, B/op and allocation counts.
