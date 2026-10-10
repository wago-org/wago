# Dominated indexed addresses: retained arm64 optimization

Enabled by default after three compilation-cost mitigations. General exact-address reuse through proven branch regions; no workload names, hashes, constants or argument values affect eligibility. `dominated-indexed-base=false` or `WAGO_ARM64_NO_DOMINATED_INDEXED_BASE=1` disables it. The legacy experiment environment alias accepts0 as disabled.

Repeated same-thread execution comparisons show roughly3% median-filtering and5% KNN improvement; Horspool improves about1.5%, while register allocation and several other cases are small/flat. Final compile overhead is2.66% median,2.68% KNN,4.42% Horspool and3.22% Rabin–Karp. Large upstream compile changes are mostly small except libtommath+3.37%; its execution is flat. These remaining costs are disclosed, not treated as gains. Twelve-round libtommath/monocypher execution confirmation is flat (-0.13%/-0.02%); the earlier monocypher slowdown did not repeat.

The initial backward/edge search incurred7–18% application compile overhead. Linear exact-seed propagation through forward joins reduced it; bounded byte reads, a branch-class filter and proven table indexes reduced compiler panicBounds sites18->7->2. The pass uses2KiB fixed stack scratch and limits functionsize4096bytes,branches128,seed distance64instructions. Backward/internal/wrapper entries begin unknown; unknown writes/calls, opaque/EH/custom streams are excluded. ADD-to-NOP preserves code size and control-flow layout.

Final default-on backend/catalog/runtime/encoder diagnostics and ordinary backend tests pass. All46core+102application oracles pass in BOTH signal and explicit bounds. All148default-on signal images match the qualified/measured prototype exactly. The full parity objective remains unfinished; this is one retained general step.

# Experiment record

Default off, `dominated-indexed-base` / `WAGO_ARM64_EXPERIMENT_DOMINATED_INDEXED_BASE=1`. Size-preserving ADD-to-NOP rewrite over finalized direct control flow. An identical earlier ADD X16,base,index must dominate all incoming paths, while every intervening instruction preserves X16 and both inputs. Outside entries after the seed (including backedges/BL) reject the rewrite. Calls, unknown/writeback operations, indirect fragments, EH and custom instructions are excluded. Function size4096bytes,128edges,64instruction lookback; edge workspace stays on the stack.

Initial diagnostic backend/catalog/encoder suite passes. Unit cases cover diamond/outside entry, seed entry, backedge bypass, protectedW/Xwrites, call/indirect/unknown/writeback invalidation, edge/function/window limits. Native memory fixture covers both paths and bounds at varied addresses. Independent write-class and branch displacement goldens were subsequently added and await qualification. Full signal corpus oracle checks and affected-image inventory queued; no timing claim.

## Initial qualification passed

Independent decoder goldens and diagnostic suite pass. All46core and102application signal oracles pass.18core and8application images change. Every differing native word is exactly ADD X16,base,index,LSL#0 -> NOP; all sizes and other words match baseline. This confirms there is no incidental layout/branch relocation change. Register-allocation removes13address computations, median69, KNN36, KMeans30, Horspool82, KMP24, Rabin-Karp80, Gini28. Retained baseline uses arithmetic-zero-off-core and interval-call-retained-app images.

Added repeated native loop paths with alternating field stores, zero/odd/even trip counts, varied addresses, both bounds/onoff; pending focused job diagnostics. Queued same-thread alternating compile/exec comparison for all8affected applications and core compile controls (fastfloat unchanged, kissfft/PCRE2 affected), plus paired kissfft execution. No retention claim.

## Original measured tradeoff

