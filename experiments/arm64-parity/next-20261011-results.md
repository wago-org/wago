# ARM64 retained changes after 37174169

Apple M4 Max, arm64. Five general optimizations are retained after `37174169`: guarded boolean XOR, taken zero branches, protected integer select reads, bounded branch indices and CSET boolean lineage. Each has exact semantic/native fixtures, a catalog option and an environment rollback. Focused timing and actual-source comparisons are recorded below; no full timing sweep was run.

The final CSET change reuses the original comparison flags when a materialized boolean is later compared with zero for an integer select. It retains the original boolean value, proves the replaced comparison's flags dead, and preserves instruction count and memory access order. Its bounded scan shares the existing branch-target preflight and adds no function fields. [Decision, source, strict fixtures, all raw samples and profile](cset-lineage/README.md).

| Final incremental CSET source comparison | Steady execution | Compilation |
|---|---:|---:|
| Polyline simplification | −18.86% | +1.14%, approximately 0.43µs |
| Particle physics | −19.97% | +2.47%, approximately 0.56µs |
| KMeans | −3.44% | +5.49%, approximately 4.15µs |
| Prime implicants | +0.61% | +5.15%, approximately 2.30µs |
| LibTomMath | −0.47% | −1.98% |
| NanoSVG | +0.31% | +0.02% |

These are six reversed 150ms source pairs against the preceding retained source, with common loop-memory-base enabled on both. The source comparison excludes bounded dispatch, whose only changed image is register VM; all compared images match their corresponding integrated versions. The two large-module compile costs observed in the first CSET prototype disappear after preflight mitigation. KMeans and prime-implicant compilation costs remain. Smaller execution movements receive no gain credit.

The CSET addition changes 14 size-preserved images, all matching independently timed and qualified images. Its rollback restores every one of the preceding four-default images, including the existing 1224B register VM. All 148 exact contracts pass with default ON and rollback OFF in both bounds modes, and plain/diagnostic backend, catalog, encoder and runtime suites pass. [Qualified source image proof](cset-lineage/retained/native-proof.txt). Disable with `WAGO_ARM64_NO_CSET_LINEAGE=1` or its per-compilation option.

The [focused ranking](next-20261011-retained-subset.md) incorporates fresh CSET captures for every affected image; older values are used only where the final image is unchanged. The complete historical database report remains intact. The following sections preserve the preceding four decisions and their measurements.

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

The fourth retained option, `bounded-branch-index`, captures an immediate i32 constant or AND-mask upper bound before normal branch-index materialization. Unsigned `x & mask` cannot exceed the mask, so an in-range bound makes the executable branch vector's default check unnecessary. Both operand orders retain ordinary evaluation and pending traps; no fact survives a local assignment or join. The existing branch-vector admission boundary and opaque fixed-stride entries remain intact. [Decision, strict fixtures and profile](bounded-branch-index/README.md).

Only the register VM changes after `1bb0757b`, from 1232B to 1224B, exactly matching the separately timed and profiled candidate. All 148 contracts pass both bounds modes and full compiler/runtime diagnostics. Of all four changes combined, 34 signal images change and 114 remain pristine `37174169`-identical; all four options disabled reproduce every pristine image. [Integrated proof](next-20261011-dispatch-retain-native-proof.txt), [diagnostics](next-20261011-dispatch-retain-diagnostics.txt).

The isolated dispatch change repeats at −6.74% execution across ten300ms pairs and −4.19% against actual pristine source, with +1.60% target compile cost (approximately0.54µs). A final four150ms reversed source comparison of all four defaults confirms VM −7.26% paired/−7.33% pooled and ANF −10.45%/−10.07%. Compilation is VM +0.12%/+0.51%, ANF +2.69%/+2.18%, glob +3.14%/+2.62% and register allocation +0.51%/+0.29%; source-screen dispersion is retained. Glob's native image is unchanged, so its execution movement receives no gain credit. Register-allocation execution is +1.72% in this short source screen and gets a focused incremental selector check. [Latest source comparison](next-20261011-dispatch-cross-summary.md), [timed-image proof](next-20261011-dispatch-cross-native-proof.txt).

The priority ranking includes the four-default VM capture and updated source controls. Rollback is `WAGO_ARM64_NO_BOUNDED_BRANCH_INDEX=1` or its per-compilation option. The dispatch operand-read experiment was rejected; CSET-lineage subsequently qualified as described above.

The focused ten200ms selector control check leaves register allocation flat at −0.58% paired (IQR −2.89% to +1.68%), so the preceding +1.72% source movement did not repeat as an incremental selector cost. Components is +0.79% (−1.09% to +2.10%). Prime implicants has a possible small +1.57% paired cost (+0.30% to +2.87%), while its pooled ratio is −0.72%; no execution gain is credited to these controls. [Control dispersion and raw observations](next-20261011-select-control-summary.md).

Wrapping experiments are archived with their source, strict checks, measurements and limitations: [dispatch operand read](../rejected-branch-index-read/README.md), [select condition read](../rejected-select-local-condition-read/README.md), [literal memory index](../rejected-literal-memory-index/README.md) and [widening dot product](../rejected-widening-dot/README.md). None of those prototypes is imported into production.
