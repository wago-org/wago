# arm64 (AArch64) Railshot Port — Shared CONTRACT

**Status: single source of truth for the per-file porters.** Every porter turning a
`railshot/amd64/*.go` file into its `railshot/arm64/*.go` twin MUST follow this
document so the twins compose into one consistent backend. When in doubt, keep the
amd64 file's *structure* and *names* and change only what this contract says to
change.

The full port **supersedes the beachhead**: `railshot/arm64/compile.go`'s `comp`
type and its `Compile(numParams, body)` entrypoint are throwaway and will be
**deleted on integration**. Do not build on `comp`. Define the real fn-based
architecture: `CompileModule` / `CompileModuleWith` / `compileFunc` /
`compileFuncAttempt` / `fn` / `scratch`, mirroring `amd64/compile.go`.

The reference C++ backend is `warp/src/core/compiler/backend/aarch64` (esp.
`aarch64_cc.hpp`); consult it for register roles and lowering shapes, but wago's
runtime ABI — not WARP's — is authoritative. When WARP and the amd64 backend
disagree, follow the amd64 backend's wago-adapted conventions.

---

## 1. Package and imports

- Target Go package name: **`arm64`** (matching the directory). *No* `//go:build`
  tag that excludes arm64 — the amd64 files carry `//go:build amd64`; the arm64
  twins carry **`//go:build arm64`**. (The neutral, arch-independent files carry no
  tag.)
- The arm64 instruction encoder is imported under the alias **`a64`**:

  ```go
  import a64 "github.com/wago-org/wago/src/core/encoder/arm64"
  ```

- The `fn` encoder field keeps the name **`a`**, but its type becomes
  **`*a64.Asm`** (amd64 used `*amd64.Asm`). Everywhere the amd64 code calls
  `f.a.Foo(...)`, the arm64 code calls `f.a.Foo(...)` on the a64 encoder — same
  field name, different method set (see §5).
- `scratch.asm` becomes `*a64.Asm`; `newScratch`/`reset` stay structurally
  identical (`sc.asm.B = sc.asm.B[:0]`).
- The rest of the imports (`wasm`, `codegen`, `runtime/abi`, stdlib) are unchanged.
  The returned compiled-module type is the arm64 encoder's equivalent of
  `amd64.CompiledModule` (`{ Code []byte; Entry, InternalEntry []int }`); if it does
  not yet exist in `encoder/arm64`, add it there with the identical shape.

---

## 2. Register model (arm64 roles)

Register **values are `a64.Reg`**: `X0=0 … X30=30`, `XZR/SP=31`. Use the encoder's
named constants (`a64.X0`, `a64.FP`, `a64.LR`, `a64.SP`, `a64.ZR`). `regNone` stays
the sentinel `a64.Reg(0xFF)`.

Roles are adapted from WARP `aarch64_cc.hpp` REGS to wago's runtime (which keeps
linMem pinned for the whole function and reads its basedata at negative offsets,
exactly as the amd64 backend does from `RBX`). Concrete assignment:

| Role | amd64 | **arm64** | Notes |
|---|---|---|---|
| Linear-memory base (whole-fn pinned) | `RBX` | **`X28`** | callee-saved; moved from the incoming arg reg in the prologue and never reallocated. Basedata read at negative offsets `[X28-…]`. |
| Linear-memory size cache (`memSizeReg`) | `R15` | **`X27`** | matches WARP `memSize=R27`. Module-wide, reserved out of every pool when explicit bounds + memory present; `regNone` otherwise. Established at offset-0 entry, refreshed by `memory.grow`. |
| Module-pinned globals (`moduleGlobalRegs`) | `R14,R13,R12` | **`X26,X25,X24`** | callee-saved; carved out per module via `f.reserved`. |
| Pinned hot-int-local base (`pinnedLocalRegs`) | `R12–R15` | **`X19,X20,X21,X22,X23`** | callee-saved; survive the Go boundary because the trampoline preserves AAPCS64 callee-saved regs (see below). Note X24–X27 are *also* callee-saved but reserved for the roles above; the assigner blocks whichever are in use via `f.reserved`. |
| Pinned hot-float-local (`pinnedFLocalRegs`) | XMM12–15 | **`V8,V9,V10,V11`** | the AAPCS64 callee-saved V-register range (low 64 bits preserved). |
| Block-merge register (`mergeReg`) | `RBP` | **`X15`** | freely-allocatable temp, not a pinned/scratch reg. |
| Float merge register (`mergeFReg`) | XMM11 | **`V15`** | freely-allocatable float temp. |
| Int result register | `RAX` | **`X0`** | AAPCS64 return reg; also arg 0. |
| Float result register | XMM0 | **`V0`** | AAPCS64 FP return. |
| Backend fixed scratch (encoder internal) | (n/a — x86 has scratch via `RAX` etc.) | **`X16,X17`** | IP0/IP1. Reserved out of the allocatable file entirely; used only by the encoder/emit helpers to materialize large immediates, effective addresses, and the memcpy/memset loops. Never hold a value elem. |
| Platform reserved | (n/a) | **`X18`** | excluded from every pool (Apple/Windows platform register). |
| Frame pointer / return | `RSP`/(implicit ret addr) | **`X29`=FP, `X30`=LR, `31`=SP** | SP is not a GPR; FP/LR are reserved out of the allocatable file. |

### AAPCS64 arg / call registers

