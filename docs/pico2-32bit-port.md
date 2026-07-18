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
Its three-slot table now contains scalar-f64, SIMD, and scalar-i64 entries. The
i64 frame covers shifts, rotates, bit counts, `eqz`, every signed/unsigned
comparison, trapping division/remainder, i32 extension, and all i64
sign-extension operations with complete pair results and canonical traps. Both
backends emit 16-byte operation thunks that write the operation id, load the
selected helper, and tail-call it without disturbing RA/LR. QEMU execution tests
exercise target helper dispatch, while architecture-neutral tests cover the full
i64 semantic frame. The helper package builds and tests with standard Go and
TinyGo.

Both compiler packages now also consume real Wasm function bodies for direct
wide-value execution beachheads. Homogeneous f32 functions have a raw-bit
baseline for constants, locals, `abs`, `neg`, `copysign`, `nop`, and `drop` by
translation to the qualified one-word integer allocator; unsupported arithmetic
still rejects explicitly pending helper integration:

- `i64.const` plus modular add/sub/multiply and bitwise operations over atomic
  two-GPR values;
- `f64.const`, `abs`, `neg`, and `copysign` over integer register pairs; and
- `v128.const`, bitwise operations, bitselect, packed `i8x16`/`i16x8` add/sub,
  and `i32x4` add/sub over four-GPR values.

The generated pair/quad code executes under both `qemu-riscv32` and `qemu-arm`;
these tests include cross-word `i64.mul`, packed carry-isolated SIMD arithmetic,
and f64 sign-bit behavior. The i32 path now also lowers variable rotates,
`clz`, `ctz`, `popcnt`, and both sign-extension instructions without requiring
Arm or RISC-V bit-manipulation extensions; bounded count loops and rotate
sequences execute under both QEMU targets. Context-aware division/remainder
thunks cover all four signed/unsigned operations, preserve the defined signed
remainder result for `min / -1`, and write distinct canonical divide-by-zero and
signed-overflow traps. Normal function lowering still needs to reserve the
context register and route the trapping opcodes through these thunks. More
complex f64/SIMD operations use the complete helper ABI while measurements
decide which additional instructions merit direct inline SWAR.

A shared fixed-capacity group allocator now owns one-, two-, and four-register
values atomically. Allocation, exact ABI acquisition, release, LRU spill-victim
selection, and exclusion masks operate on complete values; stale or partial
pair/quad release is rejected transactionally. Both direct wide compilers use
this allocator rather than independent per-register ownership.

The i64 path additionally supports pair parameters, zero-initialized pair
locals, atomic `local.get`, `local.set`, and `local.tee`, direct `clz`, `ctz`,
and `popcnt`, direct variable shifts and rotates with Wasm's modulo-64 count
semantics, plus `extend8_s`,
`extend16_s`, and `extend32_s` with complete high-word sign propagation. The
portable shift/rotate baseline uses bounded one-bit loops; later direct
cross-word sequences remain an optimization, not a correctness dependency.
Local homes use
callee-saved register pairs whose incoming values are preserved in aligned
frames. QEMU executes a parameter/local/tee/multiply fixture on both targets.

The i64 and integer-only f64 compilers have bounded 64-byte pair-spill areas
after their local-home save regions. The v128 compiler applies the same design
with a bounded 128-byte quad-spill area. All three paths support parameters,
zero-initialized locals, and atomic `local.get`, `local.set`, and `local.tee`;
occupied callee-saved homes are preserved in aligned frames. When pressure
exhausts the pool, the compiler stores and releases a complete pair or quad,
then reloads every word together when consumed. Spill slots are tracked by a
fixed bitmap and returned on reload, so bounded frames reuse dead slots instead
of exhausting capacity monotonically. QEMU fixtures force i64, f64, and v128
spill/reload after reserving local homes and verify identical results on RV32
and Thumb-2. Spill selection cannot target local homes or expose a partial
value.

The architecture-neutral runtime now also has the fixed-capacity embedded
resource layer required by firmware:

- fixed-page linear memory over caller-supplied SRAM, with explicit overflow-safe
  bounds checks, zeroed growth, bounded `memory.grow`, and deterministic reset;
- transactional code-arena allocation with power-of-two alignment, rollback on
  publication failure, a target-specific publication callback, and rejection of
  overlapping transactions;
- a bounded native-stack arena with at most 32 fixed slots, zero-on-acquire and
  zero-on-release; and
- fixed-width context, trap, and cancellation cells suitable for physical
  32-bit target addresses; and
- a single-active-invocation runtime lifecycle that acquires/releases a native
  stack, resets control cells, rejects concurrent entry, and refuses reset while
  generated code is active.

These resource managers build and test under both standard Go and TinyGo. They
contain no MMU, signal, syscall, or host-pointer assumptions.

