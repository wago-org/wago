# Scoped loop constant prototype: first affected subset

Apple M4 Max arm64; frozen `/tmp/parity-scoped-loop-const.test`; on/off/off/on, 300 ms, count 2. Median ns/op. Only cases with changed native images are included. Prototype remains opt-in; no retention decision yet.

| Case | Phase | Off ns | On ns | Change |
|---|---|---:|---:|---:|
| stats-gini | Compile | 64165.5 | 64525.5 | +0.56% |
| stats-gini | Exec | 4244.0 | 3927.5 | -7.46% |
| vision-otsu | Compile | 54209.0 | 52455.5 | -3.23% |
| vision-otsu | Exec | 9248.0 | 8574.0 | -7.29% |
| wago_kissfft_complex-roundtrip | Compile | 1090510.0 | 754245.5 | -30.84% |
| wago_kissfft_complex-roundtrip | Exec | 155580.0 | 155117.0 | -0.30% |
| wago_polybench-adi_polybench_run | Compile | 142109.0 | 149960.0 | +5.52% |
| wago_polybench-adi_polybench_run | Exec | 1366811.0 | 1372358.0 | +0.41% |
| wago_polybench-correlation_polybench_run | Compile | 116643.0 | 112213.5 | -3.80% |
| wago_polybench-correlation_polybench_run | Exec | 203335.5 | 200880.5 | -1.21% |
| wago_polybench-floyd-warshall_polybench_run | Compile | 79679.0 | 78625.5 | -1.32% |
| wago_polybench-floyd-warshall_polybench_run | Exec | 2791377.5 | 2795525.0 | +0.15% |
| wago_polybench-gemm_polybench_run | Compile | 106703.0 | 103185.0 | -3.30% |
| wago_polybench-gemm_polybench_run | Exec | 149533.0 | 145418.0 | -2.75% |
| wago_polybench-seidel-2d_polybench_run | Compile | 75093.5 | 76966.5 | +2.49% |
| wago_polybench-seidel-2d_polybench_run | Exec | 2871465.0 | 2876653.5 | +0.18% |
| wago_polybench-trisolv_polybench_run | Compile | 90361.0 | 90978.5 | +0.68% |
| wago_polybench-trisolv_polybench_run | Exec | 7135.0 | 7103.5 | -0.44% |
| wago_utf8proc_normalize-casefold | Compile | 746021.5 | 750768.0 | +0.64% |
| wago_utf8proc_normalize-casefold | Exec | 794.8 | 794.5 | -0.03% |

Gini/Otsu/GEMM are queued for a longer reverse-order repeat. Large kissfft compile variation needs caution. BLAKE3, CoreMark, JSON-AS, and QOI also change native images but are not timed by this simple-contract harness. All 148 signal-mode oracles and diagnostic core checks pass; explicit-mode qualification is queued. Fastfloat, PCRE2, and xxhash images are unchanged and their timing variation is not evidence of an execution improvement.

Longer reverse-order repeat (off/on/on/off, 600 ms, count 3) completed successfully:

| Case | Phase | Off ns | On ns | Change |
|---|---|---:|---:|---:|
| Gini | Compile | 63791.0 | 66624.5 | +4.44% |
| Gini | Exec | 4276.0 | 3848.5 | -10.00% |
| Otsu | Compile | 52297.0 | 54461.0 | +4.14% |
| Otsu | Exec | 9197.0 | 8564.0 | -6.88% |
| GEMM | Compile | 108411.0 | 106659.0 | -1.62% |
| GEMM | Exec | 148691.0 | 147444.0 | -0.84% |

All 46 cached core and 102 application explicit-mode oracles also passed. The first compile-cost mitigation is staged separately: reuse the constant decode in the existing loop scanner instead of decoding the same LEB twice. It has not been applied or measured yet. The remaining changed core contracts are queued with their host/memory oracles; BLAKE3 uses declared 1024, 16384, and 102400-byte vectors. No default enablement or parity claim follows from this prototype.

Remaining exact-result core contracts completed (off/on/on/off, 400 ms, count 2), with host initialization and exact memory oracles checked before and after timed execution:

