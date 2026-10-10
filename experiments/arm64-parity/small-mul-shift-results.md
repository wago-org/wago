# Constant multiplication shift/add prototype

Apple M4 Max arm64. Opt-in `WAGO_ARM64_EXPERIMENT_SMALL_MUL_SHIFT=1`; default remains off. Extends the existing 3/5/9 lowering to modular constants `(2^n + 1)*2^k`, using one shifted ADD and at most one LSL. Does not change function-wide literal selection. Scoped leases were disabled for this experiment.

Native-execution checks cover both integer widths, overflow, dirty i32 carriers, borrowed source reuse, and deferred multiplicands. The first test incorrectly asserted i32 carrier high bits even when the disabled baseline simplified multiplication to an identity. It was corrected to compare the defined low 32 value bits. Qualification then passed backend/encoder/runtime diagnostics and all 148 signal-mode oracles.

Native comparison identifies 11 affected core contracts and 9 applications. The initial broad subset showed fastfloat flat (+0.08% execution, -1.11% compilation). Components, register allocation, ADPCM, Gini, and Otsu images are unchanged; their timing variation is not evidence of a native optimization gain or regression. Aho and DCT names were absent from the initial requested regex, so they are not counted as tested there.

The queued next batch verifies all 148 disabled native images against the retained baseline, then times only the 20 affected contracts with reverse on/off/off/on ordering, 400 ms, count 2. No retention or default-enable decision has been made. Explicit-mode qualification is still pending.

## Affected-case reversed batch

Completed on/off/off/on, 400ms, two samples per state per pass. All 148 off-state signal contracts passed and native images exactly matched the retained baseline. Values below are medians of four samples per state; negative deltas improve latency. Cross-process compile outliers require confirmation.

| Case | Compile delta | Exec delta |
|---|---:|---:|
| Parity/audio-biquad | +2.46% | +1.85% |
| Parity/bezier-tessellation | +1.96% | +0.90% |
| Parity/files-path-trie | +3.08% | +5.67% |
| Parity/map-point-segment | +0.40% | -2.34% |
| Parity/mesh-skinning | +3.19% | +0.80% |
| Parity/search-aho-corasick | +4.01% | -4.25% |
| Parity/search-bk-tree | -2.48% | -0.06% |
| Parity/video-optical-flow | -0.95% | -0.31% |
| Parity/video-yuv420 | +0.11% | +1.54% |
| wago_coremark_2k-performance | +0.35% | +0.29% |
| wago_drwav_pcm-decode-seek | -20.10% | -0.33% |
| wago_fastfloat_decimal-parse | -0.36% | +0.17% |
| wago_json-as-simd_serializeN | +6.62% | +3.31% |
| wago_kissfft_complex-roundtrip | +0.79% | +0.21% |
| wago_nanosvg_parse-structure | +0.39% | +0.31% |
| wago_pcre2_compile-match | +32.10% | +0.45% |
| wago_utf-as-simd_validateN | +0.16% | -0.11% |
| wago_utf8proc_normalize-casefold | -0.67% | +1.06% |
| wago_yyjson_parse-edit-write | +1.04% | +2.08% |
| wago_zstd_decompress | +1.29% | -0.64% |

Not retained. Next mitigation: separate single-ADD forms from forms requiring a second shift, then remeasure the affected subset. This is an instruction-cost rule, independent of corpus identity.

## Rejected after mitigation

The one-instruction-only mitigation passed diagnostic arithmetic checks and completed the same affected 20-case reversed batch. The earlier Aho gain disappeared (-0.16%), point-segment regressed +4.08%, and no large consistent execution improvement remained. Several results fluctuate by 1–2%; mitigation-specific native-change attribution was not completed, so do not interpret these as proven code effects. Compile results are cross-process and noisy. The experiment does not justify enabling a broader strength-reduction rule. Production lowering restored to the original 3/5/9 special cases; prototype and arithmetic checks archived under `experiments/rejected-small-mul-shift/`. Final restored code is checked against the previously qualified scoped-enabled image set.
