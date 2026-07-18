# RP2350 32-bit backend port

This document is the engineering contract for bringing wago's direct WebAssembly
code generation to the two processor architectures available on Raspberry Pi
Pico 2 / RP2350:

- Armv8-M Mainline Thumb-2 on Cortex-M33 (`arm32` in this repository); and
- little-endian RV32 on Hazard3 (`riscv32` in this repository).

Neither target is admitted by the public `wago` package yet. The existing Arm
backend emits AArch64 and cannot execute on Cortex-M33; the existing RISC-V
backend emits RV64G and cannot execute on Hazard3. New target code must remain
strict: a staged compiler may support a documented subset, but unsupported Wasm
value types or instructions must be rejected rather than miscompiled.

## Product boundary

The first product is a cross-host compiler plus a small bare-metal execution
harness. Standard Go does not provide a 32-bit RISC-V port, and RP2350 does not
provide Linux, virtual memory, `mmap`, Unix signals, or the Linux
`riscv_flush_icache` syscall. Consequently the Linux/RV64 runtime is not copied
behind new build tags.

The embedded runtime profile will use:

- explicit bounds checks only;
- a fixed SRAM linear-memory arena with a configured maximum;
- a fixed generated-code arena or host-generated firmware image;
- explicit trap and cancellation cells;
- a small helper-call table for operations that are not profitable to inline;
- little-endian 32-bit ABI slots; and
- a board transport that reports results, traps, and checksums to the host test
  harness.

Signal-backed guard pages and claims of W^X isolation are outside this profile.
If runtime-generated code is executed from SRAM, code publication and executable
permissions must be qualified independently on both RP2350 processor modes.

## ISA baselines

### RV32

The semantic baseline is fixed-width RV32IM. The encoder also exposes RV32A and
Zicsr instructions, but no backend feature may require compressed instructions,
floating-point extensions, vectors, bit manipulation, or Hazard3-specific
extensions. Floating-point semantics therefore use software helpers or integer
lowering.

### Arm32

The semantic baseline is Thumb-2 integer code valid on Armv8-M Mainline.
Generated Wasm SIMD does not require NEON or MVE/Helium. The initial encoder uses
wide data-processing instructions for predictable patching and permits the
architectural 16-bit branch-exchange, service, breakpoint, and NOP forms.
RP2350's double-precision coprocessor can become a measured Arm-only f64 tier,
but it is not the cross-architecture semantic baseline.

## Value representation

Values use little-endian register groups and are owned atomically:

| Wasm value | 32-bit representation |
|---|---|
| `i32`, `f32`, reference, address | one GPR |
| `i64`, `f64` | `{lo32, hi32}` GPR pair |
| `v128` | `{w0, w1, w2, w3}` GPR quad |

A spill, move, local pin, call argument, result, or control merge must operate on
the complete group. A partial group is invalid. The existing RV64 `v128` pair
work is the ownership model, but the 32-bit backend must generalize storage from
one optional second register to a bounded register group before full railshot
integration.

Serialized ABI slots are 32 bits. `i64`/`f64` consume two adjacent slots and
`v128` consumes four. Split memory stores preflight the complete Wasm access so
an out-of-bounds operation cannot partially mutate linear memory.

## Floating-point baseline

Hazard3 has no required `F` or `D` extension. The portable f64 baseline therefore
uses deterministic helper calls with raw `{lo32, hi32}` inputs and outputs.
Helpers must cover arithmetic, square root, rounding, comparisons, min/max,
conversions, trapping truncation, saturating truncation, NaNs, signed zero, and
subnormals. Arm may replace proven helpers with RP2350 double-coprocessor
sequences only after official Wasm tests and native measurements show exact
semantic parity and a benefit.

Bitwise f64 operations (`abs`, `neg`, `copysign`, reinterpretation) should be
lowered directly with integer instructions on both architectures.

## SIMD baseline

`v128` uses four GPRs and 32-bit SWAR. Direct word-wise operations include
bitwise logic and `i32x4` modular add/sub. Narrow packed lanes use masks and
carry barriers within each 32-bit word. Operations crossing word boundaries,
64-bit lanes, floating lanes, shuffles, and conversions may use scalar lane
sequences or bounded helper calls initially.

The backend is not publicly SIMD-capable until all 256 instructions recognized
by `wasm.SIMDSubopcodeValid` are represented in the lowering registry and the
official SIMD and relaxed-SIMD suites pass. Deterministic relaxed projections
must match the admitted RV64 policy.

