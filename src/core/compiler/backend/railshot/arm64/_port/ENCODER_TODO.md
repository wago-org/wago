# arm64 encoder TODO — methods the railshot port needs but `encoder/arm64/asm.go` lacks

De-duplicated, categorized list of every `*a64.Asm` method referenced by the arm64
railshot port (`_port/*.go`) that is **not yet implemented** in
`src/core/encoder/arm64/asm.go`. Base opcode words are cross-checked against
`warp/src/core/compiler/backend/aarch64/aarch64_encoding.hpp` where WARP covers
the instruction; NEON words WARP does not cover are given from the Arm ARM
encoding format (mark: *verify with clang/llvm-objdump golden per CONTRACT §5/§10*).

Already present (for reference, do **not** re-add): Add/Sub 32/64, Adds64, Subs64,
CmpReg32/64, AddImm/SubImm 32/64, SubsImm64, CmpImm32/64, SubSP64, AddSP64,
MovReg32/64, Movz/Movk/Movn64, MovImm64, Load/Store 32/64, Ldrb/Strb/Ldrh/Strh,
StpPre, LdpPost, Madd64, Mul32/64, And/Orr/Eor 32/64, Lslv/Lsrv/Asrv 32/64,
Csel64, Cset32/64, AndImm64/OrrImm64/EorImm64, Ret/Br/Blr, Branch/Bcond/Cbz64/Cbnz64,
PatchBranch19/PatchBranch26.

Convention: methods that differ only by operand width take a `w bool` (true = 32-bit
W-form) or `wide bool`; where the port used separate `*32`/`*64` names they are
noted so the two naming styles can be reconciled when the method lands.

---

## 1. Integer ALU / data-processing

| Method | Signature | Base opcode (32 / 64) | Notes |
|---|---|---|---|
| `AddShifted` | `AddShifted(rd, rn, rm Reg, shift uint32, w bool)` | `0x0B000000 / 0x8B000000` | LSL amount in imm6 (bits 15:10), shift-type 00=LSL (bits 23:22). LEA-fold replacement. |
| `Adds32` | `Adds32(rd, rn, rm Reg)` | `0x2B000000` | 32-bit flag-setting ADD (carry-out test in table/memory grow). Adds64 exists. |
| `Sxtw` | `Sxtw(rd, rn Reg)` | `0x93407C00` | SXTW Xd, Wn (SBFM alias). |
| `Sxtb` | `Sxtb(rd, rn Reg, w bool)` | `0x13001C00 / 0x93401C00` | sign-extend byte. |
| `Sxth` | `Sxth(rd, rn Reg, w bool)` | `0x13003C00 / 0x93403C00` | sign-extend halfword. |
| `LslImm` | `LslImm(rd, rn Reg, shift uint32, w bool)` | `0x53000000 / 0xD3400000` | LSL #sh (UBFM: immr=(-sh)%N, imms=N-1-sh). simd's `LslImm64` = this w=false. |
| `LsrImm` | `LsrImm(rd, rn Reg, shift uint32, w bool)` | `0x53000000 / 0xD3400000` | LSR #sh (UBFM: immr=sh, imms=31/63). simd's `LsrImm32`/`AsrImm64` are width variants. |
| `AsrImm` | `AsrImm(rd, rn Reg, shift uint32, w bool)` | `0x13000000 / 0x93400000` | ASR #sh (SBFM). |
| `RorImm` | `RorImm(rd, rn Reg, shift uint32, w bool)` | `0x13800000 / 0x93C00000` | ROR #sh = EXTR Rd,Rn,Rn,#sh. |
| `Rorv32` | `Rorv32(rd, rn, rm Reg)` | `0x1AC02C00` | variable ROR (const-count rotl lowered here). |
| `Rorv64` | `Rorv64(rd, rn, rm Reg)` | `0x9AC02C00` | |
| `Clz` | `Clz(rd, rn Reg, w bool)` | `0x5AC01000 / 0xDAC01000` | i32/i64.clz. |
| `Rbit` | `Rbit(rd, rn Reg, w bool)` | `0x5AC00000 / 0xDAC00000` | reverse bits (ctz = RBIT+CLZ). |
| `Sdiv32` | `Sdiv32(rd, rn, rm Reg)` | `0x1AC00C00` | |
| `Sdiv64` | `Sdiv64(rd, rn, rm Reg)` | `0x9AC00C00` | |
| `Udiv32` | `Udiv32(rd, rn, rm Reg)` | `0x1AC00800` | |
| `Udiv64` | `Udiv64(rd, rn, rm Reg)` | `0x9AC00800` | |
| `Msub32` | `Msub32(rd, rn, rm, ra Reg)` | `0x1B008000` | Rd = Ra - Rn*Rm; remainder = MSUB after div. |
| `Msub64` | `Msub64(rd, rn, rm, ra Reg)` | `0x9B008000` | |
| `AndImm32` | `AndImm32(rd, rn Reg, val uint32) bool` | `0x12000000` | 32-bit bitmask-imm AND via `encodeLogicalImm(_, false)`; ok=false → fallback. |
| `OrrImm32` | `OrrImm32(rd, rn Reg, val uint32) bool` | `0x32000000` | |
| `EorImm32` | `EorImm32(rd, rn Reg, val uint32) bool` | `0x52000000` | |
| `CmnImm32` | `CmnImm32(rn Reg, imm uint32)` | `0x3100001F` | CMN (ADDS to XZR) — used for compare-against-negative. |
| `CmnImm64` | `CmnImm64(rn Reg, imm uint32)` | `0xB100001F` | |
| `Smulh` | `Smulh(rd, rn, rm Reg)` | `0x9B40FC00` | high 64 of signed 64×64 (magic-div). Ra=XZR. |
| `Umulh` | `Umulh(rd, rn, rm Reg)` | `0x9BC0FC00` | high 64 of unsigned 64×64. |
| `Smull` | `Smull(rd, rn, rm Reg)` | `0x9B207C00` | SMULL Xd,Wn,Wm (SMADDL Ra=XZR); 32×32→64 for 32-bit magic-div high half. |
| `Umull` | `Umull(rd, rn, rm Reg)` | `0x9BA07C00` | UMULL Xd,Wn,Wm (UMADDL Ra=XZR). |
| `Csel32` | `Csel32(rd, rn, rm Reg, c Cond)` | `0x1A800000` | 32-bit CSEL; driver.go's `Csel(...,w bool)` is the width-parameterized wrapper over Csel32/Csel64. |