- Wasm **reg-ABI** internal-entry args (wago's private convention, analog of amd64
  `intArgRegs = RAX,RCX,RDX,R8,R9,R10,R11`): use **`intArgRegs = X0,X1,X2,X3,X4,X5,X6,X7`**
  and **`fpArgRegs = V0,V1,V2,V3,V4,V5,V6,V7`** — the natural AAPCS64 arg registers.
- Wrapper-ABI (offset-0) entry — the analog of amd64 enterNative's
  `RDI=serArgs, RSI=linMem, RDX=trap, RCX=results`: **`X0=serArgs, X1=linMem,
  X2=trapCellPtr, X3=resultsPtr`**. The prologue moves `X1→X28` (linMem), stashes
  `X3` (results) into the frame header, and reads the trap cell from basedata (not a
  frame slot), mirroring amd64.
- Call scratch (analog of WARP `callScrRegs = R9,R10,R11`): use **`X9,X10,X11`** as
  the always-available call-clobber scratch.

### Allocation pool (`gpAlloc`, priority order — caller-saved temps first)

Mirror amd64's `gpAlloc` structure: freely-allocatable temps first, then
callee-saved pin candidates, then the "reserved scratch"/result registers last.
On arm64 there are **no fixed-register ALU ops**, so the reserved-scratch tail
exists only to keep the result register free; `numScratchGP` shrinks to **2**.

```go
var gpAlloc = []Reg{
    // freely allocatable, caller-saved temporaries (preferred for short-lived ops)
    X9, X10, X11, X12, X13, X14, X15, X8, X7, X6, X5, X4, X3, X2,
    // callee-saved: pinned-local / module-global / memSize candidates
    X19, X20, X21, X22, X23, X24, X25, X26, X27,
    // reserved scratch / result, allocated last (mirrors amd64 RAX/RDX/RCX/R8)
    X1, X0,
}
const numScratchGP = 2 // X1, X0
```

**Permanently excluded** from `gpAlloc` (never allocatable): `X28` (linMem),
`X16`,`X17` (backend scratch), `X18` (platform), `X29`(FP), `X30`(LR), `31`(SP/ZR).
`X27` (memSize) and the module-global registers are *in* the callee-saved block and
removed dynamically via `f.reserved`, exactly as amd64 keeps `R15`/`R12–R14` in
`gpAlloc` and removes them via `f.reserved`.

`pinnedLocalRegs = {X19,X20,X21,X22,X23}`; the extended pin pool (`gpPinPool`) adds
the call scratch `X9,X10,X11` and the merge register `X15`, all spill-managed
around calls by the STACK_REG model — structurally the same as amd64 adding
`R9,R10,R11,RBP`.

### Callee-saved / Go-boundary assumption (load-bearing)

The wago arm64 trampoline (the `enterNative` analog) **saves and restores all
AAPCS64 callee-saved registers** (`X19–X28`, and the low 64 bits of `V8–V15`)
around the wasm activation. Therefore wasm functions may clobber `X19–X28`/`V8–V11`
without saving them — this is what makes pinned locals and linMem free at the Go
boundary, exactly as amd64 relies on `enterNative` preserving `RBX,R12–R15`.
**Wasm functions themselves do NOT save/restore X19–X28.** The only per-function
save is **LR (and FP)** in call-making functions (see §4h).

---

## 3. Type mapping (keep the amd64 names)

Every ported file must keep the **same type, field, and method names** as amd64 so
the twins read like the originals. Concretely:

- `type Reg = a64.Reg`, `type Cond = a64.Cond` (in the arm64 `cc.go`).
- `machineType`, `storage`, `storageKind`, `elem`, `elemKind`, `stack`, `fn`,
  `regMask`, `wOp`, `aluEnc`, `localDef`, `ctrlFrame`, `funcHints`, `scratch`,
  `CompileOptions`, `CompiledModule` — **identical names and fields**. The
  operand-stack / valent-block / condense-engine architecture is architecture-
  neutral and ports verbatim except at the leaves where it calls the encoder.
- `storageKind` constants keep their names, including **`stMemRef`** (see §4a) and
  `stLocalReg`/`stGlobReg`/`stLocalRef`/`stGlobalRef`/`stSlot`/`stConst`/`stReg`.
- `Cond` maps to the a64 condition set. The amd64 `condXX` package constants become
  the a64 encodings; keep the `condXX` names so the compare/branch code is a
  mechanical rename:

  | amd64 | arm64 (`a64.Cond`) | meaning |
  |---|---|---|
  | `condE`  | `a64.CondEQ` | == |
  | `condNE` | `a64.CondNE` | != |
  | `condB`  | `a64.CondCC` | unsigned < (LO) |
  | `condAE` | `a64.CondCS` | unsigned >= (HS) |
  | `condBE` | `a64.CondLS` | unsigned <= |
  | `condA`  | `a64.CondHI` | unsigned > |
  | `condL`  | `a64.CondLT` | signed < |
  | `condGE` | `a64.CondGE` | signed >= |
  | `condLE` | `a64.CondLE` | signed <= |
  | `condG`  | `a64.CondGT` | signed > |
  | `condS`  | `a64.CondMI` | negative (sign) |
  | `condP`/`condNP` | *(no parity flag on arm64)* | must be lowered differently — see §4b note. |

  Declare these as `const condE = a64.CondEQ` … in `cc.go` exactly as amd64 declares
  `condE = amd64.CondE`.

