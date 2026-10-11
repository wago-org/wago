# Rejected dispatch-only borrowed mask read

Apple M4 Max, arm64; source `37174169` plus the separately accepted bounded-branch-index candidate. This experiment is rejected. Main production source and the accepted bounded-index archive were untouched; only this experiment's hooks/helper/tests were removed from its isolated checkout `/tmp/wago-branch-index-read-37174169` after archiving.

The native VM profile showed `MOV X9,X19; AND W9,W9,#7` immediately before executable-vector dispatch. The broader borrowed-logical-immediate optimization had already regressed VM. This separate consumer seam admitted only a direct i32 AND of a borrowed local/global and an encodable constant, and only when the existing small unique zero-argument executable vector qualified. A fresh owned destination was pinned through the normal stack flush. The existing three-operand sink emitted the immediate AND directly; non-vector consumers and owned/variable-mask expressions retained normal materialization. Operand order, dirty low-i32 interpretation, source preservation and older discarded traps remained intact.

| Variant | Pairs × budget/state | Exec paired median | Exec paired IQR | Exec pooled delta | Compile paired median | Compile paired IQR | Compile pooled delta |
|---|---|---:|---|---:|---:|---|---:|
| Remove the MOV | 8 × 150 ms | +2.60% | +1.50% to +3.56% | +3.24% | -0.09% | -1.29% to +0.04% | -0.49% |
| Preserve its slot with NOP | 10 × 250 ms | -0.24% | -0.64% to +0.73% | -0.30% | -0.27% | -0.75% to +1.42% | -1.16% |

Negative means faster. The unpadded VM shrinks1224→1220B but regresses. The mitigation restores1224B and every instruction position, changing only `+0x1cc: MOV→NOP` and the AND's source at`+0x1d0: W9→W19`; its execution gain does not repeat clearly enough to retain. This supports rejecting both versions after a material layout mitigation. It does not assign the sampled MOV observations a CPU-cycle cost.

Initial ADPCM, dilation and register-allocation native images are byte-identical across states; no execution change on those controls is attributed to this candidate. Their full raw results remain in the screen files. The feature-OFF VM matches the accepted bounded-index image exactly.

Strict diagnostics/native fixtures pass in both variants. Fixtures check constant operand commutation, encodable/unencodable/high/zero/full masks, source observation after dispatch, dirty incoming carriers, older discarded division traps, owned/variable-mask rejection, duplicate tables, vector OFF, compact policy and three-op-sink rollback, with feature ON/OFF and both bounds policies. No full148 qualification, source-cross comparison or profiler capture was run for this rejected follow-up; those are not implied by the separately qualified bounded-index feature.

The retained evidence here includes both source versions, fixtures, the layout variant's integration patch relative to the accepted bounded-index source, all execution/compilation samples and summaries, native image proofs/images, and original scripts. Frozen binaries remain `/tmp/paired-branch-index-read` and `/tmp/paired-branch-index-read-layout`. Scripts refer to the historical isolated checkout and require restoring the archived experiment to reproduce. Heavy runs used `~/benchmark-lock.py --wait --owner counted-next`; reservations completed and released normally.
