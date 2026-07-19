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

`TestPico2Release2CompileAdmission` is the opt-in module-level admission gate for
the pinned WebAssembly/spec v2.0.0 checkout. It runs `wast2json`, decodes every
emitted module, requires Arm32 and RV32 to agree, requires valid in-gate modules
to compile, requires malformed/invalid modules to reject, and counts only the
explicit multi-memory/multi-table/threads-memory64/exception/GC boundary as
outside the completion gate. Module compilation now routes i32-only signatures
through the same complete mixed-width planner instead of the legacy beachhead.

The pinned Linux/amd64 Release 2 execution target is freshly green at 1,600
modules and 48,248 assertions with zero failures, skips, or bounded gap reasons.
The first embedded admission run inspected 3,768 in-gate modules and excluded 90
modules at the then-current boundary. Routing function labels, early returns,
unreachable tails, and branch-carried results through one synthetic function
control frame reduced valid Arm32/RV32 rejections from 77 to 33. Normalized
Release 2 nullable reference encodings, null externref tables/globals/elements,
funcref global initializers, and descriptor-only passive/declarative elements
then reduced the count to 17. Resolving immutable imported i32 globals before
transactional active data/element preflight reduced it again to 8. Completing
implicit-else block parameters, reference-test stack accounting, and the exact
SIMD min-operation arity reduced it to 3. Context-switching imported start
thunks and separating declared table maxima from bounded backing capacity then
left only the guard-page stress module's intentionally huge live local frame,
while preserving identical target decisions and zero malformed/invalid
admissions. Indexed table resolution, directory-based get/set/size/grow/fill/
copy/init, and nonzero-table indirect calls then moved all 85 multi-table
modules into the gate. The current admission run passes across 3,853 modules
with only five multi-memory exclusions and one explicitly classified bounded
resource rejection for the 8.4 KiB live-local guard-page stress function. The
resource classifier accepts only the deterministic 256-slot mixed-frame limit;
all other valid-module rejections remain test failures. Closed firmware images now allocate bounded per-table descriptors/backing,
publish an indexed descriptor directory, apply active elements to their exact
target, and retain one module-wide passive/declarative descriptor set.
Linked bundles now bind imported table-directory entries directly to provider
descriptors, translate each module's local/imported `ref.func` values into one
bounded bundle-wide identity space, publish parallel entry/type/context arrays,
and switch module context for indirect calls. Active elements targeting imported
tables are applied only after complete bundle preflight. Execution assertions
still require the firmware or QEMU module runner.

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

- allocation-free scalar f32 and f64 arithmetic, rounding, square root,
  comparisons, deterministic min/max, integer/float conversion, trapping
  truncation, and saturating truncation over little-endian 32-bit slots; and
- a complete 256-op SIMD helper registry exactly matching the decoder, including
  constants, shuffle, lane-immediate operations, every SIMD memory form,
  deterministic relaxed projections, and complete-width store preflight.
  Simple operations still have direct four-GPR SWAR sequences; the helper is the
  compact correctness fallback when direct lowering would create excessive code
  size or register pressure.

The helper ABI uses fixed 32-bit layouts independent of host pointer alignment.
Its four-slot table contains scalar-f64, SIMD, scalar-i64, and scalar-f32
entries. Normal mixed module functions construct and dispatch all four helper
frames through this table, with canonical trap publication and atomic result
reloads. The
i64 frame covers shifts, rotates, bit counts, `eqz`, every signed/unsigned
comparison, trapping division/remainder, i32 extension, and all i64
sign-extension operations with complete pair results and canonical traps. Both
backends emit 16-byte operation thunks that write the operation id, load the
selected helper, and tail-call it without disturbing RA/LR. QEMU execution tests
exercise target helper dispatch, while architecture-neutral tests cover the full
i64 semantic frame. The helper package builds and tests with standard Go and
TinyGo.