- `regNone`, `maskOf`, `regMask.add/remove/has/union`, `mergeReg`, `mergeFReg` keep
  their names and semantics (`mergeReg = X15`, `mergeFReg Reg = 15` for `V15`).

---

## 4. x86 → arm64 lowering cheatsheet (the crux)

The condense engine and emit files are where the ISA difference lives. The rule of
thumb: **AArch64 is a load/store, three-operand, orthogonal RISC** — no memory
operands, no flags side-effects on ordinary ALU ops, no fixed-register
instructions. Every fold that x86 did inside one instruction becomes an explicit
sequence.

### (a) NO memory operands — `AluRM`/`AluIdx`/`cmpRM` folding disappears

x86 folds a memory source directly into an ALU/CMP (`op dest, [mem]`). AArch64
cannot. **Every memory operand becomes an explicit `LDR` into a register, then a
reg-reg op.**

- **Keep the `stMemRef` storage kind** and the deferred-load model (bounds check
  runs, address parked in a register, load deferred). What changes is one predicate:

  ```go
  // arm64: a deferred load can NEVER be folded as an ALU/CMP operand.
  func memRefFoldable(st storage, w bool) bool { return false }
  ```

  With `memRefFoldable` always false, `applyALU`/`applyMul`/`condenseCompare`
  automatically take their existing `else` branch (`memRefValue` → `LDR` into a
  reg → reg-reg op), so those functions port almost unchanged — just delete the
  `case stSlot`/`stLocalRef`/`stMemRef` *fold* arms that call `AluRM`/`AluIdx`/
  `ImulRM`/`ImulIdx` and route them through `materialize`→reg-reg instead.
- `applyALU`'s `stSlot` and `stLocalRef` arms (which emitted `AluRM dest,[RSP+off]`)
  become: `LDR tmp,[SP,#off]` (or `[X28,#off]` for a local via frame) then reg-reg.
  Prefer `f.materialize(right)` to get a register, then the reg-reg op, then
  release — this reuses the neutral materialize path and keeps the code short.
- `loadMemRef` becomes an `LDR` with the linMem base `X28` + effective-address
  register (+ folded displacement), honoring sub-width sign/zero extension via the
  load size (`LDRB/LDRH/LDR` zero-extend; use `LDRSB/LDRSH/LDRSW` or a following
  `SXTB/SXTH/SXTW` for signed). See §5 (`LoadIdx`).

### (b) Flags: `cmp`+`setcc`, `cmp`+`Jcc`, `cmov`

x86 sets EFLAGS as a side effect and consumes them with `setcc`/`Jcc`/`cmov`.
AArch64 sets **NZCV** only via the `-S` forms (`SUBS`/`ADDS`/`ANDS`) and dedicated
`CMP`/`CMN`/`TST`; ordinary `ADD`/`SUB`/`AND` do **not** touch flags.

- `cmp` + `setcc` (`condenseCompare`) → **`CMP` (a64 `CmpReg32/64` or `CmpImm*`) +
  `a64.Cset32/Cset64`** with the mapped `Cond`. The whole `condenseCompare`
  structure ports; replace `f.a.AluRI(cmpDigit,…)` with `f.a.CmpImm*`,
  `f.cmpRR`→`CmpReg*`, and `f.a.SetccReg(cc,result)`→`f.a.Cset*(result,cc)`.