## 2. Loads / stores (incl. addressing modes)

Scalar-FP (S/D) and 128-bit (Q) load/stores plus register-offset ("indexed") forms.
Several amd64-legacy names are aliases of the same encoding — reconcile naming when
adding.

| Method | Signature | Base opcode | Notes |
|---|---|---|---|
| `LoadIdx` | `LoadIdx(rt, rn, rm Reg, disp int, size int, se, z bool) bool` | LDR wT `0xB8606800`, xT `0xF8606800`, LDRB `0x38606800`, LDRH `0x78606800`, LDRSB(x) `0x38A06800`, LDRSH(x) `0x78A06800`, LDRSW `0xB8A06800` | register-offset load with option/S (LSL by size). Sub-width sign/zero-extend selected by se/z. Used for linMem + br_table table + spill reload. |
| `StoreIdx` | `StoreIdx(rn, rm, rt Reg, disp int, size int) bool` | STR wT `0xB8206800`, xT `0xF8206800`, STRB `0x38206800`, STRH `0x78206800` | register-offset store into linMem. |
| `StoreImmIdx` | `StoreImmIdx(rn, rm Reg, val uint64, size int)` | (store ZR at reg-offset; materialize val→scratch first when ≠0) | arm64 has no store-immediate; lower to StoreIdx of XZR/scratch. |
| `LeaSP` | `LeaSP(rd Reg, off int32)` | `ADD rd, SP, #imm12` (`0x91000000`, Rn=31); MOV+ADD fallback >4095 | SP-relative address (LEA-SP replacement). |
| `LdrS` / `LdrD` (a.k.a. `LdrF(...,f64)`, `FLoadDisp`) | `LdrS/LdrD(rt, rn Reg, off uint32) bool` | S `0xBD400000`, D `0xFD400000` | scalar FP scaled-imm load. `off` scaled by 4 / 8. |
| `StrS` / `StrD` (a.k.a. `StrF`, `FStoreDisp`) | `StrS/StrD(rt, rn Reg, off uint32) bool` | S `0xBD000000`, D `0xFD000000` | scalar FP scaled-imm store. |
| `LdrFIdx` / `StrFIdx` | `LdrFIdx(rt, rn, rm Reg, disp int, f64 bool)` / `StrFIdx(rn, rm, rt Reg, disp int, f64 bool)` | LDR sT `0xBC606800`, dT `0xFC606800`; STR sT `0xBC206800`, dT `0xFC206800` | scalar float register-offset (linMem) load/store. |
| `LdrQ` / `StrQ` (a.k.a. `VMovdquLoadDisp`/`VMovdquStoreDisp`) | `LdrQ(rt, rn Reg, off uint32) bool` / `StrQ(rn, rt Reg, off uint32) bool` | LDR qT `0x3DC00000`, STR qT `0x3D800000` | 128-bit scaled-imm load/store (`off` scaled by 16); v128 spill/global. |
| `LdrQIdx` / `StrQIdx` | `LdrQIdx(rt, rn, rm Reg, disp int)` / `StrQIdx(rn, rm, rt Reg, disp int)` | LDR q `0x3CE06800`, STR q `0x3CA06800` | 128-bit register-offset (linMem) load/store. |