| Workload | Phase | Change |
| --- | --- | ---: |
| compiler-register-allocation | compile | +3.52% |
| image-median | compile | +14.64% |
| ml-kmeans | compile | +6.86% |
| ml-knn | compile | +7.96% |
| search-horspool | compile | +17.87% |
| search-kmp | compile | +6.38% |
| search-rabin-karp | compile | +11.06% |
| stats-gini | compile | +3.19% |
| compile-match | compile | +0.62% |
| complex-roundtrip | compile | +2.09% |
| decimal-parse | compile | +2.10% |
| compiler-register-allocation | exec | -0.46% |
| image-median | exec | -2.04% |
| ml-kmeans | exec | -0.76% |
| ml-knn | exec | -3.69% |
| search-horspool | exec | -1.46% |
| search-kmp | exec | -0.39% |
| search-rabin-karp | exec | -0.03% |
| stats-gini | exec | -0.41% |
| complex-roundtrip | exec | +0.57% |

Compilation +7–18% on several applications for small execution effects is insufficient. Mitigation integrated default off: replace repeated backward/incoming-edge scans with linear exact-seed propagation through forward joins; backward and extra wrapper/internal entries begin unknown. Preserve4KiB/128edge/64instruction distance limits and2KiB fixed stack workspace. Eligibility may change; fresh all148oraclechecks in both bounds plus focused eight-application/core comparisons queued. Original measured source preserved in dominated-indexed-base-prototype/original-measured.go.disabled. Candidate-list drafts remain unintegrated, superseded by this linear approach.

## Linear propagation measured and confirmed

All148oracles pass in both bounds; independent join/extra-entry adversarial tests pass. Native scope18core/8apps;101/102app images identical to original version,37/46core identical. Tables below distinguish first samples and confirmation.

| Batch | Workload | Phase | Change |
| --- | --- | --- | ---: |
| dominated-indexed-base-linear-paired-compile.jsonl | compiler-register-allocation | compile | +2.90% |
| dominated-indexed-base-linear-paired-compile.jsonl | image-median | compile | +4.01% |
| dominated-indexed-base-linear-paired-compile.jsonl | ml-kmeans | compile | +4.26% |
| dominated-indexed-base-linear-paired-compile.jsonl | ml-knn | compile | +3.76% |
| dominated-indexed-base-linear-paired-compile.jsonl | search-horspool | compile | +6.87% |
| dominated-indexed-base-linear-paired-compile.jsonl | search-kmp | compile | +4.70% |
| dominated-indexed-base-linear-paired-compile.jsonl | search-rabin-karp | compile | +5.19% |
| dominated-indexed-base-linear-paired-compile.jsonl | stats-gini | compile | +2.84% |
| dominated-indexed-base-linear-core-paired-exec.jsonl | complex-roundtrip | exec | +0.39% |
| dominated-indexed-base-linear-core-paired-compile.jsonl | compile-match | compile | +0.70% |
| dominated-indexed-base-linear-core-paired-compile.jsonl | complex-roundtrip | compile | +1.08% |
| dominated-indexed-base-linear-core-paired-compile.jsonl | decimal-parse | compile | -0.07% |
| dominated-indexed-base-linear-paired-exec.jsonl | compiler-register-allocation | exec | -0.55% |
| dominated-indexed-base-linear-paired-exec.jsonl | image-median | exec | -3.76% |
| dominated-indexed-base-linear-paired-exec.jsonl | ml-kmeans | exec | -1.77% |
| dominated-indexed-base-linear-paired-exec.jsonl | ml-knn | exec | -4.70% |
| dominated-indexed-base-linear-paired-exec.jsonl | search-horspool | exec | -2.08% |
| dominated-indexed-base-linear-paired-exec.jsonl | search-kmp | exec | -0.73% |
| dominated-indexed-base-linear-paired-exec.jsonl | search-rabin-karp | exec | -0.05% |
| dominated-indexed-base-linear-paired-exec.jsonl | stats-gini | exec | -0.50% |
| dominated-indexed-base-linear-confirm-exec.jsonl | image-median | exec | -3.14% |
| dominated-indexed-base-linear-confirm-exec.jsonl | ml-knn | exec | -5.01% |
| dominated-indexed-base-linear-confirm-exec.jsonl | search-horspool | exec | -1.49% |
| dominated-indexed-base-linear-confirm-exec.jsonl | search-rabin-karp | exec | -0.18% |
| dominated-indexed-base-linear-confirm-compile.jsonl | image-median | compile | +4.09% |
| dominated-indexed-base-linear-confirm-compile.jsonl | ml-knn | compile | +3.80% |
| dominated-indexed-base-linear-confirm-compile.jsonl | search-horspool | compile | +5.90% |
| dominated-indexed-base-linear-confirm-compile.jsonl | search-rabin-karp | compile | +4.52% |
| dominated-indexed-base-linear-upstream-compile.jsonl | aead-blake2b | compile | +2.00% |
| dominated-indexed-base-linear-upstream-compile.jsonl | compile-match | compile | +0.36% |
| dominated-indexed-base-linear-upstream-compile.jsonl | modular-arithmetic | compile | +5.23% |
| dominated-indexed-base-linear-upstream-compile.jsonl | parse-edit-write | compile | +0.31% |
| dominated-indexed-base-linear-upstream-exec.jsonl | aead-blake2b | exec | +1.51% |
| dominated-indexed-base-linear-upstream-exec.jsonl | compile-match | exec | -0.36% |
| dominated-indexed-base-linear-upstream-exec.jsonl | modular-arithmetic | exec | +0.14% |
| dominated-indexed-base-linear-upstream-exec.jsonl | parse-edit-write | exec | +0.64% |

