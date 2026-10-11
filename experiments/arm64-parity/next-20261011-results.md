# ARM64 retained changes after 37174169

Apple M4 Max, arm64. Two general native-code optimizations are enabled by default after `37174169`. Each was measured against the actual pristine source compiler in reversed process order. These are focused comparisons, not a full timing sweep.

| Optimization | Changed workload | Source-build steady exec | Source-build compile | Native size |
|---|---|---:|---:|---:|
| `guarded-boolean-xor` | `applications/geo-point-in-polygon` | −4.10% paired / −3.80% pooled | +6.40% / +6.43%, approximately 2.13µs | 640B, unchanged |
| `taken-zero-branch` | `applications/hardware-boolean-anf` | −10.94% paired / −10.90% pooled | Final mitigated +1.46% / +0.68% | 1904B, unchanged |

The boolean XOR pass replaces a bounded integer guard with a conditional compare after proving speculative scratch writes and any newly cleared high bits unobservable. The branch pass redirects a taken zero-test edge past a second same-width test of the same register. Both reject exception/custom/opaque code. Admission depends on instructions and compiler state, with no corpus names or inputs.

Candidate preflights share the existing branch-target scan. The XOR pass also memoizes joined proof states. These mitigations reduced unnecessary compilation work; pristine-source controls still show possible small costs. XOR ADPCM/glob compilation increased approximately 1–2%; final branch controls were +0.86% ADPCM, +1.43% register allocation and +2.47% glob in paired ratios, with dispersion and pooled results preserved. Unchanged native images receive no execution-gain credit. [XOR evidence](guarded-boolean-xor/README.md), [branch evidence](taken-zero-branch/README.md).

Combined default-on validation passes all 148 exact contracts in both signal and explicit bounds modes, plain compiler/catalog/encoder tests, and compiler/runtime diagnostics. Only the two application images above change; each is byte-identical to its independently timed and qualified image. Disabling both options reproduces all 148 baseline signal-mode images byte for byte. [Combined image proof](next-20261011-retain-native-proof.txt), [validation script](next-20261011-retain-verify.sh), [plain tests](next-20261011-retain-plain.txt), [diagnostics](next-20261011-retain-diagnostics.txt).

Fresh prepared profiler captures pass their exact oracles and match the qualified native images. Leaf observations locate instructions; they do not measure cycles or branch frequencies. The [32-workload priority subset](next-20261011-remaining-subset.md) records the preceding `37174169` baseline against historical w2c2 captures. It remains a dated baseline until focused retained-source measurements update it.

Rollback options are `guarded-boolean-xor` and `taken-zero-branch`, or environment variables `WAGO_ARM64_NO_GUARDED_BOOLEAN_XOR=1` and `WAGO_ARM64_NO_TAKEN_ZERO_BRANCH=1`.

All builds, exact checks, profiles and timings used the shared device lock. Timing harnesses request interactive QoS and lock an OS thread; they do not establish physical-core affinity.

The subsequent default-on `integer-select-local-read` borrows protected operands for materialized local select sinks. It uses the declared destination width and releases owned temporaries after CMP/CSEL; the flags-select path retains its existing lowering. The helper is 41 lines, with no prepass, function fields or corpus recognition. [Isolated decision and evidence](integer-select-local-read/README.md).

Combined validation again passes all 148 exact contracts in both bounds modes and full compiler/runtime diagnostics. There are 33 changed signal-mode images and 115 baseline-identical images. Boolean ANF combines the isolated 1888B select image with exactly seven same-register/same-width branch retargets; all other combined images exactly match independently qualified images. All three options disabled reproduce every `37174169` signal image. [Combined proof](next-20261011-select-retain-native-proof.txt), [diagnostics](next-20261011-select-retain-diagnostics.txt).

Ten alternating 300ms pairs against `76e52be8` confirm the selector's incremental execution gains: **KMeans −6.63%** (IQR −7.03% to −6.03%) and **clipping −6.11%** (−6.70% to −3.17%). Boolean ANF is flat at +0.22% (−2.09% to +1.50%), so its earlier gain remains supported without crediting the select change. Focused incremental compilation is flat on polygon, ANF and glob. [Incremental results](next-20261011-select-incremental-summary.md), [timed-image proof](next-20261011-select-incremental-native-proof.txt).

Four reversed 150ms source pairs compare all three defaults with pristine `37174169`: KMeans −6.63%, clipping −7.41%, ANF −7.72% and polygon −7.03% paired execution. Source compilation is KMeans +0.06%, clipping +1.44%, ANF +1.27%, polygon +7.37%; polygon has a large preserved outlier and +11.07% pooled cost, so the small source screen is less precise than the individual retained-pass checks. Register-allocation/glob compile costs are approximately +3% paired; ADPCM +1.12% paired/−0.96% pooled. ADPCM/glob native images are unchanged and receive no execution-gain credit. [Source results and dispersion](next-20261011-select-cross-summary.md), [raw samples](next-20261011-select-cross-build.jsonl).

The refreshed [54-workload ranking](next-20261011-retained-subset.md) combines these integrated measurements, final affected-select captures with identical combined images, and older unchanged-image captures. The original complete historical ranking remains intact. Conventional selector rollback is `WAGO_ARM64_NO_INTEGER_SELECT_LOCAL_READ=1` or its per-compilation option.