*Naming reconciliation:* `FLoadDisp`/`FStoreDisp` (compile.go, control.go) ≡
`LdrS/LdrD`+`StrS/StrD`; `VMovdquLoadDisp`/`VMovdquStoreDisp` (compile.go, call.go)
≡ `LdrQ`/`StrQ`; `FmovReg` ≡ compile.go's `FMov`. Pick one name set (the `Ldr*`/`Str*`
set is preferred per CONTRACT) and alias the rest.

## 3. Branches & code-layout / patch helpers

| Method | Signature | Base opcode | Notes |
|---|---|---|---|
| `Bl` | `Bl() int` | `0x94000000` | BL placeholder, returns byte offset; patched by `PatchBranch26` at module layout. |
| `Adr` | `Adr(rd Reg) int` | `0x10000000` | ADR placeholder (PC-relative address, ±1 MiB), returns offset; br_table jump-table base. |
| `PatchAdr` | `PatchAdr(at, target int) bool` | — | fills ADR immlo(29:29)+immhi(23:5) = signed 21-bit (target-at). |
| `PatchU32` | `PatchU32(at int, val uint32)` | — | raw 32-bit little-endian data write (jump-table entries; NOT an instruction). |
| `PatchMovImm` | `PatchMovImm(at int, val uint32)` | — | patch a reserved MOVZ/MOVK X16 pair (16-bit halfwords) with the final frame size. |

## 4. Stack-pointer register forms

