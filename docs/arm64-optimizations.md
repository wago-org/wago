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
| Pinned locals | Hot locals stay in registers, with call spill/reload | Present; X19-X23 and V8-V11 |
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
| Select lowering | `cmov`/flags-based select | Present; `CSEL` and flags path |
| Jump-table br_table | Large br_table uses indirect jump table | Present; `ADR/LDR/BR` jump table |
| Bounds facts | Straight-line bounds-check certificates elide covered checks | Present |
| Loop precheck | Version loops to hoist invariant-base bounds checks | Present |
| Guard-page mode | Elide inline checks when runtime guard pages are active | Present on linux/arm64 |
| Small bulk memory | Constant copy/fill unroll and small dynamic 8-byte chunks | Present |
| Large bulk memory | `rep movs/stos` for large copy/fill | Tuned: arm64 dynamic copy/fill now uses 16-byte NEON chunks, 8-byte loops, and byte tails |
| Lazy local zeroing | Defer declared-local zeroing for narrow recursive memory functions | Present |
| Stack-fence elision | Skip fence for small call-free leaf frames | Present; arm64-specific env knobs |
| SIMD baseline | Full amd64 SSE implementation | Active NEON baseline with qemu execution coverage; full external SIMD spec parity still needs follow-up |
| SIMD bitselect | `pand`/`pandn`/`por` or SSE blend-style masks | Tuned: `BSL Vmask.16b,Va.16b,Vb.16b` destructive mask lowering |
| SIMD byte popcount | SSSE3 nibble-LUT shuffle/add sequence | Tuned: single `CNT Vd.16b,Vn.16b` |
| SIMD integer abs | SSE native/emulated per lane | Tuned: native NEON `ABS` including `i64x2.abs` with INT64_MIN wrap coverage |
| SIMD unsigned compares | Sign-bias then signed compare on SSE lanes | Tuned: native `CMHI`/`CMHS` for i8/i16/i32 lanes |
| SIMD signed compares | SSE compare plus swapped/inverted predicates | Tuned: `CMGT` plus `CMGE`, including native i64x2 signed compares |
| SIMD float abs/neg | Bitwise sign-mask `and/xor` | Tuned: native `FABS`/`FNEG` for f32x4/f64x2 |
| SIMD float arithmetic | SSE packed add/sub/mul/div/sqrt | Present; NEON `FADD`/`FSUB`/`FMUL`/`FDIV`/`FSQRT` |
| SIMD float min/max | SSE fixup sequences for wasm NaN/signed-zero semantics | Present as scalar-correct lane helper; native vector fixup remains future tuning until the mask sequence proves exact wasm NaN/signed-zero behavior |
| SIMD shuffle/swizzle | `pshufb`/lane shuffles | Present; NEON `TBL`-based lowering |
| SIMD narrow/pack | SSE pack/saturate ops | Tuned: native `SQXTN/SQXTN2` and `SQXTUN/SQXTUN2` for wasm signed and unsigned narrowing |
| SIMD ext/extend/extadd/extmul | SSE unpack/widen/multiply idioms | Tuned: native `SXTL`/`UXTL`, `SADDLP`/`UADDLP`, and `SMULL`/`UMULL` low/high forms |
| SIMD scalar splat | SSE insert/shuffle idioms | Tuned: lane-size `DUP` for integer and float splats |
| SIMD packed conversions | SSE scalar/vector conversion mix | Tuned: native f64/f32 demote/promote, i32-to-float conversion, and f32x4 trunc_sat; f64x2-to-i32 trunc_sat remains scalar |

Current high-priority remaining arm64 tuning work:

1. Prove a native vector float min/max fixup for wasm NaN and signed-zero rules;
   keep the current scalar lane helper until the mask sequence has spec coverage.
2. Find or reject a compact exact f64x2-to-i32x4 trunc_sat sequence. AArch64 has
   direct f32x4 FCVTZS/FCVTZU but no direct f64x2-to-i32x2 vector form.
3. Measure 16-byte NEON bulk-memory loops against real arm64 hardware; qemu only
   proves overlap/tail correctness.
4. Measure real hardware arm64 workloads against the existing amd64 benchmark
   panels; qemu is useful for correctness, not final performance claims.