Both compiler packages also retain standalone direct function beachheads.
Standalone homogeneous f32 functions have a raw-bit baseline for constants,
locals, `abs`, `neg`, `copysign`, `nop`, and `drop`. Module compilation routes
all non-i32 signatures through the context-aware mixed planner, so homogeneous
f32/f64/i64/v128 module functions use the same complete frame and helper ABI as
genuinely mixed functions:

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
signed-overflow traps. Normal function lowering reserves the fixed context
register and routes the trapping opcodes through these thunks. Mixed module functions now construct the stable 32-byte f64 helper frame in
their bounded native frame, dispatch through `ContextABI.HelperTable`, publish
helper traps, and reload complete results. This path covers f32/f64 rounding, square root, arithmetic, min/max,
comparisons, i32/i64 conversion, f32/f64 promotion/demotion, trapping truncation,
and saturating truncation; bitwise floating-point operations remain
direct. Mixed i64 helper dispatch covers shifts, rotates, bit counts, eqz and
all comparisons, division/remainder traps, i32 extension, and sign extensions;
add/sub/multiply/logic stay direct. Normal mixed functions now also construct the
stable 120-byte SIMD frame and dispatch every validated SIMD stack shape through
the SIMD helper-table slot when no direct SWAR lowering exists. This includes
scalar splats, shifts, comparisons, reductions, integer and floating
unary/binary/ternary operations, conversions, shuffle and lane immediates,
relaxed-SIMD projections, and every SIMD memory/lane-memory form. Static memory
offsets are overflow-checked before helper entry; the helper preflights complete
access widths and publishes canonical traps. Direct integer SWAR operations
remain inline. All 256 decoder-admitted core and relaxed SIMD instructions are
therefore represented in normal mixed function lowering, although official
proposal-suite qualification is still pending.
Measurements decide which helper operations merit direct target-specific
lowering.

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
cannot mutate its in-bounds low word. Normal mixed-width function lowering now routes every scalar i32/i64/f32/f64
memarg through the same registry. Mixed loads and stores retain static-offset
overflow checks, complete-width preflight, narrow signed/unsigned extension,
and no-partial-write guarantees while surrounding wide operands remain in frame
slots. SIMD memory and lane-memory forms use the complete mixed helper dispatch.

A shared module-layout stage now compiles every local function in the currently
admitted scalar and direct-SWAR subsets into one 16-byte-aligned target image.
A bounded shared frame planner assigns exact contiguous 32-bit slots to mixed
parameters, locals, operands, and results, preserving one/two/four-slot values
atomically. Genuinely mixed functions now execute constants, mixed local
get/set/tee, complete drops, all i32 arithmetic, bit counts, shifts, rotates,
signed/unsigned comparisons, trapping division/remainder, sign extensions, and
i64 wrapping, plus i64
add/sub/multiply/logic, raw-bit f32/f64 sign operations, and direct v128 bitwise
plus i32x4 add/sub operations.
The internal ABI carries the first four serialized parameter/result slots in
Arm registers and the first eight in RV32 registers. Additional slots use a
statically reserved, 16-byte-aligned outgoing area at the base of the caller's
bounded frame; callees import and publish overflow slots relative to their entry
stack pointer. Mixed functions stage complete arguments from their slot frames, preserve
live wide values, relocate direct mixed-to-mixed calls, return ordered multiple
results, execute typed or untyped atomic `select` across one/two/four-slot
values, accept terminal explicit returns, execute nested `if`/`else`, `block`,
and typed `loop` frames, resolve type-indexed parameter/result signatures,
atomically preserve block parameters and merge inline or multi-value typed
block/if results, preserve live
wide values across conditional arms, and lower
stack-neutral `br_if` with exact block results or loop parameters, plus
innermost unconditional branches carrying complete typed block results. Mixed loops poll
cancellation at every header. Nested traps propagate without publishing partial
values. Return
addresses are held in fixed frame slots, so nested and recursive mixed calls use
the same bounded native-stack checks as scalar calls. Calls into legacy
homogeneous beachheads remain rejected until those bodies use the final module
ABI. It retains validated exports and a local start-function index through
transactional publication so firmware instantiation can resolve public entries
and invoke start after state initialization. It
reconstructs validated local declarations, records bounded per-function
offset/size plus serialized parameter/result slot metadata,
performs conservative code-arena capacity preflight before code generation,
retains supported import contracts, and rejects unsupported runtime state,
incompatible signatures, missing byte-backed bodies, and unsupported
instructions before publication. Module
functions reserve a fixed context register (`R11` or `x23`) and now lower all i32
load/store widths, `memory.size`, and trapping division/remainder directly through
`ContextABI`; static-offset overflow, complete-width bounds checks, and canonical
trap writes occur in normal function code. `CompileModuleToArena` uses the fixed
`CodeArena` transaction so capacity and target publication failures clear the
entire candidate image. QEMU executes selected functions, successful memory
loads, memory traps, and division traps from module images on both architectures.
Generated i32 and mixed module functions implement `memory.size` and bounded
`memory.grow`: the extended context publishes the fixed backing maximum, growth
validates page-to-byte overflow and capacity, zeroes the newly admitted range,
refreshes the current-length field, and returns the old page count or `-1`. The
i32 path also executes `nop`, `drop`, untyped i32 `select`, typed i32
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
checks. Direct mixed-width calls, calls into the compatible i32 module ABI,
multiple results, imported callbacks, indirect calls, and bounded stack
argument/result overflow are implemented. Typed `br` and `br_if` now target
arbitrary enclosing block/loop depths, moving one/two/four-slot branch values
into canonical target homes before transfer. Value-carrying `br_table` dispatch
uses per-target atomic merge blocks, and unconditional loop backedges continue to poll cancellation at their headers.
After unconditional transfers the planner now validates and skips arbitrary
byte-backed dead instructions, including nested block/loop/if structure, until
a reachable target merge is encountered.