| Case | Phase | Off ns | On ns | Change |
|---|---|---:|---:|---:|
| CoreMark | Compile | 402912.5 | 402513.0 | -0.10% |
| CoreMark | Exec | 26360823.0 | 26161284.5 | -0.76% |
| JSON-AS serializeN | Compile | 1551777.5 | 1534064.5 | -1.14% |
| JSON-AS serializeN | Exec | 13192.0 | 13211.5 | +0.15% |
| QOI encode | Compile | 158709.0 | 156472.0 | -1.41% |
| QOI encode | Exec | 4284.0 | 4282.0 | -0.05% |

These do not establish meaningful execution gains or regressions. The batch exited successfully. BLAKE3 is still queued. A subsequent reserved batch will apply the staged decode mitigation only if the production sources still match their captured hashes, require all 148 signal-mode native images to match the existing prototype byte-for-byte, and compare old/on, mitigated/on, and mitigated/off compile/exec costs on Gini, Otsu, and an unchanged fastfloat control.

The single-decode mitigation completed diagnostics and all 148 signal-mode oracles, with **all 148 native images byte-identical** to the original scoped prototype. A three-way off/old/new batch (500 ms, count 2, reverse ordering) kept Gini execution at -9.99% and Otsu at -6.95% against disabled; compile medians were -0.51% and -4.38%. The old/new compile comparison was inconsistent (+4.17% Gini, -3.37% Otsu), so this supports removal of redundant parsing, not a firm compile-speed claim. Fastfloat remained native-identical under this mitigation, and its execution variation is not an optimization gain.

The compiler admission probe found fastfloat function 1 has bulk scratch usage globally, 76 loops, 75 call-free loops, and 26 call-free loops matching selected literals. An opt-in local bulk-scratch proof rejects `usesBulkScratch` instructions inside the loop while permitting unrelated bulk instructions elsewhere. Backend diagnostics, cached-core diagnostics, and all 148 oracles in both signal and explicit modes passed. It changed 11 core native images and zero application images relative to the single-decode scoped prototype.

| Local bulk proof case | Phase | Old ns | New ns | Change |
|---|---|---:|---:|---:|
| Fastfloat | Compile | 1103805.5 | 1096917.0 | -0.62% |
| Fastfloat | Exec | 746.8 | 733.2 | -1.82% |
| PCRE2 (unchanged code) | Compile | 9361032.0 | 9507018.5 | +1.56% |
| PCRE2 (unchanged code) | Exec | 8172.5 | 8279.0 | +1.30% |
| xxhash (unchanged code) | Compile | 97566.0 | 97572.5 | +0.01% |
| xxhash (unchanged code) | Exec | 84459.5 | 84443.5 | -0.02% |

A fresh fastfloat profile is in `profile-scoped-local-bulk-fastfloat`; stack-local traffic remains prominent. The ten other changed core contracts are queued for focused timing.

BLAKE3's vector timing harness initially rejected an unspecified return list; this was corrected to enforce the declared exact output-byte contract. The retry exited successfully. At 1024/16384/102400 bytes, execution changed +1.00%/-1.17%/-0.28%, while compilation rose +3.99%/+2.89%/+5.08% under the scoped prototype. This is not a convincing BLAKE3 execution gain. A further general compile-cost mitigation now skips consumer analysis for unselected literals and literals already recorded by the current loop scan. Native-image equality and targeted timing are queued; neither scoped feature is enabled by default.

The selected-literal consumer filter completed successfully: backend diagnostics, all 148 signal-mode oracles, and all 148 native-image comparisons passed. Old/new compile changes were -8.83% Gini, -5.59% Otsu, -0.51% BLAKE3, and +0.53% fastfloat. Their native images are identical, so execution variation in this batch is not an execution optimization. These compile numbers remain subject to process/QoS variation.

The remaining ten loop-local bulk-proof comparisons also exited successfully. Execution changes ranged from -3.32% (SHA256) to +0.72% (JSON-AS); CoreMark, LU, Cholesky, and UTF validation were approximately flat. Monocypher was -1.48%, Nussinov -2.14%, Gram-Schmidt -1.08%, and utf8proc -1.13%. LU compile cost rose +4.66%; large Gram-Schmidt/Nussinov compile drops require caution and are not treated as proven gains.

