# Apple M4 Max arm64 optimization session

Authorized window: 2026-10-10 23:57:34 through 2026-10-11 00:57:34 UTC. Baseline compiler c74d21c1. Heavy work used short shared-device reservations and FIFO handoffs; foreign reservations were respected. WAGO_SHARED_SCALAR=0 throughout. Contracts and inputs are unchanged; compiler admission never reads corpus identity.

Five general changes are retained default on: immediate regional address borrowing, keyed dominated indexed addresses, exact bounded signed quotient lowering, bounded wide local reuse pins, and ordinary shifted displacement materialization. Each has a catalog option and explicit rollback.

## Integrated execution and compilation

All five options switch together against the hour-start policy in the same frozen compiler. Sixteen applications use eight alternating150ms pairs; three simple cores and BLAKE3's declared16KiB exact vector use six pairs. Exact result checks stay active. Vector bytes are checked after warmup and each timed sample outside the timer. No observations are dropped.

| Workload | Execution paired median | Compilation paired median |
|---|---:|---:|
| ADPCM | -18.63% | +0.28% |
| Triangle raster | -8.46% | -0.49% |
| Edit distance | -6.54% | +0.15% |
| Reed–Solomon | -5.19% | -0.02% |
| DCT | -4.46% | +1.33% |
| Wave equation | -4.26% | -0.88% |
| Optical flow | -3.89% | -0.20% |
| Connected components | -3.73% | -0.46% |
| Glob | -2.04% | -0.52% |
| Fastfloat | -1.55% | -0.69% |

Median filtering was noisy initially: -1.52% paired, IQR -5.22..+.90%, versus -4.82% pooled. Twelve400ms-pair repetition gives -3.99% paired, IQR -8.45..-.30%, pooled -4.97%. Standalone keyed confirmation was -5.98% with tighter IQR. These support a gain but not a stable integrated6% improvement. Register allocation, KNN, VM, dilation and kissfft are effectively flat/weak cases; no gain is credited from minor fluctuations. LZW +.11% and BLAKE3 +.45% are small execution tradeoffs (BLAKE3 pooled -.05%). The earlier isolated BLAKE3 cost was +.66%; its padding mitigation failed to remove that cost.

[Integrated table](hour-20261011-focused-summary.md), [median repetition](hour-20261011-median-confirm-summary.md). OS-thread locking and interactive QoS do not prove physical-core affinity.

A pristine source-build comparison uses c74d21c1 with identical harness plumbing. Four reversed100ms process pairs across eight applications confirm ADPCM -19.28%, DCT -4.28%, Reed -3.83%, glob -3.42%, components -1.78%. Median remains variable (-8.85% here). Source-build compilation reveals costs hidden by option toggles: ADPCM +2.44%, glob +2.08%, point-in-polygon +2.44%, components +1.01%; DCT +.23%, regalloc +.54%, median -.96%, Reed -1.11%. This short screen has process/GC/core variation; no zero unconditional compiler-cost claim is made. Roughly2–2.5% compile costs on some small cases are accepted alongside larger repeated execution gains; point-in-polygon has no execution gain. Every selected image matches its qualified current or pristine inventory. [Cross-build table](hour-20261011-cross-build-summary.md).

## Qualification and profiler

Combined default all148 exact contracts pass in signal and explicit bounds modes:102 applications and46 cached core contracts, including memory, host, initialization and vector oracles. Full diagnostic backend/catalog/runtime/encoder suites and ordinary suites pass. Strict quotient fixtures cover exact arithmetic, traps, source/destination and older stack aliases, pressure and compaction. Strict displacement fixtures verify actual ADD/SUB encoding, live-source survival, fallback, compact gating, widths and both bounds modes. The initial ordinary fixture Stats-request failure and its test-only correction are preserved.

With all five options disabled, all148 signal images exactly match c74d21c1. Combined defaults change17 core and37 application images. [Native proof](hour-20261011-native-proof.txt). Final fixtures and ordinary tests passed again after the strict admission test was added. Enabled correctness sweeps run once per bounds mode, followed by disabled image checks; there was no full timed corpus sweep.

The profiler informed quotient saturation, DCT resident-local selection and glob displacement materialization. Fresh integrated glob before/after3s prepared Samply captures pass exact oracles;420/416-byte images equal the timed states. The matcher replaces MOV X16,#65536 plus ADD W10,W21,W16 with ADD W10,W21,#16,LSL#12. Remaining copies between permanent homes at structured joins are a possible next investigation. Leaf observations identify instruction locations; they are not CPU-time weights or branch-frequency proofs. [Profile image proof](hour-20261011-profile-native-proof.txt); retained assembly and source maps are beside it.

## Decisions and remaining work

The broader shifted ALU/compare prototype caused ADPCM +5.45%; restricting it to read-only addDisp restores ADPCM/FIR/BK images while retaining repeated triangle/edit-distance/optical/wave/glob gains. Narrowed eight-core execution shows no severe loss; four reversed100ms compile pairs show PCRE2 +1.00%, with the other seven -0.99..+.19%. This modest cost is accepted; subtle losses are not excluded by short screens. [Displacement results](ordinary-shifted-displacement-results.md).

Isolated quotient gains roughly19.5–20.8%; wide pins gain DCT4.56% with about1% compile cost; keyed facts gain median5.98%; regional borrowing gains Reed4.42%/fastfloat3.06%. Individual evidence: [quotient](bounded-quotient-results.md), [wide pins](wide-reuse-pins/README.md), [keyed addresses](dominated-indexed-key-results.md), [regional borrowing](regional-memory-read-results.md).

Equality countdown is rejected after dead-counter admission, cheaper scanning and padding mitigation: LCS -5.71% is outweighed by affected median compilation +2.76%, near-flat median execution and reproducible edit-distance +2.56%; padding worsens that loss to +3.57%. Positive-divisor global proofs, invariant-only wide pins, widened multiply, branchless remainder and store-map budgeting were also rejected after documented mitigations. Disabled source, patches, failed variants, raw data and profiler evidence remain under experiments/rejected-* and arm64-parity rejected directories. No rejected hooks enter production.

[Fresh17-case remaining ranking](hour-20261011-remaining-subset.md) orders current execution against historical device-local w2c2. The complete146-case report remains historical; w2c2 was not rerun. Point-in-polygon, register allocation, glob and VM remain substantial priorities. This subset does not establish full-corpus parity; the original optimization goal remains unfinished.

A final cheap address-seed preflight was rejected after two compile-only mitigations. Counting all eligible seeds helps ADPCM3.63% but slows DCT1.53%/Reed2.41%; stopping after two seeds yields three small wins and three losses without a balanced result. The original finalized source is restored exactly; no new oracle or image-identity claim is made for these rejected trials. See compile-seed-preflight-results.md and compile-seed-preflight-stop-results.md. Their separate-process raws and disabled patches remain reproducible.