Module metadata now retains local i32/i64/f32/f64/v128 globals with exact
mutability, serialized slot offsets, and raw constant initial values.
Instantiation preflights and initializes bounded caller-provided 32-bit cells,
`ContextABI` publishes their target base address, and scalar i32 plus mixed frame
functions execute `global.get`/`global.set` with immutable-set rejection. Pair and quad globals move through the frame atomically as complete values,
while mixed access preserves all surrounding wide operands. Nullable funcref
and externref values use one serialized slot; reference parameters, locals,
results, `ref.null`, `ref.func`, and `ref.is_null` now use the same mixed frame
and call ABI. The context now also reserves one stable pointer to a 16-byte
single-table descriptor containing table entries, length, published function
entries, and parallel function type IDs. Module metadata retains bounded local
funcref table limits and active element initializers; instantiation preflights
all active ranges before clearing and populating caller-owned table cells.
Mixed functions now execute bounded `table.get`, `table.set`, `table.size`,
`table.grow`, `table.fill`, and overlap-safe `table.copy`. Growth preflights the
fixed descriptor maximum, initializes every new complete slot, and only then
publishes the new length. Active, passive, and declarative element segments are
retained in index order; active ranges initialize transactionally, while
`table.init` and idempotent `elem.drop` use fixed 12-byte element descriptors
with dropped-state enforcement. `call_indirect` resolves function-index-plus-one
entries through parallel
published code/type arrays. Structurally identical core signatures share a
canonical type ID; bounds, null, and type mismatches publish distinct traps
before any target call.

The embedded runtime now also provides complete preflighted `memory.copy`,
`memory.fill`, passive `memory.init`, and idempotent `data.drop` semantics.
Normal i32 and mixed module code directly lower overlap-safe `memory.copy` and
`memory.fill` with complete source/destination preflight before byte loops.
Module layout retains active/passive segments in original index order;
instantiation preflights every active destination transactionally before copying
and starts active segments dropped. `ContextABI` now publishes a stable array of
12-byte target data descriptors, and normal i32 plus mixed functions execute
preflighted `memory.init` plus idempotent `data.drop` directly against those
descriptors.

Imported memory and the single imported funcref table now bind directly to the
same caller-owned `ContextABI` memory/table descriptors used by local state.
Imported-table instantiation preserves preexisting cells while transactionally
applying active element ranges; imported memory retains the host backing and
applies the same active-data preflight. Imported globals use a caller-owned
pointer directory at `ContextABI.ImportedGlobalsBase`, so mutable scalar and
wide cells remain aliased rather than copied into module-local storage. Local
constant initializers may read immutable imported globals, with every source
cell and destination range preflighted before any local global is published.

Every import now retains its module/name, kind-specific ordinal, exact
function signature and cross-module structural type ID, global type/mutability,
or table/memory limits. `ResolveEmbeddedLinks` resolves complete module/export
names transactionally, validates exact function/global contracts and Wasm limit
matching, rejects duplicate/missing providers, and produces a binding plan
before any target address is published. Function type IDs now use the shared
structural hash with in-module collision rejection, so identical signatures are
stable across independently compiled modules.