The current source exposes `scoped-loop-int-const` as an immutable per-compilation optimization, experimental and **off by default**. Its enabled path always proves the loop contains no fixed-scratch helpers. Qualification of this policy integration and same-process, locked-OS-thread on/off comparisons for Gini, Otsu, Aho, and register allocation are queued. Default enablement remains undecided.

A further compile-cost concern exists even with scoped leases disabled: the frozen retained fastfloat baseline reports 298 allocations/~454.3 KiB per compilation, while pre-mitigation prototype binaries report 302 allocations/~455.2 KiB. Retaining `*funcHintView` in compiler scratch state is a plausible escape-analysis cause. The current source now copies only four literal values, their types/count, and flags into a 40-byte value instead of retaining the full view pointer. Control-frame size is unchanged. The pending policy qualification must still prove enabled/disabled native-image equality; a queued baseline/pre-mitigation/current compile-only comparison on Gini, Otsu, fastfloat, and PCRE2 will verify allocation and latency effects. This is not yet a measured allocation reduction.

## Immutable option and compact metadata qualification

Completed backend/encoder/runtime/catalog diagnostic tests, including signed and unsigned i32-to-i64 multiplication checks. All 148 signal contracts passed in both scoped states: enabled native images equal the prior use-filter prototype, disabled images equal the retained baseline. All 148 explicit-bounds contracts passed with scoped leases enabled.

Same-process, locked-OS-thread, user-interactive QoS comparisons (8 rounds, 250ms per state; OS thread locking does not pin a physical core):

| Workload | Compile delta | Exec delta |
|---|---:|---:|
| compiler-register-allocation | +0.14% | -0.14% |
| search-aho-corasick | +0.55% | -0.83% |
| stats-gini | +1.16% | -8.82% |
| vision-otsu | +0.83% | -6.42% |

Option remains default off while broader tradeoffs are evaluated. Compact disabled-path allocation comparison is running.

## Disabled-path allocation mitigation confirmed

The baseline/old/new/new/old/baseline compile batch completed successfully. Compact value metadata restores baseline allocation counts when scoped leases are disabled:

| Case | Baseline allocations | Pointer prototype | Compact value |
|---|---:|---:|---:|
| Gini | 135 | 139 | 135 |
| Otsu | 132 | 136 | 132 |
| fastfloat | 298 | 302 | 298 |
| PCRE2 | 1335 | 1457 | 1335 |

Bytes per compile also return approximately to baseline (Gini 48,526; Otsu 44,067; fastfloat 454,31x; PCRE2 3,723,xxx). The scan-local view escaping through the function's pointer was therefore a real allocation cost; storing only the selected integer hints by value eliminates it. Native code equality already passed in both states.

Timing medians new versus baseline: fastfloat -0.30%, PCRE2 +0.35%, Gini -8.80%, Otsu -7.36%. Cross-process application timings vary, so these are not firm compile speedup claims. The allocation reduction is deterministic across all four samples, and same-thread option comparisons above remain the better enablement-cost evidence.

## Retention decision

Core compile samples interleaved on/off on one locked OS thread, 10 rounds, 250ms per state. Execution oracles were qualified separately above.

| Core workload | Compile delta |
|---|---:|
| serializeN | +0.59% |
| 2k-performance | -0.58% |
| hash | +3.01% |
| decimal-parse | +2.31% |
| compile-match | -0.17% |

Retain as default-on `scoped-loop-int-const`: confirmed 6–9% application execution gains with roughly 1% paired compile cost, modest 2–3% costs for two core cases, no large paired compile regression. Both bounds modes qualified before retention. Final default-state diagnostics and native equality are running. The disabled-path allocation mitigation is part of the retained implementation.

Final retained-state checks completed: backend/encoder/runtime/catalog diagnostics passed, all 148 signal contracts passed, and all native images matched the previously qualified scoped-enabled image set. Explicit-bounds backend diagnostics and ordinary backend tests passed with `WAGO_SHARED_SCALAR=0`. The first ordinary run exposed an unconditional statistics request in the new execution test; it now requests stats only when diagnostics are compiled in. A diagnostic retry accidentally used the shared-scalar default and failed backend-specific assertions; the correctly configured dedicated-arm64 retry passed. Failure logs are preserved alongside the final successful logs.
