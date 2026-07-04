# SIMD and relaxed SIMD implementation plan

This plan records the working contract for adding WebAssembly `v128` SIMD and
relaxed SIMD to the linux/amd64 backend.

## Goal

Implement Wasm SIMD and relaxed SIMD without turning `src/core/encoder/amd64`
into a Wasm-aware lowering layer.

Completion means:

- `v128` is a first-class backend machine type, including params, locals,
  operand-stack values, spill slots, function results, globals where supported,
  and memory traffic.
- The frontend no longer blanket-rejects `0xfd`; it accepts only SIMD opcodes
  that the backend can lower and clearly rejects the rest.
- The amd64 encoder exposes small x86/XMM primitives with golden byte tests.
- The backend lowers core SIMD instructions first, then deterministic relaxed
  SIMD choices.
- Official or focused execution tests cover the enabled instructions.
- `FEATURES.md`, `ROADMAP.md`, and architecture docs reflect the final support
  and CPU-baseline policy.

## Package boundary

Keep `src/core/encoder/amd64` as an x86-64 instruction encoder only:

- expose raw SSE/VEX helpers such as packed integer ops, packed float ops,
  shuffle/blend/extract/insert, and 128-bit memory moves;
- keep helpers named after x86 instructions or encoding forms, not Wasm
  `InstrKind`s;
- put all Wasm semantic choices, scratch-register choreography, constants, and
  multi-instruction sequences in `src/core/compiler/backend/railshot`.

## CPU baseline policy

The linux/amd64 backend's practical baseline is modern x86-64 with SSE4.1 plus
AVX/VEX.128 XMM encodings. This resolves the previous conflict where the docs
said SSE4.1 while scalar float lowering already emitted VEX/AVX forms
unconditionally. SIMD encoder helpers may therefore use VEX.128 for
non-destructive XMM operations without adding per-instruction CPUID gates.

Do not silently require AVX2, FMA, VNNI, or wider YMM/ZMM vector forms for core or
relaxed SIMD. Use those only after an explicit baseline update or a documented
feature-gated fast path with conservative fallback lowering.

## Current status

- Encoder: VEX.128 XMM register/memory helpers, movemask helpers, packed integer
  abs/multiply/signed-and-unsigned-minmax helpers, and SSE/SSE4.1 lane shuffle/insert/extract
  helpers have golden tests for the current lowering set.
- Backend: `mtV128` is present for amd64 params, locals, operand-stack values,
  spills, function results, linear-memory `v128.load`/`v128.store`, extending-load/load-splat/load-zero ops, lane memory load/store, and i8x16.swizzle.
- Frontend: `0xfd` is no longer blanket-rejected; only the currently lowered
  opcodes are accepted (`v128.const`, `v128.load`, `v128.store`, extending-load/load-splat/load-zero ops, lane memory load/store, i8x16.swizzle, splats, lane
  extract/replace, `v128.and`/`andnot`/`or`/`xor`/`not`/`bitselect`,
  `v128.any_true`, all_true/bitmask for i8x16/i16x8/i32x4/i64x2, integer neg for
  i8/i16/i32/i64 lanes, abs for i8/i16/i32/i64 lanes, i8x16 popcnt, signed/unsigned i8 narrow
  from i16 lanes, signed/unsigned i16 narrow from i32 lanes, signed/unsigned i8-to-i16, i16-to-i32, and i32-to-i64 widening extends, pairwise extadd from i8-to-i16 and i16-to-i32 lanes, signed/unsigned i8-to-i16, i16-to-i32, and i32-to-i64 extmul, add/sub for i8/i16/i32/i64 lanes, saturating add/sub for i8/i16 lanes, i16 q15mulr_sat_s,
  i16/i32 lane shifts plus i64 lane shifts, mul for i16/i32 lanes, eq/ne for those lanes, signed and unsigned ordered comparisons for i8/i16/i32,
  signed/unsigned min/max for i8/i16/i32, unsigned rounding averages for i8/i16,
  and f32x4/f64x2 abs/neg/sqrt/add/sub/mul/div plus comparisons). Other SIMD and relaxed
  SIMD opcodes remain explicit unsupported-instruction errors; `i64x2.shr_s` uses a
  baseline-safe scalarized qword-lane sequence with count masking instead of relying
  on SSE4.2/AVX2, and `i64x2.gt_s` is intentionally still rejected until a
  baseline-safe sequence or a documented SSE4.2 gate exists.
