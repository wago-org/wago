# Arm64 Railshot Optimization Matrix

This tracks amd64 railshot optimizations and the arm64-specific equivalent. The
goal is parity of intent, not byte-for-byte lowering: x86 memory operands, string
ops, CMOV, and fixed division registers are replaced with AArch64 forms.

| Optimization | amd64 shape | arm64 status |
| --- | --- | --- |
| Scratch reuse | Reused stack and encoder buffers per module compile | Present; reused `stack` and `a64.Asm` scratch |
| Deferred operand stack | Lazy constants, locals, globals, memory refs, and deferred binary trees | Present; load/store architecture keeps `stMemRef` but materializes before ALU/CMP |
| Constant folding | Integer binops, compares, unary counts, eqz, extensions | Present |
| Algebraic simplification | Identities, same-operand rewrites, power-of-two strength reduction | Present |
| Zero-extension elimination | Drop redundant `i64.extend_i32_u` for clean i32 producers | Present; uses W-register zeroing |
| Scaled-index add | `lea base + index*scale` | Present; `ADD shifted-register` |
| Small constant multiply | `lea x + x*{2,4,8}` for multiply by 3, 5, 9 | Present; `ADD shifted-register` |
| Constant-divisor division | Power-of-two and magic multiply-high paths | Present; uses orthogonal `SMULH`/`UMULH`/`SMULL`/`UMULL` instead of RDX:RAX |
| ALU immediate folding | imm32 or memory operand forms | Present; AArch64 add/sub imm12 and logical bitmask immediates |
| Immediate stores | Store constant directly to memory | Present; materialize through scratch only when required by AArch64 encoding |
| Scalar bit counts | `lzcnt`/`tzcnt`/`popcnt` | Present; `CLZ`, `RBIT+CLZ`, and NEON `CNT+ADDV` |
| FP/SIMD register pool | XMM operand pool plus pinned locals/constants | Tuned: arm64 now uses V16-V31 as transient FP/SIMD registers after V0-V15 |
| Float constant preload | Up to two call-free float constants pinned in XMM regs | Present; up to two V regs |
| Float local sink | `local.set` after float binop writes directly to pinned local | Present; NEON scalar destination sink |
| Float min/max correctness | NaN and signed-zero-correct sequences | Present; `FCMP`, bitwise zero fixups, scalar `FMIN`/`FMAX` |
| Pinned locals | Hot locals stay in registers, with call spill/reload | Present; X19-X23 (plus X24-X25 for eligible call-free functions) and V8-V14 |
| Global cell cache | Reuse derived global-cell pointer in straight-line code | Present |
| Value-pinned globals | Hot mutable int globals stay in registers per function | Present |
| Module-pinned globals | Hot globals reserved module-wide across register-ABI calls | Present; adjusted around arm64 fixed roles |
| Linear-memory size cache | Dedicated mem-size register for explicit bounds | Present; X27 |
| Register merge | Single-result block/if values merge in a register | Present; X15/V15 |
| Register ABI calls | Internal wasm calls pass simple signatures in registers | Present; arm64-specific register ABI |
| Single-register returns | Single-result functions produce directly in return register | Present; X0/V0 |
| Call-localset fuse | Direct call result can sink into local.set | Present |
| Import binding lowering | Host/cross-instance call paths resolved at link-time recompile | Present |
| Compare-branch fuse | Compare feeding branch skips boolean materialization | Present; `CMP/FCMP + B.cond` |
| Eqz fold | `eqz` inverts next branch/select condition | Present |
| Empty-edge branch fold | (arm64-specific) A value-less `br_if` whose edge emits no code lowers to a single `B.cond target` instead of `B.cond skip; B target; skip:` — one fewer taken branch per loop iteration. `opBr`/`brIfFused`; `WAGO_ARM64_NOBRFOLD=1`. Halves tight ALU/branch loops on M4 (`globals`, `sieve`, `memory.sum`). |
| Extend-add fusion | (arm64-specific) `i64.add(x, i64.extend_i32_u(y))` → `ADD Xd,Xn,Wm,UXTW`. `WAGO_ARM64_NOUXTW=1`. Correct; ~neutral on M4 (removed insns off the critical path). |
| Store→load forwarding | (arm64-specific) Adjacent `STR Xs,[SP,#k]; LDR Xd,[SP,#k]` → `STR…; MOV Xd,Xs` (NOP if same reg). `WAGO_ARM64_NOSTLDFWD=1`. Removes inlined-call arg-staging reloads; ~neutral on M4. |
| Select lowering | `cmov`/flags-based select | Present; `CSEL` and flags path |
| Jump-table br_table | Large br_table uses indirect jump table | Present; `ADR/LDR/BR` jump table |
| Bounds facts | Straight-line bounds-check certificates elide covered checks | Present |
| Loop precheck | Version loops to hoist invariant-base bounds checks | Present |
| Guard-page mode | Elide inline checks when runtime guard pages are active | Present on Linux and Darwin arm64 |
| Small bulk memory | Constant copy/fill unroll and small dynamic 8-byte chunks | Tuned: dynamic lengths below 64 B use 8-byte chunks; 64 B and above use NEON. The generated ISA corpus locks the crossover with forward copy, overlapping backward copy, and fill at 64 B/256 B/4 KiB. |
| Large bulk memory | `rep movs/stos` for large copy/fill | Tuned: arm64 dynamic copy/fill uses 64-byte unrolled NEON groups, then 32-byte, 16-byte, 8-byte, and byte tails. On Apple M4 Max, 4 KiB copy improved from ~96 ns to ~57.6 ns and fill from ~94.5 ns to ~54.2 ns, with zero allocations. |
| Lazy local zeroing | Defer declared-local zeroing for narrow recursive memory functions | Present |
| Stack-fence elision | Skip fence for small call-free leaf frames | Present; arm64-specific env knobs |
| SIMD baseline | Full amd64 SSE implementation | Complete: the same 256 decoded opcode cases are present; native Darwin/arm64 passes all 24,325 official SIMD assertions with zero skips |
| SIMD bitselect | `pand`/`pandn`/`por` or SSE blend-style masks | Tuned: `BSL Vmask.16b,Va.16b,Vb.16b` destructive mask lowering |
| SIMD boolean/unary | SSE masks and subtract-from-zero idioms | Tuned: native `MVN`, lane-width `NEG`, and `UMAXV` any/all-true reductions |
| SIMD bitmask | `pmovmskb` followed by lane compaction | Tuned: packed byte shift plus two 64-bit multiply-gathers; no per-lane extraction loop |
| SIMD byte popcount | SSSE3 nibble-LUT shuffle/add sequence | Tuned: single `CNT Vd.16b,Vn.16b` |
| SIMD integer abs | SSE native/emulated per lane | Tuned: native NEON `ABS` including `i64x2.abs` with INT64_MIN wrap coverage |
| SIMD unsigned compares | Sign-bias then signed compare on SSE lanes | Tuned: native `CMHI`/`CMHS` for i8/i16/i32 lanes |
| SIMD signed compares | SSE compare plus swapped/inverted predicates | Tuned: `CMGT` plus `CMGE`, including native i64x2 signed compares |
| SIMD float abs/neg | Bitwise sign-mask `and/xor` | Tuned: native `FABS`/`FNEG` for f32x4/f64x2 |
| SIMD float arithmetic | SSE packed add/sub/mul/div/sqrt | Present; NEON `FADD`/`FSUB`/`FMUL`/`FDIV`/`FSQRT` |
| SIMD float min/max | SSE fixup sequences for wasm NaN/signed-zero semantics | Tuned: branchless packed `FMIN`/`FMAX` fixup with ordered masks, signed-zero resolution, and canonical NaNs |
| SIMD pseudo-min/max | Commuted SSE min/max with first-operand tie/NaN behavior | Tuned: ordered packed compare plus `BSL`; first operand wins equal and unordered lanes |
| SIMD shuffle/swizzle | `pshufb`/lane shuffles | Present; NEON `TBL`-based lowering |
| SIMD narrow/pack | SSE pack/saturate ops | Tuned: native `SQXTN/SQXTN2` and `SQXTUN/SQXTUN2` for wasm signed and unsigned narrowing |
| SIMD ext/extend/extadd/extmul | SSE unpack/widen/multiply idioms | Tuned: native `SXTL`/`UXTL`, `SADDLP`/`UADDLP`, and `SMULL`/`UMULL` low/high forms |
| SIMD dot products | SSE widening multiply and horizontal-add sequences | Tuned: `SMULL`/`SMULL2` plus packed pair reductions; relaxed dot uses exact saturating narrowing |
| SIMD scalar splat and float lanes | SSE insert/shuffle and scalar moves | Tuned: one GPR-to-vector `DUP` for integer splats and vector-only `DUP`/`INS` for float lane extraction/replacement |
| SIMD packed conversions | SSE scalar/vector conversion mix | Tuned: native f64/f32 demote/promote, i32-to-float conversion, f32x4 trunc_sat, and two-instruction `FCVTZ*` + saturating narrow for f64x2-to-i32 |

## Verification

On 2026-07-10, native Darwin/arm64 verification included:

- arm64 backend and encoder packages;
- every native `TestSIMD*` execution test, including exact NaN, signed-zero,
  saturation, lane, dot-product, bitmask, and reduction cases;
- encoder golden words for each new AArch64 instruction; and
- the pinned official SIMD proposal corpus: 24,325 assertions passed, zero
  skipped modules, and zero skipped assertions.
- the pinned WebAssembly 1.0 corpus: 629 modules and 16,026 assertions passed
  with zero gaps after correcting wide narrow-sign-extension and unsigned-i64
  float conversion lowering;
- explicit and guard-page MVP/SIMD execution; and
- the complete explicit and guard-page parent/child corpus matrix.

Current high-priority remaining measurement work:

1. Extend the bulk-memory benchmark panel beyond the current generated ISA
   corpus's 64 B, 256 B, and 4 KiB wazero comparison and add libc measurements.
2. Compare real arm64 workloads against the existing amd64 benchmark panels;
   correctness parity is now established, but cross-machine timings are not
   directly comparable.
