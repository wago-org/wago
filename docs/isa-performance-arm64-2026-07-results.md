# ARM64 SIMD optimization results (2026-07)

Follow-up to `isa-performance-arm64-2026-07.md`. Baseline = that doc (Wago at branch point,
darwin/arm64 Apple M4 Max). "After" = branch `jairus/simd-perf-opt` @ d4b7cfb. All numbers are
per-export ns/op, MIN of repeated Go benchmark samples, wago vs wazero measured on the same machine.
(Spot numbers below use benchtime 30–50ms / count 2–3 for iteration speed; regenerate with the full
`make bench` harness for authoritative figures.)

## What changed

Five optimization lanes were investigated in parallel; three landed as-is, one was reworked, one
turned out to be unnecessary:

| Lane | Area | Outcome |
|---|---|---|
| l1 | v128 local register caching (systemic) | **Landed.** Pin v128 locals in NEON V registers across call-free loops so `local.get`/`local.set` stop emitting `LdrQ`/`StrQ`. Gated by `WAGO_ARM64_NO_V128_PINS`. This is the dominant win — it lifts *every* op that rides `materializeV128`. |
| l2 | float min/max, pmin, div, sqrt | **Obviated by l1.** `f32x4/f64x2.min/max` fell from ~35,000 to ~6,640 purely from l1; the −89% catastrophe was reload overhead, not the FMIN/FMAX sequence. pmin/div/sqrt now 1.2–1.4× wazero — no dedicated work needed. |
| l3 | i16x8.q15mulr_sat_s | **Landed.** Native `SQRDMULH` (AArch64 saturates `INT16_MIN²`→`0x7fff` in hardware). 28,974→8,398. |
| l4 | i64x2.shr_s / mul, i8x16.swizzle | **Reworked** (first attempt produced no patch). See below. |
| l5 | reductions (bitmask/all_true) | **Landed.** `UMINV`/`ADDV`-based sequences + new encoders. |

## Headline before → after

| Op (family) | Baseline | After | wazero | Baseline Δ | After ratio |
|---|---:|---:|---:|---:|---:|
| binary int `add/sub/and/or/xor/eq/min/max` (all widths) | ~18,800 | ~6,640 | ~3,650 | −80% | 1.77× |
| `f32x4/f64x2.min` & `.max` | ~35,000 | ~6,640 | ~3,620 | −89% | 1.82× |
| `f32x4/f64x2.add/sub` | ~18,900 | ~8,200 | ~3,850 | −80% | 2.12× |
| `i64x2.shr_s` | 13,281 | 3,006 | 1,888 | −85% | 1.59× |
| `i8x16.swizzle` | 20,895 | 3,609 | 3,614 | −83% | 1.00× |
| `i64x2.mul` | 43,502 | 24,845 | 22,465 | −49% | 1.11× |
| `i16x8.q15mulr_sat_s` | 28,974 | 8,398 | 5,354 | −81% | 1.57× |
| `i16x8_bitmask` | 6,261 | 2,790 | 1,660 | −74% | 1.68× |
| shifts `shl/shr_s/shr_u` (all widths) | 2,350–13,281 | ~3,000 | ~1,880 | mixed | ~1.6× |

## Family uniformity (post-change)

The key property after these changes: **no single lane-width is an outlier within its family.**
`add/sub/min/eq` are ~6,640 (1.77×) for i8x16/i16x8/i32x4/i64x2 alike; shifts are ~3,000 (1.6×)
across all widths (i64x2.shr_s no longer scalarizes); extmul/extend/abs/neg each share one ratio
across widths. Remaining gaps (`extend`/`neg` ~2.4×, `narrow`/`abs` ~1.9×) are *uniform shared-lowering*
gaps — improving them means improving the shared unary/widen path, which lifts all widths together.

## Remaining gaps vs wazero (future work, uniform across widths)

- Unary in-place ops (`abs` ~1.95×, `neg`/`extend_low_s` ~2.4×): still materialize an owned copy
  before writing; could write in place when the destination local == a dead source local.
- `narrow_*` ~1.88×.
- `i64x2_all_true` 2.05× (uses CMEQ; UMINV has no `.2D` form) — mild per-width difference, inherent.
- The ~1.8× floor on binary ops vs wazero's dependent-loop scheduling.

## Correctness

`go build ./...` and `go test ./src/core/compiler/backend/railshot/arm64/ -run SIMD` pass on darwin/arm64,
including shift/swizzle/q15/extmul/narrow/bitmask/all_true exec tests. l1 is gated behind
`WAGO_ARM64_NO_V128_PINS` as a kill switch. The l3 SQRDMULH saturation trick is arm64-specific — the
x86 backend must keep its `PMULHRSW` software fixup.