Function imports use a caller-published array of stable 8-byte
`ImportFunctionABI` descriptors at `ContextABI.ImportsBase`. Each descriptor
contains the callable entry and the callee `ContextABI`. Both i32 and mixed
callers save their own fixed context register, switch before dispatch, preserve
the serialized register/overflow ABI, copy the callee trap into the caller trap
cell, and restore the caller context before continuing. This supports direct
module-to-module calls as well as host callbacks and keeps local/global function
indexing stable.

Each exported local function now receives one deduplicated, 16-byte-aligned
entry thunk. Firmware passes a stable 12-byte `CallABI` containing target
addresses for `ContextABI`, serialized parameter slots, and serialized result
slots. The thunk stages the architecture's four/eight register slots plus the
bounded stack overflow area, preserves the fixed context register, invokes the
internal module ABI, and writes complete results only when the trap cell remains
clear.

Closed modules can now be serialized into one preflighted static firmware
image. The shared layout builder retains declared memory limits, lays out code,
`ContextABI`, trap/cancellation cells, helper addresses, serialized globals,
data/element descriptors and bytes, table storage, parallel function entry/type
arrays, and bounded linear-memory capacity in caller-provided storage. It
applies active data and element initializers only after complete capacity/range
validation, rejects unresolved imports, publishes target addresses without host
pointer assumptions, and preserves Thumb function-pointer bit zero through the
Arm-specific wrapper. The image reports conventional start and exported
`CallABI` entry addresses for a board harness.

Resolved function/global module graphs can now be laid out as one bounded
firmware bundle. The linker preflights every constituent image, serializes
context-aware function-import descriptors, points imported-global directories
at the provider's exact one/two/four-slot cells, evaluates immutable imported
global initializers through the binding graph, preserves target function-pointer
encoding, and publishes no addresses until every binding and capacity check
passes. Generated Arm32/RV32 tests execute calls with a distinct provider
context, restore consumer globals afterward, and propagate provider traps.
Imported memory now publishes `ContextABI.LinearMemoryContext`: every scalar,
SIMD, bulk-memory, size, and grow path reads or updates the provider context's
shared base/length/maximum fields while retaining the consumer's own trap and
data-segment state. Linked active data ranges are preflighted against the
provider's initial length, copied in module-instantiation order, and marked
dropped only after the complete bundle layout succeeds; passive `memory.init`
keeps the consumer descriptors.

Imported table execution now separates `ContextABI.TableStorage` from the
consumer's `ContextABI.Table` element descriptors. Get/set/size/grow/fill/copy,
`table.init`, and indirect dispatch therefore observe one caller-owned mutable
storage descriptor while `elem.drop` remains module-local. Firmware bundles
still reject imported tables because the current compact non-null funcref value
is a module-local function index rather than a cross-module function-reference
descriptor.

The runtime now defines the board-wire contract independently of UART/USB
plumbing: a versioned 24-byte little-endian frame header, sequence numbers,
bounded payload lengths, bitwise IEEE CRC32, distinct protocol/trap result
codes, fixed hello/call/slot payloads, and strict request/response kinds. A
caller-storage `TransportEndpoint` decodes parameter slots, preflights the full
response before dispatch, suppresses result publication on traps, and emits
hello/instantiate/start/call/cancel/reset responses without allocations.
`FirmwareTransportRunner` adds the single-image lifecycle, exact exported
parameter/result slot admission, start-before-call ordering, retryable failed
instantiation/start/reset, and trap propagation. Target wrappers construct it
from the image's filtered function-export table. The remaining firmware work is
the RP2350 SDK I/O loop and low-level generated-entry invoker, not another
host-dependent protocol.

Modules with a start function also append a 16-byte-aligned target entry thunk.
The thunk accepts only a `ContextABI` pointer in the platform's first argument
register, preserves the platform callee-saved context register and return
address, invokes the validated zero-argument/zero-result start function, and
returns the published trap code. This gives firmware a conventional ABI for
transactional instantiation/start sequencing without target-specific inline
assembly.

This is still not public backend admission. Cross-module funcref descriptors
for imported-table bundles, the RP2350 SDK transport I/O and low-level generated-entry
invoker, official module-level suite qualification, and Pico 2 hardware
qualification remain to be implemented and measured.
