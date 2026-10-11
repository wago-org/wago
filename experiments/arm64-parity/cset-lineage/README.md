# CSET boolean lineage flags, Apple M4 Max arm64

Retain `cset-lineage`, default ON with `WAGO_ARM64_NO_CSET_LINEAGE=1` rollback. The incremental production patch is [cset-lineage-retained.patch](cset-lineage-retained.patch), based on `7257c1c0`; it adds a catalog definition, binding, strict per-compilation policy fixture, bounded native pass and semantic/native fixtures. It does not change floating-point behavior, instruction layout, register clobbers, memory access order or source maps.

The general seam is a materialized boolean whose original NZCV remains intact: `CMP; CSET bool; [transparent words/MOV copies]; CMP Wbool,0; CSEL EQ/NE`. Keep the CSET for other uses, replace only the redundant boolean CMP with NOP, and map the CSEL to the CSET's semantic condition (or inverse). Exact canonical encodings, register writes, entries and flag epochs are tracked. Every changed consumer also proves the old CMP's flags dead before a later flag reader, following at most twelve known instructions/unconditional branches to an overwrite or RET. Unknown instructions, calls, SIMD, unsupported memory forms, opaque fragments and EH decline. Function size is capped at 4096B; producer-to-consumer distance is capped at 128B.

The existing mandatory branch-target scan first seeks a CSET, then a later CMP0/CSEL pair, and stops preflight work once both are present. The proof scan starts no earlier than 128B before that first CSET with unknown facts: any older producer cannot satisfy the consumer distance cap. There are no new function fields, module metadata or heap tables. This mitigation removes the initial large-module compiler penalty while preserving every qualified candidate image.

Actual-source comparison uses frozen `/tmp/paired-cset-lineage-base` (76e52be8 plus the retained local-read selector) against the final cheap preflight candidate, six reversed process pairs at 150ms, with common `loop-memory-base` ON on both. The later bounded branch-index option in 7257 changes only register-VM output; these compared native images are byte-identical to the corresponding baseline and measured candidate inventories.

| Workload | Execution | Compilation | Baseline / candidate compile us |
|---|---:|---:|---:|
| ML-kmeans | −3.44% | +5.49% | 80.502 / 84.648 |
| Polyline simplification | −18.86% | +1.14% | 50.951 / 51.378 |
| Particle physics | −19.97% | +2.47% | 32.023 / 32.585 |
| Prime implicants | +0.61% | +5.15% | 49.507 / 51.804 |
| ADPCM, unchanged native | +0.38% | +1.07% | 24.953 / 24.999 |
| LibTomMath | −0.47% | −1.98% | 2258.058 / 2221.380 |
| NanoSVG | +0.31% | +0.02% | 3353.611 / 3356.987 |

The 14 affected-only first screen is in [subset/summary.md](subset/summary.md). It found no material execution regression beyond a +2.16% quickselect observation that becomes +0.16% in the source comparison. ML's initial within-process −7.02% observation moderates to the source comparison above. The first source cross contains two large ML execution outliers (+88%/−52%); that screen is preserved, and the six-pair final comparison supersedes it. Initial core compile penalties (LibTomMath +4.14%, NanoSVG +6.94%) disappear after the cheap preflight mitigation. Costs for ML and prime implicants remain explicit.

Qualification before the final default flip: all148 ON/OFF exact-result oracles pass in SIGNAL and explicit-bounds modes; all148 SIGNAL ON images exactly match the frozen timed/first-qualified candidate, and OFF images reproduce the pristine three-option source baseline. Relative to that source, 134 images are unchanged and 14 change by exactly 72 words/36 adjacent compare-select pairs, all size-preserved: twelve apps and LibTomMath/NanoSVG. [inventory/changes.json](inventory/changes.json) and [inventory/word-proof.txt](inventory/word-proof.txt) identify every change.

Plain ARM64 backend tests pass. Full diagnostic backend/catalog/encoder/runtime tests pass with `wago_guardpage,wago_codegenstats,wago_regalloccheck`. Strict fixtures execute all fourteen condition codes, both CSET/MOV/producer/result widths, dirty carriers, saved boolean copies, transparent stack loads/stores, destination overwrites, entries, unknown/flag-reading words, opaque/EH/size/window rejection and sequential consumers. A real non-inlined call keeps twelve I64 caller locals and an older stack value live while the callee preserves the original boolean; ON admission and OFF absence are asserted with diagnostics in both bounds modes.

The fresh three-second ML profiler capture in [final/profile](final/profile) passes exact1342930999 and matches the measured 2744B native image byte for byte. [final/profile-assembly.txt](final/profile-assembly.txt) shows all eleven redundant CMPs become NOPs and their consumers use HS while the original CSET LO values and memory operations remain. Sampling establishes coverage/image identity; the timing comparisons establish performance.

The `first`, `inventory`, `mitigate`, `subset`, `cross`, `cross-cheap`, and `final` folders retain raw measurements, images, scripts and checks. Final clean default-ON/current-four-option rollback qualification also passes: [retained/strict-diagnostics.txt](retained/strict-diagnostics.txt), all148 exact ON/OFF in both bounds modes, and [retained/native-proof.txt](retained/native-proof.txt). All14 changed default images match the frozen candidate; the other134 preserve the currentfour baseline, including its existing1224B register-VM dispatch. All148 rollback images exactly match the currentfour baseline. All heavy commands used the shared device lock (`division-flags`); no full timing sweep was run.

Final source tree: `/tmp/wago-cset-lineage-retained-7257c1c0`. Frozen retained qualification binaries: `/tmp/parity-cset-lineage-retained-{signal,explicit}.test`. Production `source` contains no CSET-lineage experimental environment hook; earlier prototype source/scripts remain archived as evidence. The recorded source-pair percentages are medians of ratios; the absolute columns are separate medians.