- Globals/imports: `v128` globals remain unsupported. Host imports with `v128`
  parameters/results are rejected by the existing import signature checks;
  exported wasm functions may use the 16-byte wrapper ABI slots covered by tests.

## Suggested implementation order

1. Encoder foundations:
   - generic VEX.128 register forms for 0F, 0F38, and 0F3A opcode maps;
   - immediate forms for shuffle/blend/round/extract/insert instructions;
   - 128-bit XMM memory load/store helpers;
   - golden byte tests covering xmm0-15 and RSP/R12/indexed memory encodings.
2. Backend `v128` plumbing (initial amd64 tranche landed):
   - add `mtV128` and 16-byte spill slots/alignment;
   - share XMM allocation between float and vector values safely;
   - support `v128` params, locals, results, and frame copy paths.
3. Core SIMD tranche:
   - `v128.const`, `v128.load`, `v128.store`, extending-load/load-splat/load-zero ops, and lane memory load/store (landed);
   - `v128.and/or/xor/not/andnot/bitselect` (landed);
   - i8x16.swizzle with Wasm out-of-range zeroing semantics (landed);
   - splats and lane extract/replace (landed);
   - integer neg/abs for i8/i16/i32/i64, i8x16 popcnt, signed/unsigned i8/i16
     narrow, signed/unsigned extmul for i8-to-i16/i16-to-i32/i32-to-i64, add/sub for i8/i16/i32/i64, saturating add/sub for i8/i16, i16 q15mulr_sat_s, mul for
     i16/i32, eq/ne for those lanes, signed and unsigned ordered comparisons for i8/i16/i32,
     signed/unsigned min/max for i8/i16/i32, and unsigned rounding averages for
     i8/i16 (landed; i8/i64 mul and i64 gt/lt/le/ge plus i64 unsigned
     comparisons remain);
   - f32x4/f64x2 packed abs/neg/sqrt/add/sub/mul/div and comparisons (landed;
     focused tests include signed-zero unary lanes, non-NaN arithmetic lanes, plus
     NaN comparison masks);
   - `v128.any_true` and all_true/bitmask for i8x16/i16x8/i32x4/i64x2 (landed);
   - remaining integer arithmetic/comparisons including i8/i64 mul and i64 ordered
     comparisons;
   - remaining packed float min/max/pmin/pmax and conversions.
4. Remaining core SIMD:
   - shuffle and the remaining swizzle-adjacent/core shape-specific cases, remaining narrow/widen shape-specific cases, min/max,
     conversions, and other shape-specific corner cases.
5. Relaxed SIMD:
   - pick deterministic choices first, optimize later.

## Relaxed SIMD lowering choices

Initial relaxed SIMD lowerings should be deterministic and easy to audit:

- `i8x16.relaxed_swizzle`: use raw `pshufb` semantics where valid for the relaxed
  instruction.
- `i{8,16,32,64}x*.relaxed_laneselect`: lower like `v128.bitselect`.
- `f32x4/f64x2.relaxed_min/max`: use native packed min/max instructions.
- `f32x4/f64x2.relaxed_madd/nmadd`: start with `mul + add/sub`; add FMA only
  behind an explicit baseline decision or feature gate.
- relaxed truncations: prefer already-correct saturating sequences until native
  conversion behavior is proven acceptable for all relaxed result cases.
- `i16x8.relaxed_q15mulr_s`: use `pmulhrsw` under the documented baseline.
- relaxed dot products: start with portable SSSE3/SSE4.1 unpack/sign-extend,
  multiply, and add sequences; add AVX2/VNNI later only with a documented gate.

## Recursive handoff expectation

When using `skills/recursive-handoff`, each iteration should try to complete
three bounded slices and commit each slice atomically. Stop early if blocked or if
one slice uncovers a correctness issue that should not be built on until fixed.