- `eqz` → `TestSelf(L)` (x86 `test r,r`) becomes **`CmpImm(L,0)`** (SUBS XZR,L,#0)
  then `Cset(EQ)` — or, when feeding a branch directly, `CBZ`/`CBNZ` (no explicit
  compare). Prefer `CmpImm(L,0)`+`Cset` in `condenseCompare` for parity.
- `cmp` + `Jcc` (branch fusion, boundshoist / control) → **`CMP` + `a64.Bcond(cc)`**,
  patched with `PatchBranch19`. For the common `== 0` / `!= 0` branch, emit
  **`CBZ`/`CBNZ`** directly (no compare). Note the **±1 MiB range** of `Bcond`/
  `CBZ`/`CBNZ` (imm19) vs `Branch`'s ±128 MiB (imm26) — see §4-patching.
- `cmov` (select fusion, `Cmovcc`) → **`a64.Csel64/Csel32`**. Important semantic
  difference: x86 `cmov dest, src` uses `dest` as the false-value implicitly;
  AArch64 `CSEL Rd, Rn(true), Rm(false), cond` takes **both** sources explicitly.
  The select lowering must pass the current `dest`/false register as `Rm`.
- **No parity flag (`condP`/`condNP`).** x86 uses PF for unordered float compares
  (`f32.eq` etc. NaN handling). On arm64, `FCMP` sets NZCV with a defined unordered
  result (`V` set / specific combos); float compares lower directly to `FCMP` +
  `Cset` with the appropriate `Cond` (e.g. unordered-aware `CondVS`/`CondVC` or the
  arm64 float-compare condition table). The fp.go porter owns the exact mapping;
  do NOT emit parity-based sequences.

### (c) Fixed-register x86 ops → orthogonal arm64 ops (no fixed regs)

- **Multiply** (`imul`, x86 low-half): `Mul32`/`Mul64` (a64 `MADD Rd,Rn,Rm,XZR`).
  No `RDX:RAX` pair — 32/64-bit wasm `mul` only needs the low product. `applyMul`'s
  const/reg arms become: const → materialize into a reg (or the shift/LEA strength
  reductions from `pushBinOp`) then `Mul`; the `ImulRM`/`ImulIdx` fold arms go
  through `materialize` (see §4a).
- **Divide / remainder** (`idiv`/`div` with `RDX:RAX`, `Cdq`): AArch64 has
  orthogonal **`SDIV`/`UDIV`** (any registers, no `Cdq`, no `RDX:RAX`). Remainder is
  **`MSUB rem, quot, divisor, dividend`** (`rem = dividend - quot*divisor`). So
  `condenseDivRem` **rewrites substantially** but stays much simpler:
  - No `spillIfUsed(RAX/RDX)`, no `pinned RAX/RDX`, no `Cdq`. Allocate three normal
    regs: dividend, divisor, result.
  - div-by-zero trap: `CmpImm(divisor,0)` + `trapIf(condE, trapDivZero)` — keep the
    trap protocol identical (it uses the neutral `trapIf`).
  - signed `INT_MIN/-1` overflow trap for `div_s`: compare divisor to `-1` and
    dividend to INT_MIN, `trapIf` — same logic, arm64 compares.
  - `rem_s` `x % -1 == 0` special-case: same branch structure, `MSUB` for the
    normal remainder.
  - Constant-divisor strength reduction (`tryDivByConst`) is neutral arithmetic;
    port it, but the multiply-high path uses `SMULH`/`UMULH` (add to encoder) or
    fall back to `SDIV`/`UDIV` if you skip magic-number division in v1.
- **Variable shift** (x86 shift-by-`CL`, `ShiftCL`): AArch64 has orthogonal
  **`LSLV`/`LSRV`/`ASRV`/`RORV`** (shift `Rn` by `Rm mod width`, any registers). So
  `condenseShift`'s whole "force count into RCX / spill RCX / pin RCX" dance
  **disappears** — just materialize count into any reg and emit the variable shift.
  - `rotl` has no direct arm64 op: `rotl(x,n)` = `RORV(x, (width - n) mod width)`.
    For a constant count, use `ROR #(width-n)`. For a variable count, compute
    `NEG tmp,n` (or `SUB tmp, width, n`) then `RORV`. `rotr` = `RORV` directly.
  - Constant-count shift → immediate form (`LslImm/LsrImm/AsrImm/RorImm`, see §5),
    with the same `& (width-1)` masking the amd64 code already does.

### (d) Constant materialization

- `MovImm32`/`MovImm64` → **`a64.MovImm64`** (already emits the shortest
  MOVZ/MOVN + MOVK sequence). For a 32-bit constant, materialize the
  zero-extended `uint32` value (`f.a.MovImm64(r, uint64(uint32(v)))`) — a 32-bit
  write semantics is achieved by zero-extension; the beachhead already does this.
  Add a thin `MovImm32` helper on the encoder only if a porter wants the name
  parity (optional). Register-zeroing (`XorSelf32`) → `MovImm64(r,0)` (equivalently
  `MOVZ r,#0`); do NOT use `EOR r,r,r` expecting a flag effect.
- **Large ALU immediates**: x86 folds any imm32 into the ALU op (`AluRI`). AArch64
  add/sub take a **12-bit** immediate (optionally `<<12`); logical ops take a
  **bitmask immediate** (rotated run of ones). So `AluRI` becomes a **check-then-
  fallback**:
  - add/sub: if `0 <= imm <= 0xFFF` (or fits `imm<<12`), use `AddImm*`/`SubImm*`;
    else `MovImm64(tmp,imm)` + reg-reg `Add*`/`Sub*`.
  - and/or/xor: try `AndImm64/OrrImm64/EorImm64` (they return `ok bool` — the
    bitmask-immediate encodability check is already built in); on `false`,
    `MovImm64(tmp,imm)` + reg-reg logical. **Provide the 32-bit bitmask variants**
    (`AndImm32` etc.) — see §5.
  - compare: `CmpImm*` (imm12) with the same fallback to `MovImm64`+`CmpReg*`.
  - The amd64 `fitsImm32` gate is replaced per-op by these encodability checks.
    Keep a helper `fitsAddSubImm12(v)` and lean on the encoder's `ok` returns for
    logical/bitmask.

### (e) RIP-relative LEA + `br_table` jump table

- x86 `lea dst,[rip+disp]` / jump tables via `[base + idx*8]`. AArch64:
  **`ADR`/`ADRP`** (PC-relative address) → to add.
- `br_table`: emit the jump table (as data after the function or a literal island),
  materialize its base with `ADRP`+`ADD` (or `ADR` for a near table), then
  `LDR Xt,[base, idx, LSL #2]` (word offsets for relative entries, or `#3` for
  absolute 8-byte entries), then **`BR Xt`** (`a64.Br`, already exists). Use the
  backend scratch `X16`/`X17` for the base/target so no value register is
  clobbered. The neutral index-in-a-register hazard from the amd64 `br_table` RAX
  bug does not apply, but still keep the index in an owned register distinct from
  the table-base scratch.
- General LEA (`LeaDispW`, `LeaScaledW`) has no arm64 instruction; lower to
  arithmetic: `LeaDisp(dst,base,disp)` → `AddImm*`/`SubImm*` (or `MovImm64`+`Add`
  for large disp); `LeaScaled(dst,base,index,scale)` → **`ADD dst, base, index,
  LSL #scale`** (add shifted-register form — see §5 `AddShifted`). The
  scaled-index-add and `x*{3,5,9}` LEA strength reductions in `condenseBinary`
  therefore lower to one `ADD (shifted)` — port them using `AddShifted`.

### (f) `rep movsb` / `rep stosb` (memory.copy / memory.fill)

x86 uses the `rep movsb`/`rep stosb` string ops (`RepMovsb`). AArch64 has **no
string instruction** — emit an **explicit byte-copy / fill loop**:

```
; memory.copy: X_src, X_dst, X_len already computed (byte pointers into X28)
  CBZ   Xlen, done
loop:
  LDRB  Wtmp, [Xsrc], #1      ; post-index
  STRB  Wtmp, [Xdst], #1
  SUBS  Xlen, Xlen, #1
  B.NE  loop
done:
```

Use `X16`/`X17`/call-scratch for the loop temporaries. Add post-indexed
`LDRB/STRB` (see §5) or synthesize with base+offset + `AddImm`. For `memory.fill`
the loop stores the fill byte. Guidance: keep the overlap-correct direction for
`memory.copy` (copy backward when `dst > src` and ranges overlap) — the amd64
`rep movsb` path relies on the runtime's direction handling; replicate the same
direction decision the amd64 emit made before its `RepMovsb`. A future optimization
may widen to 8-byte copies, but v1 correctness = the byte loop.

### (g) SSE/AVX → NEON scalar-FP / vector (fp.go, simd.go) — LEAST COMPLETE

These two files are the least-complete part of the port. **Port the structure and
control flow, but expect to add nearly every encoder method** — `encoder/arm64`
currently has **no** floating-point or vector methods. Porters of `fp.go`/`simd.go`
must:

1. Keep the neutral dispatch (`condenseConvert`/float compare/`materializeF`/
   `materializeV128` shapes) identical.
2. Replace each `SseRR`/`Sse*`/`VMovdqu*`/`FLoadDisp`/`FStoreDisp`/`FMov` call with
   the NEON equivalent, and **enumerate + add** the encoder methods below. Scalar
   FP uses the `S`/`D` register views of `V0–V31`; v128 uses the `Q`/`.16b` views.

Minimum NEON/FP encoder methods to add (base opcodes where known; verify against
clang like `asm_test.go` does):

- Scalar arithmetic: `FaddS/D`, `FsubS/D`, `FmulS/D`, `FdivS/D`, `FsqrtS/D`,
  `FabsS/D`, `FnegS/D`, `FminS/D`, `FmaxS/D`, `FrintP/M/Z/N` (ceil/floor/trunc/
  nearest for `f*.ceil/floor/trunc/nearest`).
- Compare: `FcmpS/D` (sets NZCV) → then `Cset` with the float condition mapping.
- Convert: `FcvtS2D`/`FcvtD2S` (`fcvt`), `FcvtzsS/D`/`FcvtzuS/D` (f→i trunc,
  signed/unsigned), `ScvtfS/D`/`UcvtfS/D` (i→f). Handle the saturating
  `i*.trunc_sat_f*` variants per spec.
- Moves: `FmovRegS/D` (V→V), `FmovToGpr`/`FmovFromGpr` (`fmov x,d` / `fmov d,x` for
  `reinterpret` and gpr↔fpr), `FmovImm` (optional; else load from a constant island
  — see `preloadFloatConsts`, which stays neutral).
- Load/store: `LdrS/D/Q`, `StrS/D/Q` (scaled-immediate, base `X28`/`SP`), and a
  register-offset form for indexed memory access.
- v128 (simd.go): the NEON `.16b`/lane ops — add incrementally; this is explicitly
  the lowest priority and may lag behind the integer/float MVP. Structure it so the
  unsupported opcodes return a clear "unsupported" error rather than mis-encoding.

`popcnt` (integer, but NEON-based): AArch64 base has no scalar `POPCNT`. Lower
`i32/i64.popcnt` as **`FMOV Dn, Xsrc` → `CNT Vn.8b` → `ADDV Bn, Vn.8b` → `FMOV
Wdst, Sn`** (or `UADDLV`). Add `Cnt8b`, `Addv8b`, and reuse `FmovToGpr/FromGpr`.
Keep this in the integer unary path (`condenseUnary`) but flag it as needing the FP
register file.

### (h) Frame / prologue / epilogue / calls

x86: `sub rsp,frameSize` (frameless, no push), `ret` pops the return address the
`call` pushed. AArch64: **`BL` writes the return address into `LR (X30)`, pushing
nothing**, so a call-making function must preserve its own `LR`.

- **Prologue (call-making function):**
  ```
  STP  X29, X30, [SP, #-16]!    ; save FP/LR frame record (a64.StpPre)
  MOV  X29, SP                  ; frame pointer (optional but keep for backtraces)
  SUB  SP, SP, #frameSize       ; a64.SubSP64 (imm12; large frames → materialize)
  MOV  X28, X1                  ; linMem → X28 (from wrapper-ABI arg X1)
  (LDR W27,[X28,#-bdCurBytes])  ; establish memSize cache if applicable
  STR  X3,[SP,#frResultsOff]    ; stash results ptr
  <stack-fence check>           ; CMP SP, fence ; trapIf condLO
  <load params to pinned/slots>
  <zero declared locals>
  <derive pinned + module globals>
  ```
- **Leaf function (no calls):** skip the `STP`/`MOV X29,SP`/`LDP` frame record —
  `LR` is untouched, so `RET` returns directly. Still emit `SUB/ADD SP` if it uses
  spill slots/locals frame. Callee-saved `X19–X28` are NOT saved by the function
  (trampoline does it, §2).
- **Epilogue:**
  ```
  <store module globals / copy results to results buffer>
  ADD  SP, SP, #frameSize       ; a64.AddSP64
  LDP  X29, X30, [SP], #16      ; restore FP/LR (call-making only; a64.LdpPost)
  RET                           ; a64.Ret — returns via LR
  ```
- **SP alignment:** AArch64 requires SP **16-byte aligned** at all times. `align16`
  the frame; unlike amd64 there is no "+8 bias" (no return-address push to
  re-align) — the `STP …,[SP,#-16]!` keeps 16-alignment and `SUB SP` must stay a
  multiple of 16.
- **Frame-size patching:** amd64 patches a 32-bit `sub rsp` immediate after the body
  (`PatchU32(subRspAt,…)`). arm64's `SUB SP` immediate is only 12 bits. Two options,
  pick one and document it in `compile.go`:
  1. Reserve a fixed-size prologue (e.g. always a `MOVZ X16,#lo`+`MOVK`+`SUB SP,SP,X16`
     register-form slot) and patch the `MOVZ/MOVK` immediates — handles any frame
     size. Add `PatchMovz`/`PatchMovImm` helpers.
  2. If frame ≤ 4095 (common), patch the `imm12` field of the `SUB/ADD SP`
     instruction with a new `PatchAddSubImm12(at, imm)` helper, and fall back to the
     register form for larger frames.
  **Recommended:** option 1 (uniform, no size-class special-casing), using `X16`.
- **Direct wasm→wasm calls (`CallRel32`/reloc):** emit **`BL`** with a zero imm26
  placeholder, record the site in `f.relocs`, and patch at module layout with
  **`PatchBranch26`** (±128 MiB). Add the `Bl() int` placeholder method (§5). The
  neutral `callReloc` mechanism and module-layout patch loop in `CompileModuleWith`
  port unchanged except `binary.LittleEndian.PutUint32` becomes
  `a.PatchBranch26(site, target)`.
- **Indirect calls (`call_indirect`, host trampolines):** compute the target into a
  register and **`BLR Xn`** (`a64.Blr`, exists).
- **Around a call**, pinned locals/globals are spilled/reloaded by the neutral
  STACK_REG model exactly as amd64; the *only* arm64-specific addition is that
  call-making functions have already saved `LR` in the prologue.

---

## 5. Encoder method mapping (amd64 → arm64)

`E` = already exists in `src/core/encoder/arm64/asm.go`. `ADD` = the porter (or the
encoder owner) must add it; cross-check the base opcode against clang, as
`asm_test.go` does. When adding, keep the amd64-ish method-name spirit but arm64
mnemonics.

| amd64 encoder call | arm64 equivalent | Status | AArch64 base / note |
|---|---|---|---|
| `SubRsp` / `AddRsp` | `SubSP64` / `AddSP64` | **E** | imm12 only; large frames → register form (§4h) |
| `MovReg64` | `MovReg64` | **E** | `ORR Xd,XZR,Xm` |
| `MovRegReg32` (zext 32→64) | `MovReg32` | **E** | `MOV Wd,Wm` zero-extends |
| `MovImm64` | `MovImm64` | **E** | MOVZ/MOVN+MOVK |
| `MovImm32` | `MovImm64(r,uint64(uint32(v)))` | **E** (helper optional) | add `MovImm32` name if desired |
| `XorSelf32(r)` (zero reg) | `MovImm64(r,0)` / `Movz64(r,0,0)` | **E** | not a flag op on arm64 |
| `Load64/Store64/Load32/Store32` | `Load64/Store64/Load32/Store32` | **E** | ⚠ return `bool` (offset must be scaled-encodable); caller must fall back to register-offset/`AddImm` when `false` |
| `Load8/Store8/Load16/Store16` | `Ldrb/Strb/Ldrh/Strh` | **E** | zero-extending loads |
| signed sub-width load | `LdrsbRegOff`/`LdrshRegOff`/`Ldrsw` or `SXTB/SXTH/SXTW` after load | **ADD** | |
| `AluRR(rr,d,s,w)` | dispatch → `Add/Sub/And/Orr/Eor {32,64}` | **E** | 3-operand: `op Rd,Rn,Rm` with `Rd==Rn==dest` |
| `AluRI(digit,d,imm,w)` | `AddImm*`/`SubImm*` or `AndImm*`/`OrrImm*`/`EorImm*`, else `MovImm64`+reg-reg | **E (partial)** | add-sub imm12 vs bitmask imm; see §4d |
| `AluRM`/`AluIdx` (mem fold) | *(none)* → `LDR`+reg-reg | **n/a** | `memRefFoldable`→false (§4a) |
| `Cmp64/Cmp32` (`cmpRR`) | `CmpReg64/CmpReg32` | **E** | `SUBS XZR,Rn,Rm` |
| cmp-imm | `CmpImm64/CmpImm32` | **E** | imm12; else `MovImm64`+`CmpReg` |
| `TestSelf(r,w)` (eqz/divzero) | `CmpImm*(r,0)` (or `CBZ`/`CBNZ` for branch) | **E** | optional `Tst/Ands` for true TEST |
| `SetccReg(cc,r)` | `Cset64/Cset32(r,cc)` | **E** | |
| `Cmovcc(cc,dst,src)` | `Csel64/Csel32(dst,trueReg,falseReg,cc)` | **E (64)** | ⚠ both sources explicit; add `Csel32` |
| `JccPlaceholder(cc)` | `Bcond(cc)` | **E** | patch `PatchBranch19` (±1 MiB) |
| `JmpPlaceholder()` | `Branch()` | **E** | patch `PatchBranch26` (±128 MiB) |
| `Jcc`/`Jmp` on `==0`/`!=0` | `Cbz64/Cbnz64` | **E** | patch `PatchBranch19` |
| `PatchRel32` | `PatchBranch19` (cond/cbz) **or** `PatchBranch26` (B) | **E** | ⚠ backend must track which per site (two ranges) |
| `CallRel32` (direct call reloc) | `Bl()` placeholder + `PatchBranch26` | **ADD** | base `0x94000000`, imm26 |
| `CallReg`/indirect | `Blr(rn)` | **E** | |
| `Ret` | `Ret` | **E** | via LR |
| `Push`/`Pop` | `StpPre`/`LdpPost` (frame record) or pre/post-index STR/LDR | **E (pair)** | add single-reg pre/post index if needed |
| `LeaDispW(d,base,disp)` | `AddImm*`/`SubImm*` (or `MovImm64`+`Add`) | **E** | |
| `LeaScaledW(d,base,idx,scale,disp)` | `AddShifted(d,base,idx,LSL,scale)` (+ disp add) | **ADD** | `ADD (shifted reg)` base `0x8B000000` + `shift<<22` + `imm6<<10` |
| `Movsxd` (sext 32→64) | `Sxtw` | **ADD** | `SBFM Xd,Xn,#0,#31` = `0x93407C00` |
| `Movsx8` | `Sxtb` | **ADD** | `SBFM …,#0,#7` (32/64 variants) |
| `Movsx16` | `Sxth` | **ADD** | `SBFM …,#0,#15` |
| zext 8/16 (`MovzxN`) | `Uxtb`/`Uxth` (`UBFM`) or `AndImm` | **ADD** | often free via `LDRB/LDRH` |
| `ShiftImm(digit,d,cnt,w)` | `LslImm/LsrImm/AsrImm/RorImm` | **ADD** | LSL/LSR=`UBFM`, ASR=`SBFM`, ROR=`EXTR` |
| `ShiftCL(digit,d,w)` (var shift) | `Lslv*/Lsrv*/Asrv*` + `Rorv*` | **E (+Rorv ADD)** | no RCX dance; `Rorv` base `…2C00` |
| `Idiv(d,w)` | `Sdiv64/Sdiv32` | **ADD** | `SDIV` base `0x9AC00C00` (64) / `0x1AC00C00` (32) |
| `Div(d,w)` | `Udiv64/Udiv32` | **ADD** | `UDIV` base `0x9AC00800` / `0x1AC00800` |
| `Cdq(w)` | *(none — SDIV needs no RDX:RAX)* | **n/a** | delete from div lowering |
| remainder | `Msub64/Msub32` | **ADD** | `MSUB Rd,Rn,Rm,Ra` base `0x9B008000` / `0x1B008000` |
| `IMul/ImulRI/IMul` (low half) | `Mul64/Mul32` | **E** | `MADD …,XZR`; const → `MovImm64`+`Mul` |
| `ImulRM/ImulIdx` (mem fold) | `LDR`+`Mul` | **n/a** | via `materialize` (§4a) |
| mul-high (magic division) | `Smulh/Umulh` | **ADD (optional)** | only if porting `tryDivByConst` magic path |
| `Lzcnt(d,s,w)` | `Clz64/Clz32` | **ADD** | `CLZ` base `0xDAC01000` / `0x5AC01000` |
| `Tzcnt(d,s,w)` | `Rbit*` + `Clz*` | **ADD** | `RBIT` base `0xDAC00000` / `0x5AC00000` |
| `Popcnt(d,s,w)` | `Fmov(gpr→V)` + `Cnt8b` + `Addv8b`/`Uaddlv` + `Fmov(V→gpr)` | **ADD (NEON)** | no scalar popcnt (§4g) |
| `RepMovsb`/`RepStosb` | explicit LDRB/STRB loop | **n/a** | emit loop, `X16/X17` temps (§4f) |
| `SseRR`/`Sse*` (float) | `FaddS/D`,`FsubS/D`,`FmulS/D`,`FdivS/D`,`Fsqrt`,`Fabs`,`Fneg`,`Fmin`,`Fmax`,`Frint*` | **ADD** | NEON scalar (§4g); least complete |
| `FLoadDisp/FStoreDisp` | `LdrS/D`,`StrS/D` (scaled imm; reg-offset form) | **ADD** | |
| `FMov` (V→V) / reinterpret | `FmovRegS/D`, `FmovToGpr`, `FmovFromGpr` | **ADD** | `fmov` variants |
| float compare | `FcmpS/D` + `Cset(cc)` | **ADD** | NZCV, NaN-defined (§4b) |
| float↔int convert | `FcvtzsS/D`,`FcvtzuS/D`,`ScvtfS/D`,`UcvtfS/D`,`FcvtS2D`,`FcvtD2S` | **ADD** | |
| `VMovdquLoadDisp/StoreDisp` (v128) | `LdrQ`/`StrQ` | **ADD** | 128-bit `Q` load/store |
| v128 lane/vector ops (simd.go) | NEON `.16b`/lane ops | **ADD** | incremental; lowest priority |
| `Align16` | `Align16` (NOP pad) | **ADD** | `NOP` = `0xD503201F` |
| `PatchU32` (frame size) | `PatchMovImm`/`PatchAddSubImm12` | **ADD** | see §4h frame-size patching |
| `Adr`/RIP-rel / jump-table base | `Adr`/`Adrp` (+ `Br`) | **ADD** | ADR `0x10000000`, ADRP `0x90000000` (split immlo/immhi); `Br` exists |
| register-offset load/store (memRef, memcpy) | `LdrRegOff`/`StrRegOff` (`[Xn,Xm]`) | **ADD** | e.g. `LDR Xt,[Xn,Xm,LSL#0]` base `0xF8606800` |
| add/sub imm with `LSL #12` (big frame/offset) | `AddImm12Shift`/`SubImm12Shift` | **ADD** | `sh` bit in add/sub-imm |
| 32-bit bitmask logical imm | `AndImm32`/`OrrImm32`/`EorImm32` | **ADD** | `encodeLogicalImm(_, false)` already supports 32-bit |

---

## 6. Conventions & gotchas (read before writing any file)

1. **Offset-encodability is not free.** Unlike x86's arbitrary `disp32`, arm64
   scaled-immediate loads/stores encode a *scaled* 12-bit offset and the encoder
   methods **return `bool`**. Every `f.a.Load64/Store64(...)` etc. must check the
   result and fall back (materialize the offset with `AddImm`/`MovImm64` into a
   scratch, then register-offset or base+0). Frame slots (`localOff`/`spillOff`)
   are 8-byte-aligned so they usually encode, but large frames will overflow the
   12-bit range — write a `f.ld64`/`f.st64` helper that hides the fallback and use
   it everywhere instead of calling the raw encoder. **Do not ignore the `bool`.**
2. **Branch range split.** `Bcond`/`CBZ`/`CBNZ` reach ±1 MiB (imm19); `B`/`BL`
   reach ±128 MiB (imm26). The neutral control code tracks patch sites; the arm64
   twin must record *which* kind each site is (the beachhead's `pend{at, wide}`
   flag is the pattern) and call the matching `PatchBranch19`/`PatchBranch26`. A
   conditional branch to a far target must be lowered as `B.<inv> +8 ; B target`.
3. **Two-source ops, `Rd==Rn` accumulation.** amd64's in-place `op dest, src`
   (dest is both source and destination) becomes `op Rd, Rn, Rm` with
   `Rd==Rn==dest`. The condense engine's "compute LHS into dest, then apply RHS in
   place" model still works — just pass `dest` as both `Rd` and `Rn`. The RHS-alias
   / RHS-relocation hazard handling in `condenseBinary` is neutral and ports as-is
   (it exists to protect the RHS register from the LHS computation, which is still
   real on arm64).
4. **No implicit flag setting.** Never assume an `ADD`/`SUB`/`AND` set flags for a
   following branch. Use `ADDS`/`SUBS`/`ANDS`/`CMP`/`TST` explicitly. The encoder
   exposes `Adds64`/`Subs64`/`SubsImm64`; add `-S` and `TST/ANDS` variants as the
   compare/branch paths need them.
5. **`producesCleanI32`** (upper-bits-zero reasoning) stays valid: a 32-bit arm64
   write (`W`-register destination) zeroes the upper 32 bits of the `X` register,
   exactly like x86-64. Keep the redundant-zero-extend elimination.
6. **`memRefStorage`/`fmemRefStorage`/`memBorrow`** semantics are unchanged; only
   `memRefFoldable` flips to `false` (§4a) and `loadMemRef` becomes an LDR. The
   deferred-load-before-store ordering (`materializePendingLoads`) is neutral —
   port verbatim.
7. **linMem is `X28`, not a frame/`RSP` base.** Wherever amd64 addresses
   `[RBX ± off]` (basedata: globals ptr, `bdCurBytes`, fence at `-72`), arm64
   addresses `[X28 ± off]`. Wherever amd64 uses `[RSP + off]` for frame
   locals/spills, arm64 uses `[SP + off]` (SP = reg 31 in the load/store base
   position). Keep `localOff`/`spillOff`/`frameHdrBytes`/`frResultsOff` neutral;
   only the base register changes.
8. **Keep the STACK_REG / pin / merge / bounds-facts machinery neutral.** It is
   register-count and hazard logic, not ISA-specific. The only edits are the
   register *identities* (§2) and the leaf-vs-call frame record (§4h). Do not
   redesign it.
9. **Trap protocol is neutral.** `trapIf`, `trapSites`, `emitTrapStubs`,
   `retSites`, the trap-cell-in-basedata convention — port verbatim, replacing
   `Jcc`/`Jmp` placeholders with `Bcond`/`Branch` and `PatchRel32` with the
   matching branch patch.
10. **When you must add an encoder method,** add it to
    `src/core/encoder/arm64/asm.go` with a golden test in `asm_test.go` (clang +
    llvm-objdump cross-check) in the *same* change — the backend must never depend
    on an unverified opcode word.
