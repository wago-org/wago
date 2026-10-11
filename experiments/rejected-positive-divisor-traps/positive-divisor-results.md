# General positive-divisor trap proof (Apple M4 Max, arm64)

Experimental, default off. Isolated baseline: c74d21c1. `regional-memory-read` is off here. No corpus name, checksum, manifest argument, or expected-result checks enter the optimizer.

The retained profiler capture shows dynamic signed division guarded by a zero check and a divisor == -1 overflow check. The recurrent divisor has a positive entry initializer and is clamped to a positive signed minimum on every loop backedge. Existing semantic local facts intentionally exclude control-flow functions.

The experiment proves strict signed W/X positivity from machine instructions with a bounded greatest-fixed-point must-analysis over direct branches, including loops. Every wrapper/internal entry starts unknown. Unknown instructions clear all facts; known GPR writes clear both width facts for the destination. A positive W write also proves positive X, while positive X alone never implies positive W. The recognized adjacent constant/CMP/CSEL signed clamp excludes control entries after the constant. Integer addition itself never preserves positivity, because it can wrap.

Only compiler-recorded division trap sites are rewritten. Positive divisors remove their zero branch. Signed division additionally removes the divisor -1 overflow sequence when its post-division NZCV is proved dead and no branch enters the sequence. Code size stays stable. Other trap kinds and negative/unknown divisors retain their guards. Functions with calls, exception/custom entries, indirect data fragments, more than4KiB native code, or an unsettled analysis decline.

Native fixtures pass both widths and bounds modes, both borrowed/ordinary operand paths, extreme dividends, 0/1/2/16/32 trips, and a zero-writing backedge that must still trap. Independent machine-word tests distinguish MOVW zero extension and MOVX signed positivity, MOVK invalidation, clamp directions/minima, and unknown entries into the clamp.

First all148 signal-mode exact oracles passed (46 core, 102 app). Only audio-adpcm's native image changed; 476 bytes before/after. This inventory is compared against the corrected leaf-scratch baseline frozen images.

The initial instruction-state implementation used18KiB stack tables. Eight alternating pairs showed ADPCM exec -0.73% paired (-1.29% pooled), compile +10.19%; unchanged gcd compile +7.95%. This is a bad tradeoff and is not retained. Its raw timings are `positive-divisor-{paired,core-paired}-{exec,compile}.jsonl` and summary is `positive-divisor-paired-summary.md`.

Mitigation replaces instruction-state tables with approximately3KiB basic-block state and requires a recognized positive clamp before analysis. Constant-only functions retain existing constant-divisor strength reduction. This avoids the large fixed table initialization and declines no-clamp functions before CFG construction. Native induction/width/entry fixtures pass after mitigation. Confirmation timings are in progress; no retained-gain claim yet.

The second mitigation summarizes one recurrent clamp destination as each basic block's preserve/kill/generate transfer; fixed-point rounds no longer decode words. Local copies to a divisor are proved inside its guard block. This uses about2.3KiB bounded stack backing. In eight pairs ADPCM compilation fell to +6.82%; execution was -2.79% paired, -1.31% pooled, with an IQR crossing zero. No-clamp gcd compilation was -0.25% paired. The summary is `positive-divisor-summary-paired-summary.md`.

A third mitigation removes only certified guard holes through the existing native finalizer offset map and reencoder. It enables no broad compaction and deletes no frame reservations. Any deletion shifting a backward target's requested loop alignment declines. Width, initializer/trap, induction, and independent machine-word tests pass with finalizer inventory validation enabled. Exact signal oracles and focused timing are queued.

The next candidate reuses the certified signed-div guard space for a general divisor==1 identity path: `CMP divisor,1; B.NE ordinary-SDIV; MOV result,dividend; B after-SDIV`. The MOV uses W or X according to SDIV width, and both paths retain source registers. The identity branch destination joins the common branch-target inventory. The ordinary SDIV path handles every other proved-positive divisor. Negative/unknown divisors keep the original zero and signed-overflow fallback. This variant is separately enabled by `WAGO_ARM64_POSITIVE_DIVISOR_ONE=1`, pending proof and measurement.

Guard-only compaction completed all148 signal oracles and changed only ADPCM476→448 bytes. Its12-pair400ms confirmation regressed execution +5.27% paired (+4.23% pooled) and compilation +9.26%. This layout is not retained. The size-stable identity path is queued separately to isolate the divisor==1 effect from the unfavorable code-layout change.