| Method | Signature | Base opcode | Notes |
|---|---|---|---|
| `SubSPReg` | `SubSPReg(rm Reg)` | `0xCB2063E0` | SUB SP, SP, Xm (extended-reg, needed because SP can't be Rm/Rn in shifted-reg form). |
| `AddSPReg` | `AddSPReg(rm Reg)` | `0x8B2063E0` | ADD SP, SP, Xm (extended-reg). |
| `CmpSP64` | `CmpSP64(rm Reg)` | `0xEB2063FF` | CMP SP, Xm (SUBS XZR, SP, Xm extended) — stack-fence check; SP must be Rn via extended form. |

## 5. Scalar floating-point

| Method | Signature | Base opcode (S / D) | Notes |
|---|---|---|---|
| `Fadd` | `Fadd(rd, rn, rm Reg, f64 bool)` | `0x1E202800 / 0x1E602800` | (was `FAdd`) |
| `Fsub` | `Fsub(rd, rn, rm Reg, f64 bool)` | `0x1E203800 / 0x1E603800` | |
| `Fmul` | `Fmul(rd, rn, rm Reg, f64 bool)` | `0x1E200800 / 0x1E600800` | |
| `Fdiv` | `Fdiv(rd, rn, rm Reg, f64 bool)` | `0x1E201800 / 0x1E601800` | |
| `Fsqrt` | `Fsqrt(rd, rn Reg, f64 bool)` | `0x1E21C000 / 0x1E61C000` | |
| `Fmin` | `Fmin(rd, rn, rm Reg, f64 bool)` | `0x1E205800 / 0x1E605800` | verify wasm min/max NaN + signed-zero semantics vs FMIN/FMAX. |
| `Fmax` | `Fmax(rd, rn, rm Reg, f64 bool)` | `0x1E204800 / 0x1E604800` | |
| `FmovReg` | `FmovReg(rd, rn Reg, f64 bool)` | `0x1E204000 / 0x1E604000` | scalar FMOV Vd,Vn (was `FMov`). |
| `FmovFromGpr` | `FmovFromGpr(rd, rn Reg, f64 bool)` | `0x1E270000 / 0x9E670000` | FMOV Sd/Dd,Wn/Xn (gpr→V; also +0.0 from XZR/WZR). |
| `FmovToGpr` | `FmovToGpr(rd, rn Reg, f64 bool)` | `0x1E260000 / 0x9E660000` | FMOV Wd/Xd,Sn/Dn (V→gpr). |
| `Fcmp` | `Fcmp(rn, rm Reg, f64 bool)` | `0x1E202000 / 0x1E602000` | sets NZCV (was `Ucomis`). |
| `Frint` | `Frint(rd, rn Reg, f64 bool, mode byte)` | N `0x1E244000/0x1E644000`, M `0x1E254000/0x1E654000`, P `0x1E24C000/0x1E64C000`, Z `0x1E25C000/0x1E65C000` | FRINTN/M/P/Z per ceil/floor/trunc/nearest (was `Round`). |
| `Fcvtzs` | `Fcvtzs(rd, rn Reg, f64src, dstWide bool)` | wD_sN `0x1E380000`, xD_sN `0x9E380000`, wD_dN `0x1E780000`, xD_dN `0x9E780000` | f→i signed trunc. (Native `Fcvtzu 0x1E390000/…` would simplify unsigned trunc-sat — currently emulated via bias.) |
| `Scvtf` | `Scvtf(rd, rn Reg, f64, srcWide bool)` | sD_wN `0x1E220000`, dD_wN `0x1E620000`, sD_xN `0x9E220000`, dD_xN `0x9E620000` | i→f signed. simd's `CvtI2F` is the generic form (add `Ucvtf 0x1E230000/…` for unsigned). |
| `FcvtS2D` | `FcvtS2D(rd, rn Reg)` | `0x1E22C000` | FCVT Dd,Sn (promote). |
| `FcvtD2S` | `FcvtD2S(rd, rn Reg)` | `0x1E624000` | FCVT Sd,Dn (demote). |

## 6. NEON / vector

By far the largest surface (all from `simd.go`, plus popcnt in `emit.go` and the
bitwise-logical scalars in `fp.go`/`driver.go`). WARP does **not** cover most of
these, so words below are from the Arm ARM 3-same / shift-by-imm / permute /
two-reg-misc formats and **must** get clang golden tests. All 128-bit forms use
Q=1; the `size`/`immh` field selects lane width (B/H/S/D). "+size" means add
`size<<22` (00=B, 01=H, 10=S, 11=D).

**Bitwise (three-same, size fixed):** `NeonAnd16b` `0x4E201C00`, `NeonOrr16b`
`0x4EA01C00`, `NeonEor16b` `0x6E201C00`, `NeonAndn16b` (BIC) `0x4E601C00`.
Aliases: fp.go/driver.go `And16b`/`Orr16b`/`Eor16b` and `NeonMov16b` (= ORR
Vd.16b,Vn,Vn `0x4EA01C00`) map onto these.

**Integer add/sub (three-same, +size):** `NeonAdd*` base `0x4E208400`; `NeonSub*`
base `0x6E208400` (B/H/S/D via +size).

**Saturating add/sub (three-same, +size):** `NeonSqadd*` `0x4E200C00`,
`NeonUqadd*` `0x6E200C00`, `NeonSqsub*` `0x4E202C00`, `NeonUqsub*` `0x6E202C00`
(B,H variants used).

**Compare (three-same, +size):** `NeonCmeq*` `0x6E208C00`, `NeonCmgt*`
`0x4E203400` (CMGE `0x4E203C00` if needed).

**Min/Max (three-same, +size; B/H/S only):** `NeonSmin*` `0x4E206C00`,
`NeonUmin*` `0x6E206C00`, `NeonSmax*` `0x4E206400`, `NeonUmax*` `0x6E206400`.

**Rounding/avg & mul (three-same, +size):** `NeonUrhadd*` `0x6E201400`;
`NeonMulH/S` (MUL, B/H/S only) `0x4E209C00`; `NeonSqrdmulhH` `0x6E20B400`.

**Abs (two-reg-misc, +size):** `NeonAbs*` `0x4E20B800`.

**Variable shift (three-same, +size):** `NeonUshl*` (USHL) `0x6E204400`,
`NeonSshrv*`/`NeonUshrv*` = SSHL/USHL with negated count (`0x4E204400`/`0x6E204400`).
*Semantics gap:* SSE shift-by-scalar-in-xmm has no direct analog — needs DUP-broadcast
of the count and negation for right shifts (CONTRACT-noted TODO).

**Shift-by-immediate (immh:immb format):** `NeonSshr*` `0x4F000400`,
`NeonUshr*` `0x6F000400` (H/S/D via immh). immh:immb encodes lane width + shift.

**Permute / lane ops:** `NeonZip1B/H/S` `0x4E003800 (+size)`, `NeonZip2B/H/S`
`0x4E007800 (+size)`; `NeonDupD` (DUP element) `0x4E000400` with imm5;
`NeonTbl` (TBL Vd.16b) `0x4E000000`; `NeonPshufS` — general lane shuffle by imm,
**no single NEON instr** (lower to DUP/EXT/INS per imm; used for splat + extract_lane).

**Insert / extract (GPR↔lane):** `NeonInsB/H/S/D` (INS Vd.ts[idx],Wn/Xn)
`0x4E001C00` with imm5 lane; `NeonUmovB/H/S/D` (UMOV Wd/Xd,Vn.ts[idx])
`0x0E003C00` (B) / imm5-encoded, D-form `0x4E003C00`.

**Pack / narrow (XTN/SQXTN family):** `SQXTN/SQXTN2` and `SQXTUN/SQXTUN2`
forms used by wasm signed and unsigned narrowing are now clang-golden-checked in
`asm2_test.go`. `UQXTN` is still available as a future helper if an unsigned-source
narrowing opcode needs it.

**Widening / pairwise / mul-long:** pairwise `SADDLP`/`UADDLP`, widening
`SXTL`/`UXTL`, and long multiply `SMULL`/`UMULL` low/high forms are now
clang-golden-checked in `asm2_test.go`. Core `i32x4.dot_i16x8_s` has no single
baseline-NEON PMADDWD twin, so the arm64 backend lowers it as signed lane
extraction + scalar mul/add + lane insert.

**Popcnt (emit.go):** `Cnt8b` (CNT Vd.16b) `0x4E205800` (WARP `CNT_vD8b` `0x0E205800`
is the .8b/Q0 form); `Addv8b` (ADDV) — WARP has `UADDLV_hD_vN8b` `0x2E303800`;
ADDV B `0x0E31B800`.

**Movemask (largest gap):** `NeonMovemaskB` — arm64 has **no** PMOVMSKB.
Synthesize: AND with a per-lane bit-position constant then ADDV/UADDLV reduction.
Feeds `v128Movemask`, `i{8,16,32,64}x2Bitmask`, `any_true`/`all_true`. No single
opcode; needs a helper sequence.

**Float vector (three-same, +size 0=.4s / 1=.2d):** `NeonFadd` `0x4E20D400`,
`NeonFsub` `0x4EA0D400`, `NeonFmul` `0x6E20DC00`, `NeonFdiv` `0x6E20FC00`,
`NeonFmax` `0x4E20F400`, `NeonFmin` `0x4EA0F400`, `NeonFsqrt` (two-reg-misc)
`0x6EA1F800`, `NeonFcmp` (FCMEQ `0x4E20E400` / FCMGT `0x6EA0E400` / FCMGE
`0x6E20E400`; lt/le via operand swap), `NeonFrint` (FRINTN/M/P/Z vector,
`0x4E218800`-family per mode). *NaN/signed-zero min/max fixup + the SSE CMPPS
imm8 → FCMEQ/FCMGT/FCMGE predicate mapping are CONTRACT TODOs in simd.go.*

## 7. Misc / buffer helpers

| Method | Signature | Notes |
|---|---|---|
| `Align16` | `Align16()` | pad code to 16-byte boundary with NOP `0xD503201F` words (trap stubs / jump tables). |
| `Grow` | `Grow(n int)` | reserve/extend the code buffer by n bytes (module-layout copy in compile.go). |
| `MovImm32` | `MovImm32(rd Reg, v int32)` | thin wrapper = `MovImm64(rd, uint64(uint32(v)))` (W-write zero-extends); optional name-parity helper. |
| `CompiledModule` | *type*, not a method: `struct { Code []byte; Entry, InternalEntry []int }` | compile.go return type — ensure the arm64 package exports it (or reuse the shared one). |

---

### Cross-file duplicate resolution summary
- `FmovFromGpr`, `LoadIdx`, `StrQ`, `LslImm`, `AndImm32`, `Bl`, `Align16`,
  `Adds32`, `FmovReg`, `FStoreDisp`/`FLoadDisp`, `VMovdquLoadDisp`/`StoreDisp`,
  `Fadd`/`Fsub`/`Fmul`/`Fdiv` appear in multiple ported files — implement once.
- amd64-legacy emit-helper names (`FLoadDisp`/`FStoreDisp`, `VMovdqu*Disp`, `FMov`,
  `Ucomis`, `Cvttf2si`, `Round`, etc.) survive in compile.go/control.go/driver.go
  and must be reconciled to the `Ldr*`/`Str*`/`Fmov*`/`Fcvtzs`/`Scvtf`/`Frint`
  names chosen here.
