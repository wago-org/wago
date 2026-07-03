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

The current tree documents an SSE4.1 baseline, but scalar float lowering already
uses VEX/AVX forms unconditionally. Before broad SIMD enablement, choose and
document one policy:

1. treat AVX/VEX.128 as part of wago's practical amd64 baseline; or
2. add legacy SSE fallbacks/feature gates before enabling AVX-only sequences.

Do not silently require AVX2, FMA, VNNI, or newer extensions for relaxed SIMD.
Use those only after an explicit baseline update or feature-gated fast path.

## Suggested implementation order

1. Encoder foundations:
   - generic VEX.128 register forms for 0F, 0F38, and 0F3A opcode maps;
   - immediate forms for shuffle/blend/round/extract/insert instructions;
   - 128-bit XMM memory load/store helpers;
   - golden byte tests covering xmm0-15 and RSP/R12/indexed memory encodings.
2. Backend `v128` plumbing:
   - add `mtV128` and 16-byte spill slots/alignment;
   - share XMM allocation between float and vector values safely;
   - support `v128` params, locals, results, and frame copy paths.
3. Core SIMD tranche:
   - `v128.const`, `v128.load`, `v128.store`;
   - `v128.and/or/xor/not/andnot/bitselect`;
   - splats, lane extract/replace, integer add/sub/mul, comparisons;
   - packed float add/sub/mul/div/sqrt and comparisons;
   - `any_true`, `all_true`, and bitmask instructions.
4. Remaining core SIMD:
   - lane memory ops, swizzles/shuffles, narrow/widen/extmul, min/max,
     conversions, and shape-specific corner cases.
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
