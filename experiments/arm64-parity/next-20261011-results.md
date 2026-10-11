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
