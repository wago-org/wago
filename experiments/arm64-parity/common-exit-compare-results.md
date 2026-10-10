# Common-exit compare pairs: arm64

General size-preserving compare folding. Retained, default on after final verification; earlier prototype was default off. Disable with per-compilation `common-exit-compare=false`, `WAGO_ARM64_NO_COMMON_EXIT_COMPARE=1`, or `WAGO_ARM64_EXPERIMENT_COMMON_EXIT_COMPARE=0`. Two consecutive integer comparison branches sharing an exit become a conditional compare and one branch, preserving size withNOP. No GPR changes, memory reordering, or workload-specific admission. Second-immediateSP, large/nonencodableCCMP immediates, shifted second comparisons, external entries, live output flags, opaque/EH/custom streams are excluded.

Initial diagnostic suite passes; independent condition/NZCV truth tables, CMP/CMN register/immediate goldens and guard exclusions pass. Native fixtures cover100comparison combinations, bothwidths, signedboundaries/dirtycarriers, bothbounds and per-compilation overrides. Strict diagnostic admission confirms the native fixture actually uses the pass. Real custom instruction-map and extra-entry exclusions are independently checked.

All148signal/explicit corpus qualification and affected-image inventory queued. Baseline is the newly retained dominated-indexed-base compiler. No performance claim yet.

## Qualification passed

All46core+102application oracles pass in BOTH signal and explicit bounds. Three application and eight upstream images change, allsizesunchanged. Applications: dilation, Bresenham, Rabin–Karp. Strict native admission and real custom/entry guards pass. Paired focused tradeoff queued for the three affected applications and selected affected core cases; no broad timed sweep. Feature remains default off.

## Initial focused timing

Same-thread paired medians against the retained dominated-indexed-base compiler:

| Workload | Execution change | Compilation change |
|---|---:|---:|
| Dilation | -2.55% | +0.90% |
| Bresenham | +0.61% | +1.37% |
| Rabin–Karp | +0.05% | +2.13% |
| Modular arithmetic | -0.14% | +2.54% |
| PCRE compile-match | +0.23% | +0.61% |
| UTF8 normalize-casefold | -1.51% | not measured |
| YYJSON parse-structure | not measured | +1.28% |
| KissFFT complex-roundtrip | not measured | +3.36% |

Application measurements use 12 rounds of 300ms; core measurements use 8 rounds of 200ms. Small changes need confirmation. Feature remains default off. Before accepting or rejecting, the scanner now checks the first comparison and first branch before reading the remaining words. This preserves rewrite eligibility and avoids unnecessary reads for most instructions. A focused correctness and timing batch is queued through the shared reservation; it will confirm dilation/Bresenham behavior and reassess compilation overhead.

## Scanner mitigation and repeat

Independent diagnostic correctness tests pass after prefiltering. All 148 signal-mode oracle checks pass, and every native image is byte-identical to the already-qualified original candidate. This preserves its both-bounds qualification.

| Workload | Execution change | Compilation change |
|---|---:|---:|
| Dilation | -5.36% | +0.78% |
| Bresenham | +0.22% | +1.22% |
| Rabin–Karp | -0.56% | +0.74% |
| Modular arithmetic | not repeated | +1.35% |
| KissFFT complex-roundtrip | not repeated | +0.66% |

Application timing repeats use 12 rounds of 300ms; core compilation repeats use 8 rounds of 200ms. Dilation gains repeat (initial -2.55%, repeat -5.36%), while the other application execution results remain small. The prefilter reduces the observed compilation overhead to roughly 0.7–1.4%. Since emitted code is identical, the larger repeat execution gain is measurement variation, not a new execution improvement from the scanner edit.

Default-on verification is queued: diagnostics, runtime/catalog/encoder checks, all 148 exact oracles in both bounds modes, native identity to the measured prefilter candidate, and ordinary backend tests. No whole-corpus timing sweep is required for this retention check.

## Retained verification passed

The default-on compiler passes all 46 core and 102 application exact oracles in both signal and explicit bounds modes. All 148 default-on signal native images match the measured prefilter candidate. Backend diagnostics, optimization catalog, runtime, encoder and ordinary backend tests pass. Frozen retained compilers: `/tmp/parity-common-exit-compare-retained.test` and `/tmp/parity-common-exit-compare-retained-explicit.test`; native baseline directories: `/tmp/common-exit-compare-retained-core` and `/tmp/common-exit-compare-retained-app`.

Retained because the repeated meaningful execution gain comes with modest compilation overhead after mitigation. No general parity claim: most remaining gaps still require further work.