KNN/median gains repeat, but remaining compilation penalties and flat upstream execution require further mitigation. Compiler objdump shows repeated runtime.panicBounds paths on instruction reads. Captured bounded byte slices, four-byte LittleEndian reads/writes and a cheap direct-branch class filter integrated; pending diagnostics and all148native-image identity checks against measured linear version, then compile-only focused comparison. Guest execution is not remeasured unless images differ. Default remains off.

## Instruction-read mitigation passed

Diagnostics and all148signaloracles pass; all148nativeimages byte-identical to measured linear version. Goobjdump runtime.panicBounds sites18->7. Compile-only pairs:

| Workload | Disabled µs | Enabled µs | Change |
| --- | ---: | ---: | ---: |
| image-median | 75.387244 | 77.409120 | +2.68% |
| ml-knn | 65.272169 | 67.239537 | +3.01% |
| search-horspool | 72.841161 | 76.424502 | +4.92% |
| search-rabin-karp | 124.143506 | 127.753685 | +2.91% |
| aead-blake2b | 748.437256 | 754.812662 | +0.85% |
| compile-match | 9048.256553 | 9079.372789 | +0.34% |
| modular-arithmetic | 1987.117985 | 2053.247240 | +3.33% |
| parse-edit-write | 6679.256250 | 6684.495133 | +0.08% |

Further bounded-index cleanup integrated default off: canonical positive-step loop limits and explicit10bit instruction-table indexing, valid because functionsize<=4096bytes and all accepted PCs are aligned/inrange. Queued session2005 for diagnostics/all148nativeidentity then compile-only comparisons. No repeated execution timing needed if images remain identical. Retention undecided.

## Final bounded-index cleanup qualified

Diagnostics/all148signaloracles pass and all148native images match the byte-slice mitigation exactly. Compiler panicBounds sites7->2. Compile-only results:

| Workload | Change |
| --- | ---: |
| image-median | +2.66% |
| ml-knn | +2.68% |
| search-horspool | +4.42% |
| search-rabin-karp | +3.22% |
| aead-blake2b | +0.63% |
| compile-match | +0.16% |
| modular-arithmetic | +3.37% |
| parse-edit-write | +0.69% |

Retained execution estimates remain applicable because native images are unchanged. Two-workload12round300ms core execution repeat for libtommath/monocypher is queued before deciding retention (session40538). Final default-policy verification script prepared but not run; feature remains default off.