## Validation ladder

1. Encoder golden words and immediate/range rejection on the host.
2. Generated ELF execution under `qemu-arm` and `qemu-riscv32` without external
   assemblers.
3. Cross-host beachhead compiler execution for i32 control and arithmetic.
4. Pair/quad ABI, spills, memory accesses, and helper relocations.
5. Scalar official corpus, then complete f64 and SIMD proposal suites.
6. Pico SDK runner on RP2350 Arm and Hazard3 modes with identical fixtures.
7. Native code-size, compile-time, SRAM, stack, and execution measurements.

QEMU is a correctness oracle, not a Pico 2 performance oracle. Public admission
requires board evidence and explicit documentation of the embedded runtime's
memory and security model.

## Current implementation status

The initial foundation contains:

- `src/core/encoder/riscv32`: fixed-width RV32I/M/A/Zicsr encoding, relocations,
  far transfers, multiword integer sequences, and four-GPR SWAR primitives;
- `src/core/encoder/arm32`: Thumb-2 integer encoding, relocations, conditional
  far transfers, multiword integer sequences, and four-GPR SWAR primitives;
- cross-host i32/control beachheads in
  `src/core/compiler/backend/railshot/{riscv32,arm32}`; and
- direct execution tests under `qemu-riscv32` and `qemu-arm`.

The architecture-neutral `src/core/runtime/embedded32` helper ABI now adds:

- allocation-free scalar f64 arithmetic, rounding, square root, comparisons,
  deterministic min/max, integer/float conversion, trapping truncation, and
  saturating truncation over little-endian 32-bit slots; and
- a complete 256-op SIMD helper registry exactly matching the decoder, including
  constants, shuffle, lane-immediate operations, every SIMD memory form,
  deterministic relaxed projections, and complete-width store preflight.
  Simple operations still have direct four-GPR SWAR sequences; the helper is the
  compact correctness fallback when direct lowering would create excessive code
  size or register pressure.

The helper ABI uses fixed 32-bit layouts independent of host pointer alignment.
Both backends now emit 16-byte operation thunks that write the operation id,
load a function from a two-slot helper table, and tail-call it without disturbing
RA/LR. QEMU execution tests exercise both the scalar-f64 and SIMD table slots on
both architectures. The helper package builds and tests with standard Go and
TinyGo.

Both compiler packages now also consume real Wasm function bodies for direct
wide-value execution beachheads:

- `i64.const` plus modular add/sub/multiply and bitwise operations over atomic
  two-GPR values;
- `f64.const`, `abs`, `neg`, and `copysign` over integer register pairs; and
- `v128.const`, bitwise operations, bitselect, packed `i8x16`/`i16x8` add/sub,
  and `i32x4` add/sub over four-GPR values.

The generated pair/quad code executes under both `qemu-riscv32` and `qemu-arm`;
these tests include cross-word `i64.mul`, packed carry-isolated SIMD arithmetic,
and f64 sign-bit behavior. More complex f64/SIMD operations use the complete
helper ABI while measurements decide which additional instructions merit direct
inline SWAR.

A shared fixed-capacity group allocator now owns one-, two-, and four-register
values atomically. Allocation, exact ABI acquisition, release, LRU spill-victim
selection, and exclusion masks operate on complete values; stale or partial
pair/quad release is rejected transactionally. Both direct wide compilers use
this allocator rather than independent per-register ownership.

The i64 path additionally supports pair parameters, zero-initialized pair
locals, and atomic `local.get`, `local.set`, and `local.tee`. Local homes use
callee-saved register pairs whose incoming values are preserved in aligned
frames. QEMU executes a parameter/local/tee/multiply fixture on both targets.

The i64 compiler has a bounded 64-byte operand-spill area after its local-home
save region. The v128 compiler now applies the same design with a bounded
128-byte quad-spill area. Both paths support parameters, zero-initialized locals,
and atomic `local.get`, `local.set`, and `local.tee`; occupied callee-saved homes
are preserved in aligned frames. When pressure exhausts the pool, the compiler
stores and releases a complete pair or quad, then reloads every word together
when consumed. QEMU fixtures force pair and quad spill/reload after reserving
local homes and verify identical results on RV32 and Thumb-2. Spill selection
cannot target local homes or expose a partial value.

This is still not public backend admission. Pair/quad control merges and calls
in the full module compiler, calls/globals/tables/references, the bare-metal
executable and linear-memory arenas, firmware linking, and Pico 2 hardware
qualification remain to be implemented and measured.
