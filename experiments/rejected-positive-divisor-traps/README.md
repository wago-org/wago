# Rejected positive-divisor trap proof

Apple M4 Max, arm64; isolated baseline c74d21c1. Production source was never imported. All work ran through the shared benchmark lock.

A bounded native must-analysis proves signed W/X positivity through clamps, entry initialization and loop backedges, then removes only compiler-recorded division zero/overflow guards. Unknown entries and instructions decline; arithmetic wrap cannot preserve positivity. Certified guards can optionally shrink through the existing finalizer; another optional variant puts an exact divisor==1 identity path in their original footprint, with W writes preserving zero extension.

The experiment was mitigated repeatedly: 18KiB instruction states → 3KiB basic blocks → 2.3KiB one-fact block-transfer summaries → clipping cold trap regions after the proved first RET. Guard compaction and a divisor==1 path were measured independently. Only ADPCM changed in each all148 signal oracle/image inventory; the original guard compaction changed 476→448 bytes, identity compaction 476→464. Both exact inventories passed.

Final cold-region, size-preserved identity measurement: ADPCM execution -0.05% paired (IQR -0.46% to +1.52%), +1.01% pooled; compilation +6.97% paired. This has no retained execution gain and a material compiler cost. Compacted guards regressed exec +5.27%; compacted identity regressed +7.70%. No variants are retained.

Actual ADPCM divisor adds2 or subtracts1 according to the clamped quotient and then clamps to1. It does not invariably converge to1. The retained profiler is leaf observations, not instruction-cycle weights; SDIV itself had few observations. These facts explain why dropping guards or adding the identity path is weak.

Native proof fixtures passed 32/64 bit widths, both bounds modes, ordinary/borrowed inputs, positive minima, zero/negative initial states, unknown entries, backedges that write zero, and dirty i32 carriers. A later explicit CompactNative fixture functionally matched its oracle but its strict admission assertion failed because that compiler shape conservatively declines the proof. Its failure logs are preserved; no claim is made that this final strict test suite passed.

Files retain raw alternating pairs, oracle logs, native inventories, scripts, disabled implementation/tests, and the integration patch. The final experiment can be replayed from c74d21c1 by applying integration.patch and restoring disabled files. Experimental default-off gate: WAGO_ARM64_EXPERIMENT_POSITIVE_DIVISOR_TRAPS=1. Optional variants: WAGO_ARM64_POSITIVE_DIVISOR_SHRINK=1 and WAGO_ARM64_POSITIVE_DIVISOR_ONE=1.