Both code generators now consume the fixed `ContextABI` for executable scalar
memory thunks. A shared opcode registry covers every core `i32`, `i64`, `f32`,
and `f64` load/store form, including signed and unsigned narrow loads and narrow
stores. Loads and stores combine the dynamic address and static Wasm offset with
overflow detection, preflight the complete access width against the current
memory length, write the canonical `TrapMemoryOutOfBounds` value on failure, and
only then access memory. Pair results are sign- or zero-extended into complete
little-endian register pairs. QEMU tests on both targets cover successful
unaligned narrow and full-width accesses, static-offset and end-of-memory
failures, canonical trap writes, and proof that an out-of-bounds split store
cannot mutate its in-bounds low word. Normal mixed-width function lowering still
needs to route decoded memargs through this registry.

A shared module-layout stage now compiles every local function in the currently
admitted scalar and direct-SWAR subsets into one 16-byte-aligned target image.
A bounded shared frame planner assigns exact contiguous 32-bit slots to mixed
parameters, locals, operands, and results, preserving one/two/four-slot values
atomically. Genuinely mixed functions now execute constants, mixed local
get/set/tee, complete drops, i32 arithmetic/logic, i64 add/sub/logic, raw-bit
f32/f64 sign operations, and direct v128 bitwise plus i32x4 add/sub operations.
The initial internal register ABI carries up to four serialized parameter/result
slots on Arm and eight on RV32; larger signatures reject pending stack arguments.
Mixed functions now stage complete arguments from their slot frames, preserve
live wide values, relocate direct mixed-to-mixed calls, return ordered multiple
results, and propagate nested traps without publishing partial values. Return
addresses are held in fixed frame slots, so nested and recursive mixed calls use
the same bounded native-stack checks as scalar calls. Calls into legacy
homogeneous beachheads remain rejected until those bodies use the final module
ABI. It
reconstructs validated local declarations, records bounded per-function
offset/size plus serialized parameter/result slot metadata,
performs conservative code-arena capacity preflight before code generation, and
rejects imports, unsupported runtime state, incompatible signatures, missing
byte-backed bodies, and unsupported instructions before publication. Module
functions reserve a fixed context register (`R11` or `x23`) and now lower all i32
load/store widths, `memory.size`, and trapping division/remainder directly through
`ContextABI`; static-offset overflow, complete-width bounds checks, and canonical
trap writes occur in normal function code. `CompileModuleToArena` uses the fixed
`CodeArena` transaction so capacity and target publication failures clear the
entire candidate image. QEMU executes selected functions, successful memory
loads, memory traps, and division traps from module images on both architectures.
Generated i32 module functions also implement bounded `memory.grow`: the
extended context publishes the fixed backing maximum, growth validates page-to-
byte overflow and capacity, zeroes the newly admitted range, refreshes the
current-length field, and returns the old page count or `-1`. The i32 path also executes `nop`, `drop`, untyped i32 `select`, typed i32
`select`, and void-label `br_table`; homogeneous i64, f64, and v128 functions can discard complete pair or
quad values without exposing partial storage. These functions poll the fixed
cancellation cell after parameter capture at function entry and
at every loop header, writing a canonical cancellation trap without relying on
signals or firmware exceptions. Explicit `unreachable` instructions likewise
write a distinct canonical trap from normal generated module code. The i32 module ABI now preserves callee-saved local homes and LR/RA in fixed
frames, checks each proposed frame against the context's downward-growing native
stack limit, stages arguments through bounded frame slots, spills live scratch values
across calls, relocates direct calls module-wide, supports recursion, and checks
the trap cell after nested returns. Stack exhaustion writes a distinct canonical
trap before any callee-save state is exposed. Mixed-width signatures and locals
now use exact bounded stack frames with entry cancellation and stack-limit
checks. Direct mixed-width calls and multiple results are now implemented for the
register-slot ABI. Stack arguments, indirect and imported calls, calls into
legacy homogeneous beachheads, and wide structured control remain outside this
module-wide slice.

Module metadata now retains local i32 globals with exact mutability and constant
initial values. Instantiation initializes bounded caller-provided 32-bit cells,
`ContextABI` publishes their target base address, and normal generated i32
functions execute `global.get`/`global.set` with immutable-set rejection.

The embedded runtime now also provides complete preflighted `memory.copy`,
`memory.fill`, passive `memory.init`, and idempotent `data.drop` semantics.
Normal i32 module code directly lowers overlap-safe `memory.copy` and
`memory.fill` with complete source/destination preflight before byte loops.
Module layout retains active/passive segments in original index order;
instantiation preflights every active destination transactionally before copying
and starts active segments dropped. `ContextABI` now publishes a stable array of
12-byte target data descriptors, and normal i32 functions execute preflighted
`memory.init` plus idempotent `data.drop` directly against those descriptors.

This is still not public backend admission. Pair/quad structured-control
merges, stack-argument calls, wide globals/tables/references, generated-
code entry trampolines, firmware linking and transport, and Pico 2 hardware
qualification remain to be implemented and measured.
